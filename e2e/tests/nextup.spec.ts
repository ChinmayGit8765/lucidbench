import { readFileSync } from "node:fs"
import path from "node:path"

import { expect, test, type Page } from "@playwright/test"

import { BASE, makeRepo, projectYaml, shot, startDaemon, type Daemon } from "../harness"

let d: Daemon

/** A date n days from today, as the board stores it. */
function day(n: number): string {
  const t = new Date(Date.now() + n * 86400_000)
  return `${t.getFullYear()}-${String(t.getMonth() + 1).padStart(2, "0")}-${String(t.getDate()).padStart(2, "0")}`
}

const send = (p: string, method: string, body?: unknown) =>
  fetch(`${BASE}${p}`, { method, headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" }, body: body === undefined ? undefined : JSON.stringify(body) })

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    const repo = makeRepo(dd, "demo")
    const hush = makeRepo(dd, "hush")
    const demo = projectYaml("demo", "Demo", repo).replace("    local_path:", "    needs:\n      - what: a changelog\n        status: todo\n    local_path:")
    return { prefs: true, projects: `version: 1\nprojects:\n${demo}${projectYaml("hush", "Hush Works", hush, "confidential")}` }
  })
  // Four cards: due tomorrow, in progress and small, confidential, blocked.
  for (const c of [
    { title: "Write the release notes", column: "Ready", project: "demo", due: day(1) },
    { title: "Tidy the parser", column: "In progress", project: "demo", labels: ["small"] },
    { title: "Hush plan", column: "Ready", project: "hush" },
    { title: "Fix the flaky test", column: "Inbox", project: "demo", labels: ["blocked"] },
  ]) {
    const r = await send("/api/boards/work/cards", "POST", c)
    expect(r.status).toBe(201)
  }
})
test.afterAll(async () => d?.stop())

const listTitles = (page: Page) => page.getByTestId("nextup-item-title").allInnerTexts()

test("Next up ranks the seeded work and explains each score", async ({ page }) => {
  await page.goto("/nextup")
  await expect(page.getByTestId("nextup-hero-title")).toHaveText("Write the release notes")
  // Due tomorrow (20) beats in progress and small (18); the confidential
  // card and the need score nothing (ties go by id); the blocked card is last.
  await expect.poll(() => listTitles(page)).toEqual(["Tidy the parser", "Hush plan", "Demo needs a changelog", "Fix the flaky test"])
  const hero = page.getByTestId("nextup-hero")
  await expect(hero.getByTestId("nextup-breakdown")).toContainText("due in 1 day")
  await expect(hero).toContainText("Starts claude in a new worktree of Demo")
  // A row's score opens its breakdown.
  const blocked = page.locator('[data-testid="nextup-item"]', { hasText: "Fix the flaky test" })
  await blocked.getByTestId("nextup-score").click()
  await expect(blocked.getByTestId("nextup-breakdown")).toContainText("blocked: something else has to happen first")
  await page.locator("main").evaluate((m) => m.scrollTo(0, 0))
  await page.waitForTimeout(400)
  await page.screenshot({ path: shot("nextup-page") })

  // The palette finds it.
  await page.goto("/")
  await page.keyboard.press("Control+k")
  await page.keyboard.type("What should I work on")
  await page.keyboard.press("Enter")
  await expect(page).toHaveURL(/\/nextup$/)
})

test("Overview's tile opens the confirm dialog for the top pick", async ({ page }) => {
  await page.goto("/")
  await expect(page.getByTestId("nextup-tile-title")).toHaveText("Write the release notes")
  await page.waitForTimeout(600)
  await page.screenshot({ path: shot("nextup-overview-tile") })
  await page.getByTestId("nextup-tile-start").click()
  await expect(page).toHaveURL(/\/nextup$/)
  const dialog = page.getByRole("dialog")
  await expect(dialog).toContainText("Start work: Write the release notes?")
  await expect(dialog).toContainText("card")
  await page.waitForTimeout(300)
  await page.screenshot({ path: shot("nextup-confirm") })
  // Cancel starts nothing.
  await dialog.getByRole("button", { name: "Cancel" }).click()
  const sessions = (await (await fetch(`${BASE}/api/work/sessions`)).json()) as unknown[]
  expect(sessions).toHaveLength(0)
})

test("Start opens the confirm dialog and starts a session with the prefilled prompt", async ({ page }) => {
  await page.goto("/nextup")
  const need = page.locator('[data-testid="nextup-item"]', { hasText: "Demo needs a changelog" })
  await need.getByTestId("nextup-item-action").click()
  const dialog = page.getByRole("dialog")
  await expect(dialog.getByLabel("Prompt")).toHaveValue(/Demo needs a changelog, and it is todo/)
  await dialog.getByRole("button", { name: "Start session" }).click()
  await expect(page).toHaveURL(/\/work\/[^/]+$/)
  const id = page.url().split("/").pop()!
  const s = (await (await fetch(`${BASE}/api/work/sessions/${id}`)).json()) as { project: string; provider: string; prompt: string }
  expect(s.project).toBe("demo")
  expect(s.provider).toBe("claude")
  expect(s.prompt).toContain("Demo needs a changelog, and it is todo")
  // Once its turn is over, end the session so it does not wait in the list.
  await expect.poll(async () => ((await (await fetch(`${BASE}/api/work/sessions/${id}`)).json()) as { status: string }).status, { timeout: 30_000 }).toBe("waiting")
  expect((await send(`/api/work/sessions/${id}/end`, "POST")).status).toBe(200)
})

test("Ask an agent to rank reorders the list and sends no confidential title", async ({ page }) => {
  await page.goto("/nextup")
  await expect(page.getByTestId("nextup-hero-title")).toHaveText("Write the release notes")
  await page.getByTestId("nextup-rank").click()
  const dialog = page.getByRole("dialog")
  await expect(dialog).toContainText("1 confidential item goes as an id and a score only")
  await dialog.getByRole("button", { name: "Rank" }).click()
  // The fake ranks backwards: the blocked card is now on top.
  await expect(page.getByTestId("nextup-hero-title")).toHaveText("Fix the flaky test")
  await expect.poll(() => listTitles(page)).toEqual(["Demo needs a changelog", "Hush plan", "Tidy the parser", "Write the release notes"])
  await expect(page.getByTestId("nextup-ranked")).toContainText("Ordered by claude (haiku)")
  await expect(page.getByTestId("nextup-hero")).toContainText("The fake ranks the list backwards.")

  const input = readFileSync(path.join(d.state, "nextup-stdin.txt"), "utf8")
  expect(input).toContain("Write the release notes")
  for (const secret of ["Hush plan", "Hush Works", "hush"]) expect(input).not.toContain(secret)
  // The call was recorded for the Usage page.
  const usage = (await (await fetch(`${BASE}/api/usage/summary?days=1`)).json()) as { lucidbench: { sources: { name: string }[] } }
  expect(usage.lucidbench.sources.map((s) => s.name)).toContain("nextup")
})

test("Snooze hides an item until it is brought back", async ({ page }) => {
  await page.goto("/nextup")
  const row = page.locator('[data-testid="nextup-item"]', { hasText: "Tidy the parser" })
  await row.getByTestId("nextup-snooze").click()
  await page.getByRole("menuitem", { name: "For a week" }).click()
  await expect(page.locator('[data-testid="nextup-item"]', { hasText: "Tidy the parser" })).toHaveCount(0)
  const hidden = page.getByTestId("nextup-hidden")
  await expect(hidden).toContainText("Tidy the parser")
  const v = (await (await fetch(`${BASE}/api/nextup`)).json()) as { items: { title: string }[]; hidden: { title: string; until?: string }[] }
  expect(v.items.map((i) => i.title)).not.toContain("Tidy the parser")
  expect(v.hidden.find((h) => h.title === "Tidy the parser")?.until).toBeTruthy()
  await hidden.getByRole("button", { name: "Bring back Tidy the parser" }).click()
  await expect(page.locator('[data-testid="nextup-item"]', { hasText: "Tidy the parser" })).toHaveCount(1)
})
