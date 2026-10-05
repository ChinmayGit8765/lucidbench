/* Mirrors internal/mcp: keep these in step with the Go structs. */

export type Transport = "stdio" | "http" | "sse"

export interface McpServer {
  name: string
  transport: Transport
  host?: string
  /** "user", or "project:<folder>" for a Claude Code project. */
  scope: string
}

export interface McpClient {
  provider: string
  config_found: boolean
  config_hint: string
  servers: McpServer[]
  inherits?: string[]
  error?: string
}

export interface McpRow {
  name: string
  providers: string[]
  inherited: string[]
  transports: Transport[]
  hosts: string[]
}

export interface McpMatrix {
  clients: McpClient[]
  servers: McpRow[]
}

export const MCP_POLL_MS = 30000

/** The client's display name; Claude here is the Claude Code CLI. */
export const clientLabel = (p: string) =>
  ({ claude: "Claude Code", codex: "Codex", grok: "Grok", cursor: "Cursor" })[p] ?? p

export const projectScope = (scope: string) => (scope.startsWith("project:") ? scope.slice("project:".length) : null)

/** Server names a client can reach: its own (user scope first), then inherited ones. */
export function serversFor(m: McpMatrix, provider: string): { name: string; via?: string; project?: string }[] {
  const out: { name: string; via?: string; project?: string }[] = []
  const seen = new Set<string>()
  const c = m.clients.find((x) => x.provider === provider)
  for (const s of c?.servers ?? []) {
    const k = s.name.toLowerCase()
    if (seen.has(k)) continue
    seen.add(k)
    out.push({ name: s.name, project: projectScope(s.scope) ?? undefined })
  }
  for (const from of c?.inherits ?? []) {
    for (const s of m.clients.find((x) => x.provider === from)?.servers ?? []) {
      const k = s.name.toLowerCase()
      if (s.scope !== "user" || seen.has(k)) continue
      seen.add(k)
      out.push({ name: s.name, via: from })
    }
  }
  return out
}
