import { useCallback, useEffect, useMemo, useState } from "react"
import { Library, Pencil, Plus, Trash2, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/* ------------------------------------------------------------------ */
/* 类型定义(与 internal/rules/rule.go 的 JSON 字段一一对应)              */
/* ------------------------------------------------------------------ */

type Match = { host?: string; path?: string; methods?: string[] }

type HeaderMut = { phase: string; set?: Record<string, string>; remove?: string[] }
type BodyReplace = { phase: string; from: string; to: string }
type MapLocal = { status: number; file: string }
type AccessControl = { cidrs?: string[]; action?: string }
type Gateway = { upstream: string }
type MirrorRemote = { url: string }
type SecondaryProxy = { proxy_url: string }
type ReverseProxy = { listen: string; target: string }
type Highlight = { color: string; label?: string }

type Rule = {
  id: string
  enabled: boolean
  type: string
  match: Match
  header?: HeaderMut
  body_replace?: BodyReplace
  map_local?: MapLocal
  access_control?: AccessControl
  gateway?: Gateway
  mirror_remote?: MirrorRemote
  secondary_proxy?: SecondaryProxy
  reverse_proxy?: ReverseProxy
  highlight?: Highlight
}

/* 规则类型元数据: 值=后端 type, 标签=中文名 */
const RULE_TYPES: { value: string; label: string; hint: string }[] = [
  { value: "body_replace", label: "内容替换", hint: "在响应(或请求)体里做字符串替换, 自动修正 Content-Length" },
  { value: "header", label: "头改写", hint: "增/删请求或响应头" },
  { value: "map_local", label: "本地文件映射", hint: "用本地文件顶替整个响应(文件放 data-dir/map-local/ 下)" },
  { value: "mirror_remote", label: "镜像远端", hint: "用另一个 URL 的响应顶替" },
  { value: "gateway", label: "上游网关", hint: "把请求转发到另一个上游" },
  { value: "secondary_proxy", label: "二级代理", hint: "该 host 的流量再走一层代理" },
  { value: "reverse_proxy", label: "反向代理", hint: "本地监听端口反代到目标" },
  { value: "host_block", label: "拦截域名", hint: "直接 403 拒绝该域名" },
  { value: "host_allow", label: "放行域名", hint: "白名单: 存在任一放行规则时, 仅名单内 host 可过" },
  { value: "access_control", label: "访问控制", hint: "按客户端 IP 段放行/拒绝" },
  { value: "ssl_bypass", label: "不解密(SSL绕过)", hint: "该 host 不进行 MITM 解密, 仅放行隧道" },
  { value: "highlight", label: "高亮标记", hint: "命中流量在列表里按颜色高亮" },
]

const TYPE_LABEL: Record<string, string> = Object.fromEntries(RULE_TYPES.map((t) => [t.value, t.label]))

/* 只有匹配条件、没有额外字段的类型 */
const MATCH_ONLY_TYPES = new Set(["host_block", "host_allow", "ssl_bypass"])

const inputCls =
  "w-full rounded-md border border-border bg-background px-2 py-1 text-xs text-foreground outline-none ring-primary/40 focus:ring-2"
const labelCls = "mb-1 block text-[11px] font-medium text-muted-foreground"

function emptyRule(): Rule {
  return { id: "", enabled: true, type: "body_replace", match: {} }
}

/* 列表里的一行摘要 */
function summarize(r: Rule): string {
  const m = r.match ?? {}
  const parts = [m.host || "*"]
  if (m.path) parts.push(`路径:${m.path}`)
  if (m.methods && m.methods.length) parts.push(m.methods.join("/"))
  switch (r.type) {
    case "body_replace":
      if (r.body_replace) parts.push(`[${r.body_replace.phase}] ${r.body_replace.from} → ${r.body_replace.to}`)
      break
    case "header":
      if (r.header) {
        const bits: string[] = []
        if (r.header.set) bits.push(...Object.keys(r.header.set).map((k) => `${k}=${r.header!.set![k]}`))
        if (r.header.remove && r.header.remove.length) bits.push(...r.header.remove.map((k) => `-${k}`))
        parts.push(`[${r.header.phase}] ${bits.join(", ")}`)
      }
      break
    case "map_local":
      if (r.map_local) parts.push(`→ ${r.map_local.file || "(未填)"} (${r.map_local.status || 200})`)
      break
    case "mirror_remote":
      if (r.mirror_remote) parts.push(`→ ${r.mirror_remote.url}`)
      break
    case "gateway":
      if (r.gateway) parts.push(`→ ${r.gateway.upstream}`)
      break
    case "secondary_proxy":
      if (r.secondary_proxy) parts.push(`→ ${r.secondary_proxy.proxy_url}`)
      break
    case "reverse_proxy":
      if (r.reverse_proxy) parts.push(`${r.reverse_proxy.listen} → ${r.reverse_proxy.target}`)
      break
    case "access_control":
      if (r.access_control) parts.push(`${(r.access_control.cidrs ?? []).join(",")} ${r.access_control.action || "allow"}`)
      break
    case "highlight":
      if (r.highlight) parts.push(`${r.highlight.label || r.highlight.color}`)
      break
  }
  return parts.join("  ")
}

export function RulesPanel() {
  const [rules, setRules] = useState<Rule[]>([])
  const [editing, setEditing] = useState<Rule | null>(null)
  const [isNew, setIsNew] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [msg, setMsg] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/rules")
      if (!res.ok) {
        setError(`加载失败 HTTP ${res.status}`)
        return
      }
      const data = (await res.json()) as { rules: Rule[] }
      setRules(data.rules ?? [])
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : "加载失败")
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  function startAdd() {
    setEditing(emptyRule())
    setIsNew(true)
    setError(null)
    setMsg(null)
  }

  function startEdit(r: Rule) {
    /* 深拷贝, 避免表单中途修改直接改到列表数据 */
    setEditing(JSON.parse(JSON.stringify(r)) as Rule)
    setIsNew(false)
    setError(null)
    setMsg(null)
  }

  async function save() {
    if (!editing) return
    if (!editing.type) {
      setError("类型不能为空")
      return
    }
    if (editing.type === "body_replace" && editing.body_replace && !editing.body_replace.from) {
      setError("内容替换: 待替换字符串不能为空")
      return
    }
    if (editing.match.path) {
      try {
        new RegExp(editing.match.path)
      } catch {
        setError(`路径不是合法正则: ${editing.match.path}`)
        return
      }
    }
    setBusy(true)
    setError(null)
    try {
      const res = isNew
        ? await fetch("/api/rules", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(stripEmpty(editing)),
          })
        : await fetch(`/api/rules/${editing.id}`, {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(stripEmpty(editing)),
          })
      if (!res.ok) {
        setError(await res.text())
        return
      }
      setMsg(isNew ? "已添加" : "已保存")
      setEditing(null)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : "保存失败")
    } finally {
      setBusy(false)
    }
  }

  async function remove(id: string) {
    setBusy(true)
    setError(null)
    try {
      const res = await fetch(`/api/rules/${id}`, { method: "DELETE" })
      if (!res.ok) {
        setError(await res.text())
        return
      }
      setMsg("已删除")
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : "删除失败")
    } finally {
      setBusy(false)
    }
  }

  async function toggle(r: Rule) {
    setBusy(true)
    try {
      await fetch(`/api/rules/${r.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...r, enabled: !r.enabled }),
      })
      await load()
    } finally {
      setBusy(false)
    }
  }

  /* 载入内置规则集(整包替换): 后端 PUT /api/rules */
  async function loadBuiltin() {
    if (!window.confirm("载入内置规则将整包替换当前规则, 继续?")) return
    setBusy(true)
    setError(null)
    try {
      const res = await fetch("/api/rules", { method: "PUT" })
      if (!res.ok) {
        setError(await res.text())
        return
      }
      setMsg("已载入内置规则")
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : "载入失败")
    } finally {
      setBusy(false)
    }
  }

  const typeHint = useMemo(() => RULE_TYPES.find((t) => t.value === editing?.type)?.hint ?? "", [editing?.type])

  return (
    <div className="flex h-full min-h-0 flex-col gap-2 border-l border-border p-3" data-testid="rules-panel">
      <div className="flex shrink-0 items-center justify-between">
        <h2 className="text-sm font-semibold">规则</h2>
        {editing ? (
          <Button size="xs" variant="ghost" onClick={() => setEditing(null)} data-testid="rules-editor-close">
            <X className="h-3 w-3" /> 关闭
          </Button>
        ) : (
          <div className="flex items-center gap-1">
            <Button
              size="xs"
              variant="ghost"
              disabled={busy}
              onClick={() => void loadBuiltin()}
              title="载入内置规则集(整包替换当前规则): 禁用缓存/明文编码/CORS/去CSP/流量高亮"
              data-testid="rules-load-builtin"
            >
              <Library className="h-3 w-3" /> 内置
            </Button>
            <Button size="xs" variant="outline" onClick={startAdd} data-testid="rules-add">
              <Plus className="h-3 w-3" /> 新建
            </Button>
          </div>
        )}
      </div>

      {error ? <p className="shrink-0 text-xs text-destructive">{error}</p> : null}
      {msg && !error ? <p className="shrink-0 text-xs text-emerald-500">{msg}</p> : null}

      {editing ? (
        <RuleEditor
          rule={editing}
          isNew={isNew}
          busy={busy}
          typeHint={typeHint}
          onChange={setEditing}
          onSave={() => void save()}
        />
      ) : (
        <ul className="min-h-0 flex-1 space-y-1 overflow-auto text-xs" data-testid="rules-list">
          {rules.map((r) => (
            <li key={r.id} className="flex items-start justify-between gap-2 rounded border border-border px-2 py-1.5">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                  <span
                    className={cn(
                      "rounded px-1 py-0.5 text-[10px] font-medium",
                      r.enabled ? "bg-primary/15 text-primary" : "bg-muted text-muted-foreground",
                    )}
                  >
                    {TYPE_LABEL[r.type] ?? r.type}
                  </span>
                  {r.id.startsWith("builtin-") ? (
                    <span className="rounded bg-amber-500/15 px-1 py-0.5 text-[10px] font-medium text-amber-500">
                      内置
                    </span>
                  ) : null}
                  {!r.enabled ? <span className="text-[10px] text-muted-foreground">(停用)</span> : null}
                </div>
                <p className="mt-1 break-all font-mono text-[10px] text-muted-foreground">{summarize(r)}</p>
              </div>
              <div className="flex shrink-0 items-center gap-0.5">
                <button
                  title={r.enabled ? "停用" : "启用"}
                  className="h-4 w-7 rounded-full border border-border p-0.5 transition-colors"
                  onClick={() => void toggle(r)}
                  data-testid={`rules-toggle-${r.id}`}
                >
                  <span
                    className={cn(
                      "block h-2.5 w-2.5 rounded-full transition-transform",
                      r.enabled ? "translate-x-3 bg-primary" : "bg-muted-foreground",
                    )}
                  />
                </button>
                <Button size="icon-xs" variant="ghost" onClick={() => startEdit(r)} data-testid={`rules-edit-${r.id}`}>
                  <Pencil className="h-3 w-3" />
                </Button>
                <Button
                  size="icon-xs"
                  variant="ghost"
                  disabled={busy}
                  onClick={() => void remove(r.id)}
                  data-testid={`rules-del-${r.id}`}
                >
                  <Trash2 className="h-3 w-3" />
                </Button>
              </div>
            </li>
          ))}
          {rules.length === 0 ? <li className="text-muted-foreground">无规则</li> : null}
        </ul>
      )}
    </div>
  )
}

/* ------------------------------------------------------------------ */
/* 编辑器                                                              */
/* ------------------------------------------------------------------ */

function RuleEditor({
  rule,
  isNew,
  busy,
  typeHint,
  onChange,
  onSave,
}: {
  rule: Rule
  isNew: boolean
  busy: boolean
  typeHint: string
  onChange: (r: Rule) => void
  onSave: () => void
}) {
  /* 切类型时按新类型补一份默认字段, 免得后端拿到半截结构 */
  function setType(t: string) {
    const next: Rule = { ...rule, type: t }
    if (t === "body_replace" && !next.body_replace) next.body_replace = { phase: "response", from: "", to: "" }
    if (t === "header" && !next.header) next.header = { phase: "response", set: {}, remove: [] }
    if (t === "map_local" && !next.map_local) next.map_local = { status: 200, file: "" }
    if (t === "access_control" && !next.access_control) next.access_control = { cidrs: [], action: "allow" }
    if (t === "gateway" && !next.gateway) next.gateway = { upstream: "" }
    if (t === "mirror_remote" && !next.mirror_remote) next.mirror_remote = { url: "" }
    if (t === "secondary_proxy" && !next.secondary_proxy) next.secondary_proxy = { proxy_url: "" }
    if (t === "reverse_proxy" && !next.reverse_proxy) next.reverse_proxy = { listen: "", target: "" }
    if (t === "highlight" && !next.highlight) next.highlight = { color: "#f59e0b", label: "" }
    onChange(next)
  }

  function setMatch(patch: Partial<Match>) {
    onChange({ ...rule, match: { ...rule.match, ...patch } })
  }

  const methodsText = (rule.match.methods ?? []).join(",")

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto pr-1" data-testid="rules-editor">
      {/* 类型 + 启用 */}
      <div>
        <label className={labelCls}>规则类型</label>
        <select
          className={inputCls}
          value={rule.type}
          onChange={(e) => setType(e.target.value)}
          data-testid="rules-editor-type"
        >
          {RULE_TYPES.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
        {typeHint ? <p className="mt-1 text-[10px] leading-relaxed text-muted-foreground">{typeHint}</p> : null}
      </div>

      <label className="flex items-center gap-2 text-xs">
        <input
          type="checkbox"
          checked={rule.enabled}
          onChange={(e) => onChange({ ...rule, enabled: e.target.checked })}
          data-testid="rules-editor-enabled"
        />
        启用该规则
      </label>

      {/* 匹配条件 */}
      <fieldset className="space-y-2 rounded border border-border/70 p-2">
        <legend className="px-1 text-[11px] font-medium text-muted-foreground">匹配条件(空 = 不限)</legend>
        <div>
          <label className={labelCls}>域名(支持 * 通配, 如 *.example.com)</label>
          <input
            className={inputCls}
            value={rule.match.host ?? ""}
            placeholder="*.example.com"
            onChange={(e) => setMatch({ host: e.target.value })}
            data-testid="rules-editor-host"
          />
        </div>
        <div>
          <label className={labelCls}>路径(正则, 非空时按正则匹配)</label>
          <input
            className={cn(inputCls, "font-mono")}
            value={rule.match.path ?? ""}
            placeholder="/api/.*"
            onChange={(e) => setMatch({ path: e.target.value })}
            data-testid="rules-editor-path"
          />
        </div>
        <div>
          <label className={labelCls}>方法(逗号分隔, 空 = 任意)</label>
          <input
            className={inputCls}
            value={methodsText}
            placeholder="GET,POST"
            onChange={(e) =>
              setMatch({
                methods: e.target.value
                  .split(",")
                  .map((s) => s.trim().toUpperCase())
                  .filter(Boolean),
              })
            }
            data-testid="rules-editor-methods"
          />
        </div>
      </fieldset>

      {/* 按类型渲染字段 */}
      {rule.type === "body_replace" && rule.body_replace ? (
        <fieldset className="space-y-2 rounded border border-border/70 p-2">
          <legend className="px-1 text-[11px] font-medium text-muted-foreground">内容替换</legend>
          <div>
            <label className={labelCls}>阶段</label>
            <select
              className={inputCls}
              value={rule.body_replace.phase}
              onChange={(e) => onChange({ ...rule, body_replace: { ...rule.body_replace!, phase: e.target.value } })}
              data-testid="rules-editor-br-phase"
            >
              <option value="response">响应(返回客户端前)</option>
              <option value="request">请求(发往服务器前)</option>
            </select>
          </div>
          <div>
            <label className={labelCls}>查找(原内容)</label>
            <textarea
              className={cn(inputCls, "min-h-16 font-mono")}
              value={rule.body_replace.from}
              onChange={(e) => onChange({ ...rule, body_replace: { ...rule.body_replace!, from: e.target.value } })}
              data-testid="rules-editor-br-from"
            />
          </div>
          <div>
            <label className={labelCls}>替换为</label>
            <textarea
              className={cn(inputCls, "min-h-16 font-mono")}
              value={rule.body_replace.to}
              onChange={(e) => onChange({ ...rule, body_replace: { ...rule.body_replace!, to: e.target.value } })}
              data-testid="rules-editor-br-to"
            />
          </div>
          <p className="text-[10px] leading-relaxed text-amber-500">
            注意: 仅对未压缩响应生效。若响应带 gzip/br, 需另加一条「头改写」请求规则把 Accept-Encoding 改为 identity。
          </p>
        </fieldset>
      ) : null}

      {rule.type === "header" && rule.header ? (
        <fieldset className="space-y-2 rounded border border-border/70 p-2">
          <legend className="px-1 text-[11px] font-medium text-muted-foreground">头改写</legend>
          <div>
            <label className={labelCls}>阶段</label>
            <select
              className={inputCls}
              value={rule.header.phase}
              onChange={(e) => onChange({ ...rule, header: { ...rule.header!, phase: e.target.value } })}
              data-testid="rules-editor-hd-phase"
            >
              <option value="request">请求</option>
              <option value="response">响应</option>
            </select>
          </div>
          <HeaderKVRows
            title="设置"
            rows={rule.header.set ?? {}}
            onChange={(set) => onChange({ ...rule, header: { ...rule.header!, set } })}
            testid="rules-editor-hd-set"
          />
          <div>
            <label className={labelCls}>删除(头名, 逗号分隔)</label>
            <input
              className={inputCls}
              value={(rule.header.remove ?? []).join(",")}
              placeholder="Cookie,Set-Cookie"
              onChange={(e) =>
                onChange({
                  ...rule,
                  header: {
                    ...rule.header!,
                    remove: e.target.value
                      .split(",")
                      .map((s) => s.trim())
                      .filter(Boolean),
                  },
                })
              }
              data-testid="rules-editor-hd-remove"
            />
          </div>
        </fieldset>
      ) : null}

      {rule.type === "map_local" && rule.map_local ? (
        <fieldset className="space-y-2 rounded border border-border/70 p-2">
          <legend className="px-1 text-[11px] font-medium text-muted-foreground">本地文件映射</legend>
          <div>
            <label className={labelCls}>文件(data-dir/map-local/ 下的相对路径)</label>
            <input
              className={cn(inputCls, "font-mono")}
              value={rule.map_local.file}
              placeholder="mock/vip.json"
              onChange={(e) => onChange({ ...rule, map_local: { ...rule.map_local!, file: e.target.value } })}
              data-testid="rules-editor-ml-file"
            />
          </div>
          <div>
            <label className={labelCls}>状态码(0 = 200)</label>
            <input
              className={inputCls}
              type="number"
              value={rule.map_local.status || ""}
              placeholder="200"
              onChange={(e) =>
                onChange({ ...rule, map_local: { ...rule.map_local!, status: Number(e.target.value) || 0 } })
              }
              data-testid="rules-editor-ml-status"
            />
          </div>
        </fieldset>
      ) : null}

      {rule.type === "mirror_remote" && rule.mirror_remote ? (
        <fieldset className="space-y-2 rounded border border-border/70 p-2">
          <legend className="px-1 text-[11px] font-medium text-muted-foreground">镜像远端</legend>
          <div>
            <label className={labelCls}>URL</label>
            <input
              className={cn(inputCls, "font-mono")}
              value={rule.mirror_remote.url}
              placeholder="https://other.example.com/api"
              onChange={(e) => onChange({ ...rule, mirror_remote: { url: e.target.value } })}
              data-testid="rules-editor-mr-url"
            />
          </div>
        </fieldset>
      ) : null}

      {rule.type === "gateway" && rule.gateway ? (
        <fieldset className="space-y-2 rounded border border-border/70 p-2">
          <legend className="px-1 text-[11px] font-medium text-muted-foreground">上游网关</legend>
          <div>
            <label className={labelCls}>上游(host 或 http://host:port)</label>
            <input
              className={cn(inputCls, "font-mono")}
              value={rule.gateway.upstream}
              placeholder="10.0.0.2:8080"
              onChange={(e) => onChange({ ...rule, gateway: { upstream: e.target.value } })}
              data-testid="rules-editor-gw-upstream"
            />
          </div>
        </fieldset>
      ) : null}

      {rule.type === "secondary_proxy" && rule.secondary_proxy ? (
        <fieldset className="space-y-2 rounded border border-border/70 p-2">
          <legend className="px-1 text-[11px] font-medium text-muted-foreground">二级代理</legend>
          <div>
            <label className={labelCls}>代理 URL</label>
            <input
              className={cn(inputCls, "font-mono")}
              value={rule.secondary_proxy.proxy_url}
              placeholder="http://127.0.0.1:7890"
              onChange={(e) => onChange({ ...rule, secondary_proxy: { proxy_url: e.target.value } })}
              data-testid="rules-editor-sp-url"
            />
          </div>
        </fieldset>
      ) : null}

      {rule.type === "reverse_proxy" && rule.reverse_proxy ? (
        <fieldset className="space-y-2 rounded border border-border/70 p-2">
          <legend className="px-1 text-[11px] font-medium text-muted-foreground">反向代理</legend>
          <div>
            <label className={labelCls}>监听(host:port)</label>
            <input
              className={cn(inputCls, "font-mono")}
              value={rule.reverse_proxy.listen}
              placeholder="127.0.0.1:9090"
              onChange={(e) => onChange({ ...rule, reverse_proxy: { ...rule.reverse_proxy!, listen: e.target.value } })}
              data-testid="rules-editor-rp-listen"
            />
          </div>
          <div>
            <label className={labelCls}>目标(host:port 或 URL)</label>
            <input
              className={cn(inputCls, "font-mono")}
              value={rule.reverse_proxy.target}
              placeholder="127.0.0.1:8080"
              onChange={(e) => onChange({ ...rule, reverse_proxy: { ...rule.reverse_proxy!, target: e.target.value } })}
              data-testid="rules-editor-rp-target"
            />
          </div>
        </fieldset>
      ) : null}

      {rule.type === "access_control" && rule.access_control ? (
        <fieldset className="space-y-2 rounded border border-border/70 p-2">
          <legend className="px-1 text-[11px] font-medium text-muted-foreground">访问控制</legend>
          <div>
            <label className={labelCls}>CIDR(逗号分隔)</label>
            <input
              className={cn(inputCls, "font-mono")}
              value={(rule.access_control.cidrs ?? []).join(",")}
              placeholder="192.168.1.0/24,10.0.0.5"
              onChange={(e) =>
                onChange({
                  ...rule,
                  access_control: {
                    ...rule.access_control!,
                    cidrs: e.target.value
                      .split(",")
                      .map((s) => s.trim())
                      .filter(Boolean),
                  },
                })
              }
              data-testid="rules-editor-ac-cidrs"
            />
          </div>
          <div>
            <label className={labelCls}>动作</label>
            <select
              className={inputCls}
              value={rule.access_control.action ?? "allow"}
              onChange={(e) => onChange({ ...rule, access_control: { ...rule.access_control!, action: e.target.value } })}
              data-testid="rules-editor-ac-action"
            >
              <option value="allow">放行</option>
              <option value="deny">拒绝</option>
            </select>
          </div>
        </fieldset>
      ) : null}

      {rule.type === "highlight" && rule.highlight ? (
        <fieldset className="space-y-2 rounded border border-border/70 p-2">
          <legend className="px-1 text-[11px] font-medium text-muted-foreground">高亮标记</legend>
          <div>
            <label className={labelCls}>颜色</label>
            <div className="flex items-center gap-2">
              <input
                type="color"
                className="h-7 w-10 rounded border border-border bg-background"
                value={/^#[0-9a-fA-F]{6}$/.test(rule.highlight.color) ? rule.highlight.color : "#f59e0b"}
                onChange={(e) => onChange({ ...rule, highlight: { ...rule.highlight!, color: e.target.value } })}
                data-testid="rules-editor-hl-color"
              />
              <input
                className={cn(inputCls, "font-mono")}
                value={rule.highlight.color}
                onChange={(e) => onChange({ ...rule, highlight: { ...rule.highlight!, color: e.target.value } })}
              />
            </div>
          </div>
          <div>
            <label className={labelCls}>标签(可选)</label>
            <input
              className={inputCls}
              value={rule.highlight.label ?? ""}
              placeholder="广告"
              onChange={(e) => onChange({ ...rule, highlight: { ...rule.highlight!, label: e.target.value } })}
              data-testid="rules-editor-hl-label"
            />
          </div>
        </fieldset>
      ) : null}

      {MATCH_ONLY_TYPES.has(rule.type) ? (
        <p className="text-[10px] leading-relaxed text-muted-foreground">该类型只需匹配条件, 命中即生效。</p>
      ) : null}

      <div className="sticky bottom-0 -mx-3 mt-auto flex shrink-0 gap-2 border-t border-border bg-card px-3 py-2">
        <Button size="sm" className="flex-1" disabled={busy} onClick={onSave} data-testid="rules-editor-save">
          {busy ? "保存中…" : isNew ? "添加规则" : "保存修改"}
        </Button>
      </div>
    </div>
  )
}

/* 头改写的 set 键值对编辑器 */
function HeaderKVRows({
  title,
  rows,
  onChange,
  testid,
}: {
  title: string
  rows: Record<string, string>
  onChange: (set: Record<string, string>) => void
  testid: string
}) {
  const entries = Object.entries(rows)
  function setAt(key: string, nk: string, nv: string) {
    const next: Record<string, string> = {}
    for (const [k, v] of entries) {
      if (k === key) next[nk] = nv
      else next[k] = v
    }
    onChange(next)
  }
  function add() {
    onChange({ ...rows, "": "" })
  }
  function del(key: string) {
    const next: Record<string, string> = {}
    for (const [k, v] of entries) if (k !== key) next[k] = v
    onChange(next)
  }
  return (
    <div>
      <div className="mb-1 flex items-center justify-between">
        <label className="text-[11px] font-medium text-muted-foreground">{title}</label>
        <Button size="xs" variant="ghost" onClick={add} data-testid={`${testid}-add`}>
          <Plus className="h-3 w-3" /> 加一行
        </Button>
      </div>
      <div className="space-y-1">
        {entries.length === 0 ? <p className="text-[10px] text-muted-foreground">(无)</p> : null}
        {entries.map(([k, v]) => (
          <div key={k} className="flex items-center gap-1">
            <input
              className={cn(inputCls, "w-2/5 font-mono")}
              value={k}
              placeholder="头名"
              onChange={(e) => setAt(k, e.target.value, v)}
              data-testid={`${testid}-key`}
            />
            <input
              className={cn(inputCls, "flex-1 font-mono")}
              value={v}
              placeholder="值"
              onChange={(e) => setAt(k, k, e.target.value)}
              data-testid={`${testid}-val`}
            />
            <Button size="icon-xs" variant="ghost" onClick={() => del(k)}>
              <X className="h-3 w-3" />
            </Button>
          </div>
        ))}
      </div>
    </div>
  )
}

/* 去掉空字段, 别把 undefined/空结构发给后端 */
function stripEmpty(r: Rule): Rule {
  const out: Rule = {
    id: r.id,
    enabled: r.enabled,
    type: r.type,
    match: {
      host: r.match.host || undefined,
      path: r.match.path || undefined,
      methods: r.match.methods && r.match.methods.length ? r.match.methods : undefined,
    },
  }
  if (r.header) {
    const set = Object.fromEntries(Object.entries(r.header.set ?? {}).filter(([k]) => k !== ""))
    out.header = {
      phase: r.header.phase,
      set: Object.keys(set).length ? set : undefined,
      remove: r.header.remove && r.header.remove.length ? r.header.remove : undefined,
    }
  }
  if (r.body_replace)
    out.body_replace = { phase: r.body_replace.phase, from: r.body_replace.from, to: r.body_replace.to }
  if (r.map_local) out.map_local = { status: r.map_local.status, file: r.map_local.file }
  if (r.access_control) out.access_control = { cidrs: r.access_control.cidrs, action: r.access_control.action }
  if (r.gateway) out.gateway = { upstream: r.gateway.upstream }
  if (r.mirror_remote) out.mirror_remote = { url: r.mirror_remote.url }
  if (r.secondary_proxy) out.secondary_proxy = { proxy_url: r.secondary_proxy.proxy_url }
  if (r.reverse_proxy) out.reverse_proxy = { listen: r.reverse_proxy.listen, target: r.reverse_proxy.target }
  if (r.highlight) out.highlight = { color: r.highlight.color, label: r.highlight.label || undefined }
  return out
}
