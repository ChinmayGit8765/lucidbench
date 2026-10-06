import { getJSON, sendJSON } from "@/lib/api"

/** GET /api/setup (internal/setup). */
export interface SetupStatus {
  needed: boolean
  ui: boolean
  projects: boolean
  projects_hint: string
}

export interface DirEntry {
  name: string
  path: string
  git: boolean
}

export interface DirListing {
  path: string
  parent?: string
  dirs: DirEntry[]
  truncated?: boolean
}

export interface Candidate {
  id: string
  name: string
  local_path: string
  type: string
  why?: string
  existing?: string
}

export interface ScanResult {
  root: string
  repos: Candidate[]
  truncated?: boolean
}

export interface SetupEntry {
  id: string
  name: string
  local_path: string
  type?: string
}

export interface AddResult {
  added: string[]
  backup?: string
  created?: boolean
  snippet: string
  path_hint: string
}

/** The 409 body when projects.yaml cannot be appended to safely. */
export interface NotAppendable {
  error: string
  snippet: string
  path_hint: string
}

export const SETUP_PATH = "/api/setup"
export const SETUP_ROUTE = "/setup"

/** Set once setup is finished or skipped in this browser, so it never opens by itself again. */
export const SETUP_DONE_KEY = "lucidbench.setup-done"

export const setupApi = {
  status: () => getJSON<SetupStatus>(SETUP_PATH),
  dirs: (path = "") => getJSON<DirListing>(`${SETUP_PATH}/dirs?path=${encodeURIComponent(path)}`),
  scan: (root: string) => sendJSON<ScanResult>(`${SETUP_PATH}/scan`, "POST", { root }),
  preview: (entries: SetupEntry[]) => sendJSON<AddResult>(`${SETUP_PATH}/projects/preview`, "POST", { entries }),
  add: (entries: SetupEntry[]) => sendJSON<AddResult>(`${SETUP_PATH}/projects`, "POST", { entries }),
}

/** A sample braindump for trying the council on the last step. */
export const SAMPLE_BRAINDUMP = `I keep losing track of which of my side projects still build. I want a tiny
script that walks my projects folder, runs each project's own build or test
command, and prints one line per project: name, pass or fail, and how long it
took. No new dependencies, and it must never change anything in the repos.`
