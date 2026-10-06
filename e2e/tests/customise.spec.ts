import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs"
import path from "node:path"

import { expect, test } from "@playwright/test"

import { BASE, calls, makeRepo, projectYaml, shot, startDaemon, type Daemon } from "../harness"

let d: Daemon
let repo: string
let other: string

test.beforeAll(async () => {
  d = await startDaemon((dd) => {
    repo = makeRepo(dd, "demo")
    other = makeRepo(dd, "other")
    // A finished Work session with an open PR and a cost, so the sections have something to show.
    const s = path.join(dd.data, "work", "sessions", "abcd1234")
    mkdirSync(s, { recursive: true })
    writeFileSync(
      path.join(s, "session.json"),
      JSON.stringify({
        id: "abcd1234", provider: "claude", harness: "mine", project: "demo", repo_path: repo, repo_hint: repo, branch: "lucid/abcd1234-build-report",
        base_ref: "main", base_sha: "", worktree: "", worktree_hint: "", title: "Add the build report", prompt: "", status: "done",
        started: new Date(Date.now() - 3_600_000).toISOString(), ended: new Date().toISOString(), events: 0,
        pr_url: "https://github.com/example/demo/pull/7", pr_state: "open", pr_checked: new Date().toISOString(),
        usage: { provider: "claude", model: "claude-fake", input_tokens: 1200, output_tokens: 300, cost_usd: 0.42, duration_ms: 60000 },
      }),
    )
    // The other project brings its own team in its repository: a claude proposer on sonnet and one codex critic.
    mkdirSync(path.join(other, ".lucid"), { recursive: true })
    writeFileSync(
      path.join(other, ".lucid", "team.yaml"),
      "version: 1\nroles:\n  proposer: {provider: claude, model: sonnet}\n  critic: [{provider: codex, model: gpt-5-codex}]\n  builder: {provider: claude, model: sonnet}\n",
    )
    return { prefs: true, projects: `version: 1\nprojects:\n${projectYaml("demo", "Demo", repo)}${projectYaml("other", "Other", other)}` }
  })
})
test.afterAll(async () => d?.stop())

const sectionsOnDisk = async () => ((await (await fetch(`${BASE}/api/sections`)).json()) as { sections: { id: string }[] }).sections.map((s) => s.id)

test("Overview: add two built-in sections and see live data", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Add section" }).first().click()
  const dialog = page.getByRole("dialog", { name: "Add a section" })
  await dialog.getByRole("button", { name: /PRs waiting for me/ }).click()
  const prs = page.getByLabel("Section: PRs waiting for me")
  await expect(prs).toBeVisible()
  await expect(prs).toContainText("Add the build report")
  await expect(prs.getByRole("link", { name: /PR/ })).toHaveAttribute("href", "https://github.com/example/demo/pull/7")

  await page.getByRole("button", { name: "Add section" }).first().click()
  await page.getByRole("dialog", { name: "Add a section" }).getByRole("button", { name: /This week's spend/ }).click()
  const spend = page.getByLabel("Section: This week's spend")
  await expect(spend).toBeVisible()
  await expect(spend).toContainText("$0.42")
  expect(await sectionsOnDisk()).toEqual(["prs-waiting", "spend-week"])
  await page.getByLabel("Section: This week's spend").scrollIntoViewIfNeeded()
  await page.waitForTimeout(400)
  await page.screenshot({ path: shot("overview-two-sections"), fullPage: true })

  // A section that breaks the rules never reaches the page.
  const evil = await fetch(`${BASE}/api/sections/evil`, {
    method: "PUT",
    headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" },
    body: JSON.stringify({ id: "evil", title: "Config", source: { api: "/api/config" }, view: "list", fields: [{ path: "server.addr" }] }),
  })
  expect(evil.status).toBe(400)
})

test("Settings: describe a section, preview it with live data, save it", async ({ page }) => {
  await page.goto("/settings/sections/describe")
  await page.getByLabel("Describe the section").fill("Council briefs waiting for my approval, newest first")
  await page.getByRole("button", { name: "Generate" }).click()
  const preview = page.getByTestId("section-preview")
  await expect(preview).toContainText("Briefs waiting for me")
  await expect(preview).toContainText("/api/council/sessions")
  await expect(preview).toContainText("Nothing here right now")
  await page.screenshot({ path: shot("section-preview-fake") })
  expect(calls(d)).toMatch(/^claude -p --output-format json --model haiku /m)
  await page.getByRole("button", { name: "Save", exact: true }).click()
  await expect.poll(sectionsOnDisk).toContain("briefs-waiting")
})

test("Settings: the Customise page, and a feature becomes a braindump", async ({ page }) => {
  await page.goto("/settings/customise")
  await expect(page.getByRole("heading", { name: "Reskin" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Sections" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Features" })).toBeVisible()
  await page.screenshot({ path: shot("customise-page"), fullPage: true })
  await page.getByLabel("Describe a feature").fill("A weekly digest of what shipped and what it cost")
  await page.getByRole("button", { name: "Open in the Council" }).click()
  await expect(page).toHaveURL(/\/council$/)
  const dump = page.locator("#braindump")
  await expect(dump).toHaveValue(/This is a feature request for Lucidbench itself/)
  await expect(dump).toHaveValue(/A weekly digest of what shipped/)
})

test("Projects: the Team tab checks the team and saves it to the repository", async ({ page }) => {
  await page.goto("/projects/demo/team")
  const editor = page.getByTestId("team-editor")
  await expect(editor).toContainText("these are")
  await editor.getByLabel("Builder model").fill("gpt-4o")
  await editor.getByLabel("Builder budget in US dollars").fill("0.10")
  const checks = page.getByTestId("team-validation")
  await expect(checks).toContainText('"gpt-4o" does not look like a claude model')
  await expect(checks).toContainText("recent claude runs cost $0.42 on average")
  await page.screenshot({ path: shot("team-tab-validation") })

  await editor.getByLabel("Builder model").fill("sonnet")
  await expect(checks).not.toContainText("does not look like")
  await editor.getByRole("radio", { name: "In the repository" }).click()
  await editor.getByRole("button", { name: "Save team" }).click()
  await page.getByRole("dialog").getByRole("button", { name: "Write .lucid/team.yaml" }).click()
  const file = path.join(repo, ".lucid", "team.yaml")
  await expect.poll(() => existsSync(file)).toBe(true)
  const yaml = readFileSync(file, "utf8")
  expect(yaml).toContain("model: sonnet")
  expect(yaml).toContain("merge: human")
  await expect(editor).toContainText("the project's repository")

  // Import YAML: an auto merge gate is refused. Let the save toast go first,
  // or it can sit over the button.
  await expect(page.locator("[data-sonner-toast]")).toHaveCount(0, { timeout: 15_000 })
  await editor.getByRole("button", { name: "Import YAML" }).click()
  const dlg = page.getByRole("dialog", { name: "Import a team" })
  await dlg.getByLabel("Team YAML").fill("version: 1\ngates: {merge: auto}\n")
  await dlg.getByRole("button", { name: "Check" }).click()
  await expect(dlg).toContainText("gates.merge must be human")
  await expect(dlg.getByRole("button", { name: "Use this team" })).toBeDisabled()
})

test("Council: a project's team picks the proposer, the critics and their models", async ({ page }) => {
  await page.goto("/council")
  await page.locator("#braindump").fill("Report which side projects still build, with a tiny script that prints pass or fail.")
  await page.getByRole("combobox").first().selectOption({ label: "Other" })
  await expect(page.getByTestId("council-team")).toContainText("Proposer Claude (sonnet); critics Codex (gpt-5-codex)")
  const before = calls(d)
  await page.getByRole("button", { name: /Convene council/ }).click()
  await expect(page.getByText("Draft · waiting for you")).toBeVisible({ timeout: 45_000 })
  const run = calls(d).slice(before.length)
  expect(run).toMatch(/^claude .*--model sonnet/m)
  expect(run).toMatch(/^codex .*-m gpt-5-codex/m)
  expect(run).not.toMatch(/^grok /m)
})
