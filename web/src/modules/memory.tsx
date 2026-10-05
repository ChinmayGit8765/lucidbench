import { lazy, useEffect, useState } from "react"
import { FilePlus2, FileText, NotebookPen, Search } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { useApp } from "@/lib/app"
import { allPages, baseName, dirOf, pageRoute, requestMemory, type Entry } from "@/lib/memory"
import type { ModuleDef } from "@/modules/types"

/** "New page…", "Search memory…", and "Open page …" for every page once the user types. */
function useMemoryCommands(paletteOpen: boolean): Command[] {
  const { open, navigate } = useApp()
  const [pages, setPages] = useState<Entry[]>([])
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    allPages(300)
      .then((p) => !cancelled && setPages(p))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  const go = (intent: Parameters<typeof requestMemory>[0]) => {
    requestMemory(intent)
    open("memory")
  }
  return [
    { id: "memory-new", label: "New page…", group: "Actions", icon: FilePlus2, keywords: "memory note create write", run: () => go({ kind: "new-page" }) },
    { id: "memory-search", label: "Search memory…", group: "Actions", icon: Search, keywords: "memory notes find vault", run: () => go({ kind: "search" }) },
    ...pages.map((p) => ({
      id: `memory-page-${p.path}`,
      label: `${p.icon ? `${p.icon} ` : ""}${p.title || baseName(p.path)}`,
      group: "Memory",
      icon: FileText,
      hint: dirOf(p.path) || "Memory",
      keywords: p.path,
      searchOnly: true,
      run: () => navigate(pageRoute(p.path)),
    })),
  ]
}

export const memory: ModuleDef = {
  id: "memory",
  title: "Memory",
  icon: NotebookPen,
  route: "/memory",
  section: "workspace",
  kind: "core",
  order: 3,
  defaultEnabled: true,
  description: "A notes workspace over a Markdown vault that other note apps can open too.",
  keywords: "notes vault markdown wiki docs obsidian pages",
  component: lazy(() => import("@/pages/Memory")),
  useCommands: useMemoryCommands,
}
