import { useRef, useState } from "react"
import { Copy, Trash2, Upload } from "lucide-react"
import { toast } from "sonner"

import { Lumi, SPRITE_SLOTS, StateSprite, type SpriteState } from "@/components/StateSprite"
import { Button } from "@/components/ui/button"
import { errorMessage, getJSON, sendJSON } from "@/lib/api"
import { usePrefs } from "@/lib/prefs"
import { type Theme, type ThemeBundle } from "@/lib/theme"
import { cn } from "@/lib/utils"
import { Section } from "@/pages/settings/controls"

const MAX_RASTER = 1 << 20
const MAX_SVG = 256 << 10
const EXT: Record<string, string> = { "image/png": "png", "image/gif": "gif", "image/webp": "webp", "image/svg+xml": "svg" }

/** The file's content in the bundle format: SVG text, or base64 for rasters. */
async function readAsset(f: File): Promise<{ ext: string; data: string }> {
  const ext = EXT[f.type] ?? f.name.split(".").pop()?.toLowerCase() ?? ""
  if (!["png", "gif", "webp", "svg"].includes(ext)) throw new Error("Use a PNG, GIF, WebP or SVG image.")
  if (ext === "svg") {
    if (f.size > MAX_SVG) throw new Error("SVG sprites can be at most 256 KB.")
    return { ext, data: await f.text() }
  }
  if (f.size > MAX_RASTER) throw new Error("Image sprites can be at most 1 MB.")
  const bytes = new Uint8Array(await f.arrayBuffer())
  let bin = ""
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return { ext, data: btoa(bin) }
}

/** Saves the theme with one sprite slot changed: a new file, or none. */
async function saveSlot(t: Theme, slot: SpriteState, file: File | null): Promise<Theme> {
  const b = await getJSON<ThemeBundle>(`/api/themes/${encodeURIComponent(t.id)}/export`)
  const assets = { ...(b.assets ?? {}) }
  const sprites = { ...(b.theme.art?.sprites ?? {}) }
  if (file) {
    const { ext, data } = await readAsset(file)
    const name = `sprite-${slot}.${ext}`
    // Another extension for the same slot is replaced, not kept beside it.
    for (const k of Object.keys(assets)) if (k.startsWith(`sprite-${slot}.`)) delete assets[k]
    assets[name] = data
    sprites[slot] = name
  } else {
    delete sprites[slot]
  }
  const art = { ...(b.theme.art ?? {}), sprites: Object.keys(sprites).length ? sprites : undefined }
  return sendJSON<Theme>("/api/themes", "POST", { theme: { ...b.theme, art, builtin: false }, assets })
}

/**
 * Settings › Appearance › Sprites: the eight state slots of the active
 * theme, each with the theme's picture or Lumi, the placeholder. Your own
 * themes take a picture per slot from a file; a built-in theme is copied
 * first.
 */
export function Sprites({ onDuplicate }: { onDuplicate: (t: Theme) => Promise<void> }) {
  const { active, reloadThemes } = usePrefs()
  const [busy, setBusy] = useState<string | null>(null)
  const input = useRef<HTMLInputElement>(null)
  const target = useRef<SpriteState | null>(null)
  if (!active) return null
  const own = !active.builtin
  const set = async (slot: SpriteState, f: File | null) => {
    setBusy(slot)
    try {
      await saveSlot(active, slot, f)
      await reloadThemes()
      toast.success(f ? `${slot} sprite saved` : `${slot} sprite removed`, { description: active.name })
    } catch (e) {
      toast.error("Could not save the sprite", { description: errorMessage(e) })
    } finally {
      setBusy(null)
    }
  }
  return (
    <Section
      id="sprites"
      title="Sprites"
      description={
        own
          ? `Pictures ${active.name} shows for what the app is doing. Drop in a PNG, GIF, WebP or SVG per slot; it is saved in the theme's folder. SVGs are cleaned of scripts and links.`
          : `${active.name} is built in, so it uses Lumi, the placeholder. Make an editable copy to give it sprites of your own.`
      }
      actions={
        !own ? (
          <Button variant="secondary" size="sm" onClick={() => void onDuplicate(active)}>
            <Copy /> Make an editable copy
          </Button>
        ) : undefined
      }
    >
      <input
        ref={input}
        type="file"
        accept="image/png,image/gif,image/webp,image/svg+xml,.png,.gif,.webp,.svg"
        className="hidden"
        data-testid="sprite-file"
        onChange={(e) => {
          const f = e.target.files?.[0]
          if (f && target.current) void set(target.current, f)
          e.target.value = ""
        }}
      />
      <div className="grid grid-cols-2 gap-3 border-t p-4 @2xl:grid-cols-4" data-testid="sprite-slots">
        {SPRITE_SLOTS.map(({ slot, label, where }, i) => {
          const file = active.art?.sprites?.[slot]
          return (
            <figure
              key={slot}
              style={{ "--i": i } as React.CSSProperties}
              className={cn("lb-rise flex flex-col items-center gap-2 rounded-lg border bg-background/40 p-3 text-center", busy === slot && "opacity-60")}
              data-slot={slot}
            >
              <div className="relative flex h-20 items-center justify-center">
                {file ? <StateSprite state={slot} className="size-16" /> : <Lumi state={slot} className="size-16" />}
              </div>
              <figcaption className="space-y-0.5">
                <div className="text-sm font-medium">{label}</div>
                <div className="text-2xs leading-snug text-muted-foreground">{where}</div>
                <div className="font-mono text-2xs text-subtle-foreground">{file ?? "placeholder"}</div>
              </figcaption>
              {own && (
                <div className="flex items-center gap-1">
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={busy !== null}
                    aria-label={`Upload a ${label.toLowerCase()} sprite`}
                    onClick={() => {
                      target.current = slot
                      input.current?.click()
                    }}
                  >
                    <Upload /> {file ? "Replace" : "Upload"}
                  </Button>
                  {file && (
                    <Button variant="ghost" size="icon-sm" disabled={busy !== null} aria-label={`Remove the ${label.toLowerCase()} sprite`} onClick={() => void set(slot, null)}>
                      <Trash2 />
                    </Button>
                  )}
                </div>
              )}
            </figure>
          )
        })}
      </div>
    </Section>
  )
}
