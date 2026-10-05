import { lazy } from "react"
import { Plug } from "lucide-react"

import { ProviderTile } from "@/components/ProviderMark"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { MCP_POLL_MS, type McpMatrix } from "@/lib/mcp"
import type { ModuleDef } from "@/modules/types"

function McpTile() {
  const { open } = useApp()
  const poll = usePoll<McpMatrix>("/api/mcp", MCP_POLL_MS)
  const m = poll.data
  const servers = m?.servers ?? []
  const clients = (m?.clients ?? []).filter((c) => c.config_found)
  const shared = servers.filter((s) => s.providers.length + s.inherited.length > 1).length
  return (
    <StatTile
      icon={Plug}
      label="MCP servers"
      onOpen={() => open("mcp")}
      loading={poll.loading && !m}
      value={servers.length}
      aside={shared > 0 ? <StatusPill tone="info">{shared} shared</StatusPill> : undefined}
      sub={`reachable from ${clients.length} ${clients.length === 1 ? "client" : "clients"}`}
      footer={
        <div className="flex flex-wrap gap-1.5">
          {(m?.clients ?? []).map((c) => (
            <span key={c.provider} className="flex items-center gap-1 text-xs tabular-nums text-muted-foreground" title={`${c.servers.length} servers`}>
              <ProviderTile provider={c.provider} size="sm" muted={!c.config_found || c.servers.length === 0} />
              {c.servers.length}
            </span>
          ))}
        </div>
      }
    />
  )
}

export const mcp: ModuleDef = {
  id: "mcp",
  title: "MCP servers",
  icon: Plug,
  route: "/mcp",
  section: "ai",
  kind: "core",
  order: 1,
  defaultEnabled: true,
  description: "Which MCP servers each AI client can reach.",
  keywords: "servers model context protocol subscriptions access matrix",
  component: lazy(() => import("@/pages/Mcp")),
  overviewTile: McpTile,
}
