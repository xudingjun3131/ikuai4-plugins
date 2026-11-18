# Android stub (todo 39)

Minimal guidance for posting captures to a personal proxying server.

## MITM path

1. Download CA: `curl http://<host>:9090/api/ca.pem`
2. Install as user CA on the device/emulator.
3. Set Wi‑Fi proxy to `<host>:8080`.

## Report path

```bash
curl -X POST "http://<host>:9090/api/report" \
  -H "Content-Type: application/json" \
  -H "X-Report-Token: $TOKEN" \
  -d '{"agentId":"emu1","method":"GET","url":"https://example.com/","status":200}'
```

See `ReportClient.kt.example` for a sketch.
