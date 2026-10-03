# Third-party notices

## Trademarks

Claude and Anthropic are trademarks of Anthropic, PBC. OpenAI, ChatGPT and Codex are trademarks of OpenAI. Grok and xAI are trademarks of xAI. Cursor is a trademark of Anysphere, Inc.

Lucidbench is an independent project and is not affiliated with, sponsored by or endorsed by any of these companies. Their names and marks appear in the web UI only to identify the service each account belongs to (nominative use). They are shown as simple monochrome marks and are not altered to suggest endorsement.

## Provider marks

| Mark | Source | Licence |
|---|---|---|
| Claude | [simple-icons](https://github.com/simple-icons/simple-icons) `siClaude` | CC0-1.0 (icon data); the mark remains a trademark of its owner |
| Cursor | [simple-icons](https://github.com/simple-icons/simple-icons) `siCursor` | CC0-1.0 (icon data); the mark remains a trademark of its owner |
| OpenAI (Codex) | simplified drawing in `web/src/components/ProviderMark.tsx` (simple-icons has no OpenAI icon) | Not licensed by this project: a simplified rendering of a third-party trademark, shown only to identify the service |
| xAI (Grok) | simplified drawing in `web/src/components/ProviderMark.tsx` (simple-icons has no xAI icon) | Not licensed by this project: a simplified rendering of a third-party trademark, shown only to identify the service |

## Bundled fonts and libraries

| Package | Licence |
|---|---|
| Geist and Geist Mono, via `@fontsource-variable/geist` and `@fontsource-variable/geist-mono`. Copyright 2024 The Geist Project Authors (https://github.com/vercel/geist-font) | SIL Open Font License 1.1, full text in [`web/public/licenses/OFL-1.1-Geist.txt`](web/public/licenses/OFL-1.1-Geist.txt), which ships inside the web UI at `/licenses/OFL-1.1-Geist.txt` |
| `simple-icons` | CC0-1.0 |
| `sonner` | MIT |
| `lucide-react` | ISC |
| `react`, `react-dom` | MIT |
| `@radix-ui/react-slot`, `clsx`, `tailwind-merge`, `tw-animate-css`, Tailwind CSS | MIT |
| `class-variance-authority` | Apache-2.0 |

Build-time only (not shipped): Vite (MIT), TypeScript (Apache-2.0), lightningcss (MPL-2.0).

## Go modules

Compiled into the `lucidd` and `lucid` binaries (direct dependencies; transitive ones are permissively licensed, see `go.sum`):

| Module | Licence |
|---|---|
| `sigs.k8s.io/kind` | Apache-2.0 |
| `k8s.io/client-go`, `k8s.io/api`, `k8s.io/apimachinery` | Apache-2.0 |
| `go.yaml.in/yaml/v3` | MIT and Apache-2.0 |

## Agent image

`images/agent/Dockerfile` installs the official Claude Code, Codex and Grok command-line tools from npm **when you build the image**. Those tools are not part of Lucidbench, are not covered by its Apache-2.0 licence, and are used under their owners' own licences and terms. This repository contains none of their code. If you publish a built agent image, you are responsible for complying with those terms.

The Lucidbench logo is original to this project and is covered by the repository licence (Apache-2.0).
