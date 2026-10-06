# Deploying the website

The site in `site/` is static. CI builds it on every push and pull request (the `site` job) but
never deploys it. Deploying is a manual step for the repository owner.

Any static host works. The settings are the same everywhere:

| Setting | Value |
|---|---|
| Root directory | `site` |
| Install command | `npm ci` |
| Build command | `npm run build` |
| Output directory | `dist` (that is, `site/dist`) |
| Node version | 22 (set `NODE_VERSION=22` where the host asks) |

## Cloudflare Pages

Create a Pages project from the repository (or upload `site/dist` with Wrangler) with the values
above. Production branch: `main`. No environment variables are needed.

## GitHub Pages

Project pages are served from `/<repo>/`, so build with the base path:

```sh
cd site
SITE_BASE=/lucidbench/ npm run build
```

and publish `site/dist`. A custom domain at the root needs no `SITE_BASE`.

## Netlify

Base directory `site`, build command `npm run build`, publish directory `site/dist`.

## Before you publish

- `npm run check-links` in `site/` fails the build on a broken internal link.
- The Download page points at the repository's GitHub Releases and expects each release to carry
  `SHA256SUMS.txt`, as the release workflow produces.
- The site is rebuilt from `CHANGELOG.md`, so update the changelog first.
