import { formatMs, formatSize, type Stats } from "@/lib/stats"

type Props = { readonly stats: Stats }

const SEGMENTS: { key: keyof Stats["status"]; label: string; color: string }[] = [
  { key: "ok", label: "2xx 正常", color: "#0f766e" },
  { key: "redirect", label: "3xx 重定向", color: "#3b6ea8" },
  { key: "clientErr", label: "4xx 客户端", color: "#c2740a" },
  { key: "serverErr", label: "5xx 服务端", color: "#b3453b" },
  { key: "pending", label: "进行中/未知", color: "#a8b9ab" },
]

export function InsightsPanel({ stats }: Props) {
  const top = stats.hosts.slice(0, 12)
  const maxCount = Math.max(1, ...top.map((h) => h.count))
  const slow = [...stats.hosts]
    .filter((h) => h.avgMs > 0)
    .sort((a, b) => b.avgMs - a.avgMs)
    .slice(0, 6)
  const errHosts = [...stats.hosts]
    .filter((h) => h.errors > 0)
    .sort((a, b) => b.errors - a.errors)
    .slice(0, 8)
  const totalStatus = Math.max(
    1,
    stats.status.ok + stats.status.redirect + stats.status.clientErr + stats.status.serverErr + stats.status.pending,
  )

  return (
    <div className="nl-grid nl-grid--overview">
      <section className="nl-card nl-span-12">
        <div className="nl-card-head">
          <h2 className="nl-card-title">域名画像</h2>
          <span className="nl-card-sub">
            全网访问热度 · 共 {stats.hosts.length} 个域名 · 按会话数排序
          </span>
        </div>
        <div className="nl-card-body nl-card-body--flush">
          {top.length === 0 ? (
            <p className="nl-card-sub" style={{ padding: "32px 18px", textAlign: "center" }}>
              暂无域名数据。
            </p>
          ) : (
            <table className="nl-table">
              <thead>
                <tr>
                  <th>域名</th>
                  <th>会话数</th>
                  <th>热度</th>
                  <th>累计流量</th>
                  <th>异常</th>
                  <th>平均耗时</th>
                </tr>
              </thead>
              <tbody>
                {top.map((h) => (
                  <tr key={h.host}>
                    <td className="nl-host" title={h.host}>{h.host}</td>
                    <td className="nl-mono">{h.count}</td>
                    <td>
                      <span className="nl-mini-track">
                        <i style={{ width: `${(h.count / maxCount) * 100}%` }} />
                      </span>
                    </td>
                    <td className="nl-mono">{formatSize(h.bytes)}</td>
                    <td>
                      <span className={`nl-pill${h.errors > 0 ? " nl-pill--warn" : ""}`}>
                        {h.errors}
                      </span>
                    </td>
                    <td className="nl-mono">{formatMs(h.avgMs)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>

      <section className="nl-card nl-span-6">
        <div className="nl-card-head">
          <h2 className="nl-card-title">响应码构成</h2>
          <span className="nl-card-sub">全网占比</span>
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
              />
            ))}
          </div>
          <div className="nl-legend">
            {SEGMENTS.map((s) => (
              <span className="nl-legend-item" key={s.key}>
                <span className="nl-dot" style={{ background: s.color }} />
                {s.label} <b>{((stats.status[s.key] / totalStatus) * 100).toFixed(1)}%</b>
              </span>
            ))}
          </div>
        </div>
      </section>

      <section className="nl-card nl-span-6">
        <div className="nl-card-head">
          <h2 className="nl-card-title">最慢端点</h2>
          <span className="nl-card-sub">按平均响应耗时</span>
        </div>
        <div className="nl-card-body">
          {slow.length === 0 ? (
            <p className="nl-card-sub">暂无耗时数据（会话尚未完成或缺少完成时间）。</p>
          ) : (
            <div className="nl-rank">
              {slow.map((h) => (
                <div className="nl-rank-row" key={h.host}>
                  <span className="nl-rank-name" title={h.host}>{h.host}</span>
                  <span className="nl-rank-track">
                    <span
                      className="nl-rank-fill"
                      style={{
                        width: `${(h.avgMs / Math.max(1, slow[0]!.avgMs)) * 100}%`,
                        display: "block",
                        background: "linear-gradient(90deg,#c2740a,#e0a23c)",
                      }}
                    />
                  </span>
                  <span className="nl-rank-val">{formatMs(h.avgMs)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </section>

      <section className="nl-card nl-span-12">
        <div className="nl-card-head">
          <h2 className="nl-card-title">异常域名与方法分布</h2>
          <span className="nl-card-sub">异常会话占比 {stats.errorRate.toFixed(1)}%</span>
        </div>
        <div className="nl-card-body">
          {errHosts.length === 0 ? (
            <p className="nl-card-sub">当前观测窗口内没有异常会话。</p>
          ) : (
            <div className="nl-chips" style={{ marginTop: 0 }}>
              {errHosts.map((h) => (
                <span className="nl-chip" key={h.host} style={{ borderColor: "#e8c9a0" }}>
                  {h.host} <b style={{ color: "#b3453b" }}>{h.errors} 次异常</b>
                </span>
              ))}
            </div>
          )}
          <div className="nl-chips">
            {stats.methods.map((m) => (
              <span className="nl-chip" key={m.name}>
                {m.name} <b>{m.count}</b>
              </span>
            ))}
          </div>
        </div>
      </section>
    </div>
  )
}
