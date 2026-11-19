import { apiURL } from "@/lib/api"
import { useCallback, useEffect, useState } from "react"
import { FileCode2, Sparkles } from "lucide-react"

import { Button } from "@/components/ui/button"

type Template = {
  id: string
  name: string
  description?: string
  source?: string
}

export function ScriptsPanel() {
  const [templates, setTemplates] = useState<Template[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const res = await fetch(apiURL("/api/scripts/templates"))
      if (!res.ok) {
        setError(`HTTP ${res.status}`)
        return
      }
      const data = (await res.json()) as { templates: Template[] }
      setTemplates(data.templates ?? [])
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : "加载失败")
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function apply(id: string) {
    setBusy(true)
    setMsg(null)
    try {
      const res = await fetch(apiURL("/api/scripts/templates"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id }),
      })
      if (!res.ok) {
        setError(await res.text())
        return
      }
      const data = (await res.json()) as { file?: string }
      setMsg(`Applied → ${data.file ?? id}`)
      setError(null)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex h-full flex-col bg-card/30" data-testid="scripts-panel">
      <header className="flex h-9 items-center gap-2 border-b border-border/80 px-3">
        <Sparkles className="h-3.5 w-3.5 text-violet-300" />
        <span className="panel-title">Script templates</span>
      </header>
      {error ? <p className="px-3 py-2 text-xs text-rose-300">{error}</p> : null}
      {msg ? <p className="px-3 py-2 text-xs text-emerald-300">{msg}</p> : null}
      <ul className="min-h-0 flex-1 space-y-2 overflow-auto p-3">
        {templates.map((t) => (
          <li
            key={t.id}
            className="rounded-lg border border-border/70 bg-card/60 p-3 shadow-sm ring-1 ring-black/20"
          >
            <div className="flex items-start gap-2">
              <FileCode2 className="mt-0.5 h-4 w-4 shrink-0 text-sky-300" />
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium text-foreground">{t.name}</p>
                <p className="mt-0.5 text-[11px] text-muted-foreground">{t.description}</p>
                {t.source ? (
                  <pre className="mt-2 max-h-24 overflow-auto rounded bg-[#0d1118] p-2 font-mono text-[10px] text-emerald-100/80">
                    {t.source.trim()}
                  </pre>
                ) : null}
              </div>
              <Button size="sm" className="h-7 shrink-0" disabled={busy} onClick={() => void apply(t.id)}>
                应用
              </Button>
            </div>
          </li>
        ))}
        {templates.length === 0 ? (
          <li className="py-8 text-center text-xs text-muted-foreground">No templates</li>
        ) : null}
      </ul>
    </div>
  )
}
