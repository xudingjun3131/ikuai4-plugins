# iOS capture status (todo 41)

**Status:** deferred (interface reserved).

## Why deferred

iOS Network Extension / VPN capture needs Apple developer signing, entitlements, and a separate app target. Out of scope for the current personal Linux-first desktop binary.

## Stable local report protocol (for future agent)

When implemented, agents SHOULD POST captures to the desktop/local server:

```
POST /api/report/flows
Content-Type: application/json
Authorization: Bearer <optional local token>

{
  "agentId": "ios-device-uuid",
  "flows": [ /* same shape as GET /api/flows items */ ]
}
```

Until `/api/report` exists (todo 33), use:

1. System HTTP proxy pointing at desktop `proxy-addr` when possible, or
2. Export HAR and import (todo 36).

## Android

Android LAN capture is tracked separately (todo 39). Prefer Android first for personal use.
