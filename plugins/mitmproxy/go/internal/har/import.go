package har

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/GitterBois/proxying/internal/store"
)

// ToFlows parses a HAR 1.2 document into store flows.
func ToFlows(doc Document) ([]store.Flow, error) {
	if doc.Log.Version != "" && !strings.HasPrefix(doc.Log.Version, "1.") {
		return nil, fmt.Errorf("unsupported HAR version %q", doc.Log.Version)
	}
	out := make([]store.Flow, 0, len(doc.Log.Entries))
	for i, e := range doc.Log.Entries {
		f, err := flowFromEntry(e, i)
		if err != nil {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// ParseJSON unmarshals HAR bytes.
func ParseJSON(b []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(b, &doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func flowFromEntry(e Entry, idx int) (store.Flow, error) {
	started, err := time.Parse(time.RFC3339Nano, e.StartedDateTime)
	if err != nil {
		started, err = time.Parse(time.RFC3339, e.StartedDateTime)
		if err != nil {
			started = time.Unix(int64(idx), 0).UTC()
		}
	}
	done := started.Add(time.Duration(e.Time) * time.Millisecond)
	reqBody := decodeBody(e.Request.PostData)
	respBody := decodeContent(e.Response.Content)
	return store.Flow{
		ID:        store.FlowID(fmt.Sprintf("har-%d", idx+1)),
		State:     store.StateDone,
		StartedAt: started,
		DoneAt:    done,
		Scheme:    schemeFromURL(e.Request.URL),
		Protocol:  e.Request.HTTPVersion,
		Request: store.Message{
			Method:  e.Request.Method,
			URL:     e.Request.URL,
			Proto:   e.Request.HTTPVersion,
			Headers: nvToMap(e.Request.Headers),
			Body:    reqBody,
			Size:    int64(len(reqBody)),
		},
		Response: store.Message{
			Status:  e.Response.Status,
			Proto:   e.Response.HTTPVersion,
			Headers: nvToMap(e.Response.Headers),
			Body:    respBody,
			Size:    int64(len(respBody)),
		},
		Tags: []string{"har-import"},
	}, nil
}

func decodeBody(pd *PostData) []byte {
	if pd == nil || pd.Text == "" {
		return nil
	}
	if strings.EqualFold(pd.Encoding, "base64") {
		b, err := base64.StdEncoding.DecodeString(pd.Text)
		if err == nil {
			return b
		}
	}
	return []byte(pd.Text)
}

func decodeContent(c Content) []byte {
	if c.Text == "" {
		return nil
	}
	if strings.EqualFold(c.Encoding, "base64") {
		b, err := base64.StdEncoding.DecodeString(c.Text)
		if err == nil {
			return b
		}
	}
	return []byte(c.Text)
}

func nvToMap(nv []NameValue) map[string][]string {
	if len(nv) == 0 {
		return nil
	}
	out := make(map[string][]string, len(nv))
	for _, p := range nv {
		out[p.Name] = append(out[p.Name], p.Value)
	}
	return out
}

func schemeFromURL(raw string) string {
	if i := strings.Index(raw, "://"); i > 0 {
		return strings.ToLower(raw[:i])
	}
	return "http"
}
