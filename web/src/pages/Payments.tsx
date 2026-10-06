import { useState } from "react"
import { Boxes, CreditCard, ExternalLink, FlaskConical, History, Link2, Receipt, Rocket, ShieldAlert, Webhook as WebhookIcon } from "lucide-react"

import { SetupCard } from "@/components/SetupCard"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { StatusPill } from "@/components/ui/badge"
import { Card } from "@/components/ui/card"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { Tabs, type TabItem } from "@/components/ui/tabs"
import { usePoll } from "@/lib/api"
import {
  money,
  PAYMENT_STATUS,
  PAYMENTS_POLL_MS,
  priceLabel,
  type Account,
  type AuditEntry,
  type Balance,
  type Catalog,
  type Mode,
  type PaymentLink,
  type Payments as PaymentsList,
  type PaymentsStatus,
  type Webhook,
} from "@/lib/payments"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn, plural } from "@/lib/utils"
import { CopyButton } from "@/pages/payments/CopyButton"
import { SetupWizard } from "@/pages/payments/Wizard"

type TabId = "payments" | "catalog" | "webhooks" | "setup" | "audit"

const TABS: TabItem[] = [
  { id: "payments", label: "Payments", icon: Receipt },
  { id: "catalog", label: "Products and links", icon: Boxes },
  { id: "webhooks", label: "Webhooks", icon: WebhookIcon },
  { id: "setup", label: "Set up payments", icon: Rocket },
  { id: "audit", label: "Audit log", icon: History },
]

export default function Payments({ subpath }: { subpath: string[] }) {
  const status = usePoll<PaymentsStatus>("/api/payments/status", 30000)
  return (
    <div className="space-y-6">
      <PageHeader
        icon={<CreditCard />}
        title="Payments"
        description="Your Stripe account at a glance, and a test-mode setup for a project: products, prices, a payment link and a webhook. Nothing here moves money."
        actions={<RefreshButton refreshing={status.refreshing} updatedAt={status.updatedAt} />}
      />
      {status.error && !status.data ? (
        <ErrorState title="Could not check the Stripe connector" message={status.error.message} onRetry={status.refresh} />
      ) : !status.data ? (
        <Skeleton className="h-64 rounded-xl" />
      ) : !status.data.configured ? (
        <SetupCard
          title="Connect Stripe with a restricted key"
          intro="Lucidbench reads your Stripe account through its API with a key from your environment. Start with a restricted key that can only read, so the page works and nothing can be changed by mistake."
          note={
            <>
              A Stripe MCP server alone is not enough for this page: it needs the key. Test-mode keys can also create the setup objects; live-mode keys are read-only in this version.
            </>
          }
          steps={[
            <>
              In the Stripe Dashboard open <code>Developers › API keys › Create restricted key</code>. Give it <strong>Read</strong> on Balance, PaymentIntents, Products, Prices, Payment Links, Webhook Endpoints and Account.
            </>,
            <>
              To try the setup wizard, switch the Dashboard to <strong>test mode</strong> and add <strong>Write</strong> on Products, Prices, Payment Links and Webhook Endpoints to a test-mode restricted key.
            </>,
            <>
              Set the key as <code>{status.data.key_ref.replace(/^env:/, "")}</code> where the daemon starts, then restart Lucidbench. The mode (test or live) is read from the key.
            </>,
            <>
              To use another variable, point <code>integrations.stripe.key</code> at it, as <code>env:NAME</code>, in your config.
            </>,
          ]}
          vars={[{ name: status.data.key_ref.replace(/^env:/, ""), set: status.data.configured }]}
        />
      ) : (
        <Connected status={status.data} initial={subpath[0]} />
      )}
    </div>
  )
}

/** The big mode badge: this account's money is real or it is not. */
function ModeBadge({ mode, className }: { mode: Mode; className?: string }) {
  const live = mode === "live"
  const unknown = mode === "unknown"
  return (
    <span
      className={cn(
        "inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-lg font-semibold tracking-wide",
        live ? "border-warning/50 bg-warning-soft text-warning-fg" : unknown ? "border bg-neutral-soft text-neutral-fg" : "border-info/40 bg-info-soft text-info-fg",
        className,
      )}
    >
      {live ? <ShieldAlert className="size-5" /> : <FlaskConical className="size-5" />}
      {live ? "LIVE" : unknown ? "UNKNOWN" : "TEST"}
    </span>
  )
}

function Connected({ status, initial }: { status: PaymentsStatus; initial?: string }) {
  const [tab, setTab] = useState<TabId>(TABS.some((t) => t.id === initial) ? (initial as TabId) : "payments")
  const account = usePoll<Account>("/api/payments/account", PAYMENTS_POLL_MS)
  return (
    <>
      <Card className="relative overflow-hidden">
        <div aria-hidden className="tile-glow pointer-events-none absolute inset-0" />
        <div className="relative flex flex-wrap items-center gap-4 p-4">
          <ModeBadge mode={status.mode} />
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">{account.data?.name || (account.error ? "Account unavailable" : "Reading the account")}</div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              {status.mode === "live"
                ? "Real money. This version only reads a live account; it cannot create anything."
                : status.mode === "test"
                  ? "Test data only. The setup wizard can create products, prices, a link and a webhook here."
                  : "The key is not a recognisable Stripe secret or restricted key, so nothing can be created."}
            </div>
            {account.error && !account.data && <div className="mt-1 text-xs text-danger-fg">{account.error.message}</div>}
          </div>
          {account.data && (
            <div className="flex items-center gap-2">
              {account.data.country && <StatusPill tone="neutral">{account.data.country}</StatusPill>}
              <StatusPill tone={account.data.charges_enabled ? "success" : "warning"}>{account.data.charges_enabled ? "charges on" : "charges off"}</StatusPill>
              <StatusPill tone={account.data.payouts_enabled ? "success" : "warning"}>{account.data.payouts_enabled ? "payouts on" : "payouts off"}</StatusPill>
            </div>
          )}
        </div>
      </Card>
      <Tabs items={TABS} value={tab} onChange={(t) => setTab(t as TabId)} label="Payments sections" />
      {tab === "payments" && <PaymentsTab />}
      {tab === "catalog" && <CatalogTab />}
      {tab === "webhooks" && <WebhooksTab />}
      {tab === "setup" && <SetupWizard status={status} />}
      {tab === "audit" && <AuditTab />}
    </>
  )
}

const th = "px-3 py-2 font-medium"
const headRow = "border-y bg-muted/40 text-left text-2xs font-medium uppercase tracking-[0.06em] text-subtle-foreground"

function PaymentsTab() {
  const balance = usePoll<Balance>("/api/payments/balance", PAYMENTS_POLL_MS)
  const list = usePoll<PaymentsList>("/api/payments/charges?limit=25", PAYMENTS_POLL_MS)
  const now = useNow(30000)
  return (
    <div className="space-y-4">
      <div className="grid gap-3 @3xl:grid-cols-2">
        <BalanceCard title="Available" amounts={balance.data?.available} loading={!balance.data && !balance.error} error={balance.error?.message} />
        <BalanceCard title="Pending" amounts={balance.data?.pending} loading={!balance.data && !balance.error} error={balance.error?.message} />
      </div>
      <Card className="overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3">
          <h2 className="text-sm font-semibold">Recent payments</h2>
          {list.data && <span className="text-xs text-subtle-foreground">{plural(list.data.items.length, "payment")}{list.data.has_more ? ", newest first" : ""}</span>}
        </div>
        {list.error && !list.data ? (
          <div className="p-4">
            <ErrorState title="Could not read payments" message={list.error.message} onRetry={list.refresh} />
          </div>
        ) : !list.data ? (
          <Skeleton className="m-4 h-40" />
        ) : list.data.items.length === 0 ? (
          <EmptyState icon={<Receipt />} title="No payments yet" description="Payments made through a payment link or checkout show here." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className={headRow}>
                  <th className={cn(th, "pl-5")}>Amount</th>
                  <th className={th}>Status</th>
                  <th className={th}>Description</th>
                  <th className={cn(th, "pr-5 text-right")}>When</th>
                </tr>
              </thead>
              <tbody>
                {list.data.items.map((p) => {
                  const s = PAYMENT_STATUS[p.status] ?? { tone: "neutral" as const, label: p.status.replace(/_/g, " ") }
                  const iso = new Date(p.created * 1000).toISOString()
                  return (
                    <tr key={p.id} className="border-b transition-colors last:border-b-0 hover:bg-accent/40">
                      <td className="px-3 py-2 pl-5 font-medium tabular-nums">{money(p.amount, p.currency)}</td>
                      <td className="px-3 py-2">
                        <StatusPill tone={s.tone}>{s.label}</StatusPill>
                      </td>
                      <td className="max-w-[24rem] truncate px-3 py-2 text-muted-foreground" title={p.description}>
                        {p.description || <span className="text-subtle-foreground">no description</span>}
                      </td>
                      <td className="px-3 py-2 pr-5 text-right text-xs text-muted-foreground" title={absoluteTime(iso)}>
                        {relativeTime(iso, now)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  )
}

function BalanceCard({ title, amounts, loading, error }: { title: string; amounts?: { amount: number; currency: string }[]; loading: boolean; error?: string }) {
  return (
    <Card className="p-4">
      <div className="text-xs font-medium text-muted-foreground">{title}</div>
      {loading ? (
        <Skeleton className="mt-3 h-8 w-32" />
      ) : error ? (
        <div className="mt-2 text-sm text-danger-fg">{error}</div>
      ) : amounts && amounts.length > 0 ? (
        <div className="mt-1.5 space-y-0.5">
          {amounts.map((a) => (
            <div key={a.currency} className="text-2xl font-semibold tabular-nums tracking-tight">
              {money(a.amount, a.currency)}
            </div>
          ))}
        </div>
      ) : (
        <div className="mt-1.5 text-2xl font-semibold tabular-nums tracking-tight text-muted-foreground">0</div>
      )}
    </Card>
  )
}

function CatalogTab() {
  const catalog = usePoll<Catalog>("/api/payments/products", PAYMENTS_POLL_MS)
  const links = usePoll<{ links: PaymentLink[] }>("/api/payments/links", PAYMENTS_POLL_MS)
  return (
    <div className="space-y-4">
      <Card className="overflow-hidden">
        <div className="px-5 py-3">
          <h2 className="text-sm font-semibold">Products and prices</h2>
        </div>
        {catalog.error && !catalog.data ? (
          <div className="p-4">
            <ErrorState title="Could not read products" message={catalog.error.message} onRetry={catalog.refresh} />
          </div>
        ) : !catalog.data ? (
          <Skeleton className="m-4 h-32" />
        ) : catalog.data.products.length === 0 ? (
          <EmptyState icon={<Boxes />} title="No products yet" description="Use Set up payments to create a product with prices and a payment link in test mode." />
        ) : (
          <ul>
            {catalog.data.products.map((p) => (
              <li key={p.id} className="border-t px-5 py-3">
                <div className="flex items-baseline gap-2">
                  <span className="text-sm font-medium">{p.name}</span>
                  <span className="font-mono text-2xs text-subtle-foreground">{p.id}</span>
                </div>
                {p.description && <div className="mt-0.5 text-xs text-muted-foreground">{p.description}</div>}
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {p.prices.length === 0 ? (
                    <span className="text-xs text-subtle-foreground">no active prices</span>
                  ) : (
                    p.prices.map((pr) => (
                      <span key={pr.id} title={pr.id} className="inline-flex items-center gap-1.5 rounded-md border bg-muted/50 px-2 py-0.5 text-xs tabular-nums">
                        {priceLabel(pr)}
                        {pr.nickname && <span className="text-subtle-foreground">{pr.nickname}</span>}
                      </span>
                    ))
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <Card className="overflow-hidden">
        <div className="px-5 py-3">
          <h2 className="text-sm font-semibold">Payment links</h2>
        </div>
        {links.error && !links.data ? (
          <div className="p-4">
            <ErrorState title="Could not read payment links" message={links.error.message} onRetry={links.refresh} />
          </div>
        ) : !links.data ? (
          <Skeleton className="m-4 h-20" />
        ) : links.data.links.length === 0 ? (
          <EmptyState icon={<Link2 />} title="No payment links" description="A payment link is a hosted page where buyers pay." />
        ) : (
          <ul>
            {links.data.links.map((l) => (
              <li key={l.id} className="flex items-center gap-3 border-t px-5 py-2.5">
                <StatusPill tone={l.active ? "success" : "neutral"}>{l.active ? "active" : "inactive"}</StatusPill>
                <span className="min-w-0 flex-1 truncate font-mono text-xs" title={l.url}>
                  {l.url}
                </span>
                <CopyButton text={l.url} what="Link copied" label="Copy link" />
                <a href={l.url} target="_blank" rel="noreferrer" aria-label={`Open ${l.id}`} className="inline-flex size-7 items-center justify-center rounded-md text-subtle-foreground hover:bg-accent hover:text-foreground">
                  <ExternalLink className="size-3.5" />
                </a>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  )
}

function WebhooksTab() {
  const hooks = usePoll<{ webhooks: Webhook[] }>("/api/payments/webhooks", PAYMENTS_POLL_MS)
  return (
    <Card className="overflow-hidden">
      <div className="px-5 py-3">
        <h2 className="text-sm font-semibold">Webhook endpoints</h2>
        <p className="mt-0.5 text-xs text-muted-foreground">Stripe only shows an endpoint's signing secret when it is created, so it cannot be shown here.</p>
      </div>
      {hooks.error && !hooks.data ? (
        <div className="p-4">
          <ErrorState title="Could not read webhooks" message={hooks.error.message} onRetry={hooks.refresh} />
        </div>
      ) : !hooks.data ? (
        <Skeleton className="m-4 h-24" />
      ) : hooks.data.webhooks.length === 0 ? (
        <EmptyState icon={<WebhookIcon />} title="No webhook endpoints" description="Add one in Set up payments to be told when someone pays." />
      ) : (
        <ul>
          {hooks.data.webhooks.map((w) => (
            <li key={w.id} className="border-t px-5 py-3">
              <div className="flex items-center gap-2">
                <StatusPill tone={w.status === "enabled" ? "success" : "neutral"}>{w.status}</StatusPill>
                <span className="min-w-0 flex-1 truncate font-mono text-xs" title={w.url}>
                  {w.url}
                </span>
                <span className="font-mono text-2xs text-subtle-foreground">{w.id}</span>
              </div>
              <div className="mt-2 flex flex-wrap gap-1.5">
                {w.enabled_events.map((e) => (
                  <span key={e} className="rounded-md border bg-muted/50 px-1.5 py-0.5 font-mono text-2xs text-muted-foreground">
                    {e}
                  </span>
                ))}
              </div>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

function AuditTab() {
  const audit = usePoll<{ entries: AuditEntry[] }>("/api/payments/audit?limit=100", 15000)
  const now = useNow(30000)
  return (
    <Card className="overflow-hidden">
      <div className="px-5 py-3">
        <h2 className="text-sm font-semibold">Audit log</h2>
        <p className="mt-0.5 text-xs text-muted-foreground">Every write Lucidbench made to Stripe, newest first: when, in which mode, and which objects. It holds no keys and no secrets.</p>
      </div>
      {audit.error && !audit.data ? (
        <div className="p-4">
          <ErrorState title="Could not read the audit log" message={audit.error.message} onRetry={audit.refresh} />
        </div>
      ) : !audit.data ? (
        <Skeleton className="m-4 h-24" />
      ) : audit.data.entries.length === 0 ? (
        <EmptyState icon={<History />} title="No writes yet" description="Anything created from Set up payments is recorded here." />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className={headRow}>
                <th className={cn(th, "pl-5")}>When</th>
                <th className={th}>Mode</th>
                <th className={th}>Action</th>
                <th className={th}>Project</th>
                <th className={th}>Objects</th>
                <th className={cn(th, "pr-5")}>Result</th>
              </tr>
            </thead>
            <tbody>
              {audit.data.entries.map((e, i) => (
                <tr key={`${e.time}-${i}`} className="border-b last:border-b-0 hover:bg-accent/40">
                  <td className="whitespace-nowrap px-3 py-2 pl-5 text-xs text-muted-foreground" title={absoluteTime(e.time)}>
                    {relativeTime(e.time, now)}
                  </td>
                  <td className="px-3 py-2">
                    <StatusPill tone={e.mode === "live" ? "warning" : "info"}>{e.mode}</StatusPill>
                  </td>
                  <td className="px-3 py-2 font-mono text-xs">{e.action}</td>
                  <td className="px-3 py-2 text-xs text-muted-foreground">{e.project ?? ""}</td>
                  <td className="px-3 py-2 font-mono text-2xs text-muted-foreground">{e.ids.join(", ")}</td>
                  <td className="px-3 py-2 pr-5">{e.ok ? <StatusPill tone="success">ok</StatusPill> : <StatusPill tone="danger">{e.error ?? "failed"}</StatusPill>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}
