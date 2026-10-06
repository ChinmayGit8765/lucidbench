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

/**
 * Excalidraw, lazy-loaded as its own chunk. The parent keeps the handle and
 * asks for the JSON or a PNG when the user saves or exports; onDirty fires
 * on the first edit after load, so the parent can warn about unsaved work.
 */
export default function ExcalidrawCanvas({ json, onDirty, handle }: { json: string; onDirty: () => void; handle: Ref<CanvasHandle> }) {
  const api = useRef<ExcalidrawImperativeAPI | null>(null)
  const loaded = useRef(false)
  const seed = useMemo(() => initial(json), [json])
  const dark = document.documentElement.classList.contains("dark")

  useEffect(() => {
    loaded.current = false
  }, [json])

  useImperativeHandle(
    handle,
    () => ({
      serialize: () => {
        const a = api.current
        if (!a) return json
        return serializeAsJSON(a.getSceneElements(), a.getAppState(), a.getFiles(), "local")
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

  const onChange = useCallback(() => {
    // The first onChange after mounting is the load itself.
    if (!loaded.current) {
      loaded.current = true
      return
    }
    onDirty()
  }, [onDirty])

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
