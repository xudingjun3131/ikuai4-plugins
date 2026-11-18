package api

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// bodyDisplay returns a human-viewable body string.
//
//  1. 按 Content-Encoding 解压: gzip / deflate / br / zstd(支持叠加, 逆序解)
//  2. 二进制内容(含 NUL 或控制字符占比高) -> 输出明确标记 + hex dump,
//     不再把裸字节当字符串渲染成"乱码"(用户实踩: 88 字节的二进制响应
//     显示成一堆不可读字符, 还被误判成"解密失败/加密数据")
//  3. 按 Content-Type 的 charset、HTML <meta> 或 BOM 探测编码并转成 UTF-8
//     (GBK/GB2312/GB18030/Big5/Shift_JIS/Latin-1 等)
//
// 原版只解 gzip 且其余一律 string(body) 按 UTF-8 解释 —— br/deflate/zstd 的响应
// 会显示成压缩二进制乱码, GBK 等非 UTF-8 编码的中文会显示成 "ä¸­æ–‡" 这类乱码。
func bodyDisplay(headers map[string][]string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	decoded := decodeBody(headers, body)
	if isBinary(decoded) {
		return "[二进制内容 " + strconv.Itoa(len(decoded)) + " 字节 — 非文本数据" +
			"(应用层加密/序列化/压缩残留/图片等), 已按 hex 显示]\n" + hexDump(decoded)
	}
	return toUTF8(contentType(headers), decoded)
}

// isBinary 判断 body 是否为二进制(非文本)内容。
// 判据两条, 命中其一即算:
//   - 含 NUL 字节(文本不可能有)
//   - 控制字符(除 	 \n \r)占比 > 10%
//
// 只按 NUL 判会漏掉大量纯高位字节的二进制(如某些加密/序列化载荷),
// 那些内容按 UTF-8 渲染出来就是一片乱码。
func isBinary(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	if bytes.IndexByte(body, 0) >= 0 {
		return true
	}
	ctrl := 0
	for _, b := range body {
		if b < 0x20 && b != '	' && b != '\n' && b != '\r' {
			ctrl++
		}
	}
	return ctrl*100/len(body) > 10
}

// hexDump 生成经典 hexdump: 偏移 + 16 字节 hex(中间分组) + ASCII 栏。
// 最多输出 64 行(1KB), 避免大 body 把详情面板撑爆; 超出部分给一行省略提示。
func hexDump(body []byte) string {
	const maxBytes = 1024
	var sb strings.Builder
	n := len(body)
	if n > maxBytes {
		n = maxBytes
	}
	for off := 0; off < n; off += 16 {
		end := off + 16
		if end > n {
			end = n
		}
		fmt.Fprintf(&sb, "%08x  ", off)
		for i := off; i < off+16; i++ {
			if i < end {
				fmt.Fprintf(&sb, "%02x ", body[i])
			} else {
				sb.WriteString("   ")
			}
			if i == off+7 {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(" |")
		for i := off; i < end; i++ {
			c := body[i]
			if c >= 0x20 && c < 0x7f {
				sb.WriteByte(c)
			} else {
				sb.WriteByte('.')
			}
		}
		sb.WriteString("|\n")
	}
	if n < len(body) {
		fmt.Fprintf(&sb, "... 其余 %d 字节未显示(共 %d 字节)\n", len(body)-n, len(body))
	}
	return sb.String()
}

// contentType 取 Content-Type 头的第一个值(map 的键大小写可能不一, 做不敏感查找)。
func contentType(headers map[string][]string) string {
	for k, vals := range headers {
		if !strings.EqualFold(k, "Content-Type") {
			continue
		}
		if len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

// contentEncodings 按应用顺序返回 Content-Encoding 的各层(如 "gzip, br")。
func contentEncodings(headers map[string][]string) []string {
	var out []string
	for k, vals := range headers {
		if !strings.EqualFold(k, "Content-Encoding") {
			continue
		}
		for _, v := range vals {
			for _, part := range strings.Split(v, ",") {
				if p := strings.TrimSpace(part); p != "" {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// decodeBody 按 Content-Encoding 逆序逐层解压(编码按应用顺序声明, 解码须反过来)。
// 任一层解不动就停在当前层级, 绝不返回空。
func decodeBody(headers map[string][]string, body []byte) []byte {
	encs := contentEncodings(headers)
	if len(encs) == 0 {
		return body
	}
	for i := len(encs) - 1; i >= 0; i-- {
		switch strings.ToLower(encs[i]) {
		case "identity", "none", "":
			continue
		}
		out, err := decodeOne(encs[i], body)
		if err != nil {
			return body
		}
		body = out
	}
	return body
}

func decodeOne(enc string, body []byte) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "gzip", "x-gzip":
		r, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)

	case "deflate":
		// deflate 有两种形态: zlib 包装(RFC 1950)与 raw deflate(RFC 1951),
		// 服务端实现不一, 都试。
		if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
			defer zr.Close()
			if out, err := io.ReadAll(zr); err == nil {
				return out, nil
			}
		}
		return io.ReadAll(flate.NewReader(bytes.NewReader(body)))

	case "br":
		return io.ReadAll(brotli.NewReader(bytes.NewReader(body)))

	case "zstd":
		r, err := zstd.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	return nil, errUnknownEncoding
}

var errUnknownEncoding = &encodingError{"unknown content-encoding"}

type encodingError struct{ s string }

func (e *encodingError) Error() string { return e.s }

// toUTF8 把 body 转成 UTF-8 字符串。
//
// 优先级:
//   - 含 NUL 视为二进制, 原样返回(强转会把二进制搅成不可读字符)
//   - Content-Type 显式声明 charset -> 按它转(GBK/Big5/Latin-1 等都认)
//   - 无 charset 且本身已是合法 UTF-8 -> 原样返回(避免误转换把中文搞花)
//   - 无 charset 且不像 UTF-8 -> 先试 GBK 兜底, 再交给 charset 库嗅探
//     (BOM / HTML <meta>)
func toUTF8(ct string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if bytes.IndexByte(body, 0) >= 0 {
		return string(body)
	}

	_, hasCharset := charsetParam(ct)

	if !hasCharset && utf8.Valid(body) {
		return string(body)
	}

	// charset 库在"猜不出来"时会退到 windows-1252, 那对中文用户基本等于没转。
	// 大量老接口/国产服务就是这么干的: 不发 charset, 内容却是 GBK。
	// 这里加一道保守兜底: 字节形态符合双字节 CJK 规律, 且按 GB18030 解出来
	// 是含 CJK 的合法 UTF-8 时才采用, 避免把真 Latin-1 文本误判。
	if !hasCharset {
		if s, ok := gbkFallback(body); ok {
			return s
		}
	}

	r, err := charset.NewReader(bytes.NewReader(body), ct)
	if err != nil {
		return string(body)
	}
	out, err := io.ReadAll(r)
	if err != nil || len(out) == 0 {
		return string(body)
	}
	return string(out)
}

// gbkFallback 在无 charset 且内容不像 UTF-8 时, 尝试按 GB18030(GBK/GB2312 超集)解。
// 双重保守: 字节形态必须符合双字节 CJK 规律, 且解出来必须含 CJK 字符。
func gbkFallback(body []byte) (string, bool) {
	if !looksLikeDoubleByteCJK(body) {
		return "", false
	}
	out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(body)
	if err != nil || !utf8.Valid(out) || !containsCJK(out) {
		return "", false
	}
	return string(out), true
}

// looksLikeDoubleByteCJK 判断字节形态是否符合 GBK/Big5 这类双字节编码:
// 每个 >=0x81 的前导字节后必须紧跟一个合法尾字节(0x40-0xFE 除去 0x7F)。
// 这样 "café"(0xE9 在末尾无尾字节)之类的真 Latin-1 文本不会被误判。
func looksLikeDoubleByteCJK(b []byte) bool {
	pairs := 0
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			i++
			continue
		}
		if c == 0x80 || c == 0xFF {
			return false
		}
		if i+1 >= len(b) {
			return false
		}
		t := b[i+1]
		if t < 0x40 || t > 0xFE || t == 0x7F {
			return false
		}
		i += 2
		pairs++
	}
	return pairs > 0
}

func containsCJK(b []byte) bool {
	for _, r := range string(b) {
		if (r >= 0x4E00 && r <= 0x9FFF) || // CJK 统一表意
			(r >= 0x3400 && r <= 0x4DBF) || // 扩展 A
			(r >= 0xF900 && r <= 0xFAFF) { // 兼容表意
			return true
		}
	}
	return false
}

// charsetParam 判断 Content-Type 里是否显式带了 charset 参数。
func charsetParam(ct string) (string, bool) {
	if ct == "" {
		return "", false
	}
	if _, params, err := mime.ParseMediaType(ct); err == nil {
		if cs, ok := params["charset"]; ok && cs != "" {
			return cs, true
		}
	}
	return "", false
}
