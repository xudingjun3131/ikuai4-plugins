// Package rest is the local REST client (send, collections, env, from-flow).
package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GitterBois/runhooks/internal/store"
)

// SendRequest is POST /api/rest/send body.
type SendRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Timeout time.Duration     `json:"timeoutMs,omitempty"` // milliseconds if set via JSON number helper
}

// SendResponse is the result of a send.
type SendResponse struct {
	Status    int                 `json:"status"`
	Headers   map[string][]string `json:"headers"`
	Body      string              `json:"body"`
	TimingMs  int64               `json:"timingMs"`
	Error     string              `json:"error,omitempty"`
	Truncated bool                `json:"truncated,omitempty"`
}

// Collection is a named set of requests on disk.
type Collection struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Requests []CollectionReq   `json:"requests"`
	Env      map[string]string `json:"env,omitempty"`
}

// CollectionReq is one saved request.
type CollectionReq struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// Service holds collections under dataDir/collections and global env.
type Service struct {
	mu      sync.RWMutex
	dataDir string
	env     map[string]string
	client  *http.Client
}

// NewService prepares dataDir/collections and empty env.
func NewService(dataDir string) (*Service, error) {
	dir := filepath.Join(dataDir, "collections")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Service{
		dataDir: dataDir,
		env:     map[string]string{},
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
	// load env.json if present
	if b, err := os.ReadFile(filepath.Join(dataDir, "env.json")); err == nil {
		_ = json.Unmarshal(b, &s.env)
		if s.env == nil {
			s.env = map[string]string{}
		}
	}
	return s, nil
}

// Send performs an outbound HTTP(S) request. file:// and other schemes → error.
func (s *Service) Send(ctx context.Context, in SendRequest) (SendResponse, error) {
	if in.URL == "" {
		return SendResponse{}, fmt.Errorf("url required")
	}
	method := in.Method
	if method == "" {
		method = http.MethodGet
	}
	urlStr := s.subst(in.URL)
	if err := allowURL(urlStr); err != nil {
		return SendResponse{}, err
	}
	body := s.subst(in.Body)
	req, err := http.NewRequestWithContext(ctx, method, urlStr, strings.NewReader(body))
	if err != nil {
		return SendResponse{}, err
	}
	for k, v := range in.Headers {
		req.Header.Set(k, s.subst(v))
	}
	// Prefer request context deadline; fall back to client timeout.
	client := s.client
	if dl, ok := ctx.Deadline(); ok {
		c := *s.client
		c.Timeout = time.Until(dl)
		if c.Timeout < time.Second {
			c.Timeout = time.Second
		}
		client = &c
	}
	start := time.Now()
	resp, err := client.Do(req)
	timing := time.Since(start).Milliseconds()
	if err != nil {
		return SendResponse{TimingMs: timing, Error: err.Error()}, nil
	}
	defer resp.Body.Close()
	const maxBody = 1 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	trunc := false
	if len(b) > maxBody {
		b = b[:maxBody]
		trunc = true
	}
	if err != nil {
		return SendResponse{Status: resp.StatusCode, TimingMs: timing, Error: err.Error()}, nil
	}
	return SendResponse{
		Status:    resp.StatusCode,
		Headers:   resp.Header.Clone(),
		Body:      string(b),
		TimingMs:  timing,
		Truncated: trunc,
	}, nil
}

// FromFlow builds a SendRequest from a captured flow.
func FromFlow(f store.Flow) SendRequest {
	h := map[string]string{}
	for k, vs := range f.Request.Headers {
		if len(vs) > 0 {
			h[k] = vs[0]
		}
	}
	// hop-by-hop / proxy noise
	delete(h, "Proxy-Connection")
	delete(h, "Connection")
	return SendRequest{
		Method:  f.Request.Method,
		URL:     f.Request.URL,
		Headers: h,
		Body:    string(f.Request.Body),
	}
}

// Env returns a copy of environment variables for {{var}} substitution.
func (s *Service) Env() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.env))
	for k, v := range s.env {
		out[k] = v
	}
	return out
}

// SetEnv replaces env and persists to env.json.
func (s *Service) SetEnv(env map[string]string) error {
	if env == nil {
		env = map[string]string{}
	}
	s.mu.Lock()
	s.env = env
	s.mu.Unlock()
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dataDir, "env.json"), b, 0o644)
}

// ListCollections reads all *.json under collections/.
func (s *Service) ListCollections() ([]Collection, error) {
	dir := filepath.Join(s.dataDir, "collections")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Collection
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var c Collection
		if json.Unmarshal(b, &c) != nil {
			continue
		}
		if c.ID == "" {
			c.ID = strings.TrimSuffix(e.Name(), ".json")
		}
		out = append(out, c)
	}
	return out, nil
}

// SaveCollection writes one collection file.
func (s *Service) SaveCollection(c Collection) error {
	if c.ID == "" {
		return fmt.Errorf("id required")
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dataDir, "collections", c.ID+".json")
	return os.WriteFile(path, b, 0o644)
}

// GetCollection loads by id.
func (s *Service) GetCollection(id string) (Collection, error) {
	path := filepath.Join(s.dataDir, "collections", id+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return Collection{}, err
	}
	var c Collection
	if err := json.Unmarshal(b, &c); err != nil {
		return Collection{}, err
	}
	if c.ID == "" {
		c.ID = id
	}
	return c, nil
}

// DeleteCollection removes a collection file.
func (s *Service) DeleteCollection(id string) error {
	return os.Remove(filepath.Join(s.dataDir, "collections", id+".json"))
}

func (s *Service) subst(in string) string {
	s.mu.RLock()
	env := s.env
	s.mu.RUnlock()
	out := in
	for k, v := range env {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

func allowURL(raw string) error {
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "file:") {
		return fmt.Errorf("scheme not allowed: file")
	}
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return fmt.Errorf("only http and https allowed")
	}
	return nil
}

// EncodeSend is a helper for tests building JSON bodies.
func EncodeSend(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}
