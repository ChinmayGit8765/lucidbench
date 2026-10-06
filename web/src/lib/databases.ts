import { Database, FileJson, Layers, Table2, type LucideIcon } from "lucide-react"

import { sendJSON, usePoll, type Polled } from "@/lib/api"
import { usePrefs } from "@/lib/prefs"

/* Mirrors internal/databases: keep these in step with the Go structs. */

export type Engine = "postgres" | "mysql" | "redis" | "mongo"

export interface DiscoveredDb {
  id: string
  name: string
  engine: Engine
  image: string
  state: string
  running: boolean
  host: string
  /** 0 when nothing is published, or when it is stopped and docker picks the port at the next start. */
  port: number
  database?: string
  user?: string
  saved_as?: string
  compose_project?: string
}

export interface DbHealth {
  ok: boolean
  latency_ms: number
  version?: string
  size_bytes: number
  error?: string
}

export interface SavedDb {
  id: string
  engine: Engine
  host: string
  port: number
  database?: string
  user?: string
  /** The env:NAME reference, never the value. */
  password?: string
  password_set: boolean
  readonly: boolean
  label?: string
  prod?: boolean
  has_manager: boolean
  health?: DbHealth
}

export interface ManagerSpec {
  engine: Engine
  name: string
  image: string
  license: string
  writes: boolean
}

export interface DatabasesListing {
  discovered: DiscoveredDb[]
  saved: SavedDb[]
  docker_error?: string
  managers: ManagerSpec[]
  limits: { rows: number; timeout_seconds: number }
  redis_commands: string[]
}

export interface DbColumn {
  name: string
  type?: string
  nullable?: boolean
}

export interface DbTable {
  schema?: string
  name: string
  kind: string
  rows?: number
  columns: DbColumn[]
}

export interface DbResult {
  columns: string[]
  rows: string[][]
  nulls?: boolean[][]
  truncated: boolean
  row_limit: number
  elapsed_ms: number
}

export interface ManagerStatus {
  available: boolean
  name?: string
  image?: string
  license?: string
  running: boolean
  url?: string
  idle_minutes?: number
  note?: string
}

export const ENGINE_LABEL: Record<Engine, string> = { postgres: "PostgreSQL", mysql: "MySQL / MariaDB", redis: "Redis", mongo: "MongoDB" }

/** Generic icons: no vendor art. */
export const ENGINE_ICON: Record<Engine, LucideIcon> = { postgres: Database, mysql: Table2, redis: Layers, mongo: FileJson }

export const DEFAULT_PORT: Record<Engine, number> = { postgres: 5432, mysql: 3306, redis: 6379, mongo: 27017 }

/** Each saved connection is checked on every poll, so this is not fast. */
export const DATABASES_POLL_MS = 30000

export const displayName = (c: { label?: string; id: string }) => c.label || c.id

/** Whether the Databases extension is in the sidebar; its hooks stay quiet until it is. */
export function useDatabasesAdded(): boolean {
  const { prefs } = usePrefs()
  return prefs.extensions["databases"]?.added ?? false
}

/** Discovered containers and saved connections with their health. */
export function useDatabases(enabled: boolean): Polled<DatabasesListing> {
  return usePoll<DatabasesListing>(enabled ? "/api/databases?health=1" : null, DATABASES_POLL_MS)
}

export function healthCounts(saved: SavedDb[]) {
  const up = saved.filter((s) => s.health?.ok).length
  const down = saved.filter((s) => s.health && !s.health.ok)
  return { up, down, total: saved.length }
}

export interface NewConnection {
  id: string
  engine: Engine
  host: string
  port: number
  database?: string
  user?: string
  password?: string
  readonly: boolean
  label?: string
  prod?: boolean
}

export const saveConnection = (c: NewConnection) => sendJSON<SavedDb>("/api/databases", "POST", c)
export const removeConnection = (id: string) => sendJSON<unknown>(`/api/databases/${encodeURIComponent(id)}`, "DELETE")
export const runQuery = (id: string, query: string) => sendJSON<DbResult>(`/api/databases/${encodeURIComponent(id)}/query`, "POST", { query })
export const startManager = (id: string) => sendJSON<ManagerStatus>(`/api/databases/${encodeURIComponent(id)}/manager/start`, "POST")
export const stopManager = (id: string) => sendJSON<ManagerStatus>(`/api/databases/${encodeURIComponent(id)}/manager/stop`, "POST")

/** A first query for a table, in the engine's own syntax. */
export function sampleQuery(engine: Engine, t: DbTable): string {
  switch (engine) {
    case "postgres":
      return `SELECT * FROM "${t.schema ?? "public"}"."${t.name}" LIMIT 50`
    case "mysql":
      return `SELECT * FROM \`${t.schema ? t.schema + "`.`" : ""}${t.name}\` LIMIT 50`
    case "redis":
      return t.kind === "hash" ? `HGETALL ${t.name}` : t.kind === "list" ? `LRANGE ${t.name} 0 49` : t.kind === "set" ? `SMEMBERS ${t.name}` : t.kind === "zset" ? `ZRANGE ${t.name} 0 49 WITHSCORES` : `GET ${t.name}`
    case "mongo":
      return `db.${t.name}.find({})`
  }
}

/** The query box placeholder per engine. */
export const QUERY_HINT: Record<Engine, string> = {
  postgres: "SELECT * FROM my_table LIMIT 50",
  mysql: "SELECT * FROM my_table LIMIT 50",
  redis: "SCAN 0 MATCH user:* COUNT 100",
  mongo: 'db.my_collection.find({"field": "value"})',
}

export function formatBytes(n: number): string {
  if (n < 0) return "-"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}
