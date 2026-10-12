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
        description="Everything beyond the core loop. Add one and it appears under Extensions in the sidebar; each checks what it needs on this machine before you add it."
      />
      <Gallery focus={subpath[0]} />
    </div>
  )
}
