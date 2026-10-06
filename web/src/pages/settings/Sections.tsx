import { useEffect, useRef, useState } from "react"
import { Braces, LayoutDashboard, Plus, Save, Trash2, WandSparkles, X } from "lucide-react"
import { toast } from "sonner"

import { ProviderMark, tintVar } from "@/components/ProviderMark"
import { SectionPreview } from "@/components/SectionRenderer"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, getJSON, refreshAll, usePoll } from "@/lib/api"
import {
  addTemplate,
  CATALOG_PATH,
  generateSection,
  removeSection,
  saveSection,
  sectionURL,
  SECTIONS_PATH,
  type Catalog,
  type Generated,
  type Listing,
  type Placement,
  type Section,
} from "@/lib/sections"
import { cn } from "@/lib/utils"
import type { Account } from "@/pages/Accounts"
import { Section as Panel } from "@/pages/settings/controls"

const GEN_PROVIDERS = [
  { id: "claude", label: "Claude" },
  { id: "codex", label: "Codex" },
  { id: "grok", label: "Grok" },
] as const

const VIEW_LABEL: Record<string, string> = { stat: "Number", list: "List", table: "Table", bars: "Bars", markdown: "Text" }

const where = (s: Section) => (s.placement === "project" ? "each project's page" : "Overview")

function SectionRow({ s, onRemove }: { s: Section; onRemove: () => void }) {
  return (
    <li className="flex items-center gap-3 px-5 py-2.5">
      <span className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-background/60 text-subtle-foreground">
        <LayoutDashboard className="size-3.5" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{s.title}</div>
        <div className="truncate font-mono text-2xs text-subtle-foreground">
          {s.source.api} · {VIEW_LABEL[s.view] ?? s.view} · on {where(s)}
        </div>
      </div>
      <Button variant="ghost" size="sm" onClick={onRemove} aria-label={`Remove ${s.title}`}>
        <Trash2 /> Remove
      </Button>
    </li>
  )
}

/** The user's sections, the built-in templates and "Describe a section". */
export function Sections({ focus }: { focus?: string }) {
  const listing = usePoll<Listing>(SECTIONS_PATH, 30_000)
  const catalog = usePoll<Catalog>(CATALOG_PATH, 600_000)
  const describe = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (focus === "describe") describe.current?.scrollIntoView({ behavior: "smooth", block: "start" })
  }, [focus])

  const reload = () => {
    listing.refresh()
    refreshAll()
  }
  const add = async (t: Section) => {
    try {
      const s = await addTemplate(t.id)
      toast.success(`${s.title} added`, { description: `It shows on ${where(s)}.` })
      reload()
    } catch (e) {
      toast.error("Could not add the section", { description: errorMessage(e) })
    }
  }
  const remove = async (s: Section) => {
    try {
      await removeSection(s.id)
      toast.success(`${s.title} removed`, {
        action: { label: "Undo", onClick: () => void saveSection(s).then(reload) },
      })
      reload()
    } catch (e) {
      toast.error("Could not remove the section", { description: errorMessage(e) })
    }
  }
  const mine = listing.data?.sections ?? []

  return (
    <div className="space-y-5">
      <Panel
        title="Your sections"
        description="Widgets on the Overview and on each project's page. Each one reads a single read-only Lucidbench route and shows part of its answer; none of them runs code."
      >
        {listing.loading && !listing.data ? (
          <div className="space-y-2 border-t p-5">
            <Skeleton className="h-9" />
          </div>
        ) : listing.error && !listing.data ? (
          <div className="border-t p-4">
            <ErrorState title="Could not read your sections" message={listing.error.message} onRetry={listing.refresh} />
          </div>
        ) : mine.length === 0 ? (
          <EmptyState icon={<LayoutDashboard />} title="No sections yet" description="Add a template below, or describe one in your own words." className="border-t py-8" />
        ) : (
          <ul className="divide-y border-t">
            {mine.map((s) => (
              <SectionRow key={s.id} s={s} onRemove={() => void remove(s)} />
            ))}
          </ul>
        )}
        {(listing.data?.broken.length ?? 0) > 0 && (
          <p className="border-t px-5 py-3 text-xs text-warning-fg">
            Not shown, because they break the section rules: {listing.data!.broken.map((b) => `${b.file} (${b.error})`).join("; ")}
          </p>
        )}
      </Panel>

      <Panel title="Templates" description="Ready-made sections. Adding one copies it into your sections, where you can remove it again.">
        <div className="grid gap-3 border-t p-4 @2xl:grid-cols-2 @4xl:grid-cols-3">
          {(catalog.data?.templates ?? []).map((t) => (
            <div key={t.id} className="flex flex-col gap-2 rounded-xl border bg-background/40 p-3.5" data-testid={`template-${t.id}`}>
              <div className="flex items-center gap-2">
                <span className="min-w-0 flex-1 truncate text-sm font-medium">{t.title}</span>
                <Badge>{VIEW_LABEL[t.view] ?? t.view}</Badge>
              </div>
              <p className="flex-1 text-xs text-muted-foreground">{t.description}</p>
              <div className="flex items-center justify-between gap-2">
                <span className="truncate font-mono text-2xs text-subtle-foreground">{t.source.api}</span>
                <Button size="sm" variant="secondary" onClick={() => void add(t)} aria-label={`Add ${t.title}`}>
                  <Plus /> Add to {t.placement === "project" ? "projects" : "Overview"}
                </Button>
              </div>
            </div>
          ))}
          {!catalog.data && <Skeleton className="h-28" />}
        </div>
      </Panel>

      <div ref={describe} className="scroll-mt-6">
        <Panel
          id="describe"
          title="Describe a section"
          description="Say what you want to see. Your own CLI, with no tools, writes the section as JSON from the list of allowed routes; you see it with live data before anything is saved."
        >
          <Describe catalog={catalog.data} onSaved={reload} />
        </Panel>
      </div>
    </div>
  )
}

const k = (n?: number) => (n === undefined ? "?" : n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n))

function Describe({ catalog, onSaved }: { catalog: Catalog | null; onSaved: () => void }) {
  const accounts = usePoll<Account[]>("/api/accounts", 60_000)
  const [provider, setProvider] = useState("claude")
  const [profile, setProfile] = useState("default")
  const [placement, setPlacement] = useState<Placement>("overview")
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [result, setResult] = useState<Generated | null>(null)
  const [data, setData] = useState<{ ok: boolean; value: unknown } | null>(null)
  const [showJSON, setShowJSON] = useState(false)
  const [saving, setSaving] = useState(false)
  const profiles = (accounts.data ?? []).filter((a) => a.provider === provider && a.location === "host")

  const generate = async () => {
    setBusy(true)
    setErr(null)
    setResult(null)
    setData(null)
    try {
      const g = await generateSection({ description: text.trim(), provider, profile: profile === "default" ? undefined : profile, placement })
      setResult(g)
      const url = sectionURL(g.section, catalog?.apis ?? null)
      if (url) {
        getJSON<unknown>(url).then(
          (value) => setData({ ok: true, value }),
          (e) => setData({ ok: false, value: errorMessage(e) }),
        )
      }
    } catch (e) {
      setErr(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  const save = async () => {
    if (!result) return
    setSaving(true)
    try {
      const s = await saveSection(result.section)
      toast.success(`${s.title} saved`, { description: `It shows on ${where(s)}.` })
      setResult(null)
      setText("")
      onSaved()
    } catch (e) {
      toast.error("Could not save the section", { description: errorMessage(e) })
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="grid gap-0 border-t @3xl:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
      <div className="space-y-3 p-5">
        <div className="flex flex-wrap items-center gap-2">
          <div role="radiogroup" aria-label="Provider" className="flex items-center gap-1 rounded-lg border bg-muted/50 p-0.5">
            {GEN_PROVIDERS.map((p) => {
              const on = provider === p.id
              return (
                <button
                  key={p.id}
                  role="radio"
                  aria-checked={on}
                  onClick={() => {
                    setProvider(p.id)
                    setProfile("default")
                  }}
                  className={cn(
                    "flex h-7 items-center gap-1.5 rounded-md px-2.5 text-xs transition-colors",
                    on ? "bg-elevated font-medium text-foreground shadow-card" : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  <span style={{ color: on ? tintVar(p.id) : undefined }} className="flex">
                    <ProviderMark provider={p.id} className="size-3.5" />
                  </span>
                  {p.label}
                </button>
              )
            })}
          </div>
          {provider !== "grok" && profiles.length > 1 && (
            <select
              value={profile}
              onChange={(e) => setProfile(e.target.value)}
              aria-label="Account profile"
              className="h-8 rounded-md border bg-secondary px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {profiles.map((a) => (
                <option key={a.name} value={a.name}>
                  {a.name}
                </option>
              ))}
            </select>
          )}
          <div role="radiogroup" aria-label="Where it shows" className="flex items-center gap-1 rounded-lg border bg-muted/50 p-0.5">
            {(["overview", "project"] as const).map((p) => (
              <button
                key={p}
                role="radio"
                aria-checked={placement === p}
                onClick={() => setPlacement(p)}
                className={cn(
                  "h-7 rounded-md px-2.5 text-xs transition-colors",
                  placement === p ? "bg-elevated font-medium text-foreground shadow-card" : "text-muted-foreground hover:text-foreground",
                )}
              >
                {p === "overview" ? "Overview" : "Project pages"}
              </button>
            ))}
          </div>
        </div>
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          maxLength={1000}
          rows={4}
          aria-label="Describe the section"
          placeholder="Council briefs waiting for my approval, newest first, with their project…"
          className="w-full resize-none rounded-lg border bg-background/60 px-3 py-2 text-sm outline-none transition-colors placeholder:text-subtle-foreground focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/40"
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && text.trim().length >= 3 && !busy) void generate()
          }}
        />
        <div className="flex items-center justify-between gap-3">
          <p className="text-xs text-subtle-foreground">
            Runs the {GEN_PROVIDERS.find((p) => p.id === provider)?.label} CLI on this machine with no tools{provider === "claude" ? ", on haiku" : ""}. It only sees this text and the list of allowed routes.
          </p>
          <Button onClick={() => void generate()} disabled={busy || text.trim().length < 3}>
            <WandSparkles className={cn(busy && "animate-pulse")} />
            {busy ? "Generating…" : "Generate"}
          </Button>
        </div>
        {err && <ErrorState title="Generation failed" message={err} onRetry={() => void generate()} />}
      </div>

      <div className="border-t bg-muted/20 p-5 @3xl:border-l @3xl:border-t-0" data-testid="section-preview">
        {!result ? (
          <div className="flex h-full min-h-48 flex-col items-center justify-center gap-2 rounded-lg border border-dashed text-center">
            <LayoutDashboard className="size-5 text-subtle-foreground" />
            <p className="max-w-64 text-sm text-muted-foreground">
              {busy ? "Waiting for the model. This usually takes a few seconds." : "The preview appears here, with live data. Nothing is saved until you choose Save."}
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            <div className="overflow-hidden rounded-xl border bg-card">
              <div className="px-4 pb-2 pt-3.5">
                <h3 className="text-sm font-semibold">{result.section.title}</h3>
                {result.section.description && <p className="text-xs text-subtle-foreground">{result.section.description}</p>}
              </div>
              {data === null ? (
                <div className="px-4 pb-4">
                  <Skeleton className="h-6" />
                </div>
              ) : data.ok ? (
                <SectionPreview section={result.section} data={data.value} />
              ) : (
                <p className="px-4 pb-4 text-sm text-muted-foreground">Could not read {result.section.source.api}: {String(data.value)}</p>
              )}
            </div>
            <p className="font-mono text-2xs text-subtle-foreground">
              {result.section.source.api} · {VIEW_LABEL[result.section.view]} · on {where(result.section)}
              <br />
              {result.usage.provider}
              {result.model && ` · ${result.model}`} · {k(result.usage.input_tokens)} in · {k(result.usage.output_tokens)} out
              {result.usage.cost_usd ? ` · $${result.usage.cost_usd.toFixed(4)}` : ""} · {(result.usage.duration_ms / 1000).toFixed(0)}s · prompt {result.prompt_version}
            </p>
            {showJSON && (
              <pre className="max-h-64 overflow-auto rounded-lg border bg-background/60 p-3 font-mono text-2xs leading-relaxed">{JSON.stringify(result.section, null, 2)}</pre>
            )}
            <div className="flex flex-wrap gap-2 pt-1">
              <Button onClick={() => void save()} disabled={saving}>
                <Save /> Save
              </Button>
              <Button variant="secondary" onClick={() => setShowJSON((v) => !v)}>
                <Braces /> {showJSON ? "Hide JSON" : "Show JSON"}
              </Button>
              <Button variant="ghost" onClick={() => setResult(null)}>
                <X /> Discard
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
