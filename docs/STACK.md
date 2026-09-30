# Stack decision

## Use case

An open-source AI workspace that runs identically as a desktop app, in a container and on Kubernetes, with a polished chat and agent UI and a job runner that drives Kubernetes.

## Criteria and scores

Each candidate is scored 0-5 per criterion (fractions allowed); the score is multiplied by the weight. The maximum total is 100.

| Criterion | Weight | Go + React | Go + Vue | Wails | Electron + React/Node |
|---|---|---|---|---|---|
| Runs identically in container/K8s | 5 | 5 | 5 | 3 | 3 |
| K8s client/runner ecosystem | 4 | 5 | 5 | 4 | 3 |
| Polished UI speed | 4 | 5 | 4 | 4 | 5 |
| OSS contributor pool | 3 | 5 | 4 | 3 | 5 |
| Download size/perf | 2 | 5 | 4 | 5 | 2 |
| Maintainer prior fit | 2 | 4.5 | 5 | 3 | 2.5 |
| **Weighted total** | | **99** | **91** | **72** | **71** |

Also considered:

- Rust + Tauri only: slower to build and far fewer contributors for the engine and runner code.
- Python: rejected for desktop packaging (interpreter bundling, size, startup).

## Choice

**Go engine + React/shadcn UI, with a Tauri desktop shell added later.**

React's agent and chat UI component ecosystem and its contributor pool matter most for a high-star open-source repo. Go gives a single static binary, a first-class Kubernetes client and identical behaviour on a laptop, in Docker and in a cluster. Tauri wraps the same web UI later for a small native download.

- Runner-up: Go + Vue (91).
- Exit cost: low. The UI talks only to lucidd's HTTP/WebSocket API, so the frontend framework and the desktop shell can each be swapped without touching the engine.

## Other decisions

- Kubernetes for local development and the runner: kind (runner-up: k3d).
- Store: SQLite plus an Obsidian vault (from M1).
- Licence: Apache-2.0.
