import type { Tone } from "@/components/ui/badge"

/**
 * The one status palette. Every page colours these the same way, whatever
 * it calls them: a session, a council, an idea's stage, a CI run, a job, a
 * project's need or a board column.
 *
 * - waiting: amber, it is your move (a reply, an approval, a review);
 * - blocked: amber too, something it needs is missing and only you can
 *   give it;
 * - running: blue, something is working on it now;
 * - merged: green, it landed (done, passed, completed);
 * - failed: red, it stopped on an error.
 *
 * Anything else that is simply over or not started (stopped, skipped, a
 * closed PR, queued) is neutral.
 */
export const STATUS_TONE = {
  waiting: "warning",
  blocked: "warning",
  running: "info",
  merged: "success",
  failed: "danger",
  idle: "neutral",
} as const satisfies Record<string, Tone>

/** The CSS colour of a tone, for dots and bars that are not pills. */
export const toneColor = (t: Tone) => `var(--${t})`
