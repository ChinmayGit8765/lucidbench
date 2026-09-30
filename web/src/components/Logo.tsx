import { useId } from "react"

import { cn } from "@/lib/utils"

/**
 * The Lucidbench mark: a lens resting in the corner of an L-shaped bench, on a
 * tile that shifts from azure to violet like light through a prism. Built on a
 * 32px grid so it stays legible at 16px.
 */
export function LogoMark({ className, title }: { className?: string; title?: string }) {
  const id = useId().replace(/[^\w-]/g, "")
  const fill = `lb-fill-${id}`
  const shine = `lb-shine-${id}`
  return (
    <svg
      viewBox="0 0 32 32"
      className={cn("size-7 shrink-0", className)}
      role={title ? "img" : undefined}
      aria-hidden={title ? undefined : true}
      aria-label={title}
    >
      <defs>
        <linearGradient id={fill} x1="0" y1="0" x2="32" y2="32" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="oklch(0.74 0.14 225)" />
          <stop offset="0.55" stopColor="oklch(0.6 0.19 262)" />
          <stop offset="1" stopColor="oklch(0.55 0.2 298)" />
        </linearGradient>
        <linearGradient id={shine} x1="16" y1="0" x2="16" y2="32" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#fff" stopOpacity="0.28" />
          <stop offset="0.5" stopColor="#fff" stopOpacity="0" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="8" fill={`url(#${fill})`} />
      <rect width="32" height="32" rx="8" fill={`url(#${shine})`} />
      <rect x="0.5" y="0.5" width="31" height="31" rx="7.5" fill="none" stroke="#fff" strokeOpacity="0.18" />
      {/* bench: an L-shaped rest that doubles as the L of Lucidbench */}
      <path d="M8 8.5v15h16" fill="none" stroke="#fff" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
      {/* lens, with a glint */}
      <circle cx="18.5" cy="14" r="5" fill="#fff" fillOpacity="0.22" stroke="#fff" strokeWidth="2.4" />
      <path d="M16.4 12.6a2.6 2.6 0 0 1 1.6-1.4" fill="none" stroke="#fff" strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  )
}

/** Mark plus the "Lucidbench" wordmark. */
export function Wordmark({ collapsed = false }: { collapsed?: boolean }) {
  return (
    <span className="flex min-w-0 items-center gap-2.5">
      <LogoMark className="size-7" />
      {!collapsed && (
        <span className="truncate text-[15px] font-semibold tracking-[-0.02em]">
          Lucid<span className="text-muted-foreground">bench</span>
        </span>
      )}
    </span>
  )
}
