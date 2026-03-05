# Star Fleet

A CLI tool that takes a GitHub Issue and autonomously delivers a human-ready PR.

Two agents run in parallel — **Dev** writes implementation, **Test** writes tests — without seeing each other's work. A main orchestrator reviews, cross-validates, and delivers the final PR. All activity surfaces on GitHub via `gh`.

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
fleet run 42
fleet run org/repo#42
fleet run https://github.com/org/repo/issues/42
```

Run `fleet` from inside the target repository. It will:

1. Fetch and validate the issue spec
2. Create isolated git worktrees for Dev and Test agents
3. Dispatch both agents in parallel
4. Review the resulting PRs
5. Cross-validate (merge + run tests)
6. Open a final PR ready for human review

## Configuration

Create `.fleet/config.toml` in the repo root:

```toml
[agent]
backend = "claude-code"  # "claude-code" | "cursor"

[test]
command = ""  # override auto-detected test command, e.g. "go test ./..."

[validate]
max_fix_rounds = 3
max_cycles = 2
```

### `[agent]`

| Key | Default | Description |
|---|---|---|
| `backend` | `"claude-code"` | Which code agent CLI to invoke (`"claude-code"` or `"cursor"`) |

### `[test]`

| Key | Default | Description |
|---|---|---|
| `command` | `""` (auto-detect) | Test command to run during cross-validation. When empty, Star Fleet detects the project type (`go test ./...`, `npm test`, `cargo test`, etc.) |

### `[validate]`

| Key | Default | Description |
|---|---|---|
| `max_fix_rounds` | `3` | Max fix attempts per cycle before restarting |
| `max_cycles` | `2` | Max full cycles before halting |

## How It Works

```
fleet run <issue>
    │
    ▼
Fetch & validate Issue spec  (gh issue view)
    │  gaps found → post Issue comment → halt
    ▼
Create isolated worktrees
    ├─ worktrees/dev   (branch: fleet/dev/<issue-id>)
    └─ worktrees/test  (branch: fleet/test/<issue-id>)
    │
    ▼
Dispatch in parallel
    ├─► Dev Agent  → implementation → push → PR
    └─► Test Agent → tests only     → push → PR
    │
    ▼
Review both PRs via code agent
    ├─ issues → agent fixes → push
    └─ clean  → proceed
    │
    ▼
Cross-validation
    │  merge both branches, strip dev-authored tests, run suite
    ├─ pass → create final PR → done
    └─ fail → attribute → fix → retry (max 3 rounds × 2 cycles)
```

## GitHub Trail

| Stage | What gets posted |
|---|---|
| Intake | "Picked up" comment on Issue |
| Spec gap | Gap list comment on Issue, pipeline halted |
| PR review | Review comment on each agent's PR |
| Cross-validation fail | Failure attribution + output on PR |
| Done | Final merged PR opened, Issue closed |

## Architecture

Each agent runs in its own [git worktree](https://git-scm.com/docs/git-worktree) — they share the same `.git` but have completely separate working directories, so neither can read the other's files.

Agents invoke the configured code agent CLI (Claude Code or Cursor) as a subprocess with a role-specific prompt containing the issue body. The code agent is treated as a black box that reads the repo and makes commits.

```
star-fleet/
├── cmd/fleet/main.go             # entrypoint
├── internal/
│   ├── cli/                      # cobra commands + issue ref parsing
│   ├── config/                   # .fleet/config.toml loader
│   ├── gh/                       # gh CLI wrapper
│   ├── git/                      # git worktree + branch ops
│   ├── agent/                    # Backend interface, Dev/Test agents
│   ├── orchestrator/             # main pipeline
│   ├── review/                   # PR review via code agent
│   ├── validate/                 # cross-validation logic
│   └── ui/                       # terminal display
└── .fleet/config.toml            # default config
```

## License

MIT
