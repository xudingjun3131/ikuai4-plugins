import {
  Download,
  FolderTree,
  ListTree,
  ScrollText,
  TerminalSquare,
  Waypoints,
} from "lucide-react"

import { cn } from "@/lib/utils"

export type AppView = "capture" | "rules" | "rest" | "explorer" | "scripts" | "subs"

const items: { id: AppView; label: string; icon: typeof ListTree }[] = [
  { id: "capture", label: "抓包", icon: ListTree },
  { id: "rules", label: "规则", icon: Waypoints },
  { id: "subs", label: "订阅", icon: Download },
  { id: "rest", label: "编写", icon: TerminalSquare },
  { id: "explorer", label: "浏览  ", icon: FolderTree },
  { id: "scripts", label: "脚本", icon: ScrollText },
]

type Props = {
  readonly view: AppView
  readonly onChange: (v: AppView) => void
}

export function NavRail({ view, onChange }: Props) {
  return (
    <nav
      className="flex w-14 shrink-0 flex-col items-center gap-1 border-r border-border/80 bg-sidebar py-2"
      aria-label="Main"
    >
      {items.map(({ id, label, icon: Icon }) => {
        const active = view === id
        return (
          <button
            key={id}
            type="button"
            title={label}
            data-testid={`nav-${id}`}
            onClick={() => onChange(id)}
            className={cn(
              "group flex w-12 flex-col items-center gap-0.5 rounded-lg px-1 py-2 transition-colors",
              active
                ? "bg-primary/15 text-primary ring-1 ring-primary/35"
                : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
            )}
          >
            <Icon className={cn("h-4.5 w-4.5 h-4 w-4", active && "text-primary")} />
            <span className="text-[9px] font-medium tracking-wide">{label}</span>
          </button>
        )
      })}
    </nav>
  )
}
