import { useCallback, useEffect, useMemo, useState } from "react"
import {
  FolderPlus,
  Loader2,
  Play,
  Plus,
  Save,
  Trash2,
  Import,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"
import { methodClass, statusClass, formatBytes } from "@/lib/tones"

type KV = { id: string; key: string; value: string; enabled: boolean }

type SendResult = {
  status: number
  headers?: Record<string, string[]>
  body: string
  timingMs: number
  error?: string
  truncated?: boolean
}

type CollectionReq = {
  id: string
  name: string
  method: string
  url: string
  headers?: Record<string, string>
  body?: string
}

type Collection = {
  id: string
  name: string
  requests: CollectionReq[]
}

type BuilderTab = "params" | "headers" | "body" | "auth" | "env"
type RespTab = "body" | "headers"

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"] as const

function nid() {
  return Math.random().toString(36).slice(2, 10)
}

function emptyKV(): KV {
  return { id: nid(), key: "", value: "", enabled: true }
}

function parseQuery(url: string): KV[] {
  try {
    const u = new URL(url)
    const rows: KV[] = []
    u.searchParams.forEach((value, key) => {
      rows.push({ id: nid(), key, value, enabled: true })
    })
    return rows.length ? rows : [emptyKV()]
  } catch {
    return [emptyKV()]
  }
}

function applyQuery(url: string, params: KV[]): string {
  try {
    const u = new URL(url)
    // clear
    ;[...u.searchParams.keys()].forEach((k) => u.searchParams.delete(k))
    for (const p of params) {
      if (!p.enabled || !p.key.trim()) continue
      u.searchParams.append(p.key.trim(), p.value)
    }
    return u.toString()
  } catch {
    const base = url.split("?")[0] ?? url
    const q = params
      .filter((p) => p.enabled && p.key.trim())
      .map((p) => `${encodeURIComponent(p.key.trim())}=${encodeURIComponent(p.value)}`)
      .join("&")
    return q ? `${base}?${q}` : base
  }
}

function headersToKV(h?: Record<string, string>): KV[] {
  if (!h || Object.keys(h).length === 0) return [emptyKV()]
  return Object.entries(h).map(([key, value]) => ({
    id: nid(),
    key,
    value,
    enabled: true,
  }))
}

function kvToHeaders(rows: KV[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const r of rows) {
    if (!r.enabled || !r.key.trim()) continue
    out[r.key.trim()] = r.value
  }
  return out
}

function pretty(text: string): string {
  if (!text) return ""
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

export function RestPanel({ flowId }: { flowId: string | null }) {
  const [method, setMethod] = useState("GET")
  const [url, setUrl] = useState("https://httpbin.org/get")
  const [params, setParams] = useState<KV[]>([emptyKV()])
  const [headers, setHeaders] = useState<KV[]>([
    { id: nid(), key: "Content-Type", value: "application/json", enabled: true },
    emptyKV(),
  ])
  const [body, setBody] = useState('{\n  "hello": "world"\n}')
  const [authType, setAuthType] = useState<"none" | "bearer" | "basic">("none")
  const [bearer, setBearer] = useState("")
  const [basicUser, setBasicUser] = useState("")
  const [basicPass, setBasicPass] = useState("")
  const [envText, setEnvText] = useState("{\n  \"base\": \"https://httpbin.org\"\n}")
  const [tab, setTab] = useState<BuilderTab>("headers")
  const [respTab, setRespTab] = useState<RespTab>("body")
  const [result, setResult] = useState<SendResult | null>(null)
  const [collections, setCollections] = useState<Collection[]>([])
  const [activeCol, setActiveCol] = useState<string | null>(null)
  const [reqName, setReqName] = useState("未命名")
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const loadEnv = useCallback(async () => {
    const res = await fetch("/api/rest/env")
    if (!res.ok) return
    const data = (await res.json()) as { env: Record<string, string> }
    setEnvText(JSON.stringify(data.env ?? {}, null, 2))
  }, [])

  const loadCollections = useCallback(async () => {
    const res = await fetch("/api/rest/collections")
    if (!res.ok) return
    const data = (await res.json()) as { collections?: Collection[] }
    const list = data.collections ?? []
    setCollections(list)
    if (!activeCol && list[0]) setActiveCol(list[0].id)
  }, [activeCol])

  useEffect(() => {
    void loadEnv()
    void loadCollections()
  }, [loadEnv, loadCollections])

  const activeCollection = useMemo(
    () => collections.find((c) => c.id === activeCol) ?? null,
    [collections, activeCol],
  )

  function onUrlChange(next: string) {
    setUrl(next)
    setParams(parseQuery(next))
  }

  function onParamsChange(rows: KV[]) {
    setParams(rows)
    setUrl((u) => applyQuery(u, rows))
  }

  function buildHeaders(): Record<string, string> {
    const h = kvToHeaders(headers)
    if (authType === "bearer" && bearer.trim()) {
      h.Authorization = `Bearer ${bearer.trim()}`
    }
    if (authType === "basic" && (basicUser || basicPass)) {
      h.Authorization = `Basic ${btoa(`${basicUser}:${basicPass}`)}`
    }
    return h
  }

  async function send() {
    setBusy(true)
    setError(null)
    setResult(null)
    try {
      const finalURL = applyQuery(url, params)
      const res = await fetch("/api/rest/send", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          method,
          url: finalURL,
          headers: buildHeaders(),
          body: method === "GET" || method === "HEAD" ? "" : body,
        }),
      })
      const text = await res.text()
      if (!res.ok) {
        setError(text)
        return
      }
      setResult(JSON.parse(text) as SendResult)
      setRespTab("body")
    } catch (e) {
      setError(e instanceof Error ? e.message : "发送失败")
    } finally {
      setBusy(false)
    }
  }

  async function fromFlow() {
    if (!flowId) return
    setBusy(true)
    setError(null)
    try {
      // API is POST
      const res = await fetch(`/api/rest/from-flow/${flowId}`, { method: "POST" })
      if (!res.ok) {
        setError(await res.text())
        return
      }
      const data = (await res.json()) as {
        method?: string
        url?: string
        body?: string
        headers?: Record<string, string>
      }
      if (data.method) setMethod(data.method)
      if (data.url) onUrlChange(data.url)
      if (data.body != null) setBody(data.body)
      if (data.headers) setHeaders(headersToKV(data.headers))
      setReqName(`From capture ${flowId}`)
      setTab("headers")
    } finally {
      setBusy(false)
    }
  }

  async function saveEnv() {
    setBusy(true)
    setError(null)
    try {
      const env = JSON.parse(envText) as Record<string, string>
      const res = await fetch("/api/rest/env", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ env }),
      })
      if (!res.ok) setError(await res.text())
      else await loadEnv()
    } catch (e) {
      setError(e instanceof Error ? e.message : "环境JSON错误")
    } finally {
      setBusy(false)
    }
  }

  async function createCollection() {
    const name = prompt("集合名称", "我的API")
    if (!name) return
    setBusy(true)
    try {
      const col: Collection = {
        id: nid(),
        name,
        requests: [],
      }
      const res = await fetch("/api/rest/collections", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(col),
      })
      if (!res.ok) {
        setError(await res.text())
        return
      }
      await loadCollections()
      setActiveCol(col.id)
    } finally {
      setBusy(false)
    }
  }

  async function saveRequestToCollection() {
    if (!activeCollection) {
      setError("请先创建或选择集合")
      return
    }
    setBusy(true)
    setError(null)
    try {
      const req: CollectionReq = {
        id: nid(),
        name: reqName || `${method} ${url}`,
        method,
        url: applyQuery(url, params),
        headers: buildHeaders(),
        body,
      }
      const next: Collection = {
        ...activeCollection,
        requests: [...(activeCollection.requests ?? []), req],
      }
      const res = await fetch(`/api/rest/collections/${activeCollection.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(next),
      })
      if (!res.ok) {
        // try POST full replace via POST if PUT path differs
        const res2 = await fetch("/api/rest/collections", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(next),
        })
        if (!res2.ok) {
          setError(await res.text())
          return
        }
      }
      await loadCollections()
    } finally {
      setBusy(false)
    }
  }

  async function deleteCollection(id: string) {
    if (!confirm("删除该集合？")) return
    setBusy(true)
    try {
      await fetch(`/api/rest/collections/${id}`, { method: "DELETE" })
      if (activeCol === id) setActiveCol(null)
      await loadCollections()
    } finally {
      setBusy(false)
    }
  }

  function loadRequest(r: CollectionReq) {
    setMethod(r.method || "GET")
    onUrlChange(r.url)
    setHeaders(headersToKV(r.headers))
    setBody(r.body ?? "")
    setReqName(r.name)
    setTab("headers")
  }

  const bodyPreview = useMemo(() => pretty(result?.body ?? ""), [result])

  return (
    <div
      className="flex h-full min-h-0 flex-col bg-[#0b0f17] text-foreground"
      data-testid="rest-panel"
    >
      {/* top bar like Postman */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/80 bg-card/40 px-3 py-2">
        <select
          className={cn(
            "h-9 rounded-md border border-border bg-[#121826] px-2 font-mono text-xs font-bold",
            methodClass(method).includes("emerald") && "text-emerald-300",
          )}
          value={method}
          onChange={(e) => setMethod(e.target.value)}
          data-testid="rest-method"
        >
          {METHODS.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
        <input
          className="h-9 min-w-0 flex-1 rounded-md border border-border bg-[#121826] px-3 font-mono text-xs text-foreground outline-none ring-primary/40 focus:ring-2"
          value={url}
          onChange={(e) => onUrlChange(e.target.value)}
          placeholder="https://api.example.com/v1/{{path}}  可用 {{env}} 变量"
          data-testid="rest-url"
        />
        <Button
          className="h-9 gap-1.5 bg-orange-500 text-black hover:bg-orange-400"
          disabled={busy}
          onClick={() => void send()}
          data-testid="rest-send"
        >
          {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
          发送
        </Button>
        <Button
          variant="secondary"
          className="h-9 gap-1.5"
          disabled={busy || !flowId}
          onClick={() => void fromFlow()}
          data-testid="rest-from-flow"
          title={flowId ? `从流量 ${flowId} 载入` : "请先选择一条流量"}
        >
          <Import className="h-3.5 w-3.5" />
          来自流量
        </Button>
        <Button
          variant="secondary"
          className="h-9 gap-1.5"
          disabled={busy}
          onClick={() => void saveRequestToCollection()}
        >
          <Save className="h-3.5 w-3.5" />
          保存
        </Button>
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[220px_1fr]">
        {/* collections sidebar */}
        <aside className="flex min-h-0 flex-col border-r border-border/70 bg-card/20">
          <div className="flex h-9 items-center gap-2 border-b border-border/60 px-2">
            <span className="panel-title">集合</span>
            <Button
              size="sm"
              variant="ghost"
              className="ml-auto h-7 w-7 p-0"
              onClick={() => void createCollection()}
              title="新建集合"
            >
              <FolderPlus className="h-3.5 w-3.5" />
            </Button>
          </div>
          <ScrollArea className="min-h-0 flex-1">
            <ul className="p-1">
              {collections.map((c) => (
                <li key={c.id} className="mb-1">
                  <div
                    className={cn(
                      "flex items-center gap-1 rounded-md px-2 py-1.5 text-xs",
                      activeCol === c.id ? "bg-primary/15 text-primary" : "hover:bg-muted/40",
                    )}
                  >
                    <button
                      type="button"
                      className="min-w-0 flex-1 truncate text-left font-medium"
                      onClick={() => setActiveCol(c.id)}
                    >
                      {c.name}
                    </button>
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-rose-300"
                      onClick={() => void deleteCollection(c.id)}
                    >
                      <Trash2 className="h-3 w-3" />
                    </button>
                  </div>
                  {activeCol === c.id
                    ? (c.requests ?? []).map((r) => (
                        <button
                          key={r.id}
                          type="button"
                          className="flex w-full items-center gap-2 rounded px-2 py-1 pl-4 text-left text-[11px] hover:bg-muted/30"
                          onClick={() => loadRequest(r)}
                        >
                          <span className={cn("method-chip scale-90", methodClass(r.method))}>
                            {r.method}
                          </span>
                          <span className="truncate text-muted-foreground">{r.name}</span>
                        </button>
                      ))
                    : null}
                </li>
              ))}
              {collections.length === 0 ? (
                <li className="px-3 py-6 text-center text-[11px] text-muted-foreground">
                  暂无集合。点击 + 新建一个，然后保存请求。
                </li>
              ) : null}
            </ul>
          </ScrollArea>
        </aside>

        {/* builder + response */}
        <div className="flex min-h-0 min-w-0 flex-col">
          <div className="flex shrink-0 items-center gap-2 border-b border-border/60 px-3 py-1.5">
            <input
              className="h-7 min-w-0 flex-1 rounded border border-transparent bg-transparent px-1 text-xs font-medium outline-none hover:border-border focus:border-primary/50"
              value={reqName}
              onChange={(e) => setReqName(e.target.value)}
              placeholder="请求名称"
            />
            <span className="text-[10px] text-muted-foreground">{"{{var}} from Env"}</span>
          </div>

          {/* request tabs */}
          <div className="flex shrink-0 gap-0.5 border-b border-border/60 px-2">
            {(
              [
                ["params", "Params"],
                ["headers", "Headers"],
                ["body", "Body"],
                ["auth", "Auth"],
                ["env", "Env"],
              ] as const
            ).map(([id, label]) => (
              <button
                key={id}
                type="button"
                onClick={() => setTab(id)}
                className={cn(
                  "relative px-3 py-2 text-[11px] font-medium",
                  tab === id
                    ? "text-orange-300 after:absolute after:right-2 after:bottom-0 after:left-2 after:h-0.5 after:rounded-full after:bg-orange-400"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {label}
                {id === "headers" ? (
                  <span className="ml-1 text-[9px] text-muted-foreground">
                    ({headers.filter((h) => h.enabled && h.key).length})
                  </span>
                ) : null}
              </button>
            ))}
          </div>

          <div className="max-h-[40%] min-h-[8rem] shrink-0 overflow-auto border-b border-border/70">
            {tab === "params" ? (
              <KVEditor rows={params} onChange={onParamsChange} keyPlaceholder="查询参数" />
            ) : null}
            {tab === "headers" ? (
              <KVEditor rows={headers} onChange={setHeaders} keyPlaceholder="头部" />
            ) : null}
            {tab === "body" ? (
              <textarea
                className="h-full min-h-[10rem] w-full resize-y bg-[#0d1118] px-3 py-2 font-mono text-[11px] text-emerald-100/90 outline-none"
                value={body}
                onChange={(e) => setBody(e.target.value)}
                spellCheck={false}
                data-testid="rest-body"
                disabled={method === "GET" || method === "HEAD"}
              />
            ) : null}
            {tab === "auth" ? (
              <div className="space-y-3 p-3 text-xs">
                <div className="flex gap-2">
                  {(["none", "bearer", "basic"] as const).map((t) => (
                    <button
                      key={t}
                      type="button"
                      onClick={() => setAuthType(t)}
                      className={cn(
                        "rounded-md px-3 py-1.5 capitalize ring-1",
                        authType === t
                          ? "bg-primary/15 text-primary ring-primary/40"
                          : "text-muted-foreground ring-border hover:bg-muted/40",
                      )}
                    >
                      {t}
                    </button>
                  ))}
                </div>
                {authType === "bearer" ? (
                  <input
                    className="h-8 w-full rounded border border-border bg-[#121826] px-2 font-mono text-[11px]"
                    placeholder="令牌"
                    value={bearer}
                    onChange={(e) => setBearer(e.target.value)}
                  />
                ) : null}
                {authType === "basic" ? (
                  <div className="flex gap-2">
                    <input
                      className="h-8 flex-1 rounded border border-border bg-[#121826] px-2 font-mono text-[11px]"
                      placeholder="用户名"
                      value={basicUser}
                      onChange={(e) => setBasicUser(e.target.value)}
                    />
                    <input
                      className="h-8 flex-1 rounded border border-border bg-[#121826] px-2 font-mono text-[11px]"
                      placeholder="密码"
                      type="password"
                      value={basicPass}
                      onChange={(e) => setBasicPass(e.target.value)}
                    />
                  </div>
                ) : null}
              </div>
            ) : null}
            {tab === "env" ? (
              <div className="flex h-full flex-col gap-2 p-3">
                <p className="text-[11px] text-muted-foreground">
                  JSON map. Use <code className="text-orange-300">{"{{key}}"}</code> in URL, headers, or body.
                </p>
                <textarea
                  className="min-h-[8rem] flex-1 rounded border border-border bg-[#0d1118] px-2 py-1 font-mono text-[11px] text-emerald-100/90"
                  value={envText}
                  onChange={(e) => setEnvText(e.target.value)}
                  data-testid="rest-env"
                />
                <Button size="sm" className="w-fit" disabled={busy} onClick={() => void saveEnv()}>
                  保存环境
                </Button>
              </div>
            ) : null}
          </div>

          {/* response */}
          <div className="flex min-h-0 flex-1 flex-col bg-[#0a0e16]">
            <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/60 px-3 py-1.5">
              <span className="panel-title">Response</span>
              {result ? (
                <>
                  <span className={cn("status-chip", statusClass(result.status))}>
                    {result.status || "err"}
                  </span>
                  <Badge variant="outline" className="h-5 font-mono text-[10px]">
                    {result.timingMs} ms
                  </Badge>
                  <Badge variant="outline" className="h-5 font-mono text-[10px]">
                    {formatBytes(result.body?.length ?? 0)}
                  </Badge>
                  {result.truncated ? (
                    <Badge variant="outline" className="h-5 border-amber-500/40 text-[10px] text-amber-200">
                      truncated
                    </Badge>
                  ) : null}
                </>
              ) : (
                <span className="text-[11px] text-muted-foreground">点「发送」查看状态、耗时、请求头与响应体</span>
              )}
              <div className="ml-auto flex gap-1">
                {(
                  [
                    ["body", "Body"],
                    ["headers", "Headers"],
                  ] as const
                ).map(([id, label]) => (
                  <button
                    key={id}
                    type="button"
                    onClick={() => setRespTab(id)}
                    className={cn(
                      "rounded px-2 py-1 text-[10px]",
                      respTab === id ? "bg-muted text-foreground" : "text-muted-foreground",
                    )}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
            {error ? (
              <pre className="p-3 text-xs text-rose-300" data-testid="rest-error">
                {error}
              </pre>
            ) : null}
            <ScrollArea className="min-h-0 flex-1">
              {!result && !error ? (
                <p className="p-6 text-center text-[11px] text-muted-foreground">No response yet</p>
              ) : null}
              {result && respTab === "body" ? (
                <pre
                  className="whitespace-pre-wrap break-all p-3 font-mono text-[11px] leading-relaxed text-emerald-100/90"
                  data-testid="rest-result"
                >
                  {result.error ?? bodyPreview}
                </pre>
              ) : null}
              {result && respTab === "headers" ? (
                <dl className="divide-y divide-border/40 font-mono text-[11px]">
                  {Object.entries(result.headers ?? {}).map(([k, vals]) => (
                    <div key={k} className="grid grid-cols-[minmax(8rem,14rem)_1fr] gap-2 px-3 py-1.5">
                      <dt className="text-sky-300/90">{k}</dt>
                      <dd className="break-all text-foreground/90">{vals.join(", ")}</dd>
                    </div>
                  ))}
                </dl>
              ) : null}
            </ScrollArea>
          </div>
        </div>
      </div>
    </div>
  )
}

function KVEditor({
  rows,
  onChange,
  keyPlaceholder,
}: {
  rows: KV[]
  onChange: (rows: KV[]) => void
  keyPlaceholder: string
}) {
  function update(i: number, patch: Partial<KV>) {
    const next = rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r))
    if (i === rows.length - 1 && (patch.key || patch.value)) {
      next.push(emptyKV())
    }
    onChange(next)
  }
  function remove(i: number) {
    const next = rows.filter((_, idx) => idx !== i)
    onChange(next.length ? next : [emptyKV()])
  }
  return (
    <div className="p-2">
      <div className="mb-1 grid grid-cols-[1.5rem_1fr_1fr_2rem] gap-1 px-1 text-[9px] tracking-wider text-muted-foreground uppercase">
        <span />
        <span>{keyPlaceholder}</span>
        <span>值</span>
        <span />
      </div>
      {rows.map((r, i) => (
        <div key={r.id} className="mb-1 grid grid-cols-[1.5rem_1fr_1fr_2rem] items-center gap-1">
          <input
            type="checkbox"
            checked={r.enabled}
            onChange={(e) => update(i, { enabled: e.target.checked })}
            className="accent-orange-400"
          />
          <input
            className="h-7 rounded border border-border/60 bg-[#121826] px-2 font-mono text-[11px]"
            placeholder={keyPlaceholder}
            value={r.key}
            onChange={(e) => update(i, { key: e.target.value })}
          />
          <input
            className="h-7 rounded border border-border/60 bg-[#121826] px-2 font-mono text-[11px]"
            placeholder="值"
            value={r.value}
            onChange={(e) => update(i, { value: e.target.value })}
          />
          <button
            type="button"
            className="flex h-7 items-center justify-center text-muted-foreground hover:text-rose-300"
            onClick={() => remove(i)}
          >
            <Trash2 className="h-3 w-3" />
          </button>
        </div>
      ))}
      <Button
        size="sm"
        variant="ghost"
        className="mt-1 h-7 gap-1 text-[10px]"
        onClick={() => onChange([...rows, emptyKV()])}
      >
        <Plus className="h-3 w-3" />
        新行
      </Button>
    </div>
  )
}
