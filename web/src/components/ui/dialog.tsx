import * as React from "react"
import { X } from "lucide-react"

import { cn } from "@/lib/utils"

interface DialogProps {
  open: boolean
  onClose: () => void
  title: string
  description?: string
  children: React.ReactNode
}

/** Minimal modal: backdrop click and Escape close it. */
function Dialog({ open, onClose, title, description, children }: DialogProps) {
  React.useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose()
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={cn("relative w-full max-w-md rounded-xl border bg-card p-6 shadow-lg")}
      >
        <button
          onClick={onClose}
          aria-label="Close"
          className="absolute right-4 top-4 rounded-md p-1 text-muted-foreground hover:bg-accent"
        >
          <X className="size-4" />
        </button>
        <h2 className="font-semibold leading-none tracking-tight">{title}</h2>
        {description && <p className="mt-1.5 text-sm text-muted-foreground">{description}</p>}
        <div className="mt-5">{children}</div>
      </div>
    </div>
  )
}

export { Dialog }
