import { readFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { BASE, calls, readDocker, startDaemon, writeState, type Daemon } from "../harness"

// Databases: add a Postgres connection, open its on-demand manager (a fake
// docker engine runs it), and let "Sleep everything idle" stop it again.
let d: Daemon

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    writeState(dd, "docker.json", { containers: [], images: [], volumes: [] })
    return { prefs: true, extensions: ["databases"], env: { E2E_SHOP_DB_PASSWORD: "e2e-not-a-secret" } }
  })
})
test.afterAll(async () => d?.stop())

test("a saved Postgres connection opens its manager through docker run, and sleep stops it", async ({ page }) => {
  await page.goto("/databases")
  await page.getByRole("button", { name: "Add connection" }).first().click()
  const dialog = page.getByRole("dialog")
  await dialog.getByPlaceholder("Shop (local)").fill("E2E shop")
  await dialog.getByLabel("Host").fill("127.0.0.1")
  // A port nothing listens on: the health check fails fast and harmlessly.
  await dialog.getByLabel("Port").fill("1")
  await dialog.getByLabel("User").fill("e2e")
  await dialog.getByPlaceholder("SHOP_DB_PASSWORD").fill("E2E_SHOP_DB_PASSWORD")
  await dialog.getByRole("button", { name: "Save connection" }).click()
  await expect(dialog).toBeHidden()

  // The profile file holds the env reference, never a value.
  const saved = readFileSync(path.join(d.data, "databases.yaml"), "utf8")
  expect(saved).toContain("env:E2E_SHOP_DB_PASSWORD")
  expect(saved).not.toContain("e2e-not-a-secret")

  await page.getByRole("button", { name: /Open in pgweb/ }).click()
  await expect(page.getByText("pgweb is ready")).toBeVisible()
  await expect.poll(() => readDocker(d).containers.find((c) => c.name === "lucidbench-mgr-e2e-shop")?.state).toBe("running")

  // The start request: a named, labelled, loopback-only container on the
  // image's own port, with the connection string in an env file (not argv).
  const run = calls(d).split("\n").find((l) => l.startsWith("docker run "))!
  expect(run).toContain("-d --rm --name lucidbench-mgr-e2e-shop")
  expect(run).toMatch(/-p 127\.0\.0\.1:\d+:8081/)
  expect(run).toContain("--memory 256m")
  expect(run).toContain("--env-file")
  expect(run).toContain("sosedoff/pgweb:latest --bind=0.0.0.0 --listen=8081 --readonly")
  expect(run).not.toContain("e2e-not-a-secret")
  expect(calls(d)).toContain("docker run env-file keys: PGWEB_DATABASE_URL")

  // Sleep everything idle stops the manager, which is a power extra.
  const r = await fetch(`${BASE}/api/power/sleep`, { method: "POST", headers: { "X-Lucid-Confirm": "yes" } })
  expect(r.status).toBe(200)
  const body = (await r.json()) as { outcomes: { name: string; stopped: boolean }[] }
  expect(body.outcomes).toContainEqual(expect.objectContaining({ name: "lucidbench-mgr-e2e-shop", stopped: true }))
  expect(readDocker(d).containers.find((c) => c.name === "lucidbench-mgr-e2e-shop")?.state).toBe("exited")
  expect(calls(d)).toContain("docker stop lucidbench-mgr-e2e-shop")
})
