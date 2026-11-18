package rules

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/GitterBois/runhooks/internal/proxy"
)

// OnRequest implements proxy.ProxyHooks.
func (c *Chain) OnRequest(req *http.Request) (*http.Request, *http.Response, proxy.RequestAction) {
	if req == nil {
		return req, nil, proxy.RequestContinue
	}
	host, path := "", ""
	if req.URL != nil {
		host = req.URL.Hostname()
		path = req.URL.Path
	}
	if host == "" {
		host = stripPort(req.Host)
	}
	method := req.Method

	c.mu.RLock()
	rules := append([]Rule(nil), c.rules...)
	c.mu.RUnlock()

	// Allow-list: if any enabled host_allow exists, host must match at least one.
	var allowRules []Rule
	for i := range rules {
		if rules[i].Enabled && rules[i].Type == TypeHostAllow {
			allowRules = append(allowRules, rules[i])
		}
	}
	if len(allowRules) > 0 {
		ok := false
		for i := range allowRules {
			if c.matchRule(&allowRules[i], host, path, method) {
				ok = true
				break
			}
		}
		if !ok {
			return req, textResp(req, http.StatusForbidden, "blocked by host_allow"), proxy.RequestRespond
		}
	}

	for i := range rules {
		r := &rules[i]
		if !c.matchRule(r, host, path, method) {
			continue
		}
		switch r.Type {
		case TypeHostBlock:
			return req, textResp(req, http.StatusForbidden, "blocked by host_block"), proxy.RequestRespond
		case TypeMapLocal:
			if r.MapLocal == nil {
				continue
			}
			// [sub-adapt] 内联 body(订阅 echo-response 适配): 不读文件
			if r.MapLocal.File == "" && r.MapLocal.Body != "" {
				st := r.MapLocal.Status
				if st == 0 {
					st = http.StatusOK
				}
				return req, bytesResp(req, st, []byte(r.MapLocal.Body)), proxy.RequestRespond
			}
			abs, err := ResolveMapLocal(c.MapLocalRoot(), r.MapLocal.File)
			if err != nil {
				return req, textResp(req, http.StatusBadGateway, "map_local: "+err.Error()), proxy.RequestRespond
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return req, textResp(req, http.StatusBadGateway, "map_local: read: "+err.Error()), proxy.RequestRespond
			}
			st := r.MapLocal.Status
			if st == 0 {
				st = http.StatusOK
			}
			return req, bytesResp(req, st, b), proxy.RequestRespond
		case TypeMirrorRemote:
			if r.MirrorRemote == nil || r.MirrorRemote.URL == "" {
				continue
			}
			resp, err := mirrorFetch(r.MirrorRemote.URL)
			if err != nil {
				return req, textResp(req, http.StatusBadGateway, "mirror: "+err.Error()), proxy.RequestRespond
			}
			resp.Request = req
			return req, resp, proxy.RequestRespond
		case TypeGateway:
			if r.Gateway != nil && r.Gateway.Upstream != "" {
				req = applyGateway(req, r.Gateway.Upstream)
			}
		case TypeHeader:
			if r.Header != nil && strings.EqualFold(r.Header.Phase, "request") {
				applyHeader(req.Header, r.Header)
			}
		case TypeBodyReplace:
			if r.BodyReplace != nil && strings.EqualFold(r.BodyReplace.Phase, "request") {
				req = replaceBody(req, r.BodyReplace.From, r.BodyReplace.To)
			}
		case TypeHighlight:
			if r.Highlight != nil {
				if r.Highlight.Label != "" {
					req.Header.Set("X-Proxying-Highlight", r.Highlight.Label)
				}
				if r.Highlight.Color != "" {
					req.Header.Set("X-Proxying-Highlight-Color", r.Highlight.Color)
				}
			}
		}
	}
	return req, nil, proxy.RequestContinue
}

func applyGateway(req *http.Request, upstream string) *http.Request {
	u := *req.URL
	if !strings.Contains(upstream, "://") {
		upstream = "http://" + upstream
	}
	base, err := url.Parse(upstream)
	if err != nil {
		return req
	}
	u.Scheme = base.Scheme
	u.Host = base.Host
	if base.Path != "" && base.Path != "/" {
		u.Path = strings.TrimRight(base.Path, "/") + u.Path
	}
	req.URL = &u
	req.Host = base.Host
	return req
}

func mirrorFetch(raw string) (*http.Response, error) {
	resp, err := http.Get(raw)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	h := resp.Header.Clone()
	h.Set("Content-Length", fmt.Sprintf("%d", len(b)))
	return &http.Response{
		StatusCode:    resp.StatusCode,
		Status:        resp.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        h,
		Body:          io.NopCloser(bytes.NewReader(b)),
		ContentLength: int64(len(b)),
	}, nil
}

// OnResponse implements proxy.ProxyHooks.
func (c *Chain) OnResponse(resp *http.Response) (*http.Response, proxy.ResponseAction) {
	if resp == nil || resp.Request == nil {
		return resp, proxy.ResponseContinue
	}
	req := resp.Request
	host, path := "", ""
	if req.URL != nil {
		host = req.URL.Hostname()
		path = req.URL.Path
	}
	if host == "" {
		host = stripPort(req.Host)
	}
	method := req.Method

	c.mu.RLock()
	rules := append([]Rule(nil), c.rules...)
	c.mu.RUnlock()

	for i := range rules {
		r := &rules[i]
		if !c.matchRule(r, host, path, method) {
			continue
		}
		switch r.Type {
		case TypeHeader:
			if r.Header != nil && strings.EqualFold(r.Header.Phase, "response") {
				applyHeader(resp.Header, r.Header)
			}
		case TypeBodyReplace:
			if r.BodyReplace != nil && strings.EqualFold(r.BodyReplace.Phase, "response") {
				resp = replaceRespBody(resp, r.BodyReplace.From, r.BodyReplace.To)
			}
		}
	}
	return resp, proxy.ResponseContinue
}

func applyHeader(h http.Header, m *HeaderMut) {
	for _, k := range m.Remove {
		h.Del(k)
	}
	for k, v := range m.Set {
		h.Set(k, v)
	}
}

func replaceBody(req *http.Request, from, to string) *http.Request {
	if req.Body == nil {
		return req
	}
	b, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		req.Body = io.NopCloser(bytes.NewReader(nil))
		return req
	}
	// [sub-adapt] from 为空 = 整包替换(QuanX request-body 语义)
	var nb []byte
	if from == "" {
		nb = []byte(to)
	} else {
		nb = bytes.ReplaceAll(b, []byte(from), []byte(to))
	}
	req.Body = io.NopCloser(bytes.NewReader(nb))
	req.ContentLength = int64(len(nb))
	req.Header.Set("Content-Length", itoa(len(nb)))
	return req
}

func replaceRespBody(resp *http.Response, from, to string) *http.Response {
	if resp.Body == nil {
		return resp
	}
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return resp
	}
	// [sub-adapt] from 为空 = 整包替换(QuanX response-body 语义)
	var nb []byte
	if from == "" {
		nb = []byte(to)
	} else {
		nb = bytes.ReplaceAll(b, []byte(from), []byte(to))
	}
	resp.Body = io.NopCloser(bytes.NewReader(nb))
	resp.ContentLength = int64(len(nb))
	resp.Header.Set("Content-Length", itoa(len(nb)))
	return resp
}

func textResp(req *http.Request, code int, msg string) *http.Response {
	return bytesResp(req, code, []byte(msg))
}

func bytesResp(req *http.Request, code int, body []byte) *http.Response {
	h := make(http.Header)
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Length", itoa(len(body)))
	return &http.Response{
		StatusCode:    code,
		Status:        http.StatusText(code),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        h,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
