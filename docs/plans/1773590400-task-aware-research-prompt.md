# Task-Aware Research Prompt

## Problem

The `research-implement` prompt template has a rigid `Steps:` section that overrides methodology instructions in the user's task description. When a user writes a task like:

> "To see how that feature works, you can run the app yourself and probe it with the chrome-devtools-mcp. Run `ruby app.rb` in that directory, visit the app at http://localhost:4000..."

...the research agent ignores those instructions because the prompt's steps say "1. Explore the codebase structure — key directories, packages, and modules." The agent follows the explicit steps, not the free-form task text.

This was observed in a real stream (`plentish-v1-nmz`) where the task explicitly required running a Sinatra app and interacting with it via browser DevTools. The research agent spent 4 iterations doing static code reading. The first review even flagged the gap ("the app was never actually run"), but the implement agent addressed the feedback by reading *more* source code rather than actually running the app — because the prompt's steps only described code-reading methodology.

### Root cause

The `Steps:` section in `research-implement.tmpl` presents a **competing methodology** to whatever the task describes. The task says *how* to research; the prompt says *how* to research; the agent picks the prompt's steps because they're structured and explicit.

### Why this matters

Research quality directly affects every downstream phase. If research misses behaviors that are only observable through live interaction (animations, focus management, timing-dependent UX, empty states), the plan will have gaps, the implementation will be wrong, and the review will file issues that could have been avoided.

## Solution

Rewrite the `Steps:` section of `research-implement.tmpl` to defer to the task description for research methodology. The task's instructions become primary; the template's steps become fallback defaults.

Also update `research-review.tmpl` to verify that the research agent followed any task-specified methodology, not just whether the content is accurate.

No new fields, no new data structures, no code changes outside of the two `.tmpl` files and their corresponding tests.

## Files to Change

1. `internal/loop/prompts/research-implement.tmpl` — rewrite Steps section
2. `internal/loop/prompts/research-review.tmpl` — add methodology compliance check
3. `internal/loop/prompts_test.go` — update/add tests for the new prompt wording

## Current State of Each File

### `internal/loop/prompts/research-implement.tmpl` (current)

```
{{if eq .Iteration 0 -}}
You are researching a codebase to gather context for an upcoming task. Write your findings to research.md in the working directory.

Task: {{.Task}}

Steps:
1. Explore the codebase structure — key directories, packages, and modules.
2. Identify the files and components most relevant to the task.
3. Note existing patterns, conventions, and architectural decisions.
4. Document any constraints, dependencies, or risks.

Rules:
- Do not write code. Do not commit.
- Do not create beads issues — that's the review step's job.
- Focus on facts about the codebase, not on planning solutions.
{{- else -}}
You are revising your research based on feedback. The feedback is tracked as child issues under the parent beads issue.

Task: {{.Task}}
Parent issue: {{.ParentID}}

Steps:
1. Run: bd show {{.ParentID}} --children
2. For each open child issue, read it, update research.md accordingly, and close it with bd close.

Rules:
- Do not write code. Do not commit.
- Do not create new beads issues.
- Focus on facts about the codebase, not on planning solutions.
{{- end}}
```

### `internal/loop/prompts/research-review.tmpl` (current)

```
You are reviewing research gathered for a software task. Your job is to file specific, actionable issues for missing or incorrect research — not to make changes yourself.

Task: {{.Task}}
Parent issue: {{.ParentID}}

Steps:
1. Read research.md.
2. Evaluate: Does the research cover the relevant parts of the codebase? Are there blind spots? Is anything inaccurate or superficial?
3. For each improvement, file a child issue:
   bd create --parent {{.ParentID}} --title="<specific action>" --type=task --priority=2 --description="<what to research and why>"
4. After your review, write a brief summary (2-4 sentences) of your overall assessment as your FINAL output. If you filed issues, summarize what you found. If the research is sufficient with no issues filed, respond with exactly: "No further improvements needed."

IMPORTANT: Your summary text MUST be the very last thing you output. Do not end on a tool call. The summary is captured and displayed to the human operator.

Rules:
- Do NOT edit any files.
- Maximum 5 issues per review.
```

## Changes

### 1. `internal/loop/prompts/research-implement.tmpl`

Replace the iteration-0 branch. The revision branch (iteration > 0) stays the same — it already works by addressing specific feedback beads.

**New iteration-0 content:**

```
{{if eq .Iteration 0 -}}
You are researching a codebase to gather context for an upcoming task. Write your findings to research.md in the working directory.

Task: {{.Task}}

Steps:
1. Read the task description above carefully. If it specifies a research methodology — running an application, using browser tools, probing specific workflows, testing endpoints, etc. — follow that methodology. Those instructions take priority over the defaults below.
2. Explore the codebase structure — key directories, packages, and modules relevant to the task.
3. Identify the files and components most relevant to the task.
4. Note existing patterns, conventions, and architectural decisions.
5. Document any constraints, dependencies, or risks.

Rules:
- Do not write code. Do not commit.
- Do not create beads issues — that's the review step's job.
- Focus on facts about the codebase, not on planning solutions.
{{- else -}}
```

The rest of the template (the `else` branch for revision iterations) is unchanged.

Key changes:
- Step 1 is new: explicitly tells the agent to read the task for methodology instructions and follow them with priority over the default steps.
- Steps 2-5 are the original steps 1-4, renumbered, now serving as defaults for areas the task doesn't cover.
- The framing "Those instructions take priority over the defaults below" is critical — it resolves the competing-methodology problem by establishing a clear hierarchy.

### 2. `internal/loop/prompts/research-review.tmpl`

Add a methodology compliance check to the review evaluation criteria.

**New content:**

```
You are reviewing research gathered for a software task. Your job is to file specific, actionable issues for missing or incorrect research — not to make changes yourself.

Task: {{.Task}}
Parent issue: {{.ParentID}}

Steps:
1. Read research.md.
2. Read the task description above. If it specifies a research methodology (running an app, using browser tools, testing workflows, etc.), verify that the research shows evidence of actually following that methodology — not just documenting the same information through code reading alone.
3. Evaluate: Does the research cover the relevant parts of the codebase? Are there blind spots? Is anything inaccurate or superficial?
4. For each improvement, file a child issue:
   bd create --parent {{.ParentID}} --title="<specific action>" --type=task --priority=2 --description="<what to research and why>"
5. After your review, write a brief summary (2-4 sentences) of your overall assessment as your FINAL output. If you filed issues, summarize what you found. If the research is sufficient with no issues filed, respond with exactly: "No further improvements needed."

IMPORTANT: Your summary text MUST be the very last thing you output. Do not end on a tool call. The summary is captured and displayed to the human operator.

Rules:
- Do NOT edit any files.
- Maximum 5 issues per review.
```

Key changes:
- New step 2 tells the reviewer to check whether the task's specified methodology was actually followed. This closes the gap where the original stream's reviewer flagged the issue once but then accepted code-reading-based content as a fix on the second pass.
- The parenthetical "not just documenting the same information through code reading alone" is important — it prevents exactly the failure mode observed in the `plentish-v1-nmz` stream, where the agent addressed a "run the app" bead by reading more JavaScript source files instead.
- Original steps 2-4 become steps 3-5, renumbered.

### 3. `internal/loop/prompts_test.go`

Update the existing test that checks for embedded default research content, and add a new test verifying the methodology-first language is present.

**Add this test:**

```go
func TestLoadPrompt_ResearchImplementMethodologyFirst(t *testing.T) {
	original := userPromptsDir
	userPromptsDir = func() string { return "" }
	defer func() { userPromptsDir = original }()

	data := PromptData{
		Task:      "Run the app with `ruby app.rb` and probe it with chrome-devtools-mcp",
		Iteration: 0,
	}

	prompt, err := LoadPrompt("research", "implement", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The prompt should instruct the agent to follow task-specified methodology first.
	if !strings.Contains(prompt, "follow that methodology") {
		t.Error("expected research-implement to instruct following task-specified methodology")
	}

	// The task content should appear in the rendered prompt.
	if !strings.Contains(prompt, "chrome-devtools-mcp") {
		t.Error("expected prompt to contain the task description")
	}
}

func TestLoadPrompt_ResearchReviewMethodologyCompliance(t *testing.T) {
	original := userPromptsDir
	userPromptsDir = func() string { return "" }
	defer func() { userPromptsDir = original }()

	data := PromptData{
		Task:     "Run the app and test all endpoints",
		ParentID: "test-parent",
	}

	prompt, err := LoadPrompt("research", "review", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The review prompt should check methodology compliance.
	if !strings.Contains(prompt, "verify that the research shows evidence") {
		t.Error("expected research-review to check methodology compliance")
	}
}
```

No existing tests need modification — they test plan-implement and coding-rebase templates, which are unaffected. The `TestLoadPrompt_EmbeddedDefault` and `TestLoadPrompt_EmbeddedDefaultSubsequentIteration` tests use plan-implement, not research-implement, so they pass unchanged.

## What NOT to Change

- **No new fields on `PromptData`** — the task description already carries the methodology instructions via `{{.Task}}`. The problem was never missing data; it was the prompt ignoring data it already had.
- **No changes to `prompts.go`, `research.go`, `phase.go`, or `orchestrator.go`** — this is purely a template wording fix.
- **No changes to other phase templates** — plan, decompose, and coding phases don't have the same problem because their task descriptions rarely contain methodology instructions. If this pattern proves useful, it can be extended to other phases later.
- **No changes to the TUI, CLI, or stream creation flow.**

## Verification

After making the changes:

```bash
cd ~/zm_apps/streams
go test ./internal/loop/ -run TestLoadPrompt -v
```

All existing tests should pass unchanged, plus the two new tests should pass.

## Background: The Stream That Exposed This

Stream `plentish-v1-nmz` in the `plentish-v1` project was tasked with researching a Sinatra app's shopping list feature for re-implementation in Rails. The task explicitly said to run the app and probe it with chrome-devtools-mcp. Over 4 research iterations ($6.68 total cost), the agent:

1. Did pure code reading (iteration 1)
2. Was told by the reviewer to actually run the app (bead .1 filed)
3. "Addressed" the bead by reading more JavaScript/HTML files, not by running the app (iteration 2)
4. The reviewer accepted the richer content and moved on to code-level nitpicks (iterations 3-4)
5. Research converged without ever running the app

The per-stream fix (a prompt override that explicitly instructs the agent to use chrome-devtools-mcp) has already been applied to that specific stream. This plan addresses the systemic issue so future streams with task-specified methodology don't hit the same failure mode.
