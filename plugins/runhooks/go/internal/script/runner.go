// Package script runs sandboxed JS (goja) rewrite scripts as a ProxyHooks slot.
package script

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/GitterBois/runhooks/internal/proxy"
)

// Runner loads $DATA/scripts/*.js and implements proxy.ProxyHooks.
// Sandbox: only flow.request/response fields; no OS/network; timeout kills loops.
type Runner struct {
	mu      sync.RWMutex
	dir     string
	timeout time.Duration
	scripts []namedScript
}

type namedScript struct {
	name string
	src  string
}

// NewRunner loads scripts from dataDir/scripts (creates dir if missing).
func NewRunner(dataDir string, timeout time.Duration) (*Runner, error) {
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	r := &Runner{
		dir:     filepath.Join(dataDir, "scripts"),
		timeout: timeout,
	}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return nil, fmt.Errorf("script: mkdir: %w", err)
	}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload re-reads all *.js files from the scripts directory.
func (r *Runner) Reload() error {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return fmt.Errorf("script: readdir: %w", err)
	}
	var list []namedScript
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".js") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(r.dir, e.Name()))
		if err != nil {
			return fmt.Errorf("script: read %s: %w", e.Name(), err)
		}
		list = append(list, namedScript{name: e.Name(), src: string(b)})
	}
	r.mu.Lock()
	r.scripts = list
	r.mu.Unlock()
	return nil
}

// OnRequest implements proxy.ProxyHooks.
func (r *Runner) OnRequest(req *http.Request) (*http.Request, *http.Response, proxy.RequestAction) {
	if req == nil {
		return req, nil, proxy.RequestContinue
	}
	r.mu.RLock()
	scripts := append([]namedScript(nil), r.scripts...)
	timeout := r.timeout
	r.mu.RUnlock()
	if len(scripts) == 0 {
		return req, nil, proxy.RequestContinue
	}
	fr, err := snapshotRequest(req)
	if err != nil {
		return req, nil, proxy.RequestContinue
	}
	for _, s := range scripts {
		if err := r.runPhase(s, "onRequest", fr, nil, timeout); err != nil {
			// Timeout / script error: leave request unchanged for this script.
			continue
		}
	}
	applyRequest(req, fr)
	return req, nil, proxy.RequestContinue
}

// OnResponse implements proxy.ProxyHooks.
func (r *Runner) OnResponse(resp *http.Response) (*http.Response, proxy.ResponseAction) {
	if resp == nil {
		return resp, proxy.ResponseContinue
	}
	r.mu.RLock()
	scripts := append([]namedScript(nil), r.scripts...)
	timeout := r.timeout
	r.mu.RUnlock()
	if len(scripts) == 0 {
		return resp, proxy.ResponseContinue
	}
	var fr *flowReq
	if resp.Request != nil {
		fr, _ = snapshotRequest(resp.Request)
	}
	fs, err := snapshotResponse(resp)
	if err != nil {
		return resp, proxy.ResponseContinue
	}
	for _, s := range scripts {
		if err := r.runPhase(s, "onResponse", fr, fs, timeout); err != nil {
			continue
		}
	}
	applyResponse(resp, fs)
	return resp, proxy.ResponseContinue
}

func (r *Runner) runPhase(s namedScript, phase string, req *flowReq, resp *flowResp, timeout time.Duration) error {
	vm := goja.New()
	// Minimal sandbox: no require, no process — only flow object we inject.
	flowObj := vm.NewObject()
	if req != nil {
		_ = flowObj.Set("request", reqToJS(vm, req))
	}
	if resp != nil {
		_ = flowObj.Set("response", respToJS(vm, resp))
	}
	_ = vm.Set("flow", flowObj)

	done := make(chan error, 1)
	go func() {
		_, err := vm.RunString(s.src)
		if err != nil {
			done <- fmt.Errorf("%s load: %w", s.name, err)
			return
		}
		fn, ok := goja.AssertFunction(vm.Get(phase))
		if !ok {
			done <- nil // optional hook
			return
		}
		_, err = fn(goja.Undefined(), flowObj)
		done <- err
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return err
		}
		// Pull mutations back from JS objects.
		if req != nil {
			if v := flowObj.Get("request"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
				jsToReq(vm, v.ToObject(vm), req)
			}
		}
		if resp != nil {
			if v := flowObj.Get("response"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
				jsToResp(vm, v.ToObject(vm), resp)
			}
		}
		return nil
	case <-timer.C:
		vm.Interrupt("script timeout")
		<-done // wait for goroutine exit after interrupt
		return fmt.Errorf("%s: timeout", s.name)
	}
}

type flowReq struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}

type flowResp struct {
	Status  int
	Headers map[string]string
	Body    string
}

func snapshotRequest(req *http.Request) (*flowReq, error) {
	body, err := readAllBody(&req.Body)
	if err != nil {
		return nil, err
	}
	u := ""
	if req.URL != nil {
		u = req.URL.String()
	}
	return &flowReq{
		Method:  req.Method,
		URL:     u,
		Headers: flatHeaders(req.Header),
		Body:    string(body),
	}, nil
}

func snapshotResponse(resp *http.Response) (*flowResp, error) {
	body, err := readAllBody(&resp.Body)
	if err != nil {
		return nil, err
	}
	return &flowResp{
		Status:  resp.StatusCode,
		Headers: flatHeaders(resp.Header),
		Body:    string(body),
	}, nil
}

func readAllBody(body *io.ReadCloser) ([]byte, error) {
	if body == nil || *body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(*body)
	_ = (*body).Close()
	*body = io.NopCloser(bytes.NewReader(b))
	return b, err
}

func flatHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

func applyRequest(req *http.Request, fr *flowReq) {
	if fr == nil {
		return
	}
	if fr.Method != "" {
		req.Method = fr.Method
	}
	if fr.URL != "" && req.URL != nil {
		if u, err := http.NewRequest(fr.Method, fr.URL, nil); err == nil && u.URL != nil {
			// Preserve scheme/host for relative rewrites; full URL preferred.
			req.URL = u.URL
			if u.URL.Host != "" {
				req.Host = u.URL.Host
			}
		}
	}
	req.Header = make(http.Header)
	for k, v := range fr.Headers {
		req.Header.Set(k, v)
	}
	b := []byte(fr.Body)
	req.Body = io.NopCloser(bytes.NewReader(b))
	req.ContentLength = int64(len(b))
	if len(b) > 0 {
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(b)))
	} else {
		req.Header.Del("Content-Length")
	}
}

func applyResponse(resp *http.Response, fs *flowResp) {
	if fs == nil {
		return
	}
	if fs.Status > 0 {
		resp.StatusCode = fs.Status
		resp.Status = http.StatusText(fs.Status)
	}
	resp.Header = make(http.Header)
	for k, v := range fs.Headers {
		resp.Header.Set(k, v)
	}
	b := []byte(fs.Body)
	resp.Body = io.NopCloser(bytes.NewReader(b))
	resp.ContentLength = int64(len(b))
	if len(b) > 0 {
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(b)))
	} else {
		resp.Header.Del("Content-Length")
	}
}

func reqToJS(vm *goja.Runtime, fr *flowReq) *goja.Object {
	o := vm.NewObject()
	_ = o.Set("method", fr.Method)
	_ = o.Set("url", fr.URL)
	_ = o.Set("body", fr.Body)
	h := vm.NewObject()
	for k, v := range fr.Headers {
		_ = h.Set(k, v)
	}
	_ = o.Set("headers", h)
	return o
}

func respToJS(vm *goja.Runtime, fs *flowResp) *goja.Object {
	o := vm.NewObject()
	_ = o.Set("status", fs.Status)
	_ = o.Set("body", fs.Body)
	h := vm.NewObject()
	for k, v := range fs.Headers {
		_ = h.Set(k, v)
	}
	_ = o.Set("headers", h)
	return o
}

func jsToReq(vm *goja.Runtime, o *goja.Object, fr *flowReq) {
	if o == nil {
		return
	}
	if v := o.Get("method"); v != nil && !goja.IsUndefined(v) {
		fr.Method = v.String()
	}
	if v := o.Get("url"); v != nil && !goja.IsUndefined(v) {
		fr.URL = v.String()
	}
	if v := o.Get("body"); v != nil && !goja.IsUndefined(v) {
		fr.Body = v.String()
	}
	if v := o.Get("headers"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		fr.Headers = jsHeaders(v.ToObject(vm))
	}
}

func jsToResp(vm *goja.Runtime, o *goja.Object, fs *flowResp) {
	if o == nil {
		return
	}
	if v := o.Get("status"); v != nil && !goja.IsUndefined(v) {
		fs.Status = int(v.ToInteger())
	}
	if v := o.Get("body"); v != nil && !goja.IsUndefined(v) {
		fs.Body = v.String()
	}
	if v := o.Get("headers"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		fs.Headers = jsHeaders(v.ToObject(vm))
	}
}

func jsHeaders(o *goja.Object) map[string]string {
	out := make(map[string]string)
	if o == nil {
		return out
	}
	for _, k := range o.Keys() {
		v := o.Get(k)
		if v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
			out[k] = v.String()
		}
	}
	return out
}
