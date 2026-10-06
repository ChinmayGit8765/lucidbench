import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent as ReactKeyboardEvent } from "react"
import { toast } from "sonner"
import {
  AppWindow,
  ArrowLeft,
  ArrowRight,
  Camera,
  Hand,
  Maximize2,
  MonitorPlay,
  Play,
  Plus,
  RotateCw,
  Square,
  X,
} from "lucide-react"

import { PageHeader } from "@/components/Shell"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, refreshAll } from "@/lib/api"
import {
  modifiersOf,
  navigateBrowser,
  normalizeUrl,
  sendInput,
  showUrl,
  startBrowser,
  stopBrowser,
  takeScreenshot,
  useBrowser,
  type BrowserFrame,
  type BrowserInput,
  type BrowserStatus,
  type BrowserTab,
} from "@/lib/browser"
import { cn } from "@/lib/utils"

const STATE: Record<BrowserStatus["state"], { label: string; tone: "neutral" | "info" | "success" }> = {
  sleeping: { label: "Sleeping", tone: "neutral" },
  starting: { label: "Starting", tone: "info" },
  running: { label: "Running", tone: "success" },
}

/** The keys that only change modifiers send nothing by themselves. */
const MODIFIER_KEYS = new Set(["Shift", "Control", "Alt", "Meta", "AltGraph", "CapsLock"])

/**
 * The live view: the page streams as JPEG frames over server-sent events and
 * is painted straight into an image, so a frame never re-renders React. With
 * take-over on, pointer, wheel, keyboard and paste go back to the browser.
 */
function Viewport({
  target,
  takeover,
  onError,
}: {
  target: string | undefined
  takeover: boolean
  onError: (msg: string | null) => void
}) {
  const wrap = useRef<HTMLDivElement>(null)
  const img = useRef<HTMLImageElement>(null)
  const size = useRef<{ w: number; h: number }>({ w: 1280, h: 800 })
  const [live, setLive] = useState(false)
  const queue = useRef<Promise<unknown>>(Promise.resolve())
  const moving = useRef(false)

  // The stream follows the tab and stops while the page is hidden.
  useEffect(() => {
    let es: EventSource | null = null
    const open = () => {
      if (es || document.hidden) return
      es = new EventSource(`/api/browser/stream${target ? `?target=${encodeURIComponent(target)}` : ""}`)
      es.addEventListener("frame", (e) => {
        const f = JSON.parse((e as MessageEvent<string>).data) as BrowserFrame
        if (img.current) img.current.src = `data:image/jpeg;base64,${f.jpeg}`
        if (f.width && f.height) size.current = { w: f.width, h: f.height }
        setLive(true)
        onError(null)
      })
      es.addEventListener("error", (e) => {
        const data = (e as MessageEvent<string>).data
        if (data) {
          try {
            onError((JSON.parse(data) as { error: string }).error)
          } catch {
            onError(data)
          }
        }
      })
    }
    const close = () => {
      es?.close()
      es = null
    }
    const visibility = () => (document.hidden ? close() : open())
    document.addEventListener("visibilitychange", visibility)
    setLive(false)
    open()
    return () => {
      document.removeEventListener("visibilitychange", visibility)
      close()
    }
  }, [target, onError])

  const send = useCallback(
    (input: BrowserInput) => {
      queue.current = queue.current.then(() => sendInput(target, input)).catch((e) => onError(errorMessage(e)))
    },
    [target, onError],
  )

  // Page coordinates of a pointer position.
  const at = (e: { clientX: number; clientY: number }) => {
    const r = img.current?.getBoundingClientRect()
    if (!r || !r.width || !r.height) return null
    return {
      x: Math.max(0, Math.min(size.current.w, ((e.clientX - r.left) / r.width) * size.current.w)),
      y: Math.max(0, Math.min(size.current.h, ((e.clientY - r.top) / r.height) * size.current.h)),
    }
  }
  const BUTTON = ["left", "middle", "right"] as const

  // A wheel listener has to be non-passive to stop the page from scrolling.
  useEffect(() => {
    const el = img.current
    if (!el || !takeover) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const p = at(e)
      if (p) send({ type: "scroll", ...p, dx: e.deltaX, dy: e.deltaY })
    }
    el.addEventListener("wheel", onWheel, { passive: false })
    return () => el.removeEventListener("wheel", onWheel)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const onKey = (e: ReactKeyboardEvent) => {
    if (!takeover || MODIFIER_KEYS.has(e.key)) return
    // Ctrl or Cmd with V is a paste, which arrives as its own event.
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "v") return
    e.preventDefault()
    send({ type: "key", key: e.key, code: e.code, modifiers: modifiersOf(e), action: "press" })
  }

  return (
    <div
      ref={wrap}
      tabIndex={takeover ? 0 : -1}
      onKeyDown={onKey}
      onPaste={(e) => {
        if (!takeover) return
        const text = e.clipboardData.getData("text")
        if (text) {
          e.preventDefault()
          send({ type: "text", text })
        }
      }}
      className={cn(
        "relative aspect-[8/5] w-full overflow-hidden rounded-xl border bg-muted/40 outline-none",
        takeover ? "border-warning/60 ring-2 ring-warning/30" : "border-border",
      )}
    >
      <img
        ref={img}
        alt="The agent browser, live"
        draggable={false}
        className={cn("size-full select-none object-contain", !live && "invisible", takeover && "cursor-crosshair")}
        onPointerDown={(e) => {
          if (!takeover) return
          wrap.current?.focus()
          e.currentTarget.setPointerCapture(e.pointerId)
          const p = at(e)
          if (p) send({ type: "down", ...p, button: BUTTON[e.button] ?? "left", modifiers: modifiersOf(e) })
        }}
        onPointerUp={(e) => {
          if (!takeover) return
          const p = at(e)
          if (p) send({ type: "up", ...p, button: BUTTON[e.button] ?? "left", modifiers: modifiersOf(e) })
        }}
        onPointerMove={(e) => {
          // At most one move in flight, so a fast pointer never queues up.
          if (!takeover || moving.current) return
          const p = at(e)
          if (!p) return
          moving.current = true
          setTimeout(() => (moving.current = false), 50)
          send({ type: "move", ...p })
        }}
        onContextMenu={(e) => takeover && e.preventDefault()}
      />
      {!live && (
        <div className="absolute inset-0 flex items-center justify-center text-sm text-muted-foreground">
          <MonitorPlay className="mr-2 size-4" /> Waiting for the first frame…
        </div>
      )}
    </div>
  )
}

function Tabs({
  tabs,
  selected,
  onSelect,
  onClose,
  onNew,
}: {
  tabs: BrowserTab[]
  selected: string | undefined
  onSelect: (id: string) => void
  onClose: (id: string) => void
  onNew: () => void
}) {
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between border-b px-4 py-2.5">
        <h2 className="text-sm font-semibold">Tabs</h2>
        <Button variant="ghost" size="icon-sm" aria-label="New tab" title="Open a new tab" onClick={onNew}>
          <Plus />
        </Button>
      </div>
      {tabs.length === 0 ? (
        <p className="px-4 py-4 text-xs text-muted-foreground">No tab is open. Enter a URL above to open one.</p>
      ) : (
        <ul>
          {tabs.map((t) => (
            <li key={t.id} className={cn("group flex items-center gap-2 border-b px-4 py-2 last:border-b-0", t.id === selected && "bg-accent/50")}>
              <button onClick={() => onSelect(t.id)} className="min-w-0 flex-1 text-left" aria-current={t.id === selected}>
                <div className="truncate text-sm font-medium">{t.title || "Untitled"}</div>
                <div className="truncate font-mono text-2xs text-muted-foreground">{showUrl(t.url)}</div>
              </button>
              <Button variant="ghost" size="icon-sm" aria-label={`Close ${t.title || "tab"}`} onClick={() => onClose(t.id)}>
                <X />
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

function Running({ st, refresh }: { st: BrowserStatus; refresh: () => void }) {
  const [chosen, setChosen] = useState<string | undefined>()
  const tab = st.tabs.find((t) => t.id === chosen) ?? st.tabs[0]
  const [bar, setBar] = useState("")
  const [editing, setEditing] = useState(false)
  const [takeover, setTakeover] = useState(false)
  const [streamError, setStreamError] = useState<string | null>(null)
  const [port, setPort] = useState("")
  const full = useRef<HTMLDivElement>(null)

  // The bar follows the tab's address until the user types in it.
  useEffect(() => {
    if (!editing) setBar(tab ? showUrl(tab.url) : "")
  }, [tab?.url, tab?.id, editing]) // eslint-disable-line react-hooks/exhaustive-deps

  const nav = async (req: Parameters<typeof navigateBrowser>[0]) => {
    try {
      const r = await navigateBrowser({ target: tab?.id, ...req })
      if (r.error_text) toast.warning("The page did not load", { description: r.error_text })
      if (r.target) setChosen(r.target)
      refresh()
    } catch (e) {
      toast.error("Could not open that", { description: errorMessage(e) })
    }
  }
  const go = (e: FormEvent) => {
    e.preventDefault()
    setEditing(false)
    if (bar.trim()) void nav({ url: normalizeUrl(bar), action: "go" })
  }
  const shot = async () => {
    try {
      const s = await takeScreenshot(tab?.id)
      const a = document.createElement("a")
      a.href = `data:image/png;base64,${s.png}`
      a.download = `browser-${new Date().toISOString().replace(/[:.]/g, "-")}.png`
      a.click()
      toast.success("Screenshot saved", { description: `${Math.round(s.bytes / 1024)} KB` })
    } catch (e) {
      toast.error("Could not take a screenshot", { description: errorMessage(e) })
    }
  }
  const nextPort = port.trim().replace(/^:/, "")
  const previewable = /^\d{2,5}$/.test(nextPort)

  return (
    <div ref={full} className="space-y-4">
      <div className="grid gap-4 @4xl:grid-cols-[minmax(0,1fr)_280px]">
        <div className="min-w-0 space-y-3">
          <div className="flex items-center gap-1.5">
            <Button variant="secondary" size="icon" aria-label="Back" title="Back" disabled={!tab} onClick={() => nav({ action: "back" })}>
              <ArrowLeft />
            </Button>
            <Button variant="secondary" size="icon" aria-label="Forward" title="Forward" disabled={!tab} onClick={() => nav({ action: "forward" })}>
              <ArrowRight />
            </Button>
            <Button variant="secondary" size="icon" aria-label="Reload" title="Reload" disabled={!tab} onClick={() => nav({ action: "reload" })}>
              <RotateCw />
            </Button>
            <form onSubmit={go} className="min-w-0 flex-1">
              <input
                value={bar}
                onChange={(e) => {
                  setBar(e.target.value)
                  setEditing(true)
                }}
                onBlur={() => setEditing(false)}
                aria-label="Address"
                placeholder="https://example.com or localhost:5173"
                spellCheck={false}
                className="h-8 w-full rounded-md border bg-background/60 px-3 font-mono text-xs outline-none transition-colors placeholder:text-subtle-foreground hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"
              />
            </form>
            <Button variant="secondary" size="icon" aria-label="Take a screenshot" title="Take a screenshot (PNG)" disabled={!tab} onClick={shot}>
              <Camera />
            </Button>
            <Button
              variant="secondary"
              size="icon"
              aria-label="Fullscreen"
              title="Fullscreen"
              disabled={!tab}
              onClick={() => (document.fullscreenElement ? document.exitFullscreen() : full.current?.requestFullscreen())}
            >
              <Maximize2 />
            </Button>
            <Button
              variant={takeover ? "default" : "secondary"}
              size="sm"
              aria-pressed={takeover}
              disabled={!tab}
              onClick={() => setTakeover((t) => !t)}
              title="Send your mouse and keyboard to the browser"
            >
              <Hand /> Take over
            </Button>
          </div>

          {takeover && (
            <div role="status" className="flex items-center gap-3 rounded-lg border border-warning/40 bg-warning-soft px-3 py-2 text-sm text-warning-fg">
              <Hand className="size-4 shrink-0" />
              <span className="font-medium">You are controlling the browser.</span>
              <span className="hidden text-xs opacity-80 @2xl:inline">Clicks, scrolling and typing in the view go to the page. An agent using it sees what you do.</span>
              <Button variant="secondary" size="sm" className="ml-auto" onClick={() => setTakeover(false)}>
                Give control back
              </Button>
            </div>
          )}

          {tab ? (
            <Viewport target={tab.id} takeover={takeover} onError={setStreamError} />
          ) : (
            <div className="flex aspect-[8/5] w-full items-center justify-center rounded-xl border bg-muted/40 text-sm text-muted-foreground">
              No tab is open. Enter a URL above.
            </div>
          )}
          {streamError && <p className="text-xs text-danger-fg">{streamError}</p>}
        </div>

        <div className="space-y-4">
          <Tabs
            tabs={st.tabs}
            selected={tab?.id}
            onSelect={setChosen}
            onClose={(id) => void nav({ target: id, action: "close" })}
            onNew={() => void nav({ action: "new" })}
          />
          <Card className="p-4">
            <h2 className="text-sm font-semibold">Preview a dev server</h2>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              Opens your own local server in this browser, not in yours. <span className="font-mono">localhost</span> becomes{" "}
              <span className="font-mono">host.docker.internal</span>, which is how the container reaches this machine.
            </p>
            <form
              className="mt-3 flex items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                if (previewable) void nav({ url: `http://localhost:${nextPort}/`, action: "go" })
              }}
            >
              <span className="font-mono text-xs text-muted-foreground">localhost:</span>
              <input
                value={port}
                onChange={(e) => setPort(e.target.value)}
                inputMode="numeric"
                aria-label="Dev server port"
                placeholder="5173"
                className="h-8 w-20 rounded-md border bg-background/60 px-2.5 font-mono text-xs outline-none placeholder:text-subtle-foreground hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"
              />
              <Button type="submit" variant="secondary" size="sm" disabled={!previewable}>
                Open
              </Button>
            </form>
          </Card>
          <p className="px-1 text-xs leading-5 text-subtle-foreground">
            {st.version ?? "Chromium"} in a container with its own empty profile. It sleeps after {st.idle_minutes} minutes without use
            {st.held ? "; a Work session is using it now, so it stays up" : ""}.
          </p>
        </div>
      </div>
    </div>
  )
}

export default function Browser() {
  const [busy, setBusy] = useState(false)
  const [fast, setFast] = useState(false)
  const poll = useBrowser(true, fast)
  const st = poll.data
  const starting = busy || st?.state === "starting"
  // Look often only while it starts.
  useEffect(() => setFast(starting), [starting])

  const act = async (fn: () => Promise<unknown>, failed: string) => {
    setBusy(true)
    try {
      await fn()
      refreshAll()
    } catch (e) {
      toast.error(failed, { description: errorMessage(e) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<AppWindow />}
        title="Live browser"
        description="A browser your agents use, in a container with its own profile. Watch it live, take over when you need to."
        actions={
          <div className="flex items-center gap-2">
            {st && (
              <StatusPill tone={starting ? "info" : STATE[st.state].tone} pulse={starting}>
                {starting && st.state !== "running" ? "Starting" : STATE[st.state].label}
              </StatusPill>
            )}
            {st && st.state !== "sleeping" && (
              <Button variant="secondary" size="sm" disabled={busy} onClick={() => act(stopBrowser, "Could not stop the browser")}>
                <Square /> Stop
              </Button>
            )}
          </div>
        }
      />

      {!st && poll.error && <ErrorState title="Cannot read the browser" message={poll.error.message} onRetry={poll.refresh} />}
      {!st && !poll.error && <Skeleton className="h-[420px] rounded-xl" />}
      {st && st.state === "running" && <Running st={st} refresh={poll.refresh} />}
      {st && st.state !== "running" && (
        <Card>
          <EmptyState
            icon={<AppWindow />}
            title={starting ? "Starting the browser…" : "The browser is asleep"}
            description={
              st.note ??
              `It runs on demand as a Chromium container (${st.image}), pulled the first time you start it, with its own empty profile and nothing from your own browser. It stops by itself after ${st.idle_minutes} idle minutes.`
            }
          >
            <Button disabled={starting} onClick={() => act(startBrowser, "Could not start the browser")}>
              <Play /> {starting ? "Starting…" : "Start the browser"}
            </Button>
          </EmptyState>
        </Card>
      )}
    </div>
  )
}
