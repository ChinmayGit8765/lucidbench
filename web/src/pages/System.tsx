import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import type { Health } from "@/lib/health"

export default function System({ health }: { health: Health | null }) {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">System</h1>
      <Card>
        <CardHeader>
          <CardTitle>Daemon</CardTitle>
          <CardDescription>lucidd, the Lucidbench engine.</CardDescription>
        </CardHeader>
        <CardContent className="grid grid-cols-[8rem_1fr] gap-y-2 text-sm">
          <span className="text-muted-foreground">Status</span>
          <span>{health?.status ?? "unreachable"}</span>
          <span className="text-muted-foreground">Version</span>
          <span>{health?.version ?? "-"}</span>
        </CardContent>
      </Card>
    </div>
  )
}
