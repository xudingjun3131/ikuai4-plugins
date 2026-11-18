package proxy

import (
	"encoding/binary"
	"io"
	"sync"
	"time"

	"github.com/GitterBois/runhooks/internal/store"
)

const maxWSFrames = 256

// wsCapture tees WebSocket bytes while parsing RFC6455 frames into the store.
// Implements io.ReadWriter so goproxy's proxyWebsocket can use it as remoteConn.
type wsCapture struct {
	inner   io.ReadWriter
	sink    *store.Store
	flowID  store.FlowID
	maxBody int64

	mu     sync.Mutex
	frames []store.WSFrame

	// partial parse state for each direction
	c2s *wsParser // writes from client → server
	s2c *wsParser // reads from server → client
}

func newWSCapture(inner io.ReadWriter, sink *store.Store, flowID store.FlowID, maxBody int64) *wsCapture {
	w := &wsCapture{inner: inner, sink: sink, flowID: flowID, maxBody: maxBody}
	w.c2s = &wsParser{dir: "c2s", onFrame: w.record}
	w.s2c = &wsParser{dir: "s2c", onFrame: w.record}
	return w
}

func (w *wsCapture) record(fr store.WSFrame) {
	fr.Payload, fr.Truncated = capWSPayload(fr.Payload, w.maxBody)
	w.addFrame(fr)
}

func (w *wsCapture) Read(p []byte) (int, error) {
	n, err := w.inner.Read(p)
	if n > 0 {
		w.s2c.feed(p[:n])
	}
	return n, err
}

func (w *wsCapture) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.c2s.feed(p)
	}
	return w.inner.Write(p)
}

func (w *wsCapture) addFrame(fr store.WSFrame) {
	w.mu.Lock()
	if len(w.frames) >= maxWSFrames {
		w.mu.Unlock()
		return
	}
	w.frames = append(w.frames, fr)
	cp := append([]store.WSFrame(nil), w.frames...)
	w.mu.Unlock()
	if w.sink == nil || w.flowID == "" {
		return
	}
	_ = w.sink.Update(w.flowID, func(f *store.Flow) {
		f.WSFrames = cp
		f.Protocol = "websocket"
		f.State = store.StateActive
	})
}

func (w *wsCapture) finish() {
	w.mu.Lock()
	cp := append([]store.WSFrame(nil), w.frames...)
	w.mu.Unlock()
	if w.sink == nil || w.flowID == "" {
		return
	}
	_ = w.sink.Update(w.flowID, func(f *store.Flow) {
		f.WSFrames = cp
		f.Protocol = "websocket"
		f.State = store.StateDone
		f.DoneAt = time.Now()
	})
}

type wsParser struct {
	dir     string
	onFrame func(store.WSFrame)
	buf     []byte
}

func (p *wsParser) feed(b []byte) {
	p.buf = append(p.buf, b...)
	for {
		fr, n, ok := parseOneWSFrame(p.buf)
		if !ok {
			return
		}
		p.buf = p.buf[n:]
		fr.Dir = p.dir
		fr.At = time.Now()
		if p.onFrame != nil {
			p.onFrame(fr)
		}
	}
}

// feedWithCap is like feed but caps payload using maxBody from parent.

// parseOneWSFrame returns one frame, bytes consumed, and whether a full frame was available.
func parseOneWSFrame(b []byte) (store.WSFrame, int, bool) {
	if len(b) < 2 {
		return store.WSFrame{}, 0, false
	}
	opcode := int(b[0] & 0x0f)
	masked := b[1]&0x80 != 0
	plen := int64(b[1] & 0x7f)
	off := 2
	switch plen {
	case 126:
		if len(b) < 4 {
			return store.WSFrame{}, 0, false
		}
		plen = int64(binary.BigEndian.Uint16(b[2:4]))
		off = 4
	case 127:
		if len(b) < 10 {
			return store.WSFrame{}, 0, false
		}
		plen = int64(binary.BigEndian.Uint64(b[2:10]))
		off = 10
	}
	maskOff := off
	if masked {
		off += 4
	}
	if plen < 0 || plen > 1<<30 {
		// skip garbage; consume header only to avoid hang
		return store.WSFrame{}, off, true
	}
	total := off + int(plen)
	if len(b) < total {
		return store.WSFrame{}, 0, false
	}
	payload := make([]byte, plen)
	copy(payload, b[off:total])
	if masked {
		var mask [4]byte
		copy(mask[:], b[maskOff:maskOff+4])
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	fr := store.WSFrame{Opcode: opcode, Len: plen, Payload: payload}
	return fr, total, true
}

// capWSPayload truncates payload for store (caller holds maxBody).
func capWSPayload(payload []byte, max int64) (out []byte, trunc bool) {
	if max < 0 {
		max = 0
	}
	if int64(len(payload)) <= max {
		return append([]byte(nil), payload...), false
	}
	return append([]byte(nil), payload[:max]...), true
}
