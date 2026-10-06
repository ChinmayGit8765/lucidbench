import { Blocks, Info, Palette, PanelLeft, Power, Settings as SettingsIcon } from "lucide-react"

import { PageHeader } from "@/components/Shell"
import { Tabs } from "@/components/ui/tabs"
import { useApp } from "@/lib/app"
import type { ModulePageProps } from "@/modules/types"
import { Appearance } from "@/pages/settings/Appearance"
import { Extensions } from "@/pages/settings/Extensions"
import { General } from "@/pages/settings/General"
import { Infrastructure } from "@/pages/settings/Infrastructure"
import { SidebarLayout } from "@/pages/settings/SidebarLayout"

const TABS = [
  { id: "appearance", label: "Appearance", icon: Palette },
  { id: "sidebar", label: "Sidebar", icon: PanelLeft },
  { id: "extensions", label: "Extensions", icon: Blocks },
  { id: "infrastructure", label: "Infrastructure", icon: Power },
  { id: "general", label: "General", icon: Info },
]

export default function Settings({ subpath }: ModulePageProps) {
  const { navigate } = useApp()
  const tab = TABS.some((t) => t.id === subpath[0]) ? subpath[0] : "appearance"
  return (
    <div className="space-y-6">
      <PageHeader
        icon={<SettingsIcon />}
        title="Settings"
        description="How Lucidbench looks, what is in your sidebar, and where the engine keeps its files. Changes apply at once and are saved to ui.json in your data folder."
      />
      <Tabs label="Settings" items={TABS} value={tab} onChange={(t) => navigate(`/settings/${t}`)} />
      {tab === "appearance" && <Appearance focus={subpath[1]} />}
      {tab === "sidebar" && <SidebarLayout />}
      {tab === "extensions" && <Extensions focus={subpath[1]} />}
      {tab === "infrastructure" && <Infrastructure />}
      {tab === "general" && <General focus={subpath[1]} />}
    </div>
  )
}
