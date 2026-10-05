import { ArrowDown, ArrowUp, Minus, RotateCcw } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useApp } from "@/lib/app"
import { usePrefs, type Prefs } from "@/lib/prefs"
import { sidebarGroups } from "@/modules/registry"
import type { ModuleDef } from "@/modules/types"
import { Section } from "@/pages/settings/controls"

/** Writes a group's new order: index i becomes order i. */
function reorder(p: Prefs, items: ModuleDef[]): Prefs {
  const modules = { ...p.modules }
  const extensions = { ...p.extensions }
  items.forEach((m, i) => {
    if (m.kind === "core") modules[m.id] = { order: i }
    else extensions[m.id] = { added: true, order: i }
  })
  return { ...p, modules, extensions }
}

function Group({ title, hint, items, removable }: { title: string; hint?: string; items: ModuleDef[]; removable?: boolean }) {
  const { update } = usePrefs()
  const move = (i: number, d: number) => {
    const next = [...items]
    const [m] = next.splice(i, 1)
    next.splice(i + d, 0, m)
    update((p) => reorder(p, next))
  }
  const remove = (m: ModuleDef) =>
    update((p) => ({ ...p, extensions: { ...p.extensions, [m.id]: { added: false, order: p.extensions[m.id]?.order ?? m.order } } }))
  return (
    <div className="border-t">
      <div className="flex items-baseline gap-2 px-5 pb-1 pt-3">
        <h3 className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{title}</h3>
        {hint && <span className="text-2xs text-subtle-foreground">{hint}</span>}
      </div>
      <ul className="pb-2">
        {items.map((m, i) => {
          const Icon = m.icon
          return (
            <li key={m.id} className="group flex h-10 items-center gap-3 px-5 transition-colors hover:bg-accent/30">
              <span className="flex size-6 items-center justify-center rounded-md border bg-background/60 text-subtle-foreground">
                <Icon className="size-3.5" />
              </span>
              <span className={m.status === "soon" ? "flex-1 text-sm text-subtle-foreground" : "flex-1 text-sm"}>{m.title}</span>
              {m.status === "soon" && (
                <span className="rounded border border-dashed border-border-strong px-1.5 text-2xs leading-4 text-subtle-foreground">
                  {m.milestone ? `${m.milestone} · soon` : "soon"}
                </span>
              )}
              <div className="flex items-center gap-0.5 opacity-70 transition-opacity group-hover:opacity-100">
                <Button variant="ghost" size="icon-sm" aria-label={`Move ${m.title} up`} disabled={i === 0} onClick={() => move(i, -1)}>
                  <ArrowUp />
                </Button>
                <Button variant="ghost" size="icon-sm" aria-label={`Move ${m.title} down`} disabled={i === items.length - 1} onClick={() => move(i, 1)}>
                  <ArrowDown />
                </Button>
                {removable && (
                  <Button variant="ghost" size="icon-sm" aria-label={`Remove ${m.title} from the sidebar`} title="Remove from the sidebar" onClick={() => remove(m)}>
                    <Minus />
                  </Button>
                )}
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

export function SidebarLayout() {
  const { prefs, update } = usePrefs()
  const { open } = useApp()
  const { main, bottom } = sidebarGroups(prefs)
  const core = main.filter((g) => g.id !== "extensions")
  const ext = main.find((g) => g.id === "extensions")
  const customised = Object.keys(prefs.modules).length > 0 || Object.values(prefs.extensions).some((e) => e.added)
  return (
    <Section
      title="Sidebar"
      description="Core modules are always there; move them to suit you. Extensions appear here once you add them."
      actions={
        customised ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => update((p) => ({ ...p, modules: {}, extensions: Object.fromEntries(Object.entries(p.extensions).map(([id, e]) => [id, { ...e, order: 0 }])) }))}
          >
            <RotateCcw /> Default order
          </Button>
        ) : undefined
      }
    >
      {core.map((g) => (
        <Group key={g.id} title={g.label} items={g.items} />
      ))}
      {ext ? (
        <Group title="Extensions" hint="added from the gallery" items={ext.items} removable />
      ) : (
        <div className="border-t px-5 py-4 text-sm text-muted-foreground">
          No extensions in the sidebar.{" "}
          <button className="text-brand-fg underline-offset-2 hover:underline" onClick={() => open("settings", ["extensions"])}>
            Browse extensions
          </button>
        </div>
      )}
      <Group title="Pinned to the bottom" items={bottom} />
    </Section>
  )
}
