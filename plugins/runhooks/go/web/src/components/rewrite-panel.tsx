import { useCallback, useEffect, useState } from "react"

import { apiURL } from "@/lib/api"
import type { FlowDTO } from "@/lib/types"

// [v1.1.8] 图形化「断点 + Map Local + diff」面板 —— 落实 v1.1.7 测试报告
// §3.6 的产品短板(改包只靠规则/订阅/API/脚本, 缺可视化交互)与 §3.11
// 的 diff 可视化诉求。Charles/Proxyman 式「挂起 → 编辑 → 放行」闭环。
type BpSession = {
  id: string
  phase: string
  method: string
  url: string
  status?: number
  headers: Record<string, string>
  body: string
  createdAt: string
}

type Capacity = {
  max: number
  timeoutMs: number
  holds: number
  total: number
  resumed: number
  expired: number
  aborted: number
  clientGone: number
  rejected: number
}

type ScriptError = {
  script: string
  phase: string
  message: string
  at: string
  count: number
}

type MapRule = {
  id: string
  enabled: boolean
  type: string
  match?: { host?: string; path?: string }
  map_local?: { status?: number; body?: string }
}

type DiffHunk = {
  side: string
  field: string
  left: string
  right: string
  changed: boolean
}

type SessionEdit = {
  status: string
  headers: string
  body: string
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(apiURL(path))
  if (!res.ok) throw new Error(`GET ${path}: HTTP ${res.status}`)
  return (await res.json()) as T
}

async function sendJSON<T>(
  path: string,
  method: string,
  body?: unknown,
): Promise<T> {
  const res = await fetch(apiURL(path), {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${method} ${path}: HTTP ${res.status} ${await res.text()}`)
  return (await res.json()) as T
}

function headersToText(h: Record<string, string> | undefined): string {
  return Object.entries(h ?? {})
    .map(([k, v]) => `${k}: ${v}`)
    .join("\n")
}

function parseHeaders(text: string): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const line of text.split("\n")) {
    const s = line.trim()
    if (!s) continue
    const i = s.indexOf(":")
    if (i > 0) out[s.slice(0, i).trim()] = s.slice(i + 1).trim()
  }
  return Object.keys(out).length ? out : undefined
}

function shortURL(u: string): string {
  return u.length > 68 ? `${u.slice(0, 65)}…` : u
}

export function RewritePanel({ flows }: { readonly flows: readonly FlowDTO[] }) {
  const [enabled, setEnabled] = useState(false)
  const [sessions, setSessions] = useState<BpSession[]>([])
  const [capacity, setCapacity] = useState<Capacity | null>(null)
  const [matchHost, setMatchHost] = useState("")
  const [matchPath, setMatchPath] = useState("")
  const [matchMethods, setMatchMethods] = useState("")
  const [maxHolds, setMaxHolds] = useState("32")
  const [timeoutMs, setTimeoutMs] = useState("60000")
  const [errors, setErrors] = useState<ScriptError[]>([])
  const [scriptTimeoutMs, setScriptTimeoutMs] = useState(0)
  const [mapRules, setMapRules] = useState<MapRule[]>([])
  const [mlHost, setMlHost] = useState("")
  const [mlPath, setMlPath] = useState("")
  const [mlStatus, setMlStatus] = useState("200")
  const [mlBody, setMlBody] = useState("")
  const [leftId, setLeftId] = useState("")
  const [rightId, setRightId] = useState("")
  const [diff, setDiff] = useState<DiffHunk[] | null>(null)
  const [edits, setEdits] = useState<Record<string, SessionEdit>>({})
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [loadErr, setLoadErr] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const [bp, diag, rules] = await Promise.all([
        getJSON<{
          enabled: boolean
          sessions: BpSession[]
          capacity: Capacity
          match?: { host?: string; path?: string; methods?: string[] }
        }>("/api/breakpoints"),
        getJSON<{ scriptTimeoutMs: number; scriptErrors: ScriptError[] }>(
          "/api/diagnostics",
        ),
        getJSON<{ rules: MapRule[] }>("/api/rules"),
      ])
      setEnabled(bp.enabled)
      setSessions(bp.sessions ?? [])
      setCapacity(bp.capacity ?? null)
      setErrors(diag.scriptErrors ?? [])
      setScriptTimeoutMs(diag.scriptTimeoutMs ?? 0)
      setMapRules((rules.rules ?? []).filter((r) => r.type === "map_local"))
      setLoadErr(null)
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : "加载失败")
    }
  }, [])

  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => void refresh(), 1500)
    return () => window.clearInterval(timer)
  }, [refresh])

  // 新挂起的会话按原文预填编辑区; 已编辑过的会话不被轮询覆盖。
  useEffect(() => {
    setEdits((prev) => {
      const next: Record<string, SessionEdit> = {}
      for (const s of sessions) {
        next[s.id] = prev[s.id] ?? {
          status: String(s.status ?? 200),
          headers: headersToText(s.headers),
          body: s.body,
        }
      }
      const sameKeys =
        Object.keys(next).length === Object.keys(prev).length &&
        Object.keys(next).every((k) => prev[k] === next[k])
      return sameKeys ? prev : next
    })
  }, [sessions])

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true)
    setNotice(null)
    try {
      await fn()
      setNotice(ok)
      await refresh()
    } catch (e) {
      setNotice(e instanceof Error ? e.message : "操作失败")
    } finally {
      setBusy(false)
    }
  }

  const saveConfig = () =>
    void act(
      () =>
        sendJSON("/api/breakpoints", "PUT", {
          enabled,
          match: {
            host: matchHost.trim(),
            path: matchPath.trim(),
            methods: matchMethods
              .split(/[,，\s]+/)
              .map((s) => s.trim())
              .filter(Boolean),
          },
          max: Number(maxHolds) || 32,
          timeoutMs: Number(timeoutMs) || 60000,
        }),
      "断点配置与容量已保存",
    )

  const toggle = () =>
    void act(
      () => sendJSON("/api/breakpoints", "PUT", { enabled: !enabled }),
      enabled ? "断点已关闭" : "断点已开启",
    )

  const resume = (id: string, withEdit: boolean) => {
    const e = edits[id]
    const edit: Record<string, unknown> = {}
    if (withEdit && e) {
      const status = Number(e.status)
      if (Number.isFinite(status) && status > 0) edit.status = status
      const headers = parseHeaders(e.headers)
      if (headers) edit.headers = headers
      edit.body = e.body
    }
    return act(
      () =>
        sendJSON(`/api/breakpoints/${id}/resume`, "POST", edit),
      withEdit ? "已带编辑放行" : "已原样放行",
    )
  }

  const abort = (id: string) =>
    void act(
      () => sendJSON(`/api/breakpoints/${id}/abort`, "POST", {}),
      "已丢弃该挂起会话, 原文放行",
    )

  const addMapLocal = () =>
    void act(
      () =>
        sendJSON("/api/rules", "POST", {
          enabled: true,
          type: "map_local",
          match: { host: mlHost.trim(), path: mlPath.trim() },
          map_local: { status: Number(mlStatus) || 200, body: mlBody },
        }),
      "Map Local 规则已创建(内联 body, 短接上游)",
    )

  const removeMapLocal = (id: string) =>
    void act(
      () => sendJSON(`/api/rules/${id}`, "DELETE"),
      "Map Local 规则已删除",
    )

  const runDiff = () =>
    void act(async () => {
      if (!leftId || !rightId) throw new Error("请先选两条会话")
      const out = await getJSON<{ hunks: DiffHunk[] }>(
        `/api/diff?left=${encodeURIComponent(leftId)}&right=${encodeURIComponent(rightId)}`,
      )
      setDiff(out.hunks ?? [])
    }, "对比完成")

  const clearErrors = () =>
    void act(
      () => sendJSON("/api/diagnostics", "DELETE"),
      "脚本诊断已清除",
    )

  const setEdit = (id: string, patch: Partial<SessionEdit>) =>
    setEdits((prev) => ({ ...prev, [id]: { ...prev[id], ...patch } }))

  const diffOptions = flows.slice(0, 200)

  return (
    <div className="nl-rw" data-testid="rewrite-panel">
      <header className="nl-rw-head">
        <span className="nl-rw-title">断点 · 改包工作台</span>
        <span className="nl-rw-sub">
          挂起请求 → 编辑 body / headers / status → 放行, 或一键 Map Local 顶替上游
        </span>
        <span className="nl-rw-spacer" />
        <button
          type="button"
          className={`nl-btn${enabled ? " nl-btn--primary" : ""}`}
          onClick={toggle}
          disabled={busy}
          data-testid="bp-toggle"
        >
          {enabled ? "关闭断点" : "开启断点"}
        </button>
      </header>

      {loadErr ? <p className="nl-rw-err">引擎未就绪: {loadErr}</p> : null}
      {notice ? <p className="nl-rw-msg">{notice}</p> : null}

      <div className="nl-rw-scroll">
        {/* 容量可观测 + 运行时调参 */}
        <section className="nl-rw-card">
          <h3>① 断点范围与容量 <small>hold 占连接容量可观测 / 可调</small></h3>
          <div className="nl-rw-cap" data-testid="bp-capacity">
            <span>挂起 <b>{capacity?.holds ?? 0}</b> / 上限 <b>{capacity?.max ?? "—"}</b></span>
            <span>超时 <b>{capacity ? Math.round(capacity.timeoutMs / 1000) : "—"}</b>s</span>
            <span>累计 <b>{capacity?.total ?? 0}</b></span>
            <span>放行 <b>{capacity?.resumed ?? 0}</b></span>
            <span>丢弃 <b>{capacity?.aborted ?? 0}</b></span>
            <span>超时 <b>{capacity?.expired ?? 0}</b></span>
            <span>客户端断开 <b>{capacity?.clientGone ?? 0}</b></span>
            <span>超限拒绝 <b>{capacity?.rejected ?? 0}</b></span>
          </div>
          <div className="nl-rw-form">
            <label>Host 匹配<input value={matchHost} onChange={(e) => setMatchHost(e.target.value)} placeholder="留空或 * = 全部" /></label>
            <label>Path 包含<input value={matchPath} onChange={(e) => setMatchPath(e.target.value)} placeholder="/api/order" /></label>
            <label>方法(逗号分隔)<input value={matchMethods} onChange={(e) => setMatchMethods(e.target.value)} placeholder="GET,POST" /></label>
            <label>并发上限<input value={maxHolds} onChange={(e) => setMaxHolds(e.target.value)} inputMode="numeric" /></label>
            <label>超时(ms)<input value={timeoutMs} onChange={(e) => setTimeoutMs(e.target.value)} inputMode="numeric" /></label>
            <button type="button" className="nl-btn nl-btn--primary" onClick={saveConfig} disabled={busy} data-testid="bp-save">
              保存配置
            </button>
          </div>
        </section>

        {/* 挂起会话 + 可视化编辑 */}
        <section className="nl-rw-card">
          <h3>② 挂起会话 <small>每 1.5s 刷新; 编辑后放行或丢弃</small></h3>
          {sessions.length === 0 ? (
            <p className="nl-rw-empty">当前没有挂起的请求。开启断点后, 命中的请求/响应会停在这里等待放行。</p>
          ) : null}
          {sessions.map((s) => {
            const e = edits[s.id]
            return (
              <div className="nl-rw-session" key={s.id} data-testid={`bp-session-${s.id}`}>
                <div className="nl-rw-session-head">
                  <span className="nl-rw-phase">{s.phase}</span>
                  <span>{s.method}</span>
                  <span title={s.url}>{shortURL(s.url)}</span>
                  <span>状态 {s.status ?? "—"}</span>
                  <span className="nl-rw-spacer" />
                  <button type="button" className="nl-btn" disabled={busy} onClick={() => void resume(s.id, false)} data-testid="bp-resume-plain">原样放行</button>
                  <button type="button" className="nl-btn nl-btn--primary" disabled={busy} onClick={() => void resume(s.id, true)} data-testid="bp-resume-edit">带编辑放行</button>
                  <button type="button" className="nl-btn nl-btn--danger" disabled={busy} onClick={() => abort(s.id)} data-testid="bp-abort">丢弃</button>
                </div>
                {e ? (
                  <div className="nl-rw-editor">
                    <label className="nl-rw-status">status<input value={e.status} onChange={(ev) => setEdit(s.id, { status: ev.target.value })} inputMode="numeric" /></label>
                    <label className="nl-rw-headers">headers(每行 Key: Value)<textarea value={e.headers} onChange={(ev) => setEdit(s.id, { headers: ev.target.value })} rows={3} /></label>
                    <label className="nl-rw-body">响应/请求 body(明文; Content-Encoding 会自动清理)<textarea value={e.body} onChange={(ev) => setEdit(s.id, { body: ev.target.value })} rows={5} /></label>
                  </div>
                ) : null}
              </div>
            )
          })}
        </section>

        {/* Map Local 可视化 */}
        <section className="nl-rw-card">
          <h3>③ Map Local <small>内联 body 顶替上游, 连请求都不发给服务端</small></h3>
          <div className="nl-rw-form">
            <label>Host<input value={mlHost} onChange={(e) => setMlHost(e.target.value)} placeholder="tri.local" /></label>
            <label>Path<input value={mlPath} onChange={(e) => setMlPath(e.target.value)} placeholder="/secure" /></label>
            <label>status<input value={mlStatus} onChange={(e) => setMlStatus(e.target.value)} inputMode="numeric" /></label>
            <button type="button" className="nl-btn nl-btn--primary" onClick={addMapLocal} disabled={busy || !mlHost.trim()} data-testid="ml-add">创建 Map Local</button>
          </div>
          <textarea className="nl-rw-ml-body" value={mlBody} onChange={(e) => setMlBody(e.target.value)} rows={4} placeholder="内联响应 body(明文)" />
          <ul className="nl-rw-ml-list">
            {mapRules.map((r) => (
              <li key={r.id}>
                <code>{r.match?.host || "*"}{r.match?.path || ""}</code>
                <span>status {r.map_local?.status ?? 200}</span>
                <span className="nl-rw-ml-body-preview">{r.map_local?.body || "(无内联 body)"}</span>
                <span className="nl-rw-spacer" />
                <button type="button" className="nl-btn nl-btn--danger" disabled={busy} onClick={() => removeMapLocal(r.id)} data-testid={`ml-del-${r.id}`}>删除</button>
              </li>
            ))}
            {mapRules.length === 0 ? <li className="nl-rw-empty">暂无 Map Local 规则</li> : null}
          </ul>
        </section>

        {/* diff 可视化 */}
        <section className="nl-rw-card">
          <h3>④ 会话 diff <small>原文 vs 改包后, 字段级左右对照</small></h3>
          <div className="nl-rw-form">
            <label>左(基线)<select value={leftId} onChange={(e) => setLeftId(e.target.value)} data-testid="diff-left">
              <option value="">选择会话</option>
              {diffOptions.map((f) => <option key={f.id} value={f.id}>{f.request.method ?? "?"} {shortURL(f.request.url ?? "")}</option>)}
            </select></label>
            <label>右(改包后)<select value={rightId} onChange={(e) => setRightId(e.target.value)} data-testid="diff-right">
              <option value="">选择会话</option>
              {diffOptions.map((f) => <option key={f.id} value={f.id}>{f.request.method ?? "?"} {shortURL(f.request.url ?? "")}</option>)}
            </select></label>
            <button type="button" className="nl-btn nl-btn--primary" onClick={runDiff} disabled={busy} data-testid="diff-run">对比</button>
          </div>
          {diff !== null ? (
            diff.length === 0 ? (
              <p className="nl-rw-empty">两条会话逐字段一致, 无差异。</p>
            ) : (
              <table className="nl-rw-diff" data-testid="diff-result">
                <thead><tr><th>侧</th><th>字段</th><th>左</th><th>右</th></tr></thead>
                <tbody>
                  {diff.map((h, i) => (
                    <tr key={`${h.side}-${h.field}-${i}`}>
                      <td>{h.side}</td><td>{h.field}</td>
                      <td className="nl-rw-diff-l">{h.left}</td>
                      <td className="nl-rw-diff-r">{h.right}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          ) : null}
        </section>

        {/* 脚本诊断: timeout / 异常不再静默 */}
        <section className="nl-rw-card">
          <h3>⑤ 脚本诊断 <small>timeout / 异常不再静默跳过</small></h3>
          <p className="nl-rw-hint">
            单脚本预算 {scriptTimeoutMs}ms。改包四类入口: 规则(body_replace)、脚本(onResponse)、断点(resume 编辑)、Map Local 内联;
            gzip/br 响应由引擎透明解码后替换, 不再依赖上游接受 identity。
          </p>
          <div className="nl-rw-form">
            <button type="button" className="nl-btn" onClick={clearErrors} disabled={busy} data-testid="diag-clear">清除诊断</button>
          </div>
          {errors.length === 0 ? (
            <p className="nl-rw-empty">无脚本异常。</p>
          ) : (
            <ul className="nl-rw-errors" data-testid="diag-errors">
              {errors.map((e, i) => (
                <li key={`${e.script}-${e.at}-${i}`}>
                  <b>{e.script}</b> · {e.phase} · ×{e.count} · {new Date(e.at).toLocaleTimeString()}
                  <pre>{e.message}</pre>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </div>
  )
}
