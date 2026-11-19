import { formatSize, type Stats } from "@/lib/stats"

type Props = {
  readonly stats: Stats
  readonly onOpenDevices: () => void
}

const SEGMENTS: { key: keyof Stats["status"]; label: string; color: string }[] = [
  { key: "ok", label: "2xx 正常", color: "#0f766e" },
  { key: "redirect", label: "3xx 重定向", color: "#3b6ea8" },
  { key: "clientErr", label: "4xx 客户端", color: "#c2740a" },
  { key: "serverErr", label: "5xx 服务端", color: "#b3453b" },
  { key: "pending", label: "进行中/未知", color: "#a8b9ab" },
]

function Kpi({
  label,
  value,
  note,
  tone,
}: {
  readonly label: string
  readonly value: string
  readonly note: string
  readonly tone?: string
}) {
  return (
    <div className={`nl-kpi${tone ? ` ${tone}` : ""}`}>
      <div className="nl-kpi-label">{label}</div>
      <div className="nl-kpi-value">{value}</div>
      <div className="nl-kpi-note">{note}</div>
    </div>
  )
}

export function OverviewPanel({ stats, onOpenDevices }: Props) {
  const maxBucket = Math.max(1, ...stats.buckets.map((b) => b.count))
  const totalStatus = Math.max(
    1,
    stats.status.ok + stats.status.redirect + stats.status.clientErr + stats.status.serverErr + stats.status.pending,
  )
  const topDevices = stats.devices.slice(0, 6)
  const maxDeviceBytes = Math.max(1, ...topDevices.map((d) => d.bytes))

  return (
    <div className="nl-grid nl-grid--overview">
      <div className="nl-span-12">
        <div className="nl-kpis">
          <Kpi
            label="观测会话"
            value={stats.total.toLocaleString()}
            note="本次记录的全部请求/响应"
          />
          <Kpi
            label="活跃设备"
            value={String(stats.devices.length)}
            note="按 client 地址聚合的局域网终端"
            tone="nl-kpi--amber"
          />
          <Kpi
            label="累计流量"
            value={formatSize(stats.bytes)}
            note="请求体 + 响应体合计"
            tone="nl-kpi--blue"
          />
          <Kpi
            label="异常率"
            value={`${stats.errorRate.toFixed(1)}%`}
            note="错误/4xx/5xx 会话占比"
            tone="nl-kpi--red"
          />
        </div>
      </div>

      <section className="nl-card nl-span-8">
        <div className="nl-card-head">
          <h2 className="nl-card-title">流量脉搏</h2>
          <span className="nl-card-sub">
            观测窗口 {new Date(stats.from).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })} –{" "}
            {new Date(stats.to).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })} · 24 格
          </span>
        </div>
        <div className="nl-card-body">
          <div className="nl-pulse">
            {stats.buckets.map((b, i) => (
              <div
                className="nl-pulse-col"
                key={i}
                title={`${b.label} · ${b.count} 次会话 / ${formatSize(b.bytes)}`}
              >
                <div className="nl-pulse-track">
                  <div
                    className="nl-pulse-bar"
                    style={{ height: `${Math.max(2, (b.count / maxBucket) * 100)}%` }}
                  />
                </div>
                <span className="nl-pulse-label">{i % 4 === 0 ? b.label : ""}</span>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="nl-card nl-span-4">
        <div className="nl-card-head">
          <h2 className="nl-card-title">状态构成</h2>
          <span className="nl-card-sub">全网响应码分布</span>
        </div>
        <div className="nl-card-body">
          <div className="nl-stack">
            {SEGMENTS.map((s) => (
              <div
                key={s.key}
                className="nl-stack-seg"
                style={{
                  width: `${(stats.status[s.key] / totalStatus) * 100}%`,
                  background: s.color,
                }}
                title={`${s.label}: ${stats.status[s.key]}`}
              />
            ))}
          </div>
          <div className="nl-legend">
            {SEGMENTS.map((s) => (
              <span className="nl-legend-item" key={s.key}>
                <span className="nl-dot" style={{ background: s.color }} />
                {s.label} <b>{stats.status[s.key]}</b>
              </span>
            ))}
          </div>
          <div className="nl-chips">
            {stats.methods.slice(0, 6).map((m) => (
              <span className="nl-chip" key={m.name}>
                {m.name} <b>{m.count}</b>
              </span>
            ))}
            {stats.methods.length === 0 ? <span className="nl-card-sub">暂无数据</span> : null}
          </div>
        </div>
      </section>

      <section className="nl-card nl-span-7">
        <div className="nl-card-head">
          <h2 className="nl-card-title">设备流量排行</h2>
          <span className="nl-card-sub">按累计字节</span>
          <button type="button" className="nl-card-action" onClick={onOpenDevices}>
            查看星图 →
          </button>
        </div>
        <div className="nl-card-body">
          {topDevices.length === 0 ? (
            <p className="nl-card-sub">暂无设备流量。启动引擎并勾选抓包设备后，这里会按终端聚合。</p>
          ) : (
            <div className="nl-rank">
              {topDevices.map((d) => (
                <div className="nl-rank-row" key={d.short}>
                  <span className="nl-rank-name" title={d.addr}>{d.short}</span>
                  <span className="nl-rank-track">
                    <span
                      className="nl-rank-fill"
                      style={{ width: `${(d.bytes / maxDeviceBytes) * 100}%`, display: "block" }}
                    />
                  </span>
                  <span className="nl-rank-val">{formatSize(d.bytes)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </section>

      <section className="nl-card nl-span-5">
        <div className="nl-card-head">
          <h2 className="nl-card-title">观测台视角</h2>
          <span className="nl-card-sub">网关原生，非单机调试器</span>
        </div>
        <div className="nl-card-body">
          <div className="nl-legend" style={{ flexDirection: "column", gap: "12px", marginTop: 0 }}>
            <span className="nl-legend-item">
              <span className="nl-dot" style={{ background: "#0f766e" }} />
              按 <b>局域网设备</b> 聚合，而非按某个 App
            </span>
            <span className="nl-legend-item">
              <span className="nl-dot" style={{ background: "#c2740a" }} />
              按 <b>时间轴</b> 回溯，而非平铺请求列表
            </span>
            <span className="nl-legend-item">
              <span className="nl-dot" style={{ background: "#3b6ea8" }} />
              先给 <b>域名画像</b>，再看单条报文
            </span>
          </div>
          <div className="nl-chips">
            <span className="nl-chip">Top 域名 <b>{stats.hosts.length}</b></span>
            <span className="nl-chip">观测格 <b>{stats.buckets.length}</b></span>
          </div>
        </div>
      </section>
    </div>
  )
}
