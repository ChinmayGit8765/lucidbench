import { useState } from "react"
import { Check, Copy } from "lucide-react"

import { copyText } from "@/components/CopyCommand"
import { Button } from "@/components/ui/button"

/** An icon button that copies text and shows a tick for a moment. */
export function CopyButton({ text, what, label }: { text: string; what: string; label: string }) {
  const [done, setDone] = useState(false)
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label={label}
      title={label}
      onClick={async () => {
        if (await copyText(text, what)) {
          setDone(true)
          setTimeout(() => setDone(false), 1500)
        }
      }}
    >
      {done ? <Check /> : <Copy />}
    </Button>
  )
}
