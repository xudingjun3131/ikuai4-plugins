package proxy

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elazarl/goproxy"

	"github.com/GitterBois/runhooks/internal/ca"
	"github.com/GitterBois/runhooks/internal/store"
)

type ConnectPolicy interface {
	SSLBypass(host string) bool
	ClientAllowed(clientIP, host string) bool
	SecondaryProxyURL(host string) string
}

// Engine is the HTTP(S) MITM capture proxy (goproxy + local CA + store.Store).
type Engine struct {
	auth    *ca.Authority
	sink    *store.Store
	maxBody int64
	hooksMu sync.RWMutex
	hooks   HookSlots
	policy  ConnectPolicy
	px      *goproxy.ProxyHttpServer
	log     *slog.Logger
	scope   *ScopeStore // 域名拦截范围(nil=解密所有)
}

// logf 透明模式等新增路径的诊断日志(不传 logger 时静默)。
func (e *Engine) logf(format string, args ...any) {
	if e.log == nil {
		return
	}
	e.log.Info(fmt.Sprintf(format, args...))
}

// EngineConfig constructs an Engine.
type EngineConfig struct {
	Authority *ca.Authority
	Sink      *store.Store
	MaxBody   int64 // max captured body bytes per message; <0 → 0
	Hooks     HookSlots
	Policy    ConnectPolicy
	Scope     *ScopeStore
}

// NewEngine builds a MITM proxy. Authority and Sink are required.
// Client ALPN: http/1.1 only (full OnRequest/OnResponse capture). Origin may still use H2 via ForceAttemptHTTP2.
// WS: upgrade handshake captured; frames parsed into Flow.WSFrames (capped).
func NewEngine(cfg EngineConfig) (*Engine, error) {
	if cfg.Authority == nil {
		return nil, errors.New("proxy: Authority is required")
	}
	if cfg.Sink == nil {
		return nil, errors.New("proxy: Sink is required")
	}
	maxBody := cfg.MaxBody
	if maxBody < 0 {
		maxBody = 0
	}

	e := &Engine{
		auth:    cfg.Authority,
		sink:    cfg.Sink,
		maxBody: maxBody,
		hooks:   cfg.Hooks,
		policy:  cfg.Policy,
		scope:   cfg.Scope,
		log:     slog.Default(),
		px:      goproxy.NewProxyHttpServer(),
	}
	e.px.Verbose = false
	e.px.AllowHTTP2 = true
	// [keep-accept-encoding] goproxy 默认在转发前删掉 Accept-Encoding, 删完 Go
	// Transport 又自动补 gzip —— 规则里设的 Accept-Encoding: identity(让内容替换
	// 能匹配明文)会被这俩联手干掉, 上游永远回压缩流。打开此开关让请求头规则说了算:
	// 没规则时客户端原始值直传(行为与之前等价, 都是压缩流), 有规则时 identity 生效。
	e.px.KeepAcceptEncoding = true
	e.px.Tr = &http.Transport{
		Proxy:                 e.upstreamProxy,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // MITM capture origin dial
			NextProtos:         []string{"h2", "http/1.1"},
		},
	}

	mitm := &goproxy.ConnectAction{
		Action:    goproxy.ConnectMitm,
		TLSConfig: e.tlsConfigForHost,
	}
	e.px.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		clientIP := ""
		if ctx != nil && ctx.Req != nil && ctx.Req.RemoteAddr != "" {
			h, _, err := net.SplitHostPort(ctx.Req.RemoteAddr)
			if err == nil {
				clientIP = h
			}
		}
		if e.policy != nil && clientIP != "" && !e.policy.ClientAllowed(clientIP, host) {
			return goproxy.RejectConnect, host
		}
		if e.scope != nil && e.scope.Bypass(host) {
			e.logf("scope: bypass(connect) %s", host)
			return goproxy.OkConnect, host
		}
		if e.policy != nil && e.policy.SSLBypass(host) {
			return goproxy.OkConnect, host
		}
		return mitm, host
	})
	e.px.OnRequest().DoFunc(e.onRequest)
	e.px.OnResponse().DoFunc(e.onResponse)
	return e, nil
}

func (e *Engine) upstreamProxy(req *http.Request) (*url.URL, error) {
	if e.policy == nil || req == nil {
		return nil, nil
	}
	host := ""
	if req.URL != nil {
		host = req.URL.Hostname()
	}
	if host == "" {
		host = req.Host
	}
	raw := e.policy.SecondaryProxyURL(host)
	if raw == "" {
		return nil, nil
	}
	return url.Parse(raw)
}

// SetScope 设置域名范围(nil=解密所有)。
func (e *Engine) SetScope(s *ScopeStore) { e.scope = s }

// SetScope 之后由 ScopeStore.Reload 热重载, 引擎无需重启。

func (e *Engine) SetPolicy(p ConnectPolicy) {
	e.policy = p
}

// Handler returns the CONNECT+forward HTTP handler.
func (e *Engine) Handler() http.Handler { return e.px }

// NewServer returns an http.Server on addr with the MITM handler.
// [fd-limit] 同 api.NewServer: 显式代理的浏览器 keep-alive 连接同样会吃光 nofile。
// CONNECT 隧道连接会被 hijack, 不受这两个超时影响; 空闲的 keep-alive 90s 回收。
func (e *Engine) NewServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           e.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
}

func (e *Engine) SetRulesSlot(h ProxyHooks) {
	e.hooksMu.Lock()
	e.hooks.Rules = h
	e.hooksMu.Unlock()
}

func (e *Engine) SetScriptSlot(h ProxyHooks) {
	e.hooksMu.Lock()
	e.hooks.Script = h
	e.hooksMu.Unlock()
}

func (e *Engine) SetBreakpointSlot(h ProxyHooks) {
	e.hooksMu.Lock()
	e.hooks.Breakpoint = h
	e.hooksMu.Unlock()
}

func (e *Engine) slots() HookSlots {
	e.hooksMu.RLock()
	defer e.hooksMu.RUnlock()
	return e.hooks
}

func (e *Engine) tlsConfigForHost(host string, _ *goproxy.ProxyCtx) (*tls.Config, error) {
	cert, err := e.auth.Certificate(host)
	if err != nil {
		return nil, fmt.Errorf("proxy: leaf cert for %q: %w", host, err)
	}
	// Only advertise http/1.1 to client → full OnRequest/OnResponse capture (avoids goproxy H2Transport frame tunnel).
	return &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"http/1.1"},
	}, nil
}

type flowCtx struct {
	started        time.Time
	clientAddr     string
	scheme         string
	reqMsg         store.Message
	highlightColor string
	highlightLabel string
	ja3            string
	dstIP          string
}

// ctxKey 透明路径附加信息(JA3/目标IP)用的 context key
type ctxKey int

const (
	ctxKeyJA3   ctxKey = 1
	ctxKeyDstIP ctxKey = 2
)

func (e *Engine) onRequest(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	if req == nil || req.Method == http.MethodConnect {
		return req, nil
	}

	req, early := e.slots().runRequest(req)
	if early != nil {
		e.captureShortCircuit(req, early, ctx)
		return req, early
	}

	fwd, body, trunc, size, err := snapshotBody(req.Body, e.maxBody)
	if err == nil && size == 0 {
		req.Body = http.NoBody // 避免 Go Transport 对空 body 误判 chunked
	} else {
		req.Body = fwd
	}
	if err == nil {
		req.ContentLength = size
		req.TransferEncoding = nil
		req.Header.Del("Transfer-Encoding")
		req.Header.Del("Content-Length")
		if size > 0 {
			req.Header.Set("Content-Length", strconv.FormatInt(size, 10))
		}
	}
	// [compat-400] 转发前剥离逐跳头与内部标记: 显式代理客户端会带
	// Proxy-Connection, 高亮规则会注入 X-Proxying-Highlight*, 这些都不该发给上游,
	// 部分站点/WAF 见到直接回 400。
	req.Header.Del("X-Proxying-Highlight")
	req.Header.Del("X-Proxying-Highlight-Color")
	// WebSocket / protocol upgrades MUST keep Connection: Upgrade and Upgrade,
	// stripping them makes upstream answer 400 (Test_Engine_WS_captures_frames).
	isUpgrade := req.Header.Get("Upgrade") != "" || headerHas(req.Header, "Connection", "Upgrade")
	if !isUpgrade {
		for _, k := range []string{
			"Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization",
			"Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade",
		} {
			req.Header.Del(k)
		}
		for _, v := range req.Header.Values("Connection") {
			for _, tok := range strings.Split(v, ",") {
				if tok = strings.TrimSpace(tok); tok != "" {
					req.Header.Del(tok)
				}
			}
		}
		req.Header.Del("Connection")
	}

	fc := &flowCtx{
		started:    time.Now(),
		clientAddr: req.RemoteAddr,
		scheme:     schemeOf(req),
		reqMsg:     messageFromRequest(req, body, trunc, size),
	}
	if err != nil {
		fc.reqMsg = messageFromRequest(req, nil, false, 0)
	}
	fc.highlightLabel = headerFirst(req.Header, "X-Proxying-Highlight")
	fc.highlightColor = headerFirst(req.Header, "X-Proxying-Highlight-Color")
	if v, ok := req.Context().Value(ctxKeyJA3).(string); ok {
		fc.ja3 = v
	}
	if v, ok := req.Context().Value(ctxKeyDstIP).(string); ok {
		fc.dstIP = v
	}
	if e.policy != nil {
		if hf, ok := e.policy.(interface {
			HighlightFor(host, path, method string) (string, string, bool)
		}); ok {
			host, path := "", ""
			if req.URL != nil {
				host = req.URL.Hostname()
				path = req.URL.Path
			}
			if host == "" {
				host = req.Host
			}
			if c, l, ok := hf.HighlightFor(host, path, req.Method); ok {
				if fc.highlightColor == "" {
					fc.highlightColor = c
				}
				if fc.highlightLabel == "" {
					fc.highlightLabel = l
				}
			}
		}
	}
	ctx.UserData = fc
	return req, nil
}

func headerFirst(h http.Header, key string) string {
	if h == nil {
		return ""
	}
	return h.Get(key)
}

// [self-probe-filter] 路由器自身对各监听口的健康探测(实测: 对透明口 18062
// 的 https 探测约 200 次/秒, 全部为 state=error 的 flow, clientAddr 唯一值
// 是路由器自己的 LAN IP, 能把 5000 条 ring buffer 顶满、把真实设备画像挤成
// 「1 台(路由器自己)」)。这类流量不是用户要观测的局域网设备流量, 在统一记录
// 入口排除掉。只按源 IP 判, 不动转发链路 —— 探测连接照常走完代理/握手流程,
// 只是不落库。
// 逃生开关 RUNHOOKS_RECORD_SELF_TRAFFIC=1 用于本地冒烟测试(从 127.0.0.1 造流)。
var (
	selfIPOnce sync.Once
	selfIPv4s  map[string]bool
)

func localIPv4Set() map[string]bool {
	selfIPOnce.Do(func() {
		selfIPv4s = make(map[string]bool)
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if v4 := ipNet.IP.To4(); v4 != nil {
				selfIPv4s[v4.String()] = true
			}
		}
	})
	return selfIPv4s
}

// isSelfClient 判断 "host:port" / 裸 IP / "[ipv6]:port" 是否来自本机。
func isSelfClient(clientAddr string) bool {
	host := clientAddr
	if h, _, err := net.SplitHostPort(clientAddr); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		return localIPv4Set()[v4.String()]
	}
	return false
}

// record 记录层过滤: 命中范围规则的才保存(MITM 已发生, 只是不落库)。
func (e *Engine) record(f store.Flow) (store.FlowID, bool) {
	if os.Getenv("RUNHOOKS_RECORD_SELF_TRAFFIC") != "1" && isSelfClient(f.ClientAddr) {
		return "", false
	}
	if e.scope != nil && !e.scope.ShouldRecord(
		f.Request.Method,
		pathOf(f.Request.URL),
		firstHeader(f.Request.Headers, "User-Agent"),
		f.JA3,
		firstHeader(f.Response.Headers, "Content-Type"),
		f.Response.Size,
	) {
		return "", false
	}
	return e.sink.Add(f)
}

func pathOf(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Path != "" {
		return u.Path
	}
	return rawURL
}

func firstHeader(h map[string][]string, key string) string {
	if v, ok := h[key]; ok && len(v) > 0 {
		return v[0]
	}
	// http.Header 规范化大小写, 兜底再扫一遍
	lk := strings.ToLower(key)
	for k, v := range h {
		if strings.ToLower(k) == lk && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

func (e *Engine) onResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	fc, _ := ctx.UserData.(*flowCtx)
	if resp == nil {
		e.finishError(ctx, "nil response")
		return nil
	}
	resp = e.slots().runResponse(resp)

	// WebSocket: do not snapshot body (it is a live ReadWriter). Capture handshake + wrap for frames.
	if resp.StatusCode == http.StatusSwitchingProtocols && isWSUpgrade(resp.Header) {
		return e.onWSResponse(resp, ctx, fc)
	}

	fwd, body, trunc, size, err := snapshotBody(resp.Body, e.maxBody)
	resp.Body = fwd
	if err == nil {
		// [compat-400] body 已全量读入内存, 用确定的 Content-Length 回写;
		// 必须同时清掉上游的 Transfer-Encoding: chunked, 否则 CL 与 TE 并存
		// 属于非法 HTTP 响应, 严格的客户端/中间层会报 400。
		resp.ContentLength = size
		resp.TransferEncoding = nil
		resp.Header.Del("Transfer-Encoding")
		resp.Header.Del("Content-Length")
		if size > 0 {
			resp.Header.Set("Content-Length", strconv.FormatInt(size, 10))
		} else {
			resp.ContentLength = 0
		}
	}

	if fc == nil {
		return resp
	}
	done := time.Now()
	flow := store.Flow{
		State:          store.StateDone,
		StartedAt:      fc.started,
		DoneAt:         done,
		ClientAddr:     fc.clientAddr,
		Scheme:         fc.scheme,
		Protocol:       protoOf(fc.reqMsg.Proto, resp.Proto),
		Request:        fc.reqMsg,
		Response:       messageFromResponse(resp, body, trunc, size),
		HighlightColor: fc.highlightColor,
		HighlightLabel: fc.highlightLabel,
		JA3:            fc.ja3,
		DstIP:          fc.dstIP,
	}
	if err != nil {
		flow.State = store.StateError
		flow.Error = err.Error()
	}
	if ctx.Error != nil {
		flow.State = store.StateError
		flow.Error = ctx.Error.Error()
	}
	_, _ = e.record(flow)
	ctx.UserData = nil
	return resp
}

func (e *Engine) onWSResponse(resp *http.Response, ctx *goproxy.ProxyCtx, fc *flowCtx) *http.Response {
	rw, ok := resp.Body.(io.ReadWriter)
	if !ok {
		// fall through: cannot inspect
		if fc != nil {
			now := time.Now()
			_, _ = e.record(store.Flow{
				State:      store.StateDone,
				StartedAt:  fc.started,
				DoneAt:     now,
				ClientAddr: fc.clientAddr,
				Scheme:     fc.scheme,
				Protocol:   "websocket",
				Request:    fc.reqMsg,
				Response:   messageFromResponse(resp, nil, false, 0),
			})
			ctx.UserData = nil
		}
		return resp
	}
	var started time.Time
	var clientAddr, scheme string
	var reqMsg store.Message
	if fc != nil {
		started = fc.started
		clientAddr = fc.clientAddr
		scheme = fc.scheme
		reqMsg = fc.reqMsg
	} else {
		started = time.Now()
	}
	id, okAdd := e.record(store.Flow{
		State:      store.StateActive,
		StartedAt:  started,
		ClientAddr: clientAddr,
		Scheme:     scheme,
		Protocol:   "websocket",
		Request:    reqMsg,
		Response:   messageFromResponse(resp, nil, false, 0),
	})
	cap := newWSCapture(rw, e.sink, id, e.maxBody)
	if !okAdd {
		cap.flowID = ""
	}
	// goproxy checks Body.(io.ReadWriter) after this handler returns.
	resp.Body = &wsBody{wsCapture: cap}
	ctx.UserData = nil
	return resp
}

// wsBody embeds wsCapture as http.Response.Body (ReadCloser + Writer for 101).
type wsBody struct {
	*wsCapture
}

func (b *wsBody) Close() error {
	b.finish()
	if c, ok := b.inner.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func isWSUpgrade(h http.Header) bool {
	return headerHas(h, "Connection", "Upgrade") && headerHas(h, "Upgrade", "websocket")
}

func headerHas(h http.Header, name, value string) bool {
	for _, v := range h.Values(name) {
		for _, p := range splitComma(v) {
			if equalFoldTrim(p, value) {
				return true
			}
		}
	}
	return false
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

func equalFoldTrim(a, b string) bool {
	for len(a) > 0 && (a[0] == ' ' || a[0] == '\t') {
		a = a[1:]
	}
	for len(a) > 0 && (a[len(a)-1] == ' ' || a[len(a)-1] == '\t') {
		a = a[:len(a)-1]
	}
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func protoOf(reqProto, respProto string) string {
	if respProto != "" {
		return respProto
	}
	return reqProto
}

func (e *Engine) captureShortCircuit(req *http.Request, resp *http.Response, ctx *goproxy.ProxyCtx) {
	_, reqBody, reqTrunc, reqSize, _ := snapshotBody(req.Body, e.maxBody)
	req.Body = io.NopCloser(bytes.NewReader(reqBody))

	_, rb, rt, rs, _ := snapshotBody(resp.Body, e.maxBody)
	resp.Body = io.NopCloser(bytes.NewReader(rb))
	resp.ContentLength = int64(len(rb))

	now := time.Now()
	_, _ = e.record(store.Flow{
		State:      store.StateDone,
		StartedAt:  now,
		DoneAt:     now,
		ClientAddr: req.RemoteAddr,
		Scheme:     schemeOf(req),
		Request:    messageFromRequest(req, reqBody, reqTrunc, reqSize),
		Response:   messageFromResponse(resp, rb, rt, rs),
	})
	ctx.UserData = nil
}

func (e *Engine) finishError(ctx *goproxy.ProxyCtx, msg string) {
	fc, _ := ctx.UserData.(*flowCtx)
	if fc == nil {
		return
	}
	errMsg := msg
	if ctx.Error != nil {
		errMsg = ctx.Error.Error()
	}
	_, _ = e.record(store.Flow{
		State:      store.StateError,
		StartedAt:  fc.started,
		DoneAt:     time.Now(),
		ClientAddr: fc.clientAddr,
		Scheme:     fc.scheme,
		Request:    fc.reqMsg,
		Error:      errMsg,
	})
	ctx.UserData = nil
}
