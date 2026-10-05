import { useEffect, useRef, useState } from "react"
import { Braces, CalendarDays, Hash, List, Lock, Plus, ToggleLeft, Type, X } from "lucide-react"

import { cn } from "@/lib/utils"

/** Front matter keys shown elsewhere on the page, not as properties. */
export const RESERVED = new Set(["title", "icon", "cover"])

type Scalar = string | number | boolean

const isScalar = (v: unknown): v is Scalar => typeof v === "string" || typeof v === "number" || typeof v === "boolean"
const isDate = (v: unknown) => typeof v === "string" && /^\d{4}-\d{2}-\d{2}([T ][\d:.]+Z?)?$/.test(v)

function kindIcon(v: unknown) {
  if (typeof v === "boolean") return ToggleLeft
  if (typeof v === "number") return Hash
  if (isDate(v)) return CalendarDays
  if (Array.isArray(v)) return List
  if (isScalar(v) || v === null || v === undefined) return Type
  return Braces
}

/**
 * Front matter as a Notion-style properties table. Strings, numbers,
 * booleans and lists of scalars are editable in place; anything nested is
 * shown as JSON and kept as it is.
 */
export function Properties({
  front,
  onChange,
}: {
  front: Record<string, unknown>
  onChange: (next: Record<string, unknown>) => void
}) {
  const keys = Object.keys(front).filter((k) => !RESERVED.has(k))
  const [adding, setAdding] = useState(false)
  const set = (k: string, v: unknown) => onChange({ ...front, [k]: v })
  const remove = (k: string) => {
    const next = { ...front }
    delete next[k]
    onChange(next)
  }
  if (keys.length === 0 && !adding) {
    return (
      <button
        type="button"
        onClick={() => setAdding(true)}
        className="flex h-7 items-center gap-1.5 rounded-md px-1.5 text-sm text-subtle-foreground transition-colors hover:bg-accent hover:text-foreground"
      >
        <Plus className="size-3.5" /> Add a property
      </button>
    )
  }
  return (
    <div className="text-sm" role="table" aria-label="Page properties">
      {keys.map((k) => {
        const v = front[k]
        const Icon = k === "confidential" ? Lock : kindIcon(v)
        return (
          <div key={k} role="row" className="group flex min-h-8 items-start gap-2 rounded-md hover:bg-accent/40">
            <div role="rowheader" className="flex w-40 shrink-0 items-center gap-2 px-1.5 py-1.5 text-muted-foreground">
              <Icon className="size-3.5 shrink-0 text-subtle-foreground" />
              <span className="truncate" title={k}>
                {k}
              </span>
            </div>
            <div role="cell" className="min-w-0 flex-1 py-0.5">
              <Value value={v} onChange={(nv) => set(k, nv)} label={k} />
            </div>
            <button
              type="button"
              aria-label={`Remove property ${k}`}
              title="Remove property"
              onClick={() => remove(k)}
              className="mr-1 mt-1 flex size-6 shrink-0 items-center justify-center rounded text-subtle-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100"
            >
              <X className="size-3.5" />
            </button>
          </div>
        )
      })}
      {adding ? (
        <NewProperty
          taken={new Set(Object.keys(front))}
          onDone={(name) => {
            setAdding(false)
            if (name) set(name, "")
          }}
        />
      ) : (
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="mt-0.5 flex h-7 items-center gap-1.5 rounded-md px-1.5 text-sm text-subtle-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <Plus className="size-3.5" /> Add a property
        </button>
      )}
    </div>
  )
}

function Value({ value, onChange, label }: { value: unknown; onChange: (v: unknown) => void; label: string }) {
  if (typeof value === "boolean") {
    return (
      <label className="flex h-7 cursor-pointer items-center gap-2 px-1.5">
        <input
          type="checkbox"
          checked={value}
          onChange={(e) => onChange(e.target.checked)}
          className="size-4 cursor-pointer accent-[var(--brand)]"
          aria-label={label}
        />
        {label === "confidential" && value && <span className="text-xs text-warning-fg">Never sent to AI providers</span>}
      </label>
    )
  }
  if (Array.isArray(value) && value.every(isScalar)) {
    return <ListValue items={value as Scalar[]} onChange={onChange} label={label} />
  }
  if (isScalar(value) || value === null || value === undefined) {
    return <TextValue value={value ?? ""} onChange={onChange} label={label} />
  }
  return (
    <code className="block truncate px-1.5 py-1.5 font-mono text-xs text-muted-foreground" title="Nested values are kept as they are; edit them in the file.">
      {JSON.stringify(value)}
    </code>
  )
}

function TextValue({ value, onChange, label }: { value: Scalar; onChange: (v: unknown) => void; label: string }) {
  const [v, setV] = useState(String(value))
  useEffect(() => setV(String(value)), [value])
  const commit = () => {
    if (v === String(value)) return
    // Numbers stay numbers when the text still reads as one.
    onChange(typeof value === "number" && v.trim() !== "" && !Number.isNaN(Number(v)) ? Number(v) : v)
  }
  return (
    <input
      value={v}
      aria-label={label}
      placeholder="Empty"
      onChange={(e) => setV(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => e.key === "Enter" && (e.currentTarget as HTMLInputElement).blur()}
      className={cn(
        "h-7 w-full rounded-md bg-transparent px-1.5 outline-none transition-colors placeholder:text-subtle-foreground hover:bg-accent/60 focus:bg-background focus:ring-1 focus:ring-ring/40",
        typeof value === "number" && "tabular-nums",
      )}
    />
  )
}

function ListValue({ items, onChange, label }: { items: Scalar[]; onChange: (v: unknown) => void; label: string }) {
  const [editing, setEditing] = useState(false)
  const [v, setV] = useState(items.join(", "))
  useEffect(() => setV(items.join(", ")), [items])
  if (editing) {
    return (
      <input
        autoFocus
        value={v}
        aria-label={label}
        onChange={(e) => setV(e.target.value)}
        onBlur={() => {
          setEditing(false)
          const next = v.split(",").map((s) => s.trim()).filter(Boolean)
          if (next.join(",") !== items.map(String).join(",")) onChange(next)
        }}
        onKeyDown={(e) => e.key === "Enter" && (e.currentTarget as HTMLInputElement).blur()}
        className="h-7 w-full rounded-md bg-background px-1.5 outline-none ring-1 ring-ring/40"
      />
    )
  }
  return (
    <button type="button" onClick={() => setEditing(true)} className="flex min-h-7 w-full flex-wrap items-center gap-1 rounded-md px-1.5 py-1 text-left hover:bg-accent/60">
      {items.length === 0 ? (
        <span className="text-subtle-foreground">Empty</span>
      ) : (
        items.map((x, i) => (
          <span key={i} className="rounded bg-muted px-1.5 py-px text-xs text-foreground">
            {String(x)}
          </span>
        ))
      )}
    </button>
  )
}

function NewProperty({ taken, onDone }: { taken: Set<string>; onDone: (name: string) => void }) {
  const [v, setV] = useState("")
  const done = useRef(false)
  const finish = (name: string) => {
    if (done.current) return
    done.current = true
    onDone(name)
  }
  const name = v.trim().replace(/\s+/g, "_")
  const bad = name !== "" && (taken.has(name) || RESERVED.has(name))
  return (
    <div className="flex items-center gap-2 px-1.5 py-1">
      <Type className="size-3.5 text-subtle-foreground" />
      <input
        autoFocus
        value={v}
        placeholder="Property name"
        aria-label="Property name"
        onChange={(e) => setV(e.target.value)}
        onBlur={() => finish(bad ? "" : name)}
        onKeyDown={(e) => {
          if (e.key === "Enter") finish(bad ? "" : name)
          if (e.key === "Escape") finish("")
        }}
        className="h-7 w-48 rounded-md bg-background px-1.5 outline-none ring-1 ring-ring/40"
      />
      {bad && <span className="text-xs text-danger-fg">That name is taken</span>}
    </div>
  )
}
