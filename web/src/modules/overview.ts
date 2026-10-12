import { lazy } from "react"
import { Compass, House } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { useApp } from "@/lib/app"
import type { ModuleDef } from "@/modules/types"

function useHomeCommands(): Command[] {
  const { open } = useApp()
  return [
    {
      id: "nextup-what",
      label: "What should I work on?",
      group: "Actions",
      icon: Compass,
      hint: "Next up, ranked",
      keywords: "next up priority todo focus rank suggest what now",
      run: () => open("overview"),
    },
  ]
}

/**
 * Home: what needs your move, the ranked Next up list, and a few tiles. It
 * took over Next up, so /nextup (and /nextup/start/<id>, the confirm a Start
 * button opens) still land here.
 */
export const overview: ModuleDef = {
  id: "overview",
  title: "Home",
  icon: House,
  route: "/",
  aliases: ["/nextup"],
  group: "workspace",
  kind: "core",
  order: 0,
  defaultEnabled: true,
  description: "What needs your move, what to work on next and why, and a few tiles.",
  keywords: "home overview dashboard cockpit next up priority focus todo rank triage what should i work on",
  component: lazy(() => import("@/pages/Overview")),
  useCommands: useHomeCommands,
}
