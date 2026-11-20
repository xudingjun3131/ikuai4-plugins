package main

import (
	"encoding/binary"
	"strings"
	"testing"
)

func Test_dnsQuestion(t *testing.T) {
	q := make([]byte, 12)
	binary.BigEndian.PutUint16(q[0:2], 0x1234)
	binary.BigEndian.PutUint16(q[4:6], 1) // QDCOUNT=1
	q = append(q, 3, 'w', 'w', 'w', 5, 'b', 'a', 'i', 'd', 'u', 3, 'c', 'o', 'm', 0)
	q = append(q, 0, 1, 0, 1) // QTYPE=A QCLASS=IN
	name, ok := dnsQuestion(q)
	if !ok || name != "www.baidu.com" {
		t.Fatalf("dnsQuestion=%q ok=%v", name, ok)
	}
	if _, ok := dnsQuestion([]byte{1, 2, 3}); ok {
		t.Error("short packet accepted")
	}
	if _, ok := dnsQuestion(make([]byte, 12)); ok {
		t.Error("zero-question packet accepted")
	}
}

func Test_sinkholeResponse(t *testing.T) {
	q := make([]byte, 12)
	binary.BigEndian.PutUint16(q[4:6], 1)
	binary.BigEndian.PutUint16(q[6:8], 99) // garbage ANCOUNT
	q = append(q, 3, 'a', 'd', 's', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1)
	r := sinkholeResponse(q)
	if len(r) != len(q) {
		t.Fatalf("len=%d want %d", len(r), len(q))
	}
	if r[2]&0x80 == 0 {
		t.Error("QR bit not set")
	}
	if binary.BigEndian.Uint16(r[6:8]) != 0 || binary.BigEndian.Uint16(r[8:10]) != 0 {
		t.Error("answer/authority counts not zeroed")
	}
	if binary.BigEndian.Uint16(r[0:2]) != binary.BigEndian.Uint16(q[0:2]) {
		t.Error("transaction id changed")
	}
	if !strings.Contains(string(r), "ads") {
		t.Error("question lost")
	}
}
