// netfenced is the netfence plugin engine: a localhost-only management API
// plus an optional DNS sinkhole listener. Fully static (CGO_ENABLED=0) for the
// iKuai uClibc firmware; no kernel module, no root required for the API tier.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ikuai4/netfence/internal/api"
	"github.com/ikuai4/netfence/internal/core"
)

// Version is injected at build time with -X main.Version=<metadata.version>.
// build.py's version-gate reads this string back out of the binary.
var Version = "dev"

// [D9] 并发上限: 防止单包一 goroutine 在 DNS 洪泛下把路由器打爆。
const dnsMaxConcurrent = 256

var dnsSem = make(chan struct{}, dnsMaxConcurrent)

func main() {
	var (
		apiAddr  = flag.String("api", "127.0.0.1:18111", "management API listen address")
		dnsAddr  = flag.String("dns", "", "DNS sinkhole listen address (empty = disabled)")
		upstream = flag.String("upstream", "223.5.5.5:53", "upstream DNS for allowed names")
		cfgDir   = flag.String("config", "./config", "config dir on the plugin storage volume")
		showVer  = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *showVer {
		fmt.Println(Version)
		return
	}

	eng, err := core.NewEngine(core.Config{Dir: *cfgDir, ACL: core.DefaultACLConfig()})
	if err != nil {
		log.Fatalf("netfence: engine init: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", api.New(eng, Version, *cfgDir).Handler())
	srv := &http.Server{
		Addr:              *apiAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("netfence %s API on %s (config=%s)", Version, *apiAddr, *cfgDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("netfence: api: %v", err)
		}
	}()

	if *dnsAddr != "" {
		go func() {
			if err := serveDNSUDP(*dnsAddr, *upstream, eng); err != nil {
				log.Printf("netfence: dns udp stopped: %v", err)
			}
		}()
		// [D9] TCP/53: 大响应与 TC=1 截断后客户端会走 TCP, 旧版只听 UDP 会静默断解析
		go func() {
			if err := serveDNSTCP(*dnsAddr, *upstream, eng); err != nil {
				log.Printf("netfence: dns tcp stopped: %v", err)
			}
		}()
	}

	// SIGHUP = hot reload config without dropping the listener.
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	for s := range sig {
		if s == syscall.SIGHUP {
			if err := eng.Reload(*cfgDir); err != nil {
				log.Printf("netfence: reload failed: %v", err)
			} else {
				log.Printf("netfence: config reloaded")
			}
			continue
		}
		log.Printf("netfence: shutting down (%v)", s)
		_ = srv.Close()
		return
	}
}

func serveDNSUDP(addr, upstream string, eng *core.Engine) error {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	log.Printf("netfence: DNS udp/%s -> %s", addr, upstream)
	buf := make([]byte, 4096)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		q := append([]byte(nil), buf[:n]...)
		select {
		case dnsSem <- struct{}{}:
			go func() {
				defer func() { <-dnsSem }()
				resp := handleDNS(q, upstream, from, eng, "dns")
				if resp != nil {
					_, _ = conn.WriteTo(resp, from)
				}
			}()
		default:
			// 打满时回 SERVFAIL, 让客户端快速重试而不是静默超时
			_, _ = conn.WriteTo(servfailResponse(q), from)
		}
	}
}

// serveDNSTCP implements DNS-over-TCP (2-byte length prefix, RFC 7766).
func serveDNSTCP(addr, upstream string, eng *core.Engine) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	log.Printf("netfence: DNS tcp/%s -> %s", addr, upstream)
	for {
		c, err := ln.Accept()
		if err != nil {
			// 临时错误(EMFILE 等)不应让 TCP/53 永久退出; 短暂退避后重试。
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				log.Printf("netfence: dns tcp accept temporary: %v", err)
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return err
		}
		select {
		case dnsSem <- struct{}{}:
			go func() {
				defer func() { <-dnsSem; _ = c.Close() }()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				var lb [2]byte
				if _, err := io.ReadFull(c, lb[:]); err != nil {
					return
				}
				n := int(binary.BigEndian.Uint16(lb[:]))
				if n == 0 || n > 4096 {
					return
				}
				q := make([]byte, n)
				if _, err := io.ReadFull(c, q); err != nil {
					return
				}
				resp := handleDNS(q, upstream, c.RemoteAddr(), eng, "tcp")
				if resp == nil {
					return
				}
				out := make([]byte, 2+len(resp))
				binary.BigEndian.PutUint16(out[:2], uint16(len(resp)))
				copy(out[2:], resp)
				_, _ = c.Write(out)
			}()
		default:
			_ = c.Close()
		}
	}
}

// handleDNS 返回完整的 DNS 应答字节(或 nil 丢弃)。UDP 与 TCP 共用裁决逻辑。
func handleDNS(q []byte, upstream string, from net.Addr, eng *core.Engine, source string) []byte {
	name, ok := dnsQuestion(q)
	if !ok {
		return nil
	}
	clientIP := ""
	switch a := from.(type) {
	case *net.UDPAddr:
		clientIP = a.IP.String()
	case *net.TCPAddr:
		clientIP = a.IP.String()
	}
	req := core.Request{ClientIP: clientIP, Domain: name, Source: source, At: time.Now()}
	d := eng.Decide(req)
	if d.Blocked() {
		return sinkholeResponse(q)
	}

	// 转发给上游, 保留事务 ID
	up, err := net.DialTimeout("udp", upstream, 3*time.Second)
	if err != nil {
		log.Printf("netfence: upstream dial %s: %v", upstream, err)
		return servfailResponse(q) // [D9] 明确 SERVFAIL, 不再静默空应答
	}
	defer up.Close()
	_ = up.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = up.Write(q); err != nil {
		return servfailResponse(q)
	}
	resp := make([]byte, 4096)
	n, err := up.Read(resp)
	if err != nil {
		log.Printf("netfence: upstream read %s: %v", upstream, err)
		return servfailResponse(q) // 上游超时不静默, 客户端可重试/切上游
	}
	r := resp[:n]

	// [D8] 上游应答里的 A/AAAA 记录再过一遍威胁情报 L3 判定:
	// 旧版 DNS 路径不带 DstIP, 威胁 IP 层永远不可达。
	if ips := answerIPs(r); len(ips) > 0 {
		if hit, blocked := eng.DecideResolvedIP(req, ips); blocked {
			log.Printf("netfence: blocked resolved IP %s for %s", hit, name)
			return sinkholeResponse(q)
		}
	}
	return r
}

// answerIPs 从 DNS 应答的 Answer 段提取 A/AAAA 地址(只解析不压缩的 rdata,
// 覆盖绝大多数上游应答; 压缩名用指针跳过)。
func answerIPs(msg []byte) []string {
	if len(msg) < 12 {
		return nil
	}
	qd := int(binary.BigEndian.Uint16(msg[4:6]))
	an := int(binary.BigEndian.Uint16(msg[6:8]))
	if an == 0 || an > 64 {
		return nil
	}
	i := 12
	// 跳过 Question 段
	for k := 0; k < qd && i < len(msg); k++ {
		for i < len(msg) && msg[i] != 0 {
			if msg[i]&0xc0 == 0xc0 {
				i += 2
				break
			}
			i += 1 + int(msg[i])
		}
		if i < len(msg) {
			i++ // 结尾 0
		}
		i += 4 // QTYPE+QCLASS
	}
	var out []string
	for k := 0; k < an && i < len(msg); k++ {
		// NAME(可能是指针)
		if i >= len(msg) {
			break
		}
		if msg[i]&0xc0 == 0xc0 {
			i += 2
		} else {
			for i < len(msg) && msg[i] != 0 {
				i += 1 + int(msg[i])
			}
			i++
		}
		if i+10 > len(msg) {
			break
		}
		typ := binary.BigEndian.Uint16(msg[i : i+2])
		rdlen := int(binary.BigEndian.Uint16(msg[i+8 : i+10]))
		i += 10
		if rdlen < 0 || i+rdlen > len(msg) {
			break
		}
		rd := msg[i : i+rdlen]
		switch typ {
		case 1: // A
			if len(rd) == 4 {
				out = append(out, net.IP(rd).String())
			}
		case 28: // AAAA
			if len(rd) == 16 {
				out = append(out, net.IP(rd).String())
			}
		}
		i += rdlen
	}
	return out
}

// dnsQuestion extracts the first QNAME from a DNS query.
func dnsQuestion(msg []byte) (string, bool) {
	if len(msg) < 12 {
		return "", false
	}
	qd := binary.BigEndian.Uint16(msg[4:6])
	if qd == 0 {
		return "", false
	}
	var labels []string
	for i := 12; i < len(msg); {
		l := int(msg[i])
		if l == 0 {
			i++
			break
		}
		if l&0xc0 == 0xc0 || i+1+l > len(msg) {
			return "", false
		}
		labels = append(labels, string(msg[i+1:i+1+l]))
		i += 1 + l
	}
	if len(labels) == 0 {
		return "", false
	}
	return strings.Join(labels, "."), true
}

// sinkholeResponse: 被策略/情报拦截时返回无记录的成功应答(域名解析不到)。
func sinkholeResponse(q []byte) []byte { return responseWithRcode(q, 0) }

// servfailResponse: 上游失败/过载时返回 SERVFAIL, 让客户端明确感知而非空等。
func servfailResponse(q []byte) []byte { return responseWithRcode(q, 2) }

func responseWithRcode(q []byte, rcode byte) []byte {
	if len(q) < 12 {
		return nil
	}
	r := append([]byte(nil), q...)
	r[2] |= 0x80 // QR=1
	r[3] = (r[3] & 0xf0) | (rcode & 0x0f)
	binary.BigEndian.PutUint16(r[6:8], 0)  // ANCOUNT
	binary.BigEndian.PutUint16(r[8:10], 0) // NSCOUNT
	binary.BigEndian.PutUint16(r[10:12], 0)
	return r
}
