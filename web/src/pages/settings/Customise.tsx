import { useEffect, useState } from "react"
import { ArrowRight, LayoutDashboard, Lightbulb, Palette, Sparkles, type LucideIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { PROJECTS_POLL_MS, type ProjectList } from "@/lib/projects"
import { describeFeature, FEATURE_CONTEXT } from "@/lib/customise"
import { Section } from "@/pages/settings/controls"

function Tier({
  n,
  icon: Icon,
  title,
  what,
  how,
  action,
  onAction,
}: {
  n: number
  icon: LucideIcon
  title: string
  what: string
  how: string
  action: string
  onAction: () => void
}) {
  return (
    <li className="flex flex-col gap-2 rounded-xl border bg-background/40 p-4">
      <div className="flex items-center gap-2">
        <span className="flex size-8 items-center justify-center rounded-lg bg-brand-soft text-brand-fg">
          <Icon className="size-4" />
        </span>
        <div className="min-w-0">
          <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Tier {n}</div>
          <h3 className="text-sm font-semibold">{title}</h3>
        </div>
      </div>
      <p className="text-sm text-muted-foreground">{what}</p>
      <p className="flex-1 text-xs text-subtle-foreground">{how}</p>
      <Button variant="secondary" size="sm" className="self-start" onClick={onAction}>
        {action} <ArrowRight />
      </Button>
    </li>
  )
}

/** Settings › Customise: the three ways to make Lucidbench yours, with AI. */
export function Customise({ focus }: { focus?: string }) {
  const { open } = useApp()
  useEffect(() => {
    if (focus !== "feature") return
    const el = document.getElementById("feature")
    el?.scrollIntoView({ behavior: "smooth", block: "center" })
    el?.focus()
  }, [focus])
  const projects = usePoll<ProjectList>("/api/projects", PROJECTS_POLL_MS)
  const [text, setText] = useState("")

  return (
    <div className="space-y-5">
      <Section
        title="Make Lucidbench yours"
        description="Three tiers, from safest to biggest. Each one is described in your own words and drafted by your own signed-in CLI; you see the result before anything changes."
      >
        <ol className="grid gap-3 border-t p-4 @3xl:grid-cols-3">
          <Tier
            n={1}
            icon={Palette}
            title="Reskin"
            what="Colours, fonts, density and original art, as a theme."
            how="A theme is data: design tokens checked one by one and sanitised SVG. It can change how things look, never what they do."
            action="Describe a theme"
            onAction={() => open("settings", ["appearance", "describe"])}
          />
          <Tier
            n={2}
            icon={LayoutDashboard}
            title="Sections"
            what="Widgets on the Overview and on each project's page: a number, a list, a table, bars or text."
            how="A section is JSON that reads one read-only Lucidbench route from an allowlist. No URLs, no scripts, no code: only field paths and a view."
            action="Open sections"
            onAction={() => open("settings", ["sections"])}
          />
          <Tier
            n={3}
            icon={Lightbulb}
            title="Features"
            what="Something Lucidbench cannot do yet."
            how="Nothing is injected into the running app. A feature becomes a braindump in the Council, and the normal loop takes it from there: a brief, a card, an agent on the Lucidbench repo or your fork, a PR you review."
            action="Describe a feature"
            onAction={() => document.getElementById("feature")?.focus()}
          />
        </ol>
      </Section>

      <Section
        id="describe-feature"
        title="Describe a feature"
        description="Write what you want Lucidbench to do. It opens in the Council as a braindump, with a line saying this is a request for Lucidbench itself; nothing runs until you convene the council there."
      >
        <div className="space-y-3 border-t p-5">
          <textarea
            id="feature"
            value={text}
            onChange={(e) => setText(e.target.value)}
            maxLength={18000}
            rows={5}
            aria-label="Describe a feature"
            placeholder="A weekly digest page: what shipped, what is stuck, and what it cost, every Monday…"
            className="w-full resize-y rounded-lg border bg-background/60 px-3 py-2 text-sm outline-none transition-colors placeholder:text-subtle-foreground focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/40"
          />
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="max-w-xl text-xs text-subtle-foreground">The braindump starts with: “{FEATURE_CONTEXT}”</p>
            <Button onClick={() => describeFeature(text, projects.data?.projects ?? [], open)} disabled={text.trim().length < 3}>
              <Sparkles /> Open in the Council
            </Button>
          </div>
        </div>
      </Section>
    </div>
  )
}
