package api

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func hdr(kv ...string) map[string][]string {
	h := map[string][]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		h[kv[i]] = []string{kv[i+1]}
	}
	return h
}

func gzipOf(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func brotliOf(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := brotli.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func zstdOf(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := zstd.NewWriter(&b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func zlibOf(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func rawDeflateOf(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := flate.NewWriter(&b, -1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestBodyDisplay_Encodings(t *testing.T) {
	const plain = `{"msg":"中文测试 hello"}`
	cases := []struct {
		name    string
		headers map[string][]string
		body    []byte
		want    string
	}{
		{"无编码-纯UTF8", nil, []byte(plain), plain},
		{"gzip", hdr("Content-Encoding", "gzip"), gzipOf(t, plain), plain},
		{"deflate-zlib", hdr("Content-Encoding", "deflate"), zlibOf(t, plain), plain},
		{"deflate-raw", hdr("Content-Encoding", "deflate"), rawDeflateOf(t, plain), plain},
		{"br", hdr("Content-Encoding", "br"), brotliOf(t, plain), plain},
		{"zstd", hdr("Content-Encoding", "zstd"), zstdOf(t, plain), plain},
		{"identity", hdr("Content-Encoding", "identity"), []byte(plain), plain},
		{
			"叠加-gzip再br",
			hdr("Content-Encoding", "gzip, br"),
			brotliOf(t, string(gzipOf(t, plain))),
			plain,
		},
		{
			"gzip但内容是坏的-原样返回不丢内容",
			hdr("Content-Encoding", "gzip"),
			[]byte("这不是gzip"),
			"这不是gzip",
		},
		{
			"未知编码-原样返回",
			hdr("Content-Encoding", "some-future-codec"),
			[]byte(plain),
			plain,
		},
		{"空body", nil, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := bodyDisplay(c.headers, c.body)
			if got != c.want {
				t.Errorf("bodyDisplay() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBodyDisplay_Charsets(t *testing.T) {
	const cn = "中文乱码测试ABC123"

	gbkBytes, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(cn))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(gbkBytes, []byte{0}) {
		t.Fatal("GBK 编码结果不应含 NUL")
	}

	utf8Bytes := []byte(cn)

	cases := []struct {
		name    string
		headers map[string][]string
		body    []byte
		want    string
	}{
		{
			"GBK+显式charset",
			hdr("Content-Type", "text/html; charset=gbk"),
			gbkBytes,
			cn,
		},
		{
			"GB2312+显式charset",
			hdr("Content-Type", "text/html; charset=gb2312"),
			gbkBytes,
			cn,
		},
		{
			"GBK无charset-靠嗅探也要转对",
			hdr("Content-Type", "text/html"),
			gbkBytes,
			cn,
		},
		{
			"合法UTF8无charset-绝不能被动",
			hdr("Content-Type", "application/json"),
			utf8Bytes,
			cn,
		},
		{
			"无Content-Type且合法UTF8-不能动",
			nil,
			utf8Bytes,
			cn,
		},
		{
			"UTF8+显式charset=utf-8",
			hdr("Content-Type", "application/json; charset=utf-8"),
			utf8Bytes,
			cn,
		},
		{
			"Latin-1显式charset",
			hdr("Content-Type", "text/plain; charset=iso-8859-1"),
			[]byte("caf\xe9"),
			"café",
		},
		{
			// 防误判: 无 charset 的纯 Latin-1 文本(0xE9 在末尾, 没有 GBK 尾字节)
			// 绝不能被你家的 GBK 兜底搅成汉字
			"无charset的Latin-1-不许误判成GBK",
			hdr("Content-Type", "text/plain"),
			[]byte("caf\xe9 and na\xefve r\xe9sum\xe9"),
			"café and naïve résumé",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := bodyDisplay(c.headers, c.body)
			if got != c.want {
				t.Errorf("bodyDisplay() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBodyDisplay_BinaryNotGarbled(t *testing.T) {
	// 含 NUL 的二进制(如图片/压缩残留)必须被识别为二进制并给出 hex dump,
	// 而不是把裸字节当字符串渲染成乱码 —— 旧契约"原样返回"正是乱码的来源。
	bin := []byte{0x89, 'P', 'N', 'G', 0x00, 0x0d, 0x0a, 0x1a, 0x0a, 0xff, 0xfe}
	got := bodyDisplay(hdr("Content-Type", "image/png"), bin)
	if !strings.Contains(got, "[二进制内容 11 字节") {
		t.Errorf("缺少二进制标记: %q", got)
	}
	if !strings.Contains(got, "89 50 4e 47 00 0d 0a 1a  0a ff fe") {
		t.Errorf("hex dump 不含原始字节: %q", got)
	}
	if got == string(bin) {
		t.Errorf("仍在返回裸字节(会渲染成乱码): %q", got)
	}
}

func TestBodyDisplay_BinaryHighBytes(t *testing.T) {
	// 纯高位字节、无 NUL 的二进制(加密/序列化载荷常见形态)也必须识别出来
	bin := []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04, 0x99, 0x88, 0x77, 0x66}
	got := bodyDisplay(hdr("Content-Type", "application/octet-stream"), bin)
	if !strings.Contains(got, "[二进制内容 12 字节") {
		t.Errorf("高位字节二进制未被识别: %q", got)
	}
	if !strings.Contains(got, "de ad be ef 01 02 03 04") {
		t.Errorf("hex dump 不含原始字节: %q", got)
	}
}

func TestBodyDisplay_BinaryHexDumpTruncates(t *testing.T) {
	// 超过 1KB 的二进制要截断并给省略提示, 不能把详情面板撑爆
	big := bytes.Repeat([]byte{0x00, 0xff}, 2048)
	got := bodyDisplay(hdr("Content-Type", "application/octet-stream"), big)
	if !strings.Contains(got, "其余 3072 字节未显示(共 4096 字节)") {
		t.Errorf("缺少截断提示: %q", got[:200])
	}
}

func TestBodyDisplay_TextNotHexDumped(t *testing.T) {
	// 普通文本绝不能走 hex dump 分支
	got := bodyDisplay(hdr("Content-Type", "text/plain; charset=utf-8"), []byte("hello 世界"))
	if strings.Contains(got, "二进制内容") {
		t.Errorf("文本被误判成二进制: %q", got)
	}
	if got != "hello 世界" {
		t.Errorf("文本内容被改动: %q", got)
	}
}

func TestBodyDisplay_RealWorldHTML(t *testing.T) {
	// 真实形态: GBK 网页 + meta 声明 + gzip
	html := `<html><head><meta charset="gbk"></head><body>中文网页内容</body></html>`
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	got := bodyDisplay(
		hdr("Content-Type", "text/html", "Content-Encoding", "gzip"),
		gzipOf(t, string(gbk)),
	)
	if !strings.Contains(got, "中文网页内容") {
		t.Errorf("GBK+gzip 网页未正确还原: %q", got)
	}
	if strings.Contains(got, "ä¸") {
		t.Errorf("仍存在 mojibake: %q", got)
	}
}
