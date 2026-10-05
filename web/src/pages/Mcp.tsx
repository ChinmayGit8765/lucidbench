import { Check, FolderOpen, Plug, ShieldCheck } from "lucide-react"

import { ProviderMark, ProviderTile, tintVar } from "@/components/ProviderMark"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Card } from "@/components/ui/card"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import { clientLabel, MCP_POLL_MS, projectScope, type McpClient, type McpMatrix, type McpRow } from "@/lib/mcp"
import { cn } from "@/lib/utils"
import type { Account } from "@/pages/Accounts"

function TransportChip({ t }: { t: string }) {
  return <Badge className="font-mono uppercase tracking-wide">{t}</Badge>
}

/** One matrix cell: configured, configured in a project only, loaded through another client, or absent. */
function Cell({ row, client, m }: { row: McpRow; client: McpClient; m: McpMatrix }) {
  const tint = tintVar(client.provider)
  const mine = client.servers.filter((s) => s.name.toLowerCase() === row.name.toLowerCase())
  if (mine.length > 0) {
    const projects = mine.map((s) => projectScope(s.scope)).filter((x): x is string => x !== null)
    const projectOnly = projects.length === mine.length
    return (
      <span
        title={projectOnly ? `${clientLabel(client.provider)}: only in project ${projects.join(", ")}` : `${clientLabel(client.provider)}: configured`}
        className="inline-flex flex-col items-center gap-0.5"
      >
        <span
          style={{ color: tint, backgroundColor: `color-mix(in oklch, ${tint} 15%, transparent)` }}
          className="flex size-6 items-center justify-center rounded-full"
        >
          <Check className="size-3.5" strokeWidth={2.5} />
        </span>
        {projectOnly && (
          <span className="max-w-24 truncate text-2xs leading-3 text-subtle-foreground">
            {projects[0] === "~" ? "home project" : projects[0]}
          </span>
        )}
      </span>
    )
  }
  if (row.inherited.includes(client.provider)) {
    const via = (client.inherits ?? []).filter((p) => m.clients.find((c) => c.provider === p)?.servers.some((s) => s.name.toLowerCase() === row.name.toLowerCase()))
    return (
      <span
        title={`${clientLabel(client.provider)} loads this from the ${via.map(clientLabel).join(" and ")} config`}
        className="inline-flex flex-col items-center gap-0.5"
      >
        <span style={{ color: tint }} className="flex size-6 items-center justify-center rounded-full border border-dashed border-current/50">
          <Check className="size-3" />
        </span>
        <span className="text-2xs leading-3 text-subtle-foreground">via {via.map((p) => clientLabel(p).split(" ")[0]).join(", ")}</span>
      </span>
    )
  }
  return <span aria-label="not configured" className="mx-auto block size-1 rounded-full bg-border-strong" />
}

function MatrixCard({ m, signedIn }: { m: McpMatrix; signedIn: Set<string> }) {
  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 px-5 pb-3 pt-4">
        <div>
          <h2 className="text-sm font-semibold">Access matrix</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">Which subscription reaches which server. Shared servers first.</p>
        </div>
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="flex size-4 items-center justify-center rounded-full bg-brand-soft text-brand">
              <Check className="size-2.5" strokeWidth={3} />
            </span>
            configured
          </span>
          <span className="flex items-center gap-1.5">
            <span className="flex size-4 items-center justify-center rounded-full border border-dashed border-brand/60 text-brand">
              <Check className="size-2.5" />
            </span>
            loaded from another client
          </span>
        </div>
      </div>
      {m.servers.length === 0 ? (
        <div className="border-t">
          <EmptyState icon={<Plug />} title="No MCP servers configured" description="Add a server to Claude Code, Codex, Grok or Cursor and it shows up here." />
        </div>
      ) : (
        <div className="overflow-x-auto border-t">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/30 text-left">
                <th scope="col" className="px-5 py-2.5 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
                  Server
                </th>
                {m.clients.map((c) => (
                  <th key={c.provider} scope="col" className="w-28 px-2 py-2 text-center font-normal">
                    <span className="inline-flex flex-col items-center gap-1">
                      <span className="relative">
                        <ProviderTile provider={c.provider} size="sm" muted={!c.config_found} />
                        {signedIn.has(c.provider) && (
                          <span title="Signed in" className="absolute -bottom-0.5 -right-0.5 size-2 rounded-full border-2 border-card bg-success" />
                        )}
                      </span>
                      <span className="text-xs font-medium">{clientLabel(c.provider)}</span>
                    </span>
                  </th>
                ))}
                <th scope="col" className="px-3 py-2.5 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
                  Transport
                </th>
                <th scope="col" className="px-5 py-2.5 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
                  Host
                </th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {m.servers.map((r) => {
                const reach = r.providers.length + r.inherited.length
                return (
                  <tr key={r.name} className="h-14 transition-colors hover:bg-accent/30">
                    <th scope="row" className="px-5 py-2 text-left font-normal">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-sm font-medium">{r.name}</span>
                        {reach === m.clients.length && m.clients.length > 1 && (
                          <span className="rounded bg-success-soft px-1 text-2xs text-success-fg">everywhere</span>
                        )}
                      </div>
                    </th>
                    {m.clients.map((c) => (
                      <td key={c.provider} className="px-2 py-2 text-center align-middle">
                        <Cell row={r} client={c} m={m} />
                      </td>
                    ))}
                    <td className="px-3 py-2">
                      <div className="flex gap-1">
                        {r.transports.map((t) => (
                          <TransportChip key={t} t={t} />
                        ))}
                      </div>
                    </td>
                    <td className="max-w-56 truncate px-5 py-2 font-mono text-xs text-muted-foreground" title={r.hosts.join(", ")}>
                      {r.hosts.length ? r.hosts.join(", ") : <span className="text-subtle-foreground">local process</span>}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}

function ClientCard({ c }: { c: McpClient }) {
  const user = c.servers.filter((s) => s.scope === "user")
  const project = c.servers.filter((s) => s.scope !== "user")
  return (
    <Card className="flex flex-col overflow-hidden">
      <div className="flex items-start gap-3 p-4 pb-3">
        <ProviderTile provider={c.provider} muted={!c.config_found} />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">{clientLabel(c.provider)}</div>
          <div className="truncate font-mono text-2xs text-subtle-foreground" title={c.config_hint}>
            {c.config_hint}
          </div>
        </div>
        {c.error ? (
          <StatusPill tone="danger">{c.error}</StatusPill>
        ) : !c.config_found ? (
          <StatusPill tone="neutral">No config</StatusPill>
        ) : (
          <StatusPill tone={c.servers.length ? "success" : "neutral"}>
            {c.servers.length} {c.servers.length === 1 ? "server" : "servers"}
          </StatusPill>
        )}
      </div>
      {(c.inherits ?? []).length > 0 && (
        <div className="mx-4 mb-3 flex items-center gap-2 rounded-md border border-dashed px-2.5 py-1.5 text-xs text-muted-foreground">
          <span className="flex -space-x-1">
            {c.inherits!.map((p) => (
              <ProviderTile key={p} provider={p} size="sm" className="size-5 rounded-md ring-2 ring-card [&_svg]:size-3" />
            ))}
          </span>
          Also loads the {c.inherits!.map(clientLabel).join(" and ")} servers
        </div>
      )}
      {c.servers.length === 0 ? (
        <p className="mt-auto border-t px-4 py-4 text-xs text-muted-foreground">
          {c.config_found ? "No MCP servers in this config." : "Config file not found. Nothing to read."}
        </p>
      ) : (
        <ul className="divide-y border-t">
          {[...user, ...project].map((s) => {
            const proj = projectScope(s.scope)
            return (
              <li key={`${s.scope}/${s.name}`} className="flex items-center gap-2.5 px-4 py-2">
                <span className="min-w-0 flex-1 truncate font-mono text-xs font-medium">{s.name}</span>
                {proj && (
                  <Badge title={`Only in the ${proj} project`}>
                    <FolderOpen /> {proj === "~" ? "home project" : proj}
                  </Badge>
                )}
                <span className="max-w-40 truncate font-mono text-2xs text-subtle-foreground">{s.host ?? ""}</span>
                <TransportChip t={s.transport} />
              </li>
            )
          })}
        </ul>
      )}
    </Card>
  )
}

export default function Mcp() {
  const poll = usePoll<McpMatrix>("/api/mcp", MCP_POLL_MS)
  const accounts = usePoll<Account[]>("/api/accounts", 30000)
  const m = poll.data
  const signedIn = new Set((accounts.data ?? []).filter((a) => a.status === "logged_in").map((a) => a.provider))

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<Plug />}
        title="MCP servers"
        description="Which MCP servers each of your AI subscriptions can reach, from each client's own config."
        actions={<RefreshButton refreshing={poll.refreshing || accounts.refreshing} updatedAt={poll.updatedAt} />}
      />

      <div className="flex items-start gap-3 rounded-lg border bg-muted/30 px-4 py-3">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-success" />
        <p className="text-sm text-muted-foreground">
          <span className="font-medium text-foreground">Names only.</span> Lucidbench reads each server's name, transport and host. Env, headers,
          arguments, commands, tokens and full URLs are never read out, stored or sent anywhere. Project-scoped servers show the folder name only.
        </p>
      </div>

      {poll.loading && !m && (
        <div className="space-y-4" aria-busy="true" aria-label="Reading MCP configs">
          <Skeleton className="h-64 rounded-xl" />
          <div className="grid gap-3 @3xl:grid-cols-2">
            <Skeleton className="h-40 rounded-xl" />
            <Skeleton className="h-40 rounded-xl" />
          </div>
        </div>
      )}
      {poll.error && !m && <ErrorState title="Could not read MCP configs" message={poll.error.message} onRetry={poll.refresh} />}

      {m && m.clients.length === 0 && (
        <Card className="border-dashed">
          <EmptyState icon={<Plug />} title="Every provider is disabled" description="Enable a provider in your config to see its MCP servers." />
        </Card>
      )}

      {m && m.clients.length > 0 && (
        <>
          <MatrixCard m={m} signedIn={signedIn} />
          <section className="space-y-3">
            <div className="flex items-center gap-2.5">
              <h2 className="text-sm font-semibold">By client</h2>
              <span className="text-sm text-subtle-foreground">including project-scoped servers</span>
            </div>
            <div className="grid gap-3 @3xl:grid-cols-2">
              {m.clients.map((c) => (
                <ClientCard key={c.provider} c={c} />
              ))}
            </div>
          </section>
          <p className={cn("flex items-center gap-1.5 text-xs text-subtle-foreground")}>
            <ProviderMark provider="claude" className="size-3" />
            Claude Code servers come from your user config and per-project settings; Codex and Grok from config.toml; Cursor from mcp.json.
          </p>
        </>
      )}
    </div>
  )
}
