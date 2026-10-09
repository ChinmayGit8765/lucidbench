/*
 * In-test HTTP fakes for the outside APIs (Stripe, Linear, Trello): a node
 * http server on an ephemeral loopback port that logs every request and lets
 * the spec answer it. The daemon is pointed at it through its documented
 * api_url overrides (LUCID_INTEGRATIONS_*_API_URL).
 */
import { createServer, type IncomingMessage, type ServerResponse } from "node:http"

export interface Seen {
  method: string
  path: string
  query: URLSearchParams
  headers: IncomingMessage["headers"]
  body: string
}

export interface FakeServer {
  url: string
  seen: Seen[]
  close: () => Promise<void>
}

/** What a handler returns: a JSON body and an optional status. */
export type Reply = { status?: number; json: unknown } | null

export async function fakeServer(handle: (r: Seen) => Reply): Promise<FakeServer> {
  const seen: Seen[] = []
  const srv = createServer((req: IncomingMessage, res: ServerResponse) => {
    const chunks: Buffer[] = []
    req.on("data", (c) => chunks.push(c))
    req.on("end", () => {
      const u = new URL(req.url ?? "/", "http://fake.invalid")
      const s: Seen = { method: req.method ?? "GET", path: u.pathname, query: u.searchParams, headers: req.headers, body: Buffer.concat(chunks).toString("utf8") }
      seen.push(s)
      const r = handle(s) ?? { status: 404, json: { error: { code: "resource_missing", message: "not in the fake" } } }
      res.writeHead(r.status ?? 200, { "Content-Type": "application/json" })
      res.end(JSON.stringify(r.json))
    })
  })
  await new Promise<void>((ok) => srv.listen(0, "127.0.0.1", ok))
  const addr = srv.address()
  const port = typeof addr === "object" && addr ? addr.port : 0
  return {
    url: `http://127.0.0.1:${port}`,
    seen,
    close: () =>
      new Promise((ok) => {
        srv.closeAllConnections?.()
        srv.close(() => ok())
      }),
  }
}

/** Builds a fake token at runtime, so no key-shaped literal sits in the repo. */
export const fakeKey = (kind: string, mode: string, tail = "e2e0000000000000") => [kind, mode, tail].join("_")
