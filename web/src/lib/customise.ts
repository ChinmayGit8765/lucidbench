import type { Project } from "@/lib/projects"
import { putHandoff } from "@/lib/prompts"

/** The context line a feature request opens with in the Council. */
export const FEATURE_CONTEXT =
  "This is a feature request for Lucidbench itself; the work happens on the Lucidbench repo or the user's fork."

/** The project that is Lucidbench in the user's projects.yaml, if any. */
export function lucidbenchProject(ps: Project[]): Project | undefined {
  return ps.find((p) => p.id === "lucidbench") ?? ps.find((p) => /lucidbench/i.test(p.name) || /lucidbench/i.test(p.repo ?? ""))
}

/**
 * "Describe a feature": never code. The description becomes a Council
 * braindump with the feature-request context, on the Lucidbench project when
 * the user has one (and it is not confidential), and the loop takes over.
 */
export function describeFeature(text: string, projects: Project[], open: (id: string, sub?: string[]) => void) {
  const p = lucidbenchProject(projects)
  const body = `${FEATURE_CONTEXT}\n\nFeature: ${text.trim()}`
  putHandoff({ to: "council", text: body, project: p && p.visibility !== "confidential" ? p.id : undefined, from: "palette" })
  open("council")
}
