import { useState } from "react"
import { Check, Copy } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/** Copies text to the clipboard and confirms with a toast. Returns success. */
export async function copyText(text: string, what = "Copied to clipboard"): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(what)
    return true
  } catch {
    // Clipboard can be unavailable on plain http; the text stays selectable.
    toast.error("Clipboard unavailable", { description: "Select the text and copy it manually." })
    return false
  }
}

/** A monospace command with a copy button. */
export function CopyCommand({ command, className }: { command: string; className?: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    if (await copyText(command, "Command copied")) {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }
  }
  return (
    <div
      className={cn(
        "flex h-9 items-center gap-2 rounded-lg border bg-background pl-3 pr-1 font-mono text-xs",
        className,
      )}
    >
      <span aria-hidden className="select-none text-subtle-foreground">
        $
      </span>
      <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap select-all">{command}</code>
      <Button variant="ghost" size="sm" onClick={copy} aria-label="Copy command">
        {copied ? <Check className="text-success" /> : <Copy />}
        {copied ? "Copied" : "Copy"}
      </Button>
    </div>
  )
}
