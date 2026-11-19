import type { FlowDTO } from "@/lib/types"

/** 观测台的数据模型: 全部由 /api/flows 在浏览器侧聚合, 不新增后端接口。 */

export const BUCKETS = 24

export type DeviceStat = {
  addr: string
  short: string
  count: number
  bytes: number
  errors: number
  topHost: string
  lastAt: number
  spark: number[]
}

export type Bucket = { t: number; label: string; count: number; bytes: number }

export type HostStat = {
  host: string
  count: number
  bytes: number
  errors: number
  avgMs: number
}

export type StatusMix = {
  ok: number
  redirect: number
  clientErr: number
  serverErr: number
  pending: number
}

export type Stats = {
  total: number
  bytes: number
  devices: DeviceStat[]
  buckets: Bucket[]
  hosts: HostStat[]
  status: StatusMix
  methods: { name: string; count: number }[]
  errorRate: number
  from: number
  to: number
  spanMs: number
}

function ts(s?: string): number {
  if (!s) return 0
  const n = Date.parse(s)
  return Number.isNaN(n) ? 0 : n
}

export function bytesOf(f: FlowDTO): number {
  return (f.request.size ?? 0) + (f.response.size ?? 0)
}

export function durationMs(f: FlowDTO): number {
  const a = ts(f.startedAt)
  const b = ts(f.doneAt)
  if (!a || !b) return 0
  return Math.max(0, b - a)
}

export function isError(f: FlowDTO): boolean {
  const s = f.response.status ?? 0
  return Boolean(f.error) || s >= 400 || f.state === "error"
}

export function hostOf(f: FlowDTO): string {
  const raw = f.request.url ?? ""
  try {
    return new URL(raw).host || raw
  } catch {
    return raw
  }
}

/** clientAddr 形如 192.168.1.23:54321, 去掉端口作为「设备」身份。 */
export function shortAddr(addr: string): string {
  if (!addr) return "未知设备"
  if (addr.startsWith("[")) {
    const i = addr.indexOf("]")
    return i > 0 ? addr.slice(0, i + 1) : addr
  }
  const idx = addr.lastIndexOf(":")
  return idx > 0 && /^\d+$/.test(addr.slice(idx + 1)) ? addr.slice(0, idx) : addr
}

function clock(t: number): string {
  const d = new Date(t)
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`
}

type DeviceAcc = {
  addr: string
  count: number
  bytes: number
  errors: number
  lastAt: number
  hosts: Map<string, number>
  spark: number[]
}

type HostAcc = {
  count: number
  bytes: number
  errors: number
  totalMs: number
  done: number
}

export function buildStats(flows: readonly FlowDTO[], bucketCount = BUCKETS): Stats {
  const times = flows.map((f) => ts(f.startedAt)).filter((t) => t > 0)
  const to = times.length ? Math.max(...times) : Date.now()
  const min = times.length ? Math.min(...times) : to
  // 至少 5 分钟的观测窗口, 避免只有两三条流时时间轴被压成一根柱子
  const spanMs = Math.max(to - min, 5 * 60_000)
  const from = to - spanMs
  const step = spanMs / bucketCount

  const buckets: Bucket[] = Array.from({ length: bucketCount }, (_, i) => {
    const t = from + i * step
    return { t, label: clock(t), count: 0, bytes: 0 }
  })

  const devMap = new Map<string, DeviceAcc>()
  const hostMap = new Map<string, HostAcc>()
  const methodMap = new Map<string, number>()
  const status: StatusMix = { ok: 0, redirect: 0, clientErr: 0, serverErr: 0, pending: 0 }
  let bytes = 0
  let errors = 0

  for (const f of flows) {
    const t = ts(f.startedAt)
    const b = bytesOf(f)
    const host = hostOf(f) || "—"
    const err = isError(f)
    const short = shortAddr(f.clientAddr ?? "")
    bytes += b
    if (err) errors += 1

    if (t > 0) {
      const idx = Math.min(bucketCount - 1, Math.max(0, Math.floor((t - from) / step)))
      buckets[idx]!.count += 1
      buckets[idx]!.bytes += b
    }

    const d = devMap.get(short) ?? {
      addr: f.clientAddr ?? short,
      count: 0,
      bytes: 0,
      errors: 0,
      lastAt: 0,
      hosts: new Map<string, number>(),
      spark: Array.from({ length: bucketCount }, () => 0),
    }
    d.count += 1
    d.bytes += b
    if (err) d.errors += 1
    if (t > d.lastAt) d.lastAt = t
    d.hosts.set(host, (d.hosts.get(host) ?? 0) + 1)
    if (t > 0) {
      const idx = Math.min(bucketCount - 1, Math.max(0, Math.floor((t - from) / step)))
      d.spark[idx] = (d.spark[idx] ?? 0) + 1
    }
    devMap.set(short, d)

    const h = hostMap.get(host) ?? { count: 0, bytes: 0, errors: 0, totalMs: 0, done: 0 }
    h.count += 1
    h.bytes += b
    if (err) h.errors += 1
    const ms = durationMs(f)
    if (ms > 0) {
      h.totalMs += ms
      h.done += 1
    }
    hostMap.set(host, h)

    const m = (f.request.method ?? "—").toUpperCase()
    methodMap.set(m, (methodMap.get(m) ?? 0) + 1)

    const s = f.response.status ?? 0
    if (s >= 500) status.serverErr += 1
    else if (s >= 400) status.clientErr += 1
    else if (s >= 300) status.redirect += 1
    else if (s >= 200) status.ok += 1
    else status.pending += 1
  }

  const devices: DeviceStat[] = [...devMap.entries()]
    .map(([short, d]) => {
      let topHost = "—"
      let topN = 0
      for (const [h, n] of d.hosts) {
        if (n > topN) {
          topN = n
          topHost = h
        }
      }
      return {
        addr: d.addr,
        short,
        count: d.count,
        bytes: d.bytes,
        errors: d.errors,
        topHost,
        lastAt: d.lastAt,
        spark: d.spark,
      }
    })
    .sort((a, b) => b.bytes - a.bytes || b.count - a.count)

  const hosts: HostStat[] = [...hostMap.entries()]
    .map(([host, h]) => ({
      host,
      count: h.count,
      bytes: h.bytes,
      errors: h.errors,
      avgMs: h.done ? h.totalMs / h.done : 0,
    }))
    .sort((a, b) => b.count - a.count)

  const methods = [...methodMap.entries()]
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => b.count - a.count)

  return {
    total: flows.length,
    bytes,
    devices,
    buckets,
    hosts,
    status,
    methods,
    errorRate: flows.length ? (errors / flows.length) * 100 : 0,
    from,
    to,
    spanMs,
  }
}

export function formatSize(n: number): string {
  if (n <= 0) return "0 B"
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(2)} MB`
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

export function formatMs(ms: number): string {
  if (ms <= 0) return "—"
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(2)} s`
}
