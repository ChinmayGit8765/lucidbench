/*
 * Builds what the suite runs against: the fake CLIs (one binary copied under
 * each name), lucidd with the web UI embedded, and the web build itself when
 * web/dist is missing. Set E2E_SKIP_BUILD=1 to reuse what is already built.
 */
import { execFileSync } from "node:child_process"
import { copyFileSync, existsSync, mkdirSync, rmSync } from "node:fs"
import path from "node:path"

import { FAKE_NAMES, fakesBin, fakesDir, lucidd, repoRoot } from "./harness"

export default function globalSetup() {
  const exe = process.platform === "win32" ? ".exe" : ""
  const skip = process.env.E2E_SKIP_BUILD === "1" && existsSync(lucidd) && existsSync(path.join(fakesBin, `claude${exe}`))
  if (skip) return
  const run = (cmd: string, args: string[], cwd: string) => execFileSync(cmd, args, { cwd, stdio: "inherit", shell: process.platform === "win32" && cmd === "npm" })

  if (!existsSync(path.join(repoRoot, "web", "dist", "index.html"))) run("npm", ["run", "build"], path.join(repoRoot, "web"))

  rmSync(fakesBin, { recursive: true, force: true })
  mkdirSync(fakesBin, { recursive: true })
  const fake = path.join(fakesDir, `fakecli${exe}`)
  run("go", ["build", "-o", fake, "./e2e/fakecli"], repoRoot)
  for (const n of FAKE_NAMES) copyFileSync(fake, path.join(fakesBin, `${n}${exe}`))
  run("go", ["build", "-o", lucidd, "./cmd/lucidd"], repoRoot)
}
