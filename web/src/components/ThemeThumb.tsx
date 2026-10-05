import type { CSSProperties } from "react"

import { assetURL, scopedStyle, type Theme } from "@/lib/theme"
import { cn } from "@/lib/utils"

/**
 * A live miniature of the app drawn in a theme's own variables: sidebar,
 * header, two cards, a primary button and the status colours. It renders in
 * the theme's base whatever the page around it uses.
 */
export function ThemeThumb({ theme, inline, className }: { theme: Theme; inline?: Record<string, string>; className?: string }) {
  const banner = theme.art?.headerImage
  const mascot = theme.art?.sidebarMascot
  return (
    <div
      aria-hidden
      style={scopedStyle(theme) as CSSProperties}
      className={cn(
        theme.base === "dark" ? "lb-dark" : "lb-light",
        "app-backdrop relative aspect-[16/10] w-full overflow-hidden rounded-[calc(var(--radius)-2px)] border text-foreground",
        className,
      )}
    >
      <div className="absolute inset-y-0 left-0 flex w-[24%] flex-col gap-[6%] border-r bg-sidebar px-[4%] pt-[9%]">
        <span className="flex h-[7%] items-center gap-[6%]">
          <span className="aspect-square h-full rounded-[2px] bg-gradient-to-br from-brand to-[var(--brand-2)]" />
          <span className="h-[55%] flex-1 rounded-full bg-foreground/70" />
        </span>
        <span className="mt-[8%] h-[6%] rounded-[2px] bg-accent shadow-[inset_0_0_0_1px_var(--border)]">
          <span className="ml-[8%] mt-[9%] block h-[50%] w-[50%] rounded-full bg-brand" />
        </span>
        <span className="h-[4%] w-[70%] rounded-full bg-muted-foreground/40" />
        <span className="h-[4%] w-[60%] rounded-full bg-muted-foreground/40" />
        <span className="h-[4%] w-[66%] rounded-full bg-muted-foreground/30" />
        {mascot && <img src={assetURL(theme, mascot, inline)} alt="" className="theme-art mt-auto mb-[10%] aspect-square w-[60%] self-center object-contain" />}
      </div>
      <div className="absolute left-[24%] right-0 top-0 h-[13%] overflow-hidden border-b bg-background/60">
        {banner && <img src={assetURL(theme, banner, inline)} alt="" className="theme-art absolute inset-0 size-full object-cover opacity-30" />}
        <span className="absolute left-[5%] top-[38%] h-[26%] w-[22%] rounded-full bg-muted-foreground/50" />
      </div>
      <div className="absolute bottom-[8%] left-[29%] right-[5%] top-[20%] grid grid-cols-2 gap-[5%]">
        <div className="flex flex-col gap-[10%] rounded-[3px] border bg-card p-[6%] shadow-card">
          <span className="h-[9%] w-[45%] rounded-full bg-muted-foreground/45" />
          <span className="h-[14%] w-[40%] rounded-full bg-foreground/80" />
          <span className="mt-auto flex gap-[6%]">
            <span className="size-[10px] max-h-full rounded-full bg-success" />
            <span className="size-[10px] max-h-full rounded-full bg-warning" />
            <span className="size-[10px] max-h-full rounded-full bg-danger" />
          </span>
        </div>
        <div className="flex flex-col gap-[10%] rounded-[3px] border bg-card p-[6%] shadow-card">
          <span className="h-[9%] w-[55%] rounded-full bg-muted-foreground/45" />
          <span className="h-[10%] w-[80%] rounded-full bg-brand-soft" />
          <span className="mt-auto h-[18%] w-[50%] rounded-[3px] bg-primary" />
        </div>
      </div>
    </div>
  )
}
