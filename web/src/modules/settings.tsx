import { lazy } from "react"
import { Blocks, Image, Minus, Palette, Plus, Settings as SettingsIcon, SunMoon, WandSparkles } from "lucide-react"
import { toast } from "sonner"

import type { Command } from "@/components/CommandPalette"
import { useApp } from "@/lib/app"
import { usePrefs } from "@/lib/prefs"
import { DARK_DEFAULT, hasArt, LIGHT_DEFAULT } from "@/lib/theme"
import { MODULES } from "@/modules"
import { isAdded, sorted } from "@/modules/registry"
import type { ModuleDef } from "@/modules/types"

function useSettingsCommands(): Command[] {
  const { open } = useApp()
  const { prefs, update, themes, active, base } = usePrefs()
  const extensions = sorted(
    MODULES.filter((m) => m.kind === "extension" && m.status !== "soon"),
    prefs,
  )
  const setAdded = (m: ModuleDef, added: boolean) => {
    update((p) => ({ ...p, extensions: { ...p.extensions, [m.id]: { added, order: p.extensions[m.id]?.order ?? m.order } } }))
    toast.success(added ? `${m.title} added to the sidebar` : `${m.title} removed from the sidebar`)
  }
  const out: Command[] = [
    {
      id: "theme-pick",
      label: "Change theme…",
      group: "Appearance",
      icon: Palette,
      hint: active?.name,
      keywords: "theme appearance skin colours preset",
      children: themes.map((t) => ({
        id: `theme-${t.id}`,
        label: t.name,
        group: t.builtin ? "Presets" : "Your themes",
        icon: Palette,
        hint: t.id === active?.id ? "current" : t.base,
        keywords: `${t.id} ${t.description ?? ""}`,
        run: () => update((p) => ({ ...p, theme: t.id })),
      })),
    },
    {
      id: "theme-toggle",
      label: base === "dark" ? "Switch to the light theme" : "Switch to the dark theme",
      group: "Appearance",
      icon: SunMoon,
      keywords: "toggle theme dark light appearance",
      run: () => update((p) => ({ ...p, theme: base === "dark" ? LIGHT_DEFAULT : DARK_DEFAULT })),
    },
    {
      id: "theme-describe",
      label: "Describe a theme…",
      group: "Appearance",
      icon: WandSparkles,
      keywords: "generate prompt ai theme claude codex grok",
      run: () => open("settings", ["appearance", "describe"]),
    },
    {
      id: "ext-add",
      label: "Add extension…",
      group: "Extensions",
      icon: Plus,
      keywords: "extension sidebar install enable",
      children: extensions
        .filter((m) => !isAdded(m, prefs))
        .map((m) => ({ id: `ext-add-${m.id}`, label: m.title, group: "Add to the sidebar", icon: m.icon, hint: m.category, run: () => setAdded(m, true) })),
    },
    {
      id: "ext-remove",
      label: "Remove extension…",
      group: "Extensions",
      icon: Minus,
      keywords: "extension sidebar uninstall disable",
      children: extensions
        .filter((m) => isAdded(m, prefs))
        .map((m) => ({ id: `ext-rm-${m.id}`, label: m.title, group: "Remove from the sidebar", icon: m.icon, hint: m.category, run: () => setAdded(m, false) })),
    },
    {
      id: "ext-gallery",
      label: "Browse extensions",
      group: "Extensions",
      icon: Blocks,
      keywords: "extension gallery",
      run: () => open("settings", ["extensions"]),
    },
  ]
  if (hasArt(active) && active?.art?.spriteBoard?.length) {
    out.push({
      id: "sprite-board",
      label: prefs.sprite_board ? "Hide the sprite board" : "Show the sprite board",
      group: "Appearance",
      icon: Image,
      keywords: "sprites art overview",
      run: () => update((p) => ({ ...p, sprite_board: !p.sprite_board })),
    })
  }
  return out
}

export const settings: ModuleDef = {
  id: "settings",
  title: "Settings",
  icon: SettingsIcon,
  route: "/settings",
  section: "settings",
  kind: "core",
  order: 0,
  defaultEnabled: true,
  description: "Appearance, sidebar, extensions and engine details.",
  keywords: "preferences appearance theme extensions sidebar",
  component: lazy(() => import("@/pages/Settings")),
  useCommands: useSettingsCommands,
}
