import type { CSSProperties } from "react"
import { siClaude, siCursor } from "simple-icons"

import { cn } from "@/lib/utils"

/*
 * Provider marks identify the service each account belongs to (nominative
 * use). Claude and Cursor come from simple-icons (CC0). simple-icons carries
 * no OpenAI or xAI icon, so those two are simplified monochrome drawings.
 * All marks are trademarks of their owners; see THIRD_PARTY_NOTICES.md.
 */

export type ProviderId = "claude" | "codex" | "grok" | "cursor"

export interface ProviderInfo {
  id: ProviderId
  label: string
  vendor: string
  /** The provider CLI's own sign-in command, for host accounts. */
  hostLogin?: string
}

export const PROVIDERS: ProviderInfo[] = [
  { id: "claude", label: "Claude", vendor: "Anthropic", hostLogin: "claude" },
  { id: "codex", label: "Codex", vendor: "OpenAI", hostLogin: "codex login" },
  { id: "grok", label: "Grok", vendor: "xAI", hostLogin: "grok login" },
  { id: "cursor", label: "Cursor", vendor: "Anysphere" },
]

export const providerInfo = (id: string): ProviderInfo | undefined => PROVIDERS.find((p) => p.id === id)

/** CSS variable holding the provider's tint colour. */
export const tintVar = (id: string) => (PROVIDERS.some((p) => p.id === id) ? `var(--tint-${id})` : "var(--neutral)")

function OpenAIMark() {
  // Six interlocking links arranged as a hexagonal rosette.
  return (
    <g fill="none" stroke="currentColor" strokeWidth="1.7">
      {[0, 60, 120, 180, 240, 300].map((a) => (
        <rect key={a} x="8.6" y="2.6" width="6.8" height="12.2" rx="3.4" transform={`rotate(${a} 12 12)`} />
      ))}
    </g>
  )
}

function XAIMark() {
  // A diagonal stroke crossing a broken counter-stroke.
  return (
    <g fill="currentColor">
      <path d="M17.9 3h2.9L8.1 21H5.2z" />
      <path d="M3.4 3h2.9l3.6 5.1-1.45 2.05z" />
      <path d="M13.85 14.95l1.45-2.05L20.8 21h-2.9z" />
    </g>
  )
}

export function ProviderMark({ provider, className }: { provider: string; className?: string }) {
  const label = providerInfo(provider)?.vendor ?? provider
  return (
    <svg viewBox="0 0 24 24" role="img" aria-label={label} className={cn("size-4 shrink-0", className)}>
      {provider === "claude" && <path fill="currentColor" d={siClaude.path} />}
      {provider === "cursor" && <path fill="currentColor" d={siCursor.path} />}
      {provider === "codex" && <OpenAIMark />}
      {provider === "grok" && <XAIMark />}
      {!providerInfo(provider) && <circle cx="12" cy="12" r="5" fill="currentColor" />}
    </svg>
  )
}

/** The mark on a small tinted tile. */
export function ProviderTile({
  provider,
  size = "md",
  muted = false,
  className,
}: {
  provider: string
  size?: "sm" | "md" | "lg"
  muted?: boolean
  className?: string
}) {
  const tint = tintVar(provider)
  const style: CSSProperties = muted
    ? {}
    : {
        color: tint,
        backgroundColor: `color-mix(in oklch, ${tint} 13%, transparent)`,
        borderColor: `color-mix(in oklch, ${tint} 24%, transparent)`,
      }
  return (
    <span
      style={style}
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-lg border",
        muted && "border-border bg-muted text-subtle-foreground",
        size === "sm" && "size-6 rounded-md [&_svg]:size-3.5",
        size === "md" && "size-8 [&_svg]:size-4",
        size === "lg" && "size-10 rounded-xl [&_svg]:size-5",
        className,
      )}
    >
      <ProviderMark provider={provider} />
    </span>
  )
}
