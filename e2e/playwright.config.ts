import { defineConfig, devices } from "@playwright/test"

/**
 * One worker, one lucidd at a time on 127.0.0.1:7466, or E2E_PORT (each spec
 * file starts its own with a fresh data dir). Headless Chromium only.
 */
const port = Number(process.env.E2E_PORT) || 7466

export default defineConfig({
  testDir: "./tests",
  globalSetup: "./global-setup.ts",
  workers: 1,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : [["list"]],
  outputDir: "test-results",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    headless: true,
    viewport: { width: 1360, height: 860 },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    reducedMotion: "no-preference",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"], viewport: { width: 1360, height: 860 } } }],
})
