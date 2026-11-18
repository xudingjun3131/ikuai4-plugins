package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GitterBois/proxying/internal/rest"
	"github.com/GitterBois/proxying/internal/rules"
	"github.com/GitterBois/proxying/internal/store"
)

// Deps wires capture store, optional REST client, and rules for MCP tools.
type Deps struct {
	Store *store.Store
	REST  *rest.Service
	Rules *rules.Chain
}

// Server is the Model Context Protocol server for proxying (local only).
type Server struct {
	deps   Deps
	mcp    *mcp.Server
	httpH  http.Handler
	legacy *legacyHTTP // simple REST-style /api/mcp/* kept for UI
}

// New builds an MCP server with tools over the official Go SDK.
func New(deps Deps) *Server {
	s := &Server{
		deps:   deps,
		legacy: &legacyHTTP{sink: deps.Store},
	}
	ms := mcp.NewServer(&mcp.Implementation{
		Name:    "proxying",
		Version: "1.0.0",
	}, nil)
	registerTools(ms, deps)
	s.mcp = ms
	s.httpH = mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return ms
	}, nil)
	return s
}

// HTTPHandler is the Streamable HTTP MCP endpoint (mount at /mcp).
func (s *Server) HTTPHandler() http.Handler {
	if s == nil || s.httpH == nil {
		return http.NotFoundHandler()
	}
	return s.httpH
}

// LegacyHandler keeps GET /api/mcp/tools + POST /api/mcp/call for the web UI.
func (s *Server) LegacyHandler() http.HandlerFunc {
	if s == nil || s.legacy == nil {
		return func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "mcp unavailable", http.StatusServiceUnavailable)
		}
	}
	return s.legacy.Handler()
}

// RunStdio serves MCP over stdin/stdout until the client disconnects.
func (s *Server) RunStdio(ctx context.Context) error {
	if s == nil || s.mcp == nil {
		return fmt.Errorf("mcp: nil server")
	}
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}

// MCP returns the underlying SDK server (tests).
func (s *Server) MCP() *mcp.Server { return s.mcp }

func registerTools(ms *mcp.Server, deps Deps) {
	type listIn struct {
		Host   string `json:"host,omitempty" jsonschema:"filter by host substring"`
		Method string `json:"method,omitempty" jsonschema:"HTTP method filter"`
		Status int    `json:"status,omitempty" jsonschema:"response status code filter"`
		Q      string `json:"q,omitempty" jsonschema:"free-text query"`
		Limit  int    `json:"limit,omitempty" jsonschema:"max flows to return (default 50)"`
	}
	mcp.AddTool(ms, &mcp.Tool{
		Name:        "list_flows",
		Description: "List captured HTTP(S) flows from the MITM proxy buffer",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, any, error) {
		// structuredContent MUST be a JSON object (OpenCode wrapper rejects bare arrays).
		type brief struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			URL    string `json:"url"`
			Status int    `json:"status"`
			Scheme string `json:"scheme,omitempty"`
		}
		type listOut struct {
			Flows []brief `json:"flows"`
			Count int     `json:"count"`
		}
		out := listOut{Flows: []brief{}}
		if deps.Store != nil {
			list := deps.Store.List(store.Filter{
				Host:   in.Host,
				Method: in.Method,
				Status: in.Status,
				Q:      in.Q,
			})
			limit := in.Limit
			if limit <= 0 {
				limit = 50
			}
			if len(list) > limit {
				list = list[:limit]
			}
			for _, f := range list {
				out.Flows = append(out.Flows, brief{
					ID:     string(f.ID),
					Method: f.Request.Method,
					URL:    f.Request.URL,
					Status: f.Response.Status,
					Scheme: f.Scheme,
				})
			}
		}
		out.Count = len(out.Flows)
		b, _ := json.MarshalIndent(out, "", "  ")
		return textResult(string(b)), out, nil
	})

	type idIn struct {
		ID string `json:"id" jsonschema:"flow id from list_flows"`
	}
	mcp.AddTool(ms, &mcp.Tool{
		Name:        "get_flow",
		Description: "Get one captured flow by id (headers, bodies, timing metadata)",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
		if deps.Store == nil || in.ID == "" {
			return nil, nil, fmt.Errorf("id required")
		}
		f, ok := deps.Store.Get(store.FlowID(in.ID))
		if !ok {
			return nil, nil, fmt.Errorf("flow %q not found", in.ID)
		}
		b, _ := json.MarshalIndent(f, "", "  ")
		return textResult(string(b)), f, nil
	})

	mcp.AddTool(ms, &mcp.Tool{
		Name:        "clear_flows",
		Description: "Clear the in-memory capture buffer",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		if deps.Store != nil {
			deps.Store.Clear()
		}
		return textResult(`{"ok":true}`), map[string]any{"ok": true}, nil
	})

	type pauseIn struct {
		Paused bool `json:"paused" jsonschema:"true to pause capture, false to resume"`
	}
	mcp.AddTool(ms, &mcp.Tool{
		Name:        "set_capture_paused",
		Description: "Pause or resume MITM capture (paused skips adding new flows)",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pauseIn) (*mcp.CallToolResult, any, error) {
		if deps.Store != nil {
			deps.Store.SetCapturePaused(in.Paused)
		}
		out := map[string]any{"paused": in.Paused}
		b, _ := json.Marshal(out)
		return textResult(string(b)), out, nil
	})

	type sendIn struct {
		Method  string            `json:"method,omitempty" jsonschema:"HTTP method (default GET)"`
		URL     string            `json:"url" jsonschema:"absolute http(s) URL"`
		Headers map[string]string `json:"headers,omitempty"`
		Body    string            `json:"body,omitempty"`
	}
	mcp.AddTool(ms, &mcp.Tool{
		Name:        "rest_send",
		Description: "Send an HTTP(S) request via the local REST client (http/https only). Returns status/headers/body; network errors and non-2xx are still returned as data (not tool failure).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sendIn) (*mcp.CallToolResult, any, error) {
		if deps.REST == nil {
			return nil, nil, fmt.Errorf("rest client not configured")
		}
		// Cap wait so OpenCode MCP timeout is not hit on slow/down targets.
		cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		out, err := deps.REST.Send(cctx, rest.SendRequest{
			Method:  in.Method,
			URL:     in.URL,
			Headers: in.Headers,
			Body:    in.Body,
		})
		if err != nil {
			// still object for wrapper
			fail := map[string]any{"ok": false, "error": err.Error(), "url": in.URL}
			b, _ := json.MarshalIndent(fail, "", "  ")
			return textResult(string(b)), fail, nil
		}
		// shrink huge bodies for MCP transport
		body := out.Body
		if len(body) > 8000 {
			body = body[:8000] + "\n…(truncated)"
			out.Body = body
			out.Truncated = true
		}
		payload := map[string]any{
			"ok":        out.Error == "" && out.Status > 0,
			"status":    out.Status,
			"timingMs":  out.TimingMs,
			"headers":   out.Headers,
			"body":      out.Body,
			"error":     out.Error,
			"truncated": out.Truncated,
			"url":       in.URL,
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		return textResult(string(b)), payload, nil
	})

	mcp.AddTool(ms, &mcp.Tool{
		Name:        "list_rules",
		Description: "List MITM rewrite/block/map rules",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		type rulesOut struct {
			Rules any `json:"rules"`
			Count int `json:"count"`
		}
		var list any = []any{}
		n := 0
		if deps.Rules != nil {
			rl := deps.Rules.List()
			list = rl
			n = len(rl)
		}
		out := rulesOut{Rules: list, Count: n}
		b, _ := json.MarshalIndent(out, "", "  ")
		return textResult(string(b)), out, nil
	})

	type ruleIn struct {
		ID      string `json:"id,omitempty"`
		Enabled bool   `json:"enabled"`
		Type    string `json:"type" jsonschema:"host_block|host_allow|header|body_replace|map_local|..."`
		Host    string `json:"host,omitempty" jsonschema:"match host (wildcards ok)"`
		Path    string `json:"path,omitempty" jsonschema:"match path regex"`
	}
	mcp.AddTool(ms, &mcp.Tool{
		Name:        "add_host_block_rule",
		Description: "Add a host_block rule (403 for matching hosts)",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ruleIn) (*mcp.CallToolResult, any, error) {
		if deps.Rules == nil {
			return nil, nil, fmt.Errorf("rules not configured")
		}
		if in.Host == "" {
			return nil, nil, fmt.Errorf("host required")
		}
		id := in.ID
		if id == "" {
			id = fmt.Sprintf("mcp-%d", len(deps.Rules.List())+1)
		}
		list := deps.Rules.List()
		list = append(list, rules.Rule{
			ID:      id,
			Enabled: true,
			Type:    rules.TypeHostBlock,
			Match:   rules.Match{Host: in.Host, Path: in.Path},
		})
		if err := deps.Rules.Save(list); err != nil {
			return nil, nil, err
		}
		out := map[string]any{"ok": true, "id": id, "host": in.Host}
		b, _ := json.Marshal(out)
		return textResult(string(b)), out, nil
	})

	type fromFlowIn struct {
		ID   string `json:"id" jsonschema:"captured flow id"`
		Send bool   `json:"send,omitempty" jsonschema:"if true, execute the request immediately"`
	}
	mcp.AddTool(ms, &mcp.Tool{
		Name:        "rest_from_flow",
		Description: "Build a REST request from a captured flow; optionally send it",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in fromFlowIn) (*mcp.CallToolResult, any, error) {
		if deps.Store == nil || in.ID == "" {
			return nil, nil, fmt.Errorf("id required")
		}
		f, ok := deps.Store.Get(store.FlowID(in.ID))
		if !ok {
			return nil, nil, fmt.Errorf("flow not found")
		}
		req := rest.FromFlow(f)
		if !in.Send || deps.REST == nil {
			b, _ := json.MarshalIndent(req, "", "  ")
			return textResult(string(b)), req, nil
		}
		out, err := deps.REST.Send(ctx, req)
		if err != nil {
			return nil, nil, err
		}
		payload := map[string]any{"request": req, "response": out}
		b, _ := json.MarshalIndent(payload, "", "  ")
		return textResult(string(b)), payload, nil
	})

	_ = strings.Builder{} // keep import if needed
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: s},
		},
	}
}
