import { mkdirSync, writeFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { fakeKey } from "../fakes"
import { BASE, startDaemon, type Daemon } from "../harness"

// Accounts and the MCP matrix. The harness gives the fake home a signed-in
// marker for claude, codex and grok; this spec adds an API key from the
// environment and MCP client configs, and checks that nothing secret from
// those files reaches the page.
let d: Daemon

const SECRET = "FAKE-E2E-MCP-SECRET-1f2e"

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    writeFileSync(
      path.join(dd.home, ".claude.json"),
      JSON.stringify({
        mcpServers: {
          notion: { type: "http", url: `https://mcp.notion.example/mcp?token=${SECRET}`, headers: { Authorization: `Bearer ${SECRET}` } },
          filesystem: { type: "stdio", command: "/opt/fs/server", args: [SECRET], env: { KEY: SECRET } },
        },
      }),
    )
    mkdirSync(path.join(dd.home, ".codex"), { recursive: true })
    writeFileSync(
      path.join(dd.home, ".codex", "config.toml"),
      `[mcp_servers.notion]\nurl = "https://mcp.notion.example/mcp"\nbearer_token_env_var = "${SECRET}"\n\n[mcp_servers.codex-only]\ncommand = "/opt/co/server"\n`,
    )
    return { prefs: true, env: { ANTHROPIC_API_KEY: fakeKey("e2e", "anthropic", "0123456789abcdef") } }
  })
})
test.afterAll(async () => d?.stop())

test("Accounts shows what the fake CLIs and the environment sign in", async ({ page }) => {
  await page.goto("/accounts")
  for (const provider of ["claude", "codex", "grok"]) {
    await expect(page.getByText(new RegExp(`^${provider}`, "i")).first()).toBeVisible()
  }
  await expect(page.getByText("Signed in").first()).toBeVisible()
  await expect(page.getByText("Env key").first()).toBeVisible()
  // Presence only: the key itself is never on the page or in the API.
  expect(await page.content()).not.toContain("0123456789abcdef")
  const api = await (await fetch(`${BASE}/api/accounts`)).text()
  expect(api).not.toContain("0123456789abcdef")
  expect(api).toContain("logged_in")
})

test("the MCP matrix renders a row per server and a column per provider, without secrets", async ({ page }) => {
  await page.goto("/mcp")
  await expect(page.getByRole("heading", { name: "Access matrix" })).toBeVisible()
  const notion = page.getByRole("row", { name: /notion/ })
  await expect(notion).toBeVisible()
  await expect(notion).toContainText("mcp.notion.example")
  await expect(page.getByRole("row", { name: /filesystem/ })).toBeVisible()
  await expect(page.getByRole("row", { name: /codex-only/ })).toBeVisible()
  await expect(page.getByRole("heading", { name: "By client" })).toBeVisible()

  const m = (await (await fetch(`${BASE}/api/mcp`)).json()) as { servers: { name: string; providers: string[] }[] }
  expect(m.servers.find((r) => r.name === "notion")!.providers).toEqual(expect.arrayContaining(["claude", "codex"]))
  expect(m.servers.find((r) => r.name === "codex-only")!.providers).toEqual(["codex"])
  expect(await page.content()).not.toContain(SECRET)
  expect(JSON.stringify(m)).not.toContain(SECRET)
})
