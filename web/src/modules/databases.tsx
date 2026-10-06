import { lazy } from "react"
import { ArrowRight, Database, DatabaseZap } from "lucide-react"

import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useApp } from "@/lib/app"
import { displayName, healthCounts, useDatabases, useDatabasesAdded } from "@/lib/databases"
import type { AttentionItem, ModuleDef } from "@/modules/types"

function DatabasesTile() {
  const { open } = useApp()
  const poll = useDatabases(true)
  const d = poll.data
  const c = d ? healthCounts(d.saved) : null
  return (
    <StatTile
      icon={Database}
      label="Databases"
      onOpen={() => open("databases")}
      loading={poll.loading && !d && !poll.error}
      value={
        c && c.total > 0 ? (
          <span>
            {c.up}
            <span className="text-base font-normal text-subtle-foreground">/{c.total}</span>
          </span>
        ) : (
          <span className="text-base font-medium text-muted-foreground">{c ? "None saved" : "Unavailable"}</span>
        )
      }
      aside={c && c.total > 0 ? c.down.length > 0 ? <StatusPill tone="danger">{c.down.length} down</StatusPill> : <StatusPill tone="success">all healthy</StatusPill> : undefined}
      sub={
        d && c
          ? c.total > 0
            ? `healthy connections · ${d.discovered.length} database ${d.discovered.length === 1 ? "container" : "containers"} found`
            : `${d.discovered.length} database ${d.discovered.length === 1 ? "container" : "containers"} found, none saved`
          : poll.error?.message
      }
      footer={
        c && c.down.length > 0 ? (
          <div className="space-y-1">
            {c.down.slice(0, 3).map((s) => (
              <div key={s.id} className="flex items-center gap-2 text-xs text-muted-foreground">
                <span className="size-1.5 shrink-0 rounded-full bg-danger" />
                <span className="truncate font-mono">{displayName(s)}</span>
              </div>
            ))}
          </div>
        ) : undefined
      }
    />
  )
}

/** Needs attention: a saved connection that does not answer. */
function useDatabaseAttention(): AttentionItem[] | null {
  const { open } = useApp()
  const added = useDatabasesAdded()
  const poll = useDatabases(added)
  if (!added) return []
  if (!poll.data && !poll.error) return null
  return healthCountsDown(poll.data?.saved ?? []).map((s) => ({
    key: `databases-${s.id}`,
    severity: "danger" as const,
    icon: <DatabaseZap className="size-4" />,
    title: (
      <>
        <span className="font-mono text-[0.92em]">{displayName(s)}</span> is not answering
      </>
    ),
    meta: <>{s.health?.error ?? "No answer"}</>,
    action: (
      <Button variant="ghost" size="sm" onClick={() => open("databases", [s.id])}>
        View <ArrowRight />
      </Button>
    ),
  }))
}

const healthCountsDown = (saved: Parameters<typeof healthCounts>[0]) => healthCounts(saved).down

export const databases: ModuleDef = {
  id: "databases",
  title: "Databases",
  icon: Database,
  route: "/databases",
  section: "infrastructure",
  kind: "extension",
  order: 11,
  defaultEnabled: false,
  category: "data",
  description: "Postgres, MySQL, Redis and Mongo on this machine: discover containers, check health, browse tables and run read-only queries.",
  requires: { docker: true },
  keywords: "postgres postgresql mysql mariadb redis mongo sql cache schema query pgweb",
  component: lazy(() => import("@/pages/Databases")),
  overviewTile: DatabasesTile,
  useAttention: useDatabaseAttention,
}
