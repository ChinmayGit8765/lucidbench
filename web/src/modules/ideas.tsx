import { lazy, useEffect, useState } from "react"
import { Lightbulb } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { getJSON } from "@/lib/api"
import { useApp } from "@/lib/app"
import { IDEAS_PATH, STAGE_INFO, type IdeaSummary } from "@/lib/ideas"
import type { ModuleDef } from "@/modules/types"

/**
 * The ideas the palette lists. Deriving them reads every council session on
 * the daemon, so one answer serves every palette opening for a minute.
 */
let cache: { at: number; list: IdeaSummary[] } | null = null
const CACHE_MS = 60_000

/** Every idea, once the user types: jump straight to its page. */
function useIdeaCommands(paletteOpen: boolean): Command[] {
  const { open } = useApp()
  const [list, setList] = useState<IdeaSummary[]>(() => cache?.list ?? [])
  useEffect(() => {
    if (!paletteOpen) return
    if (cache && Date.now() - cache.at < CACHE_MS) {
      setList(cache.list)
      return
    }
    let cancelled = false
    getJSON<IdeaSummary[]>(IDEAS_PATH)
      .then((l) => {
        cache = { at: Date.now(), list: l }
        if (!cancelled) setList(l)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  return list.map((i) => ({
    id: `idea-${i.id}`,
    label: i.title || "Untitled idea",
    group: "Ideas",
    icon: Lightbulb,
    hint: `${STAGE_INFO[i.stage].label} · ${i.status}`,
    keywords: `idea ${i.project ?? ""} ${i.stage}`,
    searchOnly: true,
    run: () => open("ideas", [i.id]),
  }))
}

export const ideas: ModuleDef = {
  id: "ideas",
  title: "Ideas",
  icon: Lightbulb,
  route: "/ideas",
  section: "workspace",
  kind: "core",
  order: 1.5,
  defaultEnabled: true,
  description: "Follow each idea from braindump to merged PR: the council, the brief, the card, the agent sessions and the pull requests.",
  keywords: "idea braindump brief card session pull request pr stage timeline cost",
  component: lazy(() => import("@/pages/Ideas")),
  useCommands: useIdeaCommands,
}
