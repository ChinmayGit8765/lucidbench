import * as React from "react"

import { Button } from "@/components/ui/button"
import { Dialog } from "@/components/ui/dialog"

export interface ConfirmRequest {
  title: string
  description: string
  /** Extra content between the description and the buttons. */
  body?: React.ReactNode
  confirmLabel: string
  danger?: boolean
  run: () => Promise<unknown>
}

/** A confirm dialog driven by a request object; null keeps it closed. */
export function ConfirmDialog({ request, onClose }: { request: ConfirmRequest | null; onClose: () => void }) {
  const [busy, setBusy] = React.useState(false)
  const confirm = React.useRef<HTMLButtonElement>(null)
  const cancel = React.useRef<HTMLButtonElement>(null)
  // A destructive action starts on Cancel, so a stray Enter does no harm.
  React.useEffect(() => {
    if (request) (request.danger ? cancel : confirm).current?.focus()
  }, [request])
  if (!request) return null
  const go = async () => {
    setBusy(true)
    try {
      await request.run()
    } finally {
      setBusy(false)
      onClose()
    }
  }
  return (
    <Dialog open onClose={busy ? () => {} : onClose} title={request.title} description={request.description}>
      {request.body && <div className="mb-4">{request.body}</div>}
      <div className="flex justify-end gap-2">
        <Button ref={cancel} variant="secondary" onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button
          ref={confirm}
          onClick={() => void go()}
          disabled={busy}
          className={
            request.danger
              ? "border border-danger/45 bg-danger-soft text-danger-fg shadow-none hover:bg-danger/20"
              : undefined
          }
        >
          {busy ? "Working" : request.confirmLabel}
        </Button>
      </div>
    </Dialog>
  )
}
