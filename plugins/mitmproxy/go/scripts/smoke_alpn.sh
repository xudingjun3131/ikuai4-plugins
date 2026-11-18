#!/usr/bin/env bash
# smoke_alpn.sh — Browser/ALPN HTTPS gate (path B: Go client TLS via MITM).
# Fails if only plain H1 exists without negotiated-protocol evidence.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

EVIDENCE_DIR="${EVIDENCE_DIR:-$ROOT/.omo/evidence}"
OUT="${OUT:-$EVIDENCE_DIR/task-8-reqable-like-proxy.txt}"
mkdir -p "$EVIDENCE_DIR"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
LOG="$TMP/alpn_test.log"

{
  echo "=== task-8 Browser/ALPN HTTPS gate ==="
  echo "date: $(date -Iseconds)"
  echo "cwd: $ROOT"
  echo "path: B (Go client TLS NextProtos h2/http1.1 via CONNECT MITM)"
  echo "path_A: skipped (no headless Chrome required)"
  echo

  echo "=== go test -tags=alpn ./internal/proxy -run Test_Engine_ALPN ==="
  set +e
  go test -tags=alpn ./internal/proxy -count=1 -run 'Test_Engine_ALPN' -v 2>&1 | tee "$LOG"
  EC=${PIPESTATUS[0]}
  set -e
  echo "exit: $EC"
  if [[ "$EC" -ne 0 ]]; then
    echo "FAIL: alpn-tagged go test"
    exit "$EC"
  fi
  echo

  PROTO="$(grep -oE 'ALPN_NEGOTIATED=[^[:space:]]+' "$LOG" | tail -1 | cut -d= -f2- || true)"
  if [[ -z "$PROTO" ]]; then
    echo "FAIL: no ALPN_NEGOTIATED= in test log (need TLS protocol evidence)"
    exit 1
  fi

  echo "=== negotiated protocol ==="
  echo "NegotiatedProtocol: $PROTO"
  echo "ALPN_PATH: B"
  echo "PASS: TLS ALPN evidence observed through MITM"
  echo
  echo "=== ALL ACCEPTANCE PASSED ==="
  echo "Notes: path B — Go http.Client + NextProtos [h2, http/1.1] via goproxy MITM"
  echo "goproxy is H1-only today; non-empty protocol (typically http/1.1) is the gate"
  echo "evidence: $OUT"
} | tee "$OUT"
