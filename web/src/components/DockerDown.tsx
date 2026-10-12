import { useState } from "react"
import { Container, RotateCw } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { EmptyState } from "@/components/ui/states"
import { recheckHealth } from "@/lib/health"

/**
 * The one "Docker isn't running" state, for every page that needs the
 * engine. The page behind it is not mounted, so nothing polls a docker
 * endpoint (no 503s, no console errors) until the health check says the
 * engine answers; then the page opens by itself.
 */
export function DockerDown({ what = "This page" }: { what?: string }) {
  const [checking, setChecking] = useState(false)
  const [still, setStill] = useState(false)
  const retry = async () => {
    setChecking(true)
    await recheckHealth()
    setChecking(false)
    setStill(true)
  }
  return (
    <Card className="border-dashed" data-testid="docker-down">
      <EmptyState
        icon={<Container />}
        title="Docker isn't running"
        description={`${what} needs the Docker engine. Start Docker Desktop (or the docker service) and it opens by itself; Lucidbench checks every few seconds.`}
      >
        <Button variant="secondary" onClick={() => void retry()} disabled={checking}>
          <RotateCw className={checking ? "animate-spin" : undefined} /> {checking ? "Checking…" : "Retry"}
        </Button>
      </EmptyState>
      {still && !checking && (
        <p className="-mt-6 pb-6 text-center text-xs text-subtle-foreground" role="status">
          Still not answering. The engine can take a minute to start.
        </p>
      )}
    </Card>
  )
}
