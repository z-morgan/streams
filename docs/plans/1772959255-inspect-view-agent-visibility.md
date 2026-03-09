# Inspect View: Agent Visibility

## Problem

Two gaps in the inspect view:

1. **No indication of which agent is running.** The in-progress row shows `⠋ coding 2` but doesn't say whether it's the implement or review agent. The header also lacks this info.

2. **Review feedback missing from snapshots.** All four review prompts (plan, research, decompose, coding) instruct the agent to file beads issues via `bd create` tool calls and only output text when no improvements are found ("No further improvements needed."). This means `snap.Review` is empty for every iteration where the review agent actually has feedback — the substantive findings are invisible in the snapshot detail.

## Changes

### Step 1: Show current step in inspect view header and iteration list

**Files:** `internal/ui/detail.go`

In the header, append the current `IterStep` when the stream is running:

```
mystream [Running] coding iter 2 · Review
```

In the iteration list, show the step for the in-progress row:

```
⠋ coding 2 · Implement
⠋ coding 2 · Review
```

The `iterationRow` struct needs a `Step` field populated from `st.GetIterStep()` for in-progress rows. Only the Implement and Review steps are worth labeling — Autosquash/Checkpoint/Guidance are transient.

### Step 2: Add summary requirement to review prompts

**Files:** `internal/loop/prompts/{plan,research,decompose,coding}-review.tmpl`

Add an instruction to each review prompt asking the agent to always write a brief text summary of its findings, whether or not it files issues. Something like:

```
After completing your review, write a brief summary (2-4 sentences) of your overall assessment. If you filed issues, summarize what you found. If no issues, explain why the work is ready.
```

This ensures `reviewResp.Text` (and therefore `snap.Review`) is always populated with readable feedback. The existing UI code already renders `snap.Review` when non-empty — no UI changes needed for this step.

### Step 3: Show beads opened/closed in snapshot detail

**Files:** `internal/ui/detail.go`

Add two sections to `renderSnapshotDetail`:

- **Closed** — list `snap.BeadsClosed` IDs (issues the implement agent completed)
- **Opened** — list `snap.BeadsOpened` IDs (issues the review agent filed)

Display as simple ID lists. Example:

```
Closed (3)
  bd-abc  bd-def  bd-ghi

Opened (2)
  bd-jkl  bd-mno
```

This gives concrete visibility into what each agent did, complementing the prose Summary/Review text.

## Scope Notes

- Step 1 is a UI-only change (~15 lines in detail.go)
- Step 2 is a prompt-only change (4 template files, ~2 lines each)
- Step 3 is a UI-only change (~20 lines in detail.go)
- No model/store changes needed — all data already exists in `Stream` and `Snapshot`
