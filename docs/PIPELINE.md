# Pipeline Details

Detailed walkthrough of each pipeline phase.

## Phase 1: Intake

1. Fetch the issue via `gh issue view`
2. Validate the spec:
   - Body must not be empty
   - Body must be ≥50 characters
   - If validation fails → post a "Spec Gap" comment on the issue, halt
3. Post a "Picked Up" comment on the issue
4. Save state → `PhaseIntake`

## Phase 2: Worktrees

Create two isolated git worktrees:

```
<repo>/worktrees/dev    → branch: fleet/dev/<issue-id>
<repo>/worktrees/test   → branch: fleet/test/<issue-id>
```

Worktrees are idempotent — if they already exist (from a previous run), they're reused.

## Phase 3: Dispatch

Run Dev and Test agents **in parallel**:

- **Dev Agent**: receives the issue body + a prompt to implement the feature. Works in `worktrees/dev/`.
- **Test Agent**: receives the issue body + a prompt to write tests only (no implementation). Works in `worktrees/test/`.

Both agents invoke the configured backend CLI (Claude Code or Cursor) as a subprocess. The terminal shows a live split view of both agents' progress.

Per-agent checkpointing: if one agent completes and the other crashes, the completed agent's work is cached on resume.

## Phase 4: Push

Push both branches to origin. Idempotent — `git push` is a no-op if already up to date.

## Phase 5: PRs

Create (or find existing) PRs for each branch:

- `[fleet/dev] #<issue> <title>` → implementation PR
- `[fleet/test] #<issue> <title>` → tests PR

## Phase 6: Review

AI-powered code review of each PR:

1. Fetch the PR diff
2. Send to the code agent with a review prompt
3. If issues found:
   - Post review comments on the PR
   - Ask the agent to fix
   - Push fixes
4. If clean → proceed

## Phase 7: Cross-Validation

The critical quality gate:

1. Create a temporary `validate` worktree
2. Merge dev branch into it
3. Merge test branch into it
4. **Strip any test files authored by the Dev agent** (ensures only Test's independent tests run)
5. Run the test suite
6. If all pass → proceed to delivery
7. If failures:
   - Attribute to dev or test based on which files failed
   - Ask the responsible agent to fix
   - Retry (up to `max_fix_rounds` per cycle, `max_cycles` total)

### Why Strip Dev's Tests?

The Dev agent may write tests that pass trivially against its own implementation. The Test agent wrote tests independently, without seeing the implementation. If those independent tests pass, it's a much stronger correctness signal.

## Phase 8: Deliver

1. Create a `fleet/deliver/<issue>` branch
2. Merge both dev and test branches into it
3. Push and create the final PR with body: `Closes #<issue>`
4. Close the intermediate dev and test PRs
5. The issue auto-closes when the final PR is merged

## State Persistence

State is saved to `.fleet/state-<issue>.json` after each phase:

```json
{
  "owner": "org",
  "repo": "repo",
  "issue_number": 42,
  "phase": "review",
  "dev_branch": "fleet/dev/42",
  "test_branch": "fleet/test/42",
  "base_branch": "main",
  "dev_agent_done": true,
  "test_agent_done": true,
  "dev_pr": { "number": 100, "url": "..." },
  "test_pr": { "number": 101, "url": "..." },
  "val_cycle": 1,
  "val_round": 1
}
```

This enables:
- **Resume after crash**: `fleet run 42` picks up where it left off
- **Idempotent operations**: no duplicate PRs, no redundant pushes
- **Debugging**: inspect state file to understand where the pipeline stopped

## GitHub Trail

| Stage | What gets posted |
|-------|-----------------|
| Intake | "Picked up" comment on issue |
| Spec gap | Gap list + "pipeline paused" comment |
| PR created | PR with descriptive title and body |
| Review | Review comments on each PR |
| Validation fail | Failure attribution + test output on PR |
| Delivery | Final PR with "Closes #N", intermediate PRs closed |
| Exhausted | "Pipeline exhausted" comment on issue |
