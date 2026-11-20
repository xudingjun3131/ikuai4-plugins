// Package httpx holds HTTP body helpers shared by the rewrite slots
// (rules / script / breakpoint). The v1.1.7 test report §3.6 flagged that a
// body_replace on a gzip/br response silently no-ops (matching plaintext
// against compressed bytes), and that Content-Length / Content-Encoding go
// stale after a rewrite. Previously this was dodged by a builtin rule forcing
// Accept-Encoding: identity — an upstream that ignores it still broke.
//
// Fix: decode to plaintext using the declared Content-Encoding, apply the
// replacement, then re-encode with the SAME codings and normalise the headers.
// Unknown codings / corrupt streams return an error so the caller can keep the
// original body instead of forwarding garbage.
package httpx

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
)

type coding struct {
	name string
	raw  bool // deflate without the zlib wrapper
}

func parseCodings(header string) ([]coding, error) {
	var out []coding
	for _, part := range strings.Split(header, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		switch name {
		case "", "identity":
			continue
		case "gzip", "x-gzip":
			out = append(out, coding{name: "gzip"})
		case "br":
			out = append(out, coding{name: "br"})
		case "deflate":
			out = append(out, coding{name: "deflate"})
		default:
			return nil, fmt.Errorf("unsupported content-encoding %q", name)
		}
	}
	return out, nil
}

// decodeOne returns plaintext plus the coding actually seen (deflate may be
// raw rather than zlib-wrapped; remember that so re-encoding stays compatible).
func decodeOne(b []byte, c coding) ([]byte, coding, error) {
	switch c.name {
	case "gzip":
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, c, err
		}
		defer zr.Close()
		out, err := io.ReadAll(zr)
		return out, c, err
	case "br":
		out, err := io.ReadAll(brotli.NewReader(bytes.NewReader(b)))
		return out, c, err
	case "deflate":
		if zr, err := zlib.NewReader(bytes.NewReader(b)); err == nil {
			defer zr.Close()
			out, err := io.ReadAll(zr)
			return out, c, err
		}
		fr := flate.NewReader(bytes.NewReader(b))
		defer fr.Close()
		out, err := io.ReadAll(fr)
		if err != nil {
			return nil, c, err
		}
		return out, coding{name: "deflate", raw: true}, nil
	}
	return b, c, nil
}

func encodeOne(b []byte, c coding) ([]byte, error) {
	var buf bytes.Buffer
	switch c.name {
	case "gzip":
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(b); err != nil {
			_ = zw.Close()
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
	case "br":
		bw := brotli.NewWriterLevel(&buf, 4)
		if _, err := bw.Write(b); err != nil {
			_ = bw.Close()
			return nil, err
		}
		if err := bw.Close(); err != nil {
			return nil, err
		}
	case "deflate":
		var w io.WriteCloser
		if c.raw {
			fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
			if err != nil {
				return nil, err
			}
			w = fw
		} else {
			w = zlib.NewWriter(&buf)
		}
		if _, err := w.Write(b); err != nil {
			_ = w.Close()
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	default:
		return b, nil
	}
	return buf.Bytes(), nil
}

// ReplaceContent applies `from` -> `to` (empty from = whole-body replacement)
// to b, honouring the Content-Encoding header, and returns bytes encoded the
// same way. It never mutates the caller's slice.
func ReplaceContent(b []byte, encHeader, from, to string) ([]byte, error) {
	codings, err := parseCodings(encHeader)
	if err != nil {
		return nil, err
	}
	plain := b
	resolved := make([]coding, len(codings))
	// Encodings were applied left-to-right upstream; peel them right-to-left.
	for i := len(codings) - 1; i >= 0; i-- {
		out, got, err := decodeOne(plain, codings[i])
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", codings[i].name, err)
		}
		plain = out
		resolved[i] = got
	}
	var nb []byte
	if from == "" {
		nb = []byte(to)
	} else {
		nb = bytes.ReplaceAll(plain, []byte(from), []byte(to))
	}
	encoded := nb
	for i := 0; i < len(resolved); i++ {
		out, err := encodeOne(encoded, resolved[i])
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", resolved[i].name, err)
		}
		encoded = out
	}
	return encoded, nil
}

// NormalizeBodyHeaders fixes Content-Length after a body rewrite and drops
// headers that would otherwise validate against the OLD body (ETag, digests,
// chunked framing). Call it whenever resp.Body / req.Body is replaced.
func NormalizeBodyHeaders(h http.Header, n int) {
	h.Set("Content-Length", strconv.Itoa(n))
	h.Del("Transfer-Encoding")
	h.Del("ETag")
	h.Del("Content-MD5")
	h.Del("Digest")
}

// StripContentEncoding is for edits where the operator supplied a PLAIN body
// (breakpoint resume / script sandbox): keeping a gzip/br label would make the
// client try to inflate plaintext.
func StripContentEncoding(h http.Header) {
	h.Del("Content-Encoding")
}
