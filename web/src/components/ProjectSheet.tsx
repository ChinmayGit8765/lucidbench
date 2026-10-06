import { LayoutDashboard, Users } from "lucide-react"

import { SectionGrid } from "@/components/SectionGrid"
import { TeamEditor } from "@/components/TeamEditor"
import { Sheet } from "@/components/ui/dialog"
import { Tabs } from "@/components/ui/tabs"
import type { Project } from "@/lib/projects"

export type ProjectTab = "team" | "sections"

const TABS = [
  { id: "team", label: "Team", icon: Users },
  { id: "sections", label: "Sections", icon: LayoutDashboard },
]

/** A project's detail: its AI team and the sections on its page. */
export function ProjectSheet({
  project,
  tab,
  onTab,
  onClose,
}: {
  project: Project | null
  tab: ProjectTab
  onTab: (t: ProjectTab) => void
  onClose: () => void
}) {
  return (
    <Sheet
      open={!!project}
      onClose={onClose}
      title={project?.name ?? ""}
      description={project ? <span className="font-mono">{project.id}</span> : undefined}
      className="max-w-5xl"
    >
      {project && (
        <>
          <Tabs label="Project" items={TABS} value={tab} onChange={(t) => onTab(t as ProjectTab)} className="px-4 pt-2" />
          {tab === "team" ? <TeamEditor key={project.id} project={project.id} /> : <SectionGrid placement="project" projectId={project.id} className="p-5" />}
        </>
      )}
    </Sheet>
  )
}
