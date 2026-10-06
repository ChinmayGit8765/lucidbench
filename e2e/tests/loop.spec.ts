import { writeFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { BASE, calls, git, makeRepo, projectYaml, shot, startDaemon, type Daemon } from "../harness"

let d: Daemon
let repo: string
const TITLE = "Report which side projects still build"

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    repo = makeRepo(dd, "demo")
    return { prefs: true, projects: `version: 1\nprojects:\n${projectYaml("demo", "Demo", repo)}` }
  })
})
test.afterAll(async () => d?.stop())

interface BoardCard {
  id: string
  title: string
  column: string
}
const cardsOf = async () => ((await (await fetch(`${BASE}/api/boards/work`)).json()) as { cards: BoardCard[] }).cards

test("the whole loop: braindump, council, approve, work, diff, PR, merged, Done", async ({ page }) => {
  test.setTimeout(150_000)

  // Braindump → council with fake critics.
  await page.goto("/council")
  await page.getByPlaceholder(/./).first().fill("I keep losing track of which side projects still build. A tiny script that runs each one's build and prints pass or fail.")
  await page.getByRole("combobox").first().selectOption({ label: "Demo" })
  await page.getByRole("button", { name: /Convene council/ }).click()
  await expect(page.getByRole("heading", { name: TITLE })).toBeVisible({ timeout: 45_000 })
  await expect(page.getByText("Draft · waiting for you")).toBeVisible()
  expect(calls(d)).toMatch(/^codex /m)
  expect(calls(d)).toMatch(/^grok /m)

  // Approve → a card in Ready.
  await page.getByRole("button", { name: "Approve", exact: true }).click()
  await page.getByRole("dialog").getByRole("button", { name: "Approve and add card" }).click()
  await expect(page.getByText(/On the work board · Ready/)).toBeVisible()
  const card = (await cardsOf()).find((c) => c.title === TITLE)
  expect(card?.column).toBe("Ready")

  // Start work on the card; the fake claude commits in its worktree.
  await page.getByRole("button", { name: "Start work" }).click()
  await expect(page).toHaveURL(/\/work\/new\//)
  await page.getByRole("button", { name: "Start session" }).click()
  await page.getByRole("dialog").getByRole("button", { name: "Start session" }).click()
  await expect(page).toHaveURL(/\/work\/[a-z0-9]+$/)
  const working = page.locator("[data-sprite='working']")
  if (await working.isVisible().catch(() => false)) await page.screenshot({ path: shot("work-running-sprite") })
  const openPR = page.getByTestId("open-pr")
  await expect(openPR).toBeEnabled({ timeout: 60_000 })

  // The diff: one file, one commit.
  await expect(page.getByText("NOTES.md").first()).toBeVisible()
  await expect(page.getByText(/1 commit on top of/)).toBeVisible()
  await expect.poll(async () => (await cardsOf()).find((c) => c.id === card!.id)?.column).toBe("Review")

  // Open PR: pushes to the bare origin and runs the fake gh.
  await openPR.click()
  await page.getByRole("dialog").getByRole("button", { name: "Push and open PR" }).click()
  await expect(page.getByRole("link", { name: /View draft PR/ })).toBeVisible()
  expect(calls(d)).toMatch(/^gh pr create --draft/m)
  const branches = git(path.join(d.root, "remotes", "demo.git"), d.env, "branch", "--list", "lucid/*")
  expect(branches).toMatch(/lucid\//)
  await expect.poll(async () => (await cardsOf()).find((c) => c.id === card!.id)?.column).toBe("Review")

  // GitHub (the fake) says merged: the card moves to Done, with a celebration.
  writeFileSync(path.join(d.state, "pr-state"), "MERGED")
  await page.getByTestId("check-pr").click()
  await expect(page.getByTestId("celebration")).toBeVisible()
  await expect(page.getByTestId("celebration")).toContainText("PR merged")
  await page.waitForTimeout(900)
  await page.screenshot({ path: shot("celebration-pr-merged") })
  await expect.poll(async () => (await cardsOf()).find((c) => c.id === card!.id)?.column).toBe("Done")
  await expect(page.getByRole("link", { name: /View merged PR/ })).toBeVisible()

  // The board shows it in Done.
  await page.goto("/boards/work")
  await expect(page.getByRole("region", { name: "Done column" }).getByLabel(`Card: ${TITLE}`)).toBeVisible()
})
