import { expect, test } from "@playwright/test"

import { BASE, makeRepo, projectYaml, startDaemon, type Daemon } from "../harness"

// Ideas: an idea made by the council (fake claude, codex and grok) is joined
// to its brief, critiques and card, and the Ideas view lays all of it out.
let d: Daemon
const TITLE = "Report which side projects still build"

test.beforeAll(async () => {
  d = await startDaemon((dd) => ({ prefs: true, projects: `version: 1\nprojects:\n${projectYaml("demo", "Demo", makeRepo(dd, "demo"))}` }))
})
test.afterAll(async () => d?.stop())

test("an idea's structure renders: council, critiques, brief, card and timeline", async ({ page }) => {
  test.setTimeout(150_000)
  await page.goto("/council")
  await page.getByPlaceholder(/./).first().fill("Which of my side projects still build? A script that runs each build and prints pass or fail.")
  await page.getByRole("combobox").first().selectOption({ label: "Demo" })
  await page.getByRole("button", { name: /Convene council/ }).click()
  await expect(page.getByRole("heading", { level: 1, name: TITLE })).toBeVisible({ timeout: 45_000 })
  await page.getByRole("button", { name: "Approve", exact: true }).click()
  await expect(page.getByText(/On the work board · Ready/)).toBeVisible()

  const list = (await (await fetch(`${BASE}/api/ideas`)).json()) as { ideas?: { id: string; title: string; stage: string }[] } | { id: string; title: string; stage: string }[]
  const ideas = Array.isArray(list) ? list : (list.ideas ?? [])
  const idea = ideas.find((i) => i.title === TITLE)!
  expect(idea.stage).toBe("card")

  await page.goto("/ideas")
  await page.getByText(TITLE).first().click()
  await expect(page.getByRole("heading", { name: "Structure" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Timeline" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Pages that link the brief" })).toBeVisible()
  await expect(page.getByText("1 round")).toBeVisible()
  await expect(page.getByRole("list", { name: "Stage" })).toBeVisible()
  // The brief's own sections, and the card it became, are on the page.
  await expect(page.getByText("Done criteria").first()).toBeVisible()
  await expect(page.getByText(/Ready/).first()).toBeVisible()

  // The API carries the same pieces: the council's critiques came from the fake codex and grok.
  const full = JSON.stringify(await (await fetch(`${BASE}/api/ideas/${encodeURIComponent(idea.id)}`)).json())
  expect(full).toContain("codex")
  expect(full).toContain("grok")
  expect(full).toContain("Say which build command each project uses")
})
