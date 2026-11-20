import { useEffect, useMemo, useState } from "react"

import {
  ConsoleFooter,
  ConsoleHeader,
  ViewDeck,
  type ToolId,
  type ViewId,
} from "@/components/console-shell"
import { DevicesPanel } from "@/components/devices"
import { ExplorerPanel } from "@/components/explorer-panel"
import { InsightsPanel } from "@/components/insights"
import { OverviewPanel } from "@/components/overview"
import { RestPanel } from "@/components/rest-panel"
import { RewritePanel } from "@/components/rewrite-panel"
import { RulesPanel } from "@/components/rules-panel"
import { ScriptsPanel } from "@/components/scripts-panel"
import { SessionsPanel } from "@/components/sessions"
import { SubsPanel } from "@/components/subs-panel"
import { TrafficDetail } from "@/components/traffic-detail"
import { TrafficList } from "@/components/traffic-list"
import { useFlows } from "@/hooks/use-flows"
import { buildStats, shortAddr } from "@/lib/stats"

// [v1.1.8] 插件外壳页「断点改包」按钮通过 #tool=rewrite 深链进本页;
// hashchange 监听让已打开的观测台无需重载即可切到该工具。
const TOOL_HASH: Record<string, ToolId> = {
  rules: "rules",
  rewrite: "rewrite",
  subs: "subs",
  rest: "rest",
  explorer: "explorer",
  scripts: "scripts",
}

function toolFromHash(): { tool: ToolId; view: ViewId } | null {
  if (typeof window === "undefined") return null
  const m = /tool=([a-z]+)/.exec(window.location.hash || "")
  if (!m) return null
  const tool = TOOL_HASH[m[1]]
  return tool ? { tool, view: "toolbox" } : null
}

function App() {
  const initial = toolFromHash()
  const [view, setView] = useState<ViewId>(initial?.view ?? "overview")
  const [tool, setTool] = useState<ToolId>(initial?.tool ?? "rules")
  const [focusDevice, setFocusDevice] = useState<string | null>(null)
  const ctx = useFlows()

  useEffect(() => {
    const onHash = () => {
      const next = toolFromHash()
      if (next) {
        setTool(next.tool)
        setView(next.view)
      }
    }
    window.addEventListener("hashchange", onHash)
    return () => window.removeEventListener("hashchange", onHash)
  }, [])

  // 全部统计在浏览器侧从 /api/flows 聚合 —— 这是网关观测台独有的视角
  const stats = useMemo(() => buildStats(ctx.flows), [ctx.flows])
  const focusFlows = useMemo(
    () =>
      focusDevice === null
        ? ctx.flows
        : ctx.flows.filter((f) => shortAddr(f.clientAddr ?? "") === focusDevice),
    [ctx.flows, focusDevice],
  )

  const openDevice = (short: string): void => {
    setFocusDevice(short)
    setView("sessions")
  }

  return (
    <div className="nl-app">
      <ConsoleHeader
        flowCount={ctx.flows.length}
        deviceCount={stats.devices.length}
        capturePaused={ctx.capturePaused}
        error={ctx.error}
        busy={ctx.actionBusy}
        onTogglePause={() => void ctx.togglePause()}
        onClear={() => void ctx.clear()}
      />
      <ViewDeck
        view={view}
        onChange={setView}
        flowCount={ctx.flows.length}
        tool={tool}
        onToolChange={setTool}
      />

      <main className="nl-main">
        {view === "overview" ? (
          <OverviewPanel stats={stats} onOpenDevices={() => setView("devices")} />
        ) : null}

        {view === "devices" ? (
          <DevicesPanel stats={stats} onFocus={openDevice} />
        ) : null}

        {view === "sessions" ? (
          <SessionsPanel
            flows={focusFlows}
            selectedId={ctx.selectedId}
            onSelect={ctx.select}
            selected={ctx.selected}
            loading={ctx.loading}
            filter={ctx.filter}
            onFilter={ctx.setFilter}
            focusDevice={focusDevice}
            onClearFocus={() => setFocusDevice(null)}
          />
        ) : null}

        {view === "insights" ? <InsightsPanel stats={stats} /> : null}

        {view === "toolbox" ? (
          <>
            <div className="nl-tool-note">
              运维工具箱为次级功能：规则、断点改包、订阅、单请求改写、脚本调试与会话 diff。
              观测台的主视角是「全网设备 · 时间轴 · 域名画像」——数据来自整台网关，而不是某一台设备上的某个 App。
            </div>
            <div className="nl-tool-frame">
              {tool === "rules" ? (
                <div className="nl-tool-grid nl-tool-grid--rules">
                  <div className="nl-tool-split">
                    <TrafficList
                      flows={ctx.flows}
                      selectedId={ctx.selectedId}
                      onSelect={ctx.select}
                      loading={ctx.loading}
                    />
                    <TrafficDetail flow={ctx.selected} />
                  </div>
                  <RulesPanel />
                </div>
              ) : null}
              {tool === "subs" ? (
                <div className="nl-tool-grid nl-tool-grid--full">
                  <SubsPanel />
                </div>
              ) : null}
              {tool === "rewrite" ? (
                <div className="nl-tool-grid nl-tool-grid--full">
                  <RewritePanel flows={ctx.flows} />
                </div>
              ) : null}
              {tool === "rest" ? (
                <div className="nl-tool-grid nl-tool-grid--full">
                  <RestPanel flowId={ctx.selectedId} />
                </div>
              ) : null}
              {tool === "scripts" ? (
                <div className="nl-tool-grid nl-tool-grid--full">
                  <ScriptsPanel />
                </div>
              ) : null}
              {tool === "explorer" ? (
                <div className="nl-tool-grid nl-tool-grid--explorer">
                  <ExplorerPanel
                    flows={ctx.flows}
                    onHostFilter={(host: string) =>
                      ctx.setFilter({ ...ctx.filter, host })
                    }
                  />
                  <div className="nl-tool-split">
                    <TrafficList
                      flows={ctx.flows}
                      selectedId={ctx.selectedId}
                      onSelect={ctx.select}
                      loading={ctx.loading}
                    />
                    <TrafficDetail flow={ctx.selected} />
                  </div>
                </div>
              ) : null}
            </div>
          </>
        ) : null}
      </main>

      <ConsoleFooter
        flowCount={ctx.flows.length}
        deviceCount={stats.devices.length}
        capturePaused={ctx.capturePaused}
      />
    </div>
  )
}

export default App
