import { useMemo } from "react"

import { FilterBar } from "@/components/filter-bar"
import { TrafficDetail } from "@/components/traffic-detail"
import { flowHost, flowPath } from "@/lib/api"
import { bytesOf, durationMs, formatMs, formatSize, shortAddr } from "@/lib/stats"
import { methodClass, statusClass } from "@/lib/tones"
import type { FlowDTO, FlowFilter } from "@/lib/types"

type Props = {
  readonly flows: readonly FlowDTO[]
  readonly selectedId: string | null
  readonly onSelect: (id: string | null) => void
  readonly selected: FlowDTO | null
  readonly loading: boolean
  readonly filter: FlowFilter
  readonly onFilter: (f: FlowFilter) => void
  readonly focusDevice: string | null
  readonly onClearFocus: () => void
}

function timeOf(f: FlowDTO): string {
  const t = Date.parse(f.startedAt)
  if (Number.isNaN(t)) return "—"
  const d = new Date(t)
  const p = (n: number) => String(n).padStart(2, "0")
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export function SessionsPanel({
  flows,
  selectedId,
  onSelect,
  selected,
  loading,
  filter,
  onFilter,
  focusDevice,
  onClearFocus,
}: Props) {
  const maxDur = Math.max(1, ...flows.map((f) => durationMs(f)))

  const groups = useMemo(() => {
    const m = new Map<string, FlowDTO[]>()
    for (const f of flows) {
      const key = shortAddr(f.clientAddr ?? "")
      const arr = m.get(key) ?? []
      arr.push(f)
      m.set(key, arr)
    }
    return [...m.entries()]
  }, [flows])

  return (
    <div className="nl-sessions">
      {focusDevice ? (
        <div className="nl-focus">
          已聚焦设备 <b>{focusDevice}</b> 的会话时间轴
          <button type="button" onClick={onClearFocus}>查看全网</button>
        </div>
      ) : null}

      <div className="nl-filter">
        <FilterBar filter={filter} onChange={onFilter} />
      </div>

      {loading && flows.length === 0 ? (
        <div className="nl-empty">
          <h3>正在载入会话…</h3>
          <p>从网关引擎拉取已记录的流量切片。</p>
        </div>
      ) : flows.length === 0 ? (
        <div className="nl-empty">
          <h3>时间轴还是空的</h3>
          <p>
            巡流把每条会话按发生时刻排成瀑布，并按设备分组，方便回看「某台设备在某段时间做了什么」。
            在插件页启动引擎、勾选抓包设备、点「继续记录」，并在客户端安装 CA 证书后即可看到数据。
          </p>
        </div>
      ) : (
        groups.map(([device, list]) => {
          const total = list.reduce((n, f) => n + bytesOf(f), 0)
          return (
            <section className="nl-group" key={device}>
              <div className="nl-group-head">
                <span className="nl-dot" style={{ background: "#0f766e" }} />
                <b>{device}</b>
                <span className="nl-group-meta">
                  {list.length} 会话 · {formatSize(total)}
                </span>
              </div>
              <div className="nl-waterfall">
                {list.map((f) => {
                  const method = (f.request.method ?? "—").toUpperCase()
                  const status = f.response.status ?? 0
                  const dur = durationMs(f)
                  return (
                    <button
                      type="button"
                      key={f.id}
                      className={`nl-row${selectedId === f.id ? " nl-row--selected" : ""}`}
                      onClick={() => onSelect(f.id)}
                      title="展开这一条会话的原始报文"
                    >
                      <span className="nl-row-time">{timeOf(f)}</span>
                      <span className={`nl-badge ${methodClass(method)}`}>{method}</span>
                      <span className="nl-row-main">
                        <span className="nl-row-host">{flowHost(f) || "—"}</span>
                        <span className="nl-row-path">{flowPath(f)}</span>
                      </span>
                      <span className="nl-row-bar" title={`耗时 ${formatMs(dur)}`}>
                        <i style={{ width: `${Math.max(4, (dur / maxDur) * 100)}%` }} />
                      </span>
                      <span className={`nl-badge ${statusClass(status)}`}>
                        {status > 0 ? status : "—"}
                      </span>
                      <span className="nl-row-meta">{formatSize(bytesOf(f))}</span>
                    </button>
                  )
                })}
              </div>
            </section>
          )
        })
      )}

      {selected ? (
        <>
          <div className="nl-drawer-backdrop" onClick={() => onSelect(null)} />
          <aside className="nl-drawer" data-testid="session-drawer">
            <div className="nl-drawer-head">
              <h3>会话切片</h3>
              <span>点开时间轴上的任一行，查看原始请求 / 响应</span>
              <button
                type="button"
                className="nl-drawer-close"
                aria-label="关闭"
                onClick={() => onSelect(null)}
              >
                ✕
              </button>
            </div>
            <div className="nl-drawer-body">
              <TrafficDetail flow={selected} />
            </div>
          </aside>
        </>
      ) : null}
    </div>
  )
}
