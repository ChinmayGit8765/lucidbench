# Lucidbench website

A small static site: Home, Docs, Download and Changelog. It builds to plain files in `dist/`
and can be hosted anywhere that serves static files.

```sh
cd site
npm ci
npm run build          # writes dist/
npm run check-links    # every internal link and #anchor in dist/ must resolve
npm run dev            # local preview with reload
```

Needs Node 22.12 or newer. Set `SITE_BASE=/lucidbench/` at build time for a host that serves the
site from a sub-path (for example GitHub Pages project pages). The default is `/`.

## Why Astro

- **Static output, no runtime.** Each page becomes one HTML file; the only JavaScript shipped is
  a few lines for the theme toggle. No framework runtime reaches the browser.
- **The changelog is rendered from `CHANGELOG.md` at build time**, so it can never drift from the
  repository. A plain Vite setup would need a hand-written build step for that.
- **Fonts and tokens match the app.** Geist comes from the same `@fontsource-variable` packages the
  app uses, bundled into the build (no request to a font host), and the colours are the app's own
  tokens in `src/styles/site.css`.
- **Exit cost is low.** Pages are HTML with a CSS file; moving to another generator means
  re-pasting markup.

The site has no trackers, no cookies and no external requests. Illustrations are CSS drawings,
not screenshots, so no real screen or data ever enters the repository.

## Licences

| Component | Licence |
|---|---|
| Astro (build tool) | MIT |
| marked (changelog rendering) | MIT |
| Geist and Geist Mono via `@fontsource-variable` | SIL OFL 1.1 (copy in `public/licenses/`) |
| Brand mark (`public/favicon.svg`) | Apache-2.0, as the rest of the repository |

Dependencies of the build tools are not shipped in `dist/`.
