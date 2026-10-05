import {
  AppWindow,
  BrainCircuit,
  Clapperboard,
  FlaskConical,
  Gamepad2,
  Globe,
  GraduationCap,
  Library,
  Package,
  PanelsTopLeft,
  Rocket,
  Server,
  Shapes,
  SquareTerminal,
  Wrench,
  Sparkles,
  type LucideIcon,
} from "lucide-react"

import type { Tone } from "@/components/ui/badge"

/* Mirrors internal/projects: keep these in step with the Go structs. */

export type Category = "product" | "portfolio" | "tool" | "experiment" | "coursework"
export type ProjectStatus = "idea" | "active" | "paused" | "frozen" | "shipped" | "archived"
export type Visibility = "public" | "private" | "confidential"
export type NeedStatus = "todo" | "doing" | "done" | "blocked"

export interface Need {
  what: string
  from?: string
  status: NeedStatus
}

export interface Project {
  id: string
  name: string
  category: Category
  type: string
  status: ProjectStatus
  visibility: Visibility
  repo?: string
  linear?: string
  summary?: string
  builds_into: string[]
  needs: Need[]
  built_by: string[]
  needed_by: string[]
  progress: { done: number; total: number }
}

export interface ProjectList {
  configured: boolean
  path_hint: string
  projects: Project[]
  errors: string[]
}

export const PROJECTS_POLL_MS = 30000

export interface CategoryInfo {
  id: Category
  label: string
  /** Singular, for counts of one. */
  one: string
  blurb: string
  /** A blurb short enough for the summary strip. */
  short: string
  icon: LucideIcon
  /** CSS colour for dots, bars and graph nodes. Not a status colour. */
  color: string
}

export const CATEGORIES: CategoryInfo[] = [
  { id: "product", label: "Products", one: "product", blurb: "Shipped to real users", short: "For real users", icon: Rocket, color: "var(--brand)" },
  { id: "portfolio", label: "Portfolio", one: "portfolio piece", blurb: "Finished work that shows what you can do", short: "Shows your work", icon: Sparkles, color: "var(--brand-2)" },
  { id: "tool", label: "Tools", one: "tool", blurb: "Private tools that build other projects", short: "Build the others", icon: Wrench, color: "var(--tint-codex)" },
  { id: "experiment", label: "Experiments", one: "experiment", blurb: "Spikes and research", short: "Spikes, research", icon: FlaskConical, color: "var(--tint-claude)" },
  { id: "coursework", label: "Coursework", one: "coursework project", blurb: "Built under a course's process", short: "Course process", icon: GraduationCap, color: "var(--neutral)" },
]

export const categoryInfo = (c: string): CategoryInfo | undefined => CATEGORIES.find((x) => x.id === c)
export const categoryColor = (c: string) => categoryInfo(c)?.color ?? "var(--neutral)"

export const STATUS: Record<ProjectStatus, { tone: Tone; label: string }> = {
  idea: { tone: "neutral", label: "Idea" },
  active: { tone: "success", label: "Active" },
  paused: { tone: "warning", label: "Paused" },
  frozen: { tone: "info", label: "Frozen" },
  shipped: { tone: "success", label: "Shipped" },
  archived: { tone: "neutral", label: "Archived" },
}
export const STATUSES = Object.keys(STATUS) as ProjectStatus[]

export const NEED: Record<NeedStatus, { tone: Tone; label: string; color: string }> = {
  todo: { tone: "neutral", label: "To do", color: "var(--neutral)" },
  doing: { tone: "info", label: "Doing", color: "var(--info)" },
  done: { tone: "success", label: "Done", color: "var(--success)" },
  blocked: { tone: "warning", label: "Blocked", color: "var(--warning)" },
}

export const TYPES: Record<string, { label: string; icon: LucideIcon }> = {
  game: { label: "Game", icon: Gamepad2 },
  "web-app": { label: "Web app", icon: Globe },
  "desktop-app": { label: "Desktop app", icon: AppWindow },
  cli: { label: "CLI", icon: SquareTerminal },
  library: { label: "Library", icon: Library },
  service: { label: "Service", icon: Server },
  "ml-research": { label: "ML research", icon: BrainCircuit },
  site: { label: "Site", icon: PanelsTopLeft },
  "video-system": { label: "Video system", icon: Clapperboard },
}
export const typeInfo = (t: string) => TYPES[t] ?? { label: t || "Project", icon: t ? Package : Shapes }

/** Every blocked need, with the project that has it. */
export function blockedNeeds(ps: Project[]): { project: Project; need: Need }[] {
  return ps.flatMap((p) => p.needs.filter((n) => n.status === "blocked").map((need) => ({ project: p, need })))
}

/** "1 product", "3 tools". */
export function countLabel(c: CategoryInfo, n: number): string {
  return `${n} ${n === 1 ? c.one : c.label.toLowerCase()}`
}

/** github.com URL for an owner/name repo; anything else is returned as is. */
export function repoURL(repo: string): string {
  if (/^https?:\/\//.test(repo)) return repo
  return `https://github.com/${repo}`
}
