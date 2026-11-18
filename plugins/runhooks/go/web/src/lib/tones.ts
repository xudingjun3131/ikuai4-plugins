export function methodClass(method: string | undefined): string {
  switch ((method ?? "").toUpperCase()) {
    case "GET":
      return "bg-emerald-500/20 text-emerald-300 ring-1 ring-emerald-500/30"
    case "POST":
      return "bg-sky-500/20 text-sky-300 ring-1 ring-sky-500/30"
    case "PUT":
    case "PATCH":
      return "bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/30"
    case "DELETE":
      return "bg-rose-500/20 text-rose-300 ring-1 ring-rose-500/30"
    case "OPTIONS":
    case "HEAD":
      return "bg-violet-500/20 text-violet-300 ring-1 ring-violet-500/30"
    default:
      return "bg-muted text-muted-foreground ring-1 ring-border"
  }
}

export function statusClass(status: number | undefined): string {
  if (status === undefined || status === 0) {
    return "bg-muted text-muted-foreground"
  }
  if (status >= 500) return "bg-rose-500/20 text-rose-300 ring-1 ring-rose-500/35"
  if (status >= 400) return "bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/35"
  if (status >= 300) return "bg-sky-500/20 text-sky-300 ring-1 ring-sky-500/35"
  if (status >= 200) return "bg-emerald-500/20 text-emerald-300 ring-1 ring-emerald-500/35"
  return "bg-muted text-muted-foreground"
}

export function schemeClass(scheme: string | undefined): string {
  if (scheme === "https") return "text-emerald-400"
  if (scheme === "http") return "text-amber-400"
  return "text-muted-foreground"
}

export function formatDuration(startedAt?: string, doneAt?: string): string {
  if (!startedAt || !doneAt) return "—"
  const a = Date.parse(startedAt)
  const b = Date.parse(doneAt)
  if (Number.isNaN(a) || Number.isNaN(b)) return "—"
  const ms = Math.max(0, b - a)
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

export function formatBytes(n?: number): string {
  if (n === undefined || n <= 0) return "—"
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(2)} MB`
}
