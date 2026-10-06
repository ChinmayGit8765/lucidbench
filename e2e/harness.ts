/*
 * The end-to-end harness: one lucidd per spec file, on 127.0.0.1:7466, with
 * a temporary home, data dir and config, and a PATH that holds only the fake
 * CLIs (e2e/fakecli), git and the OS's own tools. No real provider CLI, gh
 * or docker can run: startDaemon refuses to start unless every outside CLI
 * name resolves into the fakes folder.
 */
import { execFileSync, spawn, type ChildProcess } from "node:child_process"
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import path from "node:path"
import { fileURLToPath } from "node:url"

export const PORT = 7466
export const BASE = `http://127.0.0.1:${PORT}`
const win = process.platform === "win32"
const exe = win ? ".exe" : ""

const here = path.dirname(fileURLToPath(import.meta.url))
export const repoRoot = path.resolve(here, "..")
export const fakesDir = path.join(here, ".fakes")
export const fakesBin = path.join(fakesDir, "bin")
export const lucidd = path.join(fakesDir, `lucidd${exe}`)

/** The names the fakes answer to; each must resolve into fakesBin. */
export const FAKE_NAMES = ["claude", "codex", "grok", "gh", "docker"]

/** The OS's own lookup tool, by absolute path, so it searches whatever PATH a test sets. */
function lookupTool(): string {
  if (win) return path.join(process.env.SystemRoot ?? "C:\\Windows", "System32", "where.exe")
  for (const dir of (process.env.PATH ?? "").split(path.delimiter)) {
    if (dir && existsSync(path.join(dir, "which"))) return path.join(dir, "which")
  }
  return "/usr/bin/which"
}

/** Every match for name on env's PATH. */
function which(name: string, env: NodeJS.ProcessEnv): string[] {
  try {
    const out = execFileSync(lookupTool(), win ? [name] : ["-a", name], { env, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] })
    return out.split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  } catch {
    return []
  }
}

/**
 * A PATH with the fakes first, then git alone. On Windows git's cmd folder
 * holds nothing else and System32 is the OS's own. Elsewhere /usr/bin often
 * holds gh and docker too (CI machines do), so git is linked into a folder
 * of its own instead.
 */
export function isolatedPath(): string {
  const found = which("git", process.env)
  if (!found.length) throw new Error("git is not on PATH")
  if (win) {
    const sys = process.env.SystemRoot ?? "C:\\Windows"
    return [fakesBin, path.dirname(found[0]), path.join(sys, "System32"), sys].join(path.delimiter)
  }
  const tools = path.join(fakesDir, "tools")
  mkdirSync(tools, { recursive: true })
  const link = path.join(tools, "git")
  if (!existsSync(link)) symlinkSync(found[0], link)
  return [fakesBin, tools].join(path.delimiter)
}

/** Fails loudly when any outside CLI name would resolve outside the fakes. */
export function assertFakesOnly(env: NodeJS.ProcessEnv) {
  for (const n of FAKE_NAMES) {
    const hits = which(n, env)
    if (hits.length === 0) throw new Error(`fake ${n} is not on the test PATH`)
    const outside = hits.filter((h) => path.resolve(path.dirname(h)).toLowerCase() !== path.resolve(fakesBin).toLowerCase())
    if (outside.length) throw new Error(`refusing to run: ${n} resolves outside the fakes folder: ${outside.join(", ")}`)
  }
}

export interface Daemon {
  root: string
  home: string
  data: string
  state: string
  env: NodeJS.ProcessEnv
  stop: () => Promise<void>
}

export interface Seed {
  /** Write ui.json so first-run setup stays closed. */
  prefs?: boolean
  /** Lines of projects.yaml (the whole file). */
  projects?: string
}

/** Runs git in a folder with the test env. */
export function git(cwd: string, env: NodeJS.ProcessEnv, ...args: string[]): string {
  return execFileSync("git", args, { cwd, env, encoding: "utf8" })
}

/**
 * A repository with one commit on main and a bare origin it was pushed to,
 * so Work can make a worktree and Open PR can push.
 */
export function makeRepo(d: Daemon, name = "demo"): string {
  const origin = path.join(d.root, "remotes", `${name}.git`)
  const repo = path.join(d.root, "code", name)
  mkdirSync(origin, { recursive: true })
  mkdirSync(repo, { recursive: true })
  git(origin, d.env, "init", "-q", "--bare", "-b", "main")
  git(repo, d.env, "init", "-q", "-b", "main")
  writeFileSync(path.join(repo, "README.md"), `# ${name}\n`)
  writeFileSync(path.join(repo, "go.mod"), `module example.com/${name}\n\ngo 1.22\n`)
  git(repo, d.env, "add", ".")
  git(repo, d.env, "commit", "-q", "-m", "chore: start")
  git(repo, d.env, "remote", "add", "origin", origin)
  git(repo, d.env, "push", "-q", "-u", "origin", "main")
  git(repo, d.env, "remote", "set-head", "origin", "main")
  return repo
}

/** A projects.yaml entry for a checkout. */
export function projectYaml(id: string, name: string, localPath: string, visibility = "private"): string {
  return `  - id: ${id}\n    name: ${name}\n    category: product\n    type: cli\n    status: active\n    visibility: ${visibility}\n    local_path: ${JSON.stringify(localPath)}\n`
}

async function waitHealthy(child: ChildProcess, log: () => string) {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error(`lucidd exited with ${child.exitCode}:\n${log()}`)
    try {
      const r = await fetch(`${BASE}/api/health`)
      if (r.ok) return
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 200))
  }
  throw new Error(`lucidd did not answer on ${BASE}:\n${log()}`)
}

/**
 * Starts lucidd with a fresh temporary everything. The seed callback runs
 * before the daemon starts, with the env and folders ready.
 */
export async function startDaemon(seed?: (d: Daemon) => Seed | void): Promise<Daemon> {
  if (!existsSync(lucidd)) throw new Error("lucidd is not built; the global setup builds it")
  const root = mkdtempSync(path.join(tmpdir(), "lucid-e2e-"))
  const home = path.join(root, "home")
  const data = path.join(root, "data")
  const state = path.join(root, "state")
  for (const d of [home, data, state, path.join(home, "AppData", "Roaming"), path.join(home, "AppData", "Local"), path.join(home, ".config")]) mkdirSync(d, { recursive: true })
  // Signed-in markers for the fake accounts: empty files, no secrets.
  mkdirSync(path.join(home, ".claude"), { recursive: true })
  writeFileSync(path.join(home, ".claude", ".credentials.json"), "{}\n")
  for (const p of [".codex", ".grok"]) {
    mkdirSync(path.join(home, p), { recursive: true })
    writeFileSync(path.join(home, p, "auth.json"), "{}\n")
  }
  const gitconfig = path.join(root, "gitconfig")
  writeFileSync(gitconfig, "[user]\n\tname = Lucid Test\n\temail = lucid-test@example.invalid\n[init]\n\tdefaultBranch = main\n[safe]\n\tdirectory = *\n[commit]\n\tgpgsign = false\n")

  const env: NodeJS.ProcessEnv = {
    PATH: isolatedPath(),
    HOME: home,
    USERPROFILE: home,
    APPDATA: path.join(home, "AppData", "Roaming"),
    LOCALAPPDATA: path.join(home, "AppData", "Local"),
    XDG_CONFIG_HOME: path.join(home, ".config"),
    TMP: path.join(root, "tmp"),
    TEMP: path.join(root, "tmp"),
    TMPDIR: path.join(root, "tmp"),
    LUCID_DATA_DIR: data,
    LUCID_CONFIG: path.join(root, "config.yaml"),
    LUCID_ADDR: `127.0.0.1:${PORT}`,
    LUCID_E2E_STATE: state,
    GIT_CONFIG_GLOBAL: gitconfig,
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_TERMINAL_PROMPT: "0",
  }
  mkdirSync(env.TMP!, { recursive: true })
  if (win) {
    env.SystemRoot = process.env.SystemRoot
    env.PATHEXT = process.env.PATHEXT ?? ".COM;.EXE;.BAT;.CMD"
    env.ComSpec = process.env.ComSpec
  }
  assertFakesOnly(env)

  const d: Daemon = { root, home, data, state, env, stop: async () => {} }
  const s = seed?.(d) ?? {}
  if (s.prefs) writeFileSync(path.join(data, "ui.json"), JSON.stringify({ theme: "midnight", overrides: {}, modules: {}, extensions: {}, sprite_board: false }))
  if (s.projects !== undefined) writeFileSync(path.join(data, "projects.yaml"), s.projects)

  const logFile = path.join(root, "lucidd.log")
  let log = ""
  const child = spawn(lucidd, [], { env, cwd: root, stdio: ["ignore", "pipe", "pipe"], windowsHide: true })
  child.stdout?.on("data", (b) => (log += b))
  child.stderr?.on("data", (b) => (log += b))
  await waitHealthy(child, () => log)
  d.stop = async () => {
    writeFileSync(logFile, log)
    if (child.exitCode === null) {
      child.kill()
      await new Promise((r) => {
        child.once("exit", r)
        setTimeout(r, 5000)
      })
    }
    if (!process.env.E2E_KEEP) {
      try {
        rmSync(root, { recursive: true, force: true, maxRetries: 5, retryDelay: 300 })
      } catch {
        /* a worktree file still locked on Windows: the OS temp cleaner gets it */
      }
    }
  }
  return d
}

/** Reads the fake CLIs' call log. */
export function calls(d: Daemon): string {
  try {
    return readFileSync(path.join(d.state, "calls.log"), "utf8")
  } catch {
    return ""
  }
}

/** Where screenshots for the report go (gitignored). */
export const shotsDir = path.join(here, "shots")
export const shot = (name: string) => path.join(shotsDir, `${name}.png`)
