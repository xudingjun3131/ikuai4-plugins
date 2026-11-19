import { Lock, Unlock } from "lucide-react"

import { ScrollArea } from "@/components/ui/scroll-area"
import { flowHost, flowIsTruncated, flowPath } from "@/lib/api"
import type { FlowDTO } from "@/lib/types"
import { cn } from "@/lib/utils"
import { formatDuration, methodClass, schemeClass, statusClass } from "@/lib/tones"

type Props = {
  readonly flows: readonly FlowDTO[]
  readonly selectedId: string | null
  readonly onSelect: (id: string) => void
  readonly loading: boolean
}

export function TrafficList({ flows, selectedId, onSelect, loading }: Props) {
  return (
    <section
      className="flex h-full min-h-0 flex-col border-r border-border/80 bg-card/40"
      data-testid="traffic-list"
      aria-label="Captured traffic"
    >
      <header className="flex h-9 shrink-0 items-center gap-2 border-b border-border/80 bg-card/60 px-3">
        <span className="panel-title">会话列表</span>
        <span className="ml-auto rounded-full bg-primary/15 px-2 py-0.5 font-mono text-[10px] text-primary tabular-nums ring-1 ring-primary/25">
          {flows.length}
        </span>
      </header>

      <div className="hidden grid-cols-[3.5rem_2.5rem_1fr_3.5rem] gap-2 border-b border-border/50 px-3 py-1 text-[9px] tracking-wider text-muted-foreground uppercase md:grid">
        <span>方法</span>
        <span>状态码</span>
        <span>主机/路径</span>
        <span className="text-right">耗时</span>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        {loading && flows.length === 0 ? (
          <p className="px-3 py-6 text-center text-xs text-muted-foreground">正在载入会话…</p>
        ) : flows.length === 0 ? (
          <div className="flex flex-col items-center gap-2 px-6 py-10 text-center" data-testid="traffic-empty">
            <p className="text-sm font-medium text-foreground/90">暂无会话</p>
            <p className="max-w-[16rem] text-[11px] leading-relaxed text-muted-foreground">
              在插件页勾选要观测的局域网设备并开启记录，会话会实时出现在这里。
            </p>
          </div>
        ) : (
          <ul className="pb-2" role="listbox">
            {flows.map((f) => {
              const method = (f.request.method ?? "—").toUpperCase()
              const status = f.response.status
              const selected = selectedId === f.id
              const host = flowHost(f)
              const path = flowPath(f)
              const https = (f.scheme ?? "").toLowerCase() === "https"
              const hl = f.highlightColor
              return (
                <li key={f.id}>
                  <button
                    type="button"
                    role="option"
                    aria-selected={selected}
                    onClick={() => onSelect(f.id)}
                    className={cn(
                      "group grid w-full grid-cols-1 gap-1 border-b border-border/40 px-3 py-2 text-left transition-colors md:grid-cols-[3.5rem_2.5rem_1fr_3.5rem] md:items-center md:gap-2",
                      selected
                        ? "bg-primary/12 shadow-[inset_3px_0_0_0] shadow-primary"
                        : "hover:bg-muted/40",
                      hl ? "border-l-2" : null,
                    )}
                    style={hl ? { borderLeftColor: hl } : undefined}
                  >
                    <span className={cn("method-chip", methodClass(method))}>{method}</span>
                    <span className={cn("status-chip", statusClass(status))}>
                      {status && status > 0 ? status : "—"}
                    </span>
                    <div className="min-w-0">
                      <div className="flex items-center gap-1.5">
                        {https ? (
                          <Lock className={cn("h-3 w-3 shrink-0", schemeClass(f.scheme))} />
                        ) : (
                          <Unlock className={cn("h-3 w-3 shrink-0", schemeClass(f.scheme))} />
                        )}
                        <span className="truncate text-[12px] font-medium text-foreground/95">
                          {host || "—"}
                        </span>
                        {flowIsTruncated(f) ? (
                          <span className="rounded bg-amber-500/15 px-1 text-[9px] text-amber-700 ring-1 ring-amber-500/30">
                            trunc
                          </span>
                        ) : null}
                        {f.highlightLabel ? (
                          <span className="rounded bg-violet-500/15 px-1 text-[9px] text-violet-700 ring-1 ring-violet-500/30">
                            {f.highlightLabel}
                          </span>
                        ) : null}
                      </div>
                      <div className="truncate font-mono text-[10px] text-muted-foreground">{path}</div>
                    </div>
                    <span className="text-right font-mono text-[10px] text-muted-foreground tabular-nums">
                      {formatDuration(f.startedAt, f.doneAt)}
                    </span>
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </ScrollArea>
    </section>
  )
}
