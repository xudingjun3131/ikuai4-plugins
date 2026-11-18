package api

import (
	"github.com/GitterBois/proxying/internal/bookmark"
	"github.com/GitterBois/proxying/internal/breakpoint"
	"github.com/GitterBois/proxying/internal/mcp"
	"github.com/GitterBois/proxying/internal/netcond"
	"github.com/GitterBois/proxying/internal/report"
	"github.com/GitterBois/proxying/internal/rest"
	"github.com/GitterBois/proxying/internal/rules"
	"github.com/GitterBois/proxying/internal/script"
	"github.com/GitterBois/proxying/internal/session"
	"github.com/GitterBois/proxying/internal/store"
)

type Deps struct {
	Version         string
	DataDir         string
	ProxyAddr       string
	Store           *store.Store
	RootPEM         func() []byte
	Rules           *rules.Chain
	OnRulesChange   func()
	Breakpoints     *breakpoint.Manager
	REST            *rest.Service
	Scripts         *script.Gallery
	OnScriptChange  func()
	Sessions        *session.Store
	Bookmarks       *bookmark.Store
	Report          *report.Server
	NetCond         *netcond.Cond
	MCP             *mcp.Server
}
