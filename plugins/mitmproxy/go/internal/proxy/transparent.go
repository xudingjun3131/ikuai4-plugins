package proxy

// 透明模式（iKuai 插件版新增）：iptables REDIRECT 把 80/443 转到代理端口后，
// 通过 SO_ORIGINAL_DST 拿真实目标，就地 MITM 后改写成绝对 URL 走 goproxy 全链路
// （rules/script/breakpoint/capture 全部复用），无需客户端配代理。

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const soOrigDst = 80 // SO_ORIGINAL_DST

// originalDst 从被 REDIRECT 过的连接取原始目标（IPv4）。
func originalDst(conn net.Conn) (string, int, error) {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return "", 0, errors.New("transparent: not a tcp connection")
	}
	var mreq *unix.IPv6Mreq
	var err error
	rc, rerr := tc.SyscallConn()
	if rerr != nil {
		return "", 0, rerr
	}
	rc.Control(func(fd uintptr) {
		mreq, err = unix.GetsockoptIPv6Mreq(int(fd), unix.SOL_IP, soOrigDst)
	})
	if err != nil {
		return "", 0, err
	}
	if mreq == nil {
		return "", 0, errors.New("transparent: nil SO_ORIGINAL_DST")
	}
	// sockaddr_in 布局映射到 IPv6Mreq.Multiaddr: [0:2]=family [2:4]=port(大端) [4:8]=IPv4
	port := int(binary.BigEndian.Uint16(mreq.Multiaddr[2:4]))
	host := net.IPv4(mreq.Multiaddr[4], mreq.Multiaddr[5], mreq.Multiaddr[6], mreq.Multiaddr[7]).String()
	return host, port, nil
}

// StartTransparent 在 addr 上启动透明模式监听（由 app 在 -transparent 时调用）。
func (e *Engine) StartTransparent(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go e.acceptTransparent(ln)
	return nil
}

func (e *Engine) acceptTransparent(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go e.serveTransparent(c)
	}
}

func (e *Engine) serveTransparent(clientConn net.Conn) {
	defer clientConn.Close()
	origHost, origPort, err := originalDst(clientConn)
	if err != nil {
		e.logf("transparent: SO_ORIGINAL_DST failed: %v", err)
		return
	}
	origAddr := net.JoinHostPort(origHost, strconv.Itoa(origPort))

	br := bufio.NewReaderSize(clientConn, 32*1024)
	// 探测 TLS ClientHello（首字节 0x16）
	first, err := br.Peek(1)
	if err != nil {
		return // 客户端秒连秒断, 正常噪声
	}
	// 连接层范围: 命中绕过规则的只做原始 TCP 隧道, 不 MITM 不解密不记录
	// (域名/目标IP/GEOIP 三维, 目标IP 即 origHost)
	if e.scope != nil && e.scope.BypassConn(origHost, origHost) {
		e.logf("scope: bypass(transparent) %s", origAddr)
		e.tunnelRaw(clientConn, br, origAddr)
		return
	}

	// JA3: TLS 流量从 ClientHello 算指纹(显式代理路径算不了)
	var ja3 string
	if first[0] == 0x16 {
		if hello, herr := br.Peek(5); herr == nil {
			recLen := int(binary.BigEndian.Uint16(hello[3:5]))
			if full, perr := br.Peek(5 + recLen); perr == nil {
				ja3 = ja3Fingerprint(full[5:])
			}
		}
	}

	scheme := "http"
	var appReader *bufio.Reader = br
	var clientWriter io.Writer = clientConn // HTTPS 时必须是 tlsConn(加密后发出), 不能写裸 conn
	if first[0] == 0x16 {
		scheme = "https"
		// 关键: Peek 已把 ClientHello 读进 br, tls.Server 必须经 bufConn 走同一缓冲,
		// 否则握手读裸 conn 等客户端已发过的字节 → 死锁
		tlsConn := tls.Server(&bufConn{Conn: clientConn, r: br}, &tls.Config{
			// 只协商 http/1.1（客户端 H2 会让请求绕过我们的 HTTP/1.1 读取路径）
			NextProtos: []string{"http/1.1"},
			// 按 SNI 动态签发证书（无 SNI 时退化为原始目标 IP）
			GetConfigForClient: func(hi *tls.ClientHelloInfo) (*tls.Config, error) {
				host := hi.ServerName
				if host == "" {
					host = origHost
				}
				cfg, err := e.tlsConfigForHost(host, nil)
				if err != nil {
					return nil, err
				}
				cfg.NextProtos = []string{"http/1.1"}
				return cfg, nil
			},
		})
		if err := tlsConn.HandshakeContext(context.Background()); err != nil {
			e.logf("transparent: tls handshake %s: %v", origAddr, err)
			return
		}
		// 后续读从 tlsConn 走(内部经 bufConn->br->conn, 缓冲字节不丢)
		appReader = bufio.NewReader(tlsConn)
		clientWriter = tlsConn
	}

	// HTTP/1.1 keep-alive 循环
	for {
		req, err := http.ReadRequest(appReader)
		if err != nil {
			return
		}
		req.URL.Scheme = scheme
		req.URL.Host = origAddr
		req.RequestURI = "" // 绝对 URL 模式要求
		req.RemoteAddr = clientConn.RemoteAddr().String()
		rctx := context.WithValue(req.Context(), ctxKeyJA3, ja3)
		rctx = context.WithValue(rctx, ctxKeyDstIP, origHost)
		req = req.WithContext(rctx)
		rw := &connRW{conn: clientConn, w: clientWriter, br: appReader, remoteAddr: req.RemoteAddr}
		e.px.ServeHTTP(rw, req)
		if rw.closeAfter {
			return
		}
		if req.Header.Get("Connection") == "close" {
			return
		}
	}
}

// tunnelRaw 原始 TCP 隧道（域名绕过时用：不解密、不记录）。
// 必须从 bufio.Reader 读——ClientHello/请求字节已被 Peek 进缓冲，读裸 conn 会丢字节。
func (e *Engine) tunnelRaw(clientConn net.Conn, br *bufio.Reader, origAddr string) {
	origin, err := net.DialTimeout("tcp", origAddr, 10*time.Second)
	if err != nil {
		e.logf("transparent: tunnel dial %s: %v", origAddr, err)
		return
	}
	defer origin.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(origin, br); done <- struct{}{} }()
	go func() { io.Copy(clientConn, origin); done <- struct{}{} }()
	<-done
}

// bufConn 让 TLS 握手复用已被 Peek 填充的 bufio 缓冲。
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(b []byte) (int, error) { return c.r.Read(b) }

// connRW 把 goproxy 的 http.ResponseWriter 输出写回客户端裸连接。
type connRW struct {
	conn       net.Conn // 裸连接(Hijack/Deadline 用)
	w          io.Writer // 协议写入侧(HTTPS=tlsConn, HTTP=裸conn)
	br         *bufio.Reader
	remoteAddr string
	hdr        http.Header
	wroteHdr   bool
	closeAfter bool
	mu         sync.Mutex
}

func (w *connRW) Header() http.Header {
	if w.hdr == nil {
		w.hdr = make(http.Header)
	}
	return w.hdr
}

func (w *connRW) WriteHeader(code int) {
	if w.wroteHdr {
		return
	}
	w.wroteHdr = true
	// 无 Content-Length 时响应结束只能靠连接关闭，标记 keep-alive 不可用
	if w.hdr.Get("Content-Length") == "" && w.hdr.Get("Transfer-Encoding") == "" && code != http.StatusSwitchingProtocols {
		w.closeAfter = true
	}
	var b []byte
	b = append(b, "HTTP/1.1 "...)
	b = strconv.AppendInt(b, int64(code), 10)
	b = append(b, ' ')
	b = append(b, http.StatusText(code)...)
	b = append(b, '\r', '\n')
	for k, vs := range w.hdr {
		for _, v := range vs {
			b = append(b, k...)
			b = append(b, ':', ' ')
			b = append(b, v...)
			b = append(b, '\r', '\n')
		}
	}
	b = append(b, '\r', '\n')
	w.conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
	w.w.Write(b)
}

func (w *connRW) Write(p []byte) (int, error) {
	if !w.wroteHdr {
		w.WriteHeader(http.StatusOK)
	}
	w.conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
	return w.w.Write(p)
}

func (w *connRW) Flush() {
	// 裸连接无服务端缓冲，无需操作；SSE 场景由 Write 直接透传
}

// Hijacker 支持 goproxy 的 WebSocket 升级路径。
func (w *connRW) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(w.br, bufio.NewWriter(w.conn)), nil
}

var _ http.ResponseWriter = (*connRW)(nil)
var _ http.Hijacker = (*connRW)(nil)

// 确保 io 引用（未来扩展）
var _ io.Writer = (io.Writer)(nil)
