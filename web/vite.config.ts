import { writeFileSync } from "node:fs"
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// Vite empties dist/ on build; restore the placeholder Go's go:embed needs.
const keepDist = {
  name: "keep-dist-placeholder",
  closeBundle() {
    writeFileSync(path.resolve(import.meta.dirname, "dist/.gitkeep"), "")
  },
}

export default defineConfig({
  plugins: [react(), tailwindcss(), keepDist],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "./src") } },
  server: { proxy: { "/api": "http://localhost:7420" } },
})
