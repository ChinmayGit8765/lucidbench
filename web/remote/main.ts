/*
 * The phone remote: a small page served at /r by lucidd's second listener.
 * It is plain TypeScript and DOM, not React: the whole page is a few KB, where
 * the desktop app's shared React chunk alone is far larger, and a phone on a
 * slow LAN or tailnet link should open it at once.
 *
 * Every element is built with textContent, never innerHTML, so nothing the
 * daemon sends is ever parsed as markup. The device token lives in
 * localStorage and goes out as a Bearer header; every write also sends
 * X-Lucid-Confirm, after a confirm tap on a sheet.
 */
import "./style.css"

const TOKEN_KEY = "lucidbench.remote.token"
const API = "/r/api"
const POLL_MS = 15_000

interface Attention {
  severity: "error" | "warning" | "info"
  kind: string
  title: string
  detail?: string
  target?: string
}
interface WorkView {
  id: string
  title: string
  project: string
  provider: string
  status: string
  started: string
  ended?: string
  events: number
  files: number
  added: number
  deleted: number
  pr_state?: string
  error?: string
  confidential: boolean
}
interface CouncilItem {
  id: string
  title: string
  project?: string
  created: string
  confidential: boolean
}
interface Overview {
  attention: Attention[]
  running: WorkView[]
  awaiting: CouncilItem[]
  ci: { configured: boolean; runs_24h: number; in_progress: number; pass_rate: number | null; failing_repos: string[] } | null
  usage: { tokens: number; cost_usd: number; windows: { provider: string; label: string; used_percent: number }[]; providers: { id: string; label: string; tokens: number }[] } | null
  follow_up: boolean
  generated_at: string
}
interface Brief {
  id: string
  title: string
  project?: string
  status: string
  stage: string
  brief: string
  blockers: string[]
  rounds: number
  card?: string
  confidential: boolean
}
interface EventView {
  time: string
  kind: string
  title?: string
  body?: string
}

const app = document.getElementById("app")!
const sheetRoot = document.getElementById("sheet-root")!

// ---------- tiny DOM helpers ----------

type Child = Node | string | null | undefined | false
function h<K extends keyof HTMLElementTagNameMap>(tag: K, props: Record<string, string | boolean | ((e: Event) => void)> = {}, ...children: Child[]): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag)
  for (const [k, v] of Object.entries(props)) {
    if (typeof v === "function") el.addEventListener(k.replace(/^on/, "").toLowerCase(), v)
    else if (typeof v === "boolean") {
      if (v) el.setAttribute(k, "")
    } else if (k === "class") el.className = v
    else el.setAttribute(k, v)
  }
  for (const c of children) {
    if (c === null || c === undefined || c === false) continue
    el.append(typeof c === "string" ? document.createTextNode(c) : c)
  }
  return el
}

function mount(...nodes: Child[]) {
  app.replaceChildren(...(nodes.filter(Boolean) as Node[]))
}

function ago(iso?: string): string {
  if (!iso) return ""
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 60) return "just now"
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`
  return `${Math.floor(s / 86400)} d ago`
}

function compact(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}k`
  return String(n)
}

// ---------- API ----------

class Unpaired extends Error {}

function token(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

async function api<T>(path: string, init: RequestInit & { write?: boolean } = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" }
  const t = token()
  if (t) headers.Authorization = `Bearer ${t}`
  if (init.write) headers["X-Lucid-Confirm"] = "yes"
  if (init.body) headers["Content-Type"] = "application/json"
  const r = await fetch(API + path, { ...init, headers, cache: "no-store", credentials: "omit" })
  if (r.status === 401 && !path.startsWith("/pair")) {
    try {
      localStorage.removeItem(TOKEN_KEY)
    } catch {
      /* private mode */
    }
    throw new Unpaired()
  }
  if (!r.ok) throw new Error((await r.text()).trim() || `${r.status} ${r.statusText}`)
  return (await r.json()) as T
}

// ---------- the confirm sheet ----------

interface SheetOpts {
  title: string
  body: string
  confirm: string
  danger?: boolean
  /** A textarea for notes; its value goes to run. */
  input?: { label: string; placeholder: string }
  run: (text: string) => Promise<void>
}

function sheet(o: SheetOpts) {
  const error = h("p", { class: "error", role: "alert" })
  const area = o.input ? h("textarea", { "aria-label": o.input.label, placeholder: o.input.placeholder, rows: "4", maxlength: "4000" }) : null
  const close = () => sheetRoot.replaceChildren()
  const go = h("button", { class: o.danger ? "btn danger" : "btn primary", type: "button" }, o.confirm)
  go.addEventListener("click", async () => {
    go.setAttribute("disabled", "")
    error.textContent = ""
    try {
      await o.run(area?.value ?? "")
      close()
    } catch (e) {
      if (e instanceof Unpaired) return unpaired()
      error.textContent = (e as Error).message
      go.removeAttribute("disabled")
    }
  })
  const panel = h(
    "div",
    { class: "sheet", role: "dialog", "aria-modal": "true", "aria-label": o.title },
    h("div", { class: "grip" }),
    h("h2", {}, o.title),
    h("p", { class: "muted" }, o.body),
    area,
    error,
    h("div", { class: "row" }, h("button", { class: "btn", type: "button", onclick: close }, "Cancel"), go),
  )
  const backdrop = h("div", { class: "backdrop", onclick: close })
  sheetRoot.replaceChildren(backdrop, panel)
  ;(area ?? go).focus()
}

// ---------- screens ----------

let pollTimer: number | undefined
let streamAbort: AbortController | undefined
/** Bumped on every screen change, so a late answer for an old screen is dropped. */
let screen = 0

function stopLive() {
  screen++
  if (pollTimer) window.clearTimeout(pollTimer)
  pollTimer = undefined
  streamAbort?.abort()
  streamAbort = undefined
}

function header(title: string, back?: boolean): HTMLElement {
  return h(
    "header",
    { class: "top" },
    back ? h("a", { class: "back", href: "#/", "aria-label": "Back" }, "‹") : h("span", { class: "logo", "aria-hidden": "true" }),
    h("h1", {}, title),
  )
}

function unpaired() {
  stopLive()
  sheetRoot.replaceChildren()
  mount(
    header("Lucidbench"),
    h(
      "section",
      { class: "card center" },
      h("h2", {}, "This phone is not paired"),
      h("p", { class: "muted" }, "On your computer, open Settings › Phone remote, turn the remote on and scan the QR code with this phone's camera. The code works once, for five minutes."),
    ),
  )
}

function pairScreen(code: string) {
  stopLive()
  const guess = /iPhone/.test(navigator.userAgent) ? "iPhone" : /iPad/.test(navigator.userAgent) ? "iPad" : /Android/.test(navigator.userAgent) ? "Android phone" : "Phone"
  const name = h("input", { type: "text", value: guess, maxlength: "60", "aria-label": "Name this phone", autocomplete: "off" })
  const error = h("p", { class: "error", role: "alert" })
  const go = h("button", { class: "btn primary wide", type: "button" }, "Pair this phone")
  go.addEventListener("click", () =>
    sheet({
      title: "Pair this phone?",
      body: "It will see what needs you, follow running agents, stop them and approve briefs. You can revoke it any time in Settings › Phone remote.",
      confirm: "Pair",
      run: async () => {
        const out = await api<{ token: string }>("/pair", { method: "POST", write: true, body: JSON.stringify({ code, name: name.value }) })
        localStorage.setItem(TOKEN_KEY, out.token)
        history.replaceState(null, "", "/r")
        route()
      },
    }),
  )
  mount(header("Pair phone"), h("section", { class: "card" }, h("label", { class: "field" }, h("span", {}, "Name this phone"), name), error, go))
}

function attentionRow(a: Attention): HTMLElement {
  const href = a.kind === "brief" && a.target ? `#/brief/${a.target}` : a.kind === "session" && a.target ? `#/session/${a.target}` : null
  const body = [h("span", { class: `dot ${a.severity}`, "aria-hidden": "true" }), h("div", { class: "grow" }, h("div", { class: "title" }, a.title), a.detail ? h("div", { class: "muted small" }, a.detail) : null)]
  return href ? h("a", { class: "item", href, "data-testid": "attention-item" }, ...body) : h("div", { class: "item", "data-testid": "attention-item" }, ...body)
}

function sessionRow(s: WorkView): HTMLElement {
  return h(
    "a",
    { class: "item", href: `#/session/${s.id}`, "data-testid": "session-row" },
    h("span", { class: `status ${s.status}` }, s.status),
    h(
      "div",
      { class: "grow" },
      h("div", { class: s.confidential ? "title redacted" : "title" }, s.title),
      h("div", { class: "muted small" }, `${s.provider} · ${s.project} · ${ago(s.started)}${s.files ? ` · ${s.files} files +${s.added} −${s.deleted}` : ""}`),
    ),
  )
}

async function home() {
  stopLive()
  const view = h("div", {}, h("p", { class: "muted pad" }, "Loading…"))
  mount(header("Lucidbench"), view)
  const mine = screen
  const load = async () => {
    try {
      const [ov, sessions] = await Promise.all([api<Overview>("/overview"), api<WorkView[]>("/work/sessions")])
      if (mine !== screen) return
      view.replaceChildren(...renderHome(ov, sessions))
    } catch (e) {
      if (mine !== screen) return
      if (e instanceof Unpaired) return unpaired()
      view.replaceChildren(h("p", { class: "error pad" }, (e as Error).message))
    }
    pollTimer = window.setTimeout(load, POLL_MS)
  }
  await load()
}

function renderHome(ov: Overview, sessions: WorkView[]): Node[] {
  const out: Node[] = []
  out.push(
    h(
      "section",
      { class: "card", "aria-label": "Needs attention" },
      h("h2", {}, "Needs you"),
      ov.attention.length ? h("div", { class: "list" }, ...ov.attention.map(attentionRow)) : h("p", { class: "muted" }, "Nothing needs you right now."),
    ),
  )
  if (ov.awaiting.length)
    out.push(
      h(
        "section",
        { class: "card", "aria-label": "Briefs to approve" },
        h("h2", {}, "Briefs to approve"),
        h(
          "div",
          { class: "list" },
          ...ov.awaiting.map((c) =>
            h("a", { class: "item", href: `#/brief/${c.id}` }, h("div", { class: "grow" }, h("div", { class: c.confidential ? "title redacted" : "title" }, c.title), h("div", { class: "muted small" }, `${c.project || "no project"} · ${ago(c.created)}`))),
          ),
        ),
      ),
    )
  out.push(
    h(
      "section",
      { class: "card", "aria-label": "Work sessions" },
      h("h2", {}, `Work · ${ov.running.length} running`),
      sessions.length ? h("div", { class: "list" }, ...sessions.slice(0, 20).map(sessionRow)) : h("p", { class: "muted" }, "No sessions yet."),
    ),
  )
  const stats: Node[] = []
  if (ov.ci?.configured) {
    const pr = ov.ci.pass_rate === null ? "no runs" : `${Math.round(ov.ci.pass_rate * 100)} % pass`
    stats.push(h("div", { class: "stat" }, h("div", { class: "muted small" }, "CI, 24 h"), h("div", { class: "big" }, pr), h("div", { class: "muted small" }, ov.ci.failing_repos.length ? `${ov.ci.failing_repos.length} failing` : `${ov.ci.runs_24h} runs`)))
  }
  if (ov.usage) {
    stats.push(h("div", { class: "stat" }, h("div", { class: "muted small" }, "Tokens today"), h("div", { class: "big" }, compact(ov.usage.tokens)), h("div", { class: "muted small" }, ov.usage.cost_usd ? `$${ov.usage.cost_usd.toFixed(2)} in Lucidbench runs` : "")))
    for (const w of ov.usage.windows.slice(0, 2))
      stats.push(h("div", { class: "stat" }, h("div", { class: "muted small" }, `${w.provider} ${w.label}`), h("div", { class: "big" }, `${Math.round(w.used_percent)} %`), bar(w.used_percent)))
  }
  if (stats.length) out.push(h("section", { class: "stats", "aria-label": "CI and usage" }, ...stats))
  out.push(h("p", { class: "muted small pad" }, `Updated ${ago(ov.generated_at)} · `, h("button", { class: "link", type: "button", onclick: () => route() }, "refresh")))
  return out
}

/** A usage bar; its width is set through CSSOM, which the page's CSP allows (a style attribute it does not). */
function bar(pct: number): HTMLElement {
  const fill = h("i", {})
  fill.style.width = `${Math.min(100, Math.max(0, pct))}%`
  return h("div", { class: "bar" }, fill)
}

function eventNode(ev: EventView): HTMLElement {
  return h("div", { class: `ev ${ev.kind}` }, h("div", { class: "ev-kind" }, ev.title ? `${ev.kind} · ${ev.title}` : ev.kind), ev.body ? h("div", { class: "ev-body" }, ev.body) : null)
}

/** Reads server-sent events from a fetch body, so the Bearer header can be sent (EventSource cannot). */
async function tail(id: string, onEvent: (ev: EventView) => void, onSession: (s: WorkView) => void, signal: AbortSignal) {
  const r = await fetch(`${API}/work/sessions/${encodeURIComponent(id)}/events`, { headers: { Authorization: `Bearer ${token() ?? ""}`, Accept: "text/event-stream" }, signal, cache: "no-store" })
  if (r.status === 401) throw new Unpaired()
  if (!r.ok || !r.body) throw new Error((await r.text()).trim() || `${r.status}`)
  const reader = r.body.pipeThrough(new TextDecoderStream()).getReader()
  let buf = ""
  for (;;) {
    const { value, done } = await reader.read()
    if (done) return
    buf += value
    let i: number
    while ((i = buf.indexOf("\n\n")) >= 0) {
      const block = buf.slice(0, i)
      buf = buf.slice(i + 2)
      let event = ""
      let data = ""
      for (const line of block.split("\n")) {
        if (line.startsWith("event: ")) event = line.slice(7)
        else if (line.startsWith("data: ")) data += line.slice(6)
      }
      if (event === "end") return
      if (!data) continue
      if (event === "session") onSession(JSON.parse(data) as WorkView)
      else if (event === "") onEvent(JSON.parse(data) as EventView)
    }
  }
}

async function sessionScreen(id: string) {
  stopLive()
  const status = h("span", { class: "status" }, "…")
  const meta = h("div", { class: "muted small" })
  const title = h("h2", {})
  const actions = h("div", { class: "row" })
  const log = h("div", { class: "log", "data-testid": "tail", "aria-live": "off" })
  const note = h("p", { class: "muted small" })
  mount(header("Session", true), h("section", { class: "card" }, h("div", { class: "row between" }, title, status), meta, actions, note), h("section", { class: "card" }, h("h2", {}, "Live tail"), log))

  let follow = false
  try {
    follow = (await api<Overview>("/overview")).follow_up
  } catch {
    /* the tail still works */
  }
  const show = (s: WorkView) => {
    title.textContent = s.title
    title.className = s.confidential ? "redacted" : ""
    status.textContent = s.status
    status.className = `status ${s.status}`
    meta.textContent = `${s.provider} · ${s.project} · started ${ago(s.started)}${s.files ? ` · ${s.files} files +${s.added} −${s.deleted}` : ""}${s.pr_state ? ` · PR ${s.pr_state}` : ""}`
    actions.replaceChildren()
    if (s.status === "running")
      actions.append(
        h("button", { class: "btn danger", type: "button", "data-testid": "stop", onclick: () =>
          sheet({
            title: "Stop this agent?",
            body: "The CLI and everything it started are ended at once. The worktree and what it committed stay.",
            confirm: "Stop agent",
            danger: true,
            run: async () => show(await api<WorkView>(`/work/sessions/${encodeURIComponent(id)}/stop`, { method: "POST", write: true })),
          }),
        }, "Stop"),
      )
    note.textContent = s.status !== "running" && !follow ? "Follow-up prompts are not available yet: a session runs once. Start a new one on the desktop." : ""
  }
  const ctl = new AbortController()
  streamAbort = ctl
  let n = 0
  try {
    await tail(
      id,
      (ev) => {
        log.append(eventNode(ev))
        if (++n > 400) log.firstElementChild?.remove()
        log.scrollTop = log.scrollHeight
      },
      show,
      ctl.signal,
    )
    if (!n) log.append(h("p", { class: "muted" }, "No events."))
  } catch (e) {
    if (ctl.signal.aborted) return
    if (e instanceof Unpaired) return unpaired()
    log.append(h("p", { class: "error" }, (e as Error).message))
  }
}

/** A brief's Markdown as plain blocks: headings and paragraphs, never parsed as HTML. */
function briefNodes(md: string): Node[] {
  const out: Node[] = []
  let para: string[] = []
  const flush = () => {
    const t = para.join("\n").trim()
    if (t) out.push(h("p", { class: "pre" }, t))
    para = []
  }
  for (const line of md.split("\n")) {
    const m = /^(#{1,4})\s+(.*)$/.exec(line.trim())
    if (m) {
      flush()
      out.push(h(m[1].length <= 1 ? "h2" : "h3", {}, m[2]))
    } else if (line.trim() === "") flush()
    else para.push(line)
  }
  flush()
  return out
}

async function briefScreen(id: string) {
  stopLive()
  const view = h("div", {}, h("p", { class: "muted pad" }, "Loading…"))
  mount(header("Brief", true), view)
  let b: Brief
  try {
    b = await api<Brief>(`/council/sessions/${encodeURIComponent(id)}`)
  } catch (e) {
    if (e instanceof Unpaired) return unpaired()
    view.replaceChildren(h("p", { class: "error pad" }, (e as Error).message))
    return
  }
  const approve = (anyway: boolean) =>
    sheet({
      title: anyway ? "Approve with open blockers?" : "Approve this brief?",
      body: anyway
        ? `A critic still has ${b.blockers.length} blocker(s). Approving records that you accepted them, and adds a card to the work board's Ready column.`
        : "The brief is marked approved and a card is added to the work board's Ready column.",
      confirm: anyway ? "Approve anyway" : "Approve and add card",
      run: async () => {
        await api(`/council/sessions/${encodeURIComponent(id)}/approve`, { method: "POST", write: true, body: JSON.stringify({ approved_with_blockers: anyway }) })
        await briefScreen(id)
      },
    })
  const draft = b.status === "draft"
  view.replaceChildren(
    h(
      "section",
      { class: "card" },
      h("div", { class: "row between" }, h("h2", { class: b.confidential ? "redacted" : "" }, b.title), h("span", { class: `status ${b.status}` }, b.status === "draft" ? "waiting for you" : b.status)),
      h("div", { class: "muted small" }, `${b.project || "no project"} · ${b.rounds} round(s)${b.card ? " · on the work board" : ""}`),
      b.blockers.length ? h("div", { class: "blockers" }, h("strong", {}, `${b.blockers.length} open blocker(s)`), h("ul", {}, ...b.blockers.map((x) => h("li", {}, x)))) : null,
      draft
        ? h(
            "div",
            { class: "row" },
            h("button", { class: "btn primary", type: "button", "data-testid": "approve", onclick: () => approve(b.blockers.length > 0) }, "Approve"),
            h("button", { class: "btn", type: "button", "data-testid": "send-back", onclick: () =>
              sheet({
                title: "Send it back",
                body: "The council runs one more round with your notes.",
                confirm: "Send back",
                input: { label: "What should change", placeholder: "What should change?" },
                run: async (notes) => {
                  await api(`/council/sessions/${encodeURIComponent(id)}/send-back`, { method: "POST", write: true, body: JSON.stringify({ notes }) })
                  await briefScreen(id)
                },
              }),
            }, "Send back"),
          )
        : null,
    ),
    h("article", { class: "card brief" }, ...briefNodes(b.brief)),
  )
}

function route() {
  const hash = location.hash
  const pair = /^#pair=([A-Za-z0-9]+)$/.exec(hash)
  if (pair) return pairScreen(pair[1])
  if (!token()) return unpaired()
  const m = /^#\/(session|brief)\/([A-Za-z0-9_-]+)$/.exec(hash)
  if (m?.[1] === "session") return void sessionScreen(m[2])
  if (m?.[1] === "brief") return void briefScreen(m[2])
  return void home()
}

window.addEventListener("hashchange", route)
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible" && (location.hash === "" || location.hash === "#/")) route()
})
route()
