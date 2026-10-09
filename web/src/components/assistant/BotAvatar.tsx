import { ProviderTile } from "@/components/ProviderMark"
import { StateSprite, type SpriteState } from "@/components/StateSprite"
import type { Bot } from "@/lib/assistant"
import { cn } from "@/lib/utils"

/** A bot's face: its emoji, a theme sprite slot, or its provider's mark. */
export function BotAvatar({ bot, size = "md", className }: { bot: Pick<Bot, "avatar" | "provider" | "name">; size?: "sm" | "md" | "lg"; className?: string }) {
  const box = size === "sm" ? "size-6 text-sm" : size === "lg" ? "size-12 text-2xl" : "size-9 text-lg"
  const slot = bot.avatar?.startsWith("sprite:") ? (bot.avatar.slice(7) as SpriteState) : null
  if (slot) {
    return (
      <span className={cn("inline-flex shrink-0 items-center justify-center", box, className)}>
        <StateSprite state={slot} className={cn(box, "size-full")} label={bot.name} />
      </span>
    )
  }
  if (bot.avatar) {
    return (
      <span role="img" aria-label={bot.name} className={cn("inline-flex shrink-0 items-center justify-center rounded-xl border bg-elevated", box, className)}>
        {bot.avatar}
      </span>
    )
  }
  return <ProviderTile provider={bot.provider} size={size} className={className} />
}
