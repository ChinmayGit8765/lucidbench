import { useEffect, useRef, useState } from "react"
import {
  Check,
  Copy,
  Download,
  Ellipsis,
  Eye,
  EyeOff,
  Monitor,
  Palette,
  RotateCcw,
  Save,
  Trash2,
  Upload,
  WandSparkles,
  X,
} from "lucide-react"
import { toast } from "sonner"

import { ProviderMark, tintVar } from "@/components/ProviderMark"
import { ThemeThumb } from "@/components/ThemeThumb"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Menu } from "@/components/ui/menu"
import { EmptyState, ErrorState } from "@/components/ui/states"
import { errorMessage, getJSON, request, sendJSON, usePoll } from "@/lib/api"
import { usePrefs, type Prefs } from "@/lib/prefs"
import {
  assetURL,
  FONTS,
  hasArt,
  resolveThemeId,
  SYSTEM_THEME,
  type Density,
  type FontChoice,
  type Overrides,
  type SidebarMode,
  type Theme,
  type ThemeBundle,
} from "@/lib/theme"
import { cn } from "@/lib/utils"
import type { Account } from "@/pages/Accounts"
import { Row, Section, Segmented, Switch } from "@/pages/settings/controls"

const ACCENTS = ["#4f8ff7", "#8b5cf6", "#ec4899", "#f97316", "#eab308", "#22c55e", "#14b8a6", "#a1a1aa"]

/* ---------- theme gallery ---------- */

function ThemeCard({
  theme,
  selected,
  onSelect,
  badge,
}: {
  theme: Theme
  selected: boolean
  onSelect: () => void
  badge?: string
}) {
  return (
    <button
      onClick={onSelect}
      aria-pressed={selected}
      className={cn(
        "group flex flex-col gap-2 rounded-xl border bg-background/40 p-2 text-left outline-none transition-[border-color,box-shadow] hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring",
        selected && "border-brand/60 shadow-[0_0_0_1px_var(--brand)]",
      )}
    >
      <ThemeThumb theme={theme} />
      <span className="flex items-center gap-1.5 px-0.5">
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{theme.name}</span>
        {badge && <span className="text-2xs text-subtle-foreground">{badge}</span>}
        {selected && (
          <span className="flex size-4 items-center justify-center rounded-full bg-brand text-primary-foreground">
            <Check className="size-2.5" strokeWidth={3} />
          </span>
        )}
      </span>
    </button>
  )
}

function SystemCard({ themes, selected, onSelect }: { themes: Theme[]; selected: boolean; onSelect: () => void }) {
  const dark = themes.find((t) => t.id === "midnight")
  const light = themes.find((t) => t.id === "daylight")
  return (
    <button
      onClick={onSelect}
      aria-pressed={selected}
      className={cn(
        "group flex flex-col gap-2 rounded-xl border bg-background/40 p-2 text-left outline-none transition-[border-color,box-shadow] hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring",
        selected && "border-brand/60 shadow-[0_0_0_1px_var(--brand)]",
      )}
    >
      <div className="relative aspect-[16/10] w-full overflow-hidden rounded-[calc(var(--radius)-2px)]">
        {light && <ThemeThumb theme={light} className="absolute inset-0 rounded-none border-0" />}
        {dark && (
          <div className="absolute inset-0 [clip-path:polygon(55%_0,100%_0,100%_100%,35%_100%)]">
            <ThemeThumb theme={dark} className="rounded-none border-0" />
          </div>
        )}
        <span className="absolute inset-0 rounded-[inherit] border" />
      </div>
      <span className="flex items-center gap-1.5 px-0.5">
        <Monitor className="size-3.5 text-subtle-foreground" />
        <span className="flex-1 text-sm font-medium">Match system</span>
        {selected && (
          <span className="flex size-4 items-center justify-center rounded-full bg-brand text-primary-foreground">
            <Check className="size-2.5" strokeWidth={3} />
          </span>
        )}
      </span>
    </button>
  )
}

/* ---------- customise ---------- */

function Customise({ prefs, set }: { prefs: Prefs; set: (o: Overrides) => void }) {
  const o = prefs.overrides
  const custom = o.accent && !ACCENTS.includes(o.accent) ? o.accent : null
  return (
    <>
      <Row label="Accent" hint="The colour of links, focus rings, selection and primary buttons.">
        <div className="flex items-center gap-1.5">
          <button
            onClick={() => set({ ...o, accent: undefined })}
            title="The theme's own accent"
            aria-pressed={!o.accent}
            className={cn(
              "flex h-6 items-center rounded-full border px-2 text-2xs text-muted-foreground transition-colors hover:text-foreground",
              !o.accent && "border-brand/60 text-foreground",
            )}
          >
            Theme
          </button>
          {ACCENTS.map((c) => (
            <button
              key={c}
              onClick={() => set({ ...o, accent: c })}
              aria-label={`Accent ${c}`}
              aria-pressed={o.accent === c}
              style={{ backgroundColor: c }}
              className={cn(
                "size-6 rounded-full border border-black/10 transition-transform hover:scale-110",
                o.accent === c && "ring-2 ring-ring ring-offset-2 ring-offset-card",
              )}
            />
          ))}
          <label
            title="Pick any colour"
            className={cn(
              "relative flex size-6 cursor-pointer items-center justify-center overflow-hidden rounded-full border bg-[conic-gradient(red,yellow,lime,aqua,blue,magenta,red)]",
              custom && "ring-2 ring-ring ring-offset-2 ring-offset-card",
            )}
          >
            <input
              type="color"
              value={custom ?? "#4f8ff7"}
              onChange={(e) => set({ ...o, accent: e.target.value })}
              className="absolute inset-0 cursor-pointer opacity-0"
              aria-label="Custom accent colour"
            />
          </label>
        </div>
      </Row>
      <Row label="Density" hint="Comfortable scales text and spacing up one step.">
        <Segmented<Density>
          label="Density"
          value={o.density ?? "compact"}
          onChange={(v) => set({ ...o, density: v === "compact" ? undefined : v })}
          options={[
            { id: "compact", label: "Compact" },
            { id: "comfortable", label: "Comfortable" },
          ]}
        />
      </Row>
      <Row label="Corner radius" hint={o.radius === undefined ? "The theme's own radius." : `${o.radius}px`}>
        <input
          type="range"
          min={0}
          max={16}
          step={1}
          value={o.radius ?? 10}
          onChange={(e) => set({ ...o, radius: Number(e.target.value) })}
          aria-label="Corner radius"
          className="w-40 accent-[var(--brand)]"
        />
        <span className="w-9 text-right text-xs tabular-nums text-muted-foreground">{o.radius === undefined ? "auto" : `${o.radius}px`}</span>
        {o.radius !== undefined && (
          <Button variant="ghost" size="icon-sm" aria-label="Reset radius" title="Use the theme's radius" onClick={() => set({ ...o, radius: undefined })}>
            <RotateCcw />
          </Button>
        )}
      </Row>
      <Row label="Font" hint={FONTS.find((f) => f.id === (o.font ?? "geist"))?.hint ?? "Geist ships with Lucidbench."}>
        <Segmented<FontChoice>
          label="Font"
          value={o.font ?? "geist"}
          onChange={(v) => set({ ...o, font: v === "geist" ? undefined : v })}
          options={FONTS.map((f) => ({ id: f.id, label: f.label, title: f.hint }))}
        />
      </Row>
      <Row label="Sidebar" hint="A rail shows icons only. Below 1024px the sidebar is always a rail.">
        <Segmented<SidebarMode>
          label="Sidebar"
          value={o.sidebar ?? "expanded"}
          onChange={(v) => set({ ...o, sidebar: v })}
          options={[
            { id: "expanded", label: "Expanded" },
            { id: "rail", label: "Rail" },
          ]}
        />
      </Row>
    </>
  )
}

/* ---------- describe a theme ---------- */

const GEN_PROVIDERS = [
  { id: "claude", label: "Claude" },
  { id: "codex", label: "Codex" },
  { id: "grok", label: "Grok" },
] as const

interface Preview extends ThemeBundle {
  usage: {
    provider: string
    model?: string
    input_tokens?: number
    output_tokens?: number
    cache_read_tokens?: number
    cost_usd?: number
    duration_ms: number
  }
  dropped: string[]
}

const k = (n?: number) => (n === undefined ? "?" : n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n))

function Describe({ onSaved }: { onSaved: (t: Theme) => void }) {
  const { preview, setPreview } = usePrefs()
  const accounts = usePoll<Account[]>("/api/accounts", 60000)
  const [provider, setProvider] = useState<string>("claude")
  const [profile, setProfile] = useState("default")
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const [started, setStarted] = useState(0)
  const [now, setNow] = useState(0)
  const [err, setErr] = useState<string | null>(null)
  const [result, setResult] = useState<Preview | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!busy) return
    const id = setInterval(() => setNow(Date.now()), 500)
    return () => clearInterval(id)
  }, [busy])

  const profiles = (accounts.data ?? []).filter((a) => a.provider === provider && a.location === "host")
  const trying = !!result && preview?.theme.id === result.theme.id

  const generate = async () => {
    setBusy(true)
    setErr(null)
    setResult(null)
    setPreview(null)
    setStarted(Date.now())
    setNow(Date.now())
    try {
      const p = await sendJSON<Preview>("/api/themes/generate", "POST", {
        description: text.trim(),
        provider,
        profile: profile === "default" ? undefined : profile,
      })
      setResult(p)
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
      const t = await sendJSON<Theme>("/api/themes", "POST", { theme: result.theme, assets: result.assets })
      toast.success("Theme saved", { description: t.name })
      setPreview(null)
      setResult(null)
      onSaved(t)
    } catch (e) {
      toast.error("Could not save the theme", { description: errorMessage(e) })
    } finally {
      setSaving(false)
    }
  }
  const discard = () => {
    setPreview(null)
    setResult(null)
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
        </div>
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          maxLength={500}
          rows={4}
          placeholder="A calm forest at dusk: mossy greens, warm lantern light, soft paper texture…"
          className="w-full resize-none rounded-lg border bg-background/60 px-3 py-2 text-sm outline-none transition-colors placeholder:text-subtle-foreground focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/40"
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && text.trim().length >= 3 && !busy) void generate()
          }}
        />
        <div className="flex items-center justify-between gap-3">
          <p className="text-xs text-subtle-foreground">
            Runs the {GEN_PROVIDERS.find((p) => p.id === provider)?.label} CLI on this machine with no tools. Art is original: colour, light and shapes, never existing characters.
          </p>
          <Button onClick={() => void generate()} disabled={busy || text.trim().length < 3}>
            <WandSparkles className={cn(busy && "animate-pulse")} />
            {busy ? `Generating ${Math.max(0, Math.round((now - started) / 1000))}s` : "Generate"}
          </Button>
        </div>
        {err && <ErrorState title="Generation failed" message={err} onRetry={() => void generate()} />}
      </div>

      <div className="border-t bg-muted/20 p-5 @3xl:border-l @3xl:border-t-0">
        {!result ? (
          <div className="flex h-full min-h-48 flex-col items-center justify-center gap-2 rounded-lg border border-dashed text-center">
            <Palette className="size-5 text-subtle-foreground" />
            <p className="max-w-64 text-sm text-muted-foreground">
              {busy ? "Waiting for the model. This usually takes under a minute." : "The preview appears here. Nothing is saved until you choose Save."}
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            <ThemeThumb theme={result.theme} inline={result.assets} />
            <div>
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-semibold">{result.theme.name}</h3>
                <Badge>{result.theme.base}</Badge>
              </div>
              {result.theme.description && <p className="mt-0.5 text-xs text-muted-foreground">{result.theme.description}</p>}
            </div>
            {result.theme.art?.spriteBoard && result.theme.art.spriteBoard.length > 0 && (
              <div className="flex gap-2">
                {result.theme.art.spriteBoard.map((s, i) => (
                  <figure key={`${s.file}-${i}`} className="flex w-16 flex-col items-center gap-1">
                    <img src={assetURL(result.theme, s.file, result.assets)} alt={s.caption ?? ""} className="theme-art size-14 rounded-md border bg-background/50 object-contain p-1" />
                    {s.caption && <figcaption className="truncate text-2xs text-muted-foreground">{s.caption}</figcaption>}
                  </figure>
                ))}
              </div>
            )}
            {result.theme.labels && Object.keys(result.theme.labels).length > 0 && (
              <p className="text-xs text-muted-foreground">
                Renames:{" "}
                {Object.entries(result.theme.labels)
                  .map(([key, v]) => `${key.replace(/_/g, " ")} → “${v}”`)
                  .join(", ")}
              </p>
            )}
            <p className="font-mono text-2xs text-subtle-foreground">
              {result.usage.provider}
              {result.usage.model && ` · ${result.usage.model}`} · {k(result.usage.input_tokens)} in · {k(result.usage.output_tokens)} out
              {result.usage.cost_usd ? ` · $${result.usage.cost_usd.toFixed(3)}` : ""} · {(result.usage.duration_ms / 1000).toFixed(0)}s
            </p>
            {result.dropped.length > 0 && (
              <p className="text-2xs text-subtle-foreground" title={result.dropped.join(", ")}>
                {result.dropped.length} unsafe or unknown {result.dropped.length === 1 ? "part was" : "parts were"} dropped.
              </p>
            )}
            <div className="flex flex-wrap gap-2 pt-1">
              <Button onClick={() => void save()} disabled={saving}>
                <Save /> {saving ? "Saving" : "Save theme"}
              </Button>
              <Button variant="secondary" onClick={() => setPreview(trying ? null : result)}>
                {trying ? <EyeOff /> : <Eye />} {trying ? "Stop trying" : "Try it"}
              </Button>
              <Button variant="ghost" onClick={discard}>
                <X /> Discard
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

/* ---------- your themes ---------- */

function download(name: string, body: string) {
  const url = URL.createObjectURL(new Blob([body], { type: "application/json" }))
  const a = document.createElement("a")
  a.href = url
  a.download = name
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

function freeId(base: string, taken: Theme[]): string {
  const stem = base.replace(/-copy(-\d+)?$/, "").slice(0, 40)
  for (let i = 1; ; i++) {
    const id = `${stem}-copy${i > 1 ? `-${i}` : ""}`
    if (!taken.some((t) => t.id === id)) return id
  }
}

function YourThemes({ apply }: { apply: (id: string) => void }) {
  const { themes, reloadThemes, prefs, update } = usePrefs()
  const mine = themes.filter((t) => !t.builtin)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const file = useRef<HTMLInputElement>(null)

  const exportOne = async (t: Theme) => {
    try {
      const b = await getJSON<ThemeBundle>(`/api/themes/${encodeURIComponent(t.id)}/export`)
      download(`${t.id}.theme.json`, JSON.stringify(b, null, 2))
    } catch (e) {
      toast.error("Could not export", { description: errorMessage(e) })
    }
  }
  const duplicate = async (t: Theme) => {
    try {
      const b = await getJSON<ThemeBundle>(`/api/themes/${encodeURIComponent(t.id)}/export`)
      const id = freeId(t.id, themes)
      const copy = await sendJSON<Theme>("/api/themes", "POST", {
        theme: { ...b.theme, id, name: `${b.theme.name} copy`.slice(0, 60), builtin: false },
        assets: b.assets,
      })
      await reloadThemes()
      toast.success("Theme duplicated", { description: copy.name })
    } catch (e) {
      toast.error("Could not duplicate", { description: errorMessage(e) })
    }
  }
  const remove = (t: Theme) =>
    setConfirm({
      title: `Delete ${t.name}?`,
      description: "The theme and its art are removed from your data folder. Export it first to keep a copy.",
      confirmLabel: "Delete theme",
      danger: true,
      run: async () => {
        try {
          await request(`/api/themes/${encodeURIComponent(t.id)}`, { method: "DELETE", headers: { "X-Lucid-Confirm": "yes" } })
          if (prefs.theme === t.id) update((p) => ({ ...p, theme: "midnight" }))
          await reloadThemes()
          toast.success("Theme deleted")
        } catch (e) {
          toast.error("Could not delete", { description: errorMessage(e) })
        }
      },
    })
  const importFile = async (f: File) => {
    try {
      const b = JSON.parse(await f.text()) as Partial<ThemeBundle>
      if (!b.theme || typeof b.theme !== "object") throw new Error("This file has no theme in it.")
      const theme = { ...b.theme, builtin: false } as Theme
      if (themes.some((t) => t.id === theme.id)) theme.id = freeId(theme.id, themes)
      const saved = await sendJSON<Theme>("/api/themes", "POST", { theme, assets: b.assets })
      await reloadThemes()
      toast.success("Theme imported", { description: saved.name })
    } catch (e) {
      toast.error("Could not import", { description: errorMessage(e) })
    }
  }

  return (
    <Section
      title="Your themes"
      description="Saved in the themes folder of your data directory. Export one to share it; import a .theme.json file to add one."
      actions={
        <>
          <input
            ref={file}
            type="file"
            accept="application/json,.json"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) void importFile(f)
              e.target.value = ""
            }}
          />
          <Button variant="secondary" size="sm" onClick={() => file.current?.click()}>
            <Upload /> Import
          </Button>
        </>
      }
    >
      {mine.length === 0 ? (
        <div className="border-t">
          <EmptyState icon={<Palette />} title="No themes of your own yet" description="Describe one above, or duplicate a preset by applying it and exporting." className="py-8" />
        </div>
      ) : (
        <ul className="divide-y border-t">
          {mine.map((t) => (
            <li key={t.id} className="flex items-center gap-3 px-5 py-2.5">
              <div className="w-20 shrink-0">
                <ThemeThumb theme={t} />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium">{t.name}</span>
                  {prefs.theme === t.id && <Badge className="text-brand-fg">in use</Badge>}
                  {hasArt(t) && <Badge>art</Badge>}
                </div>
                <div className="truncate text-xs text-muted-foreground">{t.description ?? t.id}</div>
              </div>
              {prefs.theme !== t.id && (
                <Button variant="secondary" size="sm" onClick={() => apply(t.id)}>
                  Apply
                </Button>
              )}
              <Menu
                label={`Actions for ${t.name}`}
                trigger={<Ellipsis />}
                items={[
                  { label: "Duplicate", icon: Copy, onSelect: () => void duplicate(t) },
                  { label: "Export JSON", icon: Download, onSelect: () => void exportOne(t) },
                  { label: "Delete", icon: Trash2, danger: true, onSelect: () => remove(t) },
                ]}
              />
            </li>
          ))}
        </ul>
      )}
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </Section>
  )
}

/* ---------- page ---------- */

export function Appearance({ focus }: { focus?: string }) {
  const { prefs, update, themes, themesLoaded, active, reloadThemes } = usePrefs()
  const describe = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (focus === "describe") describe.current?.scrollIntoView({ behavior: "smooth", block: "start" })
  }, [focus])
  const apply = (id: string) => update((p) => ({ ...p, theme: id }))
  const setOverrides = (o: Overrides) => update((p) => ({ ...p, overrides: o }))
  const presets = themes.filter((t) => t.builtin)
  const mine = themes.filter((t) => !t.builtin)
  const chosen = prefs.theme === SYSTEM_THEME ? SYSTEM_THEME : resolveThemeId(prefs.theme)
  const sprites = active?.art?.spriteBoard?.length ?? 0

  return (
    <div className="space-y-5">
      <Section title="Theme" description="Presets ship with Lucidbench; your own themes follow them. The whole app switches at once.">
        <div className="grid grid-cols-2 gap-3 border-t p-4 @2xl:grid-cols-3 @4xl:grid-cols-4">
          {!themesLoaded && <p className="col-span-full p-4 text-sm text-muted-foreground">Loading themes…</p>}
          {presets.length > 0 && <SystemCard themes={presets} selected={chosen === SYSTEM_THEME} onSelect={() => apply(SYSTEM_THEME)} />}
          {[...presets, ...mine].map((t) => (
            <ThemeCard key={t.id} theme={t} selected={chosen === t.id} onSelect={() => apply(t.id)} badge={t.builtin ? undefined : "yours"} />
          ))}
        </div>
        {active && hasArt(active) && (
          <Row
            label="Sprite board"
            hint={sprites > 0 ? `Show ${active.name}'s ${sprites} sprites as a card on the Overview.` : `${active.name} has art but no sprites.`}
          >
            <Switch label="Show the sprite board" checked={prefs.sprite_board && sprites > 0} onChange={(v) => update((p) => ({ ...p, sprite_board: v }))} />
          </Row>
        )}
      </Section>

      <Section
        title="Customise"
        description="Fine-tune any theme. These apply on top of whichever theme is active."
        actions={
          Object.values(prefs.overrides).some((v) => v !== undefined && v !== "expanded") ? (
            <Button variant="ghost" size="sm" onClick={() => setOverrides({ sidebar: prefs.overrides.sidebar })}>
              <RotateCcw /> Reset
            </Button>
          ) : undefined
        }
      >
        <Customise prefs={prefs} set={setOverrides} />
      </Section>

      <div ref={describe} className="scroll-mt-6">
        <Section
          id="describe"
          title="Describe a theme"
          description="Say what you want it to feel like. A model on your own subscription drafts the colours and a few pieces of original art; you preview it before anything is saved."
        >
          <Describe
            onSaved={(t) => {
              void reloadThemes().then(() =>
                update((p) => ({ ...p, theme: t.id, sprite_board: p.sprite_board || !!t.art?.spriteBoard?.length })),
              )
            }}
          />
        </Section>
      </div>

      <YourThemes apply={apply} />
    </div>
  )
}

