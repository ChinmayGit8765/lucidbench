import type { ReactNode } from "react"
import { BookOpen, ExternalLink } from "lucide-react"

import { copyText } from "@/components/CopyCommand"
import { StatusPill } from "@/components/ui/badge"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
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

export function General() {
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
