package report_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GitterBois/runhooks/internal/report"
	"github.com/GitterBois/runhooks/internal/store"
)

func Test_Report_ingest_creates_flow_and_agent(t *testing.T) {
	sink := store.New(10)
	srv := report.New(sink, "secret")
	body, _ := json.Marshal(report.Payload{
		AgentID:   "phone1",
		AgentName: "Pixel",
		Method:    "GET",
		URL:       "https://api.example.com/me",
		Status:    200,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/report", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Report-Token", "secret")
	rr := httptest.NewRecorder()
	srv.IngestHandler()(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	list := sink.List(store.Filter{Host: "api.example.com"})
	if len(list) != 1 {
		t.Fatalf("flows=%d", len(list))
	}
	agents := srv.ListAgents()
	if len(agents) != 1 || agents[0].ID != "phone1" {
		t.Fatalf("agents=%+v", agents)
	}
}

func Test_Report_token_required(t *testing.T) {
	srv := report.New(store.New(5), "tok")
	req := httptest.NewRequest(http.MethodPost, "/api/report", bytes.NewReader([]byte(`{"url":"http://x"}`)))
	rr := httptest.NewRecorder()
	srv.IngestHandler()(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rr.Code)
	}
}
