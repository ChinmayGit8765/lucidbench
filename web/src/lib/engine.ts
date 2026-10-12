import { refreshAll, sendJSON } from "@/lib/api"

/* Mirrors POST /api/engine/restart (internal/server/restart.go). */

export const RESTART_PATH = "/api/engine/restart"

/** How long the UI waits for the new engine before it gives up. */
export const RESTART_TIMEOUT_MS = 20_000

const health = async (): Promise<{ started?: string } | null> => {
  try {
    const r = await fetch("/api/health", { cache: "no-store" })
    return r.ok ? ((await r.json()) as { started?: string }) : null
  } catch {
    return null
  }
}

/**
 * Restarts the engine and waits for the new one: it is up once /api/health
 * reports a different start time. Resolves true when it came back within
 * RESTART_TIMEOUT_MS; throws when the engine refused (for example 501).
 */
export async function restartEngine(): Promise<boolean> {
  const before = (await sendJSON<{ started?: string }>(RESTART_PATH, "POST"))?.started
  const deadline = Date.now() + RESTART_TIMEOUT_MS
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 500))
    const h = await health()
    if (h?.started && h.started !== before) {
      refreshAll()
      return true
    }
  }
  return false
}
