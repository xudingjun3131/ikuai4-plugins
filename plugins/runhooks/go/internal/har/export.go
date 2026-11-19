package har

import (
	"encoding/base64"
	"net/url"
	"strings"
	"time"

	"github.com/GitterBois/runhooks/internal/store"
)

const version = "1.2"

// Document is a HAR 1.2 root object: { "log": ... }.
type Document struct {
	Log Log `json:"log"`
}

// Log is the HAR log container.
type Log struct {
	Version string  `json:"version"`
	Creator Creator `json:"creator"`
	Entries []Entry `json:"entries"`
}

// Creator identifies the exporter.
type Creator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Entry is one HAR request/response pair.
type Entry struct {
	StartedDateTime string   `json:"startedDateTime"`
	Time            float64  `json:"time"`
	Request         Request  `json:"request"`
	Response        Response `json:"response"`
	Cache           struct{} `json:"cache"`
	Timings         Timings  `json:"timings"`
}

// Timings holds HAR timing fields (ms). Unknown phases use -1.
type Timings struct {
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
}

// Request is a HAR request object.
type Request struct {
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	HTTPVersion string         `json:"httpVersion"`
	Cookies     []any          `json:"cookies"`
	Headers     []NameValue    `json:"headers"`
	QueryString []NameValue    `json:"queryString"`
	HeadersSize int            `json:"headersSize"`
	BodySize    int            `json:"bodySize"`
	PostData    *PostData      `json:"postData,omitempty"`
}

// Response is a HAR response object.
type Response struct {
	Status      int         `json:"status"`
	StatusText  string      `json:"statusText"`
	HTTPVersion string      `json:"httpVersion"`
	Cookies     []any       `json:"cookies"`
	Headers     []NameValue `json:"headers"`
	Content     Content     `json:"content"`
	RedirectURL string      `json:"redirectURL"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int         `json:"bodySize"`
}

// NameValue is a HAR name/value pair.
type NameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Content is HAR response content.
type Content struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

// PostData is HAR request body (optional).
type PostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

// FromFlows builds a HAR 1.2 document from captured flows (oldest-first).
func FromFlows(flows []store.Flow, appVersion string) Document {
	if appVersion == "" {
		appVersion = "0.0.0-dev"
	}
	entries := make([]Entry, 0, len(flows))
	for _, f := range flows {
		entries = append(entries, entryFromFlow(f))
	}
	return Document{
		Log: Log{
			Version: version,
			Creator: Creator{Name: "netloom", Version: appVersion},
			Entries: entries,
		},
	}
}

func entryFromFlow(f store.Flow) Entry {
	started := f.StartedAt
	if started.IsZero() {
		started = time.Unix(0, 0).UTC()
	}
	elapsed := 0.0
	if !f.DoneAt.IsZero() && !f.StartedAt.IsZero() {
		elapsed = float64(f.DoneAt.Sub(f.StartedAt).Milliseconds())
	}
	return Entry{
		StartedDateTime: started.UTC().Format(time.RFC3339Nano),
		Time:            elapsed,
		Request:         requestFrom(f.Request),
		Response:        responseFrom(f.Response),
		Timings: Timings{
			Send:    0,
			Wait:    elapsed,
			Receive: 0,
		},
	}
}

func requestFrom(m store.Message) Request {
	headers := headersToNV(m.Headers)
	req := Request{
		Method:      m.Method,
		URL:         m.URL,
		HTTPVersion: protoOrDefault(m.Proto),
		Cookies:     []any{},
		Headers:     headers,
		QueryString: queryFromURL(m.URL),
		HeadersSize: -1,
		BodySize:    bodySize(m),
	}
	if len(m.Body) > 0 {
		req.PostData = &PostData{
			MimeType: mimeOf(m.Headers),
			Text:     base64.StdEncoding.EncodeToString(m.Body),
			Encoding: "base64",
		}
	}
	return req
}

func responseFrom(m store.Message) Response {
	return Response{
		Status:      m.Status,
		StatusText:  statusText(m.Status),
		HTTPVersion: protoOrDefault(m.Proto),
		Cookies:     []any{},
		Headers:     headersToNV(m.Headers),
		Content: Content{
			Size:     int(m.Size),
			MimeType: mimeOf(m.Headers),
			Text:     bodyText(m.Body),
			Encoding: bodyEncoding(m.Body),
		},
		RedirectURL: firstHeader(m.Headers, "Location"),
		HeadersSize: -1,
		BodySize:    bodySize(m),
	}
}

func headersToNV(h map[string][]string) []NameValue {
	if len(h) == 0 {
		return []NameValue{}
	}
	out := make([]NameValue, 0, len(h))
	for k, vals := range h {
		for _, v := range vals {
			out = append(out, NameValue{Name: k, Value: v})
		}
	}
	return out
}

func queryFromURL(raw string) []NameValue {
	u, err := url.Parse(raw)
	if err != nil || u.RawQuery == "" {
		return []NameValue{}
	}
	q := u.Query()
	out := make([]NameValue, 0, len(q))
	for k, vals := range q {
		for _, v := range vals {
			out = append(out, NameValue{Name: k, Value: v})
		}
	}
	return out
}

func mimeOf(h map[string][]string) string {
	if m := firstHeader(h, "Content-Type"); m != "" {
		return m
	}
	return "application/octet-stream"
}

func firstHeader(h map[string][]string, name string) string {
	if h == nil {
		return ""
	}
	for k, vals := range h {
		if strings.EqualFold(k, name) && len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

func protoOrDefault(p string) string {
	if p == "" {
		return "HTTP/1.1"
	}
	return p
}

func bodySize(m store.Message) int {
	if m.Size > 0 {
		return int(m.Size)
	}
	return len(m.Body)
}

func bodyText(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

func bodyEncoding(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return "base64"
}

func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 204:
		return "No Content"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 304:
		return "Not Modified"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 500:
		return "Internal Server Error"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	default:
		if code == 0 {
			return ""
		}
		return ""
	}
}
