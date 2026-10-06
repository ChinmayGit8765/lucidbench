import { useId } from "react"

import { usePrefs } from "@/lib/prefs"
import { assetURL } from "@/lib/theme"
import { cn } from "@/lib/utils"

/**
 * The state sprites. A theme may set a picture for each slot
 * (theme.json `art.sprites`); every other theme gets Lucidbench's own
 * placeholder: Lumi, a small lens-bot drawn here in SVG and coloured by the
 * active theme's tokens.
 */
export type SpriteState = "idle" | "loading" | "working" | "thinking" | "success" | "failure" | "sleeping" | "empty" | "celebrate"

/** The slots a theme can fill, in the order Settings › Appearance lists them. */
export const SPRITE_SLOTS: { slot: Exclude<SpriteState, "idle">; label: string; where: string }[] = [
  { slot: "loading", label: "Loading", where: "Loading placeholders" },
  { slot: "working", label: "Working", where: "A Work session running; the mascot while one runs" },
  { slot: "thinking", label: "Thinking", where: "The Council deliberating" },
  { slot: "success", label: "Success", where: "A CI run that passed" },
  { slot: "failure", label: "Failure", where: "A CI run that failed" },
  { slot: "sleeping", label: "Sleeping", where: "Infrastructure asleep" },
  { slot: "empty", label: "Empty", where: "Empty boards and lists" },
  { slot: "celebrate", label: "Celebrate", where: "A card reaching Done, a PR merged" },
]

const LABEL: Record<SpriteState, string> = {
  idle: "Lumi is idle",
  loading: "Loading",
  working: "Working",
  thinking: "Thinking",
  success: "Passed",
  failure: "Failed",
  sleeping: "Asleep",
  empty: "Nothing here yet",
  celebrate: "Celebrating",
}

/** What a state sprite means, for labels and tooltips. */
export const spriteLabel = (s: SpriteState) => LABEL[s]

/** The theme's file for a state, if it has one (idle falls back to the sidebar mascot). */
export function useThemeSprite(state: SpriteState): string | null {
  const { active, inlineAssets } = usePrefs()
  if (!active) return null
  const file = state === "idle" ? active.art?.sidebarMascot : active.art?.sprites?.[state]
  return file ? assetURL(active, file, inlineAssets(active)) : null
}

/**
 * A state sprite: the theme's picture when it has one, else Lumi. Decorative
 * unless a label is given. Animations stop under reduced motion.
 */
export function StateSprite({
  state,
  className,
  label,
  placeholderOnly,
}: {
  state: SpriteState
  className?: string
  /** Read out to screen readers; omit for decoration. */
  label?: string
  /** Always draw Lumi (Settings shows the placeholder next to the theme's file). */
  placeholderOnly?: boolean
}) {
  const src = useThemeSprite(state)
  if (src && !placeholderOnly) {
    return (
      <img
        src={src}
        alt={label ?? ""}
        aria-hidden={label ? undefined : true}
        data-sprite={state}
        className={cn("theme-art size-16 object-contain", className)}
      />
    )
  }
  return <Lumi state={state} className={className} label={label} />
}

/** Lumi, the placeholder sprite: a round lens-bot with a little antenna. */
export function Lumi({ state, className, label }: { state: SpriteState; className?: string; label?: string }) {
  const bad = state === "failure"
  const good = state === "success" || state === "celebrate"
  const body = bad ? "var(--danger)" : good ? "var(--success)" : "var(--brand)"
  const shine = `lumi-shine-${useId().replace(/[^\w-]/g, "")}`
  return (
    <svg
      viewBox="0 0 64 64"
      role={label ? "img" : undefined}
      aria-label={label || undefined}
      aria-hidden={label ? undefined : true}
      data-sprite={state}
      data-placeholder="lumi"
      className={cn("lumi size-16 shrink-0 overflow-visible", `lumi-${state}`, className)}
    >
      {/* shadow */}
      <ellipse cx="32" cy="58" rx="14" ry="2.6" fill="var(--foreground)" opacity="0.12" className="lumi-shadow" />
      <g className="lumi-body">
        {/* antenna */}
        <line x1="32" y1="14" x2="32" y2="7" stroke="var(--border-strong)" strokeWidth="2" strokeLinecap="round" />
        <circle cx="32" cy="6" r="3" fill={state === "sleeping" ? "var(--neutral)" : body} className="lumi-bulb" />
        {/* arms */}
        {state === "celebrate" ? (
          <>
            <path d="M14 32 L7 20" stroke={body} strokeWidth="3.5" strokeLinecap="round" className="lumi-arm-l" />
            <path d="M50 32 L57 20" stroke={body} strokeWidth="3.5" strokeLinecap="round" className="lumi-arm-r" />
          </>
        ) : state === "working" ? (
          <>
            <path d="M14 36 L8 42" stroke={body} strokeWidth="3.5" strokeLinecap="round" />
            <path d="M50 36 L57 33" stroke={body} strokeWidth="3.5" strokeLinecap="round" className="lumi-arm-r" />
            <rect x="55" y="27" width="6" height="8" rx="1.5" fill="var(--warning)" className="lumi-tool" />
          </>
        ) : (
          <>
            <path d="M14 37 L9 43" stroke={body} strokeWidth="3.5" strokeLinecap="round" opacity="0.85" />
            <path d="M50 37 L55 43" stroke={body} strokeWidth="3.5" strokeLinecap="round" opacity="0.85" />
          </>
        )}
        {/* body: a lens */}
        <circle cx="32" cy="34" r="19" fill={body} />
        <circle cx="32" cy="34" r="19" fill={`url(#${shine})`} />
        <circle cx="32" cy="34" r="13.5" fill="var(--elevated)" stroke="var(--foreground)" strokeOpacity="0.1" />
        <defs>
          <linearGradient id={shine} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor="#fff" stopOpacity="0.35" />
            <stop offset="0.55" stopColor="#fff" stopOpacity="0" />
          </linearGradient>
        </defs>
        <Face state={state} />
        {/* glint */}
        <path d="M22.5 27.5a10 10 0 0 1 5-4" fill="none" stroke="#fff" strokeOpacity="0.55" strokeWidth="1.6" strokeLinecap="round" />
      </g>
      <Extras state={state} />
    </svg>
  )
}

function Face({ state }: { state: SpriteState }) {
  const ink = "var(--foreground)"
  const eye = (cx: number) => <circle cx={cx} cy="33" r="2.2" fill={ink} />
  switch (state) {
    case "sleeping":
      return (
        <g stroke={ink} strokeWidth="1.8" strokeLinecap="round" fill="none">
          <path d="M24.5 33.5q2.5 2 5 0" />
          <path d="M34.5 33.5q2.5 2 5 0" />
        </g>
      )
    case "failure":
      return (
        <g stroke={ink} strokeWidth="1.8" strokeLinecap="round">
          <path d="M25 31l4 4M29 31l-4 4" />
          <path d="M35 31l4 4M39 31l-4 4" />
          <path d="M27.5 41q4.5-3 9 0" fill="none" />
        </g>
      )
    case "success":
    case "celebrate":
      return (
        <g stroke={ink} strokeWidth="1.8" strokeLinecap="round" fill="none">
          <path d="M24.5 33.5q2.5-3 5 0" />
          <path d="M34.5 33.5q2.5-3 5 0" />
          <path d="M27 38.5q5 4.5 10 0" />
        </g>
      )
    case "thinking":
      return (
        <g className="lumi-eyes">
          <circle cx="28.5" cy="31" r="2.2" fill={ink} />
          <circle cx="37.5" cy="31" r="2.2" fill={ink} />
          <path d="M29.5 40h5" stroke={ink} strokeWidth="1.8" strokeLinecap="round" />
        </g>
      )
    case "working":
      return (
        <g>
          <path d="M24.5 32.5h5M34.5 32.5h5" stroke={ink} strokeWidth="2.2" strokeLinecap="round" />
          <path d="M29 39.5q3 1.5 6 0" stroke={ink} strokeWidth="1.8" strokeLinecap="round" fill="none" />
        </g>
      )
    case "empty":
      return (
        <g>
          {eye(27)}
          {eye(37)}
          <circle cx="32" cy="40" r="1.6" fill="none" stroke={ink} strokeWidth="1.5" />
        </g>
      )
    case "loading":
      return (
        <g className="lumi-eyes lumi-look">
          {eye(27)}
          {eye(37)}
          <path d="M29 39.5h6" stroke={ink} strokeWidth="1.8" strokeLinecap="round" />
        </g>
      )
    default:
      return (
        <g className="lumi-eyes lumi-blink">
          {eye(27)}
          {eye(37)}
          <path d="M28.5 39q3.5 2.5 7 0" stroke={ink} strokeWidth="1.8" strokeLinecap="round" fill="none" />
        </g>
      )
  }
}

function Extras({ state }: { state: SpriteState }) {
  switch (state) {
    case "thinking":
      return (
        <g fill="var(--muted-foreground)">
          <circle cx="50" cy="12" r="2" className="lumi-dot lumi-dot-1" />
          <circle cx="56" cy="8" r="2.4" className="lumi-dot lumi-dot-2" />
          <circle cx="62" cy="3" r="2.8" className="lumi-dot lumi-dot-3" />
        </g>
      )
    case "sleeping":
      return (
        <g fill="var(--muted-foreground)" fontFamily="var(--font-sans)" fontWeight="700">
          <text x="47" y="16" fontSize="9" className="lumi-z lumi-z-1">
            z
          </text>
          <text x="53" y="8" fontSize="7" className="lumi-z lumi-z-2">
            z
          </text>
        </g>
      )
    case "success":
      return (
        <g>
          <circle cx="52" cy="16" r="7" fill="var(--success)" />
          <path d="M48.8 16.2l2.2 2.2 4.2-4.6" stroke="#fff" strokeWidth="2" fill="none" strokeLinecap="round" strokeLinejoin="round" />
        </g>
      )
    case "failure":
      return (
        <g>
          <circle cx="52" cy="16" r="7" fill="var(--danger)" />
          <path d="M52 12.5v4.5M52 19.6v.1" stroke="#fff" strokeWidth="2.2" strokeLinecap="round" />
        </g>
      )
    case "celebrate":
      return (
        <g className="lumi-confetti">
          <rect x="6" y="6" width="4" height="4" rx="1" fill="var(--warning)" transform="rotate(20 8 8)" />
          <rect x="54" y="4" width="4" height="4" rx="1" fill="var(--brand-2)" transform="rotate(-15 56 6)" />
          <circle cx="16" cy="2" r="2" fill="var(--info)" />
          <circle cx="47" cy="12" r="1.8" fill="var(--danger)" />
          <rect x="60" y="16" width="3" height="5" rx="1" fill="var(--success)" />
        </g>
      )
    case "empty":
      return <ellipse cx="32" cy="58" rx="20" ry="3.4" fill="none" stroke="var(--border-strong)" strokeDasharray="3 3" />
    default:
      return null
  }
}
