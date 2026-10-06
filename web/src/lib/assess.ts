import { useEffect, useState } from "react"

import type { Tone } from "@/components/ui/badge"
import { getJSON, sendJSON } from "@/lib/api"

/* Mirrors internal/assess and internal/projects/assessment*.go. */

export type QuestionType = "choice" | "text" | "bool"

export interface Question {
  id: string
  text: string
  type: QuestionType
  options?: string[]
}

export interface Kind {
  id: string
  name: string
  blurb: string
  types?: string[]
  questions: Question[]
}

export type RiskLevel = "low" | "medium" | "high"

export interface Assessment {
  kind: string
  answers: Record<string, string>
  assessed_at: string
}

export interface Suggestion {
  done_criteria: string[]
  check_commands: string[]
  risk: { level: RiskLevel; reasons: string[] }
}

export interface AssessmentView {
  project: string
  assessment: Assessment | null
  source?: "app" | "projects.yaml"
  suggestion?: Suggestion
  suggested_kind?: string
  can_suggest: boolean
}

export const RISK: Record<RiskLevel, { label: string; tone: Tone }> = {
  low: { label: "Low risk", tone: "success" },
  medium: { label: "Medium risk", tone: "warning" },
  high: { label: "High risk", tone: "danger" },
}

export const assessmentPath = (id: string) => `/api/projects/${encodeURIComponent(id)}/assessment`

export const assessApi = {
  kinds: () => getJSON<Kind[]>("/api/assess/kinds"),
  get: (id: string) => getJSON<AssessmentView>(assessmentPath(id)),
  confirm: (id: string, kind: string, answers: Record<string, string>) =>
    sendJSON<AssessmentView>(assessmentPath(id), "PUT", { kind, answers }),
  preview: (id: string, kind: string, answers: Record<string, string>) =>
    sendJSON<Suggestion>(`${assessmentPath(id)}/preview`, "POST", { kind, answers }),
  suggest: (id: string, kind: string) =>
    sendJSON<{ kind: string; answers: Record<string, string>; provider: string }>(`${assessmentPath(id)}/suggest`, "POST", { kind }),
}

/** The kinds, loaded once. */
export function useKinds(): Kind[] {
  const [kinds, setKinds] = useState<Kind[]>([])
  useEffect(() => {
    let live = true
    assessApi.kinds().then(
      (k) => live && setKinds(k),
      () => {},
    )
    return () => {
      live = false
    }
  }, [])
  return kinds
}

/** Shows an option id such as "vertical-slice" as "Vertical slice". */
export function optionLabel(o: string): string {
  const s = o.replace(/-/g, " ")
  return s.charAt(0).toUpperCase() + s.slice(1)
}
