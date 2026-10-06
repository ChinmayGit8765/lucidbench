import { useEffect, useState } from "react"
import { ArrowLeft, ArrowRight, Check, Sparkles } from "lucide-react"
import { toast } from "sonner"

import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Sheet } from "@/components/ui/dialog"
import { errorMessage } from "@/lib/api"
import { assessApi, optionLabel, RISK, type AssessmentView, type Kind, type Question, type Suggestion } from "@/lib/assess"
import type { Project } from "@/lib/projects"
import { cn } from "@/lib/utils"

const field =
  "h-9 w-full rounded-md border bg-background/60 px-2.5 text-sm outline-none transition-colors placeholder:text-subtle-foreground hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"

/** Which step: choosing the kind, question i, or the summary. */
type Step = { at: "kind" } | { at: "question"; i: number } | { at: "summary" }

function Choice({ label, hint, on, onClick }: { label: string; hint?: string; on: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={on}
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-3 rounded-lg border px-3.5 py-2.5 text-left text-sm transition-colors hover:border-border-strong",
        on ? "border-brand/60 bg-brand-soft text-foreground" : "bg-background/40",
      )}
    >
      <span className={cn("grid size-4 shrink-0 place-items-center rounded-full border", on ? "border-brand bg-brand text-white" : "border-border-strong")}>
        {on && <Check className="size-3" />}
      </span>
      <span className="min-w-0 flex-1">
        <span className="font-medium">{label}</span>
        {hint && <span className="block text-xs text-muted-foreground">{hint}</span>}
      </span>
    </button>
  )
}

function Progress({ value, max }: { value: number; max: number }) {
  return (
    <div
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={value}
      aria-label="Assessment progress"
      className="h-1.5 overflow-hidden rounded-full bg-muted"
    >
      <div className="h-full rounded-full bg-brand transition-[width] duration-300" style={{ width: `${max ? (value / max) * 100 : 0}%` }} />
    </div>
  )
}

function AnswerInput({ q, value, suggested, onChange }: { q: Question; value: string; suggested: boolean; onChange: (v: string) => void }) {
  return (
    <div className="space-y-2" role={q.type === "text" ? undefined : "radiogroup"} aria-label={q.text}>
      {q.type === "choice" && q.options?.map((o) => <Choice key={o} label={optionLabel(o)} on={value === o} onClick={() => onChange(o)} />)}
      {q.type === "bool" && (
        <>
          <Choice label="Yes" on={value === "true"} onClick={() => onChange("true")} />
          <Choice label="No" on={value === "false"} onClick={() => onChange("false")} />
        </>
      )}
      {q.type === "text" && (
        <input
          className={field}
          value={value}
          maxLength={500}
          placeholder="Optional. A short answer is fine."
          onChange={(e) => onChange(e.target.value)}
          aria-label={q.text}
        />
      )}
      {suggested && value && (
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Sparkles className="size-3.5 text-brand" /> Suggested from the project's files. Change it if it is wrong.
        </p>
      )}
    </div>
  )
}

function Summary({ s, kind }: { s: Suggestion; kind: Kind }) {
  const risk = RISK[s.risk.level]
  return (
    <div className="space-y-5">
      <section className="space-y-2">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-semibold">Risk</h3>
          <StatusPill tone={risk.tone}>{risk.label}</StatusPill>
        </div>
        {s.risk.reasons.length > 0 ? (
          <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
            {s.risk.reasons.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">Nothing in your answers raises the risk.</p>
        )}
      </section>
      <section className="space-y-2">
        <h3 className="text-sm font-semibold">Suggested done criteria</h3>
        <p className="text-xs text-muted-foreground">Council offers these when you brief a {kind.name.toLowerCase()} project.</p>
        <ul className="space-y-1.5">
          {s.done_criteria.map((c) => (
            <li key={c} className="rounded-md border bg-background/40 px-3 py-2 text-sm">
              {c}
            </li>
          ))}
        </ul>
      </section>
      <section className="space-y-2">
        <h3 className="text-sm font-semibold">Checks Work may run</h3>
        {s.check_commands.length > 0 ? (
          <div className="flex flex-wrap gap-1.5">
            {s.check_commands.map((c) => (
              <code key={c} className="rounded-md border bg-muted/60 px-1.5 py-0.5 font-mono text-xs">
                {c}
              </code>
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">None suggested. Add the command that checks this project to its Work settings.</p>
        )}
      </section>
    </div>
  )
}

/**
 * The assessment flow in a slide-over: pick the kind, answer one question per
 * step, read the summary, confirm. Nothing is saved until the last step.
 */
export function AssessSheet({
  project,
  kinds,
  onClose,
  onSaved,
}: {
  project: Project | null
  kinds: Kind[]
  onClose: () => void
  onSaved: () => void
}) {
  return (
    <Sheet
      open={project !== null}
      onClose={onClose}
      className="max-w-lg"
      title={project ? `Assess ${project.name}` : ""}
      description="A few quick questions to help manage how it gets built."
    >
      {project && <Flow key={project.id} project={project} kinds={kinds} onClose={onClose} onSaved={onSaved} />}
    </Sheet>
  )
}

function Flow({ project, kinds, onClose, onSaved }: { project: Project; kinds: Kind[]; onClose: () => void; onSaved: () => void }) {
  const [view, setView] = useState<AssessmentView | null>(null)
  const [kindId, setKindId] = useState("")
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [guessed, setGuessed] = useState<Set<string>>(new Set())
  const [step, setStep] = useState<Step>({ at: "kind" })
  const [busy, setBusy] = useState<"" | "suggest" | "save" | "review">("")
  const [preview, setPreview] = useState<Suggestion | null>(null)

  useEffect(() => {
    let live = true
    assessApi.get(project.id).then(
      (v) => {
        if (!live) return
        setView(v)
        setKindId(v.assessment?.kind ?? v.suggested_kind ?? "")
        setAnswers(v.assessment?.answers ?? {})
      },
      (e) => live && toast.error(errorMessage(e)),
    )
    return () => {
      live = false
    }
  }, [project.id])

  const kind = kinds.find((k) => k.id === kindId)
  const qs = kind?.questions ?? []
  const set = (id: string, v: string) => {
    setAnswers((a) => ({ ...a, [id]: v }))
    setGuessed((g) => {
      const n = new Set(g)
      n.delete(id)
      return n
    })
  }
  const pickKind = (id: string) => {
    if (id !== kindId) {
      setKindId(id)
      setAnswers({})
      setGuessed(new Set())
    }
  }

  const review = async () => {
    if (!kind) return
    setBusy("review")
    try {
      setPreview(await assessApi.preview(project.id, kind.id, answers))
      setStep({ at: "summary" })
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy("")
    }
  }

  const answered = (q: Question) => q.type === "text" || !!answers[q.id]
  const pos = step.at === "kind" ? 0 : step.at === "question" ? step.i + 1 : qs.length + 1
  const total = qs.length + 1 // the questions, then the review

  const suggest = async () => {
    if (!kind) return
    setBusy("suggest")
    try {
      const r = await assessApi.suggest(project.id, kind.id)
      const got = Object.entries(r.answers).filter(([id]) => !answers[id])
      setAnswers((a) => ({ ...r.answers, ...a }))
      setGuessed(new Set(got.map(([id]) => id)))
      toast.success(got.length ? `${r.provider} suggested ${got.length} ${got.length === 1 ? "answer" : "answers"}. Check each one.` : `${r.provider} could not tell from the files.`)
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy("")
    }
  }

  const save = async () => {
    if (!kind) return
    setBusy("save")
    try {
      await assessApi.confirm(project.id, kind.id, answers)
      toast.success(`${project.name} assessed as ${kind.name}`)
      onSaved()
      onClose()
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy("")
    }
  }

  return (
    <div className="flex min-h-full flex-col p-5">
      <div className="mb-5 space-y-1.5">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>{step.at === "summary" ? "Review" : step.at === "kind" ? "Kind" : `Question ${step.i + 1} of ${qs.length}`}</span>
        </div>
        <Progress value={pos} max={total} />
      </div>

      <div className="flex-1 space-y-4">
        {!view && <p className="text-sm text-muted-foreground">Loading…</p>}

        {view && step.at === "kind" && (
          <>
            <h3 className="text-base font-semibold">What kind of project is {project.name}?</h3>
            {view.assessment && <p className="text-xs text-muted-foreground">Assessed before. Your earlier answers are kept if you keep the kind.</p>}
            <div className="space-y-2" role="radiogroup" aria-label="Kind of project">
              {kinds.map((k) => (
                <Choice key={k.id} label={k.name} hint={k.blurb} on={k.id === kindId} onClick={() => pickKind(k.id)} />
              ))}
            </div>
          </>
        )}

        {view && step.at === "question" && qs[step.i] && (
          <>
            <h3 className="text-base font-semibold">{qs[step.i].text}</h3>
            <AnswerInput q={qs[step.i]} value={answers[qs[step.i].id] ?? ""} suggested={guessed.has(qs[step.i].id)} onChange={(v) => set(qs[step.i].id, v)} />
          </>
        )}

        {view && step.at === "summary" && kind && (
          <>
            <h3 className="text-base font-semibold">Your answers</h3>
            <dl className="divide-y rounded-lg border text-sm">
              {qs.map((q, i) => (
                <div key={q.id} className="flex items-start gap-3 px-3 py-2">
                  <dt className="min-w-0 flex-1 text-muted-foreground">{q.text}</dt>
                  <dd className="shrink-0 text-right font-medium">
                    {q.type === "bool" ? (answers[q.id] === "true" ? "Yes" : answers[q.id] === "false" ? "No" : "—") : q.type === "choice" ? (answers[q.id] ? optionLabel(answers[q.id]) : "—") : answers[q.id] || "—"}
                  </dd>
                  <button type="button" className="text-xs text-brand-fg hover:underline" onClick={() => setStep({ at: "question", i })}>
                    Edit
                  </button>
                </div>
              ))}
            </dl>
            {preview && <Summary s={preview} kind={kind} />}
            <p className="text-xs text-muted-foreground">Saving keeps this beside your projects file. The file itself is not changed.</p>
          </>
        )}
      </div>

      <div className="sticky bottom-0 -mx-5 -mb-5 mt-6 flex items-center gap-2 border-t bg-elevated px-5 py-4">
        {step.at !== "kind" && (
          <Button variant="ghost" onClick={() => setStep(step.at === "summary" ? { at: "question", i: qs.length - 1 } : step.i === 0 ? { at: "kind" } : { at: "question", i: step.i - 1 })}>
            <ArrowLeft /> Back
          </Button>
        )}
        {kind && step.at !== "summary" && view && (
          <Button
            variant="outline"
            size="sm"
            disabled={!view.can_suggest || busy !== ""}
            title={
              project.visibility === "confidential"
                ? "Confidential projects are never sent to an AI provider"
                : !view.can_suggest
                  ? "Needs a local_path in your projects file"
                  : "Your own CLI reads the file names (not their contents) and guesses"
            }
            onClick={suggest}
          >
            <Sparkles /> {busy === "suggest" ? "Reading the repo…" : "Suggest answers from the repo"}
          </Button>
        )}
        <div className="ml-auto flex gap-2">
          {step.at === "kind" && (
            <Button disabled={!kind} onClick={() => setStep({ at: "question", i: 0 })}>
              Next <ArrowRight />
            </Button>
          )}
          {step.at === "question" && (
            <Button disabled={!answered(qs[step.i]) || busy !== ""} onClick={() => (step.i + 1 < qs.length ? setStep({ at: "question", i: step.i + 1 }) : review())}>
              {step.i + 1 < qs.length ? "Next" : "Review"} <ArrowRight />
            </Button>
          )}
          {step.at === "summary" && (
            <Button disabled={busy !== "" || !qs.every(answered)} onClick={save}>
              <Check /> {busy === "save" ? "Saving…" : "Save assessment"}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
