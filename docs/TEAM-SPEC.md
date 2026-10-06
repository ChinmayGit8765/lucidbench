# The AI-team spec: `lucid-team.yaml` version 1

A team says which provider fills each role of the Lucidbench loop for a project: who drafts the
brief, who critiques it, who builds, and which decisions always stay with you. It is a small YAML
file, so it can live in the project's repository next to the code, be reviewed in a PR, and be
written by other tools.

- **Schema:** [`docs/schemas/lucid-team.schema.json`](schemas/lucid-team.schema.json) (JSON
  Schema 2020-12). Lucidbench enforces the same rules in `internal/team`; a test keeps the two in
  step.
- **UI:** Projects › a project's **Team** button opens its Team tab.

## Example

```yaml
version: 1
roles:
  proposer: {provider: claude, model: sonnet, budget_usd: 0.50}
  critic:
    - {provider: codex, model: gpt-5-codex}
    - {provider: grok, model: grok-4}
  builder:
    provider: claude
    model: sonnet
    allowed_commands: [go, gofmt, npm]
    budget_usd: 2
  scout: {provider: claude, model: haiku, mcp_allow: [github]}
gates:
  approve_brief: human
  open_pr: human
  merge: human
```

## Fields

| Field | Meaning |
|---|---|
| `version` | Required. Must be `1`. |
| `roles.proposer` | Drafts the brief in the Council and revises it after each critique. |
| `roles.critic` | One role, or a list of up to two. The Council runs them in parallel; a critic on the proposer's provider is dropped (the proposer is the third voice). Lucidbench always writes a list. |
| `roles.builder` | Works on a card in Work, in its own worktree. |
| `roles.reviewer`, `roles.scout` | Recorded and checked; no Lucidbench run uses them yet. |
| `gates.approve_brief`, `gates.open_pr`, `gates.merge` | Who decides. Version 1 accepts only `human`, and a missing gate is `human`. Approving a brief, opening a PR and merging are never automated. |

Each role is:

| Key | Meaning |
|---|---|
| `provider` | Required: `claude`, `codex` or `grok`, the CLI signed in with your own account. |
| `profile` | A host account profile of that provider (as on the Accounts page). Omitted or `default` is the default profile; grok has none. |
| `model` | The CLI's model name or alias (`sonnet`, `haiku`, `gpt-5-codex`, `grok-4`). Omitted means the CLI's default. |
| `mcp_allow` | MCP server names the role may use, as on the MCP servers page. |
| `allowed_commands` | Builder only: the commands a Work session may run without asking, as command prefixes. Like `work.allowed_commands` in `projects.yaml`, they replace the commands detected from the project's stack; read-only git, `git add`/`commit`/`restore` and the assessment's check commands are always added. |
| `budget_usd` | What one run of the role should cost, at most, in US dollars. |

Unknown keys are errors, so a typo never silently means "use the default".

## Where a project's team comes from

In order, the first that exists and is valid:

1. `<local_path>/.lucid/team.yaml`: in the project's checkout, when `local_path` in
   `projects.yaml` is an existing folder. Commit it like any other file.
2. `<data dir>/teams/<project id>.yaml`: Lucidbench's own copy, for a project without a checkout
   or a team you would rather not commit.
3. `team:` in `config.yaml`: your default team, the same document under one key (see
   [CONFIG.md](CONFIG.md#keys)).
4. Lucidbench's defaults: a claude proposer, codex and grok critics, a claude builder. These
   change nothing: Council and Work behave as they do with no team at all.

A file that does not parse or validate is skipped, and the Team tab says so; the next source
applies. The Team tab saves to the repository (after you confirm, because it writes into your
checkout) or to the data folder, and shows the path either way. **Import YAML** checks a pasted
file before you save it.

**A team file is a suggestion from whoever wrote the repository.** A `.lucid/team.yaml` in a
repository you cloned chooses models (and so what a run costs) and can widen the builder's allowed
commands with any command that is not on the always-refused list. Lucidbench never runs anything
from it on its own: New Session shows the provider, model and the full command list before you
start, and the Council composer shows the seats and models before you convene. Read a team you did
not write the way you would read its build scripts.

## How Lucidbench uses it

- **Council:** when a braindump names a project, the composer seats the team's proposer and
  critics, and the daemon runs each with the team's `model` and `profile`. A provider you pick
  by hand in the composer wins; it still gets the team's model when the team seats that
  provider in that place. `GET /api/council/sessions/{id}` records `models`, `profiles` and
  `team` (the source), and each step its `model`.
- **Work:** a new session on the project starts with the builder's provider, model, profile and
  allowed commands. `POST /api/work/sessions` may leave `provider` empty when the project has a
  builder; anything the request sets wins. The session records `model` and `team`.
- **Budgets:** the Team tab, the Council composer and New Session warn when recent runs of a role
  on its provider cost more on average than its `budget_usd` (the last 20 runs Lucidbench
  recorded: council proposals with their revisions, each critique, each Work session). Version 1
  never stops a run.
- **MCP:** `mcp_allow` is checked against the MCP matrix. Council runs with the clean harness,
  which loads no MCP server at all, and Work loads a CLI's servers only with your own harness; the
  list does not narrow what a CLI loads yet.
- **Confidential projects:** the confidential rule wins. A confidential project's team is kept
  and shown, but Council and Work refuse the project before any provider runs, whatever the team
  says.

## Checks

The Team tab and `POST /api/team/validate` report three kinds of finding:

- **Errors** (the team cannot be saved or used): a version other than 1, an unknown provider, a
  role with no provider, a gate other than `human`, more than two critics, a malformed profile,
  model or MCP name, a command that can never be allowed (`git push`, `rm`, shells, `curl`,
  `gh`…), a budget outside 0-10000.
- **Warnings** (shown, never blocking): a provider that is not signed in on this machine, a
  profile that does not exist, an MCP server that is not configured for that provider, a model
  name that does not look like one of the provider's, recent runs over budget, and a confidential
  project.
- **Notes:** that `mcp_allow` loads only with your own harness.

## Routes

| Route | Method | Purpose |
|---|---|---|
| `/api/projects/{id}/team` | GET | `{project, project_name, team, source: repo\|data\|config\|builtin, path_hint, targets: {repo?, data}, problems, estimates, confidential}` |
| `/api/projects/{id}/team` | PUT | `{team, target: repo\|data}` saves it (needs `X-Lucid-Confirm: yes`); a schema error is `400` |
| `/api/team/validate` | POST | `{yaml \| team, project?}` returns `{team, problems, estimates, valid}`; saves nothing |
| `/api/team/default` | GET | the config default, or the built-in one |

## Writing it from other tools

The format is plain YAML with a published JSON Schema so that other tools can produce it. A visual
agent builder (PrompterJack's, for example) can export a team as `lucid-team.yaml`: map its agent
nodes onto the five roles, its model choices onto `provider` and `model`, its tool or connector
grants onto `mcp_allow`, and its cost caps onto `budget_usd`, and leave every gate `human`.
Validate the export against the schema, then drop it at `.lucid/team.yaml` in the repository or
paste it into **Import YAML**. Lucidbench never reaches out to such a tool; the file is the whole
interface.
