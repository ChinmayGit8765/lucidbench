import { CloudOff, ExternalLink, Link2, Rocket } from "lucide-react"

import { StatusPill } from "@/components/ui/badge"
import { providerLabel, STATUS_PILL, type CloudDeploy } from "@/lib/cloud"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"

/**
 * A project's deploy entries with their live status from the Cloud extension.
 * `live` is undefined while the inventory loads and null when the Cloud
 * extension is not added (the entries are listed without a status).
 */
export function DeployRows({ entries, live }: { entries: { provider: string; service: string; region?: string }[]; live: CloudDeploy[] | undefined | null }) {
  const now = useNow(30000)
  const link = "inline-flex size-5 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground [&_svg]:size-3"
  return (
    <div className="mt-3 border-t pt-3">
      <div className="mb-1.5 flex items-center gap-1.5 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
        <Rocket className="size-3" /> Deployed
      </div>
      <ul className="space-y-1">
        {entries.map((e, i) => {
          const d = live?.[i]
          const pill = d?.matched ? STATUS_PILL[d.status || "unknown"] : undefined
          return (
            <li key={`${e.provider}-${e.service}-${e.region ?? ""}-${i}`} className="flex items-center gap-2 text-sm">
              <span className="min-w-0 flex-1 truncate" title={d?.note}>
                <span className="font-mono text-xs">{e.service}</span>
                <span className="ml-1.5 text-2xs text-subtle-foreground">
                  {providerLabel(e.provider)}
                  {e.region ? ` · ${e.region}` : ""}
                </span>
              </span>
              {d?.matched && (d.updated || d.updated_text) && (
                <span className="shrink-0 text-2xs tabular-nums text-subtle-foreground" title={d.updated ? absoluteTime(d.updated) : undefined}>
                  {d.updated ? relativeTime(d.updated, now) : d.updated_text}
                </span>
              )}
              {pill ? (
                <StatusPill tone={pill.tone} pulse={pill.pulse}>
                  {pill.label}
                </StatusPill>
              ) : d ? (
                <span className="flex shrink-0 items-center gap-1 text-2xs text-subtle-foreground" title={d.note}>
                  <CloudOff className="size-3" /> not in inventory
                </span>
              ) : (
                <span className="shrink-0 text-2xs text-subtle-foreground">{live === undefined ? "checking…" : ""}</span>
              )}
              {d?.matched && (
                <span className="flex shrink-0 items-center">
                  {d.url && (
                    <a href={d.url} target="_blank" rel="noreferrer" className={link} title={d.url} aria-label={`Open ${e.service}`}>
                      <Link2 />
                    </a>
                  )}
                  {d.console && (
                    <a href={d.console} target="_blank" rel="noreferrer" className={link} title="Open in the console" aria-label={`Open ${e.service} in the console`}>
                      <ExternalLink />
                    </a>
                  )}
                </span>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
