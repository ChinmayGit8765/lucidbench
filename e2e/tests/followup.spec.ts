import { mkdirSync, writeFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { BASE, calls, makeRepo, projectYaml, REMOTE_PORT, shot, startDaemon, type Daemon } from "../harness"

// Follow-ups: a Work session waits after each turn, takes the next message in
// the same worktree, and ends when you say so. The phone remote can send the
// next message too, so the remote listens on its loopback port here.
const REMOTE = `http://127.0.0.1:${REMOTE_PORT}`
const CONFIRM = { "X-Lucid-Confirm": "yes" }
/** The session id the fake claude reports (e2e/fakecli). */
const CLAUDE_SESSION = "0e2e0e2e-1111-4222-8333-444455556666"

let d: Daemon

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    const repo = makeRepo(dd, "demo")
    mkdirSync(path.join(dd.data, "remote"), { recursive: true })
    writeFileSync(path.join(dd.data, "remote", "settings.json"), JSON.stringify({ enabled: true, mode: "lan", address: "127.0.0.1", port: REMOTE_PORT }))
    return { prefs: true, projects: `version: 1\nprojects:\n${projectYaml("demo", "Demo", repo)}` }
  })
})
test.afterAll(async () => d?.stop())

interface Session {
  id: string
  status: string
  turns?: { n: number; mode: string; status: string }[]
  pr_url?: string
  diff?: { commits: unknown[] }
}

const getSession = async (id: string) => (await (await fetch(`${BASE}/api/work/sessions/${id}`)).json()) as Session

/** Starts a session through the API and waits until its first turn is over. */
async function startWaiting(prompt: string): Promise<string> {
  const r = await fetch(`${BASE}/api/work/sessions`, {
    method: "POST",
    headers: { ...CONFIRM, "Content-Type": "application/json" },
    body: JSON.stringify({ project: "demo", prompt, provider: "claude" }),
  })
  expect(r.status).toBe(201)
  const { id } = (await r.json()) as Session
  await expect.poll(async () => (await getSession(id)).status, { timeout: 60_000 }).toBe("waiting")
  return id
}

test("a session waits, takes a follow-up as turn 2, opens a PR and ends", async ({ page }) => {
  test.setTimeout(150_000)
  const id = await startWaiting("Write a build report for the side projects")

  // The agent waits for you: a banner, the composer, the provider chip and turn 1.
  await page.goto(`/work/${id}`)
  await expect(page.getByTestId("waiting-banner")).toContainText("waiting for you")
  await expect(page.getByTestId("composer-chip")).toContainText("Claude")
  await expect(page.getByTestId("turn-indicator")).toHaveText("turn 1")
  await expect(page.getByTestId("waiting-footer")).toBeVisible()
  const input = page.getByTestId("composer-input")
  await expect(input).toBeFocused()

  // Shift+Enter is a new line; Enter sends.
  await input.fill("Also list the projects that failed")
  await input.press("Shift+Enter")
  await input.pressSequentially("and how long each one took.")
  await expect(input).toHaveValue("Also list the projects that failed\nand how long each one took.")
  await page.waitForTimeout(300)
  await page.screenshot({ path: shot("work-session-waiting-composer") })

  // The Overview has it as "1 session waiting for you".
  const overview = await page.context().newPage()
  await overview.goto("/")
  await expect(overview.getByText("1 session waiting for you")).toBeVisible()
  await overview.close()

  await input.press("Enter")
  // Turn 2 runs in the same worktree, resuming claude's own session.
  await expect(page.getByTestId("turn-separator")).toContainText("Turn 2")
  await expect(page.getByTestId("turn-separator")).toContainText("resumed with claude --resume")
  await expect(page.getByTestId("composer-stop")).toBeVisible()
  await expect(page.getByTestId("waiting-banner")).toBeVisible({ timeout: 60_000 })
  await expect(page.getByTestId("turn-indicator")).toHaveText("turn 2")
  await expect(page.getByTestId("turn-end")).toHaveCount(2)
  await expect(page.getByText("Done: the follow-up is in NOTES.md and committed.")).toBeVisible()
  await expect(page.getByTestId("waiting-footer")).toContainText("2 turns")
  expect(calls(d)).toContain(`claude -p --resume ${CLAUDE_SESSION}`)
  const s = await getSession(id)
  expect(s.turns?.map((t) => `${t.n}:${t.mode}:${t.status}`)).toEqual(["1:first:done", "2:resume:done"])
  expect(s.diff?.commits.length).toBe(2)
  await page.getByTestId("turn-separator").scrollIntoViewIfNeeded()
  await page.waitForTimeout(300)
  await page.screenshot({ path: shot("work-session-turn-2") })

  // Open PR while it waits: the fake gh opens it, and the session keeps waiting.
  await page.getByTestId("composer-open-pr").click()
  await page.getByRole("dialog").getByRole("button", { name: "Push and open PR" }).click()
  await expect(page.getByRole("link", { name: /View PR/ })).toBeVisible()
  expect(calls(d)).toMatch(/^gh pr create --draft/m)
  expect((await getSession(id)).status).toBe("waiting")

  // End: done, the composer goes away, and a follow-up is refused.
  await page.getByTestId("end-session").click()
  await page.getByRole("dialog").getByRole("button", { name: "End session" }).click()
  await expect(page.getByTestId("composer")).toHaveCount(0)
  await expect.poll(async () => (await getSession(id)).status).toBe("done")
  const again = await fetch(`${BASE}/api/work/sessions/${id}/followup`, { method: "POST", headers: { ...CONFIRM, "Content-Type": "application/json" }, body: JSON.stringify({ prompt: "more" }) })
  expect(again.status).toBe(409)
})

test("the phone sends a follow-up to a waiting session", async ({ browser }) => {
  test.setTimeout(150_000)
  const id = await startWaiting("Write a build report for the phone")

  // Pair through the QR link.
  const r = await fetch(`${BASE}/api/remote/pair`, { method: "POST", headers: CONFIRM })
  const { url } = (await r.json()) as { url: string }
  const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true })
  const p = await phone.newPage()
  await p.goto(url)
  await p.getByRole("button", { name: "Pair this phone" }).click()
  await p.getByRole("dialog", { name: "Pair this phone?" }).getByRole("button", { name: "Pair", exact: true }).click()
  await expect(p.getByRole("heading", { name: "Needs you" })).toBeVisible()
  await expect(p.getByText("The agent is waiting for your follow-up").first()).toBeVisible()

  // The session: waiting for you, with a follow-up button and no "not supported" note.
  await p.goto(`${REMOTE}/r#/session/${id}`)
  await expect(p.locator(".status").first()).toHaveText("waiting for you")
  await expect(p.getByText(/not available yet/)).toHaveCount(0)
  await p.getByTestId("follow-up").click()
  const sheet = p.getByRole("dialog", { name: "Send a follow-up" })
  await sheet.getByLabel("Follow-up").fill("Add the failing ones at the top")
  await p.waitForTimeout(400) // the sheet slides in
  await p.screenshot({ path: shot("remote-phone-follow-up") })
  await sheet.getByRole("button", { name: "Send", exact: true }).click()

  // The tail shows turn 2, and the session waits again once it is done.
  await expect(p.getByTestId("turn")).toContainText("Turn 2")
  await expect(p.getByTestId("tail")).toContainText("Add the failing ones at the top")
  await expect(p.locator(".status").first()).toHaveText("waiting for you", { timeout: 60_000 })
  await expect(p.getByTestId("tail")).toContainText("Done: the follow-up is in NOTES.md and committed.")
  await p.screenshot({ path: shot("remote-phone-turn-2") })
  await phone.close()

  const s = await getSession(id)
  expect(s.turns?.map((t) => `${t.n}:${t.status}`)).toEqual(["1:done", "2:done"])
  // The desktop's audit log has the follow-up.
  const audit = (await (await fetch(`${BASE}/api/remote/audit`)).json()) as { action: string; result: string }[]
  expect(audit.some((a) => a.action === "follow_up" && a.result === "ok")).toBe(true)
})
