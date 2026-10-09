import { lazy } from "react"
import { ArrowRight, Compass, Lock } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { kindIcon, NEXTUP_PATH, NEXTUP_POLL_MS, useScheduledRank, type NextView } from "@/lib/nextup"
import type { ModuleDef } from "@/modules/types"

/** Overview: the top pick and one button. Nothing spends from here; Start opens Next up with its confirm dialog. */
function NextUpTile() {
  const { open } = useApp()
  const poll = usePoll<NextView>(NEXTUP_PATH, NEXTUP_POLL_MS)
  useScheduledRank(poll.data, poll.refresh)
  const top = poll.data?.items[0]
  const Icon = top ? kindIcon(top.kind) : Compass
  return (
    <StatTile
      icon={Compass}
      label="Next up"
      onOpen={() => open("nextup")}
      loading={poll.loading && !poll.data}
      value={
        top ? (
          <span className="flex min-w-0 items-center gap-2 text-base font-semibold tracking-tight" data-testid="nextup-tile-title">
            <Icon className="size-4 shrink-0 text-brand" />
            <span className="truncate">{top.title}</span>
          </span>
        ) : (
          <span className="text-base text-muted-foreground">Nothing waiting</span>
        )
      }
      aside={top ? <StatusPill tone="info">{top.score}</StatusPill> : undefined}
      sub={
        top
          ? `${top.parts[0]?.why ?? top.context ?? ""}${poll.data && poll.data.items.length > 1 ? ` · ${poll.data.items.length - 1} more after it` : ""}`
          : "Cards, briefs, sessions, PRs and CI land here"
      }
      footer={
        top ? (
          <div className="relative z-20 flex items-center gap-2">
            <Button
              size="sm"
              data-testid="nextup-tile-start"
              onClick={() => (top.action.spends ? open("nextup", ["start", top.id]) : open("nextup"))}
            >
              {top.action.spends ? top.action.label : "See why"} <ArrowRight />
            </Button>
            {top.confidential && (
              <span className="inline-flex items-center gap-1 text-2xs text-subtle-foreground">
                <Lock className="size-3" /> stays on this machine
              </span>
            )}
          </div>
        ) : undefined
      }
    />
  )
}

function useNextUpCommands(): Command[] {
  const { open } = useApp()
  return [
    {
      id: "nextup-what",
      label: "What should I work on?",
      group: "Actions",
      icon: Compass,
      hint: "Next up, ranked",
      keywords: "next up priority todo focus rank suggest what now",
      run: () => open("nextup"),
    },
  ]
}

export const nextup: ModuleDef = {
  id: "nextup",
  title: "Next up",
  icon: Compass,
  route: "/nextup",
  section: "workspace",
  kind: "core",
  order: 0.5,
  defaultEnabled: true,
  description: "What to work on next: every card, brief, session, PR and failing check, scored and explained, with one action each.",
  keywords: "next up priority focus todo rank triage what should i work on",
  component: lazy(() => import("@/pages/NextUp")),
  overviewTile: NextUpTile,
  useCommands: useNextUpCommands,
}
