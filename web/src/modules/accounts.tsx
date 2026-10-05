import { lazy } from "react"
import { ArrowRight, KeyRound, Plus, Users } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { ProviderTile, PROVIDERS } from "@/components/ProviderMark"
import { StatTile } from "@/components/StatTile"
import { Button } from "@/components/ui/button"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import type { AttentionItem, ModuleDef } from "@/modules/types"
import type { Account } from "@/pages/Accounts"

function AccountsTile() {
  const { open } = useApp()
  const poll = usePoll<Account[]>("/api/accounts", 15000)
  const accs = poll.data ?? []
  const connected = PROVIDERS.filter((p) => accs.some((a) => a.provider === p.id && a.status === "logged_in"))
  const expired = accs.filter((a) => a.status === "expired")
  return (
    <StatTile
      icon={Users}
      label="AI accounts"
      onOpen={() => open("accounts")}
      loading={poll.loading && !poll.data}
      value={
        <span>
          {connected.length}
          <span className="text-base font-normal text-subtle-foreground">/{PROVIDERS.length}</span>
        </span>
      }
      sub={
        <>
          providers · {accs.length} {accs.length === 1 ? "account" : "accounts"}
          {expired.length > 0 && <span className="text-warning-fg"> · {expired.length} expired</span>}
        </>
      }
      footer={
        <div className="flex gap-1.5">
          {PROVIDERS.map((p) => (
            <ProviderTile key={p.id} provider={p.id} size="sm" muted={!connected.includes(p)} />
          ))}
        </div>
      }
    />
  )
}

function useAccountCommands(): Command[] {
  const { addAccount } = useApp()
  return [
    {
      id: "add-account",
      label: "Add account",
      group: "Actions",
      icon: Plus,
      keywords: "login profile claude codex grok",
      run: addAccount,
    },
  ]
}

/** Needs attention: sign-ins that expired, so Council and Work cannot use them. */
function useAccountAttention(): AttentionItem[] | null {
  const { open } = useApp()
  const poll = usePoll<Account[]>("/api/accounts", 15000)
  if (!poll.data) return poll.error ? [] : null
  return poll.data
    .filter((a) => a.status === "expired")
    .map(
      (a): AttentionItem => ({
        key: `acc-${a.provider}-${a.name}`,
        severity: "warning",
        icon: <KeyRound className="size-3.5 text-warning" />,
        title: <>{PROVIDERS.find((p) => p.id === a.provider)?.label ?? a.provider} sign-in expired</>,
        meta: <span className="font-mono">{a.name}</span>,
        action: (
          <Button variant="ghost" size="sm" onClick={() => open("accounts")}>
            Fix <ArrowRight />
          </Button>
        ),
      }),
    )
}

export const accounts: ModuleDef = {
  id: "accounts",
  title: "Accounts",
  icon: Users,
  route: "/accounts",
  section: "ai",
  kind: "core",
  order: 0,
  defaultEnabled: true,
  description: "Your Claude, Codex, Grok and Cursor accounts, detected by presence only.",
  keywords: "login profiles subscriptions providers",
  component: lazy(() => import("@/pages/Accounts")),
  useCommands: useAccountCommands,
  overviewTile: AccountsTile,
  useAttention: useAccountAttention,
}
