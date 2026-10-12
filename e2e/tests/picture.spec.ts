import { existsSync, readFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { makeRepo, projectYaml, startDaemon, type Daemon } from "../harness"

// Picture: a front-end canvas (Excalidraw) and a back-end schematic (Mermaid)
// are created in the UI, saved into the project's repository, and still there
// after a reload.
let d: Daemon
let repo = ""

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    repo = makeRepo(dd, "atlas")
    return { prefs: true, extensions: ["picture"], projects: `version: 1\nprojects:\n${projectYaml("atlas", "Atlas", repo)}` }
  })
})
test.afterAll(async () => d?.stop())

test("a canvas and a schematic are saved to the repo and survive a reload", async ({ page }) => {
  await page.goto("/picture")

  await page.getByRole("button", { name: "New diagram" }).click()
  await page.getByLabel("Name").fill("api-flow")
  await page.getByLabel("Mermaid source").fill("flowchart LR\n  web[Web] --> api[API]\n  api --> db[(Store)]\n")
  await page.getByRole("button", { name: "Save", exact: true }).click()
  await page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }).click()
  await expect(page.getByText("unsaved")).toBeHidden()

  await page.getByRole("button", { name: "New canvas" }).click()
  await page.getByLabel("Name").fill("login-screen")
  await page.getByRole("button", { name: "Save", exact: true }).click()
  await page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }).click()
  await expect(page.getByText("unsaved")).toBeHidden()

  // On disk, inside the project's own repository.
  const dir = path.join(repo, "docs", "picture")
  await expect.poll(() => existsSync(path.join(dir, "api-flow.mmd")) && existsSync(path.join(dir, "login-screen.excalidraw"))).toBe(true)
  expect(readFileSync(path.join(dir, "api-flow.mmd"), "utf8")).toContain("api[API]")
  expect(JSON.parse(readFileSync(path.join(dir, "login-screen.excalidraw"), "utf8")).type).toBe("excalidraw")

  // Reload: both are listed and the schematic's source comes back.
  await page.reload()
  await expect(page.getByText("api-flow", { exact: true })).toBeVisible()
  await expect(page.getByText("login-screen", { exact: true })).toBeVisible()
  await page.getByText("api-flow", { exact: true }).click()
  await expect(page.getByLabel("Mermaid source")).toHaveValue(/web\[Web\] --> api\[API\]/)
})
