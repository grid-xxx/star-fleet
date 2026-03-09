# Star Fleet

[![test](https://github.com/grid-xxx/star-fleet/actions/workflows/test.yml/badge.svg)](https://github.com/grid-xxx/star-fleet/actions/workflows/test.yml) [![Go](https://img.shields.io/github/go-mod/go-version/grid-xxx/star-fleet)](https://go.dev/)

A CLI tool that takes a GitHub Issue and autonomously delivers a human-ready PR.

A single code agent implements the feature **and** writes tests. After pushing a PR, a watch loop monitors CI and review comments, auto-fixing issues and optionally auto-merging.

## Prerequisites

- **Go 1.22+**
- **`gh`** (GitHub CLI), authenticated
- A local code agent:
  - [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (`claude` CLI), or
  - [Cursor](https://cursor.sh) (`cursor` CLI)

## Install

**One-liner** (Linux / macOS):

```bash
curl -fsSL https://raw.githubusercontent.com/nullne/star-fleet/main/install.sh | bash
```

Custom install directory:

```bash
curl -fsSL https://raw.githubusercontent.com/nullne/star-fleet/main/install.sh | bash -s -- --dir ~/.local/bin
```

**Go install:**

```bash
go install github.com/nullne/star-fleet/cmd/fleet@latest
```

**Build from source:**

```bash
git clone https://github.com/nullne/star-fleet.git
cd star-fleet
go build -o fleet ./cmd/fleet
```

## Usage

```bash
fleet run 42                             # issue in current repo
fleet run org/repo#42                    # basic run
fleet run https://github.com/org/repo/issues/42
fleet run org/repo#42 --auto-merge       # auto-merge when CI green
fleet run org/repo#42 --restart          # discard state, start fresh
fleet run org/repo#42 --no-watch         # skip watch loop
```

Run `fleet` from inside the target repository. The pipeline automatically saves progress; re-running the same command resumes from the last completed phase.

## How It Works

```
fleet run <issue>
    │
    ▼
Fetch & validate Issue spec
    │  body empty or <50 chars → post comment → halt
    ▼
Create worktree (single branch: fleet/<issue-id>)
    ▼
Code Agent implements feature + writes tests
    ▼
Push branch → Create PR
    ▼
Watch loop (monitor CI + review comments)
    ├─ Review comment → Agent fixes → push
    ├─ CI green + --auto-merge → squash-merge PR
    ├─ PR merged externally → done
    └─ Timeout / idle → exit
```

## Configuration

Create `.fleet/config.toml` in the repo root:

```toml
[agent]
backend = "claude-code"  # "claude-code" | "cursor"

[watch]
auto_merge = false       # auto-merge when CI passes
poll_interval = "30s"
idle_timeout = "30m"
timeout = "2h"
max_fix_rounds = 5

[ci]
enabled = true
required_checks = []

[test]
command = ""  # override auto-detected test command, e.g. "go test ./..."

[telegram]
bot_token = ""           # or env FLEET_TELEGRAM_BOT_TOKEN
chat_id = ""             # or env FLEET_TELEGRAM_CHAT_ID
```

### `[agent]`

| Key | Default | Description |
|---|---|---|
| `backend` | `"claude-code"` | Which code agent CLI to invoke (`"claude-code"` or `"cursor"`) |

### `[watch]`

| Key | Default | Description |
|---|---|---|
| `auto_merge` | `false` | Automatically squash-merge the PR when CI passes |
| `poll_interval` | `"30s"` | How often to poll for new events |
| `idle_timeout` | `"30m"` | Exit watch loop after this period of inactivity |
| `timeout` | `"2h"` | Maximum total watch time |
| `max_fix_rounds` | `5` | Max fix attempts before exiting the watch loop |

### `[ci]`

| Key | Default | Description |
|---|---|---|
| `enabled` | `true` | Whether to monitor CI status |
| `required_checks` | `[]` | List of required check names (empty = all checks) |

### `[test]`

| Key | Default | Description |
|---|---|---|
| `command` | `""` (auto-detect) | Test command to run. When empty, Star Fleet detects the project type (`go test ./...`, `npm test`, `cargo test`, etc.) |

### `[telegram]`

| Key | Default | Description |
|---|---|---|
| `bot_token` | `""` | Telegram bot token (or env `FLEET_TELEGRAM_BOT_TOKEN`) |
| `chat_id` | `""` | Telegram chat ID (or env `FLEET_TELEGRAM_CHAT_ID`) |

## GitHub Trail

| Stage | What gets posted |
|---|---|
| Intake | "Picked up" comment on Issue |
| Spec gap | Gap comment on Issue, pipeline halted |
| PR created | PR opened with description |
| Watch | Agent responds to review comments |
| Done | PR merged (manually or auto-merge) |

## Architecture

The code agent runs in an isolated [git worktree](https://git-scm.com/docs/git-worktree) and invokes the configured CLI (Claude Code or Cursor) as a subprocess with a prompt containing the issue body. The code agent is treated as a black box that reads the repo, implements the feature, writes tests, and makes commits.

```
star-fleet/
├── cmd/fleet/main.go             # entrypoint
├── internal/
│   ├── agent/                    # Code agent backend (claude-code, cursor)
│   ├── cli/                      # cobra commands + issue ref parsing
│   ├── config/                   # .fleet/config.toml loader
│   ├── gh/                       # gh CLI wrapper (issue, PR, merge, CI check)
│   ├── git/                      # git worktree + push with retry
│   ├── notify/                   # Telegram notifications
│   ├── orchestrator/             # main pipeline controller
│   ├── retry/                    # exponential backoff helper
│   ├── state/                    # run state persistence (.fleet/runs/)
│   ├── ui/                       # terminal display (spinners, live view)
│   └── watch/                    # PR watch loop (CI, comments, auto-merge)
└── .fleet/config.toml            # repo-level config
```

## License

MIT
