import "@excalidraw/excalidraw/index.css"

import { useCallback, useEffect, useImperativeHandle, useMemo, useRef, type Ref } from "react"
import { Excalidraw, exportToBlob, serializeAsJSON } from "@excalidraw/excalidraw"
import type { ExcalidrawImperativeAPI, ExcalidrawInitialDataState } from "@excalidraw/excalidraw/types"

// Drawing fonts are shipped with the app (see vite.config.ts), so the canvas
// makes no request to a CDN.
;(window as unknown as { EXCALIDRAW_ASSET_PATH?: string }).EXCALIDRAW_ASSET_PATH = "/excalidraw/"

export interface CanvasHandle {
  /** The canvas as Excalidraw's own JSON. */
  serialize: () => string
  /** The canvas as a PNG. */
  png: () => Promise<Blob>
  /** Treats the canvas as it is now as the saved version. */
  markSaved: () => void
}

/** The saved JSON, or an empty canvas when the text is not one. */
function initial(json: string): ExcalidrawInitialDataState | null {
  try {
    const d = JSON.parse(json) as { elements?: unknown[]; appState?: Record<string, unknown>; files?: Record<string, unknown> }
    if (!Array.isArray(d.elements)) return null
    // Collaborators are a Map in memory and do not survive JSON.
    const { collaborators: _c, ...appState } = (d.appState ?? {}) as Record<string, unknown>
    return { elements: d.elements, appState, files: d.files ?? {}, scrollToContent: true } as ExcalidrawInitialDataState
  } catch {
    return null
  }
}

const signature = (elements: readonly { id: string; version: number }[]) => elements.map((e) => `${e.id}:${e.version}`).join(",")

/**
 * Excalidraw, lazy-loaded as its own chunk. The parent keeps the handle and
 * asks for the JSON or a PNG when the user saves or exports; onDirty fires
 * on the first edit after load, so the parent can warn about unsaved work.
 */
export default function ExcalidrawCanvas({ json, onDirty, handle }: { json: string; onDirty: () => void; handle: Ref<CanvasHandle> }) {
  const api = useRef<ExcalidrawImperativeAPI | null>(null)
  // The scene as loaded or last saved. Excalidraw calls onChange for scrolling
  // and selecting too, so only a change to an element counts as an edit.
  const base = useRef<string | null>(null)
  const seed = useMemo(() => initial(json), [json])
  const dark = document.documentElement.classList.contains("dark")

  useEffect(() => {
    base.current = null
  }, [json])

  useImperativeHandle(
    handle,
    () => ({
      serialize: () => {
        const a = api.current
        if (!a) return json
        return serializeAsJSON(a.getSceneElements(), a.getAppState(), a.getFiles(), "local")
      },
      markSaved: () => {
        if (api.current) base.current = signature(api.current.getSceneElements())
      },
      png: async () => {
        const a = api.current
        if (!a) throw new Error("The canvas is not ready yet")
        return exportToBlob({
          elements: a.getSceneElements(),
          appState: { ...a.getAppState(), exportBackground: true },
          files: a.getFiles(),
          mimeType: "image/png",
        })
      },
    }),
    [json],
  )

  const onChange = useCallback(
    (elements: readonly { id: string; version: number }[]) => {
      const sig = signature(elements)
      // The first onChange after mounting is the load itself.
      if (base.current === null) base.current = sig
      else if (sig !== base.current) onDirty()
    },
    [onDirty],
  )

  return (
    <div className="h-[34rem] overflow-hidden rounded-lg border" data-testid="excalidraw">
      <Excalidraw
        excalidrawAPI={(a) => {
          api.current = a
        }}
        initialData={seed ?? undefined}
        onChange={onChange}
        theme={dark ? "dark" : "light"}
        UIOptions={{ canvasActions: { loadScene: false, saveToActiveFile: false, export: false } }}
      />
    </div>
  )
}
