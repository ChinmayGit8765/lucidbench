import { useEffect, useMemo, useRef, useState } from "react"
import { AlertTriangle, CircleCheck, FileUp, Info, Lock, Save, ShieldAlert, XCircle } from "lucide-react"
import { toast } from "sonner"

import { ProviderMark, providerInfo } from "@/components/ProviderMark"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Dialog } from "@/components/ui/dialog"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, usePoll } from "@/lib/api"
import {
  checkTeam,
  MODEL_HINTS,
  ROLE_INFO,
  saveTeam,
  SOURCE_LABEL,
  TEAM_PROVIDERS,
  teamPath,
  type RoleName,
  type Team,
  type TeamCheck,
  type TeamEstimate,
  type TeamProblem,
  type TeamProvider,
  type TeamRole,
  type TeamView,
} from "@/lib/team"
import { cn } from "@/lib/utils"
import type { Account } from "@/pages/Accounts"

interface McpRow {
  name: string
  providers: string[]
  inherited: string[]
}

/** The editor's rows: one per seat, a critic has two. */
const ROWS: { key: string; name: RoleName; label: string }[] = [
  { key: "proposer", name: "proposer", label: "Proposer" },
  { key: "critic1", name: "critic", label: "Critic 1" },
  { key: "critic2", name: "critic", label: "Critic 2" },
  { key: "builder", name: "builder", label: "Builder" },
  { key: "reviewer", name: "reviewer", label: "Reviewer" },
  { key: "scout", name: "scout", label: "Scout" },
]

type Draft = Record<string, TeamRole | null>

function toDraft(t: Team): Draft {
  const c = t.roles.critic ?? []
  return {
    proposer: t.roles.proposer ?? null,
    critic1: c[0] ?? null,
    critic2: c[1] ?? null,
    builder: t.roles.builder ?? null,
    reviewer: t.roles.reviewer ?? null,
    scout: t.roles.scout ?? null,
  }
}

function clean(r: TeamRole | null): TeamRole | undefined {
  if (!r || !r.provider) return undefined
  const out: TeamRole = { provider: r.provider }
  if (r.profile && r.profile !== "default") out.profile = r.profile
  if (r.model?.trim()) out.model = r.model.trim()
  if (r.mcp_allow?.length) out.mcp_allow = r.mcp_allow
  if (r.allowed_commands?.length) out.allowed_commands = r.allowed_commands
  if (r.budget_usd !== undefined && !Number.isNaN(r.budget_usd)) out.budget_usd = r.budget_usd
  return out
}

function fromDraft(d: Draft): Team {
  const critic = [clean(d.critic1), clean(d.critic2)].filter((r): r is TeamRole => !!r)
  return {
    version: 1,
    roles: {
      proposer: clean(d.proposer),
      critic: critic.length ? critic : undefined,
      builder: clean(d.builder),
      reviewer: clean(d.reviewer),
      scout: clean(d.scout),
    },
    gates: { approve_brief: "human", open_pr: "human", merge: "human" },
  }
}

/** The daemon's label for a row's seat ("critic" or "critic 2"). */
function seatLabel(key: string, d: Draft): string {
  if (key === "critic1") return d.critic2?.provider ? "critic 1" : "critic"
  if (key === "critic2") return d.critic1?.provider ? "critic 2" : "critic"
  return key
}

type SignIn = "signed_in" | "expired" | "missing"
function signInOf(accounts: Account[] | null, p: string): SignIn {
  const mine = (accounts ?? []).filter((a) => a.provider === p && a.location === "host")
  if (mine.some((a) => a.status === "logged_in")) return "signed_in"
  if (mine.some((a) => a.status === "expired")) return "expired"
  return "missing"
}
const DOT: Record<SignIn, string> = { signed_in: "bg-success", expired: "bg-warning", missing: "bg-neutral" }
const SIGN_LABEL: Record<SignIn, string> = { signed_in: "signed in", expired: "sign-in expired", missing: "not signed in" }

const SEV_ICON = { error: XCircle, warning: AlertTriangle, info: Info } as const
const SEV_TEXT = { error: "text-danger-fg", warning: "text-warning-fg", info: "text-muted-foreground" } as const

function Problems({ list }: { list: TeamProblem[] }) {
  if (list.length === 0) return null
  return (
    <ul className="space-y-1" aria-label="Validation">
      {list.map((p, i) => {
        const Icon = SEV_ICON[p.severity]
        return (
          <li key={i} className={cn("flex items-start gap-1.5 text-xs", SEV_TEXT[p.severity])} data-severity={p.severity}>
            <Icon className="mt-px size-3.5 shrink-0" />
            <span>{p.message}</span>
          </li>
        )
      })}
    </ul>
  )
}

const input =
  "h-8 w-full rounded-md border bg-background/60 px-2 text-xs outline-none transition-colors placeholder:text-subtle-foreground hover:border-border-strong focus-visible:border-ring"

function RoleRow({
  row,
  role,
  accounts,
  mcp,
  problems,
  estimate,
  onChange,
}: {
  row: (typeof ROWS)[number]
  role: TeamRole | null
  accounts: Account[] | null
  mcp: McpRow[]
  problems: TeamProblem[]
  estimate?: TeamEstimate
  onChange: (r: TeamRole | null) => void
}) {
  const p = (role?.provider ?? "") as TeamProvider | ""
  const set = (patch: Partial<TeamRole>) => onChange({ ...(role ?? { provider: "claude" }), ...patch })
  const profiles = p ? (accounts ?? []).filter((a) => a.provider === p && a.location === "host") : []
  const servers = mcp.filter((s) => p && (s.providers.includes(p) || s.inherited.includes(p))).map((s) => s.name)
  const chosen = role?.mcp_allow ?? []
  const chips = [...new Set([...servers, ...chosen])].sort()
  const listId = `models-${row.key}`
  return (
    <tr className="border-t align-top" data-role={row.key}>
      <td className="px-3 py-2.5">
        <div className="text-sm font-medium">{row.label}</div>
        <div className="max-w-40 text-2xs text-subtle-foreground">{ROLE_INFO[row.name]}</div>
      </td>
      <td className="px-2 py-2.5">
        <select
          value={p}
          aria-label={`${row.label} provider`}
          onChange={(e) => (e.target.value ? set({ provider: e.target.value, profile: undefined, model: undefined }) : onChange(null))}
          className={cn(input, "w-32")}
        >
          <option value="">{row.key === "critic2" ? "No second critic" : "Default"}</option>
          {TEAM_PROVIDERS.map((x) => (
            <option key={x} value={x}>
              {providerInfo(x)?.label ?? x}
              {signInOf(accounts, x) === "signed_in" ? "" : ` (${SIGN_LABEL[signInOf(accounts, x)]})`}
            </option>
          ))}
        </select>
        {p && (
          <div className="mt-1 flex items-center gap-1.5 text-2xs text-muted-foreground">
            <span className={cn("size-1.5 rounded-full", DOT[signInOf(accounts, p)])} />
            <ProviderMark provider={p} className="size-3" />
            {SIGN_LABEL[signInOf(accounts, p)]}
          </div>
        )}
        {p && p !== "grok" && profiles.length > 1 && (
          <select value={role?.profile ?? "default"} onChange={(e) => set({ profile: e.target.value })} aria-label={`${row.label} profile`} className={cn(input, "mt-1 w-32")}>
            {profiles.map((a) => (
              <option key={a.name} value={a.name}>
                {a.name}
              </option>
            ))}
          </select>
        )}
      </td>
      <td className="px-2 py-2.5">
        <input
          className={cn(input, "w-36")}
          list={listId}
          value={role?.model ?? ""}
          disabled={!p}
          placeholder={p ? "CLI default" : "—"}
          aria-label={`${row.label} model`}
          onChange={(e) => set({ model: e.target.value })}
        />
        <datalist id={listId}>{p && MODEL_HINTS[p].map((m) => <option key={m} value={m} />)}</datalist>
      </td>
      <td className="px-2 py-2.5">
        {!p ? (
          <span className="text-2xs text-subtle-foreground">—</span>
        ) : chips.length === 0 ? (
          <span className="text-2xs text-subtle-foreground">No MCP servers for {providerInfo(p)?.label}</span>
        ) : (
          <div className="flex max-w-56 flex-wrap gap-1" role="group" aria-label={`${row.label} MCP servers`}>
            {chips.map((name) => {
              const on = chosen.includes(name)
              const missing = !servers.includes(name)
              return (
                <button
                  key={name}
                  type="button"
                  aria-pressed={on}
                  title={missing ? `${name} is not configured for ${p}` : on ? "Allowed for this role" : "Not allowed for this role"}
                  onClick={() => set({ mcp_allow: on ? chosen.filter((x) => x !== name) : [...chosen, name] })}
                  className={cn(
                    "h-5 rounded-full border px-2 text-2xs transition-colors",
                    on ? "border-brand/60 bg-brand-soft text-brand-fg" : "text-muted-foreground hover:text-foreground",
                    missing && "border-warning/60 text-warning-fg line-through",
                  )}
                >
                  {name}
                </button>
              )
            })}
          </div>
        )}
        {row.key === "builder" && p && (
          <input
            className={cn(input, "mt-1.5 w-56 font-mono")}
            value={(role?.allowed_commands ?? []).join(", ")}
            placeholder="allowed commands: the project's defaults"
            aria-label="Builder allowed commands"
            onChange={(e) =>
              set({
                allowed_commands: e.target.value
                  .split(",")
                  .map((x) => x.trim())
                  .filter(Boolean),
              })
            }
          />
        )}
      </td>
      <td className="px-2 py-2.5">
        <div className="flex items-center gap-1">
          <span className="text-xs text-subtle-foreground">$</span>
          <input
            type="number"
            min={0}
            step={0.05}
            className={cn(input, "w-20 tabular-nums")}
            value={role?.budget_usd ?? ""}
            disabled={!p}
            placeholder="none"
            aria-label={`${row.label} budget in US dollars`}
            onChange={(e) => set({ budget_usd: e.target.value === "" ? undefined : Number(e.target.value) })}
          />
        </div>
        {estimate && estimate.runs > 0 && (
          <div className={cn("mt-1 text-2xs tabular-nums", estimate.over ? "font-medium text-warning-fg" : "text-subtle-foreground")}>
            recent ${estimate.avg_usd.toFixed(2)} avg · {estimate.runs} {estimate.runs === 1 ? "run" : "runs"}
          </div>
        )}
      </td>
      <td className="w-6 px-2 py-2.5">
        {problems.some((x) => x.severity === "error") ? (
          <XCircle className="size-4 text-danger" aria-label="error" />
        ) : problems.some((x) => x.severity === "warning") ? (
          <AlertTriangle className="size-4 text-warning" aria-label="warning" />
        ) : p ? (
          <CircleCheck className="size-4 text-success" aria-label="ok" />
        ) : null}
      </td>
    </tr>
  )
}

/** The Team tab of a project: role table, checks against this machine, Save and Import YAML. */
export function TeamEditor({ project }: { project: string }) {
  const view = usePoll<TeamView>(teamPath(project), 600_000)
  const accounts = usePoll<Account[]>("/api/accounts", 60_000)
  const mcp = usePoll<{ servers: McpRow[] }>("/api/mcp", 120_000)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [check, setCheck] = useState<TeamCheck | null>(null)
  const [target, setTarget] = useState<"repo" | "data">("data")
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [importing, setImporting] = useState(false)
  const loaded = useRef(false)

  const v = view.data
  useEffect(() => {
    if (!v || loaded.current) return
    loaded.current = true
    setDraft(toDraft(v.team))
    setTarget(v.source === "data" || !v.targets.repo ? "data" : "repo")
  }, [v])

  const team = useMemo(() => (draft ? fromDraft(draft) : null), [draft])
  // Check the draft against the schema and this machine as it changes.
  useEffect(() => {
    if (!team) return
    const id = setTimeout(() => {
      checkTeam({ team, project }).then(setCheck, (e) => setCheck({ team: null, problems: [{ severity: "error", code: "check", message: errorMessage(e) }], estimates: [], valid: false }))
    }, 300)
    return () => clearTimeout(id)
  }, [team, project])

  if (view.error && !v) return <ErrorState title="Could not read the team" message={view.error.message} onRetry={view.refresh} />
  if (!v || !draft || !team) {
    return (
      <div className="space-y-2 p-5">
        <Skeleton className="h-8" />
        <Skeleton className="h-8" />
        <Skeleton className="h-8" />
      </div>
    )
  }
  const problems = check?.problems ?? v.problems
  const readProblems = v.problems.filter((p) => p.code === "file")
  const byRole = (label: string) => problems.filter((p) => p.role === label)
  const general = [...readProblems, ...problems.filter((p) => !p.role && p.code !== "file")]
  const valid = check ? check.valid : !problems.some((p) => p.severity === "error")
  const estimates = check?.estimates ?? v.estimates
  const path = target === "repo" ? v.targets.repo : v.targets.data

  const doSave = async () => {
    const saved = await saveTeam(project, team, target)
    toast.success("Team saved", { description: saved.path_hint })
    loaded.current = false
    view.refresh()
  }
  const save = () => {
    if (target === "repo") {
      setConfirm({
        title: "Save the team in the repository?",
        description: `This writes ${v.targets.repo} in the project's checkout. Commit it like any other file; nothing is pushed.`,
        confirmLabel: "Write .lucid/team.yaml",
        run: () => doSave().catch((e) => toast.error("Could not save the team", { description: errorMessage(e) })),
      })
      return
    }
    void doSave().catch((e) => toast.error("Could not save the team", { description: errorMessage(e) }))
  }

  return (
    <div className="space-y-4 p-5" data-testid="team-editor">
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <span>
          {v.source === "builtin" ? "No team yet: these are " : "Read from "}
          <span className="font-medium text-foreground">{SOURCE_LABEL[v.source]}</span>
          {v.path_hint && v.source !== "builtin" && <span className="font-mono"> · {v.path_hint}</span>}
        </span>
        <span className="ml-auto flex items-center gap-1.5">
          <Lock className="size-3" /> approve brief · open PR · merge: always you
        </span>
      </div>
      {v.confidential && (
        <div className="flex items-start gap-2 rounded-lg border border-danger/30 bg-danger-soft p-3 text-sm">
          <ShieldAlert className="mt-0.5 size-4 shrink-0 text-danger-fg" />
          <span>
            {v.project_name} is confidential. The team is kept, but Council and Work never send this project to a provider, whatever it says.
          </span>
        </div>
      )}
      <div className="overflow-x-auto rounded-xl border">
        <table className="w-full text-left">
          <thead className="bg-muted/40 text-2xs uppercase tracking-[0.06em] text-subtle-foreground">
            <tr>
              <th className="px-3 py-2 font-medium">Role</th>
              <th className="px-2 py-2 font-medium">Provider</th>
              <th className="px-2 py-2 font-medium">Model</th>
              <th className="px-2 py-2 font-medium">MCP servers</th>
              <th className="px-2 py-2 font-medium">Budget / run</th>
              <th className="px-2 py-2" />
            </tr>
          </thead>
          <tbody>
            {ROWS.map((row) => {
              const label = seatLabel(row.key, draft)
              return (
                <RoleRow
                  key={row.key}
                  row={row}
                  role={draft[row.key]}
                  accounts={accounts.data}
                  mcp={mcp.data?.servers ?? []}
                  problems={draft[row.key]?.provider ? byRole(label) : []}
                  estimate={draft[row.key]?.provider ? estimates.find((e) => e.role === label) : undefined}
                  onChange={(r) => setDraft((d) => ({ ...d!, [row.key]: r }))}
                />
              )
            })}
          </tbody>
        </table>
      </div>

      <div className="space-y-1.5" data-testid="team-validation">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-semibold">Checks</h3>
          {valid ? (
            <StatusPill tone={problems.some((p) => p.severity === "warning") ? "warning" : "success"}>
              {problems.some((p) => p.severity === "warning") ? "valid, with warnings" : "valid"}
            </StatusPill>
          ) : (
            <StatusPill tone="danger">cannot be saved</StatusPill>
          )}
        </div>
        <Problems list={general} />
        <Problems list={problems.filter((p) => p.role)} />
        {problems.length === 0 && <p className="text-xs text-muted-foreground">Every provider is signed in, every MCP server is there, and the models look right.</p>}
      </div>

      <div className="flex flex-wrap items-center gap-3 border-t pt-4">
        <div role="radiogroup" aria-label="Save to" className="flex items-center rounded-lg border bg-muted/50 p-0.5">
          {(["repo", "data"] as const).map((t) => (
            <button
              key={t}
              role="radio"
              aria-checked={target === t}
              disabled={t === "repo" && !v.targets.repo}
              title={t === "repo" && !v.targets.repo ? "The project has no checkout at its local_path" : undefined}
              onClick={() => setTarget(t)}
              className={cn(
                "h-7 rounded-md px-2.5 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-50",
                target === t ? "bg-elevated font-medium text-foreground shadow-card" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t === "repo" ? "In the repository" : "In Lucidbench's data folder"}
            </button>
          ))}
        </div>
        <span className="min-w-0 flex-1 truncate font-mono text-2xs text-subtle-foreground" title={path}>
          {path}
        </span>
        <Button variant="secondary" onClick={() => setImporting(true)}>
          <FileUp /> Import YAML
        </Button>
        <Button onClick={save} disabled={!valid}>
          <Save /> Save team
        </Button>
      </div>
      {v.source === "repo" && target === "data" && (
        <p className="text-xs text-warning-fg">The repository's .lucid/team.yaml is read first, so a team saved in the data folder is used only once that file is gone.</p>
      )}
      <ImportDialog
        open={importing}
        project={project}
        onClose={() => setImporting(false)}
        onImport={(t) => {
          setDraft(toDraft(t))
          setImporting(false)
          toast.success("Team imported", { description: "Check it, then save." })
        }}
      />
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

function ImportDialog({ open, project, onClose, onImport }: { open: boolean; project: string; onClose: () => void; onImport: (t: Team) => void }) {
  const [text, setText] = useState("")
  const [res, setRes] = useState<TeamCheck | null>(null)
  const [busy, setBusy] = useState(false)
  const read = async () => {
    setBusy(true)
    try {
      setRes(await checkTeam({ yaml: text, project }))
    } catch (e) {
      setRes({ team: null, problems: [{ severity: "error", code: "check", message: errorMessage(e) }], estimates: [], valid: false })
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onClose={onClose} title="Import a team" description="Paste a lucid-team.yaml, for example one exported from a visual agent builder. It is checked first; nothing is saved until you choose Save team.">
      <textarea
        value={text}
        onChange={(e) => {
          setText(e.target.value)
          setRes(null)
        }}
        rows={10}
        aria-label="Team YAML"
        placeholder={"version: 1\nroles:\n  proposer: {provider: claude, model: sonnet}\n  critic: [{provider: codex}, {provider: grok}]\n  builder: {provider: claude, budget_usd: 2}"}
        className="w-full resize-y rounded-lg border bg-background/60 px-3 py-2 font-mono text-xs outline-none focus-visible:border-border-strong"
      />
      {res && (
        <div className="mt-2 space-y-1">
          {res.valid && <Badge className="text-success-fg">Valid</Badge>}
          <Problems list={res.problems} />
        </div>
      )}
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="secondary" onClick={() => void read()} disabled={busy || !text.trim()}>
          Check
        </Button>
        <Button onClick={() => res?.team && onImport(res.team)} disabled={!res?.team || !res.valid}>
          Use this team
        </Button>
      </div>
    </Dialog>
  )
}
