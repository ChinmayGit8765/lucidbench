import { SquareTerminal } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** On the roadmap: shown as "soon" in the sidebar, not openable yet. */
export const work: ModuleDef = {
  id: "work",
  title: "Work",
  icon: SquareTerminal,
  route: "/work",
  section: "workspace",
  kind: "core",
  order: 1,
  defaultEnabled: true,
  status: "soon",
  description: "Prompt Claude Code, Codex or Grok in a chat, with an environment per task.",
  keywords: "chat agent prompt task environment claude codex grok",
}
