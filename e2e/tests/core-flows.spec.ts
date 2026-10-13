import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { BASE, makeRepo, projectYaml, shot, startDaemon, type Daemon } from "../harness"

// The flows used every day: a Work session's state after its PR merges, the
// session at 1024x700, Council's approve bar, a board at 1024 and Settings
// that save to config.yaml.

let d: Daemon
const CONFIG = '# My config: this comment stays.\nserver:\n  addr: "127.0.0.1:7420" # replaced by the env\n\npower:\n  cluster: "always"\n'

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    const repo = makeRepo(dd, "demo")
    return { prefs: true, projects: `version: 1\nprojects:\n${projectYaml("demo", "Demo", repo)}`, config: CONFIG }
  })
})
test.afterAll(async () => d?.stop())

interface Session {
  id: string
  status: string
  pr_url?: string
  removed?: boolean
}

const send = (p: string, body?: unknown) =>
  fetch(`${BASE}${p}`, { method: "POST", headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" }, body: body === undefined ? undefined : JSON.stringify(body) })
const getSession = async (id: string) => (await (await fetch(`${BASE}/api/work/sessions/${id}`)).json()) as Session
const nextupIds = async () => ((await (await fetch(`${BASE}/api/nextup`)).json()) as { items: { id: string }[] }).items.map((i) => i.id)

async function startWaiting(prompt: string): Promise<string> {
  const r = await send("/api/work/sessions", { project: "demo", prompt, provider: "claude" })
  expect(r.status).toBe(201)
  const { id } = (await r.json()) as Session
  await expect.poll(async () => (await getSession(id)).status, { timeout: 60_000 }).toBe("waiting")
  return id
}

test("a merged PR ends the session: no 'waiting' anywhere, and the worktree comes off in one click", async ({ page }) => {
  test.setTimeout(150_000)
  const id = await startWaiting("Write a build report for the side projects")
  expect(await nextupIds()).toContain(`work:${id}`)
  expect((await send(`/api/work/sessions/${id}/pr`)).status).toBe(200)

  // GitHub (the fake) says merged.
  writeFileSync(path.join(d.state, "pr-state"), "MERGED")
  expect((await send(`/api/work/sessions/${id}/pr/refresh`)).status).toBe(200)
  expect((await getSession(id)).status).toBe("merged")

  // The session: the merged bar instead of the composer and the waiting banner.
  await page.goto(`/work/${id}`)
  await expect(page.getByTestId("ended-bar")).toContainText("Merged — remove the worktree?")
  await expect(page.getByTestId("waiting-banner")).toHaveCount(0)
  await expect(page.getByTestId("composer")).toHaveCount(0)
  await expect(page.getByTestId("waiting-footer")).toHaveCount(0)
  await expect(page.getByTestId("ended-footer")).toContainText("Merged")
  await page.screenshot({ path: shot("work-session-merged") })

  // The list counts it as merged, not waiting and not an open PR.
  await page.goto("/work")
  const counters = page.getByTestId("work-counters")
  await expect(counters).toContainText(/Waiting for you\s*0/)
  await expect(counters).toContainText(/Open PRs\s*0/)
  await expect(counters).toContainText(/Merged\s*1/)
  await expect(page.locator("tbody")).toContainText("Merged")
  await expect(page.locator("tbody")).not.toContainText("Waiting for you")

  // Next up and the Overview no longer ask for a reply.
  expect(await nextupIds()).not.toContain(`work:${id}`)
  await page.goto("/")
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible()
  await expect(page.getByText(/session(s)? waiting for you/)).toHaveCount(0)

  // One click removes the worktree: its commits landed, so nothing to discard.
  await page.goto(`/work/${id}`)
  await page.getByTestId("remove-worktree").click()
  await expect(page.getByTestId("ended-bar")).toHaveCount(0)
  await expect.poll(async () => (await getSession(id)).removed).toBe(true)
  expect((await getSession(id)).status).toBe("merged")
})

test("the Work session at 1024x700: the timeline leads and the composer sends", async ({ browser }) => {
  test.setTimeout(150_000)
  writeFileSync(path.join(d.state, "pr-state"), "OPEN")
  const id = await startWaiting("Write a build report for the side projects")
  const ctx = await browser.newContext({ viewport: { width: 1024, height: 700 } })
  const page = await ctx.newPage()
  await page.goto(`/work/${id}`)
  const composer = page.getByTestId("composer")
  await expect(page.getByTestId("waiting-banner")).toBeVisible()
  const box = (await composer.boundingBox())!
  expect(box.height).toBeLessThan(130)
  const header = (await page.getByTestId("session-header").boundingBox())!
  // The timeline gets more than half of the screen.
  expect(700 - header.height - box.height).toBeGreaterThan(350)
  const input = page.getByTestId("composer-input")
  await expect(input).toBeFocused()
  await input.fill("Also list the projects that failed")
  await page.screenshot({ path: shot("work-session-waiting-1024") })
  await input.press("Enter")
  await expect(page.getByTestId("turn-separator")).toContainText("Turn 2")
  await expect(page.getByTestId("waiting-banner")).toBeVisible({ timeout: 60_000 })
  await expect(page.getByTestId("turn-indicator")).toHaveText("turn 2")
  await ctx.close()
})

test("Council keeps Approve on screen at 1440x900, and approving takes one click", async ({ browser }) => {
  test.setTimeout(120_000)
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const page = await ctx.newPage()
  await page.goto("/council")
  await page.getByPlaceholder(/./).first().fill("I keep losing track of which side projects still build. A tiny script that runs each one's build and prints pass or fail.")
  await page.getByRole("combobox").first().selectOption({ label: "Demo" })
  await page.getByRole("button", { name: /Convene council/ }).click()
  const bar = page.getByTestId("council-actions")
  await expect(bar).toBeVisible({ timeout: 45_000 })
  await expect(bar.getByRole("list", { name: "Verdicts" })).toBeVisible()
  const approve = bar.getByRole("button", { name: "Approve", exact: true })
  await expect(approve).toBeInViewport()
  await expect(bar.getByRole("button", { name: /Send back/ })).toBeInViewport()
  await expect(bar.getByRole("button", { name: /Run again/ })).toBeInViewport()
  await page.screenshot({ path: shot("council-done-sticky") })
  await approve.click()
  await expect(page.getByRole("dialog")).toHaveCount(0)
  await expect(bar).toContainText(/On the work board · Ready/)
  await expect(bar.getByRole("button", { name: /Start work/ })).toBeInViewport()
  await ctx.close()
})

test("a board at 1024 shows a cue for the columns off-screen", async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1024, height: 700 } })
  const page = await ctx.newPage()
  await page.goto("/boards/work")
  const cue = page.getByTestId("board-scroll-cue")
  await expect(cue).toBeVisible()
  await expect(cue).toContainText(/\d more/)
  const done = page.getByRole("region", { name: "Done column" })
  await expect(done).not.toBeInViewport({ ratio: 0.9 })
  await page.screenshot({ path: shot("boards-1024-cue") })
  // Each click scrolls one column; the cue goes once nothing is left to the right.
  for (let i = 0; i < 5 && (await cue.isVisible()); i++) {
    await cue.click({ timeout: 3000 }).catch(() => undefined)
    await page.waitForTimeout(500)
  }
  await expect(cue).toHaveCount(0)
  await expect(done).toBeInViewport({ ratio: 0.95 })
  await ctx.close()
})

test("Settings › Infrastructure saves to config.yaml, keeps its comments, and survives a reload", async ({ page }) => {
  const file = d.env.LUCID_CONFIG!
  await page.goto("/settings/infrastructure")
  await page.getByRole("radiogroup", { name: "Runner mode" }).getByRole("radio", { name: "Manual" }).click()
  await page.getByTestId("config-save").click()
  const dialog = page.getByRole("dialog")
  await expect(dialog.getByTestId("config-diff")).toContainText('runners: "off"')
  await page.screenshot({ path: shot("settings-save-diff") })
  await dialog.getByRole("button", { name: "Save to config.yaml" }).click()
  // A mode waits for a restart, and the notice carries the button that does it.
  await expect(page.getByTestId("config-pending")).toContainText("Restart the engine to apply")
  await expect(page.getByTestId("config-pending").getByTestId("restart-engine")).toBeVisible()

  const saved = readFileSync(file, "utf8")
  expect(saved).toContain("# My config: this comment stays.")
  expect(saved).toContain('addr: "127.0.0.1:7420" # replaced by the env')
  expect(saved).toContain('cluster: "always"')
  expect(saved).toContain('runners: "off"')
  expect(saved.split("\n").filter((l) => l === "").length).toBe(CONFIG.split("\n").filter((l) => l === "").length)
  const backups = readdirSync(path.dirname(file)).filter((f) => f.startsWith(path.basename(file) + ".bak-"))
  expect(backups.length).toBe(1)
  expect(readFileSync(path.join(path.dirname(file), backups[0]), "utf8")).toBe(CONFIG)

  await page.reload()
  await expect(page.getByRole("radiogroup", { name: "Runner mode" }).getByRole("radio", { name: "Manual" })).toHaveAttribute("aria-checked", "true")
  await expect(page.getByTestId("config-pending")).toBeVisible()
  expect(existsSync(file + ".tmp")).toBe(false)

  // An idle time applies on save: it never joins the restart notice.
  await page.getByLabel("Runner idle minutes").fill("12")
  await page.getByTestId("config-save").click()
  await page.getByRole("dialog").getByRole("button", { name: "Save to config.yaml" }).click()
  await expect(page.getByText("The engine uses it now.")).toBeVisible()
  expect(readFileSync(file, "utf8")).toContain("runner_idle_minutes: 12")
  await expect(page.getByTestId("config-pending")).toContainText("power.runners")
  await expect(page.getByTestId("config-pending")).not.toContainText("runner_idle_minutes")
})
