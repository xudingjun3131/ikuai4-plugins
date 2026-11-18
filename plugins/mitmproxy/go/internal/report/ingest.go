package report

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GitterBois/proxying/internal/store"
)

// Agent is a LAN client that posted reports.
type Agent struct {
	ID         string    `json:"id"`
	Name       string    `json:"name,omitempty"`
	LastSeen   time.Time `json:"lastSeen"`
	ReportCount int      `json:"reportCount"`
}

// Payload is POST /api/report body.
type Payload struct {
	AgentID   string            `json:"agentId"`
	AgentName string            `json:"agentName,omitempty"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Status    int               `json:"status,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      string            `json:"body,omitempty"`
	Error     string            `json:"error,omitempty"`
	Scheme    string            `json:"scheme,omitempty"`
}

// Server ingests reports into the capture store and tracks agents.
type Server struct {
	mu     sync.RWMutex
	sink   *store.Store
	token  string
	agents map[string]*Agent
}

func New(sink *store.Store, token string) *Server {
	return &Server{
		sink:   sink,
		token:  token,
		agents: make(map[string]*Agent),
	}
}

func (s *Server) auth(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	got := r.Header.Get("X-Report-Token")
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

// IngestHandler handles POST /api/report.
func (s *Server) IngestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var p Payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if p.URL == "" {
			http.Error(w, "url required", http.StatusBadRequest)
			return
		}
		if p.Method == "" {
			p.Method = "GET"
		}
		if p.AgentID == "" {
			p.AgentID = "anonymous"
		}
		now := time.Now().UTC()
		hdrs := map[string][]string{}
		for k, v := range p.Headers {
			hdrs[k] = []string{v}
		}
		flow := store.Flow{
			State:      store.StateDone,
			StartedAt:  now,
			DoneAt:     now,
			ClientAddr: r.RemoteAddr,
			Scheme:     p.Scheme,
			Protocol:   "report",
			Request: store.Message{
				Method:  p.Method,
				URL:     p.URL,
				Headers: hdrs,
				Body:    []byte(p.Body),
				Size:    int64(len(p.Body)),
			},
			Response: store.Message{Status: p.Status},
			Error:    p.Error,
			Tags:     []string{"report", "agent:" + p.AgentID},
		}
		if s.sink != nil {
			if _, ok := s.sink.Add(flow); !ok {
				http.Error(w, "capture paused", http.StatusServiceUnavailable)
				return
			}
		}
		s.touchAgent(p.AgentID, p.AgentName, now)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "agentId": p.AgentID})
	}
}

func (s *Server) touchAgent(id, name string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[id]
	if !ok {
		a = &Agent{ID: id}
		s.agents[id] = a
	}
	a.LastSeen = at
	a.ReportCount++
	if name != "" {
		a.Name = name
	}
}

// ListAgents returns known LAN agents.
func (s *Server) ListAgents() []Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Agent, 0, len(s.agents))
	for _, a := range s.agents {
		cp := *a
		out = append(out, cp)
	}
	return out
}

// AgentsHandler GET /api/agents
func (s *Server) AgentsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"agents": s.ListAgents()})
	}
}

// String for debugging.
func (s *Server) String() string {
	return fmt.Sprintf("report.Server(agents=%d token=%v)", len(s.ListAgents()), s.token != "")
}

// NormalizeAgentID strips spaces.
func NormalizeAgentID(id string) string {
	return strings.TrimSpace(id)
}
