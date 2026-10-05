import { useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { Copy, Radio, RefreshCw } from "lucide-react"

import { copyText } from "@/components/CopyCommand"
import { Button } from "@/components/ui/button"
import { Sheet } from "@/components/ui/dialog"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, request } from "@/lib/api"
import { cn } from "@/lib/utils"

const MAX_LINES = 5000

// Terminal colour and cursor codes that some programs write to their logs.
const ANSI = new RegExp(String.fromCharCode(27) + "\\[[0-9;?]*[A-Za-z]", "g")
const clean = (l: string) => l.replace(ANSI, "")

/**
 * A log viewer in a slide-over: the last lines of a container or pod, with
 * Follow streaming new lines over server-sent events (the URL plus
 * &follow=1). Only one stream is open at a time and it closes with the sheet.
 */
export function LogDrawer({
  url,
  title,
  description,
  onClose,
}: {
  /** Tail URL, e.g. /api/docker/containers/x/logs?tail=200; null closes the drawer. */
  url: string | null
  title: ReactNode
  description?: ReactNode
  onClose: () => void
}) {
  const [lines, setLines] = useState<string[] | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [follow, setFollow] = useState(false)
  const box = useRef<HTMLPreElement>(null)

  const load = useCallback(async () => {
    if (!url) return
    setErr(null)
    setBusy(true)
    try {
      const text = await (await request(url)).text()
      setLines(text.trim() === "" ? [] : text.replace(/\n$/, "").split("\n").slice(-MAX_LINES).map(clean))
    } catch (e) {
      setLines(null)
      setErr(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }, [url])

  useEffect(() => {
    setLines(null)
    setFollow(false)
    void load()
  }, [load])

  useEffect(() => {
    if (!url || !follow) return
    setLines([])
    const es = new EventSource(`${url}&follow=1`)
    es.onmessage = (e) => setLines((ls) => [...(ls ?? []), clean(e.data as string)].slice(-MAX_LINES))
    es.addEventListener("end", () => {
      es.close()
      setFollow(false)
    })
    es.onerror = () => {
      es.close()
      setFollow(false)
    }
    return () => es.close()
  }, [url, follow])

  useEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight
  }, [lines, follow])

  const text = lines?.join("\n") ?? ""
  return (
    <Sheet
      open={url !== null}
      onClose={onClose}
      title={title}
      description={description}
      className="max-w-3xl"
      actions={
        <>
          <Button
            variant={follow ? "secondary" : "ghost"}
            size="sm"
            aria-pressed={follow}
            onClick={() => setFollow((f) => !f)}
            title="Stream new lines as they are written"
          >
            <Radio className={cn(follow && "text-success")} /> {follow ? "Following" : "Follow"}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => void load()} disabled={busy || follow}>
            <RefreshCw className={cn(busy && "animate-spin")} /> Reload
          </Button>
          <Button variant="ghost" size="sm" disabled={!text} onClick={() => void copyText(text, "Logs copied")}>
            <Copy /> Copy
          </Button>
        </>
      }
    >
      <div className="flex h-full flex-col p-4">
        {err ? (
          <ErrorState title="Could not load logs" message={err} onRetry={() => void load()} />
        ) : lines === null ? (
          <div className="space-y-2 p-2">
            <Skeleton className="h-3.5 w-3/4" />
            <Skeleton className="h-3.5 w-1/2" />
            <Skeleton className="h-3.5 w-2/3" />
          </div>
        ) : lines.length === 0 ? (
          <p className="p-6 text-center text-sm text-muted-foreground">
            {follow ? "Waiting for new lines…" : "No output yet."}
          </p>
        ) : (
          <pre ref={box} className="min-h-0 flex-1 overflow-auto rounded-lg border bg-background py-3 font-mono text-xs leading-5">
            {lines.map((l, i) => (
              <div key={i} className="flex hover:bg-accent/40">
                <span className="w-12 shrink-0 select-none pr-3 text-right tabular-nums text-subtle-foreground/70">{i + 1}</span>
                <span className="whitespace-pre-wrap break-all pr-4">{l || " "}</span>
              </div>
            ))}
          </pre>
        )}
      </div>
    </Sheet>
  )
}
