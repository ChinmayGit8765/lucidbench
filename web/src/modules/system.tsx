import { lazy } from "react"
import { Activity, Play, RefreshCw } from "lucide-react"
import { toast } from "sonner"

import type { Command } from "@/components/CommandPalette"
import { refreshAll } from "@/lib/api"
import { runHelloJob } from "@/lib/jobs"
import type { ModuleDef } from "@/modules/types"

function useSystemCommands(): Command[] {
  return [
    {
      id: "hello",
      label: "Run hello job",
      group: "Actions",
      icon: Play,
      hint: "local cluster",
      keywords: "job kubernetes test",
      run: () => void runHelloJob(),
    },
    {
      id: "refresh",
      label: "Refresh data",
      group: "Actions",
      icon: RefreshCw,
      keywords: "reload update",
      run: () => {
        refreshAll()
        toast.success("Refreshing")
      },
    },
  ]
}

/**
 * Daemon and cluster health with a quick view of the hello jobs. The full
 * cluster (pods, all jobs, events, logs) lives in the Kubernetes extension;
 * this stays core so health is visible even when that extension is removed.
 */
export const system: ModuleDef = {
  id: "system",
  title: "System",
  icon: Activity,
  route: "/system",
  section: "infrastructure",
  kind: "core",
  order: 0,
  defaultEnabled: true,
  description: "Daemon, cluster and jobs health.",
  keywords: "health daemon cluster jobs kubeconfig",
  component: lazy(() => import("@/pages/System")),
  useCommands: useSystemCommands,
}
