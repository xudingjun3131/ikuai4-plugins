package diff

import (
	"fmt"
	"strings"

	"github.com/GitterBois/proxying/internal/store"
)

// Hunk is one unified-diff style change block.
type Hunk struct {
	Side    string `json:"side"` // request|response|meta
	Field   string `json:"field"`
	Left    string `json:"left"`
	Right   string `json:"right"`
	Changed bool   `json:"changed"`
}

// Result compares two flows.
type Result struct {
	LeftID  string `json:"leftId"`
	RightID string `json:"rightId"`
	Hunks   []Hunk `json:"hunks"`
}

// Compare produces field-level hunks between a and b.
func Compare(a, b store.Flow) Result {
	r := Result{LeftID: string(a.ID), RightID: string(b.ID)}
	add := func(side, field, left, right string) {
		r.Hunks = append(r.Hunks, Hunk{
			Side:    side,
			Field:   field,
			Left:    left,
			Right:   right,
			Changed: left != right,
		})
	}
	add("meta", "scheme", a.Scheme, b.Scheme)
	add("meta", "protocol", a.Protocol, b.Protocol)
	add("meta", "error", a.Error, b.Error)
	add("request", "method", a.Request.Method, b.Request.Method)
	add("request", "url", a.Request.URL, b.Request.URL)
	add("request", "proto", a.Request.Proto, b.Request.Proto)
	add("request", "body", string(a.Request.Body), string(b.Request.Body))
	add("request", "headers", flatHeaders(a.Request.Headers), flatHeaders(b.Request.Headers))
	add("response", "status", fmt.Sprintf("%d", a.Response.Status), fmt.Sprintf("%d", b.Response.Status))
	add("response", "proto", a.Response.Proto, b.Response.Proto)
	add("response", "body", string(a.Response.Body), string(b.Response.Body))
	add("response", "headers", flatHeaders(a.Response.Headers), flatHeaders(b.Response.Headers))
	return r
}

// ChangedOnly filters to hunks where Left != Right.
func ChangedOnly(r Result) Result {
	out := Result{LeftID: r.LeftID, RightID: r.RightID}
	for _, h := range r.Hunks {
		if h.Changed {
			out.Hunks = append(out.Hunks, h)
		}
	}
	return out
}

func flatHeaders(h map[string][]string) string {
	if h == nil {
		return ""
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	// stable-ish: sort via simple insertion for small maps
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && strings.ToLower(keys[j]) < strings.ToLower(keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(strings.Join(h[k], ","))
		b.WriteByte('\n')
	}
	return b.String()
}
