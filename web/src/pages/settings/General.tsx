import { useEffect, useRef, useState, type ReactNode } from "react"
import { BookOpen, ExternalLink, RefreshCw, Sparkles } from "lucide-react"
import { toast } from "sonner"

import { PendingRestart, RestartEngineButton, SaveButton, useConfigFile, useConfigSave } from "@/components/ConfigSave"
import { copyText } from "@/components/CopyCommand"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { SETUP_ROUTE } from "@/lib/setup"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { checkNow, UPDATE_PATH, UPDATE_POLL_MS, type UpdateStatus } from "@/lib/update"
import { cn } from "@/lib/utils"
import { Section, Switch } from "@/pages/settings/controls"

interface About {
  version: string
  config_file: string
  config_found: boolean
  data_dir: string
  in_container: boolean
  os: string
}

const DOCS = "https://github.com/ChinmayGit8765/lucidbench/blob/main/docs"

function Item({ label, children, copy }: { label: string; children: ReactNode; copy?: string }) {
  return (
    <div className="grid grid-cols-[10rem_minmax(0,1fr)] items-center gap-4 border-t px-5 py-3 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 items-center gap-2">
        {children}
        {copy && (
          <button onClick={() => void copyText(copy, "Path copied")} className="text-2xs text-subtle-foreground hover:text-foreground">
            Copy
          </button>
        )}
      </dd>
    </div>
  )
}

/**
 * Where Memory keeps its pages. Save writes vault.path to config.yaml (after
 * showing the diff); Memory and Boards open the new folder once Lucidbench
 * restarts, and nothing is copied or moved.
 */
function VaultSection({ dataDir, focus }: { dataDir?: string; focus: boolean }) {
  const file = useConfigFile()
  const ref = useRef<HTMLElement>(null)
  useEffect(() => {
    if (focus) ref.current?.scrollIntoView({ behavior: "smooth", block: "start" })
  }, [focus])
  const f = file.data
  const saved = typeof f?.values["vault.path"] === "string" ? (f.values["vault.path"] as string) : ""
  const [draft, setDraft] = useState<string | null>(null)
  const value = draft ?? saved
  // The engine shows the saved path with ~ expanded, so the box follows it.
  const { save, busy, dialog } = useConfigSave(() => {
    setDraft(null)
    file.refresh()
  })
  const source = f?.sources["vault.path"] ?? "default"
  const fromEnv = source.startsWith("env:")
  const fallback = dataDir ? `${dataDir.replace(/[\\/]+$/, "")}${dataDir.includes("\\") ? "\\" : "/"}memory` : ""
  const dirty = draft !== null && draft.trim() !== saved
  return (
    <section ref={ref} id="vault" className={cn("scroll-mt-6 rounded-xl", focus && "ring-2 ring-brand/40")}>
      <Section
        title="Memory vault"
        description="The folder of Markdown pages behind Memory and Boards. Any Obsidian vault works; Lucidbench never picks one by itself. Saved to config.yaml; applies when the engine restarts."
      >
        <div className="flex flex-wrap items-center gap-2 border-t px-5 py-3">
          {file.error && !f ? (
            <span className="text-xs text-danger-fg">{file.error.message}</span>
          ) : !f ? (
            <Skeleton className="h-8 w-80" />
          ) : (
            <>
              <input
                aria-label="Vault folder"
                value={value}
                onChange={(e) => setDraft(e.target.value)}
                placeholder={fallback ? `${fallback} (built-in default)` : "~/Notes"}
                spellCheck={false}
                disabled={fromEnv}
                className="h-8 min-w-0 flex-1 rounded-md border bg-background px-2.5 font-mono text-xs outline-none placeholder:text-subtle-foreground focus-visible:ring-2 focus-visible:ring-ring"
              />
              <StatusPill tone={saved ? "info" : "neutral"}>{saved ? `vault.path · ${source}` : "built-in default"}</StatusPill>
              <SaveButton dirty={dirty && !fromEnv} busy={busy} onClick={() => void save({ "vault.path": value.trim() }, "the vault folder")} />
            </>
          )}
        </div>
        <p className="border-t px-5 py-2.5 text-xs text-muted-foreground">
          A leading <code className="font-mono">~</code> is your home folder. Leave it empty for the built-in folder in the data directory.
          {fromEnv && <span className="text-warning-fg"> {source.slice(4)} sets it, which wins over the file; change or unset that variable instead.</span>}
        </p>
        <PendingRestart file={f} keys={["vault.path"]} />
      </Section>
      {dialog}
    </section>
  )
}

/** Turns the update check on or off in config.yaml. */
function UpdateToggle() {
  const file = useConfigFile()
  const { save, dialog } = useConfigSave(() => file.refresh())
  const f = file.data
  if (!f) return <Skeleton className="h-5 w-40" />
  const on = f.values["update_check"] === true
  const src = f.sources["update_check"] ?? "default"
  return (
    <>
      <Switch label="Check for updates" checked={on} onChange={(v) => void save({ update_check: v }, v ? "update checks on" : "update checks off")} />
      <span className="text-xs text-muted-foreground">
        {on ? "On" : "Off"}
        {src.startsWith("env:") ? ` · ${src.slice(4)} overrides the file` : ""}
        {f.pending.includes("update_check") ? " · saved, the engine uses it after a restart" : ""}
      </span>
      {f.pending.includes("update_check") && <RestartEngineButton />}
      {dialog}
    </>
  )
}

/**
 * Updates: what is installed, the newest release found and a manual check.
 * Nothing is downloaded; the links open the release page.
 */
function UpdatesSection() {
  const poll = usePoll<UpdateStatus>(UPDATE_PATH, UPDATE_POLL_MS)
  const [busy, setBusy] = useState(false)
  const now = useNow(60000)
  const u = poll.data
  const check = async () => {
    setBusy(true)
    try {
      const r = await checkNow()
      poll.refresh()
      if (r.offline) toast.message("Could not reach GitHub. Lucidbench tries again later.")
      else if (r.available) toast.success(`Lucidbench ${r.latest} is out.`)
      else if (r.enabled && r.latest) toast.success("You have the newest version.")
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Section
      title="Updates"
      description="Lucidbench asks GitHub at most every six hours whether a newer version is out. It never downloads or installs anything."
      actions={
        <Button variant="secondary" size="sm" onClick={() => void check()} disabled={busy || !u?.enabled || !!u?.note?.includes("development build")} data-testid="update-check">
          <RefreshCw className={cn(busy && "animate-spin")} /> Check for updates
        </Button>
      }
    >
      {!u ? (
        <div className="border-t p-5">
          <Skeleton className="h-5 w-64" />
        </div>
      ) : (
        <dl data-testid="update-status">
          <Item label="Installed">
            <span className="font-mono">{u.current}</span>
          </Item>
          <Item label="Newest release">
            {u.latest ? (
              <>
                <span className="font-mono">{u.latest}</span>
                {u.available ? <StatusPill tone="info">update available</StatusPill> : <StatusPill tone="success">up to date</StatusPill>}
                {u.url && (
                  <a href={u.url} target="_blank" rel="noreferrer" className="text-xs text-brand hover:underline">
                    Release notes
                  </a>
                )}
                {u.available && u.download_url && (
                  <a href={u.download_url} target="_blank" rel="noreferrer" className="text-xs text-brand hover:underline">
                    Download
                  </a>
                )}
              </>
            ) : (
              <span className="text-muted-foreground">{u.note ?? "Not checked yet."}</span>
            )}
          </Item>
          {u.checked_at && (
            <Item label="Last checked">
              <span className="text-muted-foreground" title={absoluteTime(u.checked_at)}>
                {relativeTime(u.checked_at, now)}
                {u.offline ? " (GitHub could not be reached)" : ""}
              </span>
            </Item>
          )}
          <Item label="Update checks">
            <UpdateToggle />
          </Item>
        </dl>
      )}
    </Section>
  )
}

export function General({ focus }: { focus?: string }) {
  const { navigate } = useApp()
  const about = usePoll<About>("/api/about", 60000)
  const a = about.data
  return (
    <div className="space-y-5">
      <Section title="Engine" description="The lucidd daemon this window talks to.">
        {about.error && !a ? (
          <div className="border-t p-5">
            <ErrorState title="Could not read engine details" message={about.error.message} onRetry={about.refresh} />
          </div>
        ) : !a ? (
          <div className="space-y-2 border-t p-5">
            <Skeleton className="h-5 w-64" />
            <Skeleton className="h-5 w-80" />
          </div>
        ) : (
          <dl>
            <Item label="Version">
              <span className="font-mono">lucidd {a.version}</span>
            </Item>
            <Item label="Address">
              <span className="font-mono">{location.host}</span>
            </Item>
            <Item label="Runs on">
              {a.in_container ? <StatusPill tone="info">Docker container</StatusPill> : <StatusPill tone="success">This machine</StatusPill>}
              <span className="text-xs text-muted-foreground">{a.os}</span>
            </Item>
            <Item label="Config file" copy={a.config_file}>
              <span className="truncate font-mono text-xs" title={a.config_file}>
                {a.config_file}
              </span>
              {!a.config_found && <span className="text-xs text-subtle-foreground">(not created yet: built-in defaults apply)</span>}
            </Item>
            <Item label="Data folder" copy={a.data_dir}>
              <span className="truncate font-mono text-xs" title={a.data_dir}>
                {a.data_dir}
              </span>
            </Item>
            <Item label="Harness">
              <span className="text-muted-foreground">Not installed — recommended harness coming soon</span>
            </Item>
          </dl>
        )}
      </Section>

      <UpdatesSection />

      <VaultSection dataDir={a?.data_dir} focus={focus === "vault"} />

      <Section
        title="First-run setup"
        description="Accounts, the Memory vault, your projects, a theme and power modes, one calm step at a time. It changes nothing until you confirm."
        actions={
          <Button variant="secondary" size="sm" onClick={() => navigate(SETUP_ROUTE)} data-testid="run-setup">
            <Sparkles /> Run setup again
          </Button>
        }
      >
        {null}
      </Section>

      <Section title="Documentation" description="How to configure Lucidbench and how to add a module of your own.">
        <ul className="border-t py-1">
          {[
            ["Configuration", "Every config key, environment variable and file location", `${DOCS}/CONFIG.md`],
            ["Extending Lucidbench", "Write a core module or an extension in one file", `${DOCS}/EXTENDING.md`],
          ].map(([title, hint, href]) => (
            <li key={href}>
              <a href={href} target="_blank" rel="noreferrer" className="group flex items-center gap-3 px-5 py-2.5 transition-colors hover:bg-accent/30">
                <BookOpen className="size-4 text-subtle-foreground" />
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium">{title}</span>
                  <span className="block truncate text-xs text-muted-foreground">{hint}</span>
                </span>
                <ExternalLink className="size-3.5 text-subtle-foreground group-hover:text-foreground" />
              </a>
            </li>
          ))}
        </ul>
      </Section>
    </div>
  )
}
