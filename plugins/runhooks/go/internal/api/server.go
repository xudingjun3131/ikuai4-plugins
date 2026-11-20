package api

import (
	"net/http"
	"time"
)

// [plugin-proxy] iKuai 加固: WebUI 经路由器同源 /plugin_proxy/runhooks/ 代理
// (OpenResty 校验 sess_key 后 StripPrefix 转发, 引擎 webui 只绑 127.0.0.1)。
// 引擎同时挂两套路径: 裸 /api/* 与 /assets/* (代理剥前缀后落到这里, 也是
// service.sh 本地调用的形态), 以及 /plugin_proxy/runhooks/* 前缀路径(旧固件
// 直连 IP:18081 时的形态)。根路径仅直连(无 X-Plugin-Proxy 头)时 302 跳前缀,
// 代理来的根路径请求直接服 SPA——否则 Location 回环。
const WebPrefix = "/plugin_proxy/runhooks"

// NewMux builds the control-plane HTTP mux (health, flows, ca.pem, SPA).
func NewMux(d Deps) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", HealthHandler(d.Version))
	mux.HandleFunc("/api/flows", flowsCollection(d.Store))
	mux.HandleFunc("/api/flows/", getFlowHandler(d.Store))
	mux.HandleFunc("/api/events", eventsHandler(d.Store))
	mux.HandleFunc("/api/capture", captureHandler(d.Store))
	mux.HandleFunc("/api/export/har", exportHARHandler(d.Store, d.Version))
	mux.HandleFunc("/api/ca.pem", caPEMHandler(d.RootPEM))
	mux.HandleFunc("/api/rules", rulesCollection(d.Rules, d.OnRulesChange))
	mux.HandleFunc("/api/rules/", rulesItem(d.Rules, d.OnRulesChange))
	mux.HandleFunc("/api/breakpoints", breakpointsCollection(d.Breakpoints))
	mux.HandleFunc("/api/breakpoints/", breakpointsItem(d.Breakpoints))
	mux.HandleFunc("/api/diagnostics", diagnosticsHandler(d.ScriptRunner))
	mux.HandleFunc("/api/rest/send", restSendHandler(d.REST))
	mux.HandleFunc("/api/rest/from-flow/", restFromFlowHandler(d.REST, d.Store))
	mux.HandleFunc("/api/rest/env", restEnvHandler(d.REST))
	mux.HandleFunc("/api/rest/collections", restCollectionsHandler(d.REST))
	mux.HandleFunc("/api/rest/collections/", restCollectionsHandler(d.REST))
	mux.HandleFunc("/api/subscriptions", subscriptionsCollection(d.Subs))
	mux.HandleFunc("/api/subscriptions/", subscriptionsItem(d.Subs))
	registerPhaseB(mux, d)
	mux.Handle("/", spaHandler())
	return mux
}

// NewServer returns an http.Server bound to addr with the control API mux.
// [fd-limit] 真机实测: 浏览器/探针的 keep-alive 连接只增不减, 默认 nofile(1024)
// 被吃光后 accept4 报 too many open files, WebUI 整个拒服。IdleTimeout 到点收
// 空闲 keep-alive; ReadHeaderTimeout 防慢速头攻击。**不能设 ReadTimeout/
// WriteTimeout** —— /api/events 是 SSE 长连接, 写超时会把它掐掉。
// [plugin-proxy] 双挂载见 WebPrefix 注释: 裸路径(代理剥前缀后/service.sh 本地)
// + 前缀路径(旧固件直连); 根路径仅直连时 302 跳前缀。
func NewServer(addr string, d Deps) *http.Server {
	mux := NewMux(d)
	var h http.Handler = mux
	if WebPrefix != "" && WebPrefix != "/" {
		root := http.NewServeMux()
		root.Handle(WebPrefix+"/", http.StripPrefix(WebPrefix, mux))
		root.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" && r.Header.Get("X-Plugin-Proxy") != "1" {
				http.Redirect(w, r, WebPrefix+"/", http.StatusFound)
				return
			}
			mux.ServeHTTP(w, r)
		})
		h = root
	}
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
}
