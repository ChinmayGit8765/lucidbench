import { useState } from "react"

import { CopyCommand } from "@/components/CopyCommand"
import { ProviderTile, PROVIDERS } from "@/components/ProviderMark"
import { Dialog } from "@/components/ui/dialog"
import { cn } from "@/lib/utils"

/** Providers `lucid login` can sign in to. */
const LOGIN_PROVIDERS = PROVIDERS.filter((p) => p.id !== "cursor")

const NAME_RE = /^[a-z0-9][a-z0-9_.-]{0,40}$/

/**
 * "Add account": pick a provider, name the profile, and copy the command
 * that signs it in. The shell loads it on first use, so the Accounts page
 * keeps a chunk of its own.
 */
export default function AddAccountDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [provider, setProvider] = useState(LOGIN_PROVIDERS[0].id)
  const [name, setName] = useState("")
  const valid = NAME_RE.test(name)
  const command = `lucid login ${provider} --profile ${valid ? name : "<name>"}`
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Add account"
      description="Lucidbench never signs in for you. Pick a provider, name the profile, then run the command in a terminal."
    >
      <div className="space-y-5">
        <fieldset className="space-y-2">
          <legend className="mb-2 text-xs font-medium text-muted-foreground">Provider</legend>
          <div role="radiogroup" className="grid grid-cols-3 gap-2">
            {LOGIN_PROVIDERS.map((p) => {
              const on = provider === p.id
              return (
                <button
                  key={p.id}
                  type="button"
                  role="radio"
                  aria-checked={on}
                  onClick={() => setProvider(p.id)}
                  className={cn(
                    "flex flex-col items-center gap-2 rounded-lg border px-2 py-3 text-center transition-[border-color,background-color,box-shadow] duration-150",
                    on
                      ? "border-brand bg-brand-soft shadow-[0_0_0_1px_var(--brand)]"
                      : "hover:border-border-strong hover:bg-accent/50",
                  )}
                >
                  <ProviderTile provider={p.id} />
                  <span className="leading-tight">
                    <span className="block text-sm font-medium">{p.label}</span>
                    <span className="block text-2xs text-subtle-foreground">{p.vendor}</span>
                  </span>
                </button>
              )
            })}
          </div>
          <p className="text-2xs text-subtle-foreground">Cursor is detected from its own CLI sign-in.</p>
        </fieldset>
        <label className="block space-y-2">
          <span className="text-xs font-medium text-muted-foreground">Profile name</span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value.toLowerCase())}
            placeholder="work"
            aria-invalid={name !== "" && !valid}
            className="h-9 w-full rounded-lg border border-input bg-background px-3 font-mono text-sm outline-none transition-shadow placeholder:text-subtle-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 aria-[invalid=true]:border-danger"
          />
          {name && !valid && (
            <span className="block text-xs text-danger-fg">
              Use lowercase letters, digits, dot, dash or underscore, starting with a letter or digit.
            </span>
          )}
        </label>
        <div className="space-y-2">
          <span className="text-xs font-medium text-muted-foreground">Command</span>
          {valid ? (
            <CopyCommand command={command} />
          ) : (
            <div className="flex h-9 items-center rounded-lg border border-dashed px-3 font-mono text-xs text-subtle-foreground">
              {command}
            </div>
          )}
        </div>
      </div>
    </Dialog>
  )
}
