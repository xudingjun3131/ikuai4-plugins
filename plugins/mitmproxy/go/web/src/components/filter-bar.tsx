import type { FlowFilter } from "@/lib/types"

type Props = {
  readonly filter: FlowFilter
  readonly onChange: (next: FlowFilter) => void
}

const METHODS = ["", "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"] as const

const inputClass =
  "h-7 rounded-md border border-border bg-background px-2 text-xs text-foreground outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40"

export function FilterBar({ filter, onChange }: Props) {
  return (
    <div
      className="flex flex-wrap items-center gap-2 border-b border-border bg-card px-3 py-1.5"
      data-testid="filter-bar"
      role="search"
      aria-label="Filter flows"
    >
      <label className="flex items-center gap-1.5">
        <span className="sr-only">主机</span>
        <input
          type="text"
          className={`${inputClass} w-36`}
          placeholder="主机"
          value={filter.host}
          data-testid="filter-host"
          onChange={(e) => onChange({ ...filter, host: e.target.value })}
        />
      </label>
      <label className="flex items-center gap-1.5">
        <span className="sr-only">方法</span>
        <select
          className={`${inputClass} w-24`}
          value={filter.method}
          data-testid="filter-method"
          onChange={(e) => onChange({ ...filter, method: e.target.value })}
        >
          {METHODS.map((m) => (
            <option key={m || "any"} value={m}>
              {m || "方法"}
            </option>
          ))}
        </select>
      </label>
      <label className="flex items-center gap-1.5">
        <span className="sr-only">状态</span>
        <input
          type="text"
          inputMode="numeric"
          className={`${inputClass} w-20`}
          placeholder="状态"
          value={filter.status}
          data-testid="filter-status"
          onChange={(e) => onChange({ ...filter, status: e.target.value })}
        />
      </label>
      <label className="flex min-w-[8rem] flex-1 items-center gap-1.5">
        <span className="sr-only">搜索</span>
        <input
          type="search"
          className={`${inputClass} w-full min-w-[8rem]`}
          placeholder="搜索…"
          value={filter.q}
          data-testid="filter-q"
          onChange={(e) => onChange({ ...filter, q: e.target.value })}
        />
      </label>
      <label className="flex items-center gap-1.5">
        <span className="sr-only">合并</span>
        <select
          className={`${inputClass} w-16`}
          value={filter.op}
          data-testid="filter-op"
          onChange={(e) =>
            onChange({
              ...filter,
              op: e.target.value === "or" ? "or" : "and",
            })
          }
        >
          <option value="and">且</option>
          <option value="or">或</option>
        </select>
      </label>
      <label className="flex min-w-[10rem] flex-1 items-center gap-1.5">
        <span className="sr-only">条件</span>
        <input
          type="text"
          className={`${inputClass} w-full min-w-[10rem]`}
          placeholder="字段:op:值; …"
          value={filter.conds}
          data-testid="filter-conds"
          onChange={(e) =>
            onChange({
              ...filter,
              conds: e.target.value.replace(/;/g, "\n"),
            })
          }
        />
      </label>
    </div>
  )
}
