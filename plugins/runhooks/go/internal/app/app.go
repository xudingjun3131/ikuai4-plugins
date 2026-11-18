package app

import (
	"path/filepath"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/GitterBois/runhooks/internal/api"
	"github.com/GitterBois/runhooks/internal/bookmark"
	"github.com/GitterBois/runhooks/internal/breakpoint"
	"github.com/GitterBois/runhooks/internal/ca"
	"github.com/GitterBois/runhooks/internal/config"
	"github.com/GitterBois/runhooks/internal/mcp"
	"github.com/GitterBois/runhooks/internal/netcond"
	"github.com/GitterBois/runhooks/internal/proxy"
	"github.com/GitterBois/runhooks/internal/report"
	"github.com/GitterBois/runhooks/internal/rest"
	"github.com/GitterBois/runhooks/internal/rules"
	"github.com/GitterBois/runhooks/internal/script"
	"github.com/GitterBois/runhooks/internal/session"
	"github.com/GitterBois/runhooks/internal/store"
	"github.com/GitterBois/runhooks/internal/subscription"
)

type App struct {
	cfg       config.Config
	log       *slog.Logger
	auth      *ca.Authority
	sink      *store.Store
	history   *store.History
	eng       *proxy.Engine
	rules     *rules.Chain
	script    *script.Runner
	gallery   *script.Gallery
	bp        *breakpoint.Manager
	rest      *rest.Service
	sessions  *session.Store
	bookmarks *bookmark.Store
	report    *report.Server
	netcond   *netcond.Cond
	mcp       *mcp.Server
	rev       *proxy.ReverseManager
	apiSrv    *http.Server
	pxSrv     *http.Server
	scope     *proxy.ScopeStore
	subs      *subscription.Manager
	wg        sync.WaitGroup
	errCh     chan error
}

func New(cfg config.Config, log *slog.Logger, version string) (*App, error) {
	if log == nil {
		log = slog.Default()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	auth, err := ca.Load(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: ca: %w", err)
	}
	sink := store.New(cfg.MaxFlows)
	hist, err := store.NewHistory(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: history: %w", err)
	}
	sink.SetHistory(hist)

	chain, err := rules.NewChain(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: rules: %w", err)
	}
	scr, err := script.NewRunner(cfg.DataDir, cfg.ScriptTimeout)
	if err != nil {
		return nil, fmt.Errorf("app: script: %w", err)
	}
	gallery := script.NewGallery(scr.Dir())
	bp := breakpoint.NewManager(cfg.BPMax, cfg.BPTimeout)
	restSvc, err := rest.NewService(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: rest: %w", err)
	}
	sess, err := session.NewStore(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: session: %w", err)
	}
	bmarks, err := bookmark.NewStore(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: bookmark: %w", err)
	}
	rep := report.New(sink, cfg.ReportToken)
	nc := netcond.New()
	mcpSrv := mcp.New(mcp.Deps{
		Store: sink,
		REST:  restSvc,
		Rules: chain,
	})

	scope := proxy.NewScopeStore(cfg.ScopeFile, filepath.Join(cfg.DataDir, "geoip.csv"))
	eng, err := proxy.NewEngine(proxy.EngineConfig{
		Scope:     scope,
		Authority: auth,
		Sink:      sink,
		MaxBody:   cfg.MaxBody,
		Hooks: proxy.HookSlots{
			Rules:      chain,
			Script:     scr,
			Breakpoint: bp,
		},
		Policy: chain,
	})
	if err != nil {
		return nil, fmt.Errorf("app: proxy: %w", err)
	}
	rev := proxy.NewReverseManager(log)
	reloadReverse := func() {
		var specs []proxy.ReverseSpec
		for _, r := range chain.ReverseProxies() {
			if r.ReverseProxy != nil {
				specs = append(specs, proxy.ReverseSpec{
					Listen: r.ReverseProxy.Listen,
					Target: r.ReverseProxy.Target,
				})
			}
		}
		if err := rev.Apply(specs); err != nil {
			log.Warn("reverse_proxy", "err", err)
		}
	}
	reloadReverse()

	// [subscription] 规则订阅: QuanX/Surge/纯列表 → scope.sub.json + rules.sub.json + scripts/sub-*.js
	subs, err := subscription.NewManager(cfg.DataDir, scope, chain, gallery, log)
	if err != nil {
		return nil, fmt.Errorf("app: subscription: %w", err)
	}
	subs.OnRulesChanged = func() {
		eng.SetRulesSlot(chain)
		eng.SetPolicy(chain)
		reloadReverse()
	}
	subs.OnScopeChanged = func() {
		scope.Reload()
	}

	a := &App{
		cfg:       cfg,
		log:       log,
		auth:      auth,
		sink:      sink,
		history:   hist,
		eng:       eng,
		rules:     chain,
		script:    scr,
		gallery:   gallery,
		bp:        bp,
		rest:      restSvc,
		sessions:  sess,
		bookmarks: bmarks,
		report:    rep,
		netcond:   nc,
		mcp:       mcpSrv,
		rev:       rev,
		scope:     scope,
		subs:      subs,
		pxSrv:     eng.NewServer(cfg.ProxyAddr),
		errCh:     make(chan error, 2),
	}
	a.apiSrv = api.NewServer(cfg.APIAddr, api.Deps{
		Version:   version,
		DataDir:   cfg.DataDir,
		ProxyAddr: cfg.ProxyAddr,
		Store:     sink,
		RootPEM:   auth.RootPEM,
		Rules:     chain,
		OnRulesChange: func() {
			eng.SetRulesSlot(chain)
			eng.SetPolicy(chain)
			reloadReverse()
		},
		Breakpoints: bp,
		REST:        restSvc,
		Scripts:     gallery,
		OnScriptChange: func() {
			_ = scr.Reload()
			eng.SetScriptSlot(scr)
		},
		Sessions:  sess,
		Bookmarks: bmarks,
		Report:    rep,
		NetCond:   nc,
		MCP:       mcpSrv,
		Subs:      subs,
	})
	return a, nil
}

func (a *App) Store() *store.Store       { return a.sink }
func (a *App) Authority() *ca.Authority { return a.auth }

func (a *App) MCPServer() *mcp.Server { return a.mcp }

// ReloadScope SIGHUP 热重载域名拦截范围（抓包不中断）。
func (a *App) ReloadScope() {
	if a.scope != nil {
		a.scope.Reload()
		a.log.Info("scope reloaded")
	}
}

func (a *App) Start() {
	if a.subs != nil {
		a.subs.Start() // 订阅自动更新调度(每分钟 tick, 到期才拉)
	}
	start := func(name string, srv *http.Server) {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.log.Info("listen", "server", name, "addr", srv.Addr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				a.errCh <- fmt.Errorf("%s: %w", name, err)
			}
		}()
	}
	start("api", a.apiSrv)
	// 显式代理监听常开: 设备手动设代理 router:18080 也能被抓(不动 iptables)
	start("proxy", a.pxSrv)
	if a.cfg.Transparent {
		// 透明监听: iptables REDIRECT 的目标(按设备 MAC 精确引流, 见插件 service.sh)
		tAddr := a.cfg.TransparentAddr
		if tAddr == "" {
			tAddr = a.cfg.ProxyAddr
		}
		host, _, err := net.SplitHostPort(tAddr)
		if err != nil {
			a.errCh <- fmt.Errorf("transparent: %w", err)
			return
		}
		if host == "127.0.0.1" || host == "localhost" {
			// 127.0.0.1:PORT → 0.0.0.0:PORT(透明监听必须对外)
			_, p, _ := net.SplitHostPort(tAddr)
			tAddr = net.JoinHostPort("0.0.0.0", p)
		}
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.log.Info("listen", "server", "proxy-transparent", "addr", tAddr)
			if err := a.eng.StartTransparent(tAddr); err != nil {
				a.errCh <- fmt.Errorf("proxy-transparent: %w", err)
			}
		}()
	}
}

func (a *App) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-a.errCh:
		return err
	}
}

func (a *App) Shutdown(timeout time.Duration) {
	if a.subs != nil {
		a.subs.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := a.apiSrv.Shutdown(ctx); err != nil {
		a.log.Error("api shutdown", "err", err)
	}
	if err := a.pxSrv.Shutdown(ctx); err != nil {
		a.log.Error("proxy shutdown", "err", err)
	}
	if a.rev != nil {
		a.rev.Stop(ctx)
	}
	a.wg.Wait()
	a.log.Info("stopped")
}
