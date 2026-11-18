package proxy

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ReverseSpec is one reverse proxy listener.
type ReverseSpec struct {
	Listen string
	Target string
}

// ReverseManager runs reverse_proxy listeners (default loopback targets).
type ReverseManager struct {
	log *slog.Logger
	mu  sync.Mutex
	srvs []*http.Server
}

func NewReverseManager(log *slog.Logger) *ReverseManager {
	if log == nil {
		log = slog.Default()
	}
	return &ReverseManager{log: log}
}

func (m *ReverseManager) Stop(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.srvs {
		_ = s.Shutdown(ctx)
	}
	m.srvs = nil
}

func (m *ReverseManager) Apply(specs []ReverseSpec) error {
	m.Stop(context.Background())
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sp := range specs {
		if sp.Listen == "" || sp.Target == "" {
			continue
		}
		if err := assertLoopbackListen(sp.Listen); err != nil {
			return err
		}
		target := sp.Target
		if !strings.Contains(target, "://") {
			target = "http://" + target
		}
		u, err := url.Parse(target)
		if err != nil {
			return err
		}
		rp := httputil.NewSingleHostReverseProxy(u)
		// [fd-limit] 同 api.NewServer: 反向代理监听同样回收空闲 keep-alive
		srv := &http.Server{
			Addr:              sp.Listen,
			Handler:           rp,
			ReadHeaderTimeout: 15 * time.Second,
			IdleTimeout:       90 * time.Second,
		}
		m.srvs = append(m.srvs, srv)
		go func(s *http.Server, listen string) {
			m.log.Info("reverse_proxy listen", "addr", listen, "target", u.String())
			if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				m.log.Error("reverse_proxy", "addr", listen, "err", err)
			}
		}(srv, sp.Listen)
	}
	return nil
}

func assertLoopbackListen(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		if host == "localhost" {
			return nil
		}
		return errNonLoopback
	}
	if !ip.IsLoopback() {
		return errNonLoopback
	}
	return nil
}

var errNonLoopback = &net.AddrError{Err: "reverse_proxy listen must be loopback (use explicit flag later for LAN)", Addr: ""}
