import { createServer, type Server } from "node:http"
import type { AddressInfo } from "node:net"

import { expect, test } from "@playwright/test"

import { shot, startDaemon, type Daemon } from "../harness"

/**
 * The update check against a fake GitHub Releases server: lucidd reads its
 * base URL from LUCID_E2E_UPDATE_URL, an env var only these tests set. The
 * e2e lucidd is a "dev" build, which against a test feed counts as older
 * than any release.
 */

let hits = 0

function fakeReleases(): Promise<Server> {
  const releases = [
    {
      tag_name: "v0.3.0-beta.5",
      name: "Lucidbench v0.3.0-beta.5",
      prerelease: true,
      draft: false,
      html_url: "https://example.invalid/releases/v0.3.0-beta.5",
      body: "## 0.3.0-beta.5\n\n- Pace and plan on the Usage page.\n- An update check.",
      assets: [{ name: "Lucidbench_0.3.0-beta.5_x64-setup.exe", browser_download_url: "https://example.invalid/dl/setup.exe" }],
    },
    { tag_name: "v0.3.0-beta.4", prerelease: true, draft: false, html_url: "https://example.invalid/releases/v0.3.0-beta.4", body: "", assets: [] },
    { tag_name: "v0.4.0", prerelease: false, draft: true, html_url: "https://example.invalid/draft", body: "", assets: [] },
  ]
  const srv = createServer((req, res) => {
    hits++
    if (req.url?.startsWith("/repos/ChinmayGit8765/lucidbench/releases")) {
      res.writeHead(200, { "Content-Type": "application/json" })
      res.end(JSON.stringify(releases))
      return
    }
    res.writeHead(404)
    res.end()
  })
  return new Promise((resolve) => srv.listen(0, "127.0.0.1", () => resolve(srv)))
}

test.describe("with a newer release", () => {
  let d: Daemon
  let srv: Server
  test.beforeAll(async () => {
    srv = await fakeReleases()
    const url = `http://127.0.0.1:${(srv.address() as AddressInfo).port}`
    d = await startDaemon((dd) => {
      dd.env.LUCID_E2E_UPDATE_URL = url
      return { prefs: true }
    })
  })
  test.afterAll(async () => {
    await d?.stop()
    await new Promise((r) => srv?.close(r))
  })

  test("a quiet banner links to the notes and the download, and hides until the next version", async ({ page }) => {
    await page.goto("/")
    const banner = page.getByTestId("update-banner")
    await expect(banner).toContainText("Lucidbench 0.3.0-beta.5 is out (beta). You have dev.")
    await expect(banner.getByRole("link", { name: "Release notes" })).toHaveAttribute("href", "https://example.invalid/releases/v0.3.0-beta.5")
    await expect(banner.getByRole("link", { name: "Download" })).toHaveAttribute("href", "https://example.invalid/dl/setup.exe")
    await page.waitForTimeout(400)
    await page.screenshot({ path: shot("update-banner") })

    // A reload reads the cache: GitHub is asked once.
    const before = hits
    await page.reload()
    await expect(banner).toBeVisible()
    expect(hits).toBe(before)

    await banner.getByRole("button", { name: "Hide until the next version" }).click()
    await expect(banner).toHaveCount(0)
    await page.reload()
    await page.waitForTimeout(800)
    await expect(banner).toHaveCount(0)
  })

  test("Settings > General checks for updates on request", async ({ page }) => {
    await page.goto("/settings/general")
    const status = page.getByTestId("update-status")
    await expect(status).toContainText("0.3.0-beta.5")
    await expect(status).toContainText("update available")
    // Turning checks off is a switch that saves update_check to config.yaml.
    await expect(status.getByRole("switch", { name: "Check for updates" })).toHaveAttribute("aria-checked", "true")
    await page.waitForTimeout(1000)
    await page.getByTestId("update-check").click()
    await expect(page.getByText("Lucidbench 0.3.0-beta.5 is out.")).toBeVisible()
    await page.getByTestId("update-status").scrollIntoViewIfNeeded()
    await page.waitForTimeout(300)
    await page.screenshot({ path: shot("update-settings") })
  })
})

test.describe("offline", () => {
  let d: Daemon
  test.beforeAll(async () => {
    // Nothing listens on port 1: every check fails to connect.
    d = await startDaemon((dd) => {
      dd.env.LUCID_E2E_UPDATE_URL = "http://127.0.0.1:1"
      return { prefs: true }
    })
  })
  test.afterAll(async () => d?.stop())

  test("an unreachable GitHub shows no banner and no error toast", async ({ page }) => {
    await page.goto("/")
    await expect(page.getByRole("heading").first()).toBeVisible()
    await page.waitForTimeout(2500)
    await expect(page.getByTestId("update-banner")).toHaveCount(0)
    await expect(page.locator("[data-sonner-toast]")).toHaveCount(0)
    await page.goto("/settings/general")
    await expect(page.getByTestId("update-status")).toContainText("GitHub could not be reached")
  })
})
