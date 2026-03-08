# Architecture

Star Fleet is a CLI tool that takes a GitHub Issue and autonomously delivers a human-ready PR. It orchestrates two independent AI agents (Dev + Test) running in parallel, reviews their work, cross-validates, and delivers a merged PR.

## High-Level Flow

```
fleet run <issue>
    │
    ├─ Intake         Fetch issue, validate spec, post "picked up" comment
    ├─ Worktrees      Create isolated git worktrees for dev and test
    ├─ Dispatch       Run Dev Agent + Test Agent in parallel
    ├─ Push           Push both branches to origin
    ├─ PRs            Create PRs for each agent's work
    ├─ Review         Code review each PR via AI, fix issues
    ├─ Validate       Merge both branches, run tests, fix failures
    └─ Deliver        Create final PR, close intermediates
```

## Package Map

```
cmd/fleet/main.go          Entrypoint — parses args, calls CLI
internal/
├── cli/                    Cobra commands + issue ref parsing
│   ├── root.go             Root command, version flag
│   └── run.go              `fleet run` — parses issue ref, creates orchestrator
├── config/                 Configuration loading
│   └── config.go           Reads .fleet/config.toml (agent backend, test command, etc.)
├── gh/                     GitHub CLI wrapper
│   └── gh.go               Issue fetch, PR create/find/close, comments (all via `gh` CLI)
├── git/                    Git operations
│   └── worktree.go         Worktree create/remove, branch ops, merge, push
├── agent/                  AI agent abstraction
│   ├── backend.go          Backend interface (Run, Fix methods)
│   ├── claude.go           Claude Code backend implementation
│   ├── cursor.go           Cursor backend implementation
│   ├── mock.go             Mock backend for testing
│   ├── dev.go              Dev Agent — writes implementation
│   └── test.go             Test Agent — writes tests only
├── orchestrator/           Main pipeline
│   └── orchestrator.go     Phase-based state machine (intake → dispatch → review → validate → deliver)
├── review/                 PR review
│   └── review.go           AI-powered PR review, posts comments, counts issues
├── validate/               Cross-validation
│   └── validate.go         Merge branches, strip dev tests, run test suite, attribute failures
├── state/                  Pipeline state persistence
│   └── state.go            Saves/loads run state to .fleet/state-<issue>.json (enables resume)
└── ui/                     Terminal display
    └── display.go          Tree-style output, live view with parallel agent panels
```

## Key Design Decisions

### Agent Isolation via Worktrees

Each agent runs in its own [git worktree](https://git-scm.com/docs/git-worktree). Worktrees share the same `.git` directory but have completely separate working directories. This means:
- Dev cannot read Test's code (and vice versa)
- Both can commit independently without conflicts
- Cleanup is simple: `git worktree remove`

### Backend Abstraction

The `Backend` interface (`agent/backend.go`) abstracts the code agent CLI:

```go
type Backend interface {
    Run(ctx context.Context, workdir string, prompt string) error
    Fix(ctx context.Context, workdir string, feedback string) error
}
```

Currently supports Claude Code and Cursor. Adding a new backend means implementing these two methods.

### Phase-Based State Machine

The orchestrator runs through phases sequentially: `New → Intake → Dispatch → Push → PRs → Review → Validate → Done`. State is persisted to `.fleet/state-<issue>.json` after each phase, enabling:
- **Resume**: if the process crashes, re-running picks up where it left off
- **Idempotency**: phases check cached state before re-executing (e.g., won't create duplicate PRs)

### Cross-Validation

After both agents finish, their branches are merged into a temporary worktree. Dev-authored test files are stripped, then the test suite runs. This verifies that Test's independently-written tests pass against Dev's implementation — a strong signal of correctness since neither agent saw the other's work.

If tests fail, the failure is attributed (dev or test) and the responsible agent is asked to fix. This loops up to `max_fix_rounds × max_cycles` times.

### GitHub as the Communication Surface

All activity is posted to GitHub (issue comments, PR reviews, status updates). This means:
- Humans can observe progress in real-time
- The full history is preserved in GitHub's UI
- No separate dashboard or monitoring needed

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `gh` CLI | All GitHub operations (issues, PRs, comments) |
| `git` | Worktree management, branch ops, merge |
| Claude Code / Cursor | AI code agent (treated as black box subprocess) |
| `cobra` | CLI framework |
| `toml` | Config file parsing |

## Configuration

`.fleet/config.toml` in the repo root:

```toml
[agent]
backend = "claude-code"    # "claude-code" | "cursor"

[test]
command = ""               # override auto-detected test command

[validate]
max_fix_rounds = 3         # fix attempts per cycle
max_cycles = 2             # full restart cycles
```

Test command auto-detection supports: Go, Node.js (npm/yarn/pnpm), Rust, Python, and Make.
