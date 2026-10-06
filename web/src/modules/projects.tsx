import { lazy, useEffect, useState } from "react"
import { ArrowRight, FolderKanban, OctagonAlert } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { getJSON, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  blockedNeeds,
  CATEGORIES,
  categoryInfo,
  countLabel,
  PROJECTS_POLL_MS,
  typeInfo,
  type Project,
  type ProjectList,
} from "@/lib/projects"
import type { AttentionItem, ModuleDef } from "@/modules/types"

function ProjectsTile() {
  const { open } = useApp()
  const poll = usePoll<ProjectList>("/api/projects", PROJECTS_POLL_MS)
  const pl = poll.data
  const list = pl?.projects ?? []
  const blocked = blockedNeeds(list)
  const openNeeds = list.reduce((n, p) => n + p.progress.total - p.progress.done, 0)
  return (
    <StatTile
      icon={FolderKanban}
      label="Projects"
      onOpen={() => open("projects")}
      loading={poll.loading && !pl}
      value={pl?.configured ? list.length : <span className="text-base font-medium text-muted-foreground">Not set up</span>}
      aside={
        blocked.length > 0 ? (
          <StatusPill tone="warning">{blocked.length} blocked</StatusPill>
        ) : pl?.configured && list.length > 0 ? (
          <StatusPill tone="success">{list.filter((p) => p.status === "active").length} active</StatusPill>
        ) : undefined
      }
      sub={
        pl?.configured ? (
          <>
            {openNeeds} open {openNeeds === 1 ? "need" : "needs"}
            {blocked.length > 0 && <span className="text-warning-fg"> · {blocked.length} blocked</span>}
          </>
        ) : (
          <span className="font-mono">lucid projects init</span>
        )
      }
      footer={
        <div className="space-y-1.5">
          <div className="flex h-1.5 gap-0.5 overflow-hidden rounded-full bg-muted">
            {CATEGORIES.map((cat) => {
              const n = list.filter((p) => p.category === cat.id).length
              return n > 0 ? (
                <span key={cat.id} title={countLabel(cat, n)} style={{ flexGrow: n, backgroundColor: cat.color }} className="h-full" />
              ) : null
            })}
          </div>
          <div className="flex items-center gap-2.5 text-xs tabular-nums text-muted-foreground">
            {CATEGORIES.map((cat) => {
              const n = list.filter((p) => p.category === cat.id).length
              return n > 0 ? (
                <span key={cat.id} title={countLabel(cat, n)} className="flex items-center gap-1">
                  <span className="size-1.5 rounded-full" style={{ backgroundColor: cat.color }} />
                  {n}
                </span>
              ) : null
            })}
          </div>
        </div>
      }
    />
  )
}

/** "Open project …" for every project, listed once the user types. */
function useProjectCommands(paletteOpen: boolean): Command[] {
  const { openProject } = useApp()
  const [list, setList] = useState<Project[]>([])
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    getJSON<ProjectList>("/api/projects")
      .then((l) => !cancelled && setList(l.projects))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  return list.map((p) => ({
    id: `project-${p.id}`,
    label: `Open project ${p.name}`,
    group: "Projects",
    icon: typeInfo(p.type).icon,
    hint: `${categoryInfo(p.category)?.one ?? p.category} · ${p.status}`,
    keywords: `${p.id} ${p.type} ${p.repo ?? ""} ${p.linear ?? ""}`,
    searchOnly: true,
    run: () => openProject(p.id),
  }))
}

/** Needs attention: needs that block a project. */
function useProjectAttention(): AttentionItem[] | null {
  const { openProject } = useApp()
  const poll = usePoll<ProjectList>("/api/projects", PROJECTS_POLL_MS)
  if (!poll.data) return poll.error ? [] : null
  const list = poll.data.projects
  return blockedNeeds(list)
    .slice(0, 6)
    .map(({ project, need }): AttentionItem => {
      const from = need.from ? list.find((p) => p.id === need.from) : undefined
      return {
        key: `need-${project.id}-${need.what}`,
        severity: "warning",
        icon: <OctagonAlert className="size-3.5 text-warning" />,
        title: (
          <>
            {project.name} is blocked on {need.what}
          </>
        ),
        meta: from ? `needs it from ${from.name}` : `${project.progress.done}/${project.progress.total} needs done`,
        action: (
          <Button variant="ghost" size="sm" onClick={() => openProject(project.id)}>
            View <ArrowRight />
          </Button>
        ),
      }
    })
}

export const projects: ModuleDef = {
  id: "projects",
  title: "Projects",
  icon: FolderKanban,
  route: "/projects",
  section: "workspace",
  kind: "core",
  order: 2,
  defaultEnabled: true,
  description: "Your portfolio: what you build, what builds into what, and what each project needs.",
  keywords: "portfolio products tools needs dependencies graph",
  component: lazy(() => import("@/pages/Projects")),
  useCommands: useProjectCommands,
  overviewTile: ProjectsTile,
  useAttention: useProjectAttention,
}
