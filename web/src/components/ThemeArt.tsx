import { EyeOff, Sparkles } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { usePrefs } from "@/lib/prefs"
import { assetURL } from "@/lib/theme"

/**
 * The Overview sprite board: the active theme's sprites with captions. Shown
 * only when the theme has a board and the user switched it on.
 */
export function SpriteBoard() {
  const { active, prefs, update, label, inlineAssets } = usePrefs()
  const sprites = active?.art?.spriteBoard ?? []
  if (!active || sprites.length === 0 || !prefs.sprite_board) return null
  const inline = inlineAssets(active)
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between px-5 pb-3 pt-4">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Sparkles className="size-3.5 text-brand" />
          {label("sprite_board", "Sprite board")}
          <span className="font-normal text-subtle-foreground">· {active.name}</span>
        </h2>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Hide the sprite board"
          title="Hide the sprite board (Settings › Appearance brings it back)"
          onClick={() => update((p) => ({ ...p, sprite_board: false }))}
        >
          <EyeOff />
        </Button>
      </div>
      <div className="grid grid-cols-2 gap-3 border-t p-4 @2xl:grid-cols-4">
        {sprites.map((s, i) => (
          <figure
            key={`${s.file}-${i}`}
            className="group relative flex flex-col items-center gap-2 overflow-hidden rounded-lg border bg-background/40 px-3 pb-3 pt-4 transition-[border-color,transform] duration-200 hover:-translate-y-0.5 hover:border-border-strong"
          >
            <div aria-hidden className="pointer-events-none absolute inset-0 bg-[radial-gradient(closest-side,var(--brand-soft),transparent)] opacity-80" />
            <img src={assetURL(active, s.file, inline)} alt={s.caption ?? ""} className="theme-art relative aspect-square w-full max-w-28 object-contain" />
            {s.caption && <figcaption className="relative text-xs font-medium tracking-wide text-muted-foreground">{s.caption}</figcaption>}
          </figure>
        ))}
      </div>
    </Card>
  )
}

/** The active theme's empty-state illustration, if it has one. */
export function useEmptyArt(): string | null {
  const { active, inlineAssets } = usePrefs()
  const f = active?.art?.emptyState
  return active && f ? assetURL(active, f, inlineAssets(active)) : null
}
