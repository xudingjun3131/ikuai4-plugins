#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
echo "parity: go test"
go test ./...
echo "parity: build"
go build -o bin/proxying ./cmd/proxying
echo "parity: health smoke"
tmpdir=$(mktemp -d)
./bin/proxying -data-dir "$tmpdir" -api-addr 127.0.0.1:19090 -proxy-addr 127.0.0.1:18080 &
pid=$!
trap 'kill $pid 2>/dev/null || true; rm -rf "$tmpdir"' EXIT
sleep 0.4
curl -sf http://127.0.0.1:19090/api/health | grep -q '"ok":true'
curl -sf http://127.0.0.1:19090/api/flows | grep -q flows
curl -sf http://127.0.0.1:19090/api/scripts/templates | grep -q templates || true
curl -sf http://127.0.0.1:19090/api/sessions | grep -q sessions || true
curl -sf http://127.0.0.1:19090/api/mcp/tools | grep -q list_flows || true
echo "parity: OK (IN matrix core endpoints)"
