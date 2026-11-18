import { useCallback, useEffect, useRef, useState } from "react"

import {
  clearFlows,
  eventsURL,
  fetchCapturePaused,
  fetchFlows,
  setCapturePaused as putCapturePaused,
} from "@/lib/api"
import {
  emptyFlowFilter,
  type FlowDTO,
  type FlowFilter,
  type SSEEventType,
} from "@/lib/types"

function upsertFlow(list: readonly FlowDTO[], next: FlowDTO): FlowDTO[] {
  const idx = list.findIndex((f) => f.id === next.id)
  if (idx === -1) {
    return [next, ...list]
  }
  const copy = list.slice()
  copy[idx] = next
  return copy
}

function flowMatchesFilter(f: FlowDTO, filter: FlowFilter): boolean {
  const method = filter.method.trim()
  if (method && (f.request.method ?? "").toUpperCase() !== method.toUpperCase()) {
    return false
  }
  const statusRaw = filter.status.trim()
  if (statusRaw) {
    const n = Number(statusRaw)
    if (!Number.isNaN(n) && (f.response.status ?? 0) !== n) {
      return false
    }
  }
  const host = filter.host.trim().toLowerCase()
  if (host) {
    const url = (f.request.url ?? "").toLowerCase()
    if (!url.includes(host)) {
      return false
    }
  }
  const q = filter.q.trim().toLowerCase()
  if (q) {
    const hay = `${f.request.method ?? ""} ${f.request.url ?? ""} ${f.error ?? ""}`.toLowerCase()
    if (!hay.includes(q)) {
      return false
    }
  }
  const rawConds = filter.conds
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean)
  if (rawConds.length === 0) return true
  const check = (raw: string): boolean => {
    const parts = raw.split(":")
    const field = (parts[0] ?? "q").toLowerCase()
    let op = "contains"
    let value = parts.slice(1).join(":")
    if (parts.length >= 3) {
      op = parts[1]!.toLowerCase()
      value = parts.slice(2).join(":")
    }
    let actual = ""
    switch (field) {
      case "method":
        actual = f.request.method ?? ""
        break
      case "status":
        actual = String(f.response.status ?? "")
        break
      case "host":
      case "url":
      case "q":
        actual = f.request.url ?? ""
        break
      case "highlight":
        actual = `${f.highlightColor ?? ""} ${f.highlightLabel ?? ""}`
        break
      default:
        actual = `${f.request.method ?? ""} ${f.request.url ?? ""}`
    }
    if (op === "eq" || op === "=") {
      return actual.toLowerCase() === value.toLowerCase()
    }
    if (op === "neq" || op === "!=") {
      return actual.toLowerCase() !== value.toLowerCase()
    }
    return actual.toLowerCase().includes(value.toLowerCase())
  }
  if (filter.op === "or") return rawConds.some(check)
  return rawConds.every(check)
}

export function useFlows(): {
  flows: readonly FlowDTO[]
  selectedId: string | null
  select: (id: string | null) => void
  selected: FlowDTO | null
  loading: boolean
  error: string | null
  capturePaused: boolean
  filter: FlowFilter
  setFilter: (next: FlowFilter) => void
  togglePause: () => Promise<void>
  clear: () => Promise<void>
  actionBusy: boolean
} {
  const [flows, setFlows] = useState<readonly FlowDTO[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [capturePaused, setCapturePaused] = useState(false)
  const [filter, setFilterState] = useState<FlowFilter>(emptyFlowFilter)
  const [actionBusy, setActionBusy] = useState(false)
  const filterRef = useRef(filter)
  filterRef.current = filter
  const esRef = useRef<EventSource | null>(null)

  const select = useCallback((id: string | null) => {
    setSelectedId(id)
  }, [])

  const setFilter = useCallback((next: FlowFilter) => {
    setFilterState(next)
  }, [])

  useEffect(() => {
    const ac = new AbortController()
    setLoading(true)
    void (async () => {
      try {
        const [list, paused] = await Promise.all([
          fetchFlows(ac.signal, filter),
          fetchCapturePaused(ac.signal),
        ])
        if (ac.signal.aborted) return
        setFlows(list)
        setCapturePaused(paused)
        setError(null)
      } catch (e) {
        if (ac.signal.aborted) return
        setError(e instanceof Error ? e.message : "流量加载失败")
      } finally {
        if (!ac.signal.aborted) setLoading(false)
      }
    })()
    return () => ac.abort()
  }, [filter])

  useEffect(() => {
    const es = new EventSource(eventsURL())
    esRef.current = es

    const onFlow = (type: SSEEventType) => (ev: MessageEvent<string>) => {
      try {
        const flow = JSON.parse(ev.data) as FlowDTO
        if (!flow?.id) return
        if (!flowMatchesFilter(flow, filterRef.current)) {
          setFlows((prev) => prev.filter((f) => f.id !== flow.id))
          return
        }
        setFlows((prev) =>
          type === "flow.added" || type === "flow.updated"
            ? upsertFlow(prev, flow)
            : prev,
        )
      } catch {
        // ignore malformed payloads
      }
    }

    es.addEventListener("flow.added", onFlow("flow.added"))
    es.addEventListener("flow.updated", onFlow("flow.updated"))
    es.addEventListener("cleared", () => {
      setFlows([])
      setSelectedId(null)
    })
    es.addEventListener("capture_paused", (ev: MessageEvent<string>) => {
      try {
        const data = JSON.parse(ev.data) as { paused?: boolean }
        setCapturePaused(Boolean(data.paused))
      } catch {
        // ignore
      }
    })
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        setError("连接中断")
      }
    }

    return () => {
      es.close()
      esRef.current = null
    }
  }, [])

  const togglePause = useCallback(async () => {
    setActionBusy(true)
    try {
      const next = await putCapturePaused(!capturePaused)
      setCapturePaused(next)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : "暂停失败")
    } finally {
      setActionBusy(false)
    }
  }, [capturePaused])

  const clear = useCallback(async () => {
    setActionBusy(true)
    try {
      await clearFlows()
      setFlows([])
      setSelectedId(null)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : "清空失败")
    } finally {
      setActionBusy(false)
    }
  }, [])

  const selected =
    selectedId === null
      ? null
      : (flows.find((f) => f.id === selectedId) ?? null)

  return {
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
  }
}
