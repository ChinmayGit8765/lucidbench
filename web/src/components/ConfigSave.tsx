import { useState } from "react"
import { RotateCw, Save } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { errorMessage, sendJSON, usePoll } from "@/lib/api"
import { cn, plural } from "@/lib/utils"

/* Mirrors internal/config (write.go, http.go): keep these in step with the Go structs. */

/** GET /api/config/file: the keys the UI may save, as config.yaml sets them now. */
export interface ConfigFile {
  path: string
  found: boolean
  values: Record<string, string | number | boolean>
  sources: Record<string, string>
  /** Saved in the file, not yet used by the running engine: a restart applies them. */
  pending: string[]
  error?: string
}

export interface ConfigEdit {
  path: string
  created: boolean
  changed: boolean
  diff: { op: " " | "+" | "-"; text: string }[]
  overridden: string[]
  restart: string[]
  backup?: string
}

export const CONFIG_FILE_PATH = "/api/config/file"

export const useConfigFile = () => usePoll<ConfigFile>(CONFIG_FILE_PATH, 60000)

/** The change a save would make to config.yaml, as a small diff. */
export function ConfigDiff({ edit }: { edit: ConfigEdit }) {
  return (
    <div className="space-y-2">
      <div className="truncate font-mono text-2xs text-subtle-foreground" title={edit.path}>
        {edit.created ? "new file: " : ""}
        {edit.path}
      </div>
      <pre className="max-h-64 overflow-auto rounded-lg border bg-background py-1.5 font-mono text-xs leading-5" data-testid="config-diff">
        {edit.diff.map((l, i) => (
          <div
            key={i}
            className={cn(
              "whitespace-pre px-3",
              l.op === "+" ? "bg-success-soft/70 text-success-fg" : l.op === "-" ? "bg-danger-soft/70 text-danger-fg" : "text-muted-foreground",
            )}
          >
            {l.op === " " ? "  " : `${l.op} `}
            {l.text || " "}
          </div>
        ))}
      </pre>
      {edit.overridden.length > 0 && (
        <p className="text-xs text-warning-fg">
          {edit.overridden.join(", ")} {edit.overridden.length === 1 ? "is" : "are"} set by an environment variable, which wins over the file while it is set.
        </p>
      )}
      <p className="text-xs text-muted-foreground">The old file is kept as a backup next to it. Only these lines change; comments and other keys stay.</p>
    </div>
  )
}

/**
 * Saving settings to config.yaml: ask the engine for the diff, show it in a
 * confirm, then write. The returned element is the confirm dialog to render.
 */
export function useConfigSave(onSaved?: (e: ConfigEdit) => void) {
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [busy, setBusy] = useState(false)
  const save = async (set: Record<string, unknown>, what: string) => {
    setBusy(true)
    try {
      const plan = await sendJSON<ConfigEdit>("/api/config/preview", "POST", { set })
      if (!plan.changed) {
        toast.message("config.yaml already says that.")
        return
      }
      setConfirm({
        title: `Save ${what} to config.yaml?`,
        description: plan.restart.length
          ? "Lucidbench reads its config when it starts, so this applies after you restart it."
          : "The engine already uses these values; this makes them stick.",
        body: <ConfigDiff edit={plan} />,
        confirmLabel: "Save to config.yaml",
        run: async () => {
          try {
            const done = await sendJSON<ConfigEdit>("/api/config/save", "POST", { set })
            toast.success("Saved to config.yaml", {
              description: done.restart.length ? "Restart Lucidbench to apply it." : undefined,
            })
            onSaved?.(done)
          } catch (e) {
            toast.error("Could not save the config", { description: errorMessage(e) })
          }
        },
      })
    } catch (e) {
      toast.error("Could not save the config", { description: errorMessage(e) })
    } finally {
      setBusy(false)
    }
  }
  return { save, busy, dialog: <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} /> }
}

/** Saved settings the running engine does not use yet. There is no restart from the UI, so it says how. */
export function PendingRestart({ file, keys }: { file?: ConfigFile | null; keys?: string[] }) {
  const pending = (file?.pending ?? []).filter((k) => !keys || keys.includes(k))
  if (pending.length === 0) return null
  return (
    <div className="flex items-start gap-2.5 border-t bg-info-soft/60 px-5 py-3 text-sm text-info-fg" role="status" data-testid="config-pending">
      <RotateCw className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0">
        <span className="font-medium">Saved. Restart Lucidbench to apply {plural(pending.length, "change")}.</span>{" "}
        <span className="opacity-90">
          Quit the app (or stop <code className="font-mono text-xs">lucidd</code>) and start it again; until then it runs with the old values ({pending.join(", ")}).
        </span>
      </div>
    </div>
  )
}

/**
 * Names the environment variable a connector reads its secret from, and
 * saves it to config.yaml as env:NAME. Only the name is ever saved: the
 * secret stays in the environment.
 */
export function EnvVarField({ configKey, current, label }: { configKey: string; current: string; label: string }) {
  const file = useConfigFile()
  const now = current.replace(/^env:/, "")
  const [name, setName] = useState(now)
  const { save, busy, dialog } = useConfigSave(() => file.refresh())
  const valid = /^[A-Za-z_][A-Za-z0-9_]*$/.test(name)
  const pending = file.data?.pending.includes(configKey)
  return (
    <span className="mt-1.5 flex flex-wrap items-center gap-2">
      <span className="text-xs">env:</span>
      <input
        aria-label={label}
        value={name}
        onChange={(e) => setName(e.target.value.trim())}
        spellCheck={false}
        className="h-7 w-48 rounded-md border bg-background px-2 font-mono text-xs text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
      />
      <Button size="sm" variant="secondary" disabled={busy || !valid || name === now} onClick={() => void save({ [configKey]: `env:${name}` }, `${configKey} = env:${name}`)}>
        <Save /> Save
      </Button>
      {!valid && name !== "" && <span className="text-xs text-danger-fg">A variable name: letters, digits and _.</span>}
      {pending && <span className="text-xs text-info-fg">Saved · restart Lucidbench to use it</span>}
      {dialog}
    </span>
  )
}

/** A Save button for a settings section. */
export function SaveButton({ dirty, busy, onClick, label = "Save" }: { dirty: boolean; busy: boolean; onClick: () => void; label?: string }) {
  return (
    <Button size="sm" onClick={onClick} disabled={!dirty || busy} data-testid="config-save">
      <Save /> {label}
    </Button>
  )
}
