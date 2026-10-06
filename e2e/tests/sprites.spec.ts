import { readdirSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { BASE, shot, startDaemon, type Daemon } from "../harness"

let d: Daemon

test.beforeAll(async () => {
  d = await startDaemon(() => ({ prefs: true }))
})
test.afterAll(async () => d?.stop())

// A 1x1 PNG, made here so the test needs no binary fixture.
const PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=", "base64")

test("Lumi fills a loading area, and the Projects empty state leads to import", async ({ page }) => {
  // Hold the CI runs answer back so Overview's activity card is still loading.
  await page.route("**/api/ci/runs", async (route) => {
    await new Promise((r) => setTimeout(r, 4000))
    await route.continue()
  })
  await page.goto("/")
  const loading = page.getByTestId("loading-art").first()
  await expect(loading).toBeVisible()
  await expect(loading.locator("[data-sprite='loading']")).toBeVisible()
  await page.waitForTimeout(400)
  await loading.screenshot({ path: shot("loading-sprite-area") })
  await page.screenshot({ path: shot("loading-sprite-overview") })

  await page.goto("/projects")
  await expect(page.getByTestId("projects-import")).toBeVisible()
  await page.waitForTimeout(400)
  await page.screenshot({ path: shot("empty-state-projects-after") })
  await page.getByTestId("projects-import").click()
  await expect(page).toHaveURL(/\/setup\/projects/)
})

test("Sprites: copy a built-in theme, upload a sprite into a slot, and see it used", async ({ page }) => {
  await page.goto("/settings/appearance")
  const slots = page.getByTestId("sprite-slots")
  await expect(slots.locator("[data-slot]")).toHaveCount(8)
  await expect(slots.locator("[data-placeholder='lumi']")).toHaveCount(8)
  await page.getByRole("button", { name: "Make an editable copy" }).click()
  // The copy is applied; its slots take uploads.
  await expect(page.getByRole("button", { name: "Upload a working sprite" })).toBeVisible()
  await page.locator("#sprites").scrollIntoViewIfNeeded()
  await page.screenshot({ path: shot("appearance-sprites") })
  const chooser = page.waitForEvent("filechooser")
  await page.getByRole("button", { name: "Upload a celebrate sprite" }).click()
  await (await chooser).setFiles({ name: "party.png", mimeType: "image/png", buffer: PNG })
  await expect(slots.locator("[data-slot='celebrate'] img[data-sprite='celebrate']")).toBeVisible()
  await expect(slots.locator("[data-slot='celebrate']")).toContainText("sprite-celebrate.png")

  // Saved in the theme's own folder, and served as an image.
  const prefs = (await (await fetch(`${BASE}/api/prefs`)).json()) as { theme: string }
  expect(prefs.theme).toMatch(/-copy/)
  expect(readdirSync(path.join(d.data, "themes", prefs.theme))).toContain("sprite-celebrate.png")
  const img = await fetch(`${BASE}/api/themes/${prefs.theme}/assets/sprite-celebrate.png`)
  expect(img.headers.get("content-type")).toBe("image/png")

  // An SVG with a script is cleaned on the way in.
  const chooser2 = page.waitForEvent("filechooser")
  await page.getByRole("button", { name: "Upload a loading sprite" }).click()
  await (await chooser2).setFiles({
    name: "spin.svg",
    mimeType: "image/svg+xml",
    buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><script>alert(1)</script><circle cx="5" cy="5" r="4" onload="alert(2)"/></svg>'),
  })
  await expect(slots.locator("[data-slot='loading']")).toContainText("sprite-loading.svg")
  const svg = await (await fetch(`${BASE}/api/themes/${prefs.theme}/assets/sprite-loading.svg`)).text()
  expect(svg).toContain("<circle")
  expect(svg).not.toMatch(/script|onload/)
})
