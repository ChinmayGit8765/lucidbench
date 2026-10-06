import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { AlertTriangle, ChevronRight, Database as DatabaseIcon, ExternalLink, Play, Plus, RefreshCw, ShieldCheck, Square, Table2, Trash2, Wrench } from "lucide-react"
import { toast } from "sonner"

import { PageHeader } from "@/components/Shell"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Dialog } from "@/components/ui/dialog"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { Tabs } from "@/components/ui/tabs"
import { errorMessage, refreshAll, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  DEFAULT_PORT,
  ENGINE_ICON,
  ENGINE_LABEL,
  QUERY_HINT,
  displayName,
  formatBytes,
  healthCounts,
  removeConnection,
  runQuery,
  sampleQuery,
  saveConnection,
  startManager,
  stopManager,
  useDatabases,
  type DbResult,
  type DbTable,
  type DiscoveredDb,
  type Engine,
  type ManagerStatus,
  type NewConnection,
  type SavedDb,
} from "@/lib/databases"
import { cn } from "@/lib/utils"
import type { ModulePageProps } from "@/modules/types"

const field =
  "h-8 w-full rounded-md border bg-background/60 px-2.5 text-sm outline-none transition-colors placeholder:text-subtle-foreground hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"

function Cell({ label, value, sub }: { label: string; value: ReactNode; sub?: ReactNode }) {
  return (
    <div className="min-w-0 px-4 py-3.5">
      <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{label}</div>
      <div className="mt-1 truncate text-xl font-semibold tabular-nums tracking-tight">{value}</div>
      {sub && <div className="mt-0.5 truncate text-xs text-muted-foreground">{sub}</div>}
    </div>
  )
}

function EngineMark({ engine }: { engine: Engine }) {
  const Icon = ENGINE_ICON[engine]
  return (
    <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-border-strong bg-elevated text-brand [&_svg]:size-4" title={ENGINE_LABEL[engine]}>
      <Icon />
    </span>
  )
}

function slug(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
}

/* ---------- save a connection ---------- */

function ConnectionDialog({ seed, onClose, onSaved }: { seed: Partial<NewConnection> | null; onClose: () => void; onSaved: (id: string) => void }) {
  const [c, setC] = useState<NewConnection>({ id: "", engine: "postgres", host: "127.0.0.1", port: 5432, readonly: true })
  const [envName, setEnvName] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    if (!seed) return
    const engine = seed.engine ?? "postgres"
    setC({ id: "", host: "127.0.0.1", port: DEFAULT_PORT[engine], readonly: true, ...seed, engine })
    setEnvName(seed.password?.replace(/^env:/, "") ?? "")
    setError(null)
  }, [seed])
  if (!seed) return null
  const set = (patch: Partial<NewConnection>) => setC((x) => ({ ...x, ...patch }))
  const submit = async () => {
    setBusy(true)
    setError(null)
    try {
      const body: NewConnection = { ...c, id: c.id || slug(c.label ?? ""), password: envName.trim() ? `env:${envName.trim()}` : undefined, readonly: c.readonly || !!c.prod }
      const saved = await saveConnection(body)
      toast.success(`Saved ${displayName(saved)}`)
      refreshAll()
      onSaved(saved.id)
      onClose()
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  const row = (label: string, el: ReactNode, hint?: ReactNode) => (
    <label className="block space-y-1">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      {el}
      {hint && <span className="block text-2xs text-subtle-foreground">{hint}</span>}
    </label>
  )
  return (
    <Dialog open onClose={onClose} title="Save as connection" description="Lucidbench keeps the host and the name of a password variable, never the password.">
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-3">
          {row(
            "Engine",
            <select className={field} value={c.engine} onChange={(e) => set({ engine: e.target.value as Engine, port: DEFAULT_PORT[e.target.value as Engine] })}>
              {(Object.keys(ENGINE_LABEL) as Engine[]).map((e) => (
                <option key={e} value={e}>
                  {ENGINE_LABEL[e]}
                </option>
              ))}
            </select>,
          )}
          {row("Name", <input className={field} value={c.label ?? ""} maxLength={80} placeholder="Shop (local)" onChange={(e) => set({ label: e.target.value, id: c.id || slug(e.target.value) })} />)}
        </div>
        <div className="grid grid-cols-[1fr_6rem] gap-3">
          {row("Host", <input className={field} value={c.host} onChange={(e) => set({ host: e.target.value })} />)}
          {row("Port", <input className={field} type="number" min={1} max={65535} value={c.port} onChange={(e) => set({ port: Number(e.target.value) })} />)}
        </div>
        <div className="grid grid-cols-2 gap-3">
          {row(c.engine === "redis" ? "Database number" : "Database", <input className={field} value={c.database ?? ""} onChange={(e) => set({ database: e.target.value })} />)}
          {row("User", <input className={field} value={c.user ?? ""} onChange={(e) => set({ user: e.target.value })} />)}
        </div>
        {row(
          "Password variable",
          <div className="flex items-center gap-1.5">
            <span className="font-mono text-xs text-subtle-foreground">env:</span>
            <input className={cn(field, "font-mono")} value={envName} placeholder="SHOP_DB_PASSWORD" spellCheck={false} onChange={(e) => setEnvName(e.target.value)} />
          </div>,
          "The name of an environment variable that holds the password. Set it where Lucidbench starts, then restart it.",
        )}
        <div className="grid grid-cols-2 gap-3">
          {row("Id", <input className={cn(field, "font-mono")} value={c.id} placeholder="shop" onChange={(e) => set({ id: slug(e.target.value) })} />)}
          <label className="flex items-center gap-2 self-end pb-1.5 text-sm">
            <input type="checkbox" checked={!!c.prod} onChange={(e) => set({ prod: e.target.checked })} />
            Production database
          </label>
        </div>
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <ShieldCheck className="size-3.5 text-subtle-foreground" /> Every connection is read-only in this version.
        </p>
        {error && <p className="text-sm text-danger-fg">{error}</p>}
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !c.host || !(c.id || c.label)}>
            Save connection
          </Button>
        </div>
      </div>
    </Dialog>
  )
}

/* ---------- lists ---------- */

function HealthPill({ s }: { s: SavedDb }) {
  const h = s.health
  if (!h) return <StatusPill tone="neutral">Checking…</StatusPill>
  if (!h.ok) return <StatusPill tone="danger">Down</StatusPill>
  return <StatusPill tone="success">{h.latency_ms} ms</StatusPill>
}

function SavedRow({ s, on, onSelect, onRemove }: { s: SavedDb; on: boolean; onSelect: () => void; onRemove: () => void }) {
  return (
    <li className={cn("flex items-center gap-3 border-b px-5 py-3 last:border-b-0", on ? "bg-accent/60" : "hover:bg-accent/40")}>
      <EngineMark engine={s.engine} />
      <button onClick={onSelect} className="min-w-0 flex-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{displayName(s)}</span>
          {s.prod && <StatusPill tone="warning">prod</StatusPill>}
          <Badge className="font-mono">read-only</Badge>
        </div>
        <div className="truncate text-xs text-muted-foreground">
          <span className="font-mono">
            {s.host}:{s.port}
          </span>
          {s.database && <> · {s.database}</>}
          {s.health?.version && <> · {s.health.version.replace(/ on .*/, "").slice(0, 40)}</>}
          {s.health?.ok && s.health.size_bytes >= 0 && <> · {formatBytes(s.health.size_bytes)}</>}
        </div>
        {s.health && !s.health.ok && <div className="mt-0.5 truncate text-xs text-danger-fg" title={s.health.error}>{s.health.error}</div>}
        {!s.password_set && s.password && (
          <div className="mt-0.5 text-xs text-warning-fg">
            <span className="font-mono">{s.password.replace("env:", "")}</span> is not set in the environment Lucidbench runs in
          </div>
        )}
      </button>
      <HealthPill s={s} />
      <Button variant="ghost" size="icon-sm" aria-label={`Remove ${displayName(s)}`} title="Remove this connection (the database is untouched)" onClick={onRemove}>
        <Trash2 />
      </Button>
    </li>
  )
}

function DiscoveredRow({ d, onSave }: { d: DiscoveredDb; onSave: () => void }) {
  return (
    <li className="flex items-center gap-3 border-b px-5 py-3 last:border-b-0 hover:bg-accent/40">
      <EngineMark engine={d.engine} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate font-mono text-sm font-medium">{d.name}</span>
          <StatusPill tone={d.running ? "success" : "neutral"}>{d.running ? "running" : d.state}</StatusPill>
        </div>
        <div className="truncate text-xs text-muted-foreground">
          {d.image}
          {" · "}
          {d.port ? <span className="font-mono">127.0.0.1:{d.port}</span> : "no published port"}
          {d.database && <> · db {d.database}</>}
          {d.user && <> · user {d.user}</>}
        </div>
      </div>
      {d.saved_as ? (
        <Badge>saved as {d.saved_as}</Badge>
      ) : (
        <Button variant="secondary" size="sm" onClick={onSave}>
          <Plus /> Save as connection
        </Button>
      )}
    </li>
  )
}

/* ---------- schema ---------- */

function SchemaTab({ id, engine, onQuery }: { id: string; engine: Engine; onQuery: (q: string) => void }) {
  const poll = usePoll<{ tables: DbTable[] }>(`/api/databases/${encodeURIComponent(id)}/schema`, 60000)
  const [open, setOpen] = useState<string | null>(null)
  const tables = poll.data?.tables
  if (!tables && poll.error) return <div className="px-5 py-4 text-sm text-danger-fg">{poll.error.message}</div>
  if (!tables)
    return (
      <div className="space-y-2 p-5">
        <Skeleton className="h-4 w-1/3" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    )
  if (tables.length === 0)
    return <div className="px-5 py-6 text-sm text-muted-foreground">{engine === "redis" ? "No keys in this database." : "Nothing here yet: no tables or collections."}</div>
  const key = (t: DbTable) => `${t.schema ?? ""}.${t.name}`
  return (
    <ul>
      {tables.map((t) => {
        const isOpen = open === key(t)
        return (
          <li key={key(t)} className="border-b last:border-b-0">
            <div className="flex items-center gap-2 px-5 py-2 hover:bg-accent/40">
              <button
                onClick={() => setOpen(isOpen ? null : key(t))}
                aria-expanded={isOpen}
                className="flex min-w-0 flex-1 items-center gap-2 text-left text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <ChevronRight className={cn("size-3.5 shrink-0 text-subtle-foreground transition-transform", isOpen && "rotate-90")} />
                <Table2 className="size-3.5 shrink-0 text-subtle-foreground" />
                <span className="truncate font-mono">
                  {t.schema && t.schema !== "public" ? <span className="text-subtle-foreground">{t.schema}.</span> : null}
                  {t.name}
                </span>
                <Badge>{t.kind}</Badge>
              </button>
              <span className="w-28 shrink-0 text-right text-xs tabular-nums text-muted-foreground" title="An estimate from the database's own statistics">
                {t.rows !== undefined ? `~${t.rows.toLocaleString()} ${engine === "redis" ? "items" : "rows"}` : ""}
              </span>
              <Button variant="ghost" size="sm" onClick={() => onQuery(sampleQuery(engine, t))} title="Put a first query for this in the query box">
                Query
              </Button>
            </div>
            {isOpen && (
              <div className="border-t bg-muted/30 px-5 py-2">
                {t.columns.length === 0 ? (
                  <p className="py-1 text-xs text-muted-foreground">No columns to list.</p>
                ) : (
                  <table className="w-full text-xs">
                    <tbody>
                      {t.columns.map((col) => (
                        <tr key={col.name} className="border-b border-border/50 last:border-b-0">
                          <td className="w-1/3 py-1 pr-3 font-mono">{col.name}</td>
                          <td className="py-1 pr-3 font-mono text-muted-foreground">{col.type}</td>
                          <td className="py-1 text-right text-subtle-foreground">{col.nullable ? "nullable" : ""}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
            )}
          </li>
        )
      })}
    </ul>
  )
}

/* ---------- query ---------- */

function ResultTable({ r }: { r: DbResult }) {
  return (
    <div>
      <div className="flex items-center gap-3 border-y bg-muted/40 px-5 py-1.5 text-xs text-muted-foreground">
        <span className="tabular-nums">
          {r.rows.length} {r.rows.length === 1 ? "row" : "rows"}
        </span>
        <span className="tabular-nums">{r.elapsed_ms} ms</span>
        {r.truncated && (
          <span className="flex items-center gap-1 text-warning-fg">
            <AlertTriangle className="size-3.5" /> Showing the first {r.row_limit} rows
          </span>
        )}
      </div>
      <div className="max-h-96 overflow-auto">
        <table className="w-full text-left text-xs">
          <thead className="sticky top-0 bg-card">
            <tr>
              {r.columns.map((c, i) => (
                <th key={`${c}-${i}`} className="whitespace-nowrap border-b px-3 py-1.5 font-mono font-medium text-muted-foreground first:pl-5">
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {r.rows.map((row, ri) => (
              <tr key={ri} className="border-b border-border/50 last:border-b-0 hover:bg-accent/40">
                {row.map((v, ci) =>
                  r.nulls?.[ri]?.[ci] ? (
                    <td key={ci} className="px-3 py-1 italic text-subtle-foreground first:pl-5">
                      NULL
                    </td>
                  ) : (
                    <td key={ci} className="max-w-[28rem] truncate px-3 py-1 font-mono first:pl-5" title={v}>
                      {v}
                    </td>
                  ),
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function QueryTab({ s, text, setText, redisCommands }: { s: SavedDb; text: string; setText: (q: string) => void; redisCommands: string[] }) {
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<DbResult | null>(null)
  const [refused, setRefused] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const run = async () => {
    if (!text.trim() || busy) return
    setBusy(true)
    setResult(null)
    setRefused(null)
    setError(null)
    try {
      setResult(await runQuery(s.id, text))
    } catch (e) {
      const msg = errorMessage(e)
      // The server answers 403 "read-only: ..." for anything that could write.
      if (msg.startsWith("read-only")) setRefused(msg)
      else setError(msg)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div>
      <div className="space-y-2 px-5 py-4">
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
              e.preventDefault()
              void run()
            }
          }}
          rows={4}
          spellCheck={false}
          aria-label="Query"
          placeholder={QUERY_HINT[s.engine]}
          className={cn(field, "h-auto resize-y py-2 font-mono text-xs leading-relaxed")}
        />
        <div className="flex flex-wrap items-center gap-3">
          <Button size="sm" onClick={run} disabled={busy || !text.trim()}>
            <Play /> Run
          </Button>
          <span className="text-xs text-subtle-foreground">Ctrl+Enter</span>
          <span className="ml-auto flex items-center gap-1.5 text-xs text-muted-foreground">
            <ShieldCheck className="size-3.5 text-subtle-foreground" />
            {s.engine === "redis"
              ? `Read commands only: ${redisCommands.slice(0, 6).join(", ").toLowerCase()} and more`
              : s.engine === "mongo"
                ? "find with a limit, 500 documents at most"
                : "Read-only transaction · 10 s timeout · 500 rows"}
            {" · "}writes are not supported yet
          </span>
        </div>
      </div>
      {refused && (
        <div role="alert" className="mx-5 mb-4 flex items-start gap-2 rounded-lg border border-danger/30 bg-danger-soft px-3 py-2.5 text-sm text-danger-fg">
          <ShieldCheck className="mt-0.5 size-4 shrink-0" />
          <div>
            <div className="font-medium">Refused</div>
            <div className="text-xs">{refused}</div>
          </div>
        </div>
      )}
      {error && <div role="alert" className="mx-5 mb-4 rounded-lg border bg-muted/40 px-3 py-2.5 font-mono text-xs text-danger-fg">{error}</div>}
      {result && <ResultTable r={result} />}
    </div>
  )
}

/* ---------- manager ---------- */

function ManagerPanel({ s, status, onChanged }: { s: SavedDb; status: ManagerStatus; onChanged: () => void }) {
  const running = status.running && status.url
  // The page being open is what keeps the manager awake: starting a running
  // manager only counts as activity.
  useEffect(() => {
    if (!running) return
    const t = setInterval(() => void startManager(s.id).catch(() => undefined), 120000)
    return () => clearInterval(t)
  }, [running, s.id])
  if (!running) return null
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center gap-3 px-5 py-3">
        <Wrench className="size-4 text-subtle-foreground" />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">
            {status.name} <span className="font-normal text-muted-foreground">for {displayName(s)}</span>
          </div>
          <div className="truncate text-xs text-muted-foreground">
            Runs in a container on 127.0.0.1 and stops by itself after {status.idle_minutes ?? 10} minutes without this page.
          </div>
        </div>
        <Button variant="ghost" size="sm" asChild>
          <a href={status.url} target="_blank" rel="noreferrer">
            Open in browser <ExternalLink />
          </a>
        </Button>
        <Button
          variant="secondary"
          size="sm"
          onClick={async () => {
            try {
              await stopManager(s.id)
              toast.success(`${status.name} stopped`)
            } catch (e) {
              toast.error("Could not stop it", { description: errorMessage(e) })
            }
            onChanged()
          }}
        >
          <Square /> Stop
        </Button>
      </div>
      <iframe title={`${status.name} for ${displayName(s)}`} src={status.url} className="block h-[560px] w-full border-t bg-background" />
    </Card>
  )
}

/* ---------- selected connection ---------- */

function Detail({ s, redisCommands }: { s: SavedDb; redisCommands: string[] }) {
  const [tab, setTab] = useState("schema")
  const [text, setText] = useState("")
  const [starting, setStarting] = useState(false)
  const status = usePoll<ManagerStatus>(`/api/databases/${encodeURIComponent(s.id)}/manager`, 20000)
  const m = status.data
  // A fresh connection starts on its own tab and with an empty box.
  const last = useRef(s.id)
  useEffect(() => {
    if (last.current !== s.id) {
      last.current = s.id
      setTab("schema")
      setText("")
    }
  }, [s.id])
  const open = async () => {
    setStarting(true)
    const t = toast.loading(`Starting ${m?.name ?? "the manager"}…`, { description: "The first start pulls the image, which can take a minute." })
    try {
      await startManager(s.id)
      toast.success(`${m?.name} is ready`, { id: t })
    } catch (e) {
      toast.error("Could not start the manager", { id: t, description: errorMessage(e) })
    } finally {
      setStarting(false)
      status.refresh()
    }
  }
  return (
    <>
      <Card className="overflow-hidden">
        <div className="flex flex-wrap items-center gap-3 px-5 pt-4">
          <EngineMark engine={s.engine} />
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-sm font-semibold">{displayName(s)}</h2>
            <div className="truncate text-xs text-muted-foreground">
              {ENGINE_LABEL[s.engine]} · <span className="font-mono">{s.host}:{s.port}</span>
            </div>
          </div>
          {m?.available && !m.running && (
            <Button variant="secondary" size="sm" onClick={open} disabled={starting} title={`Run ${m.name} (${m.license}) in a container, pulled the first time`}>
              <Wrench /> Open in {m.name}
            </Button>
          )}
          {m && !m.available && m.note && <span className="text-xs text-subtle-foreground">{m.note}</span>}
        </div>
        <Tabs
          label="Connection"
          value={tab}
          onChange={setTab}
          className="mt-2 px-3"
          items={[
            { id: "schema", label: s.engine === "mongo" ? "Collections" : s.engine === "redis" ? "Keys" : "Tables", icon: Table2 },
            { id: "query", label: "Query", icon: Play },
          ]}
        />
        {tab === "schema" ? (
          <SchemaTab
            id={s.id}
            engine={s.engine}
            onQuery={(q) => {
              setText(q)
              setTab("query")
            }}
          />
        ) : (
          <QueryTab s={s} text={text} setText={setText} redisCommands={redisCommands} />
        )}
      </Card>
      {m && <ManagerPanel s={s} status={m} onChanged={status.refresh} />}
    </>
  )
}

/* ---------- page ---------- */

export default function Databases({ subpath }: ModulePageProps) {
  const { open } = useApp()
  const poll = useDatabases(true)
  const d = poll.data
  const [dialog, setDialog] = useState<Partial<NewConnection> | null>(null)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const selected = useMemo(() => d?.saved.find((s) => s.id === subpath[0]) ?? d?.saved[0], [d, subpath])
  const counts = d ? healthCounts(d.saved) : null
  const running = d?.discovered.filter((x) => x.running).length ?? 0

  const save = (x: DiscoveredDb) =>
    setDialog({
      id: slug(x.name),
      label: x.name,
      engine: x.engine,
      host: x.host,
      port: x.port || DEFAULT_PORT[x.engine],
      database: x.database,
      user: x.user,
      password: "",
    })
  const remove = (s: SavedDb) =>
    setConfirm({
      title: `Remove ${displayName(s)}?`,
      description: "This forgets the connection and stops its manager. The database itself is not touched.",
      confirmLabel: "Remove",
      danger: true,
      run: async () => {
        try {
          await removeConnection(s.id)
          toast.success("Connection removed")
          refreshAll()
          if (subpath[0] === s.id) open("databases")
        } catch (e) {
          toast.error("Could not remove it", { description: errorMessage(e) })
        }
      },
    })

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<DatabaseIcon />}
        title="Databases"
        description="The Postgres, MySQL, Redis and MongoDB databases on this machine: health, tables and a read-only query box."
        actions={
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1.5 text-xs text-subtle-foreground max-[1100px]:hidden">
              <ShieldCheck className="size-3.5" /> Read-only · no passwords stored
            </span>
            <Button variant="secondary" size="sm" onClick={() => setDialog({})}>
              <Plus /> Add connection
            </Button>
            <Button variant="secondary" size="sm" onClick={() => poll.refresh()} disabled={poll.refreshing}>
              <RefreshCw className={cn(poll.refreshing && "animate-spin")} />
              Refresh
            </Button>
          </div>
        }
      />

      {!d && poll.error && <ErrorState title="Cannot read the databases" message={poll.error.message} onRetry={poll.refresh} />}
      {!d && !poll.error && <Skeleton className="h-[88px] rounded-xl" />}
      {d && counts && (
        <>
          <Card className="grid grid-cols-2 divide-x divide-y overflow-hidden @3xl:grid-cols-4 @3xl:divide-y-0">
            <Cell label="Saved" value={counts.total} sub="connections" />
            <Cell
              label="Healthy"
              value={
                <span>
                  {counts.up}
                  <span className="text-base font-normal text-subtle-foreground">/{counts.total}</span>
                </span>
              }
              sub={counts.down.length > 0 ? `${counts.down.length} down` : counts.total > 0 ? "all answering" : "none saved yet"}
            />
            <Cell label="Containers" value={d.discovered.length} sub={`${running} running`} />
            <Cell label="Limits" value={`${d.limits.rows} rows`} sub={`${d.limits.timeout_seconds} s per query`} />
          </Card>

          {d.docker_error && (
            <p className="flex items-center gap-2 text-sm text-warning-fg">
              <AlertTriangle className="size-4" /> Docker could not be read, so containers are not listed: {d.docker_error}
            </p>
          )}

          <Card className="overflow-hidden">
            <div className="flex items-center gap-2 border-b px-5 py-3">
              <h2 className="text-sm font-semibold">Saved connections</h2>
              <span className="text-2xs tabular-nums text-subtle-foreground">{d.saved.length}</span>
            </div>
            {d.saved.length === 0 ? (
              <EmptyState
                icon={<DatabaseIcon />}
                title="No saved connections"
                description="Save a database container from the list below, or add a connection by hand. Passwords stay in your environment; Lucidbench keeps only the variable's name."
              />
            ) : (
              <ul>
                {d.saved.map((s) => (
                  <SavedRow key={s.id} s={s} on={selected?.id === s.id} onSelect={() => open("databases", [s.id])} onRemove={() => remove(s)} />
                ))}
              </ul>
            )}
          </Card>

          <Card className="overflow-hidden">
            <div className="flex items-center gap-2 border-b px-5 py-3">
              <h2 className="text-sm font-semibold">Database containers</h2>
              <span className="text-2xs tabular-nums text-subtle-foreground">{d.discovered.length}</span>
              <span className="ml-auto text-xs text-subtle-foreground">running or stopped; passwords are never read</span>
            </div>
            {d.discovered.length === 0 ? (
              <div className="px-5 py-6 text-sm text-muted-foreground">No Postgres, MySQL, MariaDB, Redis or Mongo containers found.</div>
            ) : (
              <ul>
                {d.discovered.map((x) => (
                  <DiscoveredRow key={x.id} d={x} onSave={() => save(x)} />
                ))}
              </ul>
            )}
          </Card>

          {selected && <Detail s={selected} redisCommands={d.redis_commands} />}
        </>
      )}
      <ConnectionDialog seed={dialog} onClose={() => setDialog(null)} onSaved={(id) => open("databases", [id])} />
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}
