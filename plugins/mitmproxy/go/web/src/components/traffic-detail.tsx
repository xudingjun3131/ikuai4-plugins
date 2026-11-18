import { useMemo, useState } from "react"
import { Copy, Check } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { flowIsTruncated } from "@/lib/api"
import type { FlowDTO, MessageDTO } from "@/lib/types"
import { cn } from "@/lib/utils"
import { formatBytes, formatDuration, methodClass, statusClass } from "@/lib/tones"

type Props = {
  readonly flow: FlowDTO | null
}

type Tab = "overview" | "headers" | "request" | "response"

function HeaderTable({ headers }: { readonly headers: MessageDTO["headers"] }) {
  if (!headers || Object.keys(headers).length === 0) {
    return <p className="px-3 py-3 text-[11px] text-muted-foreground">No headers</p>
  }
  return (
    <dl className="divide-y divide-border/50 font-mono text-[11px]">
      {Object.entries(headers).map(([k, vals]) => (
        <div key={k} className="grid grid-cols-[minmax(7rem,11rem)_1fr] gap-2 px-3 py-1.5 hover:bg-muted/30">
          <dt className="truncate font-semibold text-sky-300/90">{k}</dt>
          <dd className="min-w-0 break-all text-foreground/90">{vals.join(", ")}</dd>
        </div>
      ))}
    </dl>
  )
}

function prettyBody(msg: MessageDTO): string {
  const raw = msg.bodyDisplay ?? ""
  if (!raw) return msg.body ? "(binary / base64 — use raw capture size)" : "—"
  try {
    const parsed = JSON.parse(raw) as unknown
    return JSON.stringify(parsed, null, 2)
  } catch {
    return raw
  }
}

function BodyBlock({ msg }: { readonly msg: MessageDTO }) {
  const text = useMemo(() => prettyBody(msg), [msg])
  const [copied, setCopied] = useState(false)
  return (
    <div className="relative">
      <div className="flex items-center gap-2 border-b border-border/60 px-3 py-1.5">
        {msg.truncated ? (
          <Badge variant="outline" className="h-5 border-amber-500/40 text-[10px] text-amber-200">
            truncated
          </Badge>
        ) : null}
        <span className="font-mono text-[10px] text-muted-foreground tabular-nums">
          {formatBytes(msg.size)}
        </span>
        <Button
          variant="ghost"
          size="sm"
          className="ml-auto h-7 gap-1 text-[10px]"
          onClick={() => {
            void navigator.clipboard.writeText(text)
            setCopied(true)
            setTimeout(() => setCopied(false), 1200)
          }}
        >
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          {copied ? "已复制" : "复制"}
        </Button>
      </div>
      <pre className="max-h-[min(28rem,50vh)] overflow-auto bg-[#0d1118] px-3 py-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-all text-emerald-100/90">
        {text}
      </pre>
    </div>
  )
}

const tabs: { id: Tab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "headers", label: "Headers" },
  { id: "request", label: "Request" },
  { id: "response", label: "Response" },
]

export function TrafficDetail({ flow }: Props) {
  const [tab, setTab] = useState<Tab>("overview")

  if (!flow) {
    return (
      <section
        className="flex h-full min-h-0 flex-col bg-background/50"
        data-testid="traffic-detail"
        aria-label="Flow detail"
      >
        <div className="flex flex-1 flex-col items-center justify-center gap-2 px-8 text-center">
          <p className="text-sm font-medium text-foreground/80">详情</p>
          <p className="max-w-xs text-[11px] leading-relaxed text-muted-foreground">
            Select a row to inspect method, status, timing, headers, and bodies — mitmproxy-style detail pane.
          </p>
        </div>
      </section>
    )
  }

  const method = (flow.request.method ?? "—").toUpperCase()
  const status = flow.response.status

  return (
    <section
      className="flex h-full min-h-0 flex-col bg-background/40"
      data-testid="traffic-detail"
      aria-label="Flow detail"
    >
      <header className="shrink-0 border-b border-border/80 bg-card/50 px-3 py-2.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className={cn("method-chip", methodClass(method))}>{method}</span>
          <span className={cn("status-chip", statusClass(status))}>{status && status > 0 ? status : "—"}</span>
          {flow.scheme ? (
            <Badge variant="outline" className="h-5 text-[10px] uppercase">
              {flow.scheme}
            </Badge>
          ) : null}
          {flow.protocol ? (
            <Badge variant="outline" className="h-5 font-mono text-[10px]">
              {flow.protocol}
            </Badge>
          ) : null}
          {flowIsTruncated(flow) ? (
            <Badge variant="outline" className="h-5 border-amber-500/40 text-[10px] text-amber-200">
              truncated
            </Badge>
          ) : null}
          <span className="ml-auto font-mono text-[10px] text-muted-foreground tabular-nums">
            {formatDuration(flow.startedAt, flow.doneAt)}
          </span>
        </div>
        <p
          className="mt-1.5 truncate font-mono text-[12px] text-foreground/90"
          data-testid="detail-method"
          title={flow.request.url}
        >
          <span className="sr-only">{method} </span>
          {flow.request.url ?? "—"}
        </p>
      </header>

      <div className="flex shrink-0 gap-0.5 border-b border-border/70 bg-card/30 px-2">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={cn(
              "relative px-3 py-2 text-[11px] font-medium transition-colors",
              tab === t.id
                ? "text-primary after:absolute after:right-2 after:bottom-0 after:left-2 after:h-0.5 after:rounded-full after:bg-primary"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      <ScrollArea className="min-h-0 flex-1">
        {tab === "overview" ? (
          <div className="grid gap-0 sm:grid-cols-2">
            <MetaRow label="State" value={String(flow.state)} />
            <MetaRow label="客户" value={flow.clientAddr ?? "—"} />
            <MetaRow label="进行中" value={flow.startedAt} />
            <MetaRow label="完成" value={flow.doneAt ?? "—"} />
            <MetaRow label="时长" value={formatDuration(flow.startedAt, flow.doneAt)} />
            <MetaRow label="请求大小" value={formatBytes(flow.request.size)} />
            <MetaRow label="响应大小" value={formatBytes(flow.response.size)} />
            <MetaRow label="流量ID" value={flow.id} mono />
            {flow.error ? (
              <div className="col-span-full border-t border-border/60 bg-rose-500/10 px-3 py-2 text-xs text-rose-200">
                {flow.error}
              </div>
            ) : null}
            {flow.tags && flow.tags.length > 0 ? (
              <div className="col-span-full flex flex-wrap gap-1 border-t border-border/60 px-3 py-2">
                {flow.tags.map((t) => (
                  <Badge key={t} variant="secondary" className="text-[10px]">
                    {t}
                  </Badge>
                ))}
              </div>
            ) : null}
          </div>
        ) : null}

        {tab === "headers" ? (
          <div className="grid gap-0 lg:grid-cols-2">
            <div className="border-b border-border/60 lg:border-r lg:border-b-0">
              <h3 className="panel-title bg-muted/30 px-3 py-2">Request headers</h3>
              <HeaderTable headers={flow.request.headers} />
            </div>
            <div>
              <h3 className="panel-title bg-muted/30 px-3 py-2">响应头</h3>
              <HeaderTable headers={flow.response.headers} />
            </div>
          </div>
        ) : null}

        {tab === "request" ? <BodyBlock msg={flow.request} /> : null}
        {tab === "response" ? <BodyBlock msg={flow.response} /> : null}
      </ScrollArea>
    </section>
  )
}

function MetaRow({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="flex items-baseline justify-between gap-3 border-b border-border/40 px-3 py-2">
      <span className="text-[10px] tracking-wide text-muted-foreground uppercase">{label}</span>
      <span className={cn("truncate text-right text-[11px] text-foreground/90", mono && "font-mono")}>
        {value}
      </span>
    </div>
  )
}

