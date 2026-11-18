import { useCallback, useEffect, useMemo, useState } from "react"
import { flowHost } from "@/lib/api"
import type { FlowDTO } from "@/lib/types"
import { Button } from "@/components/ui/button"

type Prefs = {
  columns?: string[]
  tab?: string
}

type Props = {
  readonly flows: readonly FlowDTO[]
  readonly onHostFilter: (host: string) => void
}

const DEFAULT_COLS = ["method", "status", "host", "path"] as const

/** 列标识符 -> 中文显示名(标识符要原样用于偏好存储, 不能直接改值) */
const COL_LABELS: Record<string, string> = {
  method: "方法",
  status: "状态码",
  host: "主机",
  path: "路径",
  scheme: "协议",
  protocol: "协议族",
}

export function ExplorerPanel({ flows, onHostFilter }: Props) {
  const [prefs, setPrefs] = useState<Prefs>({ columns: [...DEFAULT_COLS], tab: "hosts" })
  const [tab, setTab] = useState<"hosts" | "columns">("hosts")
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/ui/prefs")
      if (!res.ok) return
      const data = (await res.json()) as { prefs?: Prefs } & Prefs
      const p = data.prefs ?? data
      if (p.columns?.length) setPrefs(p)
    } catch (e) {
      setError(e instanceof Error ? e.message : "偏好保存失败")
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const hosts = useMemo(() => {
    const m = new Map<string, number>()
    for (const f of flows) {
      const h = flowHost(f) || "(unknown)"
      m.set(h, (m.get(h) ?? 0) + 1)
    }
    return [...m.entries()].sort((a, b) => b[1] - a[1])
  }, [flows])

  async function saveColumns(cols: string[]) {
    const next = { ...prefs, columns: cols, tab: "columns" }
    setPrefs(next)
    await fetch("/api/ui/prefs", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ prefs: next }),
    })
  }

  function toggleCol(c: string) {
    const cur = prefs.columns ?? [...DEFAULT_COLS]
    const has = cur.includes(c)
    const next = has ? cur.filter((x) => x !== c) : [...cur, c]
    void saveColumns(next.length ? next : [...DEFAULT_COLS])
  }

  return (
    <div
      className="flex h-full flex-col gap-2 border-l border-border p-3"
      data-testid="explorer-panel"
    >
      <h2 className="text-sm font-semibold">浏览  </h2>
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
      <div className="flex gap-1">
        <Button
          size="sm"
          variant={tab === "hosts" ? "default" : "outline"}
          data-testid="explorer-tab-hosts"
          onClick={() => setTab("hosts")}
        >
          主机
        </Button>
        <Button
          size="sm"
          variant={tab === "columns" ? "default" : "outline"}
          data-testid="explorer-tab-columns"
          onClick={() => setTab("columns")}
        >
          栏目
        </Button>
      </div>
      {tab === "hosts" ? (
        <ul className="min-h-0 flex-1 space-y-1 overflow-auto text-xs" data-testid="explorer-hosts">
          {hosts.length === 0 ? (
            <li className="text-muted-foreground">暂无主机</li>
          ) : (
            hosts.map(([h, n]) => (
              <li key={h}>
                <button
                  type="button"
                  className="flex w-full items-center justify-between rounded px-1 py-0.5 hover:bg-muted"
                  data-testid="explorer-host"
                  onClick={() => onHostFilter(h === "(unknown)" ? "" : h)}
                >
                  <span className="truncate font-mono">{h}</span>
                  <span className="tabular-nums text-muted-foreground">{n}</span>
                </button>
              </li>
            ))
          )}
        </ul>
      ) : (
        <div className="space-y-1 text-xs" data-testid="explorer-columns">
          {["method", "status", "host", "path", "scheme", "protocol"].map((c) => {
            const on = (prefs.columns ?? []).includes(c)
            return (
              <label key={c} className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={on}
                  onChange={() => toggleCol(c)}
                  data-testid={`col-${c}`}
                />
                {COL_LABELS[c] ?? c}
              </label>
            )
          })}
        </div>
      )}
    </div>
  )
}
