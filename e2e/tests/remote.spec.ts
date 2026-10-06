import { mkdirSync, writeFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { BASE, makeRepo, projectYaml, REMOTE_PORT, shot, startDaemon, type Daemon } from "../harness"

// The phone remote listens on a second port. For the test it binds the
// loopback address; on a real machine the operator picks a LAN or tailnet one.
const REMOTE = `http://127.0.0.1:${REMOTE_PORT}`
const CONFIRM = { "X-Lucid-Confirm": "yes" }

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

interface Council {
  id: string
  status: string
  title: string
}

/** Starts a council run on the desktop API and waits for its draft brief. */
async function draftBrief(): Promise<Council> {
  const r = await fetch(`${BASE}/api/council/sessions`, {
    method: "POST",
    headers: { ...CONFIRM, "Content-Type": "application/json" },
    body: JSON.stringify({ input: "I keep losing track of which side projects still build. A tiny script that prints pass or fail for each.", project: "demo" }),
  })
  expect(r.status).toBe(202)
  const { id } = (await r.json()) as Council
  let s: Council = { id, status: "running", title: "" }
  await expect
    .poll(
      async () => {
        s = (await (await fetch(`${BASE}/api/council/sessions/${id}`)).json()) as Council
        return s.status
      },
      { timeout: 60_000 },
    )
    .toBe("draft")
  return s
}

async function pairCode(): Promise<{ code: string; url: string }> {
  const r = await fetch(`${BASE}/api/remote/pair`, { method: "POST", headers: CONFIRM })
  expect(r.status).toBe(200)
  return (await r.json()) as { code: string; url: string }
}

test("pair a phone through the API, then approve a council brief with its token", async () => {
  test.setTimeout(120_000)
  const status = (await (await fetch(`${BASE}/api/remote`)).json()) as { listening: boolean; address: string }
  expect(status.listening).toBe(true)
  expect(status.address).toBe(`127.0.0.1:${REMOTE_PORT}`)

  const brief = await draftBrief()

  // Pair: the desktop makes a one-time code; the phone exchanges it once.
  const { code, url } = await pairCode()
  expect(url).toBe(`${REMOTE}/r#pair=${code}`)
  const paired = await fetch(`${REMOTE}/r/api/pair`, { method: "POST", headers: { ...CONFIRM, "Content-Type": "application/json" }, body: JSON.stringify({ code, name: "Test phone" }) })
  expect(paired.status).toBe(200)
  const { token } = (await paired.json()) as { token: string }
  expect(token.length).toBeGreaterThan(40)
  const again = await fetch(`${REMOTE}/r/api/pair`, { method: "POST", headers: { ...CONFIRM, "Content-Type": "application/json" }, body: JSON.stringify({ code, name: "Twice" }) })
  expect(again.status).toBe(401)
  const auth = { Authorization: `Bearer ${token}` }

  // The remote is an allowlist: the desktop API is not there, even with a token.
  for (const p of ["/api/config", "/api/accounts", "/api/council/sessions", "/api/remote", "/settings", "/"]) {
    expect((await fetch(`${REMOTE}${p}`, { headers: { ...auth, ...CONFIRM } })).status, p).toBe(404)
  }

  // The overview lists the brief as waiting.
  const ov = (await (await fetch(`${REMOTE}/r/api/overview`, { headers: auth })).json()) as { awaiting: { id: string; title: string }[] }
  expect(ov.awaiting.map((a) => a.id)).toContain(brief.id)

  // Approving needs the confirm header as well as the token.
  const bare = await fetch(`${REMOTE}/r/api/council/sessions/${brief.id}/approve`, { method: "POST", headers: auth })
  expect(bare.status).toBe(403)
  const ok = await fetch(`${REMOTE}/r/api/council/sessions/${brief.id}/approve`, { method: "POST", headers: { ...auth, ...CONFIRM } })
  expect(ok.status).toBe(200)
  const s = (await (await fetch(`${BASE}/api/council/sessions/${brief.id}`)).json()) as Council
  expect(s.status).toBe("approved")
  const board = (await (await fetch(`${BASE}/api/boards/work`)).json()) as { cards: { title: string; column: string }[] }
  expect(board.cards.find((c) => c.title === s.title)?.column).toBe("Ready")

  // The desktop's audit log shows the pairing and the approval; revoking ends the token.
  const audit = (await (await fetch(`${BASE}/api/remote/audit`)).json()) as { action: string }[]
  expect(audit.map((a) => a.action)).toEqual(expect.arrayContaining(["pair", "approve"]))
  const devices = (await (await fetch(`${BASE}/api/remote/devices`)).json()) as { id: string; name: string }[]
  expect(devices.map((x) => x.name)).toContain("Test phone")
  expect(JSON.stringify(devices)).not.toContain(token)
  for (const dev of devices) expect((await fetch(`${BASE}/api/remote/devices/${dev.id}`, { method: "DELETE", headers: CONFIRM })).status).toBe(200)
  expect((await fetch(`${REMOTE}/r/api/overview`, { headers: auth })).status).toBe(401)
})

test("the phone page pairs from the QR link and approves with a confirm tap", async ({ browser, page }) => {
  test.setTimeout(120_000)
  const brief = await draftBrief()
  const { url } = await pairCode()

  const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true })
  const p = await phone.newPage()
  await p.goto(url)
  await p.getByRole("button", { name: "Pair this phone" }).click()
  await p.getByRole("dialog", { name: "Pair this phone?" }).getByRole("button", { name: "Pair", exact: true }).click()
  await expect(p.getByRole("heading", { name: "Needs you" })).toBeVisible()
  expect(p.url()).not.toContain("pair=")
  await expect(p.getByRole("region", { name: "Briefs to approve" })).toContainText(brief.title)
  await p.screenshot({ path: shot("remote-phone-home") })

  await p.getByRole("region", { name: "Briefs to approve" }).getByText(brief.title).click()
  await p.getByTestId("approve").click()
  await expect(p.getByRole("dialog", { name: "Approve this brief?" })).toBeVisible()
  await p.waitForTimeout(400) // the sheet slides in
  await p.screenshot({ path: shot("remote-phone-confirm") })
  await p.getByRole("button", { name: "Approve and add card" }).click()
  await expect(p.getByText("approved", { exact: true })).toBeVisible()
  await phone.close()

  // The desktop's Settings › Phone remote shows the device and the action.
  await page.goto("/settings/remote")
  await expect(page.getByTestId("phone-remote")).toBeVisible()
  await expect(page.getByText(`Listening on 127.0.0.1:${REMOTE_PORT}`)).toBeVisible()
  await expect(page.getByTestId("remote-audit")).toContainText("Approved a brief")
  await page.getByTestId("phone-remote").screenshot({ path: shot("remote-settings") })
})
