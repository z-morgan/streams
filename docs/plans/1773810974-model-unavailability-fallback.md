# Plan: Model Unavailability Fallback

## Problem

The existing fallback mechanism only triggers on rate-limit errors (HTTP 429, "overloaded", etc.). When a primary model becomes fully unavailable via server-side errors (e.g. HTTP 500), the stream records a runtime error and stops rather than falling back to the configured backup model.

Additionally, `runSlots` (the polish phase path) has no fallback logic at all.

## What already works correctly

Each step (implement, review, and each slot) already tries the primary model independently at the start of every call. This means "retry primary on each new agent instantiation" is already the natural behavior — no structural changes needed for that part of the requirement. The fix is purely about expanding *which errors* trigger a fallback.

## Approach

### Step 1: Add `ErrUnavailable` error kind (`stream/errors.go`)

Add a new `ErrUnavailable` constant between `ErrRateLimit` and the end of the `iota` block, with display name `"Unavailable"`. This keeps the error distinct from rate limits in the UI ("Unavailable at coding/Implement: ...") while enabling the same fallback behavior.

### Step 2: Expand `classifyError` to detect 5xx errors (`loop/loop.go`)

Add patterns to `classifyError` that map to `ErrUnavailable`:
- `"500"` — generic HTTP 500 in CLI stderr
- `"internal server error"` — HTTP 500 descriptive text
- `"503"` — service unavailable
- `"service unavailable"` — descriptive
- `"529"` — Anthropic-specific overloaded (not yet covered by "overloaded")

Move "overloaded" from the `ErrRateLimit` list to `ErrUnavailable` (it semantically means the server is overwhelmed, not that *this user* hit a limit — though behavior is the same).

Add a small helper `isFallbackEligible(kind stream.ErrorKind) bool` that returns true for both `ErrRateLimit` and `ErrUnavailable`. Use it in place of `kind == stream.ErrRateLimit` in all three fallback sites.

### Step 3: Add fallback to `runSlots` (`loop/loop.go`)

`runSlots` currently has no fallback logic — an unavailable primary model causes a slot to error-snapshot and continue. Add the same try-primary-then-fallback pattern used in the main loop's implement/review steps.

### Step 4: Improve output messages

Change the output line from:
```
[streams] Rate limit hit — falling back to <model>
```
to something that distinguishes rate limits from unavailability:
```
[streams] Model unavailable (rate limit) — falling back to <model>
[streams] Model unavailable (server error) — falling back to <model>
```

### Step 5: Tests

- Update `loop_test.go` to add a test case where the primary model returns a 500-like error and verify the fallback model is used.
- Add a test that a `ErrRuntime` error (e.g. a bad prompt / CLI crash) does NOT trigger fallback.

## File changes

| File | Change |
|------|--------|
| `internal/stream/errors.go` | Add `ErrUnavailable` constant and display name |
| `internal/loop/loop.go` | Expand `classifyError`, add `isFallbackEligible`, add fallback to `runSlots`, update log messages |
| `internal/loop/loop_test.go` | Tests for new fallback triggers |

## What does NOT change

- `FallbackConfig` struct — no new fields needed
- `stream/stream.go` — `SetFallback`/`GetFallback` unchanged
- The "retry primary on next step" behavior — already correct; each step calls primary independently
- Rate limit detection strings — keep as-is in `ErrRateLimit` (except moving "overloaded" if desired)
