import { useState } from "react"

import { AppHeader } from "@/components/app-header"
import { ExplorerPanel } from "@/components/explorer-panel"
import { FilterBar } from "@/components/filter-bar"
import { NavRail, type AppView } from "@/components/nav-rail"
import { RestPanel } from "@/components/rest-panel"
import { RulesPanel } from "@/components/rules-panel"
import { ScriptsPanel } from "@/components/scripts-panel"
import { SubsPanel } from "@/components/subs-panel"
import { TrafficDetail } from "@/components/traffic-detail"
import { TrafficList } from "@/components/traffic-list"
import { useFlows } from "@/hooks/use-flows"

function App() {
  const [view, setView] = useState<AppView>("capture")
  const {
    flows,
    selectedId,
    select,
    selected,
    loading,
    error,
    capturePaused,
    filter,
    setFilter,
    togglePause,
    clear,
    actionBusy,
  } = useFlows()

  return (
    <div className="flex h-svh flex-col overflow-hidden bg-background text-foreground">
      <AppHeader
        flowCount={flows.length}
        capturePaused={capturePaused}
        error={error}
        onTogglePause={() => {
          void togglePause()
        }}
        onClear={() => {
          void clear()
        }}
        busy={actionBusy}
      />

      <div className="flex min-h-0 flex-1">
        <NavRail view={view} onChange={setView} />

        <div className="flex min-w-0 flex-1 flex-col">
          {view === "capture" ? (
            <>
              <div className="flex shrink-0 items-center gap-2 border-b border-border/70 bg-card/20 px-3 py-1.5">
                <FilterBar filter={filter} onChange={setFilter} />
              </div>
              <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(22rem,34%)_1fr]">
                <TrafficList
                  flows={flows}
                  selectedId={selectedId}
                  onSelect={select}
                  loading={loading}
                />
                <TrafficDetail flow={selected} />
              </div>
            </>
          ) : null}

          {view === "rules" ? (
            <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[1fr_minmax(20rem,28rem)]">
              <div className="grid min-h-0 grid-cols-1 border-r border-border/70 lg:grid-cols-[minmax(18rem,40%)_1fr]">
                <TrafficList
                  flows={flows}
                  selectedId={selectedId}
                  onSelect={select}
                  loading={loading}
                />
                <TrafficDetail flow={selected} />
              </div>
              <RulesPanel />
            </div>
          ) : null}

          {view === "rest" ? <RestPanel flowId={selectedId} /> : null}

          {view === "explorer" ? (
            <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(16rem,22rem)_1fr]">
              <ExplorerPanel
                flows={flows}
                onHostFilter={(host) => setFilter({ ...filter, host })}
              />
              <div className="grid min-h-0 grid-cols-1 lg:grid-cols-[minmax(18rem,40%)_1fr]">
                <TrafficList
                  flows={flows}
                  selectedId={selectedId}
                  onSelect={select}
                  loading={loading}
                />
                <TrafficDetail flow={selected} />
              </div>
            </div>
          ) : null}

          {view === "scripts" ? <ScriptsPanel /> : null}

          {view === "subs" ? <SubsPanel /> : null}
        </div>
      </div>
    </div>
  )
}

export default App
