import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  BookOpen,
  Check,
  ChevronDown,
  ChevronRight,
  ClipboardCopy,
  Code2,
  Copy,
  FileText,
  FolderTree,
  Gavel,
  Info,
  KanbanSquare,
  Loader2,
  Lock,
  Plus,
  RotateCcw,
  Save,
  ScrollText,
  Search,
  Send,
  ShieldAlert,
  Sparkles,
  SquareTerminal,
  Trash2,
  TriangleAlert,
  Vote,
  WandSparkles,
  X,
  type LucideIcon,
} from "lucide-react"
import { toast } from "sonner"

import { ProviderTile, providerInfo } from "@/components/ProviderMark"
import { PageHeader } from "@/components/Shell"
import { Badge, StatusPill, type Tone } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Dialog, Sheet } from "@/components/ui/dialog"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, getJSON, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { DEFAULT_BOARD, type Board } from "@/lib/boards"
import { memoryApi, type Hit } from "@/lib/memory"
import { PROJECTS_POLL_MS, type ProjectList } from "@/lib/projects"
import {
  contextPath,
  deleteSnippet,
  deleteTemplate,
  formatTokens,
  improvePrompt,
  putHandoff,
  refKey,
  renderPrompt,
  saveSnippet,
  saveTemplate,
  slugId,
  SNIPPETS_PATH,
  SOURCE_INFO,
  takeHandoff,
  TARGET_INFO,
  TEMPLATES_PATH,
  type Finding,
  type ImproveResult,
  type Ref,
  type Rendered,
  type Section,
  type SectionId,
  type SectionMeta,
  type Snippet,
  type Source,
  type Target,
  type Template,
  type TemplateList,
} from "@/lib/prompts"
import { cn, isMac, plural } from "@/lib/utils"
import type { ModulePageProps } from "@/modules/types"

const ORDER: SectionId[] = ["role", "context", "contract", "task", "constraints", "verify", "report"]
const DRAFT_KEY = "lucidbench:studio-draft"

/** What the editor holds: a template's sections, edited. */
interface Draft {
  template: string
  name: string
  sections: Record<SectionId, string>
  project: string
  target: Target
  context: Ref[]
}

function toDraft(t: Template, project = "", context: Ref[] = []): Draft {
  const sections = Object.fromEntries(ORDER.map((id) => [id, ""])) as Record<SectionId, string>
  for (const s of t.sections) sections[s.id] = s.body
  return { template: t.id, name: t.name, sections, project, target: t.target, context }
}

/** The sections in the template's own order first, then any others the user filled. */
function sectionsOf(d: Draft, t?: Template): Section[] {
  const order = [...(t?.sections.map((s) => s.id) ?? []), ...ORDER.filter((id) => !t?.sections.some((s) => s.id === id))]
  return order.map((id) => ({ id, body: d.sections[id] ?? "" }))
}

function loadDraft(): Draft | null {
  try {
    const raw = localStorage.getItem(DRAFT_KEY)
    return raw ? (JSON.parse(raw) as Draft) : null
  } catch {
    return null
  }
}

const SEVERITY: Record<Finding["severity"], { tone: Tone; icon: LucideIcon; label: string }> = {
  error: { tone: "danger", icon: ShieldAlert, label: "blocks send" },
  warning: { tone: "warning", icon: TriangleAlert, label: "warning" },
  info: { tone: "neutral", icon: Info, label: "note" },
}

const SOURCE_ICON: Record<Ref["kind"], LucideIcon> = {
  project: FolderTree,
  rules: Gavel,
  repo: Code2,
  decisions: Vote,
  page: FileText,
  card: KanbanSquare,
}

export default function Studio({ subpath }: ModulePageProps) {
  const { navigate, open } = useApp()
  const list = usePoll<TemplateList>(TEMPLATES_PATH, 60000)
  const snippets = usePoll<Snippet[]>(SNIPPETS_PATH, 60000)
  const projects = usePoll<ProjectList>("/api/projects", PROJECTS_POLL_MS)
  const templates = list.data?.templates ?? []
  const meta = useMemo(() => new Map((list.data?.sections ?? []).map((s) => [s.id, s])), [list.data])

  const [draft, setDraft] = useState<Draft | null>(null)
  const [mono, setMono] = useState(() => localStorage.getItem("lucidbench:studio-mono") === "1")
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [saving, setSaving] = useState(false)
  const [improving, setImproving] = useState(false)
  const current = templates.find((t) => t.id === draft?.template)

  // First load: a handoff from another page, the template in the URL, the
  // last draft, or the builder.
  const started = useRef(false)
  useEffect(() => {
    if (started.current || templates.length === 0) return
    started.current = true
    const h = takeHandoff("studio")
    const byId = (id?: string) => templates.find((t) => t.id === id)
    if (h) {
      const t = byId(h.template) ?? byId("builder")!
      // The palette's "New prompt from template" brings no text and no project: keep the last one.
      const d = toDraft(t, h.project ?? loadDraft()?.project ?? "")
      if (h.text.trim()) d.sections.task = h.text
      setDraft(d)
      return
    }
    const fromURL = byId(subpath[0])
    const saved = loadDraft()
    if (fromURL && fromURL.id !== saved?.template) {
      setDraft(toDraft(fromURL, saved?.project ?? ""))
    } else if (saved && byId(saved.template)) {
      setDraft(saved)
    } else {
      setDraft(toDraft(byId("builder") ?? templates[0]))
    }
  }, [templates, subpath])

  // A link or the palette asked for another template while the page is open.
  useEffect(() => {
    if (!started.current || !draft || !subpath[0] || subpath[0] === draft.template) return
    takeHandoff("studio") // the palette's request, already answered here
    const t = templates.find((x) => x.id === subpath[0])
    if (t) setDraft(toDraft(t, draft.project, draft.context))
  }, [subpath[0]]) // only when the URL changes, not on every edit

  // The URL follows the template; the draft survives leaving the page.
  useEffect(() => {
    if (!draft) return
    localStorage.setItem(DRAFT_KEY, JSON.stringify(draft))
    if (subpath[0] !== draft.template) navigate(`/studio/${encodeURIComponent(draft.template)}`)
  }, [draft]) // the URL is derived from the draft, not the other way round

  const pick = (t: Template) => {
    const dirty = draft && current && sectionsOf(draft, current).some((s) => s.body !== (current.sections.find((x) => x.id === s.id)?.body ?? ""))
    const go = () => setDraft(toDraft(t, draft?.project ?? "", draft?.context ?? []))
    if (dirty && t.id !== draft?.template) {
      setConfirm({
        title: `Switch to ${t.name}?`,
        description: "Your edits to this prompt are replaced by the template's sections. Save it first to keep them.",
        confirmLabel: "Switch",
        run: async () => go(),
      })
    } else go()
  }

  const setSection = (id: SectionId, body: string) => setDraft((d) => (d ? { ...d, sections: { ...d.sections, [id]: body } } : d))
  const update = (p: Partial<Draft>) => setDraft((d) => (d ? { ...d, ...p } : d))

  // Project sources follow the chosen project.
  const setProject = (project: string) =>
    setDraft((d) =>
      d
        ? {
            ...d,
            project,
            context: d.context
              .map((r) => (SOURCE_INFO[r.kind].needs === "project" ? { ...r, project } : r))
              .filter((r) => SOURCE_INFO[r.kind].needs !== "project" || project),
          }
        : d,
    )

  // Live render, debounced.
  const [rendered, setRendered] = useState<Rendered | null>(null)
  const [renderErr, setRenderErr] = useState<string | null>(null)
  const [rendering, setRendering] = useState(false)
  const sections = useMemo(() => (draft ? sectionsOf(draft, current) : []), [draft, current])
  useEffect(() => {
    if (!draft) return
    let alive = true
    setRendering(true)
    const id = setTimeout(() => {
      renderPrompt({ sections, context: draft.context, project: draft.project || undefined, target: draft.target })
        .then((r) => {
          if (!alive) return
          setRendered(r)
          setRenderErr(null)
        })
        .catch((e) => alive && setRenderErr(errorMessage(e)))
        .finally(() => alive && setRendering(false))
    }, 400)
    return () => {
      alive = false
      clearTimeout(id)
    }
  }, [sections, draft?.context, draft?.project, draft?.target]) // what the text depends on; the name does not change it

  const ps = projects.data?.projects ?? []
  const project = ps.find((p) => p.id === draft?.project)
  const readOnly = !!current?.read_only

  const send = () => {
    if (!draft || !rendered) return
    if (draft.target === "copy") {
      void navigator.clipboard.writeText(rendered.text).then(
        () => toast.success("Prompt copied", { description: `${rendered.chars.toLocaleString()} characters, ≈ ${formatTokens(rendered.tokens)} tokens` }),
        () => toast.error("Could not copy"),
      )
      return
    }
    if (rendered.blocked) return
    if (draft.target === "work") {
      if (!draft.project) {
        toast.error("Pick a project", { description: "Work runs the agent in a worktree of the project's checkout." })
        return
      }
      putHandoff({ to: "work", text: rendered.text, project: draft.project, from: "studio" })
      navigate(`/work/new/project/${encodeURIComponent(draft.project)}`)
      return
    }
    putHandoff({ to: "council", text: rendered.text, project: draft.project || undefined, from: "studio" })
    open("council")
  }

  if (list.error && !list.data) return <ErrorState title="Could not load the templates" message={list.error.message} onRetry={list.refresh} />

  return (
    <div className="space-y-5">
      <PageHeader
        icon={<WandSparkles />}
        title="Prompt Studio"
        description="Build the big prompts in sections, give them context from your projects and Memory, and see exactly what will be sent before you send it."
        actions={
          <>
            <Button variant="secondary" size="sm" disabled={!draft || readOnly} onClick={() => setImproving(true)}>
              <Sparkles /> Improve this prompt
            </Button>
            <Button size="sm" disabled={!draft} onClick={() => setSaving(true)}>
              <Save /> Save…
            </Button>
          </>
        }
      />

      {!draft ? (
        <div className="grid gap-4 @5xl:grid-cols-[13.5rem_minmax(0,1fr)_19rem]">
          <Skeleton className="h-96 rounded-xl" />
          <Skeleton className="h-96 rounded-xl" />
          <Skeleton className="h-96 rounded-xl" />
        </div>
      ) : (
        <>
          <div className="grid items-start gap-4 @5xl:grid-cols-[13.5rem_minmax(0,1fr)_19rem]">
            <TemplateList
              templates={templates}
              current={draft.template}
              onPick={pick}
              snippets={snippets.data ?? []}
              onSnippet={(s) => {
                const id = s.section ?? "task"
                setSection(id, [draft.sections[id]?.trimEnd(), s.body].filter(Boolean).join("\n"))
                setCollapsed((c) => ({ ...c, [id]: false }))
                toast.success(`Added “${s.name}” to ${meta.get(id)?.title ?? id}`)
              }}
              onDeleteTemplate={(t) =>
                setConfirm({
                  title: t.overrides ? `Reset ${t.name} to the built-in?` : `Delete ${t.name}?`,
                  description: t.overrides ? "Your version is removed and the built-in template comes back." : "The template is removed from your data folder.",
                  confirmLabel: t.overrides ? "Reset" : "Delete",
                  danger: true,
                  run: async () => {
                    try {
                      await deleteTemplate(t.id)
                      list.refresh()
                    } catch (e) {
                      toast.error("Could not delete", { description: errorMessage(e) })
                    }
                  },
                })
              }
              onDeleteSnippet={(s) =>
                void deleteSnippet(s.id).then(
                  () => snippets.refresh(),
                  (e) => toast.error("Could not delete", { description: errorMessage(e) }),
                )
              }
            />

            <Card className="min-w-0 overflow-hidden">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b bg-muted/30 px-4 py-2.5">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <h2 className="truncate text-sm font-semibold">{current?.name ?? draft.name}</h2>
                    {current && <SourceBadge t={current} />}
                  </div>
                  {current?.description && <p className="truncate text-xs text-muted-foreground">{current.description}</p>}
                </div>
                <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <input
                    type="checkbox"
                    checked={mono}
                    onChange={(e) => {
                      setMono(e.target.checked)
                      localStorage.setItem("lucidbench:studio-mono", e.target.checked ? "1" : "0")
                    }}
                    className="accent-[var(--brand)]"
                  />
                  Monospace
                </label>
                <Button
                  variant="ghost"
                  size="sm"
                  title="Collapse or expand every section"
                  onClick={() => {
                    const all = ORDER.every((id) => collapsed[id])
                    setCollapsed(Object.fromEntries(ORDER.map((id) => [id, !all])))
                  }}
                >
                  <ChevronDown /> {ORDER.every((id) => collapsed[id]) ? "Expand all" : "Collapse all"}
                </Button>
              </div>
              {readOnly && (
                <div className="flex items-start gap-2.5 border-b bg-info-soft/60 px-4 py-2.5 text-xs">
                  <Lock className="mt-0.5 size-3.5 shrink-0 text-info-fg" />
                  <span className="min-w-0 flex-1 text-muted-foreground">{current?.note} Duplicate it to experiment; the council keeps using the versioned prompt.</span>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      update({ template: "builder", name: `${current?.name} (copy)`, target: "copy" })
                      toast.message("Copied into an editable draft", { description: "Save it as your own template to keep it." })
                    }}
                  >
                    <Copy /> Duplicate
                  </Button>
                </div>
              )}
              <div>
                {sections.map((s) => {
                  const m = meta.get(s.id)
                  const inTemplate = !!current?.sections.some((x) => x.id === s.id)
                  // A section the template does not use starts folded until it has text.
                  const folded = collapsed[s.id] ?? (!inTemplate && !s.body.trim() && !(s.id === "context" && draft.context.length > 0))
                  return (
                    <SectionEditor
                      key={s.id}
                      meta={m}
                      id={s.id}
                      body={s.body}
                      mono={mono}
                      readOnly={readOnly}
                      inTemplate={inTemplate}
                      collapsed={folded}
                      onToggle={() => setCollapsed((c) => ({ ...c, [s.id]: !folded }))}
                      onChange={(v) => setSection(s.id, v)}
                      workOwned={draft.target === "work" && (s.id === "role" || s.id === "constraints")}
                      findings={(rendered?.lint ?? []).filter((f) => f.section === s.id && f.severity !== "info")}
                      sources={s.id === "context" ? draft.context.length : 0}
                    />
                  )
                })}
              </div>
            </Card>

            <ContextPanel
              projects={projects.data}
              project={draft.project}
              onProject={setProject}
              refs={draft.context}
              onRefs={(context) => update({ context })}
              variables={list.data?.variables ?? []}
              vars={project}
            />
          </div>

          <Preview
            draft={draft}
            rendered={rendered}
            rendering={rendering}
            error={renderErr}
            onTarget={(target) => update({ target })}
            onSend={send}
            projectName={project?.name}
          />
        </>
      )}

      {draft && (
        <SaveDialog
          open={saving}
          onClose={() => setSaving(false)}
          draft={draft}
          current={current}
          sections={sections}
          meta={meta}
          onSaved={(t) => {
            list.refresh()
            snippets.refresh()
            if (t) update({ template: t.id, name: t.name })
          }}
        />
      )}
      {draft && (
        <ImproveSheet
          open={improving}
          onClose={() => setImproving(false)}
          sections={sections}
          project={draft.project}
          meta={meta}
          onApply={(secs) => {
            const next = { ...draft.sections }
            for (const id of ORDER) next[id] = secs.find((s) => s.id === id)?.body ?? (id === "context" ? next.context : "")
            update({ sections: next })
            setImproving(false)
            toast.success("Applied the improved sections")
          }}
        />
      )}
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

function SourceBadge({ t }: { t: Template }) {
  if (t.source === "council")
    return (
      <Badge>
        <Lock /> council · read-only
      </Badge>
    )
  if (t.overrides) return <Badge className="border-brand/30 text-brand-fg">your version</Badge>
  if (t.source === "user") return <Badge className="border-brand/30 text-brand-fg">yours</Badge>
  return <Badge>built-in v{t.version}</Badge>
}

/* ---------- left: templates and snippets ---------- */

function TemplateList({
  templates,
  current,
  onPick,
  snippets,
  onSnippet,
  onDeleteTemplate,
  onDeleteSnippet,
}: {
  templates: Template[]
  current: string
  onPick: (t: Template) => void
  snippets: Snippet[]
  onSnippet: (s: Snippet) => void
  onDeleteTemplate: (t: Template) => void
  onDeleteSnippet: (s: Snippet) => void
}) {
  const groups: { label: string; items: Template[] }[] = [
    { label: "Templates", items: templates.filter((t) => t.source === "builtin" || t.overrides) },
    { label: "Yours", items: templates.filter((t) => t.source === "user" && !t.overrides) },
    { label: "Council prompts", items: templates.filter((t) => t.source === "council") },
  ]
  return (
    <Card className="overflow-hidden @5xl:sticky @5xl:top-4">
      <nav aria-label="Templates" className="max-h-[calc(100vh-12rem)] overflow-y-auto p-1.5">
        {groups
          .filter((g) => g.items.length > 0)
          .map((g) => (
            <div key={g.label} className="mb-1.5">
              <div className="px-2.5 pb-1 pt-2 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{g.label}</div>
              {g.items.map((t) => {
                const on = t.id === current
                return (
                  <div key={t.id} className="group relative">
                    <button
                      onClick={() => onPick(t)}
                      aria-current={on ? "true" : undefined}
                      className={cn(
                        "flex w-full items-start gap-2 rounded-md px-2.5 py-1.5 text-left outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring",
                        on ? "bg-brand-soft text-foreground" : "hover:bg-accent/60",
                      )}
                    >
                      <span className={cn("mt-0.5 shrink-0", on ? "text-brand" : "text-subtle-foreground")}>
                        {t.source === "council" ? <Lock className="size-3.5" /> : t.target === "work" ? <SquareTerminal className="size-3.5" /> : t.target === "council" ? <Vote className="size-3.5" /> : <ScrollText className="size-3.5" />}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">{t.name}</span>
                        <span className="block truncate text-2xs text-subtle-foreground">
                          {t.overrides ? "edited · " : ""}
                          {t.source === "council" ? "the council uses it" : t.target === "work" ? "for Work" : t.target === "council" ? "for the Council" : "to copy and paste"}
                        </span>
                      </span>
                    </button>
                    {t.source === "user" && (
                      <button
                        onClick={() => onDeleteTemplate(t)}
                        aria-label={t.overrides ? `Reset ${t.name}` : `Delete ${t.name}`}
                        title={t.overrides ? "Reset to the built-in" : "Delete"}
                        className="absolute right-1.5 top-1.5 hidden rounded p-1 text-subtle-foreground hover:bg-accent hover:text-foreground group-hover:block"
                      >
                        {t.overrides ? <RotateCcw className="size-3" /> : <Trash2 className="size-3" />}
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
          ))}
        <div className="mt-1 border-t px-2.5 pb-1 pt-2.5">
          <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Snippets</div>
          {snippets.length === 0 ? (
            <p className="mt-1 text-2xs leading-4 text-subtle-foreground">Save a section as a snippet to reuse it in any prompt.</p>
          ) : (
            <ul className="mt-1 space-y-0.5">
              {snippets.map((s) => (
                <li key={s.id} className="group flex items-center gap-1">
                  <button
                    onClick={() => onSnippet(s)}
                    title={`Add to ${s.section ?? "task"}: ${s.body.slice(0, 120)}`}
                    className="flex min-w-0 flex-1 items-center gap-1.5 rounded px-1 py-1 text-left text-xs hover:bg-accent/60"
                  >
                    <Plus className="size-3 shrink-0 text-subtle-foreground" />
                    <span className="truncate">{s.name}</span>
                    <span className="ml-auto shrink-0 text-2xs text-subtle-foreground">{s.section ?? "task"}</span>
                  </button>
                  <button onClick={() => onDeleteSnippet(s)} aria-label={`Delete ${s.name}`} className="hidden rounded p-0.5 text-subtle-foreground hover:text-foreground group-hover:block">
                    <X className="size-3" />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </nav>
    </Card>
  )
}

/* ---------- centre: one section ---------- */

function SectionEditor({
  id,
  meta,
  body,
  mono,
  readOnly,
  inTemplate,
  collapsed,
  onToggle,
  onChange,
  workOwned,
  findings,
  sources,
}: {
  id: SectionId
  meta?: SectionMeta
  body: string
  mono: boolean
  readOnly: boolean
  inTemplate: boolean
  collapsed: boolean
  onToggle: () => void
  onChange: (v: string) => void
  workOwned: boolean
  findings: Finding[]
  sources: number
}) {
  const ref = useRef<HTMLTextAreaElement>(null)
  // Grow with the text, up to a point; the rest scrolls.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = "auto"
    // scrollHeight leaves out the border, which would show a scrollbar.
    el.style.height = `${Math.min(Math.max(el.scrollHeight + 2, 64), 520)}px`
  }, [body, collapsed, mono])
  const empty = body.trim() === ""
  if (readOnly && empty) return null
  const dormant = !inTemplate && empty
  const title = meta?.title ?? id
  return (
    <section className="border-b last:border-b-0">
      <button
        onClick={onToggle}
        aria-expanded={!collapsed}
        className="flex w-full items-center gap-2 px-4 py-2.5 text-left outline-none hover:bg-accent/30 focus-visible:bg-accent/40"
      >
        {collapsed ? <ChevronRight className="size-3.5 text-subtle-foreground" /> : <ChevronDown className="size-3.5 text-subtle-foreground" />}
        <span className={cn("text-sm font-semibold", empty && "text-muted-foreground")}>{title}</span>
        {workOwned && (
          <StatusPill tone="info" className="h-4">
            Work adds its own
          </StatusPill>
        )}
        {findings.map((f) => (
          <StatusPill key={f.code} tone={SEVERITY[f.severity].tone} className="h-4">
            {f.code === "empty-task" ? "empty" : f.code.replace(/^no-/, "no ").replace(/-/g, " ")}
          </StatusPill>
        ))}
        <span className="ml-auto truncate pl-3 text-2xs text-subtle-foreground">
          {sources > 0
            ? `${empty ? "" : `${body.length.toLocaleString()} chars + `}${plural(sources, "source")} from the right`
            : empty
              ? inTemplate
                ? "empty"
                : "not in this template"
              : `${body.length.toLocaleString()} chars`}
        </span>
      </button>
      {!collapsed && (
        <div className="px-4 pb-3.5">
          {meta?.hint && <p className="mb-1.5 text-2xs text-subtle-foreground">{meta.hint}</p>}
          <textarea
            ref={ref}
            aria-label={title}
            value={body}
            readOnly={readOnly}
            onChange={(e) => onChange(e.target.value)}
            placeholder={readOnly ? "" : dormant ? `Add a ${title.toLowerCase()} section…` : meta?.hint}
            spellCheck={!mono}
            className={cn(
              "block w-full resize-y rounded-lg border bg-background/50 px-3 py-2 text-sm leading-6 outline-none [font-variant-ligatures:none] placeholder:text-subtle-foreground/70 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30",
              mono && "font-mono text-[0.8125rem] leading-[1.35rem]",
              readOnly && "cursor-default bg-muted/30 text-muted-foreground",
              workOwned && "opacity-70",
            )}
          />
          {workOwned && (
            <p className="mt-1.5 text-2xs text-subtle-foreground">
              Sent to Work, this section is replaced by Work's own {id === "role" ? "role" : "safety rules (worktree only, commit, never push, the allowed commands)"}; lines you add here beyond those are kept.
            </p>
          )}
        </div>
      )}
    </section>
  )
}

/* ---------- right: context sources ---------- */

function ContextPanel({
  projects,
  project,
  onProject,
  refs,
  onRefs,
  variables,
  vars,
}: {
  projects: ProjectList | null
  project: string
  onProject: (id: string) => void
  refs: Ref[]
  onRefs: (r: Ref[]) => void
  variables: { Name: string; Hint: string }[]
  vars?: { name: string }
}) {
  const [info, setInfo] = useState<Record<string, Source | { error: string }>>({})
  const [pageQuery, setPageQuery] = useState("")
  const [hits, setHits] = useState<Hit[]>([])
  const [board, setBoard] = useState<Board | null>(null)
  const ps = projects?.projects ?? []
  const chosen = ps.find((p) => p.id === project)

  // Read each source once, to show its size and whether it is confidential.
  const keys = refs.map(refKey).join(",")
  useEffect(() => {
    for (const r of refs) {
      const k = refKey(r)
      if (info[k]) continue
      getJSON<Source>(contextPath(r))
        .then((s) => setInfo((m) => ({ ...m, [k]: s })))
        .catch((e) => setInfo((m) => ({ ...m, [k]: { error: errorMessage(e) } })))
    }
  }, [keys]) // each source is read once, when it is added

  useEffect(() => {
    getJSON<{ id: string }[]>("/api/boards")
      .then((bs) => (bs.some((b) => b.id === DEFAULT_BOARD) ? getJSON<Board>(`/api/boards/${DEFAULT_BOARD}`) : null))
      .then((b) => setBoard(b))
      .catch(() => setBoard(null))
  }, [])

  useEffect(() => {
    const q = pageQuery.trim()
    if (!q) {
      setHits([])
      return
    }
    const id = setTimeout(() => {
      memoryApi
        .search(q, 6)
        .then(setHits)
        .catch(() => setHits([]))
    }, 250)
    return () => clearTimeout(id)
  }, [pageQuery])

  const has = (r: Ref) => refs.some((x) => refKey(x) === refKey(r))
  const toggle = (r: Ref) => onRefs(has(r) ? refs.filter((x) => refKey(x) !== refKey(r)) : [...refs, r])
  const total = refs.reduce((n, r) => {
    const s = info[refKey(r)]
    return n + (s && "tokens" in s ? s.tokens : 0)
  }, 0)

  return (
    <div className="space-y-3 @5xl:sticky @5xl:top-4">
      <Card className="p-3.5">
        <label className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground" htmlFor="studio-project">
          Project
        </label>
        <div className="relative mt-1.5">
          <select
            id="studio-project"
            value={project}
            onChange={(e) => onProject(e.target.value)}
            className="h-8 w-full appearance-none rounded-md border bg-background/60 pl-2.5 pr-7 text-sm outline-none hover:border-border-strong focus-visible:border-ring"
          >
            <option value="">No project</option>
            {ps.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
                {p.visibility === "confidential" ? " (confidential)" : ""}
              </option>
            ))}
          </select>
          <ChevronDown className="pointer-events-none absolute right-2 top-2.5 size-3.5 text-subtle-foreground" />
        </div>
        {chosen?.visibility === "confidential" && (
          <p className="mt-1.5 flex items-center gap-1 text-2xs text-danger-fg">
            <Lock className="size-3" /> Confidential: only Copy is allowed.
          </p>
        )}
        <p className="mt-1.5 text-2xs leading-4 text-subtle-foreground">Fills the {"{{project.*}}"} variables and the project sources below.</p>
      </Card>

      <Card className="p-3.5">
        <div className="flex items-baseline justify-between">
          <h3 className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Context</h3>
          <span className="text-2xs tabular-nums text-subtle-foreground" title="Estimate: characters / 4">
            {refs.length > 0 && `≈ ${formatTokens(total)} tokens`}
          </span>
        </div>

        {refs.length > 0 && (
          <ul aria-label="Selected context" className="mt-2 flex flex-wrap gap-1.5">
            {refs.map((r) => {
              const s = info[refKey(r)]
              const Icon = SOURCE_ICON[r.kind]
              const err = s && "error" in s ? s.error : null
              const src = s && "label" in s ? s : null
              return (
                <li
                  key={refKey(r)}
                  title={err ?? src?.label}
                  className={cn(
                    "inline-flex h-6 max-w-full items-center gap-1 rounded-full border pl-2 pr-1 text-2xs",
                    src?.confidential ? "border-danger/40 bg-danger-soft text-danger-fg" : err ? "border-warning/40 bg-warning-soft text-warning-fg" : "bg-background/60 text-muted-foreground",
                  )}
                >
                  {src?.confidential ? <Lock className="size-3 shrink-0" /> : <Icon className="size-3 shrink-0" />}
                  <span className="truncate">{src ? src.label.replace(/^[^:]+: /, `${SOURCE_INFO[r.kind].label}: `) : SOURCE_INFO[r.kind].label}</span>
                  <span className="shrink-0 font-mono tabular-nums opacity-80">{src ? formatTokens(src.tokens) : err ? "!" : "…"}</span>
                  <button onClick={() => toggle(r)} aria-label="Remove" className="flex size-4 shrink-0 items-center justify-center rounded-full hover:bg-accent hover:text-foreground">
                    <X className="size-3" />
                  </button>
                </li>
              )
            })}
          </ul>
        )}

        <div className="mt-3 space-y-1">
          <div className="text-2xs text-subtle-foreground">{chosen ? `From ${chosen.name}` : "Pick a project for these"}</div>
          {(["project", "rules", "repo", "decisions"] as const).map((k) => {
            const r: Ref = { kind: k, project }
            const on = has(r)
            const Icon = SOURCE_ICON[k]
            return (
              <button
                key={k}
                disabled={!project}
                aria-pressed={on}
                onClick={() => toggle(r)}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md border px-2.5 py-1.5 text-left text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-50",
                  on ? "border-brand/50 bg-brand-soft" : "bg-background/40 hover:bg-accent/50",
                )}
              >
                <Icon className={cn("size-3.5 shrink-0", on ? "text-brand" : "text-subtle-foreground")} />
                <span className="flex-1">{SOURCE_INFO[k].label}</span>
                <span className="text-2xs text-subtle-foreground">
                  {k === "project" ? "entry + assessment" : k === "rules" ? "contracts, AGENTS…" : k === "repo" ? "tree, depth 2" : "recent briefs"}
                </span>
                {on && <Check className="size-3.5 text-brand" />}
              </button>
            )
          })}
        </div>

        <div className="mt-3">
          <div className="mb-1 text-2xs text-subtle-foreground">Memory page</div>
          <label className="flex h-8 items-center gap-2 rounded-md border bg-background/50 px-2.5 focus-within:border-ring">
            <Search className="size-3.5 text-subtle-foreground" />
            <input
              value={pageQuery}
              onChange={(e) => setPageQuery(e.target.value)}
              placeholder="Search pages"
              aria-label="Search Memory pages"
              className="min-w-0 flex-1 bg-transparent text-xs outline-none placeholder:text-subtle-foreground"
            />
          </label>
          {hits.length > 0 && (
            <ul className="mt-1 overflow-hidden rounded-md border">
              {hits.map((h) => {
                const r: Ref = { kind: "page", path: h.path }
                return (
                  <li key={h.path}>
                    <button
                      onClick={() => {
                        toggle(r)
                        setPageQuery("")
                      }}
                      className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-xs hover:bg-accent/50"
                    >
                      {h.confidential ? <Lock className="size-3 shrink-0 text-danger-fg" /> : <FileText className="size-3 shrink-0 text-subtle-foreground" />}
                      <span className="min-w-0 flex-1 truncate">{h.title}</span>
                      {has(r) && <Check className="size-3 text-brand" />}
                    </button>
                  </li>
                )
              })}
            </ul>
          )}
        </div>

        {board && board.cards.length > 0 && (
          <div className="mt-3">
            <div className="mb-1 text-2xs text-subtle-foreground">Card</div>
            <div className="relative">
              <select
                value=""
                onChange={(e) => e.target.value && toggle({ kind: "card", board: DEFAULT_BOARD, id: e.target.value })}
                aria-label="Add a card"
                className="h-8 w-full appearance-none rounded-md border bg-background/60 pl-2.5 pr-7 text-xs outline-none hover:border-border-strong focus-visible:border-ring"
              >
                <option value="">Add a card from the work board…</option>
                {board.cards.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.title} · {c.column}
                  </option>
                ))}
              </select>
              <ChevronDown className="pointer-events-none absolute right-2 top-2.5 size-3.5 text-subtle-foreground" />
            </div>
          </div>
        )}
      </Card>

      <details className="group rounded-xl border bg-card px-3.5 py-2.5 shadow-card">
        <summary className="flex cursor-pointer list-none items-center gap-1.5 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
          <BookOpen className="size-3" /> Variables
          <ChevronRight className="ml-auto size-3 transition-transform group-open:rotate-90" />
        </summary>
        <ul className="mt-2 space-y-1.5">
          {variables.map((v) => (
            <li key={v.Name} className="text-2xs leading-4">
              <code className="rounded bg-muted px-1 font-mono text-foreground">{`{{${v.Name}}}`}</code>
              <span className="ml-1 text-subtle-foreground">{v.Hint}</span>
            </li>
          ))}
        </ul>
        {!vars && <p className="mt-2 text-2xs text-subtle-foreground">Pick a project to fill the project variables.</p>}
      </details>
    </div>
  )
}

/* ---------- bottom: the live preview ---------- */

function Preview({
  draft,
  rendered,
  rendering,
  error,
  onTarget,
  onSend,
  projectName,
}: {
  draft: Draft
  rendered: Rendered | null
  rendering: boolean
  error: string | null
  onTarget: (t: Target) => void
  onSend: () => void
  projectName?: string
}) {
  const [full, setFull] = useState(false)
  const blocked = !!rendered?.blocked && draft.target !== "copy"
  const errors = (rendered?.lint ?? []).filter((f) => f.severity === "error")
  const sendLabel = draft.target === "work" ? "Send to Work" : draft.target === "council" ? "Send to Council" : "Copy prompt"
  const SendIcon = draft.target === "copy" ? ClipboardCopy : Send
  const why = blocked ? errors[0]?.message : draft.target === "work" && !draft.project ? "Pick a project: Work needs its checkout." : null
  const copy = useCallback(() => {
    if (!rendered) return
    void navigator.clipboard.writeText(rendered.text).then(() => toast.success("Prompt copied"))
  }, [rendered])

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-4 py-3">
        <div className="flex items-baseline gap-2">
          <h2 className="text-sm font-semibold">Preview</h2>
          {rendered && (
            <span className="text-xs tabular-nums text-muted-foreground" title={rendered.tokens_note}>
              ≈ {formatTokens(rendered.tokens)} tokens · {rendered.chars.toLocaleString()} chars
              <span className="ml-1 text-2xs text-subtle-foreground">(estimate: characters ÷ 4)</span>
            </span>
          )}
          {rendering && <Loader2 className="size-3.5 animate-spin text-subtle-foreground" />}
        </div>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <span className="text-2xs font-medium uppercase tracking-wider text-subtle-foreground">Send to</span>
          <div role="radiogroup" aria-label="Target" className="inline-flex h-8 rounded-md border bg-muted/50 p-0.5">
            {(["work", "council", "copy"] as Target[]).map((t) => (
              <button
                key={t}
                role="radio"
                aria-checked={draft.target === t}
                onClick={() => onTarget(t)}
                title={TARGET_INFO[t].blurb}
                className={cn(
                  "inline-flex items-center gap-1.5 rounded px-3 text-xs transition-colors",
                  draft.target === t ? "bg-elevated font-medium text-foreground shadow-card" : "text-muted-foreground hover:text-foreground",
                )}
              >
                {t === "work" ? <SquareTerminal className="size-3.5" /> : t === "council" ? <Vote className="size-3.5" /> : <ClipboardCopy className="size-3.5" />}
                {TARGET_INFO[t].label}
              </button>
            ))}
          </div>
          {draft.target !== "copy" && (
            <Button variant="secondary" size="sm" onClick={copy} disabled={!rendered?.text}>
              <Copy /> Copy
            </Button>
          )}
          <Button size="sm" onClick={onSend} disabled={!rendered?.text || blocked || (draft.target === "work" && !draft.project)} title={why ?? TARGET_INFO[draft.target].blurb}>
            <SendIcon /> {sendLabel}
            {draft.target === "work" && projectName ? <span className="opacity-75">· {projectName}</span> : null}
          </Button>
        </div>
      </div>

      {error && <ErrorState className="m-4" title="Could not render the prompt" message={error} />}

      {rendered && (rendered.lint ?? []).length > 0 && (
        <ul aria-label="Lint" className="divide-y border-b">
          {(rendered.lint ?? []).map((f, i) => {
            const S = SEVERITY[f.severity]
            return (
              <li
                key={`${f.code}-${i}`}
                className={cn("flex items-start gap-2.5 px-4 py-2 text-xs", f.severity === "error" && draft.target !== "copy" && "bg-danger-soft/50")}
              >
                <S.icon className={cn("mt-0.5 size-3.5 shrink-0", f.severity === "error" ? "text-danger-fg" : f.severity === "warning" ? "text-warning-fg" : "text-subtle-foreground")} />
                <span className="min-w-0 flex-1 text-foreground/90">{f.message}</span>
                <StatusPill tone={S.tone} className="h-4">
                  {S.label}
                </StatusPill>
              </li>
            )
          })}
        </ul>
      )}

      <div className="relative">
        {!rendered ? (
          <div className="space-y-2 p-4">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-4 w-3/4" />
          </div>
        ) : rendered.text.trim() === "" ? (
          <p className="p-4 text-sm text-muted-foreground">Write the task above; the prompt appears here as it will be sent.</p>
        ) : (
          <pre
            aria-label="Rendered prompt"
            className={cn("overflow-auto whitespace-pre-wrap break-words bg-background/40 px-4 py-3 font-mono [font-variant-ligatures:none] text-[0.8125rem] leading-[1.4rem] text-foreground/90", !full && "max-h-[26rem]")}
          >
            {rendered.text}
          </pre>
        )}
        {rendered && rendered.text.length > 2400 && (
          <div className="flex justify-center border-t py-1.5">
            <button onClick={() => setFull((f) => !f)} className="text-xs text-muted-foreground hover:text-foreground">
              {full ? "Show less" : "Show the whole prompt"}
            </button>
          </div>
        )}
      </div>
      {why && draft.target !== "copy" && (
        <div className="flex items-center gap-2 border-t bg-muted/30 px-4 py-2 text-xs text-muted-foreground">
          <ShieldAlert className="size-3.5 shrink-0 text-danger-fg" />
          {why}
        </div>
      )}
      <div className="border-t px-4 py-2 text-2xs text-subtle-foreground">
        {TARGET_INFO[draft.target].blurb} <kbd className="ml-1 rounded border bg-muted px-1 font-sans">{isMac() ? "⌘" : "Ctrl"} K</kbd> “New prompt from template…”
      </div>
    </Card>
  )
}

/* ---------- save and improve ---------- */

function SaveDialog({
  open,
  onClose,
  draft,
  current,
  sections,
  meta,
  onSaved,
}: {
  open: boolean
  onClose: () => void
  draft: Draft
  current?: Template
  sections: Section[]
  meta: Map<SectionId, SectionMeta>
  onSaved: (t?: Template) => void
}) {
  const [kind, setKind] = useState<"template" | "snippet">("template")
  const [name, setName] = useState("")
  const [section, setSection] = useState<SectionId>("task")
  const [busy, setBusy] = useState(false)
  const own = current?.source === "user"
  const builtin = current?.source === "builtin" || current?.overrides
  useEffect(() => {
    if (!open) return
    setName(own ? (current?.name ?? "") : "")
    const firstFilled = sections.find((s) => s.body.trim() && s.id !== "role")
    setSection((firstFilled?.id as SectionId) ?? "task")
  }, [open]) // reset when the dialog opens, not while typing

  const save = async (as: "new" | "same" | "override") => {
    setBusy(true)
    try {
      if (kind === "snippet") {
        const body = draft.sections[section]
        await saveSnippet({ id: slugId(name || `${section}-snippet`), name: name || `${meta.get(section)?.title} snippet`, section, body })
        toast.success("Snippet saved")
        onSaved()
      } else {
        const id = as === "override" && current ? current.id : as === "same" && current ? current.id : slugId(name)
        const t = await saveTemplate({
          id,
          name: as === "override" ? (current?.name ?? name) : name || current?.name || "My prompt",
          description: as === "new" ? `Made in Prompt Studio from ${current?.name ?? "a template"}.` : current?.description,
          target: draft.target,
          order: current?.order ?? 50,
          version: current?.version ?? 1,
          sections: sections.filter((s) => s.body.trim() !== "" || current?.sections.some((x) => x.id === s.id)),
        })
        toast.success(as === "override" ? `${t.name}: your version is saved` : `Saved “${t.name}”`)
        onSaved(t)
      }
      onClose()
    } catch (e) {
      toast.error("Could not save", { description: errorMessage(e) })
    } finally {
      setBusy(false)
    }
  }
  const sectionBody = draft.sections[section]?.trim() ?? ""
  return (
    <Dialog open={open} onClose={onClose} title="Save" description="Templates and snippets are kept in your data folder, not in the repository.">
      <div className="mb-4 inline-flex rounded-md border bg-muted/50 p-0.5">
        {(["template", "snippet"] as const).map((k) => (
          <button
            key={k}
            onClick={() => setKind(k)}
            className={cn("rounded px-3 py-1 text-xs", kind === k ? "bg-elevated font-medium shadow-card" : "text-muted-foreground")}
          >
            {k === "template" ? "As a template" : "A section as a snippet"}
          </button>
        ))}
      </div>
      {kind === "snippet" && (
        <label className="mb-3 block">
          <span className="text-xs font-medium">Section</span>
          <select value={section} onChange={(e) => setSection(e.target.value as SectionId)} className="mt-1 h-8 w-full rounded-md border bg-background/60 px-2 text-sm">
            {sections
              .filter((s) => s.body.trim())
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {meta.get(s.id)?.title ?? s.id} · {s.body.trim().length} chars
                </option>
              ))}
          </select>
        </label>
      )}
      <label className="block">
        <span className="text-xs font-medium">Name</span>
        <input
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={kind === "template" ? "e.g. Go service builder" : "e.g. Run the Go checks"}
          className="mt-1 h-9 w-full rounded-md border bg-background/60 px-2.5 text-sm outline-none focus-visible:border-ring"
        />
      </label>
      <div className="mt-5 flex flex-wrap justify-end gap-2">
        {kind === "template" && builtin && current && (
          <Button variant="secondary" disabled={busy} onClick={() => void save("override")} title="Studio shows your version of this built-in; Work's safety rules never change">
            Save as your {current.name}
          </Button>
        )}
        {kind === "template" && own && (
          <Button variant="secondary" disabled={busy} onClick={() => void save("same")}>
            Update “{current?.name}”
          </Button>
        )}
        <Button disabled={busy || (kind === "template" ? !name.trim() : !sectionBody)} onClick={() => void save("new")}>
          {busy ? <Loader2 className="animate-spin" /> : <Save />} {kind === "template" ? "Save as new" : "Save snippet"}
        </Button>
      </div>
    </Dialog>
  )
}

const IMPROVE_MODELS: Record<string, string> = { claude: "haiku", codex: "", grok: "" }

function ImproveSheet({
  open,
  onClose,
  sections,
  project,
  meta,
  onApply,
}: {
  open: boolean
  onClose: () => void
  sections: Section[]
  project: string
  meta: Map<SectionId, SectionMeta>
  onApply: (s: Section[]) => void
}) {
  const [provider, setProvider] = useState("claude")
  const [model, setModel] = useState("haiku")
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<ImproveResult | null>(null)
  const [err, setErr] = useState<string | null>(null)
  useEffect(() => {
    if (open) {
      setResult(null)
      setErr(null)
    }
  }, [open])
  const text = sections.filter((s) => s.id !== "context").map((s) => s.body.trim()).filter(Boolean).join("\n\n")
  const run = async () => {
    setBusy(true)
    setErr(null)
    try {
      setResult(
        await improvePrompt({ sections: sections.filter((s) => s.id !== "context"), provider, model: model || undefined, project: project || undefined }),
      )
    } catch (e) {
      setErr(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Sheet
      open={open}
      onClose={busy ? () => {} : onClose}
      title="Improve this prompt"
      description="One call with no tools restructures your prompt into sections. You see the result first; nothing changes until you apply it."
    >
      <div className="space-y-4 p-5">
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <div className="mb-1 text-2xs font-medium uppercase tracking-wider text-subtle-foreground">Model</div>
            <div className="flex gap-1">
              {["claude", "codex", "grok"].map((p) => (
                <button
                  key={p}
                  onClick={() => {
                    setProvider(p)
                    setModel(IMPROVE_MODELS[p])
                  }}
                  className={cn(
                    "flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-xs",
                    provider === p ? "border-brand/60 bg-brand-soft" : "hover:bg-accent/50",
                  )}
                >
                  <ProviderTile provider={p} size="sm" />
                  {providerInfo(p)?.label ?? p}
                </button>
              ))}
            </div>
          </div>
          <label>
            <div className="mb-1 text-2xs font-medium uppercase tracking-wider text-subtle-foreground">Model alias</div>
            <input
              value={model}
              onChange={(e) => setModel(e.target.value)}
              placeholder="CLI default"
              className="h-8 w-32 rounded-md border bg-background/60 px-2 font-mono text-xs outline-none focus-visible:border-ring"
            />
          </label>
          <Button disabled={busy || !text} onClick={() => void run()} className="ml-auto">
            {busy ? <Loader2 className="animate-spin" /> : <Sparkles />} {result ? "Ask again" : "Improve"}
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          Sends the sections' text ({text.length.toLocaleString()} characters; context sources stay here) to {providerInfo(provider)?.label ?? provider} on your own
          account. The default is the cheapest model. Confidential projects, links to confidential pages and secret-looking strings are refused.
        </p>
        {err && <ErrorState title="Could not improve the prompt" message={err} />}
        {busy && (
          <div className="space-y-2">
            <Skeleton className="h-16" />
            <Skeleton className="h-24" />
          </div>
        )}
        {result && (
          <>
            <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/30 px-3 py-2 text-xs">
              <ProviderTile provider={result.provider} size="sm" />
              <span className="font-medium">{providerInfo(result.provider)?.label ?? result.provider}</span>
              {result.usage.model && <Badge className="font-mono">{result.usage.model}</Badge>}
              <span className="ml-auto tabular-nums text-muted-foreground">
                {result.usage.cost_usd ? `$${result.usage.cost_usd.toFixed(4)}` : "cost not reported"} · {((result.usage.duration_ms ?? 0) / 1000).toFixed(1)} s
              </span>
            </div>
            <ol className="space-y-3">
              {result.sections.map((s) => (
                <li key={s.id} className="rounded-lg border">
                  <div className="border-b bg-muted/30 px-3 py-1.5 text-xs font-semibold">{meta.get(s.id)?.title ?? s.id}</div>
                  <pre className="whitespace-pre-wrap break-words px-3 py-2 font-mono text-xs leading-5">{s.body}</pre>
                </li>
              ))}
            </ol>
            <div className="flex justify-end gap-2">
              <Button variant="secondary" onClick={onClose}>
                Discard
              </Button>
              <Button onClick={() => onApply(result.sections)}>
                <Check /> Apply to the editor
              </Button>
            </div>
            <p className="text-right text-2xs text-subtle-foreground">Applying replaces every section except Context; your sources stay.</p>
          </>
        )}
      </div>
    </Sheet>
  )
}
