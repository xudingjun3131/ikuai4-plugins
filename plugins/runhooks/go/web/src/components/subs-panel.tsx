import { useCallback, useEffect, useState } from "react"
import { Download, Plus, RefreshCw, Trash2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/* ------------------------------------------------------------------ */
/* 类型(与 internal/subscription/types.go 的 JSON 字段一一对应)           */
/* ------------------------------------------------------------------ */

type Sub = {
  id: string
  name: string
  url: string
  format: string
  enabled: boolean
  interval_min: number
  last_update: number
  last_error?: string
  rule_count: number
  host_count: number
  ip_count: number
  rewrite_count: number
  script_count: number
  skipped_count: number
}

type Effective = {
  domains: string[]
  dst_ips: string[]
  countries: string[]
  paths: string[]
  user_agents: string[]
  rules: number
  scripts: number
}

type Parsed = {
  format: string
  mitm_hosts: string[]
  rules: { type: string; value: string; policy: string; raw: string }[]
  rewrites: { pattern: string; kind: string; value: string }[]
  scripts: { name: string; kind: string; fetch_err?: string }[]
  warnings: string[]
  skipped: number
}

const FORMAT_LABEL: Record<string, string> = {
  auto: "自动识别",
  quanx: "QuanX",
  surge: "Surge",
  list: "纯列表",
}

const inputCls =
  "w-full rounded-md border border-border bg-background px-2 py-1 text-xs text-foreground outline-none ring-primary/40 focus:ring-2"
const labelCls = "mb-1 block text-[11px] font-medium text-muted-foreground"

function fmtTime(ts: number): string {
  if (!ts) return "从未"
  const d = new Date(ts * 1000)
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export function SubsPanel() {
  const [subs, setSubs] = useState<Sub[]>([])
  const [effective, setEffective] = useState<Effective | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [url, setUrl] = useState("")
  const [name, setName] = useState("")
  const [format, setFormat] = useState("auto")
  const [intervalMin, setIntervalMin] = useState(0)
  const [detail, setDetail] = useState<{ id: string; parsed: Parsed | null } | null>(null)

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/subscriptions")
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const body = (await res.json()) as { subs: Sub[]; effective: Effective }
      setSubs(body.subs || [])
      setEffective(body.effective || null)
      setError("")
    } catch (e) {
      setError(String(e))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const add = async () => {
    if (!url.trim()) return
    setBusy(true)
    try {
      const res = await fetch("/api/subscriptions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: url.trim(), name: name.trim(), format, interval_min: intervalMin }),
      })
      if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`)
      setUrl("")
      setName("")
      await load()
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }

  const act = async (id: string, method: string, path = "", body?: unknown) => {
    setBusy(true)
    try {
      const res = await fetch(`/api/subscriptions/${id}${path}`, {
        method,
        headers: body ? { "Content-Type": "application/json" } : undefined,
        body: body ? JSON.stringify(body) : undefined,
      })
      if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`)
      await load()
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }

  const showDetail = async (id: string) => {
    if (detail?.id === id) {
      setDetail(null)
      return
    }
    try {
      const res = await fetch(`/api/subscriptions/${id}/parsed`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const parsed = (await res.json()) as Parsed
      setDetail({ id, parsed })
    } catch (e) {
      setError(String(e))
    }
  }

  return (
    <section className="flex h-full min-h-0 flex-col bg-card/40" data-testid="subs-panel">
      <header className="flex h-9 shrink-0 items-center gap-2 border-b border-border/80 bg-card/60 px-3">
        <Download className="h-3.5 w-3.5 text-violet-300" />
        <span className="panel-title">规则订阅</span>
        <span className="ml-auto rounded-full bg-primary/15 px-2 py-0.5 font-mono text-[10px] text-primary tabular-nums ring-1 ring-primary/25">
          {subs.length}
        </span>
      </header>

      {error ? <p className="px-3 py-2 text-xs text-rose-300">{error}</p> : null}

      <div className="min-h-0 flex-1 overflow-auto p-3">
        {/* 生效范围总览 */}
        {effective ? (
          <div className="mb-3 rounded-lg border border-border/70 bg-card/60 p-3 text-[11px] text-muted-foreground shadow-sm ring-1 ring-black/20">
            <p className="mb-1 font-medium text-foreground/80">当前生效(全部启用订阅的并集)</p>
            <p>
              解密域名 <b className="text-foreground">{(effective.domains || []).length}</b> · 引擎规则{" "}
              <b className="text-foreground">{effective.rules || 0}</b> · 脚本{" "}
              <b className="text-foreground">{effective.scripts || 0}</b>
              {(effective.dst_ips || []).length ? <> · IP 段 {effective.dst_ips.length}</> : null}
              {(effective.countries || []).length ? <> · 国家 {effective.countries.length}</> : null}
            </p>
            {(effective.domains || []).length > 0 ? (
              <p className="mt-1 max-h-16 overflow-auto font-mono text-[10px] leading-relaxed text-emerald-100/80">
                {(effective.domains || []).slice(0, 40).join("  ")}
                {effective.domains.length > 40 ? `  …共 ${effective.domains.length} 个` : ""}
              </p>
            ) : null}
          </div>
        ) : null}

        {/* 新增订阅 */}
        <div className="mb-3 rounded-lg border border-border/70 bg-card/60 p-3 shadow-sm ring-1 ring-black/20">
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_10rem_7rem_6rem_auto]">
            <div>
              <label className={labelCls} htmlFor="sub-url">
                订阅地址 (http/https)
              </label>
              <input
                id="sub-url"
                className={inputCls}
                placeholder="https://example.com/quanx.conf"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
              />
            </div>
            <div>
              <label className={labelCls} htmlFor="sub-name">
                备注名
              </label>
              <input
                id="sub-name"
                className={inputCls}
                placeholder="可选"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div>
              <label className={labelCls} htmlFor="sub-format">
                格式
              </label>
              <select
                id="sub-format"
                className={inputCls}
                value={format}
                onChange={(e) => setFormat(e.target.value)}
              >
                {Object.entries(FORMAT_LABEL).map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className={labelCls} htmlFor="sub-interval">
                更新(分钟)
              </label>
              <input
                id="sub-interval"
                className={inputCls}
                type="number"
                min={0}
                step={5}
                value={intervalMin}
                onChange={(e) => setIntervalMin(Number(e.target.value))}
              />
            </div>
            <div className="flex items-end">
              <Button size="sm" className="h-7 gap-1" disabled={busy || !url.trim()} onClick={() => void add()}>
                <Plus className="h-3 w-3" />
                添加
              </Button>
            </div>
          </div>
          <p className="mt-2 text-[10px] text-muted-foreground">
            支持 QuanX(.conf) / Surge(.conf) / 纯域名列表; 0 = 仅手动更新。订阅规则自动适配为解密范围 + 改写规则 + 脚本, 与手写配置分层存放互不覆盖。
          </p>
        </div>

        {/* 订阅列表 */}
        <ul className="space-y-2">
          {subs.map((s) => (
            <li
              key={s.id}
              className="rounded-lg border border-border/70 bg-card/60 p-3 shadow-sm ring-1 ring-black/20"
            >
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm font-medium text-foreground/90">{s.name}</span>
                <span className="rounded bg-muted/60 px-1.5 py-0.5 text-[10px] text-muted-foreground">
                  {FORMAT_LABEL[s.format] ?? s.format}
                </span>
                <span
                  className={cn(
                    "rounded px-1.5 py-0.5 text-[10px] ring-1",
                    s.enabled
                      ? "bg-emerald-500/15 text-emerald-300 ring-emerald-500/30"
                      : "bg-muted/60 text-muted-foreground ring-border",
                  )}
                >
                  {s.enabled ? "已启用" : "已暂停"}
                </span>
                {s.interval_min > 0 ? (
                  <span className="text-[10px] text-muted-foreground">每 {s.interval_min} 分钟更新</span>
                ) : null}
                <span className="ml-auto flex items-center gap-1">
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-7 gap-1 text-[10px]"
                    disabled={busy}
                    onClick={() => void act(s.id, "POST", "/update")}
                  >
                    <RefreshCw className="h-3 w-3" />
                    更新
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-7 text-[10px]"
                    disabled={busy}
                    onClick={() => void act(s.id, "PUT", "", { enabled: !s.enabled })}
                  >
                    {s.enabled ? "暂停" : "启用"}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-7 text-[10px]"
                    onClick={() => void showDetail(s.id)}
                  >
                    {detail?.id === s.id ? "收起明细" : "明细"}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-7 gap-1 text-[10px] text-rose-300"
                    disabled={busy}
                    onClick={() => void act(s.id, "DELETE")}
                  >
                    <Trash2 className="h-3 w-3" />
                    删除
                  </Button>
                </span>
              </div>
              <p className="mt-1 truncate font-mono text-[10px] text-muted-foreground" title={s.url}>
                {s.url}
              </p>
              <p className="mt-1 text-[10px] text-muted-foreground">
                域名 {s.host_count} · IP {s.ip_count} · 规则行 {s.rule_count} · 改写 {s.rewrite_count} · 脚本{" "}
                {s.script_count}
                {s.skipped_count > 0 ? <> · 跳过 {s.skipped_count}</> : null} · 更新于 {fmtTime(s.last_update)}
              </p>
              {s.last_error ? <p className="mt-1 text-[10px] text-rose-300">错误: {s.last_error}</p> : null}

              {detail?.id === s.id && detail.parsed ? (
                <div className="mt-2 space-y-2 rounded-md border border-border/60 bg-background/60 p-2 text-[10px]">
                  <p className="font-medium text-foreground/70">
                    解析明细(格式 {detail.parsed.format}, 跳过 {detail.parsed.skipped} 行)
                  </p>
                  {(detail.parsed.mitm_hosts || []).length > 0 ? (
                    <p>
                      <span className="text-muted-foreground">[mitm] 主机: </span>
                      <span className="font-mono text-emerald-100/80">{detail.parsed.mitm_hosts.join(", ")}</span>
                    </p>
                  ) : null}
                  {(detail.parsed.rules || []).length > 0 ? (
                    <div>
                      <p className="text-muted-foreground">规则(前 50 条):</p>
                      <ul className="ml-3 list-disc space-y-0.5 font-mono">
                        {(detail.parsed.rules || []).slice(0, 50).map((r, i) => (
                          <li key={i}>
                            {r.type},{r.value},{r.policy}
                          </li>
                        ))}
                      </ul>
                    </div>
                  ) : null}
                  {(detail.parsed.rewrites || []).length > 0 ? (
                    <div>
                      <p className="text-muted-foreground">改写指令(前 30 条):</p>
                      <ul className="ml-3 list-disc space-y-0.5 font-mono">
                        {(detail.parsed.rewrites || []).slice(0, 30).map((r, i) => (
                          <li key={i}>
                            {r.kind} ← {r.pattern}
                          </li>
                        ))}
                      </ul>
                    </div>
                  ) : null}
                  {(detail.parsed.scripts || []).length > 0 ? (
                    <p>
                      <span className="text-muted-foreground">脚本: </span>
                      <span className="font-mono">
                        {(detail.parsed.scripts || []).map((s2) => `${s2.name}${s2.fetch_err ? "(未取到)" : ""}`).join(", ")}
                      </span>
                    </p>
                  ) : null}
                  {(detail.parsed.warnings || []).length > 0 ? (
                    <div>
                      <p className="text-amber-300">适配说明({detail.parsed.warnings.length}):</p>
                      <ul className="ml-3 list-disc space-y-0.5 text-amber-200/80">
                        {(detail.parsed.warnings || []).slice(0, 20).map((w, i) => (
                          <li key={i}>{w}</li>
                        ))}
                      </ul>
                    </div>
                  ) : null}
                </div>
              ) : null}
            </li>
          ))}
          {subs.length === 0 ? (
            <li className="py-8 text-center text-xs text-muted-foreground">
              还没有订阅。粘贴 QuanX/Surge 订阅地址, 规则会自动适配到抓包范围与改写规则。
            </li>
          ) : null}
        </ul>
      </div>
    </section>
  )
}
