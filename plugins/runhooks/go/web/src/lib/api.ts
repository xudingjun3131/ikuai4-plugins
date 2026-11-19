import type { FlowDTO, FlowFilter, FlowsResponse } from "@/lib/types"

// [plugin-proxy] <a href> 不经过 fetch/EventSource 包装, 必须在 URL 生成处就带上
// Vite base 前缀(/plugin_proxy/runhooks)。否则真机点「导出 HAR」「下载 CA」会
// 打到网关根路径 /api/... 被 Nginx 404 吃掉, 浏览器只能回退 download 属性存到
// 404 HTML。引擎同时在裸 /api/* 与 /plugin_proxy/runhooks/api/* 两套路径挂载
// (见 internal/api/server.go), 这里统一用带前缀的形式, 不再依赖运行期包装层。
const BASE = (import.meta.env.BASE_URL || "/").replace(/\/+$/, "")
export function apiURL(path: string): string {
  return `${BASE}${path}`
}

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
  if (!filter) return apiURL("/api/flows")
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
  return apiURL(qs ? `/api/flows?${qs}` : "/api/flows")
}

export function eventsURL(): string {
  return apiURL("/api/events")
}

export function caPemURL(): string {
  return apiURL("/api/ca.pem")
}

export function captureURL(): string {
  return apiURL("/api/capture")
}

export function exportHarURL(): string {
  return apiURL("/api/export/har")
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
