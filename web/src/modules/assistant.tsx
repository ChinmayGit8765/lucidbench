import { lazy, useEffect, useState } from "react"
import { Bot as BotIcon, MessageCircle, Plus, WandSparkles } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { getJSON } from "@/lib/api"
import { useApp } from "@/lib/app"
import { BOTS_PATH, type Bot } from "@/lib/assistant"
import type { ModuleDef } from "@/modules/types"

/** "Ask Lucid…", "Parse a braindump…", "New bot…", and a chat with each bot once the user types. */
function useAssistantCommands(paletteOpen: boolean): Command[] {
  const { open } = useApp()
  const [bots, setBots] = useState<Bot[]>([])
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    getJSON<Bot[]>(BOTS_PATH)
      .then((l) => !cancelled && setBots(l))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  return [
    { id: "assistant-ask", label: "Ask Lucid…", group: "Actions", icon: MessageCircle, keywords: "assistant chat ask talk agent", run: () => open("assistant") },
    { id: "assistant-braindump", label: "Parse a braindump…", group: "Actions", icon: WandSparkles, keywords: "assistant braindump split items ideas", run: () => open("assistant", ["braindump"]) },
    { id: "assistant-new-bot", label: "New bot…", group: "Actions", icon: Plus, keywords: "assistant bot agent persona", run: () => open("assistant", ["bots"]) },
    ...bots.map((b) => ({
      id: `assistant-bot-${b.id}`,
      label: `Chat with ${b.name}`,
      group: "Bots",
      icon: BotIcon,
      hint: `${b.provider}${b.model ? ` · ${b.model}` : ""}`,
      keywords: `bot ${b.provider} ${b.source ?? ""}`,
      searchOnly: true,
      run: () => open("assistant", ["bot", b.id]),
    })),
  ]
}

export const assistant: ModuleDef = {
  id: "assistant",
  title: "Assistant",
  icon: MessageCircle,
  route: "/assistant",
  section: "ai",
  kind: "core",
  order: 1.5,
  defaultEnabled: true,
  description: "Talk to Claude, Codex, Grok or your own bots. Answers come with proposed actions you apply with a click; it also splits braindumps into items.",
  keywords: "assistant chat ask lucid bot agent persona braindump parse actions",
  component: lazy(() => import("@/pages/Assistant")),
  useCommands: useAssistantCommands,
}
