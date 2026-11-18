package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/GitterBois/runhooks/internal/store"
)

// snapshotBody reads the full stream for forwarding and a capped copy for capture.
func snapshotBody(r io.ReadCloser, max int64) (forward io.ReadCloser, captured []byte, truncated bool, size int64, err error) {
	if r == nil {
		return http.NoBody, nil, false, 0, nil
	}
	defer r.Close()
	if max < 0 {
		max = 0
	}
	all, err := io.ReadAll(r)
	if err != nil {
		return http.NoBody, nil, false, 0, err
	}
	size = int64(len(all))
	if size > max {
		captured = append([]byte(nil), all[:max]...)
		truncated = true
	} else {
		captured = append([]byte(nil), all...)
	}
	return io.NopCloser(bytes.NewReader(all)), captured, truncated, size, nil
}

func cloneHeader(h http.Header) map[string][]string {
	if h == nil {
		return nil
	}
	out := make(map[string][]string, len(h))
	for k, vv := range h {
		out[k] = append([]string(nil), vv...)
	}
	return out
}

func requestURL(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	if req.URL.IsAbs() {
		return req.URL.String()
	}
	scheme := schemeOf(req)
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	path := req.URL.RequestURI()
	if path == "" {
		path = "/"
	}
	return scheme + "://" + host + path
}

func messageFromRequest(req *http.Request, body []byte, truncated bool, size int64) store.Message {
	return store.Message{
		Method:    req.Method,
		URL:       requestURL(req),
		Proto:     req.Proto,
		Headers:   cloneHeader(req.Header),
		Body:      body,
		Truncated: truncated,
		Size:      size,
	}
}

func messageFromResponse(resp *http.Response, body []byte, truncated bool, size int64) store.Message {
	return store.Message{
		Proto:     resp.Proto,
		Status:    resp.StatusCode,
		Headers:   cloneHeader(resp.Header),
		Body:      body,
		Truncated: truncated,
		Size:      size,
	}
}

func textResponse(req *http.Request, status int, text string) *http.Response {
	b := []byte(text)
	h := make(http.Header)
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	return &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        h,
		Body:          io.NopCloser(bytes.NewReader(b)),
		ContentLength: int64(len(b)),
		Request:       req,
	}
}

func schemeOf(req *http.Request) string {
	if req == nil {
		return "http"
	}
	if req.TLS != nil {
		return "https"
	}
	if req.URL != nil && req.URL.Scheme != "" {
		return strings.ToLower(req.URL.Scheme)
	}
	if p := req.Header.Get("X-Forwarded-Proto"); p != "" {
		return strings.ToLower(p)
	}
	return "http"
}
