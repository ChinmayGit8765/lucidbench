import { cpSync, writeFileSync } from "node:fs"
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

// Excalidraw loads its drawing fonts at run time. Without a local copy it falls
// back to a public CDN, so the app ships them (all but the 13 MB CJK set,
// which stays a lazy CDN fetch the first time CJK text is drawn).
const excalidrawFonts = {
  name: "excalidraw-fonts",
  closeBundle() {
    cpSync(
      path.resolve(import.meta.dirname, "node_modules/@excalidraw/excalidraw/dist/prod/fonts"),
      path.resolve(import.meta.dirname, "dist/excalidraw/fonts"),
      { recursive: true, filter: (src) => !/[\\/]Xiaolai([\\/]|$)/.test(src) },
    )
  },
}

export default defineConfig({
  plugins: [react(), tailwindcss(), keepDist, excalidrawFonts],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "./src") } },
  server: { proxy: { "/api": "http://127.0.0.1:7420" } },
})
