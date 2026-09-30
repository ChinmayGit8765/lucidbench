import { useEffect, useState } from "react"
import { Activity, Users } from "lucide-react"

import { cn } from "@/lib/utils"
import { useHealth } from "@/lib/health"
import Accounts from "@/pages/Accounts"
import System from "@/pages/System"

type Page = "accounts" | "system"

const nav: { id: Page; label: string; icon: typeof Users }[] = [
  { id: "accounts", label: "Accounts", icon: Users },
  { id: "system", label: "System", icon: Activity },
]

const pageFromPath = (): Page => (location.pathname === "/system" ? "system" : "accounts")

export default function App() {
  const [page, setPageState] = useState<Page>(pageFromPath)
  const setPage = (p: Page) => {
    history.pushState(null, "", `/${p}`)
    setPageState(p)
  }
  useEffect(() => {
    const onPop = () => setPageState(pageFromPath())
    window.addEventListener("popstate", onPop)
    return () => window.removeEventListener("popstate", onPop)
  }, [])
  const health = useHealth()
  const ok = health?.status === "ok"

  return (
    <div className="flex h-screen">
      <aside className="flex w-56 shrink-0 flex-col border-r bg-card/50 p-3">
        <div className="px-2 py-3 text-sm font-semibold tracking-tight">Lucidbench</div>
        <nav className="mt-2 flex flex-col gap-1">
          {nav.map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              onClick={() => setPage(id)}
              className={cn(
                "flex items-center gap-2 rounded-md px-2 py-2 text-left text-sm transition-colors",
                page === id
                  ? "bg-accent text-accent-foreground"
                  : "text-muted-foreground hover:bg-accent/60 hover:text-accent-foreground",
              )}
            >
              <Icon className="size-4" />
              {label}
            </button>
          ))}
        </nav>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center justify-between border-b px-6">
          <span className="font-semibold tracking-tight">Lucidbench</span>
          <span className="flex items-center gap-2 text-xs text-muted-foreground">
            <span className={cn("size-2 rounded-full", ok ? "bg-emerald-500" : "bg-destructive")} />
            {ok ? `daemon ok · ${health.version}` : "daemon unreachable"}
          </span>
        </header>
        <main className="flex-1 overflow-auto p-6">
          <div className="mx-auto max-w-5xl">
            {page === "accounts" ? <Accounts /> : <System health={health} />}
          </div>
        </main>
      </div>
    </div>
  )
}
