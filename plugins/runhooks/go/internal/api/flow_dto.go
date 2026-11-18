package api

import (
	"encoding/base64"
	"time"

	"github.com/GitterBois/runhooks/internal/store"
)

// flowsResponse is GET /api/flows JSON body (frozen contract).
type flowsResponse struct {
	Flows []flowDTO `json:"flows"`
}

// flowDTO is the JSON shape for one captured flow.
type flowDTO struct {
	ID         string       `json:"id"`
	State      string       `json:"state"`
	StartedAt  time.Time    `json:"startedAt"`
	DoneAt     *time.Time   `json:"doneAt,omitempty"`
	ClientAddr string       `json:"clientAddr,omitempty"`
	Scheme     string       `json:"scheme,omitempty"`
	Protocol   string       `json:"protocol,omitempty"`
	Request    messageDTO   `json:"request"`
	Response   messageDTO   `json:"response"`
	Error          string       `json:"error,omitempty"`
	Tags           []string     `json:"tags,omitempty"`
	WSFrames       []wsFrameDTO `json:"wsFrames,omitempty"`
	HighlightColor string       `json:"highlightColor,omitempty"`
	HighlightLabel string       `json:"highlightLabel,omitempty"`
}

// wsFrameDTO is one captured WebSocket frame in API JSON.
type wsFrameDTO struct {
	Dir       string    `json:"dir"`
	Opcode    int       `json:"opcode"`
	Len       int64     `json:"len"`
	Payload   string    `json:"payload,omitempty"` // base64
	Truncated bool      `json:"truncated,omitempty"`
	At        time.Time `json:"at"`
}

// messageDTO is request or response in API JSON.
// Body is base64 of raw captured bytes; BodyDisplay is optional gunzipped text for UI.
type messageDTO struct {
	Method      string              `json:"method,omitempty"`
	URL         string              `json:"url,omitempty"`
	Proto       string              `json:"proto,omitempty"`
	Status      int                 `json:"status,omitempty"`
	Headers     map[string][]string `json:"headers,omitempty"`
	Body        string              `json:"body,omitempty"` // base64
	BodyDisplay string              `json:"bodyDisplay,omitempty"`
	Truncated   bool                `json:"truncated,omitempty"`
	Size        int64               `json:"size,omitempty"`
}

func flowToDTO(f store.Flow) flowDTO {
	dto := flowDTO{
		ID:         string(f.ID),
		State:      string(f.State),
		StartedAt:  f.StartedAt,
		ClientAddr: f.ClientAddr,
		Scheme:     f.Scheme,
		Protocol:   f.Protocol,
		Request:    messageToDTO(f.Request),
		Response:   messageToDTO(f.Response),
		Error:          f.Error,
		Tags:           f.Tags,
		HighlightColor: f.HighlightColor,
		HighlightLabel: f.HighlightLabel,
	}
	if !f.DoneAt.IsZero() {
		t := f.DoneAt
		dto.DoneAt = &t
	}
	if len(f.WSFrames) > 0 {
		dto.WSFrames = make([]wsFrameDTO, len(f.WSFrames))
		for i, fr := range f.WSFrames {
			dto.WSFrames[i] = wsFrameDTO{
				Dir:       fr.Dir,
				Opcode:    fr.Opcode,
				Len:       fr.Len,
				Truncated: fr.Truncated,
				At:        fr.At,
			}
			if len(fr.Payload) > 0 {
				dto.WSFrames[i].Payload = base64.StdEncoding.EncodeToString(fr.Payload)
			}
		}
	}
	return dto
}

func messageToDTO(m store.Message) messageDTO {
	dto := messageDTO{
		Method:    m.Method,
		URL:       m.URL,
		Proto:     m.Proto,
		Status:    m.Status,
		Headers:   m.Headers,
		Truncated: m.Truncated,
		Size:      m.Size,
	}
	if len(m.Body) > 0 {
		dto.Body = base64.StdEncoding.EncodeToString(m.Body)
		dto.BodyDisplay = bodyDisplay(m.Headers, m.Body)
	}
	return dto
}
