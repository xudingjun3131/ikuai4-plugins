package store

import "time"

// FlowID is a branded capture flow identifier.
type FlowID string

// State is the lifecycle of a captured flow.
type State string

const (
	StatePending State = "pending"
	StateActive  State = "active"
	StateDone    State = "done"
	StateError   State = "error"
)

// Message is a captured HTTP request or response (frozen contract).
type Message struct {
	Method    string
	URL       string
	Proto     string
	Status    int // response only; 0 for request
	Headers   map[string][]string
	Body      []byte
	Truncated bool
	Size      int64
}

// WSFrame is one captured WebSocket data/control frame (todo 22).
type WSFrame struct {
	Dir       string    // c2s | s2c
	Opcode    int       // RFC6455 opcode
	Len       int64     // full payload length
	Payload   []byte    // capped capture
	Truncated bool
	At        time.Time
}

// Flow is one captured request/response exchange (frozen contract).
type Flow struct {
	ID             FlowID
	State          State
	StartedAt      time.Time
	DoneAt         time.Time // zero until terminal
	ClientAddr     string
	Scheme         string
	Protocol       string // e.g. HTTP/1.1, HTTP/2.0, websocket
	Request        Message
	Response       Message
	Error          string
	Tags           []string
	WSFrames       []WSFrame
	HighlightColor string
	HighlightLabel string
	JA3            string // TLS 客户端指纹(md5), 仅透明路径可算
	DstIP          string // 目标 IP(连接层范围匹配用)
}

type CondOp string

const (
	CondAND CondOp = "and"
	CondOR  CondOp = "or"
)

// Cond is one multi-condition search clause (field: host|method|status|q|path|header|tag|protocol|scheme).
type Cond struct {
	Field string
	Op    string // eq|contains|neq; empty defaults by field
	Value string
}

// Filter selects flows for List. Host/Method/Status/Q always AND; Conds use Op (default and).
type Filter struct {
	Host   string
	Method string
	Status int
	Q      string
	Op     CondOp
	Conds  []Cond
}
