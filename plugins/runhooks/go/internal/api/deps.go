package api

import (
	"github.com/GitterBois/runhooks/internal/bookmark"
	"github.com/GitterBois/runhooks/internal/breakpoint"
	"github.com/GitterBois/runhooks/internal/mcp"
	"github.com/GitterBois/runhooks/internal/netcond"
	"github.com/GitterBois/runhooks/internal/report"
	"github.com/GitterBois/runhooks/internal/rest"
	"github.com/GitterBois/runhooks/internal/rules"
	"github.com/GitterBois/runhooks/internal/script"
	"github.com/GitterBois/runhooks/internal/session"
	"github.com/GitterBois/runhooks/internal/store"
	"github.com/GitterBois/runhooks/internal/subscription"
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
	Subs            *subscription.Manager
}
