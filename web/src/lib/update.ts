import { sendJSON } from "@/lib/api"

/* Mirrors internal/update.Status. */
export interface UpdateStatus {
  enabled: boolean
  current: string
  repo?: string
  latest?: string
  url?: string
  download_url?: string
  notes_excerpt?: string
  prerelease: boolean
  available: boolean
  checked_at?: string
  next_check?: string
  /** The last attempt could not reach GitHub. Never shown as an error. */
  offline?: boolean
  note?: string
}

export const UPDATE_PATH = "/api/update"
/** The daemon asks GitHub at most every six hours; asking it hourly costs nothing. */
export const UPDATE_POLL_MS = 60 * 60 * 1000

/** Asks GitHub now (at most once a minute on the daemon's side). */
export const checkNow = () => sendJSON<UpdateStatus>(`${UPDATE_PATH}/check`, "POST")

const DISMISS_KEY = "lucid.update.dismissed"

export const dismissedVersion = () => {
  try {
    return localStorage.getItem(DISMISS_KEY) ?? ""
  } catch {
    return ""
  }
}

export const dismissVersion = (v: string) => {
  try {
    localStorage.setItem(DISMISS_KEY, v)
  } catch {
    /* private mode: the banner comes back next time, which is fine */
  }
}
