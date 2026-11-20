// Package breakpoint pauses matched in-flight requests/responses for edit+resume.
//
// Hold model: OnRequest/OnResponse block on a channel until Resume or timeout.
// goproxy runs hooks on the request goroutine, so this holds that connection
// for up to Timeout (default 60s). Max concurrent holds is Max (default 32).
// Residual risk: long holds pin proxy worker capacity; CONNECT/H2 frames not held.
package breakpoint

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GitterBois/runhooks/internal/httpx"
	"github.com/GitterBois/runhooks/internal/proxy"
)

// Phase is request or response hold.
type Phase string

const (
	PhaseRequest  Phase = "request"
	PhaseResponse Phase = "response"
)

// Match selects which traffic to pause (empty host = match all when enabled).
type Match struct {
	Host    string   `json:"host,omitempty"`
	Path    string   `json:"path,omitempty"`
	Methods []string `json:"methods,omitempty"`
}

// Session is a held request or response awaiting resume.
type Session struct {
	ID        string            `json:"id"`
	Phase     Phase             `json:"phase"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Status    int               `json:"status,omitempty"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
	CreatedAt time.Time         `json:"createdAt"`
}

// ResumeEdit optional mutations on resume.
type ResumeEdit struct {
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url,omitempty"`
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    *string           `json:"body,omitempty"`
}

type hold struct {
	session Session
	edit    chan ResumeEdit
	cancel  chan struct{}
}

// Stats is the live capacity picture for the hold pool. v1.1.7 测试报告 §3.6
// 把「断点 hold 占连接容量(默认 32/60s)」列为质量风险: 以前没有任何可观测
// 指标, 也没有主动释放手段。
type Stats struct {
	Max        int   `json:"max"`
	TimeoutMs  int64 `json:"timeoutMs"`
	Holds      int   `json:"holds"`
	Total      int64 `json:"total"`
	Resumed    int64 `json:"resumed"`
	Expired    int64 `json:"expired"`
	Aborted    int64 `json:"aborted"`
	ClientGone int64 `json:"clientGone"`
	Rejected   int64 `json:"rejected"`
}

// Manager implements proxy.ProxyHooks and the breakpoint control plane.
type Manager struct {
	mu      sync.Mutex
	max     int
	timeout time.Duration
	enabled bool
	match   Match
	holds   map[string]*hold
	stop    chan struct{}

	total, resumed, expired, aborted, clientGone, rejected int64
}

// NewManager creates a breakpoint manager (disabled until SetEnabled).
func NewManager(max int, timeout time.Duration) *Manager {
	if max < 1 {
		max = 32
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Manager{
		max:     max,
		timeout: timeout,
		holds:   make(map[string]*hold),
		stop:    make(chan struct{}),
	}
}

// SetEnabled toggles matching (global on/off for Phase A).
func (m *Manager) SetEnabled(on bool) {
	m.mu.Lock()
	m.enabled = on
	m.mu.Unlock()
}

// SetMatch updates host/path/method matchers.
func (m *Manager) SetMatch(match Match) {
	m.mu.Lock()
	m.match = match
	m.mu.Unlock()
}

// Config returns current enable/match settings.
func (m *Manager) Config() (enabled bool, match Match) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled, m.match
}

// Stats returns the live hold-pool capacity and counters.
func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Stats{
		Max:        m.max,
		TimeoutMs:  m.timeout.Milliseconds(),
		Holds:      len(m.holds),
		Total:      m.total,
		Resumed:    m.resumed,
		Expired:    m.expired,
		Aborted:    m.aborted,
		ClientGone: m.clientGone,
		Rejected:   m.rejected,
	}
}

// SetLimits tunes the hold pool at runtime (PUT /api/breakpoints), so an
// operator can shrink the connection footprint without restarting the engine.
func (m *Manager) SetLimits(max int, timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if max >= 1 {
		m.max = max
	}
	if timeout > 0 {
		m.timeout = timeout
	}
}

// List returns open sessions.
func (m *Manager) List() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, 0, len(m.holds))
	for _, h := range m.holds {
		out = append(out, h.session)
	}
	return out
}

// Resume completes a hold with optional edit. Safe concurrent out-of-order.
func (m *Manager) Resume(id string, edit ResumeEdit) error {
	m.mu.Lock()
	h, ok := m.holds[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("not found")
	}
	delete(m.holds, id)
	m.mu.Unlock()
	select {
	case h.edit <- edit:
		m.mu.Lock()
		m.resumed++
		m.mu.Unlock()
		return nil
	case <-h.cancel:
		return fmt.Errorf("expired")
	case <-time.After(200 * time.Millisecond):
		return fmt.Errorf("expired")
	}
}

// Abort discards a held session immediately and lets the request proceed with
// its ORIGINAL body, instead of pinning a proxy worker until the 60s timeout.
func (m *Manager) Abort(id string) error {
	m.mu.Lock()
	h, ok := m.holds[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("not found")
	}
	delete(m.holds, id)
	m.aborted++
	close(h.cancel)
	m.mu.Unlock()
	return nil
}

// Stop cancels all holds and disables further pauses.
func (m *Manager) Stop() {
	m.mu.Lock()
	select {
	case <-m.stop:
	default:
		close(m.stop)
		m.stop = make(chan struct{})
	}
	m.enabled = false
	holds := m.holds
	m.holds = make(map[string]*hold)
	m.mu.Unlock()
	for _, h := range holds {
		close(h.cancel)
	}
}

// OnRequest implements proxy.ProxyHooks — may block until resume/timeout.
func (m *Manager) OnRequest(req *http.Request) (*http.Request, *http.Response, proxy.RequestAction) {
	if req == nil || !m.shouldHold(req) {
		return req, nil, proxy.RequestContinue
	}
	sess, editCh, cancelCh, ok := m.tryHold(PhaseRequest, req, nil)
	if !ok {
		return req, nil, proxy.RequestContinue
	}
	edit, timedOut := m.wait(req.Context(), sess.ID, editCh, cancelCh)
	if timedOut {
		return req, nil, proxy.RequestContinue
	}
	applyReqEdit(req, edit)
	return req, nil, proxy.RequestContinue
}

// OnResponse implements proxy.ProxyHooks — may block until resume/timeout.
func (m *Manager) OnResponse(resp *http.Response) (*http.Response, proxy.ResponseAction) {
	if resp == nil || resp.Request == nil || !m.shouldHold(resp.Request) {
		return resp, proxy.ResponseContinue
	}
	sess, editCh, cancelCh, ok := m.tryHold(PhaseResponse, resp.Request, resp)
	if !ok {
		return resp, proxy.ResponseContinue
	}
	ctx := context.Background()
	if resp.Request != nil {
		ctx = resp.Request.Context()
	}
	edit, timedOut := m.wait(ctx, sess.ID, editCh, cancelCh)
	if timedOut {
		return resp, proxy.ResponseContinue
	}
	applyRespEdit(resp, edit)
	return resp, proxy.ResponseContinue
}

func (m *Manager) shouldHold(req *http.Request) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled {
		return false
	}
	match := m.match
	host := ""
	path := ""
	if req.URL != nil {
		host = req.URL.Hostname()
		path = req.URL.Path
	}
	if host == "" {
		host = stripPort(req.Host)
	}
	if match.Host != "" && !hostMatch(match.Host, host) {
		return false
	}
	if match.Path != "" && !strings.Contains(path, match.Path) {
		return false
	}
	if len(match.Methods) > 0 {
		ok := false
		for _, meth := range match.Methods {
			if strings.EqualFold(meth, req.Method) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func (m *Manager) tryHold(phase Phase, req *http.Request, resp *http.Response) (Session, chan ResumeEdit, chan struct{}, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.holds) >= m.max {
		m.rejected++
		return Session{}, nil, nil, false
	}
	body, headers, status, method, rawURL := capture(phase, req, resp)
	id := newID()
	sess := Session{
		ID:        id,
		Phase:     phase,
		Method:    method,
		URL:       rawURL,
		Status:    status,
		Headers:   headers,
		Body:      body,
		CreatedAt: time.Now().UTC(),
	}
	editCh := make(chan ResumeEdit, 1)
	cancelCh := make(chan struct{})
	m.holds[id] = &hold{session: sess, edit: editCh, cancel: cancelCh}
	m.total++
	return sess, editCh, cancelCh, true
}

// wait blocks until resume, expiry, client disconnect, or Stop. A client that
// gives up no longer pins a proxy worker for the full hold timeout.
func (m *Manager) wait(ctx context.Context, id string, editCh chan ResumeEdit, cancelCh chan struct{}) (ResumeEdit, bool) {
	timer := time.NewTimer(m.timeout)
	defer timer.Stop()
	select {
	case edit := <-editCh:
		return edit, false
	case <-cancelCh:
		m.dropReason(id, reasonExpired)
		return ResumeEdit{}, true
	case <-timer.C:
		m.dropReason(id, reasonExpired)
		return ResumeEdit{}, true
	case <-ctx.Done():
		m.dropReason(id, reasonClientGone)
		return ResumeEdit{}, true
	case <-m.stopCh():
		m.dropReason(id, reasonExpired)
		return ResumeEdit{}, true
	}
}

func (m *Manager) stopCh() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stop
}

type dropReason string

const (
	reasonExpired    dropReason = "expired"
	reasonClientGone dropReason = "clientGone"
)

func (m *Manager) dropReason(id string, why dropReason) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.holds, id)
	switch why {
	case reasonClientGone:
		m.clientGone++
	default:
		m.expired++
	}
}

func capture(phase Phase, req *http.Request, resp *http.Response) (body string, headers map[string]string, status int, method, rawURL string) {
	method = req.Method
	if req.URL != nil {
		rawURL = req.URL.String()
	}
	headers = map[string]string{}
	if phase == PhaseRequest {
		for k, vs := range req.Header {
			if len(vs) > 0 {
				headers[k] = vs[0]
			}
		}
		if req.Body != nil {
			b, _ := io.ReadAll(req.Body)
			_ = req.Body.Close()
			req.Body = io.NopCloser(bytes.NewReader(b))
			body = string(b)
		}
		return
	}
	if resp != nil {
		status = resp.StatusCode
		for k, vs := range resp.Header {
			if len(vs) > 0 {
				headers[k] = vs[0]
			}
		}
		if resp.Body != nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(b))
			body = string(b)
		}
	}
	return
}

func applyReqEdit(req *http.Request, edit ResumeEdit) {
	if edit.Method != "" {
		req.Method = edit.Method
	}
	if edit.URL != "" {
		if u, err := http.NewRequest(req.Method, edit.URL, nil); err == nil && u.URL != nil {
			req.URL = u.URL
			if u.URL.Host != "" {
				req.Host = u.URL.Host
			}
		}
	}
	if edit.Headers != nil {
		req.Header = make(http.Header)
		for k, v := range edit.Headers {
			req.Header.Set(k, v)
		}
	}
	if edit.Body != nil {
		b := []byte(*edit.Body)
		req.Body = io.NopCloser(bytes.NewReader(b))
		req.ContentLength = int64(len(b))
		// 断点面板里编辑的是明文 body: 去掉残留 Content-Encoding, 重算
		// Content-Length 并清掉针对旧 body 的校验头。
		httpx.StripContentEncoding(req.Header)
		httpx.NormalizeBodyHeaders(req.Header, len(b))
	}
}

func applyRespEdit(resp *http.Response, edit ResumeEdit) {
	if edit.Status > 0 {
		resp.StatusCode = edit.Status
		resp.Status = http.StatusText(edit.Status)
	}
	if edit.Headers != nil {
		resp.Header = make(http.Header)
		for k, v := range edit.Headers {
			resp.Header.Set(k, v)
		}
	}
	if edit.Body != nil {
		b := []byte(*edit.Body)
		resp.Body = io.NopCloser(bytes.NewReader(b))
		resp.ContentLength = int64(len(b))
		httpx.StripContentEncoding(resp.Header)
		httpx.NormalizeBodyHeaders(resp.Header, len(b))
	}
}

func hostMatch(pattern, host string) bool {
	pattern = strings.ToLower(pattern)
	host = strings.ToLower(host)
	// [match-all] 空与 "*" 都是全匹配。漏了这条的话 "*" 会落到末尾的
	// strings.Contains(host, "*") —— host 里不可能含星号, 恒 false,
	// 于是"全匹配"的断点从来挂不住任何请求(用户视角: 开了断点没反应)。
	if pattern == "" || pattern == "*" {
		return true
	}
	if pattern == host {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suf := pattern[1:] // .example.com
		return strings.HasSuffix(host, suf) || host == pattern[2:]
	}
	return strings.Contains(host, pattern)
}

func stripPort(host string) string {
	if i := strings.LastIndex(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
