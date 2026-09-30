import { useState } from "react"
import { ChevronDown, ChevronRight, Plus, Users } from "lucide-react"

import { CopyCommand } from "@/components/CopyCommand"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog } from "@/components/ui/dialog"
import { ErrorState, LoadingState } from "@/components/ui/states"
import { cn } from "@/lib/utils"
import { usePoll } from "@/lib/api"

interface Account {
  provider: string
  name: string
  location: "host" | "volume" | "env"
  config_dir?: string
  status: "logged_in" | "expired" | "missing" | "unknown"
  detail?: string
}

const PROVIDERS = [
  { id: "claude", label: "Claude" },
  { id: "codex", label: "Codex" },
  { id: "grok", label: "Grok" },
  { id: "cursor", label: "Cursor" },
]

/** Providers `lucid login` can sign in to. */
const LOGIN_PROVIDERS = PROVIDERS.filter((p) => p.id !== "cursor")

const STATUS: Record<Account["status"], { dot: string; label: string }> = {
  logged_in: { dot: "bg-emerald-500", label: "Logged in" },
  expired: { dot: "bg-amber-500", label: "Expired" },
  missing: { dot: "bg-muted-foreground/50", label: "Not logged in" },
  unknown: { dot: "bg-muted-foreground/50", label: "Unknown" },
}

const NAME_RE = /^[a-z0-9][a-z0-9_.-]{0,40}$/

function AccountCard({ account }: { account: Account }) {
  const [open, setOpen] = useState(false)
  const st = STATUS[account.status] ?? STATUS.unknown
  return (
    <Card className="p-4">
      <div className="flex items-center gap-3">
        <span className={cn("size-2.5 shrink-0 rounded-full", st.dot)} title={st.label} />
        <span className="min-w-0 flex-1 truncate font-medium">{account.name}</span>
        <Badge>{account.location}</Badge>
      </div>
      <p className="mt-2 text-sm text-muted-foreground">{account.detail || st.label}</p>
      {account.config_dir && (
        <div className="mt-2">
          <button
            onClick={() => setOpen(!open)}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
            details
          </button>
          {open && (
            <code className="mt-1.5 block break-all rounded-md bg-background px-2 py-1.5 font-mono text-xs text-muted-foreground">
              {account.config_dir}
            </code>
          )}
        </div>
      )}
    </Card>
  )
}

function Section({ title, accounts }: { title: string; accounts: Account[] }) {
  return (
    <section className="space-y-3">
      <h2 className="text-sm font-medium text-muted-foreground">{title}</h2>
      <div className="grid gap-3 sm:grid-cols-2">
        {accounts.map((a) => (
          <AccountCard key={`${a.provider}/${a.location}/${a.name}`} account={a} />
        ))}
      </div>
    </section>
  )
}

function AddAccountDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [provider, setProvider] = useState(LOGIN_PROVIDERS[0].id)
  const [name, setName] = useState("")
  const valid = NAME_RE.test(name)
  const command = `lucid login ${provider} --profile ${valid ? name : "<name>"}`
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Add account"
      description="Lucidbench never signs in for you. Run this command in a terminal and follow the prompts."
    >
      <div className="space-y-4">
        <label className="block space-y-1.5 text-sm">
          <span className="text-muted-foreground">Provider</span>
          <select
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
          >
            {LOGIN_PROVIDERS.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
          </select>
        </label>
        <label className="block space-y-1.5 text-sm">
          <span className="text-muted-foreground">Profile name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value.toLowerCase())}
            placeholder="work"
            className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
          {name && !valid && (
            <span className="text-xs text-destructive">
              Use lowercase letters, digits, dot, dash or underscore, starting with a letter or digit.
            </span>
          )}
        </label>
        <div className="space-y-1.5">
          <span className="text-sm text-muted-foreground">Command</span>
          {valid ? (
            <CopyCommand command={command} />
          ) : (
            <div className="rounded-md border border-dashed px-3 py-2 font-mono text-xs text-muted-foreground">
              {command}
            </div>
          )}
        </div>
      </div>
    </Dialog>
  )
}

export default function Accounts() {
  const { data, error, loading } = usePoll<Account[]>("/api/accounts")
  const [adding, setAdding] = useState(false)

  const all = data ?? []
  const keys = all.filter((a) => a.location === "env")
  const byProvider = PROVIDERS.map((p) => ({
    ...p,
    accounts: all.filter((a) => a.provider === p.id && a.location !== "env"),
  })).filter((g) => g.accounts.length > 0)

  return (
    <div className="space-y-8">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Accounts</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Your Claude, Codex, Grok and Cursor accounts in one place.
          </p>
        </div>
        <Button onClick={() => setAdding(true)}>
          <Plus /> Add account
        </Button>
      </div>

      {loading && !data && <LoadingState label="Detecting accounts..." />}
      {error && !data && (
        <ErrorState title="Could not load accounts" message={error.message} />
      )}
      {error && data && (
        <p className="text-xs text-destructive">Refresh failed: {error.message}. Showing the last result.</p>
      )}

      {data && all.length === 0 && (
        <Card className="border-dashed">
          <CardHeader className="items-center text-center">
            <div className="mb-2 flex size-12 items-center justify-center rounded-full bg-muted">
              <Users className="size-6 text-muted-foreground" />
            </div>
            <CardTitle>No accounts detected</CardTitle>
            <CardDescription>
              Sign in to a provider CLI on this machine, or add a profile to get started.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex justify-center">
            <Button variant="outline" onClick={() => setAdding(true)}>
              <Plus /> Add account
            </Button>
          </CardContent>
        </Card>
      )}

      {byProvider.map((g) => (
        <Section key={g.id} title={g.label} accounts={g.accounts} />
      ))}
      {keys.length > 0 && <Section title="API keys" accounts={keys} />}

      <AddAccountDialog key={String(adding)} open={adding} onClose={() => setAdding(false)} />
    </div>
  )
}
