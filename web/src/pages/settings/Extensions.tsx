import { useEffect, useMemo, useRef, useState } from "react"
import { Check, Minus, Plus, Search } from "lucide-react"
import { toast } from "sonner"

import { StatusPill, type Tone } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import type { McpMatrix } from "@/lib/mcp"
import { usePrefs } from "@/lib/prefs"
import { cn } from "@/lib/utils"
import { MODULES } from "@/modules"
import { isAdded } from "@/modules/registry"
import type { ExtensionCategory, ModuleDef, Requirements } from "@/modules/types"

interface HostTools {
  clis: Record<string, boolean>
  env: Record<string, boolean>
  docker: boolean
}

const CATEGORIES: { id: ExtensionCategory; label: string }[] = [
  { id: "devops", label: "DevOps" },
  { id: "cloud", label: "Cloud" },
  { id: "data", label: "Data" },
  { id: "design", label: "Design" },
  { id: "business", label: "Business" },
  { id: "productivity", label: "Productivity" },
]
const categoryLabel = (c?: string) => CATEGORIES.find((x) => x.id === c)?.label ?? c ?? ""

interface ReqCheck {
  label: string
  ok: boolean
  optional?: boolean
}

/** Each requirement as a found / missing check against this machine. */
function checks(r: Requirements | undefined, tools: HostTools | null, mcp: McpMatrix | null): ReqCheck[] {
  if (!r) return []
  const out: ReqCheck[] = []
  const cli = (spec: string, optional?: boolean) => {
    const names = spec.split("|")
    const found = names.find((n) => tools?.clis[n])
    out.push({
      label: found ? `${found} found` : names.length > 1 ? `needs ${names.slice(0, -1).join(", ")} or ${names[names.length - 1]}` : optional ? `${spec} optional` : `${spec} missing`,
      ok: !!found,
      optional,
    })
  }
  for (const c of r.clis ?? []) cli(c)
  for (const m of r.mcp ?? []) {
    const found = (mcp?.servers ?? []).some((s) => s.name.toLowerCase().includes(m.toLowerCase()))
    out.push({ label: `${m} MCP ${found ? "found" : "missing"}`, ok: found })
  }
  for (const e of r.env ?? []) out.push({ label: tools?.env[e] ? `${e} set` : `${e} not set`, ok: !!tools?.env[e] })
  if (r.docker) out.push({ label: tools?.docker ? "Docker running" : "Docker not running", ok: !!tools?.docker })
  for (const c of r.optional?.clis ?? []) cli(c, true)
  return out
}

function met(r: Requirements | undefined, cs: ReqCheck[]): boolean {
  const needed = cs.filter((c) => !c.optional)
  if (needed.length === 0) return true
  return r?.anyOf ? needed.some((c) => c.ok) : needed.every((c) => c.ok)
}

function ExtensionCard({
  m,
  added,
  tools,
  mcp,
  loading,
  focused,
  onToggle,
}: {
  m: ModuleDef
  added: boolean
  tools: HostTools | null
  mcp: McpMatrix | null
  loading: boolean
  focused: boolean
  onToggle: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (focused) ref.current?.scrollIntoView({ behavior: "smooth", block: "center" })
  }, [focused])
  const Icon = m.icon
  const soon = m.status === "soon"
  const cs = checks(m.requires, tools, mcp)
  const ready = met(m.requires, cs)
  const tone = (c: ReqCheck): Tone => (c.ok ? "success" : c.optional ? "neutral" : m.requires?.anyOf && ready ? "neutral" : "warning")
  return (
    <div
      ref={ref}
      className={cn(
        "group relative flex flex-col rounded-xl border bg-card p-4 shadow-card transition-[border-color,box-shadow]",
        soon ? "opacity-70" : "hover:border-border-strong",
        focused && "border-brand/60 shadow-[0_0_0_1px_var(--brand)]",
      )}
    >
      <div className="flex items-start gap-3">
        <span className="relative flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-border-strong bg-elevated text-brand">
          <span aria-hidden className="absolute inset-0 bg-gradient-to-b from-brand-soft to-transparent" />
          <Icon className="relative size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h3 className="truncate text-sm font-semibold">{m.title}</h3>
            {added && !soon && (
              <span className="flex items-center gap-1 text-2xs text-success-fg">
                <Check className="size-3" /> in sidebar
              </span>
            )}
          </div>
          <span className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{categoryLabel(m.category)}</span>
        </div>
      </div>
      <p className="mt-2.5 text-sm text-muted-foreground">{m.description}</p>
      {cs.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1.5">
          {loading
            ? <span className="text-2xs text-subtle-foreground">Checking this machine…</span>
            : cs.map((c) => (
                <StatusPill key={c.label} tone={tone(c)}>
                  {c.label}
                </StatusPill>
              ))}
        </div>
      )}
      <div className="mt-auto flex items-center justify-between gap-2 pt-4">
        <span className="text-2xs text-subtle-foreground">
          {soon ? "On the roadmap" : !loading && !ready ? "Works once its requirements are met" : m.requires?.anyOf ? "Needs any one of these" : ""}
        </span>
        {soon ? (
          <Button size="sm" variant="secondary" disabled>
            Coming soon
          </Button>
        ) : added ? (
          <Button size="sm" variant="secondary" onClick={onToggle}>
            <Minus /> Remove
          </Button>
        ) : (
          <Button size="sm" onClick={onToggle}>
            <Plus /> Add to sidebar
          </Button>
        )}
      </div>
    </div>
  )
}

export function Extensions({ focus }: { focus?: string }) {
  const { prefs, update } = usePrefs()
  const { open } = useApp()
  const tools = usePoll<HostTools>("/api/host/tools", 60000)
  const mcp = usePoll<McpMatrix>("/api/mcp", 60000)
  const [query, setQuery] = useState("")
  const [cat, setCat] = useState<ExtensionCategory | "all">("all")
  const all = useMemo(() => MODULES.filter((m) => m.kind === "extension"), [])
  const shown = all
    .filter((m) => cat === "all" || m.category === cat)
    .filter((m) => {
      const q = query.trim().toLowerCase()
      return !q || `${m.title} ${m.description ?? ""} ${m.keywords ?? ""} ${m.category ?? ""}`.toLowerCase().includes(q)
    })
    .sort((a, b) => Number(a.status === "soon") - Number(b.status === "soon") || a.order - b.order)

  const toggle = (m: ModuleDef) => {
    const added = !isAdded(m, prefs)
    update((p) => ({ ...p, extensions: { ...p.extensions, [m.id]: { added, order: p.extensions[m.id]?.order ?? m.order } } }))
    toast.success(added ? `${m.title} added to the sidebar` : `${m.title} removed from the sidebar`, {
      action: added ? { label: "Open", onClick: () => open(m.id) } : undefined,
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex h-8 w-64 items-center gap-2 rounded-lg border bg-muted/40 px-2.5 text-sm focus-within:border-border-strong">
          <Search className="size-3.5 text-subtle-foreground" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search extensions"
            aria-label="Search extensions"
            className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-subtle-foreground"
          />
        </label>
        <div role="radiogroup" aria-label="Category" className="flex flex-wrap items-center gap-1">
          {[{ id: "all" as const, label: "All" }, ...CATEGORIES].map((c) => {
            const on = cat === c.id
            const n = c.id === "all" ? all.length : all.filter((m) => m.category === c.id).length
            if (n === 0) return null
            return (
              <button
                key={c.id}
                role="radio"
                aria-checked={on}
                onClick={() => setCat(c.id)}
                className={cn(
                  "flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-xs transition-colors",
                  on ? "border-brand/50 bg-brand-soft text-brand-fg" : "text-muted-foreground hover:border-border-strong hover:text-foreground",
                )}
              >
                {c.label}
                <span className="tabular-nums text-subtle-foreground">{n}</span>
              </button>
            )
          })}
        </div>
      </div>
      {shown.length === 0 ? (
        <p className="rounded-xl border border-dashed px-5 py-10 text-center text-sm text-muted-foreground">No extensions match.</p>
      ) : (
        <div className="grid gap-3 @2xl:grid-cols-2 @5xl:grid-cols-3">
          {shown.map((m) => (
            <ExtensionCard
              key={m.id}
              m={m}
              added={isAdded(m, prefs)}
              tools={tools.data}
              mcp={mcp.data}
              loading={tools.loading && !tools.data}
              focused={focus === m.id}
              onToggle={() => toggle(m)}
            />
          ))}
        </div>
      )}
      <p className="text-xs text-subtle-foreground">
        Extensions ship with Lucidbench and run inside the engine. Installing third-party extensions at runtime is not supported yet; see docs/EXTENDING.md.
      </p>
    </div>
  )
}
