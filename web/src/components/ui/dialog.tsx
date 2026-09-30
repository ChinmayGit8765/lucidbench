import * as React from "react"
import { X } from "lucide-react"

import { cn } from "@/lib/utils"

function useEscape(open: boolean, onClose: () => void) {
  React.useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose()
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, onClose])
}

function CloseButton({ onClose }: { onClose: () => void }) {
  return (
    <button
      onClick={onClose}
      aria-label="Close"
      className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
    >
      <X className="size-4" />
    </button>
  )
}

interface DialogProps {
  open: boolean
  onClose: () => void
  title: string
  description?: string
  children: React.ReactNode
}

/** Minimal modal: backdrop click and Escape close it. */
function Dialog({ open, onClose, title, description, children }: DialogProps) {
  useEscape(open, onClose)
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/50 backdrop-blur-[2px] animate-in fade-in-0" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="relative w-full max-w-md rounded-xl border bg-elevated p-5 shadow-pop animate-in fade-in-0 zoom-in-95 slide-in-from-bottom-2 duration-200"
      >
        <div className="absolute right-3 top-3">
          <CloseButton onClose={onClose} />
        </div>
        <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
        {description && <p className="mt-1 pr-6 text-sm text-muted-foreground">{description}</p>}
        <div className="mt-5">{children}</div>
      </div>
    </div>
  )
}

interface SheetProps {
  open: boolean
  onClose: () => void
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  children: React.ReactNode
  className?: string
}

/** A slide-over panel from the right edge. */
function Sheet({ open, onClose, title, description, actions, children, className }: SheetProps) {
  useEscape(open, onClose)
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50">
      <div className="absolute inset-0 bg-black/40 animate-in fade-in-0" onClick={onClose} />
      <aside
        role="dialog"
        aria-modal="true"
        className={cn(
          "absolute inset-y-0 right-0 flex w-full max-w-2xl flex-col border-l bg-elevated shadow-pop animate-in slide-in-from-right duration-300 ease-[var(--ease-out-soft)]",
          className,
        )}
      >
        <header className="flex items-start gap-3 border-b px-5 py-4">
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-base font-semibold tracking-tight">{title}</h2>
            {description && <div className="mt-0.5 text-xs text-muted-foreground">{description}</div>}
          </div>
          <div className="flex items-center gap-1">
            {actions}
            <CloseButton onClose={onClose} />
          </div>
        </header>
        <div className="min-h-0 flex-1 overflow-auto">{children}</div>
      </aside>
    </div>
  )
}

export { Dialog, Sheet }
