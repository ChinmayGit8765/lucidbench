/** The phone remote's desktop API (internal/remote): settings, pairing, devices and the audit log. */
import { sendJSON } from "@/lib/api"

export const REMOTE_PATH = "/api/remote"

export type RemoteMode = "lan" | "tailnet"

export interface RemoteSettings {
  enabled: boolean
  mode: RemoteMode
  address: string
  port: number
}

export interface RemoteStatus {
  settings: RemoteSettings
  listening: boolean
  address?: string
  url?: string
  error?: string
  tailnet?: string
}

export interface RemoteInterface {
  name: string
  address: string
  kind: "loopback" | "private" | "tailnet" | "other"
}

export interface RemoteDevice {
  id: string
  name: string
  created: string
  last_seen: string
}

export interface RemoteAudit {
  time: string
  action: string
  device?: string
  name?: string
  target?: string
  result: string
  client?: string
}

export interface PairCode {
  code: string
  url: string
  expires: string
  qr_svg: string
}

export const saveRemote = (s: RemoteSettings) => sendJSON<RemoteStatus>(`${REMOTE_PATH}/settings`, "PUT", s)
export const newPairCode = () => sendJSON<PairCode>(`${REMOTE_PATH}/pair`, "POST")
export const cancelPairCode = () => sendJSON<null>(`${REMOTE_PATH}/pair`, "DELETE")
export const revokeDevice = (id: string) => sendJSON<RemoteDevice>(`${REMOTE_PATH}/devices/${encodeURIComponent(id)}`, "DELETE")

export const KIND_LABEL: Record<RemoteInterface["kind"], string> = {
  private: "LAN",
  tailnet: "Tailscale",
  loopback: "this computer only",
  other: "other",
}

export const ACTION_LABEL: Record<string, string> = {
  enable: "Remote turned on",
  disable: "Remote turned off",
  pair: "Phone paired",
  pair_failed: "Pairing refused",
  auth_failed: "Unknown token refused",
  revoke: "Phone revoked",
  stop: "Stopped a session",
  approve: "Approved a brief",
  send_back: "Sent a brief back",
  follow_up: "Follow-up prompt",
}
