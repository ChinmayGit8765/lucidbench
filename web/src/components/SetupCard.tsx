import type { ReactNode } from "react"
import { KeyRound, PlugZap } from "lucide-react"

import { StatusPill } from "@/components/ui/badge"
import { Card } from "@/components/ui/card"

/**
 * What a connector page shows before it has credentials: what is missing,
 * how to create it and where to put it. Values never appear here, only the
 * environment variable names the config points at.
 */
export function SetupCard({
  title,
  intro,
  steps,
  vars,
  note,
}: {
  title: string
  intro: ReactNode
  steps: ReactNode[]
  /** Variable names with whether each is set. */
  vars: { name: string; set: boolean }[]
  /** An extra callout, for example "an MCP server is not enough". */
  note?: ReactNode
}) {
  return (
    <Card className="relative overflow-hidden">
      <div aria-hidden className="tile-glow pointer-events-none absolute inset-0" />
      <div className="relative grid gap-6 p-6 @3xl:grid-cols-[minmax(0,1fr)_16rem]">
        <div className="min-w-0">
          <div className="flex items-center gap-2.5">
            <span className="flex size-9 items-center justify-center rounded-xl border border-border-strong bg-elevated text-brand shadow-card">
              <KeyRound className="size-4" />
            </span>
            <h2 className="text-base font-semibold tracking-tight">{title}</h2>
          </div>
          <p className="mt-3 max-w-xl text-sm text-muted-foreground">{intro}</p>
          {note && (
            <div className="mt-4 flex max-w-xl items-start gap-2.5 rounded-lg border border-info/30 bg-info-soft px-3 py-2.5 text-sm">
              <PlugZap className="mt-0.5 size-4 shrink-0 text-info-fg" />
              <div className="text-info-fg">{note}</div>
            </div>
          )}
          <ol className="mt-5 max-w-xl space-y-3">
            {steps.map((s, i) => (
              <li key={i} className="flex gap-3 text-sm">
                <span className="mt-px flex size-5 shrink-0 items-center justify-center rounded-full border bg-background/60 text-2xs font-semibold tabular-nums text-muted-foreground">{i + 1}</span>
                <span className="min-w-0 text-muted-foreground [&_code]:rounded [&_code]:border [&_code]:bg-background/60 [&_code]:px-1 [&_code]:py-px [&_code]:font-mono [&_code]:text-xs [&_code]:text-foreground">{s}</span>
              </li>
            ))}
          </ol>
        </div>
        <div className="rounded-lg border bg-background/50 p-3.5">
          <div className="text-xs font-medium text-muted-foreground">Environment</div>
          <ul className="mt-2.5 space-y-2">
            {vars.map((v) => (
              <li key={v.name} className="flex items-center justify-between gap-2">
                <span className="truncate font-mono text-xs">{v.name}</span>
                <StatusPill tone={v.set ? "success" : "warning"}>{v.set ? "set" : "not set"}</StatusPill>
              </li>
            ))}
          </ul>
          <p className="mt-3 text-2xs leading-relaxed text-subtle-foreground">Lucidbench reads the variable when it needs it and never stores, logs or shows the value.</p>
        </div>
      </div>
    </Card>
  )
}
