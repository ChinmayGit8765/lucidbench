import { expect, test } from "@playwright/test"

import { fakeKey, fakeServer, type FakeServer } from "../fakes"
import { BASE, startDaemon, type Daemon } from "../harness"

// Linear and Trello against fake servers on loopback ports: the keys are
// env references and the API URLs are the documented overrides.
let d: Daemon
let linear: FakeServer
let trello: FakeServer

const send = (p: string, method: string, body?: unknown) =>
  fetch(`${BASE}${p}`, { method, headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" }, body: body === undefined ? undefined : JSON.stringify(body) })

const state = (id: string, name: string, type: string, position: number) => ({ id, name, type, color: "#888888", position })
const STATES = [state("st1", "Todo", "unstarted", 1), state("st2", "In Progress", "started", 2), state("st3", "Done", "completed", 3)]
const issue = (n: number, title: string, st: (typeof STATES)[number]) => ({
  id: `iss${n}`, identifier: `E2E-${n}`, title, url: `https://linear.example.test/e2e/issue/E2E-${n}/x`, priority: 2, priorityLabel: "High",
  updatedAt: "2026-10-01T10:00:00Z", state: st, team: { id: "team1", key: "E2E" }, project: { id: "proj1", name: "Fixture project" },
  assignee: { id: "me1", name: "Test Viewer" }, labels: { nodes: [{ name: "bug", color: "#cc3333" }] },
})
const ISSUES = [issue(7, "Fix the login redirect", STATES[0]), issue(8, "Write the import docs", STATES[1])]

function linearHandler(r: { body: string }) {
  const q = (JSON.parse(r.body || "{}") as { query?: string }).query ?? ""
  if (q.includes("viewer")) {
    return {
      json: {
        data: {
          viewer: { id: "me1", name: "Test Viewer" },
          teams: { nodes: [{ id: "team1", key: "E2E", name: "E2E team", activeCycle: null, states: { nodes: STATES } }] },
          projects: { nodes: [{ id: "proj1", name: "Fixture project", teams: { nodes: [{ id: "team1" }] } }] },
        },
      },
    }
  }
  if (q.includes("issues(")) return { json: { data: { issues: { nodes: ISSUES, pageInfo: { hasNextPage: false, endCursor: "" } } } } }
  if (q.includes("issue(id")) return { json: { data: { issue: ISSUES[0] } } }
  return { json: { errors: [{ message: "not in the fake" }] } }
}

const TRELLO_CARDS = [
  { id: "card1", name: "Ship the beta", desc: "", idList: "list1", idBoard: "board1", url: "https://trello.example.test/c/AbCd1234/ship", due: null, pos: 1, shortLink: "AbCd1234", labels: [] },
  { id: "card2", name: "Buy a domain", desc: "", idList: "list2", idBoard: "board1", url: "https://trello.example.test/c/EfGh5678/domain", due: null, pos: 1, shortLink: "EfGh5678", labels: [] },
]

function trelloHandler(r: { method: string; path: string }) {
  if (r.path === "/1/members/me/boards") return { json: [{ id: "board1", name: "Launch board", url: "https://trello.example.test/b/board1", dateLastActivity: "2026-10-01T10:00:00Z" }] }
  if (r.path === "/1/boards/board1") return { json: { id: "board1", name: "Launch board", url: "https://trello.example.test/b/board1" } }
  if (r.path === "/1/boards/board1/lists") return { json: [{ id: "list1", name: "To do", pos: 1 }, { id: "list2", name: "Doing", pos: 2 }] }
  if (r.path === "/1/boards/board1/cards") return { json: TRELLO_CARDS }
  const one = /^\/1\/cards\/(\w+)$/.exec(r.path)
  if (one && r.method === "GET") return { json: TRELLO_CARDS.find((c) => c.id === one[1] || c.shortLink === one[1]) }
  return null
}

test.beforeAll(async () => {
  linear = await fakeServer(linearHandler)
  trello = await fakeServer(trelloHandler)
  d = await startDaemon(() => ({
    prefs: true,
    extensions: ["linear", "trello"],
    env: {
      E2E_LINEAR_KEY: fakeKey("lin_api", "e2e"),
      E2E_TRELLO_KEY: "e2etrellokey",
      E2E_TRELLO_TOKEN: "e2etrellotoken",
      LUCID_INTEGRATIONS_LINEAR_TOKEN: "env:E2E_LINEAR_KEY",
      LUCID_INTEGRATIONS_LINEAR_API_URL: `${linear.url}/graphql`,
      LUCID_INTEGRATIONS_TRELLO_KEY: "env:E2E_TRELLO_KEY",
      LUCID_INTEGRATIONS_TRELLO_TOKEN: "env:E2E_TRELLO_TOKEN",
      LUCID_INTEGRATIONS_TRELLO_API_URL: `${trello.url}/1`,
    },
  }))
})
test.afterAll(async () => {
  await d?.stop()
  await linear?.close()
  await trello?.close()
})

test("Linear lists issues by state and a card links to one", async ({ page }) => {
  await page.goto("/linear")
  await expect(page.getByRole("link", { name: "E2E-7: Fix the login redirect, open in Linear" })).toBeVisible()
  await expect(page.getByRole("region", { name: "Todo column" })).toContainText("Fix the login redirect")
  await expect(page.getByRole("region", { name: "In Progress column" })).toContainText("Write the import docs")
  expect(linear.seen[0].headers.authorization).toMatch(/^lin_api_/)

  const r = await send("/api/boards/work/cards", "POST", { title: "Linear link e2e", column: "Inbox" })
  expect(r.status).toBe(201)
  await page.goto("/boards/work")
  await page.getByLabel("Card: Linear link e2e").click()
  await page.getByRole("button", { name: "Link issue" }).click()
  await page.getByLabel("ENG-12 or an issue URL").fill("E2E-7")
  await page.getByRole("button", { name: "Link", exact: true }).click()
  await expect(page.getByRole("link", { name: /Open in Linear/ })).toBeVisible()
  await expect
    .poll(async () => {
      const b = (await (await fetch(`${BASE}/api/boards/work`)).json()) as { cards: { title: string; linear?: string }[] }
      return b.cards.find((c) => c.title === "Linear link e2e")?.linear
    })
    .toBe("E2E-7")
})

test("Trello lists a board's cards and a card links to one", async ({ page }) => {
  await page.goto("/trello")
  await expect(page.getByRole("region", { name: "To do list" })).toContainText("Ship the beta")
  await expect(page.getByRole("region", { name: "Doing list" })).toContainText("Buy a domain")
  expect(trello.seen.some((s) => s.headers.authorization?.includes('oauth_consumer_key="e2etrellokey"'))).toBe(true)

  const r = await send("/api/boards/work/cards", "POST", { title: "Trello link e2e", column: "Inbox" })
  expect(r.status).toBe(201)
  await page.goto("/boards/work")
  await page.getByLabel("Card: Trello link e2e").click()
  await page.getByRole("button", { name: "Link Trello card" }).click()
  await page.getByLabel("A Trello card URL or id").fill("https://trello.com/c/AbCd1234/ship")
  await page.getByRole("button", { name: "Link", exact: true }).click()
  await expect(page.getByRole("link", { name: /Open in Trello/ })).toBeVisible()
  await expect
    .poll(async () => {
      const b = (await (await fetch(`${BASE}/api/boards/work`)).json()) as { cards: { title: string; trello?: string }[] }
      return b.cards.find((c) => c.title === "Trello link e2e")?.trello
    })
    .toBeTruthy()
})
