# Development Guide

## Prerequisites

- Go 1.22+
- `gh` CLI (authenticated)
- One of: Claude Code (`claude`), Cursor (`cursor`)

## Build

```bash
make build          # builds ./fleet binary
```

## Test

```bash
make test           # unit tests (short mode)
make check          # lint + test + build
```

Integration tests require a real GitHub repo and authenticated `gh`:

```bash
go test ./test/integration/ -v
```

Clean up test repos created by integration tests:

```bash
make clean-test-repos
```

## Project Structure

```
cmd/fleet/          CLI entrypoint
internal/
├── cli/            Command definitions
├── config/         Config loading (.fleet/config.toml)
├── gh/             GitHub CLI wrapper
├── git/            Git/worktree operations
├── agent/          Backend interface + Dev/Test agents
├── orchestrator/   Main pipeline state machine
├── review/         PR review logic
├── validate/       Cross-validation logic
├── state/          State persistence
└── ui/             Terminal display
```

## Adding a New Backend

1. Create `internal/agent/<name>.go`
2. Implement the `Backend` interface:
   ```go
   type Backend interface {
       Run(ctx context.Context, workdir string, prompt string) error
       Fix(ctx context.Context, workdir string, feedback string) error
   }
   ```
3. Register in `internal/agent/backend.go` `NewBackend()` switch
4. Add config option in `internal/config/config.go`

## Release

Releases are automated via GitHub Actions (`.github/workflows/release.yml`):
- Every push to `main` triggers a release
- Version is `0.1.<commit-count>`
- Builds for: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- Creates GitHub Release with tarballs + checksums

## Debugging

### Inspect pipeline state

```bash
cat .fleet/state-<issue-number>.json | jq .
```

### Resume a failed run

```bash
fleet run <issue>    # automatically resumes from last checkpoint
```

### Restart from scratch

```bash
fleet run --restart <issue>
```

### Check worktrees

```bash
git worktree list
```
