# Remove Quality Gates

Gates were an informational-only keyword-matching system that parsed review text for signals about pattern conformance, simplicity, readability, and hindsight. They never influenced convergence — that's bead-count-driven. The review agent already evaluates all these dimensions explicitly in its prompt and files actionable beads for failures. Gates are a lossy summary of work the reviewer already does.

## Step 1: Strengthen review prompt to cover hindsight

The coding-review prompt already lists pattern conformance, simplicity, readability, and correctness as evaluation criteria. The one gate concern not explicitly covered is **hindsight** — questioning whether the overall approach is wrong, not just tactical issues.

Add a fifth bullet to the coding-review.tmpl evaluation criteria:

```
- Hindsight: Stepping back, is the overall approach sound? Should anything be reconsidered?
```

**Files:** `internal/loop/prompts/coding-review.tmpl`

## Step 2: Remove gate evaluation from the loop

Delete the gate evaluation block in `loop.go` (lines ~179-183) that calls `DefaultGates()` and populates `gateResults`. Stop passing `gateResults` into the snapshot construction (~line 207).

**Files:** `internal/loop/loop.go`

## Step 3: Remove GateResults from Snapshot

Remove the `GateResults []GateResult` field from the `Snapshot` struct and delete the `GateResult` struct.

**Files:** `internal/stream/snapshot.go`

## Step 4: Remove gate rendering from TUI

Delete the gate results rendering block in `detail.go` (lines ~269-280) that displays `[+]`/`[-]` markers.

**Files:** `internal/ui/detail.go`

## Step 5: Delete gates implementation and tests

Delete `gates.go` and `gates_test.go` entirely. Remove the `TestFullPipelineGateResults` integration test from `pipeline_test.go`.

**Files:**
- `internal/loop/gates.go` (delete)
- `internal/loop/gates_test.go` (delete)
- `internal/loop/pipeline_test.go` (remove gate-related test)

## Step 6: Update documentation

Remove the Quality Gates section from `ARCHITECTURE.md` (~lines 473-496) and the R3 requirement from `REQUIREMENTS.md` (~lines 46-52). Add a brief note that the review agent is responsible for all quality evaluation.

**Files:** `ARCHITECTURE.md`, `REQUIREMENTS.md`

## Step 7: Close related beads issues

Close `streams-67i` (Configurable quality gates), `streams-6bl` (QA phase), and `streams-do5` (gates vs review beads discussion) as superseded — gates removed, review agent owns quality evaluation.

## Step 8: Build and test

Run `go build ./...` and `go test ./...` to verify clean removal.
