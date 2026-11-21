package main

import (
	"encoding/binary"
	"testing"
)

// [D9] 上游失败返回 SERVFAIL(rcode=2), 不再静默空应答断解析。
func Test_D9_servfailResponse(t *testing.T) {
	q := make([]byte, 12)
	binary.BigEndian.PutUint16(q[0:2], 0xabcd)
	binary.BigEndian.PutUint16(q[4:6], 1)
	q = append(q, 3, 'a', 'd', 's', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1)
	r := servfailResponse(q)
	if r[2]&0x80 == 0 {
		t.Error("QR bit not set")
	}
	if r[3]&0x0f != 2 {
		t.Errorf("rcode=%d want 2(SERVFAIL)", r[3]&0x0f)
	}
	if binary.BigEndian.Uint16(r[6:8]) != 0 {
		t.Error("ANCOUNT not zeroed")
	}
	if binary.BigEndian.Uint16(r[0:2]) != 0xabcd {
		t.Error("transaction id changed")
	}
	// sinkhole 仍是 rcode=0 的空应答(域名解析不到)
	s := sinkholeResponse(q)
	if s[3]&0x0f != 0 {
		t.Errorf("sinkhole rcode=%d want 0", s[3]&0x0f)
	}
}

// [D8] 从 DNS 应答 Answer 段提取 A/AAAA, 供威胁情报 L3 复核。
func Test_D8_answerIPs(t *testing.T) {
	// 构造: header(QD=1 AN=2) + question "x.test" + 2 条 answer(NAME 指针 + A/AAAA)
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[4:6], 1) // QDCOUNT
	binary.BigEndian.PutUint16(msg[6:8], 2) // ANCOUNT
	msg = append(msg, 1, 'x', 4, 't', 'e', 's', 't', 0, 0, 1, 0, 1)
	// A 记录: 203.0.113.9
	msg = append(msg, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 203, 0, 113, 9)
	// AAAA 记录: 2001:db8::1
	msg = append(msg, 0xc0, 0x0c, 0, 28, 0, 1, 0, 0, 0, 60, 0, 16,
		0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1)
	ips := answerIPs(msg)
	if len(ips) != 2 {
		t.Fatalf("answerIPs=%v want 2", ips)
	}
	if ips[0] != "203.0.113.9" || ips[1] != "2001:db8::1" {
		t.Errorf("answerIPs=%v", ips)
	}
	// 畸形包不 panic
	if got := answerIPs([]byte{1, 2, 3}); len(got) != 0 {
		t.Errorf("short packet should yield none, got %v", got)
	}
}

// [D9] DNS-over-TCP 长度前缀封装: 2 字节大端长度 + 报文。
func Test_D9_tcpLengthPrefix(t *testing.T) {
	resp := []byte{0x12, 0x34, 0x56}
	out := make([]byte, 2+len(resp))
	binary.BigEndian.PutUint16(out[:2], uint16(len(resp)))
	copy(out[2:], resp)
	if binary.BigEndian.Uint16(out[:2]) != 3 || out[2] != 0x12 {
		t.Errorf("framing wrong: %v", out)
	}
}
