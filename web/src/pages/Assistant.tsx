import { useEffect, useMemo, useRef, useState } from "react"
import { Bot as BotIcon, MessageCircle, Plus, SendHorizontal, ShieldAlert, Sparkles, Trash2, WandSparkles } from "lucide-react"
import { toast } from "sonner"

import { BotAvatar } from "@/components/assistant/BotAvatar"
import { BraindumpParse } from "@/components/assistant/BraindumpParse"
import { ProposalCard, useApplyProposals, type Outcome } from "@/components/assistant/Proposals"
import { ProviderMark } from "@/components/ProviderMark"
import { PageHeader } from "@/components/Shell"
import { StateSprite } from "@/components/StateSprite"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { EmptyState } from "@/components/ui/states"
import { Tabs } from "@/components/ui/tabs"
import { errorMessage, getJSON, refreshAll, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  ASSISTANT_PROVIDERS,
  BOTS_PATH,
  conversationPath,
  CONVERSATIONS_PATH,
  DEFAULT_MODEL,
  deleteConversation,
  recordOutcome,
  sendTurn,
  type Bot,
  type Conversation,
  type ConversationSummary,
  type Message,
} from "@/lib/assistant"
import type { ProjectList } from "@/lib/projects"
import { relativeTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import { formatCost } from "@/lib/work"
import type { ModulePageProps } from "@/modules/types"
import { Bots } from "@/pages/assistant/Bots"

/**
 * /assistant is the chat (/assistant/c/<id> one conversation, /assistant/bot/<id>
 * a new one with a bot), /assistant/bots the bots, /assistant/braindump the parser.
 */
export default function Assistant({ subpath }: ModulePageProps) {
  const { open } = useApp()
  const tab = subpath[0] === "bots" ? "bots" : subpath[0] === "braindump" ? "braindump" : "chat"
  return (
    <div className="space-y-5">
      <PageHeader
        icon={<MessageCircle />}
        title="Assistant"
        description="Talk to Claude, Codex, Grok or one of your bots. It answers with no tools and may propose actions; nothing happens until you click Apply."
      />
      <Tabs
        label="Assistant"
        value={tab}
        onChange={(id) => open("assistant", id === "chat" ? [] : [id])}
        items={[
          { id: "chat", label: "Chat", icon: MessageCircle },
          { id: "bots", label: "Bots", icon: BotIcon },
          { id: "braindump", label: "Parse a braindump", icon: WandSparkles },
        ]}
      />
      {tab === "bots" ? (
        <Bots />
      ) : tab === "braindump" ? (
        <Card className="p-5">
          <BraindumpParse />
        </Card>
      ) : (
        <Chat key={subpath.join("/")} conversationId={subpath[0] === "c" ? subpath[1] : undefined} botId={subpath[0] === "bot" ? subpath[1] : undefined} />
      )}
    </div>
  )
}

function Chat({ conversationId, botId }: { conversationId?: string; botId?: string }) {
  const { open, navigate } = useApp()
  const now = useNow(30000)
  const list = usePoll<ConversationSummary[]>(CONVERSATIONS_PATH, 15000)
  const bots = usePoll<Bot[]>(BOTS_PATH, 30000)
  const projects = usePoll<ProjectList>("/api/projects", 30000)
  const [conv, setConv] = useState<Conversation | null>(null)
  const [loadErr, setLoadErr] = useState<string | null>(null)
  const [talkTo, setTalkTo] = useState(botId ? `bot:${botId}` : "claude")
  const [model, setModel] = useState("")
  const [project, setProject] = useState("")
  const [text, setText] = useState("")
  const [sending, setSending] = useState(false)
  const end = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!conversationId) return
    getJSON<Conversation>(conversationPath(conversationId))
      .then((c) => {
        setConv(c)
        if (c.bot) setTalkTo(`bot:${c.bot}`)
        if (c.project) setProject(c.project)
      })
      .catch((e) => setLoadErr(errorMessage(e)))
  }, [conversationId])
  useEffect(() => end.current?.scrollIntoView({ block: "end" }), [conv?.messages.length, sending])

  const bot = talkTo.startsWith("bot:") ? bots.data?.find((b) => b.id === talkTo.slice(4)) : undefined
  const provider = bot?.provider ?? talkTo
  const ps = projects.data?.projects ?? []
  const confidential = ps.find((p) => p.id === project)?.visibility === "confidential"
  // A bot's conversation stays with that bot.
  const locked = !!conv?.bot

  const send = async () => {
    const message = text.trim()
    if (!message || sending) return
    setSending(true)
    try {
      const res = await sendTurn({
        conversation: conv?.id,
        message,
        bot: bot?.id,
        provider: bot ? undefined : provider,
        model: bot ? undefined : model.trim() || undefined,
        project: project || undefined,
      })
      setText("")
      setConv(res.conversation)
      list.refresh()
      if (!conv) navigate(`/assistant/c/${encodeURIComponent(res.conversation.id)}`)
    } catch (e) {
      toast.error("The assistant could not answer", { description: errorMessage(e) })
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="grid gap-4 @4xl:grid-cols-[15rem_1fr]">
      <aside className="space-y-2" aria-label="Conversations">
        <Button variant="secondary" className="w-full" onClick={() => open("assistant")} data-testid="assistant-new-chat">
          <Plus /> New chat
        </Button>
        <ul className="space-y-1">
          {(list.data ?? []).map((c) => (
            <li key={c.id}>
              <button
                onClick={() => open("assistant", ["c", c.id])}
                className={cn(
                  "w-full rounded-md px-2.5 py-1.5 text-left text-sm transition-colors hover:bg-accent/60",
                  c.id === conv?.id && "bg-brand-soft text-brand-fg",
                )}
              >
                <div className="truncate font-medium">{c.title || "Untitled"}</div>
                <div className="flex items-center gap-1.5 text-2xs text-subtle-foreground">
                  {relativeTime(c.updated, now)}
                  {c.pending > 0 && <span className="rounded-full bg-info-soft px-1.5 text-info-fg">{c.pending} to review</span>}
                </div>
              </button>
            </li>
          ))}
        </ul>
      </aside>

      <Card className="flex min-h-[32rem] flex-col">
        <div className="flex-1 space-y-4 overflow-auto p-5" data-testid="assistant-thread">
          {loadErr && <p role="alert" className="text-sm text-danger-fg">{loadErr}</p>}
          {!conv?.messages.length && !sending && (
            <EmptyState
              icon={<Sparkles />}
              title={bot ? `Ask ${bot.name}` : "Ask Lucid"}
              description="Ask what to do next, or say what to make: a card, a project, an idea, a page, a council or a Work session. Each proposal waits for your Apply."
            />
          )}
          {conv?.messages.map((m, i) => <MessageView key={i} m={m} index={i} conv={conv} onChange={setConv} bots={bots.data ?? []} />)}
          {sending && (
            <div className="flex items-center gap-3 text-sm text-muted-foreground" data-testid="assistant-thinking">
              <StateSprite state="thinking" className="size-10" /> {bot ? bot.name : "Lucid"} is thinking…
            </div>
          )}
          <div ref={end} />
        </div>

        <div className="space-y-2 border-t p-4">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <label className="flex items-center gap-1.5">
              <span className="text-xs text-subtle-foreground">Talk to</span>
              <select
                aria-label="Talk to"
                value={talkTo}
                disabled={locked}
                onChange={(e) => setTalkTo(e.target.value)}
                className="h-8 rounded-md border bg-background/50 px-2"
                data-testid="assistant-talk-to"
              >
                {ASSISTANT_PROVIDERS.map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
                {(bots.data ?? []).map((b) => (
                  <option key={b.id} value={`bot:${b.id}`}>
                    Bot: {b.name}
                  </option>
                ))}
              </select>
            </label>
            {!bot && (
              <input
                aria-label="Model"
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder={DEFAULT_MODEL[provider] || "default model"}
                className="h-8 w-32 rounded-md border bg-background/50 px-2"
              />
            )}
            <label className="flex items-center gap-1.5">
              <span className="text-xs text-subtle-foreground">About</span>
              <select
                aria-label="Project"
                value={project}
                disabled={!!conv}
                onChange={(e) => setProject(e.target.value)}
                className="h-8 rounded-md border bg-background/50 px-2"
              >
                <option value="">No project</option>
                {ps.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.visibility === "confidential" ? `${p.id} (confidential)` : p.name}
                  </option>
                ))}
              </select>
            </label>
            {bot && (
              <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <ProviderMark provider={bot.provider} /> {bot.model || "default model"}
              </span>
            )}
          </div>
          {confidential && (
            <p className="flex items-center gap-1.5 text-xs text-danger-fg">
              <ShieldAlert className="size-3.5" /> This project is confidential: the assistant never sends it to a provider, so it will refuse.
            </p>
          )}
          <div className="flex items-end gap-2">
            <textarea
              aria-label="Message"
              value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault()
                  void send()
                }
              }}
              rows={2}
              placeholder={bot ? `Message ${bot.name}…` : "Ask Lucid…"}
              className="min-h-[2.75rem] flex-1 resize-y rounded-md border bg-background/50 p-2.5 text-sm outline-none focus:border-ring"
              data-testid="assistant-input"
            />
            <Button onClick={() => void send()} disabled={sending || !text.trim()} data-testid="assistant-send">
              <SendHorizontal /> Send
            </Button>
          </div>
          <p className="text-2xs text-subtle-foreground">Each message is one run of the CLI on your own account, with no tools. The model sees project ids, names and kinds; a confidential project shows as its id only.</p>
          {conv && (
            <div className="flex justify-end">
              <Button
                size="sm"
                variant="ghost"
                onClick={async () => {
                  try {
                    await deleteConversation(conv.id)
                    list.refresh()
                    open("assistant")
                  } catch (e) {
                    toast.error("Could not delete the conversation", { description: errorMessage(e) })
                  }
                }}
              >
                <Trash2 /> Delete conversation
              </Button>
            </div>
          )}
        </div>
      </Card>
    </div>
  )
}

function MessageView({ m, index, conv, onChange, bots }: { m: Message; index: number; conv: Conversation; onChange: (c: Conversation) => void; bots: Bot[] }) {
  const { open } = useApp()
  const { apply, dialogs } = useApplyProposals(conv.bot)
  const [busy, setBusy] = useState<number | null>(null)
  const bot = useMemo(() => bots.find((b) => b.id === m.bot), [bots, m.bot])
  const record = async (k: number, o: Outcome) => {
    try {
      onChange(await recordOutcome(conv.id, index, k, o.status, o.note))
      if (o.status === "applied") {
        refreshAll()
        const p = m.proposals?.[k]
        const id = (o.answer as { id?: string } | null)?.id
        if (p?.action === "start_work" && id) toast.success("Session started", { action: { label: "Open", onClick: () => open("work", [id]) } })
        else if (p?.action === "start_council" && id) toast.success("Council started", { action: { label: "Open", onClick: () => open("council", [id]) } })
        else toast.success(`Applied: ${p?.summary ?? ""}`)
      }
    } catch (e) {
      toast.error("Could not record the outcome", { description: errorMessage(e) })
    }
  }
  if (m.role === "user") {
    return (
      <div className="flex justify-end" data-testid="assistant-message" data-role="user">
        <div className="max-w-[80%] whitespace-pre-wrap rounded-xl rounded-br-sm bg-brand-soft px-3.5 py-2 text-sm">{m.text}</div>
      </div>
    )
  }
  return (
    <div className="flex items-start gap-3" data-testid="assistant-message" data-role="assistant">
      {bot ? <BotAvatar bot={bot} size="sm" /> : <StateSprite state={m.error ? "failure" : "idle"} className="size-8" />}
      <div className="min-w-0 flex-1 space-y-2">
        {m.text && <div className="whitespace-pre-wrap text-sm leading-relaxed">{m.text}</div>}
        {m.error && <p role="alert" className="rounded-md border border-danger/30 bg-danger-soft p-2.5 text-sm">{m.error}</p>}
        {m.note && <p className="text-xs text-warning-fg">{m.note}</p>}
        {(m.proposals ?? []).map((p, k) => (
          <ProposalCard
            key={k}
            p={p}
            busy={busy === k}
            onApply={async () => {
              setBusy(k)
              try {
                await apply(p, (o) => void record(k, o))
              } finally {
                setBusy(null)
              }
            }}
            onSkip={() => void record(k, { status: "skipped" })}
          />
        ))}
        <div className="flex items-center gap-1.5 text-2xs text-subtle-foreground">
          {m.provider && <ProviderMark provider={m.provider} className="size-3" />}
          {[bot?.name, m.provider, m.model, m.usage ? formatCost(m.usage.cost_usd) : null].filter(Boolean).join(" · ")}
        </div>
      </div>
      {dialogs}
    </div>
  )
}
