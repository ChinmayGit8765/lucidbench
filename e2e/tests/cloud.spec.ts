import { expect, test } from "@playwright/test"

import { BASE, startDaemon, writeState, type Daemon } from "../harness"

// Cloud: the fake vercel CLI answers whoami, project ls and ls with one ready
// and one errored deployment; every other CLI is simply not installed.
let d: Daemon

const at = (minutes: number) => Date.now() - minutes * 60_000

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    writeState(dd, "vercel.json", {
      whoami: { username: "e2e-user", team: { slug: "e2e-team" } },
      project: {
        projects: [
          { name: "shop-web", latestProductionUrl: "shop-web.example.test", updatedAt: at(60) },
          { name: "docs-site", latestProductionUrl: "docs-site.example.test", updatedAt: at(600) },
        ],
      },
      ls: {
        deployments: [
          { url: "shop-web-abc.example.test", name: "shop-web", state: "READY", target: "production", createdAt: at(60), meta: { githubCommitRef: "main" } },
          { url: "docs-site-def.example.test", name: "docs-site", state: "ERROR", target: "production", createdAt: at(5), meta: { githubCommitRef: "fix-links" } },
        ],
      },
    })
    return { prefs: true, extensions: ["cloud"] }
  })
})
test.afterAll(async () => d?.stop())

test("Cloud lists the deploys a CLI reports and shows the failing one", async ({ page }) => {
  await page.goto("/cloud")
  await expect(page.getByText("e2e-user").first()).toBeVisible()
  await expect(page.getByText("shop-web").first()).toBeVisible()
  await expect(page.getByText("docs-site").first()).toBeVisible()

  // The API agrees: one ready and one failed project, and the failure is the docs site.
  const all = (await (await fetch(`${BASE}/api/cloud/vercel`)).json()) as {
    state: string
    counts: { ready: number; failed: number }
    sections: { id: string; resources: { name: string; status?: string; detail?: string }[] }[]
  }
  expect(all.state).toBe("connected")
  expect(all.counts).toMatchObject({ ready: 1, failed: 1 })
  const projects = all.sections.find((s) => s.id === "projects")!.resources
  expect(projects.find((r) => r.name === "docs-site")).toMatchObject({ status: "failed", detail: "error · production · fix-links" })
  await expect(page.getByText("failed", { exact: false }).first()).toBeVisible()

  // A CLI that is not installed is said so, not hidden.
  const gcloud = (await (await fetch(`${BASE}/api/cloud/gcloud`)).json()) as { state: string }
  expect(gcloud.state).toBe("not_installed")
})
