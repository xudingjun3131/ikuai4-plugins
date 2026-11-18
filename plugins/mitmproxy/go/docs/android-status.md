# Android capture MVP (todo 39)

**Status:** stub project + protocol docs (no full Android Studio app in this repo).

## Goal

Phone on same LAN posts captures to **this** personal server — not a cloud registry.

## Options

1. **System / app proxy** → `http://<lan-ip>:8080` with user CA installed (`GET /api/ca.pem`).
2. **Report ingest** → `POST /api/report` with optional `X-Report-Token` (see report package).

## Report payload

```json
{
  "agentId": "pixel-7",
  "agentName": "lab-phone",
  "method": "GET",
  "url": "https://api.example.com/v1",
  "status": 200,
  "headers": {"user-agent": "..."},
  "body": "",
  "scheme": "https"
}
```

Response `201` → flow appears in capture list tagged `report` + `agent:<id>`.

Agents: `GET /api/agents` (todo 40) — local list only, **no cloud device registry**.

## Stub layout

```
mobile/android-stub/
  README.md
  ReportClient.kt.example
```

## Limits

- No rewrite / breakpoint on the mobile stub itself.
- User must trust the local CA for MITM path.
- Bind API to LAN only when intentional (`-api-addr 0.0.0.0:9090`); default remains loopback.
