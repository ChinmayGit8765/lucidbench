import { mkdirSync, writeFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { BASE, calls, makeRepo, projectYaml, shot, startDaemon, type Daemon } from "../harness"

// The Assistant: a chat whose proposals wait for Apply, bots you can task,
// and a braindump split into items. Every model answer comes from the fake
// claude (e2e/fakecli), which proposes one card and splits any dump into
// three items.

let d: Daemon

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    const repo = makeRepo(dd, "demo")
    // An agent the user already made in Claude Code, for Import.
    const agents = path.join(dd.home, ".claude", "agents")
    mkdirSync(agents, { recursive: true })
    writeFileSync(path.join(agents, "fixture-helper.md"), "---\nname: fixture-helper\ndescription: Answers questions about the fixture.\nmodel: haiku\n---\nBe brief.\n")
    return {
      prefs: true,
      projects: `version: 1\nprojects:\n${projectYaml("demo", "Demo", repo)}${projectYaml("hush", "Quiet Harbour", repo, "confidential")}`,
    }
  })
})
test.afterAll(async () => d?.stop())

interface Card {
  title: string
  column: string
  project?: string
  labels?: string[]
}
const cards = async () => ((await (await fetch(`${BASE}/api/boards/work`)).json()) as { cards: Card[] }).cards

test("chat: a proposed card waits for Apply, then it is on the board", async ({ page }) => {
  await page.goto("/")
  // Ask Lucid… from the mascot launcher.
  await page.getByTestId("launcher").last().click()
  await page.getByTestId("launcher-ask").click()
  await expect(page).toHaveURL(/\/assistant$/)

  await page.getByTestId("assistant-input").fill("make a card to write the README for project demo")
  await page.getByTestId("assistant-send").click()
  const proposal = page.getByTestId("assistant-proposal")
  await expect(proposal).toHaveAttribute("data-action", "create_card")
  await expect(proposal).toContainText("Write the README")
  await expect(page.getByText("I'll put that on the board for demo.")).toBeVisible()
  await expect(page).toHaveURL(/\/assistant\/c\//)

  // Proposed is not applied: no card yet.
  expect((await cards()).find((c) => c.title === "Write the README")).toBeUndefined()
  await page.waitForTimeout(300)
  await page.screenshot({ path: shot("assistant-chat-actions") })

  await proposal.getByTestId("proposal-apply").click()
  await expect(proposal).toHaveAttribute("data-status", "applied")
  await expect.poll(async () => (await cards()).find((c) => c.title === "Write the README")).toMatchObject({ project: "demo", column: "Inbox" })

  // The model ran with no tools, and the confidential project's name never reached it.
  const log = calls(d)
  expect(log).toContain("--tools")
  expect(log).not.toContain("Quiet Harbour")

  // A conversation about the confidential project is refused before any CLI runs.
  const before = calls(d).length
  const r = await fetch(`${BASE}/api/assistant/turn`, {
    method: "POST",
    headers: { "X-Lucid-Confirm": "yes", "Content-Type": "application/json" },
    body: JSON.stringify({ message: "what next?", project: "hush" }),
  })
  expect(r.status).toBe(403)
  expect(calls(d).length).toBe(before)
})

test("bots: make one, import one, and task it with a Work session through the confirm dialog", async ({ page }) => {
  test.setTimeout(120_000)
  await page.goto("/assistant/bots")
  await page.getByTestId("bots-new").click()
  await page.getByLabel("Bot name").fill("Doc Writer")
  await page.getByLabel("Bot model").fill("haiku")
  await page.getByLabel("Bot persona").fill("You write clear, short documentation.")
  await page.getByLabel("Bot sprite").selectOption("thinking")
  await page.getByTestId("bot-save").click()
  await expect(page.getByTestId("bot-card")).toContainText("Doc Writer")

  // Import lists the agent in the fake home, read-only.
  await page.getByTestId("bots-import").click()
  const list = page.getByTestId("bots-import-list")
  await expect(list).toContainText("fixture-helper")
  await expect(page.getByTestId("import-note-grok")).toContainText("exposes no saved agents")
  await list.getByRole("button", { name: "Import", exact: true }).click()
  await expect(list).toContainText("imported")
  await page.keyboard.press("Escape")
  await expect(page.getByTestId("bot-card")).toHaveCount(2)
  await page.waitForTimeout(300)
  await page.screenshot({ path: shot("assistant-bots") })

  // Task this bot: a Work session, only after the confirm dialog.
  await page.getByTestId("bot-card").filter({ hasText: "Doc Writer" }).getByTestId("bot-task").click()
  await page.getByLabel("Task project").selectOption("demo")
  await page.getByLabel("What to do").fill("Write the build notes")
  await page.getByTestId("bot-task-go").click()
  const confirm = page.getByRole("dialog", { name: "Start Doc Writer on demo?" })
  await expect(confirm).toBeVisible()
  expect((await (await fetch(`${BASE}/api/work/sessions`)).json()) as unknown[]).toHaveLength(0)
  await confirm.getByRole("button", { name: "Start session" }).click()
  await expect(page).toHaveURL(/\/work\/[^/]+$/)
  const id = page.url().split("/").pop()!
  const session = async () => (await (await fetch(`${BASE}/api/work/sessions/${id}`)).json()) as { status: string; prompt: string; provider: string; model?: string }
  await expect.poll(async () => (await session()).status, { timeout: 60_000 }).toBe("waiting")
  const s = await session()
  expect(s.provider).toBe("claude")
  expect(s.model).toBe("haiku")
  expect(s.prompt).toContain("You write clear, short documentation.")
  expect(s.prompt).toContain("Write the build notes")
})

test("braindump: split into three items and apply two", async ({ page }) => {
  await page.goto("/assistant/braindump")
  await page.getByLabel("Paste a braindump").fill("ok so the demo needs a changelog page. also maybe a tool that renames photos by date?? and how should releases be numbered")
  await page.getByTestId("braindump-parse-run").click()
  const items = page.getByTestId("braindump-item")
  await expect(items).toHaveCount(3)
  await expect(items.nth(1)).toContainText("new project?")
  await page.waitForTimeout(300)
  await page.screenshot({ path: shot("assistant-braindump-parse") })

  await items.nth(0).getByRole("checkbox").check()
  await items.nth(1).getByRole("checkbox").check()
  await page.getByTestId("braindump-apply-selected").click()
  await expect(items.nth(0)).toHaveAttribute("data-status", "applied")
  await expect(items.nth(1)).toHaveAttribute("data-status", "applied")
  await expect(items.nth(2)).toHaveAttribute("data-status", "pending")
  const all = await cards()
  expect(all.find((c) => c.title === "Add a changelog page to demo")).toMatchObject({ project: "demo", column: "Inbox" })
  expect(all.find((c) => c.title === "Build a tool that renames photos by date")?.labels).toContain("idea")
  // The council item was not applied, so no council ran.
  expect((await (await fetch(`${BASE}/api/council/sessions`)).json()) as unknown[]).toHaveLength(0)
})
