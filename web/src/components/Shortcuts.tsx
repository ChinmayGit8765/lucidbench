import { useEffect, useRef } from "react"

import { Dialog } from "@/components/ui/dialog"
import { isMac } from "@/lib/utils"

/** "g" then a letter: where it goes. */
export const GO_KEYS: { key: string; module: string; label: string }[] = [
  { key: "o", module: "overview", label: "Overview" },
  { key: "w", module: "work", label: "Work" },
  { key: "b", module: "boards", label: "Boards" },
  { key: "m", module: "memory", label: "Memory" },
  { key: "c", module: "council", label: "Council" },
  { key: "i", module: "ideas", label: "Ideas" },
  { key: "s", module: "settings", label: "Settings" },
]

/** True while the user is typing somewhere: shortcuts stay out of the way. */
function typing(e: KeyboardEvent): boolean {
  const t = e.target as HTMLElement | null
  if (!t) return false
  if (t.isContentEditable || t.closest("[contenteditable='true'], [contenteditable='']")) return true
  const tag = t.tagName
  if (tag === "TEXTAREA" || tag === "SELECT") return true
  if (tag === "INPUT") {
    const type = (t as HTMLInputElement).type
    return !["checkbox", "radio", "button", "submit", "range", "color", "file"].includes(type)
  }
  return false
}

/** A dialog or sheet is open: its own keys win. */
const modalOpen = () => !!document.querySelector("[aria-modal='true']")

/**
 * The single-key shortcuts: ? for the cheat sheet, / to search, n for a new
 * braindump, and g then a letter to go somewhere. Ignored while typing, with
 * a modifier held, or while a dialog is open.
 */
export function useShortcuts({
  onHelp,
  onSearch,
  onNew,
  onGo,
  enabled = true,
}: {
  onHelp: () => void
  onSearch: () => void
  onNew: () => void
  onGo: (module: string) => void
  enabled?: boolean
}) {
  const pendingG = useRef(0)
  const handlers = useRef({ onHelp, onSearch, onNew, onGo })
  handlers.current = { onHelp, onSearch, onNew, onGo }
  useEffect(() => {
    if (!enabled) return
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || typing(e) || modalOpen()) return
      const h = handlers.current
      const k = e.key
      if (Date.now() - pendingG.current < 1200) {
        pendingG.current = 0
        const go = GO_KEYS.find((g) => g.key === k.toLowerCase())
        if (go) {
          e.preventDefault()
          h.onGo(go.module)
        }
        return
      }
      if (k === "?") {
        e.preventDefault()
        h.onHelp()
      } else if (k === "/") {
        e.preventDefault()
        h.onSearch()
      } else if (k === "n" || k === "N") {
        e.preventDefault()
        h.onNew()
      } else if (k === "g" || k === "G") {
        pendingG.current = Date.now()
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [enabled])
}

function Keys({ keys }: { keys: string[] }) {
  return (
    <span className="flex items-center gap-1">
      {keys.map((k, i) => (
        <span key={i} className="flex items-center gap-1">
          {i > 0 && <span className="text-2xs text-subtle-foreground">then</span>}
          <kbd className="min-w-6 rounded-md border border-border-strong bg-background px-1.5 py-0.5 text-center font-sans text-xs shadow-card">{k}</kbd>
        </span>
      ))}
    </span>
  )
}

/** The cheat sheet behind "?". */
export function ShortcutSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const mod = isMac() ? "⌘" : "Ctrl"
  const general: [string[], string][] = [
    [["?"], "This list"],
    [[`${mod} K`], "Command palette"],
    [["/"], "Search everything"],
    [["n"], "New braindump"],
    [["Esc"], "Close a dialog or panel"],
  ]
  return (
    <Dialog open={open} onClose={onClose} title="Keyboard shortcuts" description="Single keys work anywhere you are not typing.">
      <div className="grid gap-5 sm:grid-cols-2" data-testid="shortcuts">
        <section>
          <h3 className="mb-2 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">General</h3>
          <ul className="space-y-2">
            {general.map(([keys, label]) => (
              <li key={label} className="flex items-center justify-between gap-3 text-sm">
                <span className="text-muted-foreground">{label}</span>
                <Keys keys={keys} />
              </li>
            ))}
          </ul>
        </section>
        <section>
          <h3 className="mb-2 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Go to</h3>
          <ul className="space-y-2">
            {GO_KEYS.map((g) => (
              <li key={g.key} className="flex items-center justify-between gap-3 text-sm">
                <span className="text-muted-foreground">{g.label}</span>
                <Keys keys={["g", g.key]} />
              </li>
            ))}
          </ul>
        </section>
      </div>
    </Dialog>
  )
}
