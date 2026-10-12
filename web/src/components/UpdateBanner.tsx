import { useState } from "react"
import { ArrowUpCircle, X } from "lucide-react"

import { useAfterSettle, usePoll } from "@/lib/api"
import { dismissedVersion, dismissVersion, UPDATE_PATH, UPDATE_POLL_MS, type UpdateStatus } from "@/lib/update"

/**
 * A quiet line under the header when a newer Lucidbench is out. It links to
 * the release notes and the download; nothing is downloaded or installed.
 * A failed check shows nothing at all: being offline is not news.
 */
export function UpdateBanner() {
  // A background check (the daemon asks GitHub at most every six hours): after the page.
  const poll = usePoll<UpdateStatus>(useAfterSettle() ? UPDATE_PATH : null, UPDATE_POLL_MS)
  const [hidden, setHidden] = useState(dismissedVersion)
  const u = poll.data
  if (!u?.available || !u.latest || hidden === u.latest) return null
  return (
    <div
      role="status"
      data-testid="update-banner"
      className="flex items-center gap-3 border-b bg-brand-soft/60 px-5 py-2 text-sm animate-in fade-in-0 duration-300 md:px-8"
    >
      <ArrowUpCircle className="size-4 shrink-0 text-brand" />
      <span className="min-w-0 flex-1 truncate" title={u.notes_excerpt}>
        Lucidbench {u.latest} is out{u.prerelease ? " (beta)" : ""}. You have {u.current}.
      </span>
      {u.url && (
        <a href={u.url} target="_blank" rel="noreferrer" className="shrink-0 text-xs font-medium text-brand hover:underline">
          Release notes
        </a>
      )}
      {u.download_url && (
        <a href={u.download_url} target="_blank" rel="noreferrer" className="shrink-0 text-xs font-medium text-brand hover:underline">
          Download
        </a>
      )}
      <button
        aria-label="Hide until the next version"
        title="Hide until the next version"
        className="shrink-0 rounded p-0.5 text-subtle-foreground hover:bg-accent hover:text-foreground"
        onClick={() => {
          dismissVersion(u.latest!)
          setHidden(u.latest!)
        }}
      >
        <X className="size-3.5" />
      </button>
    </div>
  )
}
