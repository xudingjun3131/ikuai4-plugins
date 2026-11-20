package httpx_test

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"testing"

	"github.com/andybalholm/brotli"

	"github.com/GitterBois/runhooks/internal/httpx"
)

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func zlibBytes(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func rawDeflateBytes(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw, err := flate.NewWriter(&b, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func brBytes(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	bw := brotli.NewWriterLevel(&b, 4)
	if _, err := bw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decodeGzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("result is not gzip: %v", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func decodeBr(t *testing.T, b []byte) string {
	t.Helper()
	out, err := io.ReadAll(brotli.NewReader(bytes.NewReader(b)))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func decodeZlib(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("result is not zlib: %v", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func decodeRawDeflate(t *testing.T, b []byte) string {
	t.Helper()
	fr := flate.NewReader(bytes.NewReader(b))
	defer fr.Close()
	out, err := io.ReadAll(fr)
	if err != nil {
		t.Fatalf("result is not raw deflate: %v", err)
	}
	return string(out)
}

// v1.1.7 gap: plaintext replace against gzip bytes used to be a silent no-op.
func TestReplaceContent_Gzip(t *testing.T) {
	orig := gzipBytes(t, "HELLO-ORIGINAL-BODY path=/case1")
	got, err := httpx.ReplaceContent(orig, "gzip", "ORIGINAL", "PATCHED")
	if err != nil {
		t.Fatal(err)
	}
	if decodeGzip(t, got) != "HELLO-PATCHED-BODY path=/case1" {
		t.Fatalf("gzip rewrite wrong: %q", decodeGzip(t, got))
	}
}

func TestReplaceContent_GzipWholeBody(t *testing.T) {
	orig := gzipBytes(t, "anything-at-all")
	got, err := httpx.ReplaceContent(orig, "gzip", "", "ENTIRELY-FABRICATED")
	if err != nil {
		t.Fatal(err)
	}
	if decodeGzip(t, got) != "ENTIRELY-FABRICATED" {
		t.Fatalf("whole-body rewrite wrong: %q", decodeGzip(t, got))
	}
}

func TestReplaceContent_Brotli(t *testing.T) {
	orig := brBytes(t, "HELLO-ORIGINAL-BR")
	got, err := httpx.ReplaceContent(orig, "br", "ORIGINAL", "PATCHED")
	if err != nil {
		t.Fatal(err)
	}
	if decodeBr(t, got) != "HELLO-PATCHED-BR" {
		t.Fatalf("br rewrite wrong: %q", decodeBr(t, got))
	}
}

func TestReplaceContent_DeflateZlib(t *testing.T) {
	orig := zlibBytes(t, "HELLO-ORIGINAL-DEFLATE")
	got, err := httpx.ReplaceContent(orig, "deflate", "ORIGINAL", "PATCHED")
	if err != nil {
		t.Fatal(err)
	}
	if decodeZlib(t, got) != "HELLO-PATCHED-DEFLATE" {
		t.Fatalf("deflate(zlib) rewrite wrong: %q", decodeZlib(t, got))
	}
}

func TestReplaceContent_DeflateRaw(t *testing.T) {
	orig := rawDeflateBytes(t, "HELLO-ORIGINAL-RAW")
	got, err := httpx.ReplaceContent(orig, "deflate", "ORIGINAL", "PATCHED")
	if err != nil {
		t.Fatal(err)
	}
	if decodeRawDeflate(t, got) != "HELLO-PATCHED-RAW" {
		t.Fatalf("deflate(raw) rewrite wrong: %q", decodeRawDeflate(t, got))
	}
}

func TestReplaceContent_IdentityAndEmpty(t *testing.T) {
	for _, enc := range []string{"", "identity", "IDENTITY"} {
		got, err := httpx.ReplaceContent([]byte("a-b-a"), enc, "b", "c")
		if err != nil {
			t.Fatalf("enc=%q: %v", enc, err)
		}
		if string(got) != "a-c-a" {
			t.Fatalf("enc=%q got %q", enc, got)
		}
	}
}

func TestReplaceContent_StackedCodings(t *testing.T) {
	// "gzip, br" = gzip applied first, then br (RFC: order in which applied).
	// Decode peels br first, then gzip; re-encode applies gzip then br again.
	inner := gzipBytes(t, "STACK-ORIGINAL")
	orig := brBytes(t, string(inner))
	got, err := httpx.ReplaceContent(orig, "gzip, br", "ORIGINAL", "PATCHED")
	if err != nil {
		t.Fatal(err)
	}
	brOut := decodeBr(t, got)
	if decodeGzip(t, []byte(brOut)) != "STACK-PATCHED" {
		t.Fatalf("stacked rewrite wrong: %q", decodeGzip(t, []byte(brOut)))
	}
}

func TestReplaceContent_UnknownEncodingFailsLoudly(t *testing.T) {
	if _, err := httpx.ReplaceContent([]byte("x"), "zstd", "a", "b"); err == nil {
		t.Fatal("unknown encoding must fail loudly, not silently skip")
	}
}

func TestReplaceContent_CorruptStreamFailsLoudly(t *testing.T) {
	if _, err := httpx.ReplaceContent([]byte("not-gzip"), "gzip", "a", "b"); err == nil {
		t.Fatal("corrupt gzip stream must fail loudly")
	}
}

func TestNormalizeBodyHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Length", "9999")
	h.Set("ETag", `W/"old"`)
	h.Set("Content-MD5", "old")
	h.Set("Digest", "sha-256=old")
	h.Set("Transfer-Encoding", "chunked")
	httpx.NormalizeBodyHeaders(h, 7)
	if h.Get("Content-Length") != "7" {
		t.Fatalf("Content-Length not recomputed: %q", h.Get("Content-Length"))
	}
	for _, k := range []string{"ETag", "Content-MD5", "Digest", "Transfer-Encoding"} {
		if h.Get(k) != "" {
			t.Fatalf("stale %s header survived: %q", k, h.Get(k))
		}
	}
	h.Set("Content-Encoding", "gzip")
	httpx.StripContentEncoding(h)
	if h.Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding not stripped: %q", h.Get("Content-Encoding"))
	}
}
