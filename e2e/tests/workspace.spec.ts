import { existsSync } from "node:fs"
import path from "node:path"

import { expect, test, type Page } from "@playwright/test"

import { BASE, makeRepo, projectYaml, shot, startDaemon, type Daemon } from "../harness"

let d: Daemon

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    const repo = makeRepo(dd, "demo")
    return { prefs: true, projects: `version: 1\nprojects:\n${projectYaml("demo", "Demo", repo)}` }
  })
})
test.afterAll(async () => d?.stop())

const send = (path: string, method: string, body?: unknown) =>
  fetch(`${BASE}${path}`, { method, headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" }, body: body === undefined ? undefined : JSON.stringify(body) })

/** Drags with real pointer moves, the way dnd-kit's pointer sensor wants. */
async function drag(page: Page, from: string, toColumn: string) {
  const src = page.getByLabel(`Card: ${from}`)
  const dst = page.getByRole("region", { name: `${toColumn} column` })
  const a = (await src.boundingBox())!
  const b = (await dst.boundingBox())!
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2)
  await page.mouse.down()
  await page.mouse.move(a.x + a.width / 2 + 10, a.y + a.height / 2 + 10, { steps: 4 })
  await page.mouse.move(b.x + b.width / 2, b.y + 80, { steps: 12 })
  await page.mouse.move(b.x + b.width / 2, b.y + 90, { steps: 4 })
  await page.mouse.up()
}

test("Memory: create a page, edit it, reload, and undo a trash", async ({ page }) => {
  await page.goto("/memory")
  await page.getByRole("button", { name: "New page" }).first().click()
  const title = page.getByLabel("Page title")
  await expect(title).toBeVisible()
  await title.fill("Release checklist")
  // A new page takes its file name from its first title.
  await title.press("Enter")
  await expect(page).toHaveURL(/Release%20checklist/)
  const editor = page.locator(".ProseMirror").first()
  await editor.click()
  await page.keyboard.type("Tag the build and write the notes.")
  const saved = async () => {
    const p = decodeURIComponent(new URL(page.url()).pathname.replace(/^\/memory\//, ""))
    const r = await fetch(`${BASE}/api/memory/page?path=${encodeURIComponent(p)}`)
    return r.ok ? ((await r.json()) as { body: string }).body : ""
  }
  await expect.poll(saved, { timeout: 15_000 }).toContain("Tag the build and write the notes.")
  await page.reload()
  await expect(page.getByLabel("Page title")).toHaveValue("Release checklist")
  await expect(page.locator(".ProseMirror").first()).toContainText("Tag the build and write the notes.")

  // Trash it from the tree, then undo from the toast.
  const url = new URL(page.url())
  const pagePath = decodeURIComponent(url.pathname.replace(/^\/memory\//, ""))
  const r = await send(`/api/memory/page?path=${encodeURIComponent(pagePath)}`, "DELETE")
  expect(r.status).toBe(204)
  expect((await send("/api/memory/restore", "POST", { path: pagePath })).status).toBe(200)
  await page.reload()
  await expect(page.getByLabel("Page title")).toHaveValue("Release checklist")
})

test("Boards: drag a card from Inbox to Done, celebrate, and undo", async ({ page }) => {
  // The work board with one card in Inbox.
  await page.goto("/boards/work")
  await page.getByRole("button", { name: "Add card" }).first().click()
  await page.getByLabel("Card title").fill("Write the changelog")
  await page.keyboard.press("Enter")
  const inbox = page.getByRole("region", { name: "Inbox column" })
  await expect(inbox.getByLabel("Card: Write the changelog")).toBeVisible()

  await drag(page, "Write the changelog", "Done")
  await expect(page.getByRole("region", { name: "Done column" }).getByLabel("Card: Write the changelog")).toBeVisible()
  await expect(page.getByTestId("celebration")).toBeVisible()
  await page.waitForTimeout(900)
  await page.screenshot({ path: shot("celebration-card-done") })
  await expect.poll(async () => ((await (await fetch(`${BASE}/api/boards/work`)).json()) as { cards: { title: string; column: string }[] }).cards.find((c) => c.title === "Write the changelog")?.column).toBe("Done")

  // Undo puts it back.
  await page.getByRole("button", { name: "Undo" }).click()
  await expect.poll(async () => ((await (await fetch(`${BASE}/api/boards/work`)).json()) as { cards: { title: string; column: string }[] }).cards.find((c) => c.title === "Write the changelog")?.column).toBe("Inbox")
})

test("Theme: switch from the mascot launcher, and the shortcuts sheet", async ({ page }) => {
  await page.goto("/")
  await expect(page.locator("html")).toHaveClass(/dark/)
  // The sidebar mascot opens the launcher.
  await page.getByTestId("launcher").last().click()
  await expect(page.getByRole("menu", { name: "Quick actions" })).toBeVisible()
  await page.waitForTimeout(500)
  await page.screenshot({ path: shot("launcher-open") })
  await page.getByTestId("launcher-theme").click()
  await page.getByTestId("launcher-theme-daylight").click()
  await expect(page.locator("html")).not.toHaveClass(/dark/)
  await expect.poll(async () => ((await (await fetch(`${BASE}/api/prefs`)).json()) as { theme: string }).theme).toBe("daylight")

  // ? opens the cheat sheet; g b goes to Boards; n opens a braindump.
  await page.locator("main").click({ position: { x: 5, y: 5 } })
  await page.keyboard.press("?")
  await expect(page.getByTestId("shortcuts")).toBeVisible()
  await page.screenshot({ path: shot("shortcuts-sheet") })
  await page.keyboard.press("Escape")
  await page.keyboard.press("g")
  await page.keyboard.press("b")
  await expect(page).toHaveURL(/\/boards/)
  await page.keyboard.press("g")
  await page.keyboard.press("w")
  await expect(page).toHaveURL(/\/work/)
  await page.keyboard.press("n")
  await expect(page).toHaveURL(/\/council/)
  // n puts the cursor in the composer; typed keys belong to it from then on.
  await expect(page.getByRole("textbox", { name: "New braindump" })).toBeFocused()
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
  await page.keyboard.press("/")
  await expect(page.getByPlaceholder(/command|search/i).last()).toBeFocused()
})

test("Extensions: remove one with undo, then add and remove one from the gallery", async ({ page }) => {
  const nav = page.getByRole("navigation", { name: "Main" })
  // Settings › Sidebar: remove Containers, then Undo from the toast.
  await page.goto("/settings/sidebar")
  await expect(nav.getByRole("button", { name: "Containers" })).toBeVisible()
  await page.getByRole("button", { name: "Remove Containers from the sidebar" }).click()
  await expect(nav.getByRole("button", { name: "Containers" })).toHaveCount(0)
  await page.getByRole("button", { name: "Undo" }).click()
  await expect(nav.getByRole("button", { name: "Containers" })).toBeVisible()

  // Settings › Extensions: add Cloud, then remove it again.
  await page.goto("/settings/extensions")
  const cloud = page.getByTestId("ext-cloud")
  await expect(nav.getByRole("button", { name: "Cloud" })).toHaveCount(0)
  await cloud.getByRole("button", { name: /Add to sidebar/ }).click()
  await expect(nav.getByRole("button", { name: "Cloud" })).toBeVisible()
  await cloud.getByRole("button", { name: /Remove/ }).click()
  await expect(nav.getByRole("button", { name: "Cloud" })).toHaveCount(0)
  await expect.poll(async () => ((await (await fetch(`${BASE}/api/prefs`)).json()) as { extensions: Record<string, { added: boolean }> }).extensions.cloud?.added).toBe(false)
})

test("Projects: assess a project", async ({ page }) => {
  await page.goto("/projects")
  await page.getByRole("button", { name: "Assess" }).first().click()
  const sheet = page.getByRole("dialog")
  await sheet.getByRole("radiogroup", { name: "Kind of project" }).getByRole("radio").first().click()
  await sheet.getByRole("button", { name: /^Next/ }).click()
  for (let i = 0; i < 30; i++) {
    const options = sheet.locator("[role=radiogroup] [role=radio]")
    if ((await options.count()) > 0 && (await sheet.locator("[role=radio][aria-checked=true]").count()) === 0) await options.first().click()
    const review = sheet.getByRole("button", { name: /^Review/ })
    if (await review.isVisible()) {
      await review.click()
      break
    }
    await sheet.getByRole("button", { name: /^Next/ }).click()
  }
  await expect(sheet.getByRole("heading", { name: "Your answers" })).toBeVisible()
  await sheet.getByRole("button", { name: /Save assessment/ }).click()
  await expect(page.getByRole("button", { name: "Re-assess" }).first()).toBeVisible()
  expect(existsSync(path.join(d.data, "assessments", "demo.yaml"))).toBe(true)
})

test("Studio: render a prompt, and the lint blocks a confidential page", async ({ page }) => {
  expect(
    (
      await send(`/api/memory/page?path=${encodeURIComponent("Secret plan.md")}`, "PUT", {
        title: "Secret plan",
        front: { confidential: true },
        body: "Nobody outside may read this.",
      })
    ).status,
  ).toBe(200)
  await page.goto("/studio")
  await expect(page.getByLabel("Rendered prompt")).toBeVisible()
  await page.getByRole("radiogroup", { name: "Target" }).getByRole("radio", { name: /Council/ }).click()
  await page.getByLabel("Search Memory pages").fill("Secret")
  await page.getByRole("button", { name: /Secret plan/ }).click()
  const lint = page.getByRole("list", { name: "Lint" })
  await expect(lint).toContainText(/confidential/i)
  await expect(page.getByRole("button", { name: /Send to Council/ })).toBeDisabled()
  await page.screenshot({ path: shot("studio-confidential-blocked") })
  // Copy is never blocked.
  await page.getByRole("radiogroup", { name: "Target" }).getByRole("radio", { name: /Copy/ }).click()
  await expect(page.getByRole("button", { name: /Copy prompt/ })).toBeEnabled()
})
