import { createContext, useContext } from "react"

import type { Health } from "@/lib/health"

/** App-wide navigation and shared state, available to every module page. */
export interface AppValue {
  health: Health | null | undefined
  /** Goes to a URL path inside the app. */
  navigate: (path: string) => void
  /**
   * Opens a module by id. A roadmap module only says it is coming; an
   * extension that is not in the sidebar opens its card in Settings ›
   * Extensions instead, so no link is ever dead.
   */
  open: (id: string, subpath?: string[]) => void
  /** Opens the Projects page scrolled to one project. */
  openProject: (id: string) => void
  /** The project the Projects page should highlight. */
  focus: { id: string; n: number } | null
  addAccount: () => void
  openPalette: () => void
}

export const AppContext = createContext<AppValue | null>(null)

export function useApp(): AppValue {
  const v = useContext(AppContext)
  if (!v) throw new Error("useApp outside AppContext")
  return v
}
