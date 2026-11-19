import { Download, Eraser, Pause, Play, Shield } from "lucide-react"

import { caPemURL, exportHarURL } from "@/lib/api"

export type ViewId = "overview" | "devices" | "sessions" | "insights" | "toolbox"
export type ToolId = "rules" | "subs" | "rest" | "explorer" | "scripts"

const VIEWS: { id: ViewId; label: string; hint: string }[] = [
  { id: "overview", label: "总览", hint: "全网流量脉搏与关键指标" },
  { id: "devices", label: "设备星图", hint: "按局域网设备聚合的流量画像" },
  { id: "sessions", label: "会话瀑布", hint: "按时间轴回溯的会话切片" },
  { id: "insights", label: "域名洞察", hint: "域名热度、状态构成与慢端点" },
]

const TOOLS: { id: ToolId; label: string }[] = [
  { id: "rules", label: "规则" },
  { id: "subs", label: "订阅" },
  { id: "rest", label: "编写" },
  { id: "explorer", label: "浏览" },
  { id: "scripts", label: "脚本" },
]

function BrandMark() {
  return (
    <svg className="nl-brand-mark" viewBox="0 0 64 64" fill="none" aria-hidden="true">
      <path
        d="M12 20c10 0 10 12 20 12s10-12 20-12"
        stroke="#14b8a6"
        strokeWidth="3"
        strokeLinecap="round"
      />
      <path
        d="M12 32c10 0 10 12 20 12s10-12 20-12"
        stroke="#7fd8c7"
        strokeWidth="3"
        strokeLinecap="round"
        strokeOpacity="0.75"
      />
      <path
        d="M12 44c10 0 10-12 20-12s10 12 20 12"
        stroke="#e0a23c"
        strokeWidth="3"
        strokeLinecap="round"
      />
      <circle cx="32" cy="32" r="5.5" fill="#0e211c" stroke="#eaf3ee" strokeWidth="2.5" />
    </svg>
  )
}

type HeaderProps = {
  readonly flowCount: number
  readonly deviceCount: number
  readonly capturePaused: boolean
  readonly error: string | null
  readonly busy: boolean
  readonly onTogglePause: () => void
  readonly onClear: () => void
}

export function ConsoleHeader({
  flowCount,
  deviceCount,
  capturePaused,
  error,
  busy,
  onTogglePause,
  onClear,
}: HeaderProps) {
  return (
    <header className="nl-ribbon">
      <div className="nl-brand">
        <BrandMark />
        <div>
          <h1 className="nl-brand-name">
            巡流 NetLoom
          </h1>
          <p className="nl-brand-sub">局域网流量观测台 · 运行于网关</p>
        </div>
      </div>

      <span className="nl-ribbon-spacer" />

      {error ? <span className="nl-ribbon-error" title={error}>{error}</span> : null}

      <span className={`nl-state${capturePaused ? " nl-state--paused" : ""}`}>
        {capturePaused ? <span className="nl-dot" style={{ background: "#e0a23c" }} /> : <span className="live-dot" />}
        {capturePaused ? "记录已暂停" : "实时观测中"}
      </span>
      <span className="nl-ribbon-count">
        {flowCount.toLocaleString()} 会话 / {deviceCount} 设备
      </span>

      <div className="nl-ribbon-actions">
        <button
          type="button"
          className={`nl-btn${capturePaused ? " nl-btn--primary" : ""}`}
          onClick={onTogglePause}
          disabled={busy}
          data-testid="toggle-pause"
        >
          {capturePaused ? <Play size={14} /> : <Pause size={14} />}
          {capturePaused ? "继续记录" : "暂停记录"}
        </button>
        <button
          type="button"
          className="nl-btn"
          onClick={onClear}
          disabled={busy || flowCount === 0}
          data-testid="clear-flows"
        >
          <Eraser size={14} />
          清空
        </button>
        <a className="nl-btn" href={exportHarURL()} download="netloom.har" data-testid="export-har">
          <Download size={14} />
          导出 HAR
        </a>
        <a className="nl-btn" href={caPemURL()} download="netloom-ca.pem" data-testid="download-ca">
          <Shield size={14} />
          CA 证书
        </a>
      </div>
    </header>
  )
}

type DeckProps = {
  readonly view: ViewId
  readonly onChange: (v: ViewId) => void
  readonly flowCount: number
  readonly tool: ToolId
  readonly onToolChange: (t: ToolId) => void
}

export function ViewDeck({ view, onChange, flowCount, tool, onToolChange }: DeckProps) {
  return (
    <>
      <div className="nl-deck">
        <div className="nl-deck-tabs">
          {VIEWS.map((v) => (
            <button
              key={v.id}
              type="button"
              title={v.hint}
              data-testid={`view-${v.id}`}
              className={`nl-deck-tab${view === v.id ? " nl-deck-tab--active" : ""}`}
              onClick={() => onChange(v.id)}
            >
              {v.label}
              {v.id === "sessions" ? <span className="nl-deck-num">{flowCount}</span> : null}
            </button>
          ))}
        </div>
        <span className="nl-deck-divider" />
        <button
          type="button"
          title="单请求调试工具(次级)"
          data-testid="view-toolbox"
          className={`nl-deck-tab${view === "toolbox" ? " nl-deck-tab--active" : ""}`}
          onClick={() => onChange("toolbox")}
        >
          运维工具箱
        </button>
        <span className="nl-ribbon-spacer" />
        <span className="nl-deck-tag">GATEWAY CONSOLE</span>
      </div>

      {view === "toolbox" ? (
        <div className="nl-deck-sub">
          <span className="nl-deck-sub-label">次级工具</span>
          {TOOLS.map((t) => (
            <button
              key={t.id}
              type="button"
              className={`nl-deck-tool${tool === t.id ? " nl-deck-tool--active" : ""}`}
              onClick={() => onToolChange(t.id)}
            >
              {t.label}
            </button>
          ))}
        </div>
      ) : null}
    </>
  )
}

type FooterProps = {
  readonly flowCount: number
  readonly deviceCount: number
  readonly capturePaused: boolean
}

export function ConsoleFooter({ flowCount, deviceCount, capturePaused }: FooterProps) {
  return (
    <footer className="nl-foot">
      <span>
        巡流 NetLoom · <strong>运行于网关</strong>：抓包、解密与统计全部在路由器本地完成，数据不出局域网
      </span>
      <span className="nl-foot-spacer" />
      <span>
        观测会话 <strong>{flowCount}</strong> · 活跃设备 <strong>{deviceCount}</strong> · 记录状态{" "}
        <strong>{capturePaused ? "已暂停" : "实时"}</strong>
      </span>
    </footer>
  )
}
