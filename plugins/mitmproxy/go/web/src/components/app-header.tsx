import {
  Download,
  Eraser,
  Pause,
  Play,
  Radio,
  Shield,
  Activity,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { caPemURL, exportHarURL } from "@/lib/api"

type Props = {
  readonly flowCount: number
  readonly capturePaused: boolean
  readonly error: string | null
  readonly onTogglePause: () => void
  readonly onClear: () => void
  readonly busy?: boolean
  readonly proxyHint?: string
}

export function AppHeader({
  flowCount,
  capturePaused,
  error,
  onTogglePause,
  onClear,
  busy = false,
  proxyHint = "RouterIP:18070",
}: Props) {
  return (
    <header className="flex h-11 shrink-0 items-center gap-2 border-b border-border/80 bg-gradient-to-r from-card via-card to-primary/5 px-3">
      <div className="flex items-center gap-2.5 pr-2">
        <div className="flex h-7 w-7 items-center justify-center rounded-md bg-primary/20 ring-1 ring-primary/40">
          <Activity className="h-4 w-4 text-primary" />
        </div>
        <div className="leading-tight">
          <h1 className="text-sm font-semibold tracking-tight">Mitmproxy 抓包</h1>
          <p className="text-[10px] text-muted-foreground">流量捕获 · HTTPS 解密</p>
        </div>
      </div>

      <div className="mx-1 hidden h-6 w-px bg-border sm:block" />

      <div className="hidden items-center gap-1.5 rounded-md bg-muted/50 px-2 py-1 font-mono text-[10px] text-muted-foreground ring-1 ring-border/60 md:flex">
        <Radio className="h-3 w-3 text-primary" />
        代理 {proxyHint}
      </div>

      <div className="flex-1" />

      {error ? (
        <span className="max-w-[14rem] truncate text-[11px] text-rose-300" title={error}>
          {error}
        </span>
      ) : null}

      {capturePaused ? (
        <Badge
          variant="outline"
          className="h-6 gap-1.5 border-amber-500/40 bg-amber-500/10 text-[10px] text-amber-200"
          data-testid="capture-badge"
        >
          <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
          已暂停
        </Badge>
      ) : (
        <Badge
          variant="outline"
          className="h-6 gap-1.5 border-emerald-500/40 bg-emerald-500/10 text-[10px] text-emerald-200"
          data-testid="capture-badge"
        >
          <span className="live-dot" />
          实时
        </Badge>
      )}

      <span className="hidden font-mono text-[11px] text-muted-foreground tabular-nums sm:inline">
        {flowCount.toLocaleString()} 条
      </span>

      <div className="flex items-center gap-1">
        <Button
          variant={capturePaused ? "default" : "secondary"}
          size="sm"
          className="h-8 gap-1.5"
          data-testid="toggle-pause"
          disabled={busy}
          onClick={onTogglePause}
        >
          {capturePaused ? (
            <>
              <Play className="h-3.5 w-3.5" />
              继续
            </>
          ) : (
            <>
              <Pause className="h-3.5 w-3.5" />
              暂停
            </>
          )}
        </Button>
        <Button
          variant="secondary"
          size="sm"
          className="h-8 gap-1.5"
          data-testid="clear-flows"
          disabled={busy || flowCount === 0}
          onClick={onClear}
        >
          <Eraser className="h-3.5 w-3.5" />
          清空
        </Button>
        <Button
          variant="secondary"
          size="sm"
          className="h-8 gap-1.5"
          render={<a href={exportHarURL()} download="mitmproxy.har" data-testid="export-har" />}
        >
          <Download className="h-3.5 w-3.5" />
          HAR导出
        </Button>
        <Button
          variant="secondary"
          size="sm"
          className="h-8 gap-1.5"
          render={<a href={caPemURL()} download="ca.pem" data-testid="download-ca" />}
        >
          <Shield className="h-3.5 w-3.5" />
          CA证书
        </Button>
      </div>
    </header>
  )
}
