import { useEffect, useMemo, useState } from "react"
import { ExternalLink, Link2, X } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Dialog } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/states"
import { errorMessage, getJSON } from "@/lib/api"
import type { Card } from "@/lib/boards"
import { linearApi, type LinearMeta, type LinearStatus } from "@/lib/linear"

const field =
  "h-9 w-full rounded-lg border bg-background px-3 text-sm outline-none transition-colors hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/40"

/** The card's link to a remote item: opens it there, and can unlink. The remote owns the item. */
export function RemoteLink({ label, id, url, onUnlink }: { label: string; id: string; url?: string; onUnlink: () => void }) {
  return (
    <div className="flex items-center gap-1.5">
      <a
        href={url}
        target="_blank"
        rel="noreferrer"
        className="flex min-w-0 flex-1 items-center gap-2 rounded-md border bg-background/60 px-2.5 py-1.5 text-sm hover:border-border-strong"
      >
        <span className="font-mono text-xs text-brand-fg">{id.length > 14 ? id.slice(0, 8) : id}</span>
        <span className="truncate">Open in {label}</span>
        <ExternalLink className="ml-auto size-3 shrink-0 text-subtle-foreground" />
      </a>
      <button
        type="button"
        aria-label={`Unlink from ${label}`}
        title="Unlink (the item stays in the remote)"
        onClick={onUnlink}
        className="inline-flex size-7 items-center justify-center rounded-md text-subtle-foreground hover:bg-accent hover:text-foreground"
      >
        <X className="size-3.5" />
      </button>
    </div>
  )
}

/** An inline "paste an identifier or URL" box that links the card to an existing remote item. */
export function LinkInput({
  placeholder,
  link,
  onLinked,
  onClose,
}: {
  placeholder: string
  link: (text: string) => Promise<Card>
  onLinked: (c: Card) => void
  onClose: () => void
}) {
  const [v, setV] = useState("")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState("")
  const submit = async () => {
    if (!v.trim() || busy) return
    setBusy(true)
    setErr("")
    try {
      onLinked(await link(v.trim()))
    } catch (e) {
      setErr(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div>
      <div className="flex gap-1.5">
        <input
          autoFocus
          value={v}
          onChange={(e) => setV(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void submit()
            if (e.key === "Escape") onClose()
          }}
          placeholder={placeholder}
          aria-label={placeholder}
          className="h-8 min-w-0 flex-1 rounded-md border bg-background/60 px-2.5 text-sm outline-none placeholder:text-subtle-foreground hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"
        />
        <Button size="sm" onClick={() => void submit()} disabled={busy || !v.trim()}>
          <Link2 /> {busy ? "Linking" : "Link"}
        </Button>
        <Button size="sm" variant="ghost" onClick={onClose}>
          Cancel
        </Button>
      </div>
      {err && <p className="mt-1.5 text-xs text-danger-fg">{err}</p>}
    </div>
  )
}

/**
 * "Promote to Linear": pick a team and a project, then create a real issue
 * titled like the card. Nothing is created before the button is pressed.
 */
export function PromoteDialog({ board, card, open, onClose, onDone }: { board: string; card: Card; open: boolean; onClose: () => void; onDone: (c: Card) => void }) {
  // The dialog sits over the card sheet, which also closes on Escape. Take the
  // key first (capture phase) so Escape closes only the dialog.
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return
      e.stopImmediatePropagation()
      onClose()
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [open, onClose])
  return (
    <Dialog open={open} onClose={onClose} title="Promote to Linear" description="Creates a new issue in your Linear workspace from this card, and links the two. Linear owns the issue afterwards.">
      {open && <PromoteForm board={board} card={card} onClose={onClose} onDone={onDone} />}
    </Dialog>
  )
}

function PromoteForm({ board, card, onClose, onDone }: { board: string; card: Card; onClose: () => void; onDone: (c: Card) => void }) {
  const [status, setStatus] = useState<LinearStatus | null>(null)
  const [meta, setMeta] = useState<LinearMeta | null>(null)
  const [error, setError] = useState("")
  const [team, setTeam] = useState("")
  const [project, setProject] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let cancelled = false
    getJSON<LinearStatus>("/api/linear/status")
      .then((s) => {
        if (cancelled) return
        setStatus(s)
        if (!s.configured) return
        return getJSON<LinearMeta>("/api/linear/meta").then((m) => {
          if (cancelled) return
          setMeta(m)
          setTeam(m.teams[0]?.id ?? "")
        })
      })
      .catch((e) => !cancelled && setError(errorMessage(e)))
    return () => {
      cancelled = true
    }
  }, [])

  const projects = useMemo(() => (meta?.projects ?? []).filter((p) => p.team_ids.includes(team)), [meta, team])
  const teamKey = meta?.teams.find((t) => t.id === team)?.key

  const create = async () => {
    setBusy(true)
    setError("")
    try {
      const r = await linearApi.promote(board, card.id, team, project)
      toast.success(`Created ${r.issue.identifier} in Linear`)
      onDone(r.card)
      onClose()
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  if (status && !status.configured) {
    return (
      <div className="space-y-4">
        <p className="text-sm text-muted-foreground">
          The Linear connector has no API key yet. Set <span className="font-mono text-foreground">{status.token_ref.replace(/^env:/, "")}</span> and restart, then try again. The Linear page walks through it.
        </p>
        <div className="flex justify-end">
          <Button variant="secondary" onClick={onClose}>
            Close
          </Button>
        </div>
      </div>
    )
  }
  return (
    <div className="space-y-4">
      <div>
        <div className="text-xs font-medium text-muted-foreground">Issue title</div>
        <div className="mt-1.5 rounded-lg border bg-muted/40 px-3 py-2 text-sm">{card.title}</div>
      </div>
      {!meta && !error ? (
        <Skeleton className="h-24 rounded-lg" />
      ) : meta ? (
        <>
          <div>
            <label htmlFor="promote-team" className="text-xs font-medium text-muted-foreground">
              Team
            </label>
            <select
              id="promote-team"
              value={team}
              onChange={(e) => {
                setTeam(e.target.value)
                setProject("")
              }}
              className={`${field} mt-1.5`}
            >
              {meta.teams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name} ({t.key})
                </option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor="promote-project" className="text-xs font-medium text-muted-foreground">
              Project
            </label>
            <select id="promote-project" value={project} onChange={(e) => setProject(e.target.value)} className={`${field} mt-1.5`}>
              <option value="">No project</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </div>
        </>
      ) : null}
      {error && (
        <p role="alert" className="rounded-lg border border-danger/30 bg-danger-soft px-3 py-2 text-sm text-danger-fg">
          {error}
        </p>
      )}
      <p className="text-xs text-subtle-foreground">
        This creates a real issue{teamKey ? ` in ${teamKey}` : ""}. The card keeps its identifier and link; changes in Linear do not flow back.
      </p>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="secondary" onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button type="button" onClick={() => void create()} disabled={busy || !team || !meta}>
          {busy ? "Creating" : "Create in Linear"}
        </Button>
      </div>
    </div>
  )
}
