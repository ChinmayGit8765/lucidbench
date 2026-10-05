import { lazy, useEffect, useState } from "react"
import { FileText, PenLine, Vote } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { getJSON, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { COMPOSE_EVENT, COUNCIL_POLL_MS, SESSIONS_PATH, STATUS, type CouncilSummary } from "@/lib/council"
import { cn } from "@/lib/utils"
import type { ModuleDef } from "@/modules/types"

const DOT: Record<CouncilSummary["status"], string> = {
  running: "bg-info",
  draft: "bg-warning",
  approved: "bg-success",
  failed: "bg-danger",
}

/** Overview: how many briefs wait for approval, and the newest one. */
function CouncilTile() {
  const { open } = useApp()
  const poll = usePoll<CouncilSummary[]>(SESSIONS_PATH, COUNCIL_POLL_MS)
  const list = poll.data ?? []
  const drafts = list.filter((s) => s.status === "draft")
  const running = list.filter((s) => s.status === "running").length
  const recent = list.slice(0, 12).reverse()
  return (
    <StatTile
      icon={Vote}
      label="Briefs waiting for approval"
      onOpen={() => open("council", drafts.length === 1 ? [drafts[0].id] : [])}
      loading={poll.loading && !poll.data}
      value={drafts.length}
      sub={
        drafts.length > 0
          ? drafts[0].title || "Untitled brief"
          : list.length === 0
            ? "Turn a braindump into a brief"
            : "Nothing waiting"
      }
      aside={
        running > 0 ? (
          <StatusPill tone="info" pulse>
            {running} running
          </StatusPill>
        ) : undefined
      }
      footer={
        recent.length > 0 ? (
          <div className="flex items-center gap-1" aria-label="Recent council sessions">
            {recent.map((s) => (
              <span
                key={s.id}
                title={`${s.title || "Untitled"} · ${STATUS[s.status].label}`}
                className={cn("h-1.5 flex-1 rounded-full", DOT[s.status], s.status === "approved" && "opacity-60")}
              />
            ))}
          </div>
        ) : undefined
      }
    />
  )
}

/** "New braindump…", plus every brief that waits for approval once the user types. */
function useCouncilCommands(paletteOpen: boolean): Command[] {
  const { open } = useApp()
  const [list, setList] = useState<CouncilSummary[]>([])
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    getJSON<CouncilSummary[]>(SESSIONS_PATH)
      .then((l) => !cancelled && setList(l))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  return [
    {
      id: "council-new",
      label: "New braindump…",
      group: "Actions",
      icon: PenLine,
      hint: "Council",
      keywords: "council brief idea dump plan clarify",
      run: () => {
        open("council")
        // The page may already be open; ask it to focus the composer.
        setTimeout(() => window.dispatchEvent(new Event(COMPOSE_EVENT)), 50)
      },
    },
    ...list
      .filter((s) => s.status === "draft")
      .map((s) => ({
        id: `council-${s.id}`,
        label: `Review brief ${s.title || "untitled"}`,
        group: "Council",
        icon: FileText,
        hint: "waiting for approval",
        keywords: `${s.project ?? ""} ${s.input}`,
        searchOnly: true,
        run: () => open("council", [s.id]),
      })),
  ]
}

export const council: ModuleDef = {
  id: "council",
  title: "Council",
  icon: Vote,
  route: "/council",
  section: "ai",
  kind: "core",
  order: 2,
  defaultEnabled: true,
  description: "Turn a messy braindump into a clear brief: one model drafts, two critique, and you approve.",
  keywords: "braindump brief idea clarify models critique review plan",
  component: lazy(() => import("@/pages/Council")),
  useCommands: useCouncilCommands,
  overviewTile: CouncilTile,
}
