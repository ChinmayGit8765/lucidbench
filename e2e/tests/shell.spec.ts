import { expect, test, type Page, type Request, type Response } from "@playwright/test"

import { BASE, calls, makeRepo, projectYaml, shot, startDaemon, type Daemon } from "../harness"

// The shell: nine sidebar entries that fit, old addresses that still land,
// Home with your moves first, one braindump composer, and a stopped docker
// that Home neither calls nor complains about in the console. The fake
// docker has no engine in this spec (no docker.json).
let d: Daemon

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    const repo = makeRepo(dd, "demo")
    const demo = projectYaml("demo", "Demo", repo).replace("    local_path:", "    needs:\n      - what: a signing key\n        status: blocked\n    local_path:")
    // A ui.json from before the infra extensions became opt-in: Runners,
    // Containers and Kubernetes stay added, so docker is wanted and down.
    return { prefs: true, extensions: ["payments", "trello"], projects: `version: 1\nprojects:\n${demo}` }
  })
})
test.afterAll(async () => d?.stop())

const ENTRIES = ["Home", "Ideas", "Work", "Projects", "Memory", "Assistant", "Accounts", "Extensions", "Settings"]
const GONE = ["Overview", "Next up", "Council", "Boards", "Usage", "MCP servers", "Prompt Studio", "System"]

const put = (p: string, body: unknown) =>
  fetch(`${BASE}${p}`, { method: "PUT", headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" }, body: JSON.stringify(body) })

test("the sidebar is nine entries, and it fits a 713 px rail without scrolling", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto("/")
  const nav = page.getByRole("navigation", { name: "Main" })
  for (const name of ENTRIES) await expect(nav.getByRole("button", { name, exact: true })).toBeVisible()
  for (const name of GONE) await expect(nav.getByRole("button", { name, exact: true })).toHaveCount(0)
  // The added extensions sit under Extensions.
  await expect(nav.getByRole("group", { name: "Added extensions" }).getByRole("button", { name: "Payments" })).toBeVisible()
  const box = await nav.evaluate((n) => ({ scroll: n.scrollHeight, client: n.clientHeight }))
  expect(box.client).toBeGreaterThanOrEqual(700)
  expect(box.scroll).toBeLessThanOrEqual(box.client)
  await page.locator("aside").first().screenshot({ path: shot("shell-sidebar") })
})

test("old addresses redirect, and the breadcrumb names the parent", async ({ page }) => {
  const crumb = page.getByRole("navigation", { name: "Breadcrumb" })
  for (const [from, to, trail] of [
    ["/nextup", "/", "Home"],
    ["/council", "/ideas", "Ideas"],
    ["/settings/extensions", "/extensions", "Extensions"],
    ["/settings/extensions/cloud", "/extensions/cloud", "Extensions"],
    ["/assistant/braindump", "/ideas/new/split", "Ideas"],
    ["/boards", "/boards/work", "Work›Boards"],
    ["/system", "/system", "Settings›System"],
    ["/studio", "/studio", "Settings›Prompt Studio"],
    ["/settings", "/settings", "Settings"],
    ["/usage", "/usage", "Accounts›Usage"],
    ["/mcp", "/mcp", "Accounts›MCP servers"],
    ["/payments", "/payments", "Extensions›Payments"],
  ]) {
    await page.goto(from)
    await expect(page, `${from} should land on ${to}`).toHaveURL(`${BASE}${to}`)
    await expect(crumb).toHaveText(trail.replace("›", ""))
  }
  // The family tabs move between the pages of one sidebar entry.
  await page.goto("/accounts")
  await page.getByTestId("family-tabs").getByRole("button", { name: "Usage" }).click()
  await expect(page).toHaveURL(`${BASE}/usage`)
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("button", { name: "Accounts", exact: true })).toHaveAttribute("aria-current", "page")
})

test("Home puts your moves first, then Next up, then at most four tiles; the machine's trouble comes last", async ({ page }) => {
  await page.goto("/")
  const moves = page.getByTestId("needs-your-move")
  await expect(moves.getByText("Demo is blocked on a signing key")).toBeVisible()
  // Docker is down: one row for it, below the user's own moves.
  const rows = moves.getByTestId("attention-row")
  await expect(rows.last()).toHaveAttribute("data-kind", "infra")
  await expect(rows.last()).toContainText("Docker is not running")
  await expect(rows.first()).toHaveAttribute("data-kind", "move")
  const next = page.getByTestId("nextup-section")
  await expect(next).toBeVisible()
  const [a, b] = [await moves.boundingBox(), await next.boundingBox()]
  expect(a!.y).toBeLessThan(b!.y)
  expect(await page.locator("section[aria-label='At a glance'] > div").count()).toBeLessThanOrEqual(4)
  // No raw "exit status" text anywhere a person reads.
  await expect(page.locator("main")).not.toContainText(/exit status \d/)
  await page.screenshot({ path: shot("shell-home"), fullPage: true })
})

/** Expects the one composer, once, focused when it briefs. */
async function expectComposer(page: Page, mode: "brief" | "split") {
  const c = page.getByTestId("braindump-composer")
  await expect(c).toHaveCount(1)
  await expect(c).toHaveAttribute("data-mode", mode)
  if (mode === "brief") await expect(page.getByRole("textbox", { name: "New braindump" })).toBeFocused()
  else await expect(page.getByLabel("Paste a braindump")).toBeVisible()
}

test("every New braindump opens the same composer: Home, the palette, the launcher and the Assistant", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New braindump" }).first().click()
  await expect(page).toHaveURL(`${BASE}/ideas/new`)
  await expectComposer(page, "brief")

  await page.goto("/memory")
  await page.keyboard.press("Control+k")
  await page.keyboard.type("braindump")
  // One action, not one per module.
  const options = page.getByRole("option", { name: /braindump/i })
  await expect(options).toHaveCount(1)
  await options.click()
  await expect(page).toHaveURL(`${BASE}/ideas/new`)
  await expectComposer(page, "brief")

  await page.goto("/projects")
  await page.getByTestId("launcher").last().click()
  await page.getByTestId("launcher-braindump").click()
  await expect(page).toHaveURL(`${BASE}/ideas/new`)
  await expectComposer(page, "brief")

  await page.goto("/assistant")
  await page.getByTestId("assistant-braindump").click()
  await expect(page).toHaveURL(`${BASE}/ideas/new/split`)
  await expectComposer(page, "split")
  // The composer switches between its two modes in place.
  await page.getByTestId("braindump-mode-brief").click()
  await expect(page.getByTestId("braindump-composer")).toHaveAttribute("data-mode", "brief")
})

/** Every /api request and response while Home loads and sits for a few seconds. */
async function watchHome(page: Page, ms = 6000) {
  const requests: string[] = []
  const bad: string[] = []
  const errors: string[] = []
  const onReq = (r: Request) => r.url().includes("/api/") && requests.push(new URL(r.url()).pathname + new URL(r.url()).search)
  const onRes = (r: Response) => r.status() >= 400 && bad.push(`${r.status()} ${new URL(r.url()).pathname}`)
  page.on("request", onReq)
  page.on("response", onRes)
  page.on("console", (m) => m.type() === "error" && errors.push(m.text()))
  await page.goto("/")
  await expect(page.getByTestId("nextup-section")).toBeVisible()
  await page.waitForTimeout(ms)
  page.off("request", onReq)
  page.off("response", onRes)
  return { requests, bad, errors }
}

const DOCKER_ENDPOINTS = /^\/api\/(docker|ci|k8s|cluster|jobs|power|databases|browser)\b/

test("with docker down, Home calls no docker endpoint, gets no 503 and logs no error", async ({ page }) => {
  const before = calls(d).split("\n").filter((l) => l.startsWith("docker ")).length
  const { requests, bad, errors } = await watchHome(page, 12000)
  expect(bad).toEqual([])
  expect(errors).toEqual([])
  expect(requests.filter((p) => DOCKER_ENDPOINTS.test(p))).toEqual([])
  // The daemon backs off too: a handful of docker spawns over the visit, not one per poll.
  const spawned = calls(d).split("\n").filter((l) => l.startsWith("docker ")).length - before
  expect(spawned).toBeLessThanOrEqual(3)
})

test("with docker down, each docker page shows the one calm state and polls nothing of docker", async ({ page }) => {
  const requests: string[] = []
  const bad: string[] = []
  const errors: string[] = []
  page.on("request", (r) => r.url().includes("/api/") && requests.push(new URL(r.url()).pathname))
  page.on("response", (r) => r.status() >= 400 && bad.push(`${r.status()} ${new URL(r.url()).pathname}`))
  page.on("console", (m) => m.type() === "error" && errors.push(m.text()))
  for (const [p, name] of [
    ["/containers", "Containers"],
    ["/kubernetes", "Kubernetes"],
    ["/runners", "Runners & CI"],
  ]) {
    await page.goto(p)
    await expect(page.getByRole("heading", { level: 1, name, exact: true })).toBeVisible()
    await expect(page.getByTestId("docker-down")).toContainText("Docker isn't running")
    await page.waitForTimeout(2500)
  }
  // Retry asks the health check again, and still nothing of docker.
  await page.getByTestId("docker-down").getByRole("button", { name: "Retry" }).click()
  await expect(page.getByTestId("docker-down")).toContainText("Still not answering")
  await page.screenshot({ path: shot("docker-down-runners") })
  // Settings › System shows the same state for the cluster and its jobs.
  await page.goto("/system")
  await expect(page.getByTestId("docker-down")).toBeVisible()
  await page.waitForTimeout(1500)
  expect(bad).toEqual([])
  expect(errors).toEqual([])
  expect(requests.filter((p) => /^\/api\/(docker|ci|k8s|cluster|jobs|databases|browser)(\/|$)/.test(p))).toEqual([])
})

test("a new install asks Home for twelve things at most, and nothing about docker", async ({ page }) => {
  // What a new install saves: the current version, and no extensions.
  expect((await put("/api/prefs", { version: 2, theme: "midnight", overrides: {}, modules: {}, extensions: {}, sprite_board: false })).status).toBe(200)
  try {
    const { requests, bad } = await watchHome(page, 1500)
    expect(bad).toEqual([])
    expect(requests.filter((p) => DOCKER_ENDPOINTS.test(p) || p.includes("docker=1") || p.startsWith("/api/host/"))).toEqual([])
    expect(new Set(requests).size, requests.join("\n")).toBeLessThanOrEqual(12)
    // The infra extensions are not in the sidebar; the gallery still offers them.
    await expect(page.getByRole("navigation", { name: "Main" }).getByRole("button", { name: "Containers" })).toHaveCount(0)
    await page.goto("/extensions/containers")
    await expect(page.getByTestId("ext-containers")).toBeVisible()
  } finally {
    // Back to the seeded sidebar for the tests after this one.
    await put("/api/prefs", { theme: "midnight", overrides: {}, modules: {}, extensions: { payments: { added: true, order: 0 }, trello: { added: true, order: 1 } }, sprite_board: false })
  }
})

test("Accounts, Payments and Trello each have their h1", async ({ page }) => {
  for (const [p, name] of [
    ["/accounts", "Accounts"],
    ["/payments", "Payments"],
    ["/trello", "Trello"],
  ]) {
    await page.goto(p)
    await expect(page.getByRole("heading", { level: 1, name, exact: true })).toBeVisible()
  }
})
