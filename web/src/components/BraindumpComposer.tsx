import { useEffect, useState } from "react"
import { ListChecks, Vote } from "lucide-react"

import { BraindumpParse } from "@/components/assistant/BraindumpParse"
import { Card } from "@/components/ui/card"
import { COMPOSE_EVENT, type BraindumpMode } from "@/lib/council"
import { cn } from "@/lib/utils"
import { Composer } from "@/pages/Council"

const MODES: { id: BraindumpMode; label: string; hint: string; icon: typeof Vote }[] = [
  { id: "brief", label: "Brief it", hint: "The council drafts a brief, two critics look for what would go wrong, and you approve it", icon: Vote },
  { id: "split", label: "Split into items", hint: "One call splits a long dump into items you keep as cards, ideas or briefs", icon: ListChecks },
]

/**
 * The one braindump composer. Every "New braindump" in the app (Home, Ideas,
 * the palette, the launcher, the n shortcut, the Assistant, the board)
 * opens this, at the top of Ideas. It either briefs the dump with the
 * council or splits it into items.
 */
export function BraindumpComposer({ initialMode = "brief", autoFocus = false }: { initialMode?: BraindumpMode; autoFocus?: boolean }) {
  const [mode, setMode] = useState<BraindumpMode>(initialMode)
  useEffect(() => setMode(initialMode), [initialMode])
  useEffect(() => {
    const on = (e: Event) => {
      const m = (e as CustomEvent<BraindumpMode | undefined>).detail
      if (m) setMode(m)
    }
    window.addEventListener(COMPOSE_EVENT, on)
    return () => window.removeEventListener(COMPOSE_EVENT, on)
  }, [])
  return (
    <section aria-label="New braindump" className="space-y-3" data-testid="braindump-composer" data-mode={mode}>
      <div role="radiogroup" aria-label="What to do with the braindump" className="inline-flex items-center gap-0.5 rounded-lg border bg-muted/40 p-0.5">
        {MODES.map((m) => {
          const on = m.id === mode
          return (
            <button
              key={m.id}
              role="radio"
              aria-checked={on}
              title={m.hint}
              onClick={() => setMode(m.id)}
              data-testid={`braindump-mode-${m.id}`}
              className={cn(
                "flex h-7 items-center gap-1.5 rounded-md px-3 text-xs transition-colors",
                on ? "bg-elevated font-medium text-foreground shadow-card" : "text-muted-foreground hover:text-foreground",
              )}
            >
              <m.icon className={cn("size-3.5", on ? "text-brand" : "text-subtle-foreground")} />
              {m.label}
            </button>
          )
        })}
      </div>
      {mode === "brief" ? (
        <Composer autoFocus={autoFocus} />
      ) : (
        <Card>
          <BraindumpParse className="p-5" />
        </Card>
      )}
    </section>
  )
}
