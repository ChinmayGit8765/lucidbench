import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import {
  ArrowRight,
  ChevronDown,
  Circle,
  CircleCheck,
  CircleDot,
  ClipboardCheck,
  EyeOff,
  FolderGit2,
  FolderKanban,
  GitBranch,
  Globe,
  LayoutGrid,
  Lock,
  OctagonAlert,
  PenTool,
  Play,
  Search,
  Share2,
  Users,
  X,
} from "lucide-react"

import { AssessSheet } from "@/components/AssessSheet"
import { CopyCommand } from "@/components/CopyCommand"
import { DeployRows } from "@/components/Deploys"
import { GraphLegend, ProjectGraph, graphEdges } from "@/components/ProjectGraph"
import { ProjectSheet, type ProjectTab } from "@/components/ProjectSheet"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { usePictureAdded, usePictureSummary } from "@/lib/picture"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { RISK, useKinds } from "@/lib/assess"
import { useCloudAdded, useCloudDeploys, type CloudDeploy } from "@/lib/cloud"
import {
  blockedNeeds,
  CATEGORIES,
  categoryColor,
  categoryInfo,
  NEED,
  PROJECTS_POLL_MS,
  repoURL,
  STATUS,
  STATUSES,
  TYPES,
  typeInfo,
  type Category,
  type Need,
  type Project,
  type ProjectList,
  type Visibility,
} from "@/lib/projects"
import { cn } from "@/lib/utils"
import { homeHint } from "@/lib/work"
import type { ModulePageProps } from "@/modules/types"

type View = "grid" | "graph"

const param = (k: string) => new URLSearchParams(location.search).get(k)

/** Writes one query parameter in place, keeping the others (such as ?theme=). */
function setParam(k: string, v: string | null) {
  const q = new URLSearchParams(location.search)
  if (v === null) q.delete(k)
  else q.set(k, v)
  const s = q.toString()
  history.replaceState(null, "", `${location.pathname}${s ? `?${s}` : ""}`)
}

const VISIBILITY: Record<Visibility, { label: string; icon: typeof Globe; hint: string }> = {
  public: { label: "Public", icon: Globe, hint: "Public repository" },
  private: { label: "Private", icon: EyeOff, hint: "Private repository" },
  confidential: { label: "Confidential", icon: Lock, hint: "Confidential: never sent to AI providers" },
}

const DOT: Record<string, string> = {
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-danger",
  info: "bg-info",
  neutral: "bg-neutral",
}

/* ---------- small pieces ---------- */

function CategoryDot({ category, className }: { category: string; className?: string }) {
  return <span aria-hidden className={cn("size-2 shrink-0 rounded-full", className)} style={{ backgroundColor: categoryColor(category) }} />
}

function TypeTile({ p }: { p: Project }) {
  const t = typeInfo(p.type)
  const Icon = t.icon
  const c = categoryColor(p.category)
  return (
    <span
      title={t.label}
      style={{
        color: c,
        backgroundColor: `color-mix(in oklch, ${c} 12%, transparent)`,
        borderColor: `color-mix(in oklch, ${c} 24%, transparent)`,
      }}
      className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg border [&_svg]:size-4"
    >
      <Icon />
    </span>
  )
}

function VisibilityChip({ v }: { v: Visibility }) {
  const info = VISIBILITY[v]
  if (!info) return null
  const Icon = info.icon
  if (v === "confidential") {
    return (
      <span
        title={info.hint}
        className="inline-flex h-5 items-center gap-1 rounded-md border border-warning/30 bg-warning-soft px-1.5 text-2xs font-medium text-warning-fg [&_svg]:size-3"
      >
        <Icon /> Confidential
        <span className="font-normal opacity-80">· never sent to AI providers</span>
      </span>
    )
  }
  return (
    <Badge title={info.hint}>
      <Icon /> {info.label}
    </Badge>
  )
}

function NeedIcon({ status, className }: { status: Need["status"]; className?: string }) {
  const cls = cn("size-3.5 shrink-0", className)
  if (status === "done") return <CircleCheck aria-label="done" className={cn(cls, "text-success")} />
  if (status === "doing") return <CircleDot aria-label="doing" className={cn(cls, "text-info")} />
  if (status === "blocked") return <OctagonAlert aria-label="blocked" className={cn(cls, "text-warning")} />
  return <Circle aria-label="to do" className={cn(cls, "text-subtle-foreground")} />
}

/** A thin bar split into done, doing and blocked shares of all needs. */
function NeedsBar({ needs, className }: { needs: Need[]; className?: string }) {
  const n = needs.length
  if (n === 0) return null
  const share = (s: Need["status"]) => (needs.filter((x) => x.status === s).length / n) * 100
  return (
    <div aria-hidden className={cn("flex h-1.5 overflow-hidden rounded-full bg-muted", className)}>
      {(["done", "doing", "blocked"] as const).map((s) => (
        <span key={s} style={{ width: `${share(s)}%`, backgroundColor: NEED[s].color }} className="h-full" />
      ))}
    </div>
  )
}

function ProjectLink({ id, byId, onFocus, className }: { id: string; byId: Map<string, Project>; onFocus: (id: string) => void; className?: string }) {
  const t = byId.get(id)
  return (
    <button
      type="button"
      onClick={() => onFocus(id)}
      title={t ? `Show ${t.name}` : id}
      className={cn(
        "inline-flex h-5 max-w-full items-center gap-1.5 rounded-md border bg-background/60 px-1.5 text-2xs font-medium text-muted-foreground transition-colors hover:border-border-strong hover:text-foreground",
        className,
      )}
    >
      <CategoryDot category={t?.category ?? ""} className="size-1.5" />
      <span className="truncate">{t?.name ?? id}</span>
    </button>
  )
}

/* ---------- project card ---------- */

function ProjectCard({
  p,
  byId,
  onFocus,
  focused,
  kindName,
  onAssess,
  onTeam,
  deploys,
  pictures,
}: {
  p: Project
  byId: Map<string, Project>
  onFocus: (id: string) => void
  focused: boolean
  kindName?: string
  onAssess: (p: Project) => void
  /** Opens the project's detail on its Team tab. */
  onTeam: (p: Project) => void
  /** Live status of p.deploy, by position; null when the Cloud extension is not added. */
  deploys: CloudDeploy[] | undefined | null
  /** How many diagrams and canvases p has; null when the Picture extension is not added. */
  pictures: number | null
}) {
  const { open } = useApp()
  const st = STATUS[p.status] ?? { tone: "neutral" as const, label: p.status }
  const t = typeInfo(p.type)
  return (
    <Card
      id={`project-${p.id}`}
      className={cn(
        "flex scroll-mt-24 flex-col p-4 transition-[border-color,box-shadow] duration-300 hover:border-border-strong",
        focused && "border-brand/60 shadow-[0_0_0_3px_var(--brand-soft)]",
      )}
    >
      <div className="flex items-start gap-3">
        <TypeTile p={p} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium" title={p.name}>
            {p.name}
          </div>
          <div className="truncate text-xs text-subtle-foreground">
            {t.label} · <span className="font-mono">{p.id}</span>
          </div>
        </div>
        <StatusPill tone={st.tone}>{st.label}</StatusPill>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        <VisibilityChip v={p.visibility} />
        {p.repo && (
          <a
            href={repoURL(p.repo)}
            target="_blank"
            rel="noreferrer"
            title={`Open ${p.repo}`}
            className="inline-flex h-5 min-w-0 items-center gap-1 rounded-md border bg-muted/60 px-1.5 font-mono text-2xs text-muted-foreground transition-colors hover:border-border-strong hover:text-foreground [&_svg]:size-3"
          >
            <GitBranch className="shrink-0" />
            <span className="truncate">{p.repo.replace(/^https?:\/\/[^/]+\//, "").split("/").pop()}</span>
          </a>
        )}
        {p.linear && <Badge className="font-mono" title="Linear">{p.linear}</Badge>}
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        {p.assessment && (
          <Badge title={`Assessed as ${kindName ?? p.assessment.kind}`}>
            <ClipboardCheck /> {kindName ?? p.assessment.kind}
          </Badge>
        )}
        {p.assessment && p.risk && RISK[p.risk] && (
          <StatusPill tone={RISK[p.risk].tone}>{RISK[p.risk].label}</StatusPill>
        )}
        <Button variant="ghost" size="sm" className="ml-auto h-6 px-2 text-xs" onClick={() => onTeam(p)} aria-label={`Team of ${p.name}`}>
          <Users /> Team
        </Button>
        <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={() => onAssess(p)}>
          <ClipboardCheck /> {p.assessment ? "Re-assess" : "Assess"}
        </Button>
      </div>

      {p.summary && <p className="mt-2.5 line-clamp-2 text-sm text-muted-foreground">{p.summary}</p>}

      {p.local_path && (
        <div className="mt-2.5 flex items-center gap-2">
          <span className="flex min-w-0 flex-1 items-center gap-1.5 font-mono text-2xs text-muted-foreground" title={`local_path: ${homeHint(p.local_path)}`}>
            <FolderGit2 className="size-3 shrink-0 text-subtle-foreground" />
            <span className="truncate">{homeHint(p.local_path)}</span>
          </span>
          {p.visibility !== "confidential" && (
            <Button variant="ghost" size="sm" className="h-6 shrink-0 px-2 text-xs" onClick={() => open("work", ["new", "project", p.id])}>
              <Play /> Start work
            </Button>
          )}
        </div>
      )}

      {pictures !== null && (
        <div className="mt-2.5 flex items-center gap-2">
          <span className="flex min-w-0 flex-1 items-center gap-1.5 text-2xs text-muted-foreground">
            <PenTool className="size-3 shrink-0 text-subtle-foreground" />
            {pictures === 0 ? "No diagrams or canvases" : `${pictures} ${pictures === 1 ? "diagram or canvas" : "diagrams and canvases"}`}
          </span>
          <Button variant="ghost" size="sm" className="h-6 shrink-0 px-2 text-xs" onClick={() => open("picture", [p.id])}>
            <PenTool /> Picture
          </Button>
        </div>
      )}

      {p.deploy && p.deploy.length > 0 && <DeployRows entries={p.deploy} live={deploys} />}

      {(p.builds_into.length > 0 || p.built_by.length > 0) && (
        <div className="mt-3 space-y-1.5">
          {p.builds_into.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="flex items-center gap-1 text-2xs font-medium text-subtle-foreground">
                Builds into <ArrowRight className="size-3" />
              </span>
              {p.builds_into.map((id) => (
                <ProjectLink key={id} id={id} byId={byId} onFocus={onFocus} />
              ))}
            </div>
          )}
          {p.built_by.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-2xs font-medium text-subtle-foreground">Built with</span>
              {p.built_by.map((id) => (
                <ProjectLink key={id} id={id} byId={byId} onFocus={onFocus} />
              ))}
            </div>
          )}
        </div>
      )}

      {p.needs.length > 0 && (
        <div className="mt-3 border-t pt-3">
          <div className="mb-2 flex items-center gap-2.5">
            <span className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Needs</span>
            <NeedsBar needs={p.needs} className="flex-1" />
            <span className="text-2xs tabular-nums text-muted-foreground">
              {p.progress.done}/{p.progress.total}
            </span>
          </div>
          <ul className="space-y-1">
            {p.needs.map((n, i) => (
              <li key={i} className="flex items-start gap-2 text-sm">
                <NeedIcon status={n.status} className="mt-[3px]" />
                <span className={cn("min-w-0 flex-1", n.status === "done" && "text-subtle-foreground line-through decoration-border-strong")}>
                  {n.what}
                </span>
                {n.from && (
                  <span className="flex shrink-0 items-center gap-1 text-2xs text-subtle-foreground">
                    from
                    <ProjectLink id={n.from} byId={byId} onFocus={onFocus} className="max-w-36" />
                  </span>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {p.needed_by.length > 0 && (
        <div className="mt-auto flex flex-wrap items-center gap-1.5 pt-3">
          <span className="text-2xs font-medium text-subtle-foreground">Needed by</span>
          {p.needed_by.map((id) => (
            <ProjectLink key={id} id={id} byId={byId} onFocus={onFocus} />
          ))}
        </div>
      )}
    </Card>
  )
}

/* ---------- summary ---------- */

function SummaryStrip({ ps, tab, onTab }: { ps: Project[]; tab: Category | "all"; onTab: (c: Category | "all") => void }) {
  const count = (s: string) => ps.filter((p) => p.status === s).length
  const needs = ps.flatMap((p) => p.needs)
  const done = needs.filter((n) => n.status === "done").length
  const blocked = needs.filter((n) => n.status === "blocked").length
  return (
    <Card className="grid grid-cols-3 divide-x overflow-hidden @3xl:grid-cols-[repeat(5,minmax(0,1fr))_minmax(0,1.7fr)] max-@3xl:divide-y">
      {CATEGORIES.map((c) => {
        const n = ps.filter((p) => p.category === c.id).length
        const on = tab === c.id
        return (
          <button
            key={c.id}
            onClick={() => onTab(on ? "all" : c.id)}
            aria-pressed={on}
            className={cn(
              "relative min-w-0 px-4 py-3.5 text-left outline-none transition-colors hover:bg-accent/40 focus-visible:bg-accent/60",
              on && "bg-accent/50",
            )}
          >
            <span aria-hidden className="absolute inset-x-0 top-0 h-0.5" style={{ backgroundColor: on ? c.color : "transparent" }} />
            <div className="flex items-center gap-1.5 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
              <CategoryDot category={c.id} className="size-1.5" />
              {c.label}
            </div>
            <div className={cn("mt-1 text-xl font-semibold tabular-nums tracking-tight", n === 0 && "text-subtle-foreground")}>{n}</div>
            <div className="mt-0.5 truncate text-xs text-muted-foreground" title={c.blurb}>
              {c.short}
            </div>
          </button>
        )
      })}
      <div className="col-span-3 flex min-w-0 flex-col justify-center gap-2 px-4 py-3 @3xl:col-span-1">
        <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
          {(["active", "paused", "frozen", "shipped"] as const)
            .filter((s) => s !== "shipped" || count(s) > 0)
            .map((s) => (
              <span key={s} className="flex items-baseline gap-1.5">
                <span className="text-xl font-semibold tabular-nums tracking-tight">{count(s)}</span>
                <span className="flex items-center gap-1 text-xs text-muted-foreground">
                  <span aria-hidden className={cn("size-1.5 rounded-full", DOT[STATUS[s].tone])} />
                  {STATUS[s].label.toLowerCase()}
                </span>
              </span>
            ))}
        </div>
        <div className="flex items-center gap-2.5">
          <NeedsBar needs={needs} className="flex-1" />
          <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
            {done}/{needs.length} needs done
            {blocked > 0 && <span className="text-warning-fg"> · {blocked} blocked</span>}
          </span>
        </div>
      </div>
    </Card>
  )
}

/* ---------- toolbar ---------- */

function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: T
  options: { id: T; label: ReactNode; count?: number }[]
  onChange: (v: T) => void
}) {
  return (
    <div role="radiogroup" aria-label={label} className="flex flex-wrap items-center gap-0.5 rounded-lg border bg-muted/40 p-0.5">
      {options.map((o) => {
        const on = value === o.id
        return (
          <button
            key={o.id}
            role="radio"
            aria-checked={on}
            onClick={() => onChange(o.id)}
            className={cn(
              "flex h-6 items-center gap-1.5 rounded-md px-2 text-xs transition-colors",
              on ? "bg-elevated font-medium text-foreground shadow-card" : "text-muted-foreground hover:text-foreground",
              o.count === 0 && !on && "opacity-55",
            )}
          >
            {o.label}
            {o.count !== undefined && <span className="tabular-nums text-subtle-foreground">{o.count}</span>}
          </button>
        )
      })}
    </div>
  )
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  options: { id: string; label: string }[]
}) {
  return (
    <label className="relative flex items-center">
      <span className="sr-only">{label}</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={cn(
          "h-7 appearance-none rounded-md border bg-muted/40 pl-2.5 pr-7 text-xs outline-none transition-colors hover:border-border-strong focus-visible:border-ring",
          value ? "border-brand/40 text-foreground" : "text-muted-foreground",
        )}
      >
        <option value="">{label}: any</option>
        {options.map((o) => (
          <option key={o.id} value={o.id}>
            {label}: {o.label}
          </option>
        ))}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2 size-3.5 text-subtle-foreground" />
    </label>
  )
}

/* ---------- needs list (graph view) ---------- */

function OpenNeeds({ ps, byId, onFocus }: { ps: Project[]; byId: Map<string, Project>; onFocus: (id: string) => void }) {
  const order = { blocked: 0, doing: 1, todo: 2, done: 3 }
  const rows = ps
    .flatMap((p) => p.needs.map((n) => ({ p, n })))
    .filter((r) => r.n.status !== "done")
    .sort((a, b) => order[a.n.status] - order[b.n.status] || a.p.name.localeCompare(b.p.name))
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between px-5 pb-3 pt-4">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          Open needs
          <span className="font-normal tabular-nums text-subtle-foreground">{rows.length}</span>
        </h2>
        <span className="text-xs text-subtle-foreground">Blocked first</span>
      </div>
      {rows.length === 0 ? (
        <p className="border-t px-5 py-6 text-center text-sm text-muted-foreground">Every need is done.</p>
      ) : (
        <ul className="divide-y border-t">
          {rows.map(({ p, n }, i) => (
            <li key={`${p.id}-${i}`} className="flex items-center gap-3 px-5 py-2 transition-colors hover:bg-accent/30">
              <NeedIcon status={n.status} />
              <span className="min-w-0 flex-1 truncate text-sm">{n.what}</span>
              <span className="flex shrink-0 items-center gap-1.5 text-2xs text-subtle-foreground">
                <ProjectLink id={p.id} byId={byId} onFocus={onFocus} />
                {n.from && (
                  <>
                    needs it from
                    <ProjectLink id={n.from} byId={byId} onFocus={onFocus} />
                  </>
                )}
              </span>
              <StatusPill tone={NEED[n.status].tone} className="w-[4.75rem] justify-center">
                {NEED[n.status].label}
              </StatusPill>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

/* ---------- page ---------- */

function ProjectsSkeleton() {
  return (
    <div className="space-y-6" aria-busy="true" aria-label="Loading projects">
      <Skeleton className="h-[86px] rounded-xl" />
      <Skeleton className="h-7 w-96" />
      <div className="grid gap-3 @3xl:grid-cols-2 @6xl:grid-cols-3">
        {[0, 1, 2, 3, 4, 5].map((i) => (
          <Card key={i} className="space-y-3 p-4">
            <div className="flex items-center gap-3">
              <Skeleton className="size-8 rounded-lg" />
              <div className="flex-1 space-y-1.5">
                <Skeleton className="h-3.5 w-28" />
                <Skeleton className="h-3 w-36" />
              </div>
            </div>
            <Skeleton className="h-3.5 w-3/4" />
            <Skeleton className="h-3.5 w-1/2" />
          </Card>
        ))}
      </div>
    </div>
  )
}

export default function Projects({ subpath }: ModulePageProps) {
  const { focus, navigate } = useApp()
  const poll = usePoll<ProjectList>("/api/projects", PROJECTS_POLL_MS)
  const data = poll.data
  const all = useMemo(() => data?.projects ?? [], [data])
  const byId = useMemo(() => new Map(all.map((p) => [p.id, p])), [all])

  const [view, setViewState] = useState<View>(() => (param("view") === "graph" ? "graph" : "grid"))
  const [tab, setTab] = useState<Category | "all">(() => (categoryInfo(param("category") ?? "")?.id ?? "all"))
  const [status, setStatus] = useState("")
  const [type, setType] = useState("")
  const [visibility, setVisibility] = useState("")
  const [q, setQ] = useState("")
  const [focused, setFocused] = useState<string | null>(null)
  const [assessing, setAssessing] = useState<Project | null>(null)
  // /projects/<id>/team or /projects/<id>/sections opens that project's detail.
  const [detail, setDetail] = useState<{ id: string; tab: ProjectTab } | null>(null)
  const route = subpath.join("/")
  useEffect(() => {
    const [id, t] = route.split("/")
    if (id) setDetail({ id, tab: t === "sections" ? "sections" : "team" })
  }, [route])
  const kinds = useKinds()
  // Deploy status runs the user's CLIs, so it waits for the Cloud extension to be added.
  const cloudAdded = useCloudAdded()
  const deployPoll = useCloudDeploys(cloudAdded && all.some((p) => (p.deploy?.length ?? 0) > 0))
  // Diagram counts come from one list, and only once the Picture extension is added.
  const pictureAdded = usePictureAdded()
  const pictureSummary = usePictureSummary(pictureAdded)
  const timer = useRef<number | undefined>(undefined)

  const setView = (v: View) => {
    setViewState(v)
    setParam("view", v === "graph" ? "graph" : null)
  }
  const chooseTab = (c: Category | "all") => {
    setTab(c)
    setParam("category", c === "all" ? null : c)
  }

  const query = q.trim().toLowerCase()
  const match = (p: Project) =>
    (tab === "all" || p.category === tab) &&
    (!status || p.status === status) &&
    (!type || p.type === type) &&
    (!visibility || p.visibility === visibility) &&
    (!query ||
      [p.name, p.id, p.summary ?? "", p.repo ?? "", p.linear ?? "", p.type, ...p.needs.map((n) => n.what)]
        .join(" ")
        .toLowerCase()
        .includes(query))
  const shown = all.filter(match)
  const filtered = !!(status || type || visibility || query)
  const clear = () => {
    setStatus("")
    setType("")
    setVisibility("")
    setQ("")
  }

  /** Shows a project's card: switches to the grid, clears filters that hide it, scrolls and highlights. */
  const show = (id: string) => {
    const p = byId.get(id)
    if (!p) return
    setView("grid")
    if (!match(p)) {
      clear()
      if (tab !== "all" && tab !== p.category) chooseTab("all")
    }
    setFocused(id)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setFocused(null), 2200)
    requestAnimationFrame(() =>
      requestAnimationFrame(() => document.getElementById(`project-${id}`)?.scrollIntoView({ behavior: "smooth", block: "center" })),
    )
  }

  // A project opened from elsewhere (command palette, overview).
  const focusKey = focus ? `${focus.id}:${focus.n}` : ""
  useEffect(() => {
    if (focus && byId.has(focus.id)) show(focus.id)
  }, [focusKey, byId.size])
  useEffect(() => () => window.clearTimeout(timer.current), [])

  const types = Object.keys(TYPES).filter((t) => all.some((p) => p.type === t))
  const blocked = blockedNeeds(all).length
  const edges = graphEdges(shown)
  const unlinked = shown.filter((p) => !edges.some((e) => e.from === p.id || e.to === p.id))

  const sections =
    tab === "all"
      ? CATEGORIES.map((c) => ({ c, items: shown.filter((p) => p.category === c.id) })).filter((s) => s.items.length > 0)
      : [{ c: categoryInfo(tab)!, items: shown }]
  const other = tab === "all" ? shown.filter((p) => !categoryInfo(p.category)) : []

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<FolderKanban />}
        title="Projects"
        description="Everything you build, what kind of thing it is, and what needs what. Read from your own projects file; it never lives in the repository."
        actions={
          <>
            {data?.configured && (
              <Segmented<View>
                label="View"
                value={view}
                onChange={setView}
                options={[
                  { id: "grid", label: <><LayoutGrid className="size-3.5" /> Cards</> },
                  { id: "graph", label: <><Share2 className="size-3.5" /> What needs what</> },
                ]}
              />
            )}
            <RefreshButton refreshing={poll.refreshing} updatedAt={poll.updatedAt} />
          </>
        }
      />

      {poll.loading && !data && <ProjectsSkeleton />}
      {poll.error && !data && <ErrorState title="Could not load projects" message={poll.error.message} onRetry={poll.refresh} />}

      {data && data.errors.length > 0 && (
        <div role="alert" className="rounded-lg border border-warning/30 bg-warning-soft p-3.5">
          <div className="flex items-center gap-2 text-sm font-medium">
            <OctagonAlert className="size-4 text-warning" />
            {data.errors.length} {data.errors.length === 1 ? "problem" : "problems"} in{" "}
            <span className="font-mono text-xs">{data.path_hint}</span>
          </div>
          <ul className="mt-2 space-y-0.5 pl-6 font-mono text-xs text-muted-foreground">
            {data.errors.slice(0, 12).map((e) => (
              <li key={e} className="list-disc break-words">
                {e}
              </li>
            ))}
            {data.errors.length > 12 && <li className="list-none">… and {data.errors.length - 12} more</li>}
          </ul>
        </div>
      )}

      {data && !data.configured && (
        <Card className="overflow-hidden">
          <div className="grid @3xl:grid-cols-[1fr_1.1fr]">
            <EmptyState
              className="py-9"
              icon={<FolderKanban />}
              satellites={CATEGORIES.slice(0, 4).map((c) => (
                <span
                  key={c.id}
                  style={{ color: c.color, backgroundColor: `color-mix(in oklch, ${c.color} 12%, transparent)` }}
                  className="flex size-6 items-center justify-center rounded-md border [&_svg]:size-3.5"
                >
                  <c.icon />
                </span>
              ))}
              title="Map what you build"
              description="Start from the git repositories already on this machine: pick a folder, tick the ones to add, and Lucidbench writes them to projects.yaml. Work needs a project to start an agent in."
            >
              <Button onClick={() => navigate("/setup/projects")} data-testid="projects-import">
                <FolderGit2 /> Import from a folder
              </Button>
            </EmptyState>
            <div className="space-y-3 border-t bg-muted/30 p-5 @3xl:border-l @3xl:border-t-0">
              <div className="text-xs font-medium text-muted-foreground">Or by hand · 1 · Write the example file</div>
              <CopyCommand command="lucid projects init" />
              <div className="pt-1 text-xs font-medium text-muted-foreground">2 · Edit it: products, portfolio pieces, tools, experiments, coursework, and what builds into what</div>
              <div className="flex min-h-9 items-center break-all rounded-lg border bg-background px-3 py-2 font-mono text-xs">{data.path_hint}</div>
              <p className="pt-1 text-xs text-subtle-foreground">
                The file stays on this machine and is re-read on every refresh. Set <span className="font-mono">LUCID_PROJECTS</span> to keep it
                somewhere else.
              </p>
            </div>
          </div>
        </Card>
      )}

      {data?.configured && (
        <>
          <SummaryStrip ps={all} tab={tab} onTab={chooseTab} />

          <div className="flex flex-wrap items-center justify-between gap-2">
            <Segmented<Category | "all">
              label="Category"
              value={tab}
              onChange={chooseTab}
              options={[
                { id: "all", label: "All", count: all.length },
                ...CATEGORIES.map((c) => ({ id: c.id, label: c.label, count: all.filter((p) => p.category === c.id).length })),
              ]}
            />
            <div className="flex flex-wrap items-center gap-1.5">
              <label className="relative flex items-center">
                <span className="sr-only">Search projects</span>
                <Search className="pointer-events-none absolute left-2 size-3.5 text-subtle-foreground" />
                <input
                  value={q}
                  onChange={(e) => setQ(e.target.value)}
                  placeholder="Search projects"
                  className="h-7 w-44 rounded-md border bg-muted/40 pl-7 pr-2 text-xs outline-none transition-colors placeholder:text-subtle-foreground hover:border-border-strong focus-visible:border-ring"
                />
              </label>
              <FilterSelect label="Status" value={status} onChange={setStatus} options={STATUSES.map((s) => ({ id: s, label: STATUS[s].label }))} />
              <FilterSelect label="Type" value={type} onChange={setType} options={types.map((t) => ({ id: t, label: TYPES[t].label }))} />
              <FilterSelect
                label="Visibility"
                value={visibility}
                onChange={setVisibility}
                options={(Object.keys(VISIBILITY) as Visibility[]).map((v) => ({ id: v, label: VISIBILITY[v].label }))}
              />
              {filtered && (
                <Button variant="ghost" size="sm" onClick={clear}>
                  <X /> Clear
                </Button>
              )}
            </div>
          </div>

          {shown.length === 0 && (
            <Card className="border-dashed">
              <EmptyState
                icon={<Search />}
                title={all.length === 0 ? "No projects yet" : "No projects match"}
                description={all.length === 0 ? `Add projects to ${data.path_hint}.` : "Try another category, or clear the filters."}
              >
                {filtered && (
                  <Button variant="secondary" size="sm" onClick={clear}>
                    Clear filters
                  </Button>
                )}
              </EmptyState>
            </Card>
          )}

          {view === "graph" && shown.length > 0 && (
            <div className="space-y-4">
              <Card className="overflow-hidden">
                <div className="flex flex-wrap items-center justify-between gap-3 px-5 pb-2 pt-4">
                  <div>
                    <h2 className="text-sm font-semibold">What needs what</h2>
                    <p className="mt-0.5 text-sm text-muted-foreground">Tools on the left feed what they build; hover a project to trace it, click to open its card.</p>
                  </div>
                  {blocked > 0 && <StatusPill tone="warning">{blocked} blocked</StatusPill>}
                </div>
                <div className="px-3 pb-2">
                  {edges.length > 0 ? (
                    <ProjectGraph projects={shown} onOpen={show} />
                  ) : (
                    <p className="px-2 py-8 text-center text-sm text-muted-foreground">
                      No links between these projects. Add <span className="font-mono">builds_into</span> or a need with{" "}
                      <span className="font-mono">from</span>.
                    </p>
                  )}
                </div>
                <div className="flex flex-wrap items-center justify-between gap-3 border-t bg-muted/20 px-5 py-2.5">
                  <GraphLegend />
                  <div className="flex items-center gap-3 text-xs text-muted-foreground">
                    {CATEGORIES.filter((c) => shown.some((p) => p.category === c.id)).map((c) => (
                      <span key={c.id} className="flex items-center gap-1.5">
                        <CategoryDot category={c.id} />
                        {c.label}
                      </span>
                    ))}
                  </div>
                </div>
                {unlinked.length > 0 && (
                  <div className="flex flex-wrap items-center gap-1.5 border-t px-5 py-3">
                    <span className="mr-1 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
                      Not linked · {unlinked.length}
                    </span>
                    {unlinked.map((p) => (
                      <ProjectLink key={p.id} id={p.id} byId={byId} onFocus={show} />
                    ))}
                  </div>
                )}
              </Card>
              <OpenNeeds ps={shown} byId={byId} onFocus={show} />
            </div>
          )}

          {view === "grid" &&
            [...sections, ...(other.length ? [{ c: null, items: other }] : [])].map(({ c, items }) => (
              <section key={c?.id ?? "other"} className="space-y-3">
                <div className="flex items-center gap-2.5">
                  {c ? <CategoryDot category={c.id} /> : <CategoryDot category="" />}
                  <h2 className="text-sm font-semibold">{c?.label ?? "Other"}</h2>
                  {c && <span className="text-sm text-subtle-foreground">{c.blurb}</span>}
                  <span className="ml-auto text-xs tabular-nums text-subtle-foreground">
                    {items.length} {items.length === 1 ? "project" : "projects"}
                  </span>
                </div>
                <div className="grid gap-3 @3xl:grid-cols-2 @6xl:grid-cols-3">
                  {items.map((p) => (
                    <ProjectCard
                      key={p.id}
                      p={p}
                      byId={byId}
                      onFocus={show}
                      focused={focused === p.id}
                      kindName={kinds.find((k) => k.id === p.assessment?.kind)?.name}
                      onAssess={setAssessing}
                      onTeam={(x) => setDetail({ id: x.id, tab: "team" })}
                      pictures={pictureAdded ? (pictureSummary.data?.find((s) => s.project === p.id)?.count ?? 0) : null}
                      deploys={cloudAdded ? (deployPoll.data ? (deployPoll.data.projects[p.id] ?? []) : deployPoll.error ? [] : undefined) : null}
                    />
                  ))}
                </div>
              </section>
            ))}
        </>
      )}
      <ProjectSheet
        project={detail ? (byId.get(detail.id) ?? null) : null}
        tab={detail?.tab ?? "team"}
        onTab={(t) => setDetail((d) => (d ? { ...d, tab: t } : d))}
        onClose={() => {
          setDetail(null)
          if (route) navigate("/projects")
        }}
      />
      <AssessSheet project={assessing} kinds={kinds} onClose={() => setAssessing(null)} onSaved={poll.refresh} />
    </div>
  )
}
