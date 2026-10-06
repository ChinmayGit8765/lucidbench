import { useEffect, useRef, type ReactNode } from "react"
import { BookOpen, ExternalLink, Sparkles } from "lucide-react"

import { CopyCommand, copyText } from "@/components/CopyCommand"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { SETUP_ROUTE } from "@/lib/setup"
import { cn } from "@/lib/utils"
import { Section } from "@/pages/settings/controls"

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

interface ConfigView {
  vault: { path: string }
  sources: Record<string, string>
}

/**
 * Where Memory keeps its pages. The daemon reads config at start and has no
 * write route, so changing the vault is a config edit plus a restart; this
 * shows the effective path and exactly what to change.
 */
function VaultSection({ dataDir, focus }: { dataDir?: string; focus: boolean }) {
  const cfg = usePoll<ConfigView>("/api/config", 60000)
  const ref = useRef<HTMLElement>(null)
  useEffect(() => {
    if (focus) ref.current?.scrollIntoView({ behavior: "smooth", block: "start" })
  }, [focus])
  const c = cfg.data
  const configured = !!c?.vault.path
  const source = c?.sources["vault.path"] ?? "default"
  const effective = configured ? c!.vault.path : dataDir ? `${dataDir.replace(/[\\/]+$/, "")}${dataDir.includes("\\") ? "\\" : "/"}memory` : ""
  return (
    <section ref={ref} id="vault" className={cn("scroll-mt-6 rounded-xl", focus && "ring-2 ring-brand/40")}>
      <Section title="Memory vault" description="The folder of Markdown pages behind Memory and Boards. Any Obsidian vault works; Lucidbench never picks one by itself.">
        <dl>
          <Item label="Location" copy={effective || undefined}>
            {cfg.error && !c ? (
              <span className="text-xs text-danger-fg">{cfg.error.message}</span>
            ) : !c ? (
              <Skeleton className="h-5 w-64" />
            ) : (
              <>
                <span className="truncate font-mono text-xs" title={effective}>
                  {effective}
                </span>
                <StatusPill tone={configured ? "info" : "neutral"}>{configured ? `vault.path · ${source}` : "built-in default"}</StatusPill>
              </>
            )}
          </Item>
        </dl>
        <div className="space-y-3 border-t px-5 py-4">
          <div className="text-sm font-medium">Use a different vault</div>
          <ol className="list-decimal space-y-2 pl-5 text-sm text-muted-foreground">
            <li>
              Find your config file:
              <CopyCommand command="lucid config path" className="mt-1.5" />
            </li>
            <li>
              Set the vault folder in it (a leading <code className="font-mono text-xs">~</code> is your home folder):
              <pre className="mt-1.5 overflow-x-auto rounded-lg border bg-background px-3 py-2 font-mono text-xs text-foreground">{"vault:\n  path: ~/Notes"}</pre>
              or set the <code className="font-mono text-xs">LUCID_VAULT_PATH</code> environment variable.
            </li>
            <li>Restart Lucidbench. Memory and Boards then open that folder; nothing is copied or moved.</li>
          </ol>
        </div>
      </Section>
    </section>
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
