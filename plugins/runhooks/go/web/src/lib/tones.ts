export function methodClass(method: string | undefined): string {
  switch ((method ?? "").toUpperCase()) {
    case "GET":
      return "bg-emerald-50 text-emerald-700 ring-1 ring-emerald-200"
    case "POST":
      return "bg-sky-50 text-sky-700 ring-1 ring-sky-200"
    case "PUT":
    case "PATCH":
      return "bg-amber-50 text-amber-700 ring-1 ring-amber-200"
    case "DELETE":
      return "bg-rose-50 text-rose-700 ring-1 ring-rose-200"
    case "OPTIONS":
    case "HEAD":
      return "bg-violet-50 text-violet-700 ring-1 ring-violet-200"
    default:
      return "bg-muted text-muted-foreground ring-1 ring-border"
  }
}

export function statusClass(status: number | undefined): string {
  if (status === undefined || status === 0) {
    return "bg-muted text-muted-foreground"
  }
  if (status >= 500) return "bg-rose-50 text-rose-700 ring-1 ring-rose-200"
  if (status >= 400) return "bg-amber-50 text-amber-700 ring-1 ring-amber-200"
  if (status >= 300) return "bg-sky-50 text-sky-700 ring-1 ring-sky-200"
  if (status >= 200) return "bg-emerald-50 text-emerald-700 ring-1 ring-emerald-200"
  return "bg-muted text-muted-foreground"
}

export function schemeClass(scheme: string | undefined): string {
  if (scheme === "https") return "text-emerald-700"
  if (scheme === "http") return "text-amber-700"
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
