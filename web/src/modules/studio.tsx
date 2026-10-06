import { lazy, useEffect, useState } from "react"
import { Lock, ScrollText, SquareTerminal, Vote, WandSparkles } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { getJSON } from "@/lib/api"
import { useApp } from "@/lib/app"
import { TEMPLATES_PATH, type Template, type TemplateList } from "@/lib/prompts"
import type { ModuleDef } from "@/modules/types"

const iconOf = (t: Template) => (t.source === "council" ? Lock : t.target === "work" ? SquareTerminal : t.target === "council" ? Vote : ScrollText)

/** "New prompt from template…": a nested list of every template, read when the palette opens. */
function useStudioCommands(paletteOpen: boolean): Command[] {
  const { open } = useApp()
  const [templates, setTemplates] = useState<Template[]>([])
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    getJSON<TemplateList>(TEMPLATES_PATH)
      .then((l) => !cancelled && setTemplates(l.templates))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  return [
    {
      id: "studio-new",
      label: "New prompt from template…",
      group: "Actions",
      icon: WandSparkles,
      hint: "Prompt Studio",
      keywords: "prompt template builder critic scout researcher reviewer brief studio compose",
      children: templates.map((t) => ({
        id: `studio-${t.id}`,
        label: t.name,
        group: t.source === "council" ? "Council prompts (read-only)" : "Templates",
        icon: iconOf(t),
        hint: t.description,
        keywords: t.id,
        run: () => {
          localStorage.removeItem("lucidbench:studio-draft")
          open("studio", [t.id])
        },
      })),
    },
  ]
}

export const studio: ModuleDef = {
  id: "studio",
  title: "Prompt Studio",
  icon: WandSparkles,
  route: "/studio",
  section: "ai",
  kind: "core",
  order: 2.5,
  defaultEnabled: true,
  description: "Compose big prompts in sections, with context from projects and Memory, a live preview and lint, then send them to Work or the Council.",
  keywords: "prompt template sections builder critic scout researcher reviewer improve lint context",
  component: lazy(() => import("@/pages/Studio")),
  useCommands: useStudioCommands,
}
