package chls

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/GitterBois/proxying/internal/store"
)

// Doc is a best-effort JSON-shaped Charles export (not binary .chls).
// Real .chls is a proprietary binary; we accept JSON exports / simplified dumps.
type Doc struct {
	// Entries mirrors a minimal subset often seen in Charles JSON exports.
	Entries []Entry `json:"entries,omitempty"`
	// Charles sometimes nests under "charles" or "requests".
	Requests []Entry `json:"requests,omitempty"`
}

// Entry is one captured exchange in a simplified Charles JSON dump.
type Entry struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Path   string `json:"path,omitempty"`
	Host   string `json:"host,omitempty"`
	Scheme string `json:"scheme,omitempty"`
	Status int    `json:"status,omitempty"`
	StatusText string `json:"statusText,omitempty"`
	Request  *BodyPart `json:"request,omitempty"`
	Response *BodyPart `json:"response,omitempty"`
}

// BodyPart holds optional body text.
type BodyPart struct {
	Body    string            `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// ParseBestEffort tries JSON Charles-like dumps. Binary .chls returns a clear error.
func ParseBestEffort(b []byte) ([]store.Flow, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("empty input")
	}
	// Binary Charles magic / zip-ish
	if b[0] == 0x00 || (len(b) > 4 && b[0] == 'P' && b[1] == 'K') {
		return nil, fmt.Errorf("binary .chls not supported; export JSON from Charles or use HAR")
	}
	trim := strings.TrimSpace(string(b))
	if !strings.HasPrefix(trim, "{") && !strings.HasPrefix(trim, "[") {
		return nil, fmt.Errorf("chls: expected JSON export (binary .chls unsupported)")
	}
	var doc Doc
	if err := json.Unmarshal(b, &doc); err != nil {
		// try bare array
		var arr []Entry
		if err2 := json.Unmarshal(b, &arr); err2 != nil {
			return nil, fmt.Errorf("chls json: %w", err)
		}
		doc.Entries = arr
	}
	entries := doc.Entries
	if len(entries) == 0 {
		entries = doc.Requests
	}
	out := make([]store.Flow, 0, len(entries))
	for i, e := range entries {
		url := e.URL
		if url == "" && e.Host != "" {
			scheme := e.Scheme
			if scheme == "" {
				scheme = "https"
			}
			path := e.Path
			if path == "" {
				path = "/"
			}
			url = scheme + "://" + e.Host + path
		}
		if url == "" {
			continue
		}
		method := e.Method
		if method == "" {
			method = "GET"
		}
		status := e.Status
		var reqBody, respBody []byte
		reqHdrs := map[string][]string{}
		respHdrs := map[string][]string{}
		if e.Request != nil {
			reqBody = []byte(e.Request.Body)
			for k, v := range e.Request.Headers {
				reqHdrs[k] = []string{v}
			}
		}
		if e.Response != nil {
			respBody = []byte(e.Response.Body)
			for k, v := range e.Response.Headers {
				respHdrs[k] = []string{v}
			}
		}
		out = append(out, store.Flow{
			ID:       store.FlowID(fmt.Sprintf("chls-%d", i+1)),
			State:    store.StateDone,
			Scheme:   schemeOf(url),
			Protocol: "chls-import",
			Request: store.Message{
				Method:  method,
				URL:     url,
				Headers: reqHdrs,
				Body:    reqBody,
				Size:    int64(len(reqBody)),
			},
			Response: store.Message{
				Status:  status,
				Headers: respHdrs,
				Body:    respBody,
				Size:    int64(len(respBody)),
			},
			Tags: []string{"chls-import"},
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("chls: no entries found in JSON")
	}
	return out, nil
}

func schemeOf(raw string) string {
	if i := strings.Index(raw, "://"); i > 0 {
		return strings.ToLower(raw[:i])
	}
	return "http"
}
