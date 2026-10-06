import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { git, shot, startDaemon, type Daemon } from "../harness"

let d: Daemon
let code: string

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    // Two repositories to find, one level and two levels down, and a plain folder.
    code = path.join(dd.root, "code")
    const mk = (rel: string, files: Record<string, string>) => {
      const dir = path.join(code, rel)
      mkdirSync(dir, { recursive: true })
      git(dir, dd.env, "init", "-q", "-b", "main")
      for (const [f, body] of Object.entries(files)) writeFileSync(path.join(dir, f), body)
    }
    mk("tide-app", { "package.json": `{"dependencies":{"react":"19"}}` })
    mk(path.join("tools", "ledger-cli"), { "go.mod": "module ledger\n", "main.go": "package main\n" })
    mkdirSync(path.join(code, "notes"), { recursive: true })
  })
})
test.afterAll(async () => d?.stop())

test("first-run setup: accounts, a new vault, imported projects, a theme, power, and finish", async ({ page }) => {
  await page.goto("/")
  await expect(page).toHaveURL(/\/setup/)
  await expect(page.getByTestId("setup")).toBeVisible()

  // 1. Accounts: the fake signed-in markers show as signed in; Cursor shows how to sign in.
  await expect(page.getByTestId("setup-account-claude")).toContainText("Signed in")
  await expect(page.getByTestId("setup-account-cursor")).toContainText("Sign in from the Cursor app")
  await page.screenshot({ path: shot("setup-1-accounts") })
  await page.getByTestId("setup-next").click()

  // 2. Memory: create the default vault.
  await expect(page.getByRole("heading", { name: "Memory vault" })).toBeVisible()
  await page.getByTestId("setup-vault-create").click()
  await expect(page.getByTestId("setup-vault-ready")).toBeVisible()
  await page.screenshot({ path: shot("setup-2-memory") })
  expect(existsSync(path.join(d.data, "memory"))).toBe(true)
  // The existing-folder path shows the snippet and never writes config.
  await page.getByTestId("setup-vault-existing").click()
  await page.getByTestId("setup-vault-picker").getByLabel("Folder path").fill(code)
  await page.getByTestId("setup-vault-picker").getByRole("button", { name: "Go" }).click()
  await page.getByTestId("setup-vault-picker-pick").click()
  await expect(page.getByTestId("setup-vault-snippet")).toContainText("vault:")
  expect(existsSync(path.join(d.root, "config.yaml"))).toBe(false)
  await page.getByTestId("setup-next").click()

  // 3. Projects: scan a folder, preview, add.
  await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible()
  const picker = page.getByTestId("setup-projects-picker")
  await picker.getByLabel("Folder path").fill(code)
  await picker.getByRole("button", { name: "Go" }).click()
  await expect(picker.getByRole("button", { name: /tide-app/ })).toBeVisible()
  await page.getByTestId("setup-projects-picker-pick").click()
  const table = page.getByTestId("setup-repos")
  await expect(table.locator("tbody tr")).toHaveCount(2)
  await expect(table.getByLabel("Type of tide-app")).toHaveValue("web-app")
  await expect(table.getByLabel("Type of ledger-cli")).toHaveValue("cli")
  await expect(page.getByTestId("setup-projects-preview")).toContainText("id: tide-app")
  await page.screenshot({ path: shot("setup-3-projects") })
  await page.getByTestId("setup-projects-add").click()
  await page.getByRole("dialog").getByRole("button", { name: "Add projects" }).click()
  await expect(page.getByTestId("setup-projects-done")).toContainText("2 projects added")
  const yaml = readFileSync(path.join(d.data, "projects.yaml"), "utf8")
  expect(yaml).toContain("id: tide-app")
  expect(yaml).toContain("id: ledger-cli")
  await page.getByTestId("setup-next").click()

  // 4. Theme: Daylight switches the whole app to light.
  await page.getByTestId("setup-themes").getByRole("button", { name: /Daylight/ }).click()
  await expect(page.locator("html")).not.toHaveClass(/dark/)
  await page.screenshot({ path: shot("setup-4-theme") })
  await page.getByTestId("setup-next").click()

  // 5. Power: changing a mode shows the block to paste.
  await page.getByRole("radiogroup", { name: "Runner mode" }).getByRole("radio", { name: "On demand" }).click()
  await expect(page.getByTestId("setup-power-snippet")).toContainText('runners: "on-demand"')
  await page.screenshot({ path: shot("setup-5-power") })
  await page.getByTestId("setup-next").click()

  // 6. Try it, then finish.
  await expect(page.getByTestId("setup-try")).toBeVisible()
  await page.screenshot({ path: shot("setup-6-try") })
  await page.getByTestId("setup-finish").click()
  await expect(page).toHaveURL(/127\.0\.0\.1:7466\/$/)
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible()
  await expect.poll(() => existsSync(path.join(d.data, "ui.json"))).toBe(true)

  // Setup does not open by itself again, and Settings can run it again.
  await page.reload()
  await expect(page).not.toHaveURL(/\/setup/)
  await page.goto("/settings/general")
  await page.getByTestId("run-setup").click()
  await expect(page).toHaveURL(/\/setup/)

  // The last step opens the sample braindump in the council, without starting it.
  await page.goto("/setup/try")
  await page.getByTestId("setup-try").click()
  await expect(page).toHaveURL(/\/council/)
  await expect(page.getByRole("textbox").first()).toHaveValue(/side projects still build/)
  await expect(page.getByRole("button", { name: /Convene council/ })).toBeEnabled()
})
