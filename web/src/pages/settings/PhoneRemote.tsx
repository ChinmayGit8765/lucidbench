import { useEffect, useState } from "react"
import { QrCode, RefreshCw, ShieldAlert, Smartphone, Trash2 } from "lucide-react"

import { CopyCommand } from "@/components/CopyCommand"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, refreshAll, usePoll } from "@/lib/api"
import {
  ACTION_LABEL,
  cancelPairCode,
  KIND_LABEL,
  newPairCode,
  REMOTE_PATH,
  revokeDevice,
  saveRemote,
  type PairCode,
  type RemoteAudit,
  type RemoteDevice,
  type RemoteInterface,
  type RemoteMode,
  type RemoteSettings,
  type RemoteStatus,
} from "@/lib/remote"
import { relativeTime, useNow } from "@/lib/time"
import { Row, Section, Segmented, Switch } from "@/pages/settings/controls"

/**
 * Settings › Phone remote: the opt-in second listener a paired phone uses.
 * Off by default. Turning it on binds one chosen interface address, never
 * every interface; the QR code holds a one-time pairing code.
 */
export function PhoneRemote() {
  const status = usePoll<RemoteStatus>(REMOTE_PATH, 5000)
  const ifs = usePoll<RemoteInterface[]>(`${REMOTE_PATH}/interfaces`, 30000)
  const devices = usePoll<RemoteDevice[]>(`${REMOTE_PATH}/devices`, 10000)
  const audit = usePoll<RemoteAudit[]>(`${REMOTE_PATH}/audit?limit=20`, 10000)
  const now = useNow(5000)
  const [draft, setDraft] = useState<RemoteSettings | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [pair, setPair] = useState<PairCode | null>(null)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)

  // The form starts from the saved settings and follows them until edited.
  const saved = status.data?.settings
  useEffect(() => {
    if (saved && !draft) setDraft(saved)
  }, [saved, draft])

  // A pairing code is shown until it expires or a phone pairs.
  const pairedCount = devices.data?.length ?? 0
  const [pairedAtStart, setPairedAtStart] = useState(0)
  useEffect(() => {
    if (!pair) return
    if (pairedCount > pairedAtStart || new Date(pair.expires).getTime() <= now) setPair(null)
  }, [pair, pairedCount, pairedAtStart, now])

  if (status.error && !status.data) return <ErrorState title="Cannot read the remote's settings" message={status.error.message} onRetry={status.refresh} />
  if (!status.data || !draft) return <Skeleton className="h-64" />
  const st = status.data
  const list = ifs.data ?? []
  const dirty = JSON.stringify(draft) !== JSON.stringify(st.settings)

  const apply = async (next: RemoteSettings) => {
    setBusy(true)
    setError(null)
    try {
      const out = await saveRemote(next)
      setDraft(out.settings)
      if (!out.settings.enabled) setPair(null)
      status.refresh()
      audit.refresh()
      if (out.error) setError(out.error)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  const enable = (on: boolean) => {
    const next = { ...draft, enabled: on }
    if (!on) return void apply(next)
    setConfirm({
      title: "Turn on the phone remote?",
      description:
        "A second listener opens on the chosen interface. Anyone on that network can reach its page, but only a paired phone with its token can read or act. A paired phone can see what needs you, follow and stop running agents, and approve or send back briefs; it cannot reach settings, accounts, files or secrets.",
      confirmLabel: "Turn on",
      run: () => apply(next),
    })
  }

  const showCode = async () => {
    setError(null)
    try {
      setPairedAtStart(pairedCount)
      setPair(await newPairCode())
    } catch (e) {
      setError(errorMessage(e))
    }
  }

  const choices = list.filter((i) => i.kind !== "other" || i.address === draft.address)
  return (
    <div className="space-y-6" data-testid="phone-remote">
      <Section
        id="remote"
        title="Phone remote"
        description="Check on Lucidbench from your phone: what needs you, running agents with a live tail, and briefs to approve. Off unless you turn it on."
        actions={<StatusPill tone={st.listening ? "success" : st.settings.enabled ? "danger" : "neutral"}>{st.listening ? `Listening on ${st.address}` : st.settings.enabled ? "Not listening" : "Off"}</StatusPill>}
      >
        <div className="mx-5 mb-3 flex gap-3 rounded-lg border border-warning/40 bg-warning-soft px-3 py-2.5 text-xs text-warning-fg">
          <ShieldAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
          <p>
            The remote opens a port on your network. It speaks plain HTTP, so use it on a network you trust, or over Tailscale from anywhere else. Never forward it to the internet. Every write needs a paired token and a confirm tap, and every one is logged below.
          </p>
        </div>
        <Row label="Enable the remote" hint="Binds a second listener; lucidd itself stays on 127.0.0.1.">
          <Switch checked={st.settings.enabled} onChange={enable} label="Enable the phone remote" />
        </Row>
        <Row label="Listen on" hint={draft.mode === "tailnet" ? (st.tailnet ? `This machine's Tailscale address is ${st.tailnet}.` : "No Tailscale address on this machine now.") : "One interface only; never every interface."}>
          <Segmented<RemoteMode>
            label="Remote mode"
            value={draft.mode}
            onChange={(mode) => setDraft({ ...draft, mode })}
            options={[
              { id: "lan", label: "LAN interface", title: "An address on your local network" },
              { id: "tailnet", label: "Tailnet only", title: "This machine's Tailscale address (100.64.0.0/10)" },
            ]}
          />
          {draft.mode === "lan" && (
            <select
              aria-label="Interface"
              className="h-8 rounded-md border bg-background px-2 text-sm"
              value={draft.address}
              onChange={(e) => setDraft({ ...draft, address: e.target.value })}
            >
              <option value="">Choose an interface…</option>
              {choices.map((i) => (
                <option key={`${i.name}-${i.address}`} value={i.address}>
                  {i.address} · {i.name} ({KIND_LABEL[i.kind]})
                </option>
              ))}
            </select>
          )}
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
            Port
            <input
              type="number"
              aria-label="Port"
              min={1024}
              max={65535}
              value={draft.port}
              onChange={(e) => setDraft({ ...draft, port: Number(e.target.value) || 0 })}
              className="h-8 w-20 rounded-md border bg-background px-2 text-sm text-foreground"
            />
          </label>
          <Button size="sm" variant="secondary" disabled={!dirty || busy} onClick={() => void apply(draft)}>
            Save
          </Button>
        </Row>
        {(error || st.error) && (
          <p role="alert" className="border-t px-5 py-3 text-sm text-danger-fg">
            {error || st.error}
          </p>
        )}
      </Section>

      <Section
        title="Pair a phone"
        description="Scan the code with the phone's camera. It works once, for five minutes, and gives the phone its own token, kept on the phone; this computer keeps only its hash."
        actions={
          <Button size="sm" onClick={() => void showCode()} disabled={!st.listening} data-testid="pair-phone">
            {pair ? <RefreshCw /> : <QrCode />}
            {pair ? "New code" : "Show pairing code"}
          </Button>
        }
      >
        {!st.listening && <p className="border-t px-5 py-3 text-sm text-muted-foreground">Turn the remote on first.</p>}
        {pair && (
          <div className="flex flex-wrap items-center gap-6 border-t px-5 py-4">
            <img src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(pair.qr_svg)}`} alt="Pairing QR code" className="size-48 rounded-lg bg-white p-1" data-testid="pair-qr" />
            <div className="min-w-0 space-y-2 text-sm">
              <p>
                Expires in {Math.min(5, Math.max(1, Math.ceil((new Date(pair.expires).getTime() - now) / 60000)))} min. Or open this address on the phone:
              </p>
              <CopyCommand command={pair.url} />
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  void cancelPairCode()
                  setPair(null)
                }}
              >
                Cancel code
              </Button>
            </div>
          </div>
        )}
      </Section>

      <Section title="Paired devices" description="Revoking a phone stops its token at once, including a live tail it has open.">
        {devices.data?.length ? (
          <ul className="divide-y border-t">
            {devices.data.map((d) => (
              <li key={d.id} className="flex items-center gap-3 px-5 py-2.5 text-sm">
                <Smartphone className="size-4 text-muted-foreground" aria-hidden />
                <div className="min-w-0 flex-1">
                  <div className="font-medium">{d.name}</div>
                  <div className="text-xs text-muted-foreground">
                    Paired {relativeTime(d.created, now)} · last seen {relativeTime(d.last_seen, now)}
                  </div>
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`Revoke ${d.name}`}
                  onClick={() =>
                    setConfirm({
                      title: `Revoke ${d.name}?`,
                      description: "Its token stops working at once. Pair it again with a new code to use it.",
                      confirmLabel: "Revoke",
                      danger: true,
                      run: async () => {
                        await revokeDevice(d.id)
                        devices.refresh()
                        audit.refresh()
                      },
                    })
                  }
                >
                  <Trash2 />
                  Revoke
                </Button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="border-t px-5 py-3 text-sm text-muted-foreground">No phone is paired.</p>
        )}
      </Section>

      <Section title="Recent remote actions" description="Pairings, revocations and every write from a phone, from remote/audit.jsonl in your data folder.">
        {audit.data?.length ? (
          <ul className="divide-y border-t" data-testid="remote-audit">
            {audit.data.map((a, i) => (
              <li key={`${a.time}-${i}`} className="flex flex-wrap items-baseline gap-x-3 px-5 py-2 text-sm">
                <span className="font-medium">{ACTION_LABEL[a.action] ?? a.action}</span>
                {a.name && <span className="text-muted-foreground">{a.name}</span>}
                {a.target && <code className="text-xs text-muted-foreground">{a.target}</code>}
                {a.result !== "ok" && <span className="text-xs text-danger-fg">{a.result}</span>}
                <span className="ml-auto text-xs text-muted-foreground">
                  {relativeTime(a.time, now)}
                  {a.client ? ` · ${a.client}` : ""}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="border-t px-5 py-3 text-sm text-muted-foreground">Nothing yet.</p>
        )}
      </Section>
      <ConfirmDialog
        request={confirm}
        onClose={() => {
          setConfirm(null)
          refreshAll()
        }}
      />
    </div>
  )
}
