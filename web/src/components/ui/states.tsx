import { AlertTriangle } from "lucide-react"

import { Card } from "@/components/ui/card"

export function LoadingState({ label = "Loading..." }: { label?: string }) {
  return <div className="py-8 text-center text-sm text-muted-foreground">{label}</div>
}

export function ErrorState({ title, message }: { title: string; message?: string }) {
  return (
    <Card className="border-destructive/40 p-4">
      <div className="flex items-start gap-3">
        <AlertTriangle className="mt-0.5 size-4 shrink-0 text-destructive" />
        <div className="min-w-0 text-sm">
          <div className="font-medium">{title}</div>
          {message && <div className="mt-1 break-words text-muted-foreground">{message}</div>}
        </div>
      </div>
    </Card>
  )
}
