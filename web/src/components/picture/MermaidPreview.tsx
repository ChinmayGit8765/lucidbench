import { useEffect, useRef, useState } from "react"

import { Skeleton } from "@/components/ui/states"

const isDark = () => document.documentElement.classList.contains("dark")

let counter = 0

/**
 * Renders Mermaid source as an SVG. The mermaid package is a lazy chunk,
 * loaded on the first preview, and runs in its strict security mode (no
 * script or click handlers from the source). A syntax error is shown instead
 * of the diagram; the last good drawing is not kept, so the preview is never
 * out of step with the text.
 */
export function MermaidPreview({ source, className, actualSize }: { source: string; className?: string; actualSize?: boolean }) {
  const [svg, setSvg] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const latest = useRef(0)

  useEffect(() => {
    const run = ++latest.current
    const t = window.setTimeout(async () => {
      if (!source.trim()) {
        setSvg(null)
        setError(null)
        setLoading(false)
        return
      }
      try {
        const { default: mermaid } = await import("mermaid")
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          suppressErrorRendering: true,
          theme: isDark() ? "dark" : "default",
          flowchart: { htmlLabels: false },
        })
        await mermaid.parse(source)
        const out = await mermaid.render(`lb-mermaid-${++counter}`, source)
        if (run !== latest.current) return
        setSvg(out.svg)
        setError(null)
      } catch (e) {
        if (run !== latest.current) return
        setSvg(null)
        setError(e instanceof Error ? e.message : String(e))
      } finally {
        // mermaid leaves a scratch node behind when a render throws.
        document.querySelectorAll('[id^="dlb-mermaid-"]').forEach((n) => n.remove())
        if (run === latest.current) setLoading(false)
      }
    }, 300)
    return () => window.clearTimeout(t)
  }, [source])

  if (error)
    return (
      <div role="alert" className={className}>
        <p className="text-sm font-medium text-danger-fg">This does not parse as Mermaid</p>
        <pre className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap rounded-md border border-danger/30 bg-danger-soft p-3 font-mono text-xs text-danger-fg">{error}</pre>
      </div>
    )
  if (loading && !svg) return <Skeleton className="h-40 w-full" />
  if (!svg) return <p className="text-sm text-muted-foreground">Nothing to draw yet.</p>
  // The SVG is mermaid's own output from strict mode, never the raw source.
  // Actual size pins the drawing to its natural width, so a wide diagram
  // scrolls instead of shrinking past reading.
  const natural = actualSize ? Number(/viewBox="[-\d.]+ [-\d.]+ ([\d.]+) /.exec(svg)?.[1]) : NaN
  return (
    <div
      data-testid="mermaid-svg"
      className={className}
      style={Number.isFinite(natural) ? { width: `${Math.ceil(natural)}px`, maxWidth: "none" } : undefined}
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}
