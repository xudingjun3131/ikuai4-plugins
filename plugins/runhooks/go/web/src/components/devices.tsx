import { formatSize, type Stats } from "@/lib/stats"

type Props = {
  readonly stats: Stats
  readonly onFocus: (short: string) => void
}

function Topology({ devices }: { readonly devices: Stats["devices"] }) {
  const top = devices.slice(0, 6)
  const max = Math.max(1, ...top.map((d) => d.bytes))
  const cx = 320
  const cy = 140
  const rx = 236
  const ry = 96

  return (
    <svg className="nl-topo" viewBox="0 0 640 280" role="img" aria-label="局域网设备到网关的流量拓扑">
      {top.map((d, i) => {
        const angle = -Math.PI / 2 + (i / top.length) * Math.PI * 2
        const x = cx + rx * Math.cos(angle)
        const y = cy + ry * Math.sin(angle)
        const w = 1.2 + (d.bytes / max) * 6
        const anchor = x < cx - 12 ? "end" : x > cx + 12 ? "start" : "middle"
        const lx = x < cx - 12 ? x - 13 : x > cx + 12 ? x + 13 : x
        return (
          <g key={d.short}>
            <line
              x1={cx}
              y1={cy}
              x2={x}
              y2={y}
              stroke="#0f766e"
              strokeOpacity={0.2 + (d.bytes / max) * 0.5}
              strokeWidth={w}
              strokeLinecap="round"
            />
            <circle cx={x} cy={y} r={7} fill="#ffffff" stroke="#0f766e" strokeWidth={2.5} />
            <text x={lx} y={y - 11} textAnchor={anchor} fontSize="11" fontFamily="monospace" fill="#34473c">
              {d.short}
            </text>
            <text x={lx} y={y + 20} textAnchor={anchor} fontSize="9" fill="#8a9b90">
              {formatSize(d.bytes)} · {d.count} 次
            </text>
          </g>
        )
      })}
      <circle cx={cx} cy={cy} r={36} fill="#0e211c" />
      <circle cx={cx} cy={cy} r={36} fill="none" stroke="#14b8a6" strokeWidth={2} />
      <text x={cx} y={cy - 1} textAnchor="middle" fontSize="13" fontWeight="700" fill="#eaf3ee">
        网关
      </text>
      <text x={cx} y={cy + 15} textAnchor="middle" fontSize="9" fill="#7fd8c7">
        NetLoom
      </text>
    </svg>
  )
}

function avatarOf(short: string): string {
  if (short.includes(".")) {
    const tail = short.split(".").pop() ?? short
    return tail.slice(0, 3)
  }
  return short.replace(/[^0-9a-zA-Z]/g, "").slice(0, 3) || "设备"
}

export function DevicesPanel({ stats, onFocus }: Props) {
  return (
    <div className="nl-grid nl-grid--overview">
      <section className="nl-card nl-span-12">
        <div className="nl-card-head">
          <h2 className="nl-card-title">设备星图</h2>
          <span className="nl-card-sub">
            以网关为中心的流量拓扑 · 连线粗细代表累计字节 · 最多展示 6 台
          </span>
        </div>
        <div className="nl-card-body" style={{ paddingTop: 6, paddingBottom: 6 }}>
          {stats.devices.length === 0 ? (
            <p className="nl-card-sub" style={{ padding: "36px 0", textAlign: "center" }}>
              暂无在线设备流量。在插件页勾选抓包设备后，拓扑会在这里自动生长。
            </p>
          ) : (
            <Topology devices={stats.devices} />
          )}
        </div>
      </section>

      <section className="nl-card nl-span-12">
        <div className="nl-card-head">
          <h2 className="nl-card-title">终端画像</h2>
          <span className="nl-card-sub">共 {stats.devices.length} 台 · 点卡片进入该设备的会话瀑布</span>
        </div>
        <div className="nl-card-body">
          {stats.devices.length === 0 ? (
            <div className="nl-empty">
              <h3>还没有观测到设备</h3>
              <p>
                巡流按 client 地址把会话聚合成「设备」。请先在插件管理页启动引擎、勾选要观测的局域网终端，
                并在客户端安装 CA 证书，流量就会出现在这里。
              </p>
            </div>
          ) : (
            <div className="nl-devices">
              {stats.devices.map((d) => {
                const maxSpark = Math.max(1, ...d.spark)
                return (
                  <button
                    type="button"
                    className="nl-device"
                    key={d.short}
                    onClick={() => onFocus(d.short)}
                    data-testid={`device-${d.short}`}
                  >
                    <div className="nl-device-top">
                      <span className="nl-device-avatar">{avatarOf(d.short)}</span>
                      <span style={{ minWidth: 0 }}>
                        <span className="nl-device-name" style={{ display: "block" }}>{d.short}</span>
                        <span className="nl-device-addr">{d.addr}</span>
                      </span>
                    </div>
                    <div className="nl-device-metrics">
                      <span className="nl-device-metric">
                        <span>会话</span>
                        <b>{d.count}</b>
                      </span>
                      <span className="nl-device-metric">
                        <span>流量</span>
                        <b>{formatSize(d.bytes)}</b>
                      </span>
                      <span className="nl-device-metric">
                        <span>异常</span>
                        <b>{d.errors}</b>
                      </span>
                    </div>
                    <div className="nl-spark" title="该设备在观测窗口内的会话分布">
                      {d.spark.map((n, i) => (
                        <i key={i} style={{ height: `${Math.max(8, (n / maxSpark) * 100)}%` }} />
                      ))}
                    </div>
                    <div className="nl-device-top-host">
                      最常访问 <b>{d.topHost}</b>
                    </div>
                  </button>
                )
              })}
            </div>
          )}
        </div>
      </section>
    </div>
  )
}
