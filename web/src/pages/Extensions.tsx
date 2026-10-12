import { Blocks } from "lucide-react"

import { PageHeader } from "@/components/Shell"
import type { ModulePageProps } from "@/modules/types"
import { Extensions as Gallery } from "@/pages/settings/Extensions"

/** /extensions lists every extension; /extensions/<id> highlights one. */
export default function Extensions({ subpath }: ModulePageProps) {
  return (
    <div className="space-y-6">
      <PageHeader
        icon={<Blocks />}
        title="Extensions"
        description="Everything beyond the core loop. Each checks what it needs before you add it to the sidebar."
      />
      <Gallery focus={subpath[0]} />
    </div>
  )
}
