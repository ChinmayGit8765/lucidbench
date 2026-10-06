import { useState } from "react"
import { AlertTriangle, ArrowLeft, Check, CircleSlash, FlaskConical, KeyRound, Link2, Plus, Rocket, ShieldAlert, Tag, Trash2, Webhook as WebhookIcon, X, Boxes } from "lucide-react"
import { toast } from "sonner"

import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { EmptyState, Skeleton } from "@/components/ui/states"
import { errorMessage, refreshAll, usePoll } from "@/lib/api"
import {
  paymentsApi,
  toMinor,
  type ExecuteResult,
  type Plan,
  type PlanRequest,
  type PaymentsStatus,
  type StepResult,
} from "@/lib/payments"
import { PROJECTS_POLL_MS, type ProjectList } from "@/lib/projects"
import { cn, plural } from "@/lib/utils"
import { CopyButton } from "@/pages/payments/CopyButton"

const field =
  "h-8 w-full rounded-md border bg-background/60 px-2.5 text-sm outline-none transition-colors placeholder:text-subtle-foreground hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"

interface Row {
  amount: string
  currency: string
  interval: string
  nickname: string
}

interface Form {
  project: string
  name: string
  description: string
  prices: Row[]
  successUrl: string
  webhookUrl: string
  events: string
}

const emptyRow = (): Row => ({ amount: "", currency: "usd", interval: "", nickname: "" })

const INITIAL: Form = {
  project: "",
  name: "",
  description: "",
  prices: [{ amount: "", currency: "usd", interval: "month", nickname: "" }],
  successUrl: "",
  webhookUrl: "",
  events: "checkout.session.completed",
}

function toRequest(f: Form): PlanRequest {
  const events = f.events.split(/[\s,]+/).filter(Boolean)
  return {
    project: f.project,
    product_name: f.name,
    description: f.description || undefined,
    prices: f.prices.map((p) => ({
      amount: toMinor(p.amount, p.currency),
      currency: p.currency,
      interval: p.interval || undefined,
      nickname: p.nickname || undefined,
    })),
    success_url: f.successUrl,
    webhook_url: f.webhookUrl || undefined,
    webhook_events: f.webhookUrl ? events : undefined,
  }
}

type Phase = { name: "form" } | { name: "review"; plan: Plan } | { name: "result"; plan: Plan; result: ExecuteResult }

/** "Set up payments for <project>": form, plan review, then the executed result. */
export function SetupWizard({ status }: { status: PaymentsStatus }) {
  const projects = usePoll<ProjectList>("/api/projects", PROJECTS_POLL_MS)
  const [form, setForm] = useState<Form>(INITIAL)
  const [phase, setPhase] = useState<Phase>({ name: "form" })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)

  const list = projects.data?.projects ?? []
  const project = form.project || list[0]?.id || ""

  const review = async () => {
    setBusy(true)
    setError(null)
    try {
      const plan = await paymentsApi.plan(toRequest({ ...form, project }))
      setPhase({ name: "review", plan })
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  // The plan goes back exactly as it came: the server checks it against its digest.
  const run = async (plan: Plan) => {
    try {
      const result = await paymentsApi.execute(plan)
      setPhase({ name: "result", plan, result })
      refreshAll()
      if (result.status === "complete") toast.success("Created in your Stripe test account")
      else toast.warning(result.status === "partial" ? "Some steps failed" : "Nothing was created")
    } catch (e) {
      toast.error("Could not run the plan", { description: errorMessage(e) })
    }
  }

  const askRun = (plan: Plan) =>
    setConfirm({
      title: "Create these objects in your Stripe test account?",
      description: `${plural(plan.steps.length, "object")} will be created in test mode for ${plan.project}. Nothing moves money, and each write is recorded in the audit log.`,
      confirmLabel: "Create in test mode",
      run: () => run(plan),
    })

  const reset = () => {
    // Drops the result, and with it the webhook secret held in this state.
    setPhase({ name: "form" })
    setForm({ ...INITIAL, project })
    setError(null)
  }

  if (projects.error && !projects.data) {
    return (
      <Card>
        <EmptyState icon={<AlertTriangle />} title="Could not read your projects" description={projects.error.message} />
      </Card>
    )
  }
  if (!projects.data) return <Skeleton className="h-80 rounded-xl" />
  if (list.length === 0) {
    return (
      <Card className="border-dashed">
        <EmptyState icon={<Boxes />} title="Add a project first" description="A payments setup belongs to one of your projects. Add one under Projects, then come back." />
      </Card>
    )
  }

  return (
    <>
      {!status.writes_allowed && (
        <div role="note" className="flex items-start gap-2.5 rounded-lg border border-warning/40 bg-warning-soft px-3.5 py-3 text-sm text-warning-fg">
          <ShieldAlert className="mt-0.5 size-4 shrink-0" />
          <div>
            {status.mode === "live" ? (
              <>
                <strong>Live mode writes are not supported in this version.</strong> You can build and read a plan, but it cannot be run against a live account. Use a test-mode key to create objects.
              </>
            ) : (
              <>
                <strong>No test key is available.</strong> You can build and read a plan, but nothing can be created until a test-mode key is set.
              </>
            )}
          </div>
        </div>
      )}
      {phase.name === "form" && (
        <FormCard form={{ ...form, project }} projects={list.map((p) => ({ id: p.id, name: p.name }))} onChange={setForm} onReview={() => void review()} busy={busy} error={error} />
      )}
      {phase.name === "review" && (
        <ReviewCard plan={phase.plan} onBack={() => setPhase({ name: "form" })} onRun={() => askRun(phase.plan)} />
      )}
      {phase.name === "result" && <ResultCard plan={phase.plan} result={phase.result} onRetry={() => askRun(phase.plan)} onDone={reset} />}
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

function Label({ text, hint, children }: { text: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="block min-w-0">
      <span className="mb-1 block text-xs font-medium text-muted-foreground">{text}</span>
      {children}
      {hint && <span className="mt-1 block text-2xs text-subtle-foreground">{hint}</span>}
    </label>
  )
}

function FormCard({
  form,
  projects,
  onChange,
  onReview,
  busy,
  error,
}: {
  form: Form
  projects: { id: string; name: string }[]
  onChange: (f: Form) => void
  onReview: () => void
  busy: boolean
  error: string | null
}) {
  const set = (patch: Partial<Form>) => onChange({ ...form, ...patch })
  const setRow = (i: number, patch: Partial<Row>) => set({ prices: form.prices.map((r, j) => (j === i ? { ...r, ...patch } : r)) })
  return (
    <Card className="p-5">
      <div className="flex items-center gap-2.5">
        <span className="flex size-8 items-center justify-center rounded-lg border border-border-strong bg-elevated text-brand">
          <Rocket className="size-4" />
        </span>
        <div>
          <h2 className="text-sm font-semibold">Set up payments for a project</h2>
          <p className="text-xs text-muted-foreground">Fill in the form and review the plan. Nothing is created until you confirm.</p>
        </div>
      </div>
      <div className="mt-5 grid gap-4 @3xl:grid-cols-2">
        <Label text="Project">
          <select aria-label="Project" value={form.project} onChange={(e) => set({ project: e.target.value })} className={field}>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name || p.id}
              </option>
            ))}
          </select>
        </Label>
        <Label text="Product name">
          <input aria-label="Product name" value={form.name} onChange={(e) => set({ name: e.target.value })} placeholder="Pro plan" className={field} />
        </Label>
        <div className="@3xl:col-span-2">
          <Label text="Description (optional)">
            <input aria-label="Description" value={form.description} onChange={(e) => set({ description: e.target.value })} placeholder="What buyers get" className={field} />
          </Label>
        </div>
      </div>

      <div className="mt-5">
        <div className="mb-2 flex items-center justify-between">
          <h3 className="text-xs font-medium text-muted-foreground">Prices</h3>
          <Button variant="ghost" size="sm" onClick={() => set({ prices: [...form.prices, emptyRow()] })} disabled={form.prices.length >= 10}>
            <Plus /> Add a price
          </Button>
        </div>
        <div className="space-y-2">
          {form.prices.map((r, i) => (
            <div key={i} className="grid items-end gap-2 @3xl:grid-cols-[8rem_6rem_9rem_minmax(0,1fr)_2rem]">
              <Label text={i === 0 ? "Amount" : ""}>
                <input aria-label={`Price ${i + 1} amount`} inputMode="decimal" value={r.amount} onChange={(e) => setRow(i, { amount: e.target.value })} placeholder="49.00" className={cn(field, "tabular-nums")} />
              </Label>
              <Label text={i === 0 ? "Currency" : ""}>
                <input aria-label={`Price ${i + 1} currency`} value={r.currency} maxLength={3} onChange={(e) => setRow(i, { currency: e.target.value.toLowerCase() })} className={cn(field, "font-mono uppercase")} />
              </Label>
              <Label text={i === 0 ? "Billing" : ""}>
                <select aria-label={`Price ${i + 1} billing`} value={r.interval} onChange={(e) => setRow(i, { interval: e.target.value })} className={field}>
                  <option value="">One-off</option>
                  <option value="day">Every day</option>
                  <option value="week">Every week</option>
                  <option value="month">Every month</option>
                  <option value="year">Every year</option>
                </select>
              </Label>
              <Label text={i === 0 ? "Nickname (optional)" : ""}>
                <input aria-label={`Price ${i + 1} nickname`} value={r.nickname} onChange={(e) => setRow(i, { nickname: e.target.value })} placeholder="Monthly" className={field} />
              </Label>
              <Button variant="ghost" size="icon-sm" aria-label={`Remove price ${i + 1}`} onClick={() => set({ prices: form.prices.filter((_, j) => j !== i) })} disabled={form.prices.length === 1}>
                <Trash2 />
              </Button>
            </div>
          ))}
        </div>
      </div>

      <div className="mt-5 grid gap-4 @3xl:grid-cols-2">
        <div className="@3xl:col-span-2">
          <Label text="Success URL" hint="Where buyers land after paying.">
            <input aria-label="Success URL" value={form.successUrl} onChange={(e) => set({ successUrl: e.target.value })} placeholder="https://example.com/thanks" className={field} />
          </Label>
        </div>
        <Label text="Webhook URL (optional)" hint="Stripe tells this https address when something happens.">
          <input aria-label="Webhook URL" value={form.webhookUrl} onChange={(e) => set({ webhookUrl: e.target.value })} placeholder="https://example.com/hooks/stripe" className={field} />
        </Label>
        <Label text="Webhook events" hint="Separate with commas.">
          <input aria-label="Webhook events" value={form.events} onChange={(e) => set({ events: e.target.value })} disabled={!form.webhookUrl} className={cn(field, "font-mono text-xs", !form.webhookUrl && "opacity-50")} />
        </Label>
      </div>

      {error && (
        <div role="alert" className="mt-4 flex items-start gap-2 rounded-lg border border-danger/30 bg-danger-soft px-3 py-2 text-sm text-danger-fg">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          {error}
        </div>
      )}
      <div className="mt-5 flex justify-end">
        <Button onClick={onReview} disabled={busy}>
          {busy ? "Building" : "Review the plan"}
        </Button>
      </div>
    </Card>
  )
}

const KIND_ICON: Record<string, typeof Tag> = { product: Boxes, price: Tag, payment_link: Link2, webhook_endpoint: WebhookIcon }

function ReviewCard({ plan, onBack, onRun }: { plan: Plan; onBack: () => void; onRun: () => void }) {
  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center gap-3 px-5 py-4">
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">Review the plan for {plan.project}</h2>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {plural(plan.steps.length, "object")} would be created, in this order. Building the plan changed nothing.
          </p>
        </div>
        <StatusPill tone={plan.mode === "live" ? "warning" : "info"}>{plan.mode === "live" ? "LIVE key" : plan.mode === "test" ? "TEST key" : "no test key"}</StatusPill>
      </div>
      <ol className="border-t">
        {plan.steps.map((s, i) => {
          const Icon = KIND_ICON[s.kind] ?? Tag
          return (
            <li key={s.id} className="flex gap-3 border-b px-5 py-3 last:border-b-0">
              <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border bg-background/60 text-2xs font-semibold tabular-nums text-muted-foreground">{i + 1}</span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <Icon className="size-3.5 text-subtle-foreground" />
                  {s.title}
                </div>
                <div className="mt-0.5 text-xs text-muted-foreground">{s.detail}</div>
                <div className="mt-1 truncate font-mono text-2xs text-subtle-foreground" title={s.idempotency_key}>
                  POST {s.path} · key {s.idempotency_key}
                </div>
              </div>
            </li>
          )
        })}
      </ol>
      {plan.warnings.length > 0 && (
        <ul className="space-y-1 border-t bg-warning-soft px-5 py-3 text-xs text-warning-fg">
          {plan.warnings.map((w) => (
            <li key={w} className="flex gap-2">
              <AlertTriangle className="mt-px size-3.5 shrink-0" />
              {w}
            </li>
          ))}
        </ul>
      )}
      {!plan.writable && (
        <div role="alert" className="flex items-start gap-2.5 border-t border-warning/40 bg-warning-soft px-5 py-3 text-sm text-warning-fg">
          <CircleSlash className="mt-0.5 size-4 shrink-0" />
          <div>
            <strong>This plan cannot be run.</strong> {plan.refusal ? plan.refusal.charAt(0).toUpperCase() + plan.refusal.slice(1) : "Writes need a test-mode key"}.
          </div>
        </div>
      )}
      <div className="flex items-center justify-between gap-2 border-t px-5 py-3">
        <Button variant="ghost" onClick={onBack}>
          <ArrowLeft /> Edit the form
        </Button>
        <Button onClick={onRun} disabled={!plan.writable}>
          <FlaskConical /> Create in test mode
        </Button>
      </div>
    </Card>
  )
}

const RESULT_TONE = { complete: "success", partial: "warning", failed: "danger" } as const
const RESULT_LABEL = { complete: "All created", partial: "Partly created", failed: "Nothing created" } as const

function StepRow({ s }: { s: StepResult }) {
  const Icon = s.status === "created" ? Check : s.status === "failed" ? X : CircleSlash
  return (
    <li className="flex gap-3 border-b px-5 py-3 last:border-b-0">
      <span
        className={cn(
          "mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border",
          s.status === "created" ? "border-success/40 bg-success-soft text-success-fg" : s.status === "failed" ? "border-danger/40 bg-danger-soft text-danger-fg" : "bg-muted text-subtle-foreground",
        )}
      >
        <Icon className="size-3.5" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{s.title}</div>
        {s.object_id && (
          <div className="mt-0.5 flex items-center gap-2 font-mono text-xs text-muted-foreground">
            {s.object_id}
            {s.url && s.kind === "payment_link" && (
              <a href={s.url} target="_blank" rel="noreferrer" className="truncate text-brand-fg hover:underline">
                {s.url}
              </a>
            )}
          </div>
        )}
        {s.status === "failed" && <div className="mt-0.5 text-xs text-danger-fg">{s.error}</div>}
        {s.status === "skipped" && <div className="mt-0.5 text-xs text-subtle-foreground">Skipped because an earlier step failed.</div>}
      </div>
    </li>
  )
}

function ResultCard({ plan, result, onRetry, onDone }: { plan: Plan; result: ExecuteResult; onRetry: () => void; onDone: () => void }) {
  return (
    <div className="space-y-4">
      <Card className="overflow-hidden">
        <div className="flex flex-wrap items-center gap-3 px-5 py-4">
          <div className="min-w-0 flex-1">
            <h2 className="text-sm font-semibold">Setup result for {plan.project}</h2>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {result.status === "complete"
                ? "Everything was created in your Stripe test account and linked to the project."
                : result.status === "partial"
                  ? "Some objects exist. Running the plan again continues where it stopped, without creating duplicates."
                  : "Nothing was created. Fix the cause and run the plan again."}
            </p>
          </div>
          <StatusPill tone={RESULT_TONE[result.status]}>{RESULT_LABEL[result.status]}</StatusPill>
          <StatusPill tone="info">TEST</StatusPill>
        </div>
        <ol className="border-t">
          {result.steps.map((s) => (
            <StepRow key={s.id} s={s} />
          ))}
        </ol>
        {result.note && <div className="border-t bg-warning-soft px-5 py-3 text-xs text-warning-fg">{result.note}</div>}
        <div className="flex items-center justify-between gap-2 border-t px-5 py-3">
          {result.status !== "complete" ? (
            <Button variant="secondary" onClick={onRetry}>
              Run the plan again
            </Button>
          ) : (
            <span className="text-xs text-subtle-foreground">The ids are saved beside the project and in the audit log.</span>
          )}
          <Button onClick={onDone}>Done</Button>
        </div>
      </Card>
      {result.webhook_secret && <SecretCard secret={result.webhook_secret} />}
    </div>
  )
}

/** The webhook signing secret, shown once. It lives only in this component's props. */
function SecretCard({ secret }: { secret: string }) {
  return (
    <Card className="border-warning/40">
      <div className="flex items-start gap-3 p-5">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-warning/40 bg-warning-soft text-warning-fg">
          <KeyRound className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold">Webhook signing secret</h3>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Shown once. Lucidbench does not store or log it, and cannot show it again. Copy it into your server's environment now; if you lose it, roll the secret in the Stripe Dashboard.
          </p>
          <div className="mt-3 flex items-center gap-2 rounded-md border bg-background/60 px-3 py-2">
            <code data-testid="webhook-secret" className="min-w-0 flex-1 select-all break-all font-mono text-xs">
              {secret}
            </code>
            <CopyButton text={secret} what="Signing secret copied" label="Copy the signing secret" />
          </div>
        </div>
      </div>
    </Card>
  )
}
