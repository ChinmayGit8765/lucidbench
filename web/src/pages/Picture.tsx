import { Suspense, lazy, useEffect, useMemo, useRef, useState } from "react"
import { Download, FileText, FolderGit2, Layers, PenTool, Plus, Save, Sparkles, Wand2 } from "lucide-react"
import { toast } from "sonner"

import type { CanvasHandle } from "@/components/picture/ExcalidrawCanvas"
import { MermaidPreview } from "@/components/picture/MermaidPreview"
import { PageHeader } from "@/components/Shell"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Dialog } from "@/components/ui/dialog"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { pictureApi, slugName, type PictureItem, type PictureKind, type PictureListing, type PictureSummary } from "@/lib/picture"
import { cn } from "@/lib/utils"
import type { ModulePageProps } from "@/modules/types"

// Excalidraw is large: it loads only when a canvas is opened.
const ExcalidrawCanvas = lazy(() => import("@/components/picture/ExcalidrawCanvas"))

const field =
  "h-8 w-full rounded-md border bg-background/60 px-2.5 text-sm outline-none transition-colors placeholder:text-subtle-foreground hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"

const BLANK_MERMAID = "flowchart LR\n  a[Client] --> b[Server]\n  b --> c[(Database)]\n"
const BLANK_CANVAS = JSON.stringify({ type: "excalidraw", version: 2, source: "lucidbench", elements: [], appState: {}, files: {} })

/** The picture open in the editor. `exists` means it has been saved at least once. */
interface Doc {
  key: number
  name: string
  kind: PictureKind
  text: string
  exists: boolean
  dirty: boolean
  /** Where the text came from, when it did not come from a saved file. */
  note?: string
}

let docKey = 0
const newDoc = (d: Omit<Doc, "key">): Doc => ({ key: ++docKey, ...d })

const DRAFT_CHOICES = [
  { id: "auto", label: "Cheapest signed-in (Claude Haiku first)", provider: "", model: "" },
  { id: "claude-haiku", label: "Claude · Haiku", provider: "claude", model: "haiku" },
  { id: "claude-sonnet", label: "Claude · Sonnet", provider: "claude", model: "sonnet" },
  { id: "codex", label: "Codex · default model", provider: "codex", model: "" },
  { id: "grok", label: "Grok · default model", provider: "grok", model: "" },
]

function download(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = name
  a.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

function KindIcon({ kind }: { kind: PictureKind }) {
  return kind === "mermaid" ? <Layers className="size-3.5 shrink-0" /> : <PenTool className="size-3.5 shrink-0" />
}

export default function Picture({ subpath }: ModulePageProps) {
  const { open } = useApp()
  const summary = usePoll<PictureSummary[]>("/api/picture", 30000)
  const projects = summary.data ?? []
  const projectId = projects.find((p) => p.project === subpath[0])?.project ?? projects[0]?.project ?? null
  const project = projects.find((p) => p.project === projectId) ?? null
  const listPoll = usePoll<PictureListing>(projectId ? `/api/picture/${encodeURIComponent(projectId)}` : null, 30000)
  const listing = listPoll.data && listPoll.data.project.project === projectId ? listPoll.data : null

  const [doc, setDoc] = useState<Doc | null>(null)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [drafting, setDrafting] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)
  const canvas = useRef<CanvasHandle>(null)

  // Another project is another set of pictures.
  useEffect(() => {
    setDoc(null)
  }, [projectId])

  const guard = (go: () => void) => {
    if (doc?.dirty)
      setConfirm({ title: "Discard unsaved changes?", description: `${doc.name || "This picture"} has changes that are not saved.`, confirmLabel: "Discard", danger: true, run: async () => go() })
    else go()
  }

  const openItem = (it: PictureItem) =>
    guard(() => {
      if (!projectId) return
      setBusy(it.name)
      pictureApi
        .read(projectId, it.name, it.kind)
        .then((r) => setDoc(newDoc({ name: it.name, kind: it.kind, text: r.content, exists: true, dirty: false })))
        .catch((e) => toast.error(errorMessage(e)))
        .finally(() => setBusy(null))
    })

  const fromLive = () =>
    guard(() => {
      if (!projectId) return
      setBusy("live")
      pictureApi
        .live(projectId)
        .then((r) =>
          setDoc(
            newDoc({
              name: "live-state",
              kind: "mermaid",
              text: r.mermaid,
              exists: false,
              dirty: true,
              note: r.notes.length > 0 ? `Built from live state. ${r.notes.join("; ")}.` : "Built from live state: deploys, containers, databases, runners and builds_into links. Not saved yet.",
            }),
          ),
        )
        .catch((e) => toast.error(errorMessage(e)))
        .finally(() => setBusy(null))
    })

  const setText = (text: string) => setDoc((d) => (d ? { ...d, text, dirty: true } : d))

  const save = async () => {
    if (!doc || !projectId || !listing) return
    const name = slugName(doc.name)
    if (!name) {
      toast.error("Give it a name first (a-z, 0-9, - and _)")
      return
    }
    const text = doc.kind === "excalidraw" ? (canvas.current?.serialize() ?? doc.text) : doc.text
    let target: { where: string; path: string }
    try {
      target = await pictureApi.target(projectId, name, doc.kind)
    } catch (e) {
      toast.error(errorMessage(e))
      return
    }
    const overwrites = listing.items.some((i) => i.name === name && i.kind === doc.kind)
    setConfirm({
      title: overwrites ? "Replace this file?" : "Save this picture?",
      description:
        target.where === "repo" ? "It is written into the project's repository, where git will see it:" : "This project has no local folder, so it is saved as a Memory page:",
      body: <div className="break-all rounded-md border bg-muted/50 p-2.5 font-mono text-xs">{target.path}</div>,
      confirmLabel: overwrites ? "Replace" : "Save",
      danger: overwrites,
      run: async () => {
        try {
          await pictureApi.write(projectId, name, doc.kind, text)
          toast.success(`Saved ${name}`)
          setDoc((d) => (d && d.key === doc.key ? { ...d, name, text: d.kind === "excalidraw" ? d.text : text, exists: true, dirty: false, note: undefined } : d))
          listPoll.refresh()
          summary.refresh()
        } catch (e) {
          toast.error(errorMessage(e))
        }
      },
    })
  }

  const exportPNG = async () => {
    if (!doc) return
    try {
      const blob = await canvas.current?.png()
      if (blob) download(blob, `${slugName(doc.name) || "canvas"}.png`)
    } catch (e) {
      toast.error(errorMessage(e))
    }
  }

  const items = listing?.items ?? []
  const empty = useMemo(() => projects.length === 0 && !summary.loading, [projects.length, summary.loading])

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<PenTool />}
        title="Picture"
        description="Back-end schematics in Mermaid and front-end design canvases in Excalidraw, kept next to the code."
        actions={
          project && (
            <select
              aria-label="Project"
              className={cn(field, "w-56")}
              value={project.project}
              onChange={(e) => guard(() => open("picture", [e.target.value]))}
            >
              {projects.map((p) => (
                <option key={p.project} value={p.project}>
                  {p.name || p.project}
                  {p.count > 0 ? ` (${p.count})` : ""}
                </option>
              ))}
            </select>
          )
        }
      />

      {summary.error && !summary.data && <ErrorState title="Cannot load projects" message={summary.error.message} onRetry={summary.refresh} />}
      {summary.loading && !summary.data && <Skeleton className="h-64 w-full" />}
      {empty && !summary.error && (
        <EmptyState icon={<FolderGit2 />} title="No projects yet" description="Pictures belong to a project. Add one to projects.yaml, then come back." />
      )}

      {project && (
        <div className="grid gap-4 @3xl:grid-cols-[17rem_minmax(0,1fr)]">
          <Card className="space-y-3 self-start p-3">
            <div className="flex items-center gap-2 px-1">
              <span className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Pictures</span>
              <Badge className="ml-auto" title={listing?.where === "repo" ? "Saved in the repository" : "Saved in Memory"}>
                {listing?.where === "repo" ? <FolderGit2 /> : <FileText />} {listing?.where === "repo" ? "docs/picture" : "Memory"}
              </Badge>
            </div>
            {listPoll.loading && !listing && <Skeleton className="h-16 w-full" />}
            {listing && items.length === 0 && <p className="px-1 text-sm text-muted-foreground">Nothing saved for this project yet.</p>}
            <ul className="space-y-0.5">
              {items.map((it) => (
                <li key={`${it.kind}/${it.name}`}>
                  <button
                    onClick={() => openItem(it)}
                    disabled={busy === it.name}
                    className={cn(
                      "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent",
                      doc?.exists && doc.name === it.name && doc.kind === it.kind && "bg-accent text-foreground",
                    )}
                  >
                    <KindIcon kind={it.kind} />
                    <span className="min-w-0 flex-1 truncate font-mono text-xs">{it.name}</span>
                    <span className="text-2xs text-subtle-foreground">{it.kind === "mermaid" ? ".mmd" : ".excalidraw"}</span>
                  </button>
                </li>
              ))}
            </ul>
            <div className="space-y-1.5 border-t pt-3">
              <Button variant="secondary" size="sm" className="w-full justify-start" onClick={() => guard(() => setDoc(newDoc({ name: "", kind: "mermaid", text: BLANK_MERMAID, exists: false, dirty: false })))}>
                <Plus /> New diagram
              </Button>
              <Button variant="secondary" size="sm" className="w-full justify-start" onClick={() => guard(() => setDoc(newDoc({ name: "", kind: "excalidraw", text: BLANK_CANVAS, exists: false, dirty: false })))}>
                <PenTool /> New canvas
              </Button>
              <Button variant="secondary" size="sm" className="w-full justify-start" onClick={fromLive} disabled={busy === "live"}>
                <Wand2 /> New from live state
              </Button>
              <Button
                variant="secondary"
                size="sm"
                className="w-full justify-start"
                onClick={() => setDrafting(true)}
                disabled={!listing?.can_draft}
                title={listing?.can_draft === false ? listing.draft_why : "Ask your AI CLI to sketch the architecture from the file tree and README"}
              >
                <Sparkles /> Draft from code
              </Button>
              {listing?.can_draft === false && <p className="px-1 text-2xs text-subtle-foreground">{listing.draft_why}</p>}
            </div>
          </Card>

          <div className="min-w-0 space-y-3">
            {!doc && (
              <EmptyState
                icon={<PenTool />}
                title="Pick a picture or start one"
                description="A diagram is Mermaid text with a live preview. A canvas is an Excalidraw whiteboard for front-end layouts."
              />
            )}
            {doc && (
              <>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-subtle-foreground">
                    <KindIcon kind={doc.kind} />
                  </span>
                  <input
                    aria-label="Name"
                    className={cn(field, "w-56 font-mono")}
                    placeholder="name (a-z, 0-9, - and _)"
                    value={doc.name}
                    disabled={doc.exists}
                    onChange={(e) => setDoc({ ...doc, name: e.target.value, dirty: true })}
                  />
                  <span className="font-mono text-xs text-subtle-foreground">{doc.kind === "mermaid" ? ".mmd" : ".excalidraw"}</span>
                  {doc.dirty && <StatusPill tone="warning">unsaved</StatusPill>}
                  <div className="ml-auto flex items-center gap-2">
                    {doc.kind === "excalidraw" && (
                      <Button variant="secondary" size="sm" onClick={() => void exportPNG()}>
                        <Download /> Export PNG
                      </Button>
                    )}
                    <Button size="sm" onClick={() => void save()}>
                      <Save /> Save
                    </Button>
                  </div>
                </div>
                {doc.note && <p className="rounded-md border border-brand/30 bg-brand-soft px-3 py-2 text-xs text-foreground">{doc.note}</p>}
                {doc.kind === "mermaid" ? (
                  <div className="grid gap-3 @3xl:grid-cols-2">
                    <textarea
                      aria-label="Mermaid source"
                      spellCheck={false}
                      className={cn(field, "h-[34rem] resize-none py-2 font-mono text-xs leading-relaxed")}
                      value={doc.text}
                      onChange={(e) => setText(e.target.value)}
                    />
                    <Card className="h-[34rem] overflow-auto p-4">
                      <MermaidPreview source={doc.text} className="[&_svg]:mx-auto [&_svg]:h-auto [&_svg]:max-w-full" />
                    </Card>
                  </div>
                ) : (
                  <Suspense fallback={<Skeleton className="h-[34rem] w-full" />}>
                    <ExcalidrawCanvas key={doc.key} json={doc.text} handle={canvas} onDirty={() => setDoc((d) => (d && !d.dirty ? { ...d, dirty: true } : d))} />
                  </Suspense>
                )}
              </>
            )}
          </div>
        </div>
      )}

      <DraftDialog
        open={drafting}
        projectName={project?.name || project?.project || ""}
        onClose={() => setDrafting(false)}
        onDraft={async (choice) => {
          if (!projectId) return
          const r = await pictureApi.draft(projectId, choice.provider, choice.model)
          const cost = r.usage.cost_usd ? ` · cost $${r.usage.cost_usd.toFixed(4)}` : ""
          setDrafting(false)
          setDoc(
            newDoc({
              name: "draft",
              kind: "mermaid",
              text: r.mermaid,
              exists: false,
              dirty: true,
              note: `AI draft from ${r.provider}${r.model ? ` (${r.model})` : ""}${cost}. A preview only: check it, edit it and save it if it is right.`,
            }),
          )
        }}
      />
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

function DraftDialog({
  open,
  projectName,
  onClose,
  onDraft,
}: {
  open: boolean
  projectName: string
  onClose: () => void
  onDraft: (c: (typeof DRAFT_CHOICES)[number]) => Promise<void>
}) {
  const [id, setId] = useState("auto")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const choice = DRAFT_CHOICES.find((c) => c.id === id) ?? DRAFT_CHOICES[0]
  return (
    <Dialog open={open} onClose={busy ? () => {} : onClose} title="Draft from code" description={`Sketch ${projectName}'s architecture with your own AI CLI.`}>
      <div className="space-y-3">
        <p className="text-sm text-muted-foreground">
          It sees only a listing of the repository's file names (three levels deep, without .git, node_modules, dist or credential-looking files) and the first 200 lines of the README, with no tools. The result is a preview and is never saved on its own.
        </p>
        <label className="block space-y-1">
          <span className="text-xs font-medium text-muted-foreground">Provider and model</span>
          <select className={field} value={id} onChange={(e) => setId(e.target.value)} disabled={busy}>
            {DRAFT_CHOICES.map((c) => (
              <option key={c.id} value={c.id}>
                {c.label}
              </option>
            ))}
          </select>
        </label>
        {error && (
          <p role="alert" className="rounded-md border border-danger/30 bg-danger-soft p-2.5 text-xs text-danger-fg">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button
            disabled={busy}
            onClick={() => {
              setBusy(true)
              setError(null)
              onDraft(choice)
                .catch((e) => setError(errorMessage(e)))
                .finally(() => setBusy(false))
            }}
          >
            <Sparkles /> {busy ? "Drafting" : "Draft it"}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
