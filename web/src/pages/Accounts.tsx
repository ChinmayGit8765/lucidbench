import { useState, type CSSProperties } from "react"
import { ArrowRight, CheckCircle2, ChevronDown, CircleDashed, Container, Copy, HardDrive, KeyRound, Plug, Plus, Users } from "lucide-react"

import { copyText, CopyCommand } from "@/components/CopyCommand"
import { ProviderTile, PROVIDERS, providerInfo, tintVar } from "@/components/ProviderMark"
import { PageHeader } from "@/components/Shell"
import { Badge, StatusPill, type Tone } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Dialog } from "@/components/ui/dialog"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import { clientLabel, MCP_POLL_MS, serversFor, type McpMatrix } from "@/lib/mcp"
import { cn } from "@/lib/utils"

export interface Account {
  provider: string
  name: string
  location: "host" | "volume" | "env"
  config_dir?: string
  status: "logged_in" | "expired" | "missing" | "unknown"
  detail?: string
}

/** Providers `lucid login` can sign in to. */
const LOGIN_PROVIDERS = PROVIDERS.filter((p) => p.id !== "cursor")

const STATUS: Record<Account["status"], { tone: Tone; label: string }> = {
  logged_in: { tone: "success", label: "Signed in" },
  expired: { tone: "warning", label: "Expired" },
  missing: { tone: "neutral", label: "Not signed in" },
  unknown: { tone: "neutral", label: "Unknown" },
}

const LOCATION: Record<Account["location"], { label: string; hint: string; icon: typeof HardDrive }> = {
  host: { label: "Host", hint: "Signed in on this machine", icon: HardDrive },
  volume: { label: "Volume", hint: "Docker volume profile", icon: Container },
  env: { label: "Env key", hint: "API key from an environment variable", icon: KeyRound },
}

const NAME_RE = /^[a-z0-9][a-z0-9_.-]{0,40}$/

/** The command that (re)signs this account in, if there is one. */
function loginCommand(a: Account): string | null {
  if (a.location === "volume") return `lucid login ${a.provider} --profile ${a.name}`
  if (a.location === "host") return providerInfo(a.provider)?.hostLogin ?? null
  return null
}

/** Shows RFC 3339 times in details as local date and time. */
function friendlyDetail(detail: string): string {
  return detail.replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?(\.\d+)?(Z|[+-]\d{2}:\d{2})/g, (m) => {
    const d = new Date(m)
    return Number.isNaN(d.getTime())
      ? m
      : d.toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })
  })
}

const tintWash = (provider: string): CSSProperties => ({
  backgroundImage: `linear-gradient(to bottom, color-mix(in oklch, ${tintVar(provider)} 7%, transparent), transparent 70%)`,
})

function AccountCard({ account }: { account: Account }) {
  const [open, setOpen] = useState(false)
  const st = STATUS[account.status] ?? STATUS.unknown
  const loc = LOCATION[account.location] ?? LOCATION.host
  const info = providerInfo(account.provider)
  const login = loginCommand(account)
  const LocIcon = loc.icon
  return (
    <Card
      style={tintWash(account.provider)}
      className="flex flex-col p-4 transition-[border-color,box-shadow] duration-200 hover:border-border-strong"
    >
      <div className="flex items-start gap-3">
        <ProviderTile provider={account.provider} />
        <div className="min-w-0 flex-1">
          <div className="truncate font-mono text-sm font-medium" title={account.name}>
            {account.name}
          </div>
          <div className="truncate text-xs text-muted-foreground">
            {info ? `${info.label} · ${info.vendor}` : account.provider}
          </div>
        </div>
        <StatusPill tone={st.tone}>{st.label}</StatusPill>
      </div>

      <p className="mt-3 line-clamp-2 min-h-5 text-sm text-muted-foreground">
        {account.detail ? friendlyDetail(account.detail) : loc.hint}
      </p>

      <div className="mt-3 flex items-center gap-1.5 border-t pt-3">
        <Badge title={loc.hint}>
          <LocIcon /> {loc.label}
        </Badge>
        <div className="flex-1" />
        {account.config_dir && (
          <Button
            variant="ghost"
            size="sm"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
            className="px-2"
          >
            Details
            <ChevronDown className={cn("transition-transform duration-200", open && "rotate-180")} />
          </Button>
        )}
        {login && (
          <Button
            variant="ghost"
            size="sm"
            className="px-2"
            title={login}
            onClick={() => void copyText(login, "Login command copied")}
          >
            <Copy /> Copy login
          </Button>
        )}
      </div>

      {account.config_dir && (
        <div
          className={cn(
            "grid transition-[grid-template-rows,opacity] duration-200 ease-[var(--ease-out-soft)]",
            open ? "grid-rows-[1fr] opacity-100" : "grid-rows-[0fr] opacity-0",
          )}
        >
          <div className="overflow-hidden">
            <dl className="mt-3 grid grid-cols-[5.5rem_1fr] gap-x-3 gap-y-1.5 rounded-lg border bg-background/60 p-3 text-xs">
              <dt className="text-subtle-foreground">{account.location === "volume" ? "Volume" : "Config dir"}</dt>
              <dd className="min-w-0 break-all font-mono">{account.config_dir}</dd>
              <dt className="text-subtle-foreground">Location</dt>
              <dd>{loc.hint}</dd>
              <dt className="text-subtle-foreground">Status</dt>
              <dd className="font-mono">{account.status}</dd>
            </dl>
          </div>
        </div>
      )}
    </Card>
  )
}

function Section({
  provider,
  title,
  subtitle,
  accounts,
  mcp,
  onOpenMcp,
}: {
  provider?: string
  title: string
  subtitle?: string
  accounts: Account[]
  mcp?: McpMatrix | null
  onOpenMcp?: () => void
}) {
  return (
    <section id={provider ? `provider-${provider}` : "api-keys"} className="scroll-mt-6 space-y-3">
      <div className="flex items-center gap-2.5">
        {provider ? (
          <ProviderTile provider={provider} size="sm" />
        ) : (
          <span className="inline-flex size-6 items-center justify-center rounded-md border bg-muted text-muted-foreground">
            <KeyRound className="size-3.5" />
          </span>
        )}
        <h2 className="text-sm font-semibold">{title}</h2>
        {subtitle && <span className="text-sm text-subtle-foreground">{subtitle}</span>}
        <span className="ml-auto text-xs tabular-nums text-subtle-foreground">
          {accounts.length} {accounts.length === 1 ? "account" : "accounts"}
        </span>
      </div>
      {provider && mcp && <McpChips provider={provider} mcp={mcp} onOpen={onOpenMcp} />}
      <div className="grid gap-3 md:grid-cols-2">
        {accounts.map((a) => (
          <AccountCard key={`${a.provider}/${a.location}/${a.name}`} account={a} />
        ))}
      </div>
    </section>
  )
}

/** The MCP servers this provider's client can reach, as a compact chip row. */
function McpChips({ provider, mcp, onOpen }: { provider: string; mcp: McpMatrix; onOpen?: () => void }) {
  const client = mcp.clients.find((c) => c.provider === provider)
  if (!client) return null
  const servers = serversFor(mcp, provider)
  const max = 8
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <button
        type="button"
        onClick={onOpen}
        title="Open the MCP access matrix"
        className="group flex h-5 items-center gap-1 rounded-md pr-1 text-2xs font-medium text-subtle-foreground transition-colors hover:text-foreground"
      >
        <Plug className="size-3" />
        MCP
        <ArrowRight className="size-3 opacity-0 transition-opacity group-hover:opacity-100" />
      </button>
      {servers.length === 0 ? (
        <span className="text-2xs text-subtle-foreground">
          {client.config_found ? "no servers configured" : `no ${clientLabel(provider)} MCP config found`}
        </span>
      ) : (
        <>
          {servers.slice(0, max).map((s) => (
            <Badge
              key={s.name}
              title={s.via ? `Loaded from the ${clientLabel(s.via)} config` : s.project ? `Only in the ${s.project} project` : "Configured"}
              className={cn("font-mono", (s.via || s.project) && "border-dashed bg-transparent")}
            >
              {s.name}
            </Badge>
          ))}
          {servers.length > max && <span className="text-2xs tabular-nums text-subtle-foreground">+{servers.length - max} more</span>}
        </>
      )}
    </div>
  )
}

function SummaryStrip({ accounts }: { accounts: Account[] }) {
  const stats = PROVIDERS.map((p) => {
    const mine = accounts.filter((a) => a.provider === p.id)
    return {
      ...p,
      total: mine.length,
      signedIn: mine.filter((a) => a.status === "logged_in").length,
      unknown: mine.filter((a) => a.status === "unknown").length,
    }
  })
  const connected = stats.filter((s) => s.signedIn > 0).length
  const attention = accounts.filter((a) => a.status === "expired").length
  return (
    <Card className="overflow-hidden">
      <div className="flex flex-col lg:flex-row">
        <div className="border-b p-5 lg:w-64 lg:shrink-0 lg:border-b-0 lg:border-r">
          <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Providers</div>
          <div className="mt-1.5 flex items-baseline gap-1.5">
            <span className="text-2xl font-semibold tabular-nums tracking-tight">{connected}</span>
            <span className="text-sm text-muted-foreground">
              of <span className="tabular-nums">{PROVIDERS.length}</span> connected
            </span>
          </div>
          <div className="mt-3 flex gap-1" aria-hidden>
            {stats.map((s) => (
              <span
                key={s.id}
                className="h-1.5 flex-1 rounded-full bg-muted"
                style={s.signedIn > 0 ? { backgroundColor: tintVar(s.id) } : undefined}
              />
            ))}
          </div>
          <div className="mt-3 text-xs tabular-nums text-muted-foreground">
            {accounts.length} {accounts.length === 1 ? "account" : "accounts"}
            {attention > 0 && <span className="text-warning-fg"> · {attention} need attention</span>}
          </div>
        </div>
        <div className="grid flex-1 grid-cols-2 sm:grid-cols-4">
          {stats.map((s, i) => (
            <a
              key={s.id}
              href={s.total > 0 ? `#provider-${s.id}` : undefined}
              onClick={(e) => {
                if (s.total === 0) return
                e.preventDefault()
                document.getElementById(`provider-${s.id}`)?.scrollIntoView({ behavior: "smooth", block: "start" })
              }}
              className={cn(
                "flex flex-col gap-3 p-4 outline-none transition-colors focus-visible:bg-accent/60",
                s.total > 0 && "hover:bg-accent/40",
                i > 0 && "border-l",
                i >= 2 && "max-sm:border-t",
                i === 2 && "max-sm:border-l-0",
              )}
            >
              <div className="flex items-start justify-between">
                <ProviderTile provider={s.id} muted={s.signedIn === 0} />
                {s.signedIn > 0 ? (
                  <CheckCircle2 aria-label="Connected" className="size-4 text-success" />
                ) : (
                  <CircleDashed aria-label="Not connected" className="size-4 text-subtle-foreground/70" />
                )}
              </div>
              <div className="min-w-0">
                <div className={cn("text-sm font-medium", s.signedIn === 0 && "text-muted-foreground")}>{s.label}</div>
                <div className="truncate text-xs tabular-nums text-subtle-foreground">
                  {s.total === 0
                    ? "Not found"
                    : s.signedIn > 0
                      ? `${s.signedIn} signed in`
                      : s.unknown > 0
                        ? "Detected"
                        : "Not signed in"}
                </div>
              </div>
            </a>
          ))}
        </div>
      </div>
    </Card>
  )
}

function AccountCardSkeleton() {
  return (
    <Card className="p-4">
      <div className="flex items-start gap-3">
        <Skeleton className="size-8 rounded-lg" />
        <div className="flex-1 space-y-1.5">
          <Skeleton className="h-3.5 w-24" />
          <Skeleton className="h-3 w-32" />
        </div>
        <Skeleton className="h-5 w-16 rounded-full" />
      </div>
      <Skeleton className="mt-4 h-3.5 w-2/3" />
      <div className="mt-4 flex items-center justify-between border-t pt-3">
        <Skeleton className="h-5 w-14" />
        <Skeleton className="h-5 w-24" />
      </div>
    </Card>
  )
}

function AccountsSkeleton() {
  return (
    <div className="space-y-8" aria-busy="true" aria-label="Detecting accounts">
      <Card className="flex h-[142px] items-center gap-6 p-5">
        <div className="w-56 space-y-3">
          <Skeleton className="h-3 w-20" />
          <Skeleton className="h-6 w-32" />
          <Skeleton className="h-1.5 w-full rounded-full" />
        </div>
        <div className="grid flex-1 grid-cols-4 gap-6 max-sm:hidden">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="space-y-2">
              <Skeleton className="size-8 rounded-lg" />
              <Skeleton className="h-3.5 w-16" />
            </div>
          ))}
        </div>
      </Card>
      {[0, 1].map((i) => (
        <div key={i} className="space-y-3">
          <div className="flex items-center gap-2.5">
            <Skeleton className="size-6" />
            <Skeleton className="h-4 w-24" />
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <AccountCardSkeleton />
            <AccountCardSkeleton />
          </div>
        </div>
      ))}
    </div>
  )
}

export function AddAccountDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [provider, setProvider] = useState(LOGIN_PROVIDERS[0].id)
  const [name, setName] = useState("")
  const valid = NAME_RE.test(name)
  const command = `lucid login ${provider} --profile ${valid ? name : "<name>"}`
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Add account"
      description="Lucidbench never signs in for you. Pick a provider, name the profile, then run the command in a terminal."
    >
      <div className="space-y-5">
        <fieldset className="space-y-2">
          <legend className="mb-2 text-xs font-medium text-muted-foreground">Provider</legend>
          <div role="radiogroup" className="grid grid-cols-3 gap-2">
            {LOGIN_PROVIDERS.map((p) => {
              const on = provider === p.id
              return (
                <button
                  key={p.id}
                  type="button"
                  role="radio"
                  aria-checked={on}
                  onClick={() => setProvider(p.id)}
                  className={cn(
                    "flex flex-col items-center gap-2 rounded-lg border px-2 py-3 text-center transition-[border-color,background-color,box-shadow] duration-150",
                    on
                      ? "border-brand bg-brand-soft shadow-[0_0_0_1px_var(--brand)]"
                      : "hover:border-border-strong hover:bg-accent/50",
                  )}
                >
                  <ProviderTile provider={p.id} />
                  <span className="leading-tight">
                    <span className="block text-sm font-medium">{p.label}</span>
                    <span className="block text-2xs text-subtle-foreground">{p.vendor}</span>
                  </span>
                </button>
              )
            })}
          </div>
          <p className="text-2xs text-subtle-foreground">Cursor is detected from its own CLI sign-in.</p>
        </fieldset>
        <label className="block space-y-2">
          <span className="text-xs font-medium text-muted-foreground">Profile name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value.toLowerCase())}
            placeholder="work"
            aria-invalid={name !== "" && !valid}
            className="h-9 w-full rounded-lg border border-input bg-background px-3 font-mono text-sm outline-none transition-shadow placeholder:text-subtle-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 aria-[invalid=true]:border-danger"
          />
          {name && !valid && (
            <span className="block text-xs text-danger-fg">
              Use lowercase letters, digits, dot, dash or underscore, starting with a letter or digit.
            </span>
          )}
        </label>
        <div className="space-y-2">
          <span className="text-xs font-medium text-muted-foreground">Command</span>
          {valid ? (
            <CopyCommand command={command} />
          ) : (
            <div className="flex h-9 items-center rounded-lg border border-dashed px-3 font-mono text-xs text-subtle-foreground">
              {command}
            </div>
          )}
        </div>
      </div>
    </Dialog>
  )
}

export default function Accounts({ onAdd, onOpenMcp }: { onAdd: () => void; onOpenMcp?: () => void }) {
  const { data, error, loading, refresh } = usePoll<Account[]>("/api/accounts")
  const mcp = usePoll<McpMatrix>("/api/mcp", MCP_POLL_MS)


  const all = data ?? []
  const keys = all.filter((a) => a.location === "env")
  const byProvider = PROVIDERS.map((p) => ({
    ...p,
    accounts: all.filter((a) => a.provider === p.id && a.location !== "env"),
  })).filter((g) => g.accounts.length > 0)

  return (
    <div className="space-y-8">
      <PageHeader
        icon={<Users />}
        title="Accounts"
        description="Your Claude, Codex, Grok and Cursor accounts in one place. Detected by presence only; tokens are never read."
        actions={
          <Button onClick={onAdd}>
            <Plus /> Add account
          </Button>
        }
      />

      {loading && !data && <AccountsSkeleton />}
      {error && !data && (
        <ErrorState title="Could not load accounts" message={error.message} onRetry={refresh} />
      )}
      {error && data && (
        <ErrorState title="Refresh failed, showing the last result" message={error.message} onRetry={refresh} />
      )}

      {data && all.length === 0 && (
        <Card className="border-dashed">
          <EmptyState
            icon={<Users />}
            satellites={PROVIDERS.map((p) => (
              <ProviderTile key={p.id} provider={p.id} size="sm" />
            ))}
            title="No accounts detected"
            description="Sign in to a provider CLI on this machine, or add a profile, and it shows up here within a few seconds."
          >
            <Button onClick={onAdd}>
              <Plus /> Add account
            </Button>
          </EmptyState>
        </Card>
      )}

      {data && all.length > 0 && <SummaryStrip accounts={all} />}

      {byProvider.map((g) => (
        <Section
          key={g.id}
          provider={g.id}
          title={g.label}
          subtitle={g.vendor}
          accounts={g.accounts}
          mcp={mcp.data}
          onOpenMcp={onOpenMcp}
        />
      ))}
      {keys.length > 0 && <Section title="API keys" subtitle="from the environment" accounts={keys} />}


    </div>
  )
}
