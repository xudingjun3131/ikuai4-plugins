package proxy

// JA3 TLS 客户端指纹：对 ClientHello 的
// SSLVersion,Ciphers,Extensions,SupportedGroups,ECPointFormats 取 MD5。
// 用于按"哪个客户端库/App 发起的连接"匹配（透明路径可算，显式代理路径不可算）。

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
)

// ja3Fingerprint 解析 ClientHello 明文（不含 TLS 记录头），返回 JA3 md5。
// 解析失败返回 ""。
func ja3Fingerprint(hello []byte) string {
	// handshake: type(1) len(3) | version(2) random(32) sid_len(1) sid | cs_len(2) cs | comp_len(1) comp | ext_len(2) ext
	if len(hello) < 4 || hello[0] != 0x01 {
		return ""
	}
	hsLen := int(hello[1])<<16 | int(hello[2])<<8 | int(hello[3])
	body := hello[4:]
	if hsLen < len(body) {
		body = body[:hsLen]
	}
	if len(body) < 2+32+1 {
		return ""
	}
	ver := int(binary.BigEndian.Uint16(body[0:2]))
	off := 2 + 32
	if off >= len(body) {
		return ""
	}
	sidLen := int(body[off])
	off += 1 + sidLen
	if off+2 > len(body) {
		return ""
	}
	csLen := int(binary.BigEndian.Uint16(body[off : off+2]))
	off += 2
	if off+csLen > len(body) {
		return ""
	}
	var ciphers []string
	for i := 0; i+2 <= csLen; i += 2 {
		c := binary.BigEndian.Uint16(body[off+i : off+i+2])
		if isGREASE(c) {
			continue
		}
		ciphers = append(ciphers, strconv.Itoa(int(c)))
	}
	off += csLen
	if off+1 > len(body) {
		return ""
	}
	compLen := int(body[off])
	off += 1 + compLen
	if off+2 > len(body) {
		return ""
	}
	extLen := int(binary.BigEndian.Uint16(body[off : off+2]))
	off += 2
	if off+extLen > len(body) {
		return ""
	}
	extEnd := off + extLen

	var exts, groups, formats []string
	for off+4 <= extEnd {
		et := binary.BigEndian.Uint16(body[off : off+2])
		el := int(binary.BigEndian.Uint16(body[off+2 : off+4]))
		off += 4
		if off+el > extEnd {
			break
		}
		data := body[off : off+el]
		off += el
		if isGREASE(et) {
			continue
		}
		exts = append(exts, strconv.Itoa(int(et)))
		switch et {
		case 0x000a: // supported_groups: 2字节列表长度前缀 + 2字节一组
			if len(data) < 2 {
				break
			}
			list := data[2:]
			for i := 0; i+2 <= len(list); i += 2 {
				g := binary.BigEndian.Uint16(list[i : i+2])
				if !isGREASE(g) {
					groups = append(groups, strconv.Itoa(int(g)))
				}
			}
		case 0x000b: // ec_point_formats: 1字节列表长度前缀 + 1字节一个
			if len(data) < 1 {
				break
			}
			list := data[1:]
			for i := 0; i < len(list); i++ {
				formats = append(formats, strconv.Itoa(int(list[i])))
			}
		}
	}

	ja3 := fmt.Sprintf("%d,%s,%s,%s,%s", ver,
		joinDash(ciphers), joinDash(exts), joinDash(groups), joinDash(formats))
	sum := md5.Sum([]byte(ja3))
	return hex.EncodeToString(sum[:])
}

func joinDash(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += "-"
		}
		out += v
	}
	return out
}

// isGREASE 排除 GREASE 保留值（0x0a0a/0x1a1a/.../0xfafa：
// 两个字节的低 4 位都是 0xa，且高 4 位相同）。
func isGREASE(v uint16) bool {
	return v&0x0f0f == 0x0a0a && (v>>12) == ((v>>4)&0x0f)
}
