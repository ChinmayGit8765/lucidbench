import { useCallback, useEffect, useRef, useState } from "react"

/** An API failure; status is 0 when the daemon could not be reached. */
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function request(path: string, init?: RequestInit): Promise<Response> {
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
    throw new ApiError(0, "Cannot reach the Lucidbench daemon.")
  }
  if (!res.ok) {
    const text = (await res.text()).trim()
    throw new ApiError(res.status, text || res.statusText)
  }
  return res
}

export async function getJSON<T>(path: string): Promise<T> {
  return (await request(path)).json() as Promise<T>
}

export interface Polled<T> {
  data: T | null
  error: ApiError | null
  loading: boolean
  refresh: () => void
}

/** Fetches JSON now and every intervalMs; keeps the last good data on errors. */
export function usePoll<T>(path: string, intervalMs = 10000): Polled<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const [loading, setLoading] = useState(true)
  const alive = useRef(true)

  const load = useCallback(async () => {
    try {
      const body = await getJSON<T>(path)
      if (!alive.current) return
      setData(body)
      setError(null)
    } catch (e) {
      if (!alive.current) return
      setError(e instanceof ApiError ? e : new ApiError(0, String(e)))
    } finally {
      if (alive.current) setLoading(false)
    }
  }, [path])

  useEffect(() => {
    alive.current = true
    void load()
    const id = setInterval(() => void load(), intervalMs)
    return () => {
      alive.current = false
      clearInterval(id)
    }
  }, [load, intervalMs])

  return { data, error, loading, refresh: () => void load() }
}
