// Package breakpoint pauses matched in-flight requests/responses for edit+resume.
//
// Hold model: OnRequest/OnResponse block on a channel until Resume or timeout.
// goproxy runs hooks on the request goroutine, so this holds that connection
// for up to Timeout (default 60s). Max concurrent holds is Max (default 32).
// Residual risk: long holds pin proxy worker capacity; CONNECT/H2 frames not held.
package breakpoint

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GitterBois/proxying/internal/proxy"
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

// Manager implements proxy.ProxyHooks and the breakpoint control plane.
type Manager struct {
	mu      sync.Mutex
	max     int
	timeout time.Duration
	enabled bool
	match   Match
	holds   map[string]*hold
	stop    chan struct{}
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
		return nil
	case <-h.cancel:
		return fmt.Errorf("expired")
	case <-time.After(200 * time.Millisecond):
		return fmt.Errorf("expired")
	}
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
	edit, timedOut := m.wait(sess.ID, editCh, cancelCh)
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
	edit, timedOut := m.wait(sess.ID, editCh, cancelCh)
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
	return sess, editCh, cancelCh, true
}

func (m *Manager) wait(id string, editCh chan ResumeEdit, cancelCh chan struct{}) (ResumeEdit, bool) {
	timer := time.NewTimer(m.timeout)
	defer timer.Stop()
	select {
	case edit := <-editCh:
		return edit, false
	case <-cancelCh:
		m.drop(id)
		return ResumeEdit{}, true
	case <-timer.C:
		m.drop(id)
		return ResumeEdit{}, true
	case <-m.stopCh():
		m.drop(id)
		return ResumeEdit{}, true
	}
}

func (m *Manager) stopCh() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stop
}

func (m *Manager) drop(id string) {
	m.mu.Lock()
	delete(m.holds, id)
	m.mu.Unlock()
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
