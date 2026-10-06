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
| `@milkdown/kit` (the Memory editor: Milkdown core, presets and plugins), loaded only when a Memory page opens | MIT |
| ProseMirror (`prosemirror-*`, `orderedmap`, `rope-sequence`, `w3c-keyname`, `crelt`), bundled with the Memory editor | MIT |
| remark and unified (`remark-*`, `unified`, `micromark*`, `mdast-util-*`, `unist-util-*`, `vfile*`), bundled with the Memory editor | MIT |
| `@floating-ui/dom`, `nanoid`, `lodash-es`, bundled with the Memory editor | MIT |
| `@dnd-kit/core`, `@dnd-kit/sortable`, `@dnd-kit/utilities` (Boards drag and drop) | MIT |
| `mermaid` (Picture diagrams), loaded only when a diagram previews | MIT |
| Mermaid's notable bundled dependencies: `d3*` (ISC, BSD-3-Clause), `dagre-d3-es`, `cytoscape` and its layouts, `katex`, `langium`, `@mermaid-js/parser`, `khroma`, `marked`, `lodash-es` | MIT, ISC or BSD-3-Clause |
| `robust-predicates` (Mermaid, via `delaunator`) | Unlicense |
| `elkjs` (Mermaid's optional ELK layout, a separate chunk fetched only for a diagram that asks for the ELK renderer; shipped unmodified) | EPL-2.0 |
| `dompurify` (Mermaid's sanitiser, used under Apache-2.0) | MPL-2.0 OR Apache-2.0 |
| `@excalidraw/excalidraw` (Picture design canvases), loaded only when a canvas opens | MIT |
| Excalidraw's notable bundled dependencies: `roughjs`, `perfect-freehand`, `jotai`, `nanoid`, `@radix-ui/*`, `immer`, `fuzzy` | MIT |
| `pako` (Excalidraw) | MIT AND Zlib |
| `fractional-indexing` (Excalidraw) | CC0-1.0 |
| Excalidraw's drawing fonts (Excalifont, Virgil, Cascadia Code, Nunito, Lilita One, Assistant, Liberation Sans, Comic Shanns), copied into the web UI at `/excalidraw/fonts` by the build so the canvas makes no CDN request. The CJK set (Xiaolai) is not copied. | Published by the Excalidraw project under open font licences (mostly SIL OFL 1.1); the font files carry no licence text, see the Excalidraw repository |

Build-time only (not shipped): Vite (MIT), TypeScript (Apache-2.0), lightningcss (MPL-2.0).

The Memory editor's optional sanitiser is not used; `dompurify` is bundled only through Mermaid (see above). The editor's unused CodeMirror, Lezer and Vue dependencies (MIT) are not part of the built bundle.

Excalidraw's package also lists `sass`, `chokidar` and other build tools as dependencies; none of them is part of the built bundle. tldraw is not used (its licence requires a watermark or a commercial licence).

## Go modules

Compiled into the `lucidd` and `lucid` binaries (direct dependencies; transitive ones are permissively licensed, see `go.sum`):

| Module | Licence |
|---|---|
| `sigs.k8s.io/kind` | Apache-2.0 |
| `k8s.io/client-go`, `k8s.io/api`, `k8s.io/apimachinery` | Apache-2.0 |
| `go.yaml.in/yaml/v3` | MIT and Apache-2.0 |
| `github.com/BurntSushi/toml` | MIT |
| `github.com/jackc/pgx/v5` (the Databases extension's Postgres driver) | MIT |
| `github.com/go-sql-driver/mysql` (MySQL and MariaDB) | MPL-2.0, used unmodified as a library |
| `github.com/redis/go-redis/v9` (Redis) | BSD-2-Clause |
| `go.mongodb.org/mongo-driver/v2` (MongoDB) | Apache-2.0 |
| `golang.org/x/sys` and other `golang.org/x/*` modules (Copyright The Go Authors) | BSD-3-Clause |

## Images pulled at run time

The Databases extension can start a database manager as a container. The image is pulled from its
registry the first time you open it, runs on your machine bound to 127.0.0.1, and is not part of
Lucidbench or redistributed with it. Lucidbench only runs the unmodified image.

| Image | Licence |
|---|---|
| `sosedoff/pgweb` ([pgweb](https://github.com/sosedoff/pgweb), Copyright Dan Sosedoff) | MIT |
| `adminer` ([Adminer](https://www.adminer.org), Jakub Vrana) | Apache-2.0 or GPL-2.0 (the choice is the user's; Lucidbench does not link or modify it, and it is used under Apache-2.0) |

RedisInsight is not offered because it is not under a permissive licence.

## Desktop app

The Windows desktop app (`desktop/`) ships the `lucidd` binary described above and renders the UI with the system WebView2 runtime, which is not redistributed. The installer includes this file, `LICENSE` and the font licence texts.

It is built with [Tauri](https://tauri.app) 2 and Rust crates from crates.io:

| Crates | Licence |
|---|---|
| `tauri`, `tauri-plugin-single-instance`, `tauri-plugin-window-state`, `windows-sys`, `serde`, `serde_json` and most of the dependency tree | MIT or Apache-2.0 |
| `cssparser`, `cssparser-macros`, `selectors`, `dtoa-short`, `option-ext` | MPL-2.0 (used unmodified as libraries) |
| `brotli`, `brotli-decompressor`, `alloc-no-stdlib`, `alloc-stdlib` | BSD-3-Clause (brotli: BSD-3-Clause and MIT) |
| `unicode-ident` and other Unicode data crates | Unicode-3.0 (with MIT or Apache-2.0 where offered) |
| `zlib-rs`, `foldhash`, `miniz_oxide` | Zlib (miniz_oxide: MIT, Zlib or Apache-2.0) |
| several small crates | Unlicense or MIT, CC0-1.0 / MIT-0 / Apache-2.0 |

The exact set is pinned in `desktop/src-tauri/Cargo.lock`; `cargo about` or `cargo-license` lists every crate with its licence.

## Agent image

`images/agent/Dockerfile` installs the official Claude Code, Codex and Grok command-line tools from npm **when you build the image**. Those tools are not part of Lucidbench, are not covered by its Apache-2.0 licence, and are used under their owners' own licences and terms. This repository contains none of their code. If you publish a built agent image, you are responsible for complying with those terms.

The Lucidbench logo is original to this project and is covered by the repository licence (Apache-2.0).
