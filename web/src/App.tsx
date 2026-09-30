import { useEffect, useState } from "react"
import { Toaster } from "sonner"

import { Header, Sidebar, useSidebar, type Page } from "@/components/Shell"
import { useHealth } from "@/lib/health"
import { useTheme } from "@/lib/theme"
import Accounts from "@/pages/Accounts"
import System from "@/pages/System"

const pageFromPath = (): Page => (location.pathname === "/system" ? "system" : "accounts")

export default function App() {
  const [page, setPageState] = useState<Page>(pageFromPath)
  const setPage = (p: Page) => {
    // Keep ?theme= so a forced theme survives navigation.
    history.pushState(null, "", `/${p}${location.search}`)
    setPageState(p)
  }
  useEffect(() => {
    const onPop = () => setPageState(pageFromPath())
    window.addEventListener("popstate", onPop)
    return () => window.removeEventListener("popstate", onPop)
  }, [])
  useEffect(() => {
    document.title = `${page === "system" ? "System" : "Accounts"} · Lucidbench`
  }, [page])

  const health = useHealth()
  const theme = useTheme()
  const sidebar = useSidebar()

  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar
        page={page}
        onNavigate={setPage}
        rail={sidebar.rail}
        wide={sidebar.wide}
        onToggle={sidebar.toggle}
        version={health?.version}
      />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header page={page} health={health} theme={theme.pref} onTheme={theme.setPref} />
        <main className="flex-1 overflow-auto">
          <div key={page} className="mx-auto max-w-5xl px-5 py-7 animate-in fade-in-0 slide-in-from-bottom-1 duration-300 md:px-8">
            {page === "accounts" ? <Accounts /> : <System health={health} />}
          </div>
        </main>
      </div>
      <Toaster
        theme={theme.resolved}
        position="bottom-right"
        toastOptions={{ className: "!font-sans !text-sm !rounded-lg !border-border-strong !shadow-pop" }}
        style={
          {
            "--normal-bg": "var(--elevated)",
            "--normal-text": "var(--foreground)",
            "--normal-border": "var(--border-strong)",
          } as React.CSSProperties
        }
      />
    </div>
  )
}
