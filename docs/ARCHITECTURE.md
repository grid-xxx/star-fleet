# Star Fleet Architecture

This document describes the high-level architecture of Star Fleet, a CLI tool that takes a GitHub issue and autonomously delivers a human-ready pull request.

## Overview

Star Fleet operates in two modes:

1. **CLI mode** (`fleet run`) — A developer runs the tool locally against a specific issue. The pipeline executes in their terminal against the local repository clone.
2. **Server mode** (`fleet serve`) — A long-running HTTP server receives GitHub webhook events and triggers pipeline runs automatically for any repository that has the GitHub App installed.

Both modes funnel into the same core pipeline: **orchestrator → agent → PR → review → watch**.

## High-Level Data Flow

```
                         ┌─────────────────────────┐
                         │   GitHub (Issues, PRs,   │
                         │   Webhooks, CI Checks)   │
                         └──────┬──────────┬────────┘
                                │          │
                    webhook     │          │  gh CLI / API
                    events      │          │
                                ▼          ▼
┌──────────────┐     ┌──────────────┐  ┌────────────┐
│  fleet run   │     │   webhook    │  │     gh      │
│  (CLI mode)  │     │   server     │  │  (wrapper)  │
└──────┬───────┘     └──────┬───────┘  └──────┬─────┘
       │                    │                  │
       │  ┌─────────────────┘                  │
       │  │  ┌─────────────┐                   │
       │  │  │  repocache   │ ← clone/fetch    │
       │  │  └──────┬──────┘                   │
       │  │         │                          │
       ▼  ▼         ▼                          │
   ┌───────────────────┐                       │
   │   orchestrator    │ ◄─────────────────────┘
   │   (pipeline)      │
   └──────┬────────────┘
          │
          │  phases: intake → implement → pr → review → watch
          │
          ▼
   ┌───────────────┐    subprocess (PTY)
   │    agent      │ ──────────────────────► claude / cursor CLI
   │  (code agent) │
   └───────────────┘
```

## Main Components

### `cmd/fleet` — Entrypoint

The binary entrypoint. Delegates immediately to the `cli` package, which defines the cobra command tree:

- `fleet run <issue>` — Parse the issue reference and run the orchestrator pipeline.
- `fleet serve` — Start the webhook HTTP server.

### `internal/orchestrator` — Pipeline Controller

The orchestrator is the central nervous system. It owns the full lifecycle of a single issue-to-PR pipeline, advancing through a series of **phases**:

| Phase | What happens |
|---|---|
| **Intake** | Fetch the GitHub issue, validate the spec (body must exist and be ≥50 chars), post a "picked up" comment. |
| **Implement** | Create a git worktree, invoke the code agent to implement the feature and write tests. |
| **PR** | Push the branch and create (or find) a pull request. |
| **Review** | Run automated code review. If issues are found, the agent fixes them and the review repeats (up to `max_rounds`). |
| **Watch** | Enter a polling loop that monitors CI status and PR comments/reviews. The agent responds to feedback, pushes fixes, and optionally auto-merges. |
| **Done** | Terminal state. |

Key design decisions:

- **Resumability** — State is persisted to `.fleet/runs/<number>.json` after each phase transition. Re-running the same command resumes from the last checkpoint.
- **Dependency injection** — Every external dependency (GitHub, git, agent backend, state, watch loop) is abstracted behind an interface on the `Orchestrator` struct, making the pipeline fully testable with mocks.
- **Restart support** — `--restart` discards saved state, closes any existing PR, removes the old worktree, and creates a new versioned branch (`fleet/<N>-v2`, `-v3`, etc.).

### `internal/agent` — Code Agent Abstraction

The agent package defines the `Backend` interface and the `CodeAgent` orchestration layer.

**`Backend` interface:**

```go
type Backend interface {
    Run(ctx context.Context, workdir string, prompt string, output io.Writer) error
}
```

Every backend is a subprocess launched inside a pseudo-terminal (PTY) so that it behaves as if running interactively. The PTY output is captured and streamed to the live TUI display.

**Implementations:**

| Backend | CLI command | Key flags |
|---|---|---|
| `ClaudeBackend` | `claude -p <prompt> --dangerously-skip-permissions` | Headless, non-interactive |
| `CursorBackend` | `cursor agent -p <prompt> --trust --yolo` | Headless, auto-accept |
| `MockBackend` | (no-op) | For testing |

**`CodeAgent`** wraps a `Backend` with issue context and provides three operations:

- `Run()` — Full implementation: the agent reads the codebase, implements the feature, writes tests, and commits.
- `Fix()` — Targeted fix: given review feedback, the agent addresses the issues and commits.
- `HandleEvent()` — PR event response: given context about a comment, review, or CI failure, the agent decides whether to fix code, reply, or do nothing.

**Review output protocol** — For review and event-handling prompts, the agent writes its response to a known file (`.fleet-review-output.md`), which Star Fleet reads back. This avoids parsing unstructured CLI output.

### `internal/ghapp` — GitHub App Authentication

The `ghapp` package implements GitHub App authentication using JWT + installation access tokens:

1. **JWT generation** — Signs a short-lived JWT (10 min) using the App's RSA private key.
2. **Installation lookup** — Resolves `owner` → `installation_id` via `GET /users/{owner}/installation`.
3. **Token exchange** — Exchanges the JWT for a scoped installation access token via `POST /app/installations/{id}/access_tokens`.

Both the installation ID mapping and tokens are cached in memory with expiry-aware refresh (tokens are refreshed 5 minutes before expiration).

This component is used in two places:
- **`fleet serve`** — The webhook server uses App tokens for all git operations (clone, fetch, push) and API calls.
- **Code review** — When `review.app_id` and `review.app_key_file` are configured, reviews are submitted via the GitHub API as the App identity (instead of the developer's `gh` CLI credentials).

### `internal/webhook` — Webhook Server

The webhook package provides the HTTP server for `fleet serve` mode. It has two layers:

**`Server`** — HTTP plumbing:
- `POST /webhook` — Receives GitHub webhook payloads. Verifies the HMAC-SHA256 signature against the configured secret, extracts the event type from `X-GitHub-Event`, and delegates to the `Handler`.
- `GET /health` — Returns `{"status":"ok"}` for load balancer probes.
- Body is limited to 1 MB. Graceful shutdown on SIGINT/SIGTERM.

**`Handler`** — Event routing and concurrency control:
- **`issues` event** — Triggers a pipeline run when an issue is labeled with the configured label (default: `fleet`). Ignores bot users.
- **`issue_comment` event** — Triggers a run when a comment body matches `/fleet run`. Ignores bot users.
- **Per-repo serialization** — A mutex map (`map[string]bool`) ensures only one pipeline runs per `owner/repo` at a time. If a repo is busy, the event is rejected with status `"busy"`.
- **Async execution** — Pipeline runs are launched in a goroutine so the webhook response returns immediately with `"triggered"`.

The `Runner` interface (`Run(owner, repo string, number int) error`) is implemented by `pipelineRunner` in the `cli` package, which uses `repocache` to obtain a local clone and then runs the orchestrator.

### `internal/repocache` — Repository Clone Cache

The repocache manages a pool of local git clones under a work directory, used exclusively by `fleet serve`:

```
<workdir>/
├── org-a/
│   ├── repo-1/    ← full clone
│   └── repo-2/
└── org-b/
    └── repo-3/
```

**Key behaviors:**

- `Ensure(ctx, owner, repo)` — If the repo is already cloned, runs `git fetch origin` + `git reset --hard origin/<branch>` to update. Otherwise, performs a fresh `git clone`.
- **Per-repo mutex** — Concurrent calls for the same repo are serialized; different repos proceed in parallel.
- **Token-based auth** — Uses an inline git credential helper (`GIT_CONFIG_KEY_0=credential.helper`) to inject the installation token without writing it to disk, URLs, or `.git/config`. Tokens are redacted in error messages.
- **Bot identity** — Configures `user.name` and `user.email` as `star-fleets[bot]` on each clone.

### `internal/webhook` + `internal/repocache` — Server Mode Flow

When `fleet serve` receives a webhook:

```
Webhook → signature check → Handler.HandleEvent()
  → label/command match? → bot check → per-repo lock
    → pipelineRunner.Run(owner, repo, number)
      → repocache.Ensure() (clone or fetch+reset)
        → config.Load() (read .fleet/config.toml)
          → orchestrator.Run() (full pipeline)
```

## Supporting Packages

### `internal/gh` — GitHub CLI Wrapper

Wraps the `gh` CLI for all GitHub operations: fetch issues, create/merge/close PRs, post comments, submit reviews, check CI status, list comments and reviews. All functions shell out to `gh` with JSON output and parse the results.

Also includes `APIReviewClient`, which uses the GitHub REST API directly (with App installation tokens) for submitting reviews — used when GitHub App credentials are configured for the review phase.

### `internal/git` — Git Operations

Wraps the `git` CLI for worktree management, push (with exponential backoff retry), branch operations, diff, commit, and status checks.

### `internal/review` — Code Review

The `Reviewer` instructs an agent backend to inspect the diff between a PR's base and head branches. The review prompt asks the agent to run `git diff base...head` itself and report issues. Responses are classified as approvals (via keyword matching: "LGTM", "no issues found", etc.) or lists of issues (counted by bullet points or numbered items).

### `internal/watch` — PR Watch Loop

A polling loop that monitors a PR for new events after it's been created:

- **Event polling** — Fetches PR comments, reviews, and CI check runs. Deduplicates against previously processed event IDs stored in `RunState`.
- **Event handling** — For each actionable event (comment, review, CI failure), invokes the code agent's `HandleEvent()`, which may fix code and push, post a reply, or take no action.
- **Exit conditions** — The loop exits on: PR merged, PR closed, total timeout, idle timeout (no events), max fix rounds reached, or CI green + auto-merge eligible.
- **Self-comment detection** — Comments posted by Star Fleet (identified by emoji-prefixed headers like `## 🚀 Star Fleet`) are skipped to prevent feedback loops.

### `internal/state` — Run State Persistence

Tracks pipeline progress as a JSON file at `.fleet/runs/<number>.json`. Contains the current phase, branch name, PR info, review round count, watch loop metadata (processed event IDs, fix count, timestamps), and agent completion flags. The state directory is auto-gitignored.

Thread-safe via `sync.Mutex` — the watch loop and agent can update state concurrently.

### `internal/config` — Configuration

Loads `.fleet/config.toml` from the repo root with sensible defaults. Supports environment variable overrides for sensitive values (`FLEET_TELEGRAM_BOT_TOKEN`, `FLEET_TELEGRAM_CHAT_ID`, `FLEET_REVIEW_APP_ID`, `FLEET_REVIEW_APP_KEY`).

### `internal/notify` — Notifications

Optional Telegram notifications for lifecycle events: PR created, PR merged, run failed. Uses a `Notifier` interface with a `Nop` implementation when unconfigured.

### `internal/ui` — Terminal Display

Rich terminal output using [lipgloss](https://github.com/charmbracelet/lipgloss) for styling. Features:

- **Step display** — Success/fail/warn markers with labels.
- **Spinners** — Animated braille spinners for in-progress operations.
- **Live view** — Multi-panel real-time display that captures agent subprocess output (via the `io.Writer` interface on `AgentPanel`) and renders the last few lines with ANSI cursor control for in-place updates.

### `internal/retry` — Exponential Backoff

Generic retry helper with exponential backoff, used for git push and PR creation where transient network failures are expected.

## Directory Layout

```
star-fleet/
├── cmd/fleet/main.go                  # Binary entrypoint
├── internal/
│   ├── agent/                         # Code agent backends (claude, cursor, mock)
│   │   ├── agent.go                   #   CodeAgent: Run/Fix/HandleEvent
│   │   ├── backend.go                 #   Backend interface + RunForReview
│   │   ├── claude.go                  #   Claude Code backend
│   │   ├── cursor.go                  #   Cursor backend
│   │   ├── mock.go                    #   Mock backend for tests
│   │   └── pty.go                     #   PTY subprocess runner
│   ├── cli/                           # Cobra commands (run, serve)
│   ├── config/                        # .fleet/config.toml loader + defaults
│   ├── gh/                            # GitHub CLI wrapper + REST API client
│   │   ├── gh.go                      #   gh CLI operations
│   │   └── review_api.go             #   APIReviewClient (GitHub App)
│   ├── ghapp/                         # GitHub App JWT + installation tokens
│   ├── git/                           # Git CLI wrapper (worktree, push, etc.)
│   ├── notify/                        # Telegram notifications
│   ├── orchestrator/                  # Main pipeline controller
│   ├── repocache/                     # Local repo clone cache (server mode)
│   ├── retry/                         # Exponential backoff helper
│   ├── review/                        # Automated code review logic
│   ├── state/                         # Run state persistence (.fleet/runs/)
│   ├── ui/                            # Terminal display (lipgloss, live view)
│   └── watch/                         # PR watch loop (events, handler)
│       ├── events.go                  #   Event polling + classification
│       ├── handler.go                 #   Event→agent dispatch + push/reply
│       └── watch.go                   #   Main polling loop
├── test/integration/                  # Integration tests
├── .fleet/config.toml                 # Repo-level configuration
├── fleet.toml                         # Project metadata
└── Makefile                           # Build targets
```

## Key Design Patterns

1. **Phase-based pipeline with checkpointing** — Each phase is a discrete unit of work. State is saved after completion, enabling crash recovery and resumability.

2. **Agent as black box** — The code agent is invoked as an external subprocess. Star Fleet doesn't parse or understand the agent's code changes — it only knows whether the agent succeeded and whether new commits appeared.

3. **Interface-driven testability** — The orchestrator depends on 6 interfaces (`GHClient`, `GitClient`, `StateManager`, `WatchRunner`, `BackendFactory`, `ReviewRunner`). Tests inject mocks for all external I/O.

4. **PTY-based subprocess capture** — Agents run inside a pseudo-terminal so they behave identically to interactive use. Output is streamed to the TUI's live display panels via `io.Writer`.

5. **Two authentication paths** — CLI mode uses the developer's `gh` CLI credentials. Server mode uses GitHub App installation tokens with in-memory caching and credential helper injection.

6. **Self-comment filtering** — The watch loop identifies its own comments by header markers to prevent infinite feedback loops.
