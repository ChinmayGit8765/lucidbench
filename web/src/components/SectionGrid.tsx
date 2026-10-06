import { useState } from "react"
import { LayoutDashboard, Plus, WandSparkles } from "lucide-react"
import { toast } from "sonner"

import { SectionRenderer } from "@/components/SectionRenderer"
import { Button } from "@/components/ui/button"
import { Dialog } from "@/components/ui/dialog"
import { errorMessage, refreshAll, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { addTemplate, CATALOG_PATH, removeSection, saveSection, SECTIONS_PATH, type Catalog, type Listing, type Placement, type Section } from "@/lib/sections"
import { cn } from "@/lib/utils"

/** Picks a built-in template, or goes to "Describe a section". */
function AddSectionDialog({ open, onClose, placement, onAdded }: { open: boolean; onClose: () => void; placement: Placement; onAdded: () => void }) {
  const { open: go } = useApp()
  const catalog = usePoll<Catalog>(open ? CATALOG_PATH : null, 600_000)
  const [busy, setBusy] = useState<string | null>(null)
  const templates = (catalog.data?.templates ?? []).filter((t) => (t.placement ?? "overview") === placement)
  const add = async (t: Section) => {
    setBusy(t.id)
    try {
      const s = await addTemplate(t.id)
      toast.success(`${s.title} added`)
      onAdded()
      onClose()
    } catch (e) {
      toast.error("Could not add the section", { description: errorMessage(e) })
    } finally {
      setBusy(null)
    }
  }
  return (
    <Dialog open={open} onClose={onClose} title="Add a section" description="A ready-made section, or describe your own and preview it with live data first.">
      <ul className="space-y-1.5">
        {templates.map((t) => (
          <li key={t.id}>
            <button
              onClick={() => void add(t)}
              disabled={busy !== null}
              className="flex w-full items-start gap-3 rounded-lg border bg-background/40 px-3 py-2.5 text-left transition-colors hover:border-border-strong hover:bg-accent/40 disabled:opacity-60"
            >
              <LayoutDashboard className="mt-0.5 size-4 shrink-0 text-brand" />
              <span className="min-w-0">
                <span className="block text-sm font-medium">{t.title}</span>
                <span className="block text-xs text-muted-foreground">{t.description}</span>
              </span>
            </button>
          </li>
        ))}
      </ul>
      <div className="mt-4 flex justify-end">
        <Button
          variant="secondary"
          onClick={() => {
            onClose()
            go("settings", ["sections", "describe"])
          }}
        >
          <WandSparkles /> Describe a section…
        </Button>
      </div>
    </Dialog>
  )
}

/**
 * The user's sections for one place: the Overview, or a project's page
 * (projectId fills "{project}" in their filters). With none yet, it offers
 * to add one.
 */
export function SectionGrid({ placement, projectId, className }: { placement: Placement; projectId?: string; className?: string }) {
  const listing = usePoll<Listing>(SECTIONS_PATH, 30_000)
  const [adding, setAdding] = useState(false)
  const mine = (listing.data?.sections ?? []).filter((s) => (s.placement ?? "overview") === placement)
  const reload = () => {
    listing.refresh()
    refreshAll()
  }
  const remove = async (s: Section) => {
    try {
      await removeSection(s.id)
      toast.success(`${s.title} removed`, { action: { label: "Undo", onClick: () => void saveSection(s).then(reload) } })
      reload()
    } catch (e) {
      toast.error("Could not remove the section", { description: errorMessage(e) })
    }
  }
  if (!listing.data) return null
  return (
    <section className={cn("space-y-3", className)} aria-label="Sections">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold">{placement === "project" ? "Sections" : "Your sections"}</h2>
        <Button variant="ghost" size="sm" onClick={() => setAdding(true)}>
          <Plus /> Add section
        </Button>
      </div>
      {mine.length === 0 ? (
        <button
          onClick={() => setAdding(true)}
          className="flex w-full items-center justify-center gap-2 rounded-xl border border-dashed px-4 py-5 text-sm text-muted-foreground transition-colors hover:border-border-strong hover:text-foreground"
        >
          <LayoutDashboard className="size-4" />
          {placement === "project" ? "Add a section to every project's page" : "Add a section: PRs waiting, this week's spend, cards due soon…"}
        </button>
      ) : (
        <div className={cn("grid gap-3", placement === "overview" ? "@3xl:grid-cols-2" : "")}>
          {mine.map((s) => (
            <SectionRenderer key={s.id} section={s} projectId={projectId} onRemove={() => void remove(s)} />
          ))}
        </div>
      )}
      <AddSectionDialog open={adding} onClose={() => setAdding(false)} placement={placement} onAdded={reload} />
    </section>
  )
}
