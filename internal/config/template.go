package config

// Template is the commented default config written by `lucid config init`.
// It is kept identical to config.example.yaml in the repository root (a test
// enforces this) and every value shown is the built-in default.
const Template = `# Lucidbench configuration.
#
# This file is yours: it lives in your user config directory and is never
# part of the repository. Every key is optional; the values below are the
# built-in defaults. Environment variables (LUCID_*) override this file.
#
# SECRETS: never put API keys or tokens in this file. Any secret setting takes
# a reference to an environment variable, written as env:NAME.

server:
  # Address the daemon (lucidd) listens on, as host:port. The default is
  # loopback only. ":7420" listens on every interface; the daemon can control
  # runner containers and re-run CI, so only do that on a network you trust.
  addr: "127.0.0.1:7420"

providers:
  # For each provider: enabled = false hides it from detection entirely.
  # extra_dirs = additional config directories to scan for accounts, for
  # example a second Claude profile.
  claude:
    enabled: true
    extra_dirs: []
  codex:
    enabled: true
    extra_dirs: []
  grok:
    enabled: true
    extra_dirs: []
  cursor:
    enabled: true
    extra_dirs: []

cluster:
  # Name of the local kind cluster (lowercase letters, digits, dashes).
  name: "lucidbench"

agent:
  # Container image used to run provider CLIs.
  image: "lucidbench/agent:dev"

vault:
  # Path to your notes vault. Empty means not configured.
  path: ""

ui:
  # dark, light or system.
  theme: "dark"
`
