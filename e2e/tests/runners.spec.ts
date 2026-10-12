import { expect, test } from "@playwright/test"

import { fakeServer, type FakeServer } from "../fakes"
import { startDaemon, writeState, type Daemon } from "../harness"

// Runners & CI against a fake GitHub API (LUCID_CI_GITHUB_API_URL, test-only),
// the token from the fake `gh auth token`, and a runner container in the fake
// docker engine.
let d: Daemon
let gh: FakeServer

const iso = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString()
const run = (id: number, name: string, conclusion: string, branch: string) => ({
  id, run_number: id, name, display_title: `${name} on ${branch}`, head_branch: branch, event: "push", status: "completed", conclusion,
  created_at: iso(30), run_started_at: iso(29), updated_at: iso(25), html_url: `https://github.example.test/e2e/app/actions/runs/${id}`, actor: { login: "e2e-user" },
})

function ghHandler(r: { method: string; path: string }) {
  if (r.path === "/repos/e2e/app/actions/runners")
    return { json: { total_count: 1, runners: [{ id: 1, name: "e2e-runner-1", os: "linux", status: "online", busy: false, labels: [{ name: "self-hosted" }] }] } }
  if (r.path === "/repos/e2e/app/actions/runs")
    return { json: { total_count: 2, workflow_runs: [run(102, "CI", "failure", "fix-parser"), run(101, "Release", "success", "main")] } }
  if (r.method === "POST" && r.path === "/repos/e2e/app/actions/runs/102/rerun-failed-jobs") return { status: 201, json: {} }
  return null
}

test.beforeAll(async () => {
  gh = await fakeServer(ghHandler)
  d = await startDaemon((dd) => {
    writeState(dd, "docker.json", {
      containers: [{ name: "e2e-runner-1", image: "example/github-runner:2", state: "running", labels: "", env: ["REPO_URL=https://github.com/e2e/app", "RUNNER_NAME=e2e-runner-1"] }],
    })
    return {
      prefs: true,
      extensions: ["runners"],
      env: { LUCID_CI_GITHUB_API_URL: gh.url, LUCID_CI_GITHUB_REPOS: "e2e/app", LUCID_CI_GITHUB_TOKEN: "env:E2E_NO_SUCH_TOKEN" },
    }
  })
})
test.afterAll(async () => {
  await d?.stop()
  await gh?.close()
})

test("Runners & CI lists runs from GitHub, shows the failing one and re-runs it only after confirming", async ({ page }) => {
  await page.goto("/runners")
  await expect(page.getByText("e2e-runner-1").first()).toBeVisible()
  const failing = page.getByRole("row", { name: /CI/ }).filter({ hasText: "fix-parser" })
  await expect(failing).toBeVisible()
  await expect(page.getByRole("row", { name: /Release/ }).filter({ hasText: "main" })).toBeVisible()
  // The token came from the fake `gh auth token`.
  expect(gh.seen[0].headers.authorization).toBe("Bearer gho_fake_token_for_tests")

  // Cancel: nothing is sent to GitHub.
  await failing.getByRole("button", { name: "Re-run" }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog).toContainText("Re-run failed jobs?")
  await expect(dialog).toContainText("fix-parser")
  await dialog.getByRole("button", { name: "Cancel" }).click()
  await expect(dialog).toBeHidden()
  expect(gh.seen.filter((s) => s.method === "POST")).toHaveLength(0)

  // Confirm: GitHub is asked to re-run only the failed jobs.
  await failing.getByRole("button", { name: "Re-run" }).click()
  await page.getByRole("dialog").getByRole("button", { name: "Re-run failed jobs" }).click()
  await expect.poll(() => gh.seen.filter((s) => s.method === "POST").map((s) => s.path)).toEqual(["/repos/e2e/app/actions/runs/102/rerun-failed-jobs"])
})
