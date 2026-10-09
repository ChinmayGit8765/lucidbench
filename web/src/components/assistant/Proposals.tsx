import { useState, type ReactNode } from "react"
import { Check, Copy, FileText, FolderPlus, Lightbulb, Link2, ListPlus, PenLine, SkipForward, SquareTerminal, StickyNote, TriangleAlert, Vote } from "lucide-react"
import { toast } from "sonner"

import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Dialog } from "@/components/ui/dialog"
import { errorMessage } from "@/lib/api"
import { ACTION_LABEL, appliedNote, checkAction, runRequests, type Proposal } from "@/lib/assistant"
import { cn } from "@/lib/utils"

const ICON: Record<string, typeof Check> = {
  create_card: StickyNote,
  create_project: FolderPlus,
  create_idea: Lightbulb,
  create_page: FileText,
  start_council: Vote,
  start_work: SquareTerminal,
  add_needs: ListPlus,
  link_builds_into: Link2,
}

export type Outcome = { status: "applied" | "skipped" | "failed"; note?: string; answer?: unknown }

/** What a spending action will do, said before it runs. */
function spendWords(p: Proposal): { title: string; description: string; label: string; text: string } {
  if (p.action === "start_work") {
    const provider = String(p.args.provider || "the project's builder")
    return {
      title: `Start ${provider} on ${String(p.args.project)}?`,
      description: `Lucidbench makes a new worktree next to the project's checkout and runs ${provider} there with your own account, so the run counts against your plan. Nothing is pushed until you open a PR.`,
      label: "Start session",
      text: String(p.args.prompt ?? ""),
    }
  }
  return {
    title: "Start a council on this braindump?",
    description: `The proposer and critics each run their own CLI on your accounts, so it counts against your plans: up to two rounds${p.args.project ? `, on ${String(p.args.project)}` : ""}.`,
    label: "Start council",
    text: String(p.args.braindump ?? ""),
  }
}

/**
 * Applying proposals: each is checked again against what is there now, a
 * spending one goes through the confirm dialog, and a projects.yaml change
 * shows its snippet to paste. Nothing runs without the user's click.
 */
export function useApplyProposals(bot?: string): {
  apply: (p: Proposal, done: (o: Outcome) => void) => Promise<void>
  applyMany: (ps: Proposal[], done: (i: number, o: Outcome) => void) => Promise<void>
  dialogs: ReactNode
} {
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [snippet, setSnippet] = useState<{ p: Proposal; done: (o: Outcome) => void } | null>(null)

  const recheck = async (p: Proposal): Promise<Proposal | null> => {
    try {
      const fresh = await checkAction(p.action, p.args, bot)
      if (!fresh.valid) {
        toast.error("This can no longer be applied", { description: fresh.problems.join(" ") })
        return null
      }
      return fresh
    } catch (e) {
      toast.error("Could not check the action", { description: errorMessage(e) })
      return null
    }
  }

  const run = async (p: Proposal, done: (o: Outcome) => void) => {
    try {
      const answer = await runRequests(p)
      done({ status: "applied", note: appliedNote(p, answer), answer })
    } catch (e) {
      const msg = errorMessage(e)
      toast.error(`Could not apply: ${p.summary}`, { description: msg })
      done({ status: "failed", note: msg.slice(0, 300) })
    }
  }

  const apply = async (p: Proposal, done: (o: Outcome) => void) => {
    const fresh = await recheck(p)
    if (!fresh) return
    if (!fresh.requests?.length) {
      if (fresh.snippet) setSnippet({ p: fresh, done })
      return
    }
    if (fresh.spends) {
      const w = spendWords(fresh)
      setConfirm({
        title: w.title,
        description: w.description,
        body: <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-2 text-xs">{w.text}</pre>,
        confirmLabel: w.label,
        run: () => run(fresh, done),
      })
      return
    }
    await run(fresh, done)
  }

  const applyMany = async (ps: Proposal[], done: (i: number, o: Outcome) => void) => {
    const fresh: { i: number; p: Proposal }[] = []
    for (const [i, p] of ps.entries()) {
      const f = await recheck(p)
      if (f?.requests?.length) fresh.push({ i, p: f })
    }
    const go = async () => {
      for (const { i, p } of fresh) await run(p, (o) => done(i, o))
    }
    const spending = fresh.filter((f) => f.p.spends)
    if (spending.length === 0) return go()
    setConfirm({
      title: `Apply ${fresh.length} items, ${spending.length} of them a council?`,
      description: "Each council runs the proposer and critics on your own accounts, so it counts against your plans. The other items only write to your board and Memory.",
      body: (
        <ul className="max-h-48 space-y-1 overflow-auto text-xs">
          {fresh.map(({ p }, k) => (
            <li key={k} className="flex items-center gap-2">
              <span className={cn("size-1.5 rounded-full", p.spends ? "bg-warning" : "bg-success")} />
              {p.summary}
            </li>
          ))}
        </ul>
      ),
      confirmLabel: "Apply all",
      run: go,
    })
  }

  const dialogs = (
    <>
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
      <SnippetDialog
        p={snippet?.p ?? null}
        onClose={() => setSnippet(null)}
        onDone={() => {
          snippet?.done({ status: "applied", note: "snippet copied for projects.yaml" })
          setSnippet(null)
        }}
      />
    </>
  )
  return { apply, applyMany, dialogs }
}

/** projects.yaml is the user's own file: Lucidbench never rewrites it, so the change is a snippet to paste. */
function SnippetDialog({ p, onClose, onDone }: { p: Proposal | null; onClose: () => void; onDone: () => void }) {
  const [copied, setCopied] = useState(false)
  if (!p) return null
  return (
    <Dialog
      open
      onClose={onClose}
      title="Paste this into projects.yaml"
      description="projects.yaml is yours: Lucidbench only ever appends new projects with a checkout, and never rewrites an entry. Paste the lines where the comment says."
    >
      <pre data-testid="assistant-snippet" className="max-h-64 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs">
        {p.snippet}
      </pre>
      <div className="mt-4 flex justify-end gap-2">
        <Button
          variant="secondary"
          onClick={() => {
            void navigator.clipboard?.writeText(p.snippet ?? "").then(() => setCopied(true))
          }}
        >
          {copied ? <Check /> : <Copy />} {copied ? "Copied" : "Copy"}
        </Button>
        <Button onClick={onDone}>Mark as done</Button>
      </div>
    </Dialog>
  )
}

const STATUS_TONE = { pending: "info", applied: "success", skipped: "neutral", failed: "danger" } as const

/** One proposed action: what it does, its problems, Apply and Skip. */
export function ProposalCard({
  p,
  onApply,
  onSkip,
  busy,
}: {
  p: Proposal
  onApply: () => void
  onSkip: () => void
  busy?: boolean
}) {
  const Icon = ICON[p.action] ?? PenLine
  const detail = detailOf(p)
  const pending = p.status === "pending"
  return (
    <div
      data-testid="assistant-proposal"
      data-action={p.action}
      data-status={p.status}
      className={cn("rounded-lg border bg-elevated/60 p-3", !p.valid && "border-danger/30 bg-danger-soft/30", !pending && "opacity-80")}
    >
      <div className="flex items-start gap-2.5">
        <span className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md bg-brand-soft text-brand-fg">
          <Icon className="size-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{ACTION_LABEL[p.action] ?? p.action}</span>
            {p.spends && <StatusPill tone="warning">spends</StatusPill>}
            {!p.valid && <StatusPill tone="danger">cannot apply</StatusPill>}
            {p.valid && p.snippet && !p.requests?.length && <StatusPill tone="neutral">snippet</StatusPill>}
            {!pending && <StatusPill tone={STATUS_TONE[p.status]}>{p.status}</StatusPill>}
          </div>
          <div className="mt-0.5 text-sm font-medium">{p.summary}</div>
          {detail && <p className="mt-1 line-clamp-3 whitespace-pre-wrap text-xs text-muted-foreground">{detail}</p>}
          {p.problems.length > 0 && (
            <ul className="mt-1.5 space-y-0.5 text-xs text-danger-fg">
              {p.problems.map((x, i) => (
                <li key={i} className="flex items-start gap-1">
                  <TriangleAlert className="mt-0.5 size-3 shrink-0" /> {x}
                </li>
              ))}
            </ul>
          )}
          {p.note && <p className="mt-1 text-2xs text-subtle-foreground">{p.note}</p>}
        </div>
        {pending && p.valid && (
          <div className="flex shrink-0 items-center gap-1">
            <Button size="sm" onClick={onApply} disabled={busy} data-testid="proposal-apply">
              <Check /> Apply
            </Button>
            <Button size="sm" variant="ghost" onClick={onSkip} disabled={busy} data-testid="proposal-skip">
              <SkipForward /> Skip
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

function detailOf(p: Proposal): string {
  const a = p.args
  const s = (k: string) => (typeof a[k] === "string" ? (a[k] as string) : "")
  switch (p.action) {
    case "create_card":
      return [s("project") && `on ${s("project")}`, s("column") && `in ${s("column")}`, s("body")].filter(Boolean).join(" · ")
    case "create_idea":
      return [s("project") && `on ${s("project")}`, s("body")].filter(Boolean).join(" · ")
    case "create_page":
      return s("markdown")
    case "start_council":
      return s("braindump")
    case "start_work":
      return [s("provider") || "the project's builder", s("model"), s("prompt")].filter(Boolean).join(" · ")
    case "create_project":
      return [s("kind"), s("local_path") || "no checkout yet"].filter(Boolean).join(" · ")
    default:
      return p.snippet ?? ""
  }
}
