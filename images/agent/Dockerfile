# Lucidbench agent image: official provider CLIs, no credentials baked in.
# Credentials arrive at run time via a mounted profile (see internal/runner).
FROM node:22-bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Claude Code, Codex, and the official Grok CLI (@xai-official/grok).
RUN npm install -g @anthropic-ai/claude-code @openai/codex @xai-official/grok \
    && npm cache clean --force

# Cursor's CLI (cursor-agent) is intentionally NOT installed: its only Linux
# install path is the unpinned curl-piped script at cursor.com/install.
# TODO: add once a pinned, non-interactive install exists.

USER root
ENV HOME=/root
WORKDIR /work
