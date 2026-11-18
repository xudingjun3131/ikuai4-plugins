import type { FlowDTO, FlowFilter, FlowsResponse } from "@/lib/types"

/** Same-origin control API (embedded SPA or Vite proxy). */
function statusQueryValue(raw: string): string | null {
  const s = raw.trim()
  if (!s) return null
  if (!/^\d{3}$/.test(s)) return null
  const n = Number(s)
  if (n < 100 || n > 599) return null
  return s
}

export function flowsURL(filter?: FlowFilter): string {
  if (!filter) return "/api/flows"
  const params = new URLSearchParams()
  if (filter.host.trim()) params.set("host", filter.host.trim())
  if (filter.method.trim()) params.set("method", filter.method.trim())
  const status = statusQueryValue(filter.status)
  if (status !== null) params.set("status", status)
  if (filter.q.trim()) params.set("q", filter.q.trim())
  if (filter.op === "or") params.set("op", "or")
  const conds = filter.conds
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean)
  for (const c of conds) params.append("cond", c)
  const qs = params.toString()
  return qs ? `/api/flows?${qs}` : "/api/flows"
}

export function eventsURL(): string {
  return "/api/events"
}

export function caPemURL(): string {
  return "/api/ca.pem"
}

export function captureURL(): string {
  return "/api/capture"
}

export function exportHarURL(): string {
  return "/api/export/har"
}

export async function fetchCapturePaused(
  signal?: AbortSignal,
): Promise<boolean> {
  const res = await fetch(captureURL(), { signal })
  if (!res.ok) {
    throw new Error(`GET /api/capture failed: ${res.status}`)
  }
  const body = (await res.json()) as { paused?: boolean }
  return Boolean(body.paused)
}

export async function setCapturePaused(paused: boolean): Promise<boolean> {
  const res = await fetch(captureURL(), {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ paused }),
  })
  if (!res.ok) {
    throw new Error(`PUT /api/capture failed: ${res.status}`)
  }
  const body = (await res.json()) as { paused?: boolean }
  return Boolean(body.paused)
}

export async function clearFlows(): Promise<void> {
  const res = await fetch(flowsURL(), { method: "DELETE" })
  if (!res.ok) {
    throw new Error(`DELETE /api/flows failed: ${res.status}`)
  }
}

export async function fetchFlows(
  signal?: AbortSignal,
  filter?: FlowFilter,
): Promise<readonly FlowDTO[]> {
  const res = await fetch(flowsURL(filter), { signal })
  if (!res.ok) {
    throw new Error(`GET /api/flows failed: ${res.status}`)
  }
  const body = (await res.json()) as FlowsResponse
  return body.flows ?? []
}

export function flowIsTruncated(f: FlowDTO): boolean {
  return Boolean(f.request.truncated || f.response.truncated)
}

export function flowHost(f: FlowDTO): string {
  const raw = f.request.url ?? ""
  try {
    return new URL(raw).host || raw
  } catch {
    return raw
  }
}

export function flowPath(f: FlowDTO): string {
  const raw = f.request.url ?? ""
  try {
    const u = new URL(raw)
    return `${u.pathname}${u.search}`
  } catch {
    return raw
  }
}
