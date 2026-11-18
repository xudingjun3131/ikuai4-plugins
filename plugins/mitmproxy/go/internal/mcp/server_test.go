package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GitterBois/proxying/internal/mcp"
	"github.com/GitterBois/proxying/internal/store"
)

func TestLegacyToolsList(t *testing.T) {
	sink := store.New(10)
	s := mcp.New(mcp.Deps{Store: sink})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/tools", nil)
	s.LegacyHandler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	tools, _ := body["tools"].([]any)
	if len(tools) < 3 {
		t.Fatalf("tools=%v", body)
	}
}

func TestLegacyListFlows(t *testing.T) {
	sink := store.New(10)
	sink.Add(store.Flow{
		State:     store.StateDone,
		StartedAt: time.Now(),
		Request:   store.Message{Method: "GET", URL: "http://example.com/"},
		Response:  store.Message{Status: 200},
	})
	s := mcp.New(mcp.Deps{Store: sink})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/call", strings.NewReader(`{"name":"list_flows","arguments":{}}`))
	s.LegacyHandler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestHTTPHandlerNotNil(t *testing.T) {
	s := mcp.New(mcp.Deps{Store: store.New(5)})
	if s.HTTPHandler() == nil {
		t.Fatal("nil http handler")
	}
	// Streamable endpoint should accept something (may 400 without session — not nil crash)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	s.HTTPHandler().ServeHTTP(rr, req)
	_ = context.Background()
	if rr.Code == 0 {
		t.Fatal("no response")
	}
}
