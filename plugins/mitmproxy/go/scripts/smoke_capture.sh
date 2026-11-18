#!/usr/bin/env bash
# smoke_capture.sh — start bin, curl HTTP+HTTPS through MITM, assert capture via go test List().
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BIN="${BIN:-$ROOT/bin/proxying}"
EVIDENCE_DIR="${EVIDENCE_DIR:-$ROOT/.omo/evidence}"
OUT="${OUT:-$EVIDENCE_DIR/task-7-reqable-like-proxy.sh.out}"
mkdir -p "$EVIDENCE_DIR"

TMP="$(mktemp -d)"
cleanup_all() {
  if [[ -n "${PID:-}" ]] && kill -0 "$PID" 2>/dev/null; then
    kill -TERM "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  if [[ -n "${ORIGIN_PID:-}" ]] && kill -0 "$ORIGIN_PID" 2>/dev/null; then
    kill -TERM "$ORIGIN_PID" 2>/dev/null || true
    wait "$ORIGIN_PID" 2>/dev/null || true
  fi
  if [[ -n "${TLS_PID:-}" ]] && kill -0 "$TLS_PID" 2>/dev/null; then
    kill -TERM "$TLS_PID" 2>/dev/null || true
    wait "$TLS_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP"
}
trap cleanup_all EXIT

DATA="$TMP/data"
mkdir -p "$DATA"

pick_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

PROXY_PORT="$(pick_port)"
API_PORT="$(pick_port)"
PROXY_ADDR="127.0.0.1:${PROXY_PORT}"
API_ADDR="127.0.0.1:${API_PORT}"

exec > >(tee "$OUT") 2>&1

echo "=== task-7 smoke_capture evidence ==="
echo "date: $(date -Iseconds)"
echo "cwd: $ROOT"
echo "proxy: $PROXY_ADDR api: $API_ADDR data: $DATA"
echo

echo "=== go test ./... ==="
go test ./...
echo "exit: $?"
echo

echo "=== go build -o bin/proxying ./cmd/proxying ==="
go build -o "$BIN" ./cmd/proxying
echo "exit: $?"
echo

echo "=== go test ./internal/app -run Test_App_capture (List >=1 HTTP+HTTPS) ==="
go test ./internal/app -count=1 -run 'Test_App_capture_HTTP_and_HTTPS_via_List' -v
echo "exit: $?"
echo

LOG="$TMP/proxying.log"
echo "=== start $BIN ==="
"$BIN" \
  -proxy-addr "$PROXY_ADDR" \
  -api-addr "$API_ADDR" \
  -data-dir "$DATA" \
  >"$LOG" 2>&1 &
PID=$!
echo "pid: $PID"

for _ in $(seq 1 50); do
  if curl -sf "http://${API_ADDR}/api/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.05
done

echo
echo "=== curl GET health ==="
HEALTH="$(curl -sf "http://${API_ADDR}/api/health")"
echo "$HEALTH"
echo "$HEALTH" | grep -q '"ok":true'
echo "PASS: health ok"
echo

ORIGIN_PORT="$(pick_port)"
ORIGIN_ADDR="127.0.0.1:${ORIGIN_PORT}"
python3 - "$ORIGIN_PORT" <<'PY' &
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

port = int(sys.argv[1])

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/smoke-http":
            self.send_response(404)
            self.end_headers()
            return
        body = b"smoke-http-ok"
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass

HTTPServer(("127.0.0.1", port), H).serve_forever()
PY
ORIGIN_PID=$!
sleep 0.15

echo "=== curl HTTP via proxy ==="
HTTP_BODY="$(curl -sf -x "http://${PROXY_ADDR}" "http://${ORIGIN_ADDR}/smoke-http")"
echo "body: $HTTP_BODY"
test "$HTTP_BODY" = "smoke-http-ok"
echo "PASS: HTTP via proxy"
echo

TLS_DIR="$TMP/tls"
mkdir -p "$TLS_DIR"
openssl req -x509 -newkey rsa:2048 -keyout "$TLS_DIR/key.pem" -out "$TLS_DIR/cert.pem" \
  -days 1 -nodes -subj "/CN=127.0.0.1" 2>/dev/null

TLS_PORT="$(pick_port)"
TLS_ADDR="127.0.0.1:${TLS_PORT}"
python3 - "$TLS_PORT" "$TLS_DIR/cert.pem" "$TLS_DIR/key.pem" <<'PY' &
import ssl
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

port = int(sys.argv[1])
cert, key = sys.argv[2], sys.argv[3]

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/smoke-https":
            self.send_response(404)
            self.end_headers()
            return
        body = b"smoke-https-ok"
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass

httpd = HTTPServer(("127.0.0.1", port), H)
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain(cert, key)
httpd.socket = ctx.wrap_socket(httpd.socket, server_side=True)
httpd.serve_forever()
PY
TLS_PID=$!
sleep 0.25

CA_PEM="$DATA/ca.pem"
test -f "$CA_PEM"
echo "=== curl HTTPS via proxy --cacert $CA_PEM ==="
HTTPS_BODY="$(curl -sf -x "http://${PROXY_ADDR}" --cacert "$CA_PEM" "https://${TLS_ADDR}/smoke-https")"
echo "body: $HTTPS_BODY"
test "$HTTPS_BODY" = "smoke-https-ok"
echo "PASS: HTTPS MITM via proxy with local CA"
echo

kill -TERM "$ORIGIN_PID" "$TLS_PID" 2>/dev/null || true
wait "$ORIGIN_PID" 2>/dev/null || true
wait "$TLS_PID" 2>/dev/null || true
ORIGIN_PID=""
TLS_PID=""

echo "=== SIGTERM binary ==="
kill -TERM "$PID"
wait "$PID"
EC=$?
echo "process_exit: $EC"
test "$EC" -eq 0
PID=""
echo "PASS: clean shutdown"
echo

echo "=== server log ==="
cat "$LOG"
echo

echo "=== ALL ACCEPTANCE PASSED ==="
echo "Notes: flow List() >=1 HTTP+HTTPS asserted by go test ./internal/app"
echo "Binary smoke: health + HTTP proxy + HTTPS MITM --cacert ca.pem"
echo "evidence: $OUT"
