import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { BASE, makeRepo, projectYaml, shot, startDaemon, type Daemon } from "../harness"

let d: Daemon

const iso = (msFromNow: number) => new Date(Date.now() + msFromNow).toISOString()
const MIN = 60_000

/** A Codex rollout log: two turns and a 5-hour window at 40 % that resets in three hours. */
function codexLog(home: string) {
  const resets = Math.floor((Date.now() + 180 * MIN) / 1000)
  const rl = JSON.stringify({ primary: { used_percent: 40, window_minutes: 300, resets_at: resets }, secondary: null })
  const tok = (ts: string, total: number, limits: string) =>
    JSON.stringify({ timestamp: ts, type: "event_msg", payload: { type: "token_count", info: { total_token_usage: { input_tokens: total, cached_input_tokens: 0, output_tokens: 0, total_tokens: total } } } }).replace(
      /}}$/,
      `,"rate_limits":${limits}}}`,
    )
  const dir = path.join(home, ".codex", "sessions", "2026", "01", "01")
  mkdirSync(dir, { recursive: true })
  writeFileSync(
    path.join(dir, "rollout-e2e.jsonl"),
    [
      JSON.stringify({ timestamp: iso(-100 * MIN), type: "session_meta", payload: { cwd: path.join(home, "code", "demo") } }),
      JSON.stringify({ timestamp: iso(-100 * MIN), type: "turn_context", payload: { model: "gpt-x" } }),
      tok(iso(-90 * MIN), 30000, "null"),
      tok(iso(-20 * MIN), 40000, rl),
    ].join("\n") + "\n",
  )
}

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    codexLog(dd.home)
    // Two recent "Ask an agent to rank" runs on codex: the estimate for a ranking.
    mkdirSync(path.join(dd.data, "nextup", "runs"), { recursive: true })
    for (const [i, n] of [3000, 5000].entries()) {
      writeFileSync(
        path.join(dd.data, "nextup", "runs", `r${i}.json`),
        JSON.stringify({ kind: "rank", created: iso(-(i + 1) * 30 * MIN), usage: { provider: "codex", model: "gpt-x", input_tokens: n, output_tokens: 0, duration_ms: 1 } }),
      )
    }
    const repo = makeRepo(dd, "demo")
    return { prefs: true, projects: `version: 1\nprojects:\n${projectYaml("demo", "Demo", repo)}` }
  })
  const r = await fetch(`${BASE}/api/boards/work/cards`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" },
    body: JSON.stringify({ title: "Write the release notes", column: "Ready", project: "demo" }),
  })
  expect(r.status).toBe(201)
})
test.afterAll(async () => d?.stop())

test("Usage projects the pace and plans the day before the reset", async ({ page }) => {
  await page.goto("/usage")
  // 40 % was the 40k tokens spent since the window started, so codex's
  // window holds about 100k; the pace line says how long it lasts.
  const pace = page.getByTestId("pace").filter({ hasText: "codex" })
  await expect(pace.getByTestId("pace-summary")).toContainText(/codex/)

  // A typed item: a ranking, estimated from the two recorded runs.
  await page.getByTestId("plan-title").fill("Rank the queue")
  await page.getByTestId("plan-kind").selectOption("rank")
  await page.getByTestId("plan-add").click()
  const ranked = page.getByTestId("plan-item").filter({ hasText: "Rank the queue" })
  await expect(ranked.getByTestId("plan-verdict")).toHaveText("fits")
  await expect(ranked.getByTestId("plan-fit")).toContainText("Fits before codex's 5h limit resets")
  await expect(ranked.getByTestId("plan-use")).toHaveText("codex")
  await expect(ranked).toContainText("~4.0k tokens")
  await expect(page.getByTestId("plan-windows")).toContainText("1 of 1 planned item fit")

  // The top of Next up comes in as a Work turn with no estimate yet.
  await page.getByTestId("plan-from-nextup").click()
  const fromNext = page.getByTestId("plan-item").filter({ hasText: "Write the release notes" })
  await expect(fromNext).toContainText("from Next up")
  await expect(fromNext.getByTestId("plan-verdict")).toHaveText("unknown")
  await expect(fromNext.getByTestId("plan-fit")).toContainText("No recent runs of this kind")

  const saved = path.join(d.data, "usage", "plan.json")
  expect(existsSync(saved)).toBe(true)
  expect(readFileSync(saved, "utf8")).toContain('"nextup_id"')

  await page.getByRole("region", { name: "Planning" }).scrollIntoViewIfNeeded()
  await page.waitForTimeout(400)
  await page.getByRole("region", { name: "Planning" }).screenshot({ path: shot("usage-planner") })

  // Removing an item saves at once.
  await ranked.getByRole("button", { name: "Remove Rank the queue" }).click()
  await expect(page.getByTestId("plan-item")).toHaveCount(1)
  expect(readFileSync(saved, "utf8")).not.toContain("Rank the queue")
})
