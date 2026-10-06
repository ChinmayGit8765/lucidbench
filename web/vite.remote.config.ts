import path from "node:path"
import { defineConfig } from "vite"

// The phone remote's page (web/remote), a second build into dist/r after the
// desktop app's. It shares nothing with the desktop bundle: lucidd's remote
// listener serves dist/r and nothing else, at /r.
export default defineConfig({
  root: path.resolve(import.meta.dirname, "remote"),
  base: "/r/",
  publicDir: path.resolve(import.meta.dirname, "remote/public"),
  build: {
    outDir: path.resolve(import.meta.dirname, "dist/r"),
    emptyOutDir: true,
    assetsInlineLimit: 0,
  },
})
