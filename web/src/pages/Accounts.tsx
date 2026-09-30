import { Plus, Users } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export default function Accounts() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Accounts</h1>
        <p className="text-sm text-muted-foreground">
          Your Claude, ChatGPT/Codex, Grok and Cursor accounts in one place.
        </p>
      </div>
      <Card className="border-dashed">
        <CardHeader className="items-center text-center">
          <div className="mb-2 flex size-12 items-center justify-center rounded-full bg-muted">
            <Users className="size-6 text-muted-foreground" />
          </div>
          <CardTitle>No accounts detected yet</CardTitle>
          <CardDescription>Account detection arrives in milestone M1.</CardDescription>
        </CardHeader>
        <CardContent className="flex justify-center">
          <Button variant="outline" disabled>
            <Plus /> Add account
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
