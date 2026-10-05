import { Cloud } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** Extension on the roadmap: listed in the gallery, not addable yet. */
export const cloud: ModuleDef = {
  id: "cloud",
  title: "Cloud",
  icon: Cloud,
  route: "/cloud",
  section: "infrastructure",
  kind: "extension",
  order: 10,
  defaultEnabled: false,
  status: "soon",
  category: "cloud",
  description: "GCP, Cloudflare, Vercel, AWS and Azure deployments in one place.",
  requires: { clis: ["gcloud|wrangler|vercel|aws|az"] },
  keywords: "gcp cloudflare vercel aws azure deploy",
}
