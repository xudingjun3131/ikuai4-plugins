// Package proxy implements the HTTP(S) MITM capture and forwarding core.
//
// Construct for production (todo 7 wires this in main):
//
//	auth, _ := ca.Load(cfg.DataDir)
//	sink := store.New(cfg.MaxFlows)
//	eng, _ := proxy.NewEngine(proxy.EngineConfig{
//		Authority: auth,
//		Sink:      sink,
//		MaxBody:   cfg.MaxBody,
//		Hooks:     proxy.HookSlots{}, // Rules/Script/Breakpoint later
//	})
//	proxySrv := eng.NewServer(cfg.ProxyAddr)
package proxy
