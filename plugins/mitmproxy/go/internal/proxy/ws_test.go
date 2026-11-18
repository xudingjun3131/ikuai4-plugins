package proxy

import (
	"encoding/binary"
	"testing"
)

func Test_parseOneWSFrame_unmasked_text(t *testing.T) {
	// FIN + text opcode, unmasked, payload "hi"
	raw := []byte{0x81, 0x02, 'h', 'i'}
	fr, n, ok := parseOneWSFrame(raw)
	if !ok || n != 4 {
		t.Fatalf("ok=%v n=%d", ok, n)
	}
	if fr.Opcode != 1 || fr.Len != 2 || string(fr.Payload) != "hi" {
		t.Fatalf("got opcode=%d len=%d payload=%q", fr.Opcode, fr.Len, fr.Payload)
	}
}

func Test_parseOneWSFrame_masked(t *testing.T) {
	mask := [4]byte{0x01, 0x02, 0x03, 0x04}
	payload := []byte{'h', 'i'}
	masked := make([]byte, 2)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	raw := []byte{0x81, 0x82, mask[0], mask[1], mask[2], mask[3], masked[0], masked[1]}
	fr, n, ok := parseOneWSFrame(raw)
	if !ok || n != 8 {
		t.Fatalf("ok=%v n=%d", ok, n)
	}
	if string(fr.Payload) != "hi" {
		t.Fatalf("payload=%q", fr.Payload)
	}
}

func Test_parseOneWSFrame_extended_len(t *testing.T) {
	// 126-byte length encoding, empty payload
	raw := make([]byte, 4)
	raw[0] = 0x81
	raw[1] = 126
	binary.BigEndian.PutUint16(raw[2:4], 0)
	fr, n, ok := parseOneWSFrame(raw)
	if !ok || n != 4 || fr.Len != 0 {
		t.Fatalf("ok=%v n=%d len=%d", ok, n, fr.Len)
	}
}

func Test_capWSPayload(t *testing.T) {
	p, trunc := capWSPayload([]byte("hello"), 3)
	if !trunc || string(p) != "hel" {
		t.Fatalf("p=%q trunc=%v", p, trunc)
	}
}
