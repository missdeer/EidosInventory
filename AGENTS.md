# AGENTS.md — 6-rule

These rules apply to every task in this project unless explicitly overridden.
Bias: caution over speed on non-trivial work. Use judgment on trivial tasks.
Each rule ends with a ❌/✅ pair — match the pattern, not the slogan.

## Rule 0 — Modern CLI only (enforced by PreToolUse hook)
Mappings: `find`→`fd`, `grep`→`rg`, `cat`→`bat` (or Read), `ls`→`eza`, `diff`→`delta`, JSON parsing → `jq` (never `python -c "import json"`).
The hook `.claude/hooks/legacy-cli-pretool.sh` will **deny** any Bash call whose first segment-token is a legacy tool and tell you the replacement — reissue with the modern equivalent, don't retry the same command. `git grep` / `git diff` are fine.
On Windows, `fd` is backed by the Everything search index, so a filename search returns instantly even across a whole drive. Search roots are per-drive (`C:\`, `D:\`, …), not a unified `/` — pass the drive root you want, e.g. `fd -t f 'mingw32-make' 'D:\'`. Prefer reaching for `fd` first whenever you need to locate a file.
Use the built-in file-editing tools; never edit files with external tools like `perl` / `awk` / `sed`. Every file edit — including bulk renames and repeated substitutions across a file — goes through Edit / Write / NotebookEdit, so the change is reviewable as a diff.
- ❌ `find . -name "*.go" | xargs grep TODO`
- ✅ `fd -e go -x rg TODO`

## Rule 1 — Think, ask, surface conflicts
State material assumptions before coding. Ask only when ambiguity changes acceptance criteria, permissions, irreversible outcomes, or would cause substantial rework; otherwise state a reasonable assumption and continue. Reuse authorization already given for the same scope. If two patterns in the codebase contradict, pick one (more recent / more tested), say why, flag the other for cleanup; never blend them. Use the model only for judgment work (classification, drafting, summarization, extraction); for routing / retries / deterministic transforms, write code — don't ask the model.
- ❌ Picking interpretation A and producing 200 lines of code for it; or writing a retry loop by prompting the model.
- ✅ "Using A because it matches the existing behavior; this choice is reversible and stays within the requested scope." Ask before proceeding if A and B imply different business outcomes. / Retries live in a `for` loop with explicit backoff.

## Rule 2 — Minimal, surgical, conformant changes
Smallest diff that solves the stated problem. No speculative features, no abstractions for single-use code, no "improvements" to adjacent code / comments / formatting. Match the codebase's existing style even if you disagree — if you genuinely think a convention is harmful, surface it; don't fork silently. Senior-engineer test: would they call this overcomplicated or out-of-scope? If yes, simplify.
- ❌ Bug fix that also renames variables in nearby functions "while we're here", or introduces a `Strategy` interface for one caller.
- ✅ Smallest diff that fixes the bug; new abstraction only when ≥2 real call sites exist.

## Rule 3 — Read before you write
Read the change site and the direct dependencies needed to understand its behavior. Before adding a helper, search the relevant shared utilities in `libs/` and existing callers; do not read unrelated modules by default. Confirm a new helper has a real call site before committing it; newly introduced `unusedfunc` / `unusedparams` are blocking findings, not advisories.
- ❌ Writing `parseDate()` helper and trusting nothing similar exists.
- ✅ `rg -i 'parseDate|ParseDate' libs/ tools/` first, then either reuse or add.

## Rule 4 — Goal-driven loop
Define success criteria up front, then iterate until verified. Don't follow a fixed step list — strong criteria let you self-correct. For feature work, "compiles" and "`go vet` clean" are not "feature works" — exercise the actual behavior and cite the evidence (command run, output observed).
- ❌ "Done — `go build ./...` passes."
- ✅ "Criteria: import R41 into `dewu-burgeon-sales-daily` for week N. Ran `./bin/...`; row count matches source xlsx (1,234); spot-checked 3 rows against `usage.md` query."

## Rule 5 — Report honestly: checkpoint, fail loud, tests verify intent
**Checkpoint** after each significant step — what's done, what's verified, what's left; if you lose track, stop and restate. **Fail loud** — "completed" is wrong if anything was skipped silently; "tests pass" is wrong if any were skipped or marked `t.Skip`; surface uncertainty, don't hide it. **Tests encode intent**, not just behavior — a test that can't fail when the business rule changes is broken; assert *why* the value matters (the rule), not just *what* it is right now.
- ❌ "All 3 subtasks done!" when subtask 2 silently fell through to a default, or a test that just re-encodes the current return value with no link to the business rule.
- ✅ "2 of 3 done. Subtask 2 hit Y — need your call on Z before continuing." / `assert sale_price == cost * (1 + REQUIRED_MARGIN)` instead of `assert sale_price == 13.75`.

# Project Overview

## Repository Layout

## Database

# Build

# Go Coding Standards

`go.mod` declares `go 1.26.1`. When writing Go in this repo, reach directly for the idioms introduced in 1.20 / 1.21 / 1.22 / 1.24 so that `gopls` / `staticcheck` / `go vet` do not flag the code as needing modernization. The rules below cover the lint findings actually observed in this repo; write code in modern form **on first draft**, not after lint complains.

## Idioms you must use by default

- **`for i := range N` (Go 1.22+)**: replace `for i := 0; i < N; i++` with an integer `range`. If `i` is unused, write `for range N`.
  - Only applies to step-1 C-style loops with a single upper-bound expression and no `<=`. Loops with step ≠ 1, compound conditions (`i<a && i<b`), or `<=` upper bounds stay in their original form.
  - Caveat: `range N` evaluates `N` once at loop start. If `N` is a pure read-only getter like `wb.NumSheets()` and the loop body does not mutate the receiver, the conversion is safe; otherwise keep the C-style form.
- **`strings.SplitSeq` / `strings.FieldsSeq` (Go 1.24+)**: when you only need to iterate the split results in order and do not need a slice or its length, use `for x := range strings.SplitSeq(s, sep)` instead of `for _, x := range strings.Split(s, sep)` — it avoids one intermediate slice allocation.
  - Keep `strings.Split` when you need `len()`, random indexing, or positional access like `parts[0]` / `parts[1]`.
- **`min` / `max` builtins (Go 1.21+, `minmax` hint)**:
  - `if a > b { a = b }` → `a = min(a, b)`
  - `if a < b { a = b }` → `a = max(a, b)`
  - Nested `if` chains computing bounds across several values likewise — `min(a, b, c)` accepts any number of arguments.
- **`copy(dst, src)` (`S1001`)**: always use `copy` for bulk slice copies; never write `for i := range src { dst[i] = src[i] }`.
- **`strings.Cut` / `strings.CutPrefix` / `strings.CutSuffix` (Go 1.20+, `stringscut` / `stringscutprefix` hints)**:
  - `if i := strings.Index(s, sep); i >= 0 { left, right := s[:i], s[i+len(sep):]; ... }` → `if left, right, ok := strings.Cut(s, sep); ok { ... }`
  - `if strings.HasPrefix(s, p) { s = strings.TrimPrefix(s, p); ... }` → `if rest, ok := strings.CutPrefix(s, p); ok { ... }`. Likewise use `CutSuffix` for the `HasSuffix` + `TrimSuffix` pattern.
- **`any` instead of `interface{}`** (Go 1.18+).
- **`errors.Is` / `errors.As`** for error comparison — never `err == target` or naked type assertions. Wrap errors with `fmt.Errorf("...: %w", err)`.

## Common lint warnings to avoid up front

- **`unusedfunc` / `unusedparams`**: every new function or parameter must have a caller or a clear reason. When a parameter exists only to satisfy an interface, name it `_`. Don't commit a helper that has no call site yet — confirm the caller first.
- **Pre-allocate slices**: when the final length is known, use `make([]T, 0, n)` or `make([]T, n)` to avoid repeated `append` reallocations.
- **`context.Context` is always the first parameter** of a function. Do not stash it in a struct field for long-term storage.
- **Goroutine exit paths must be explicit before launch**: choose exactly one of `WaitGroup`, channel close, or `ctx` cancellation, and make it obvious.
- **`fmt.Errorf` wraps at most one error with `%w`**; combine multiple errors with `errors.Join` (Go 1.20+).

## Looking up Go package / module documentation

For any module or package published on **pkg.go.dev** — synopsis, exported symbols, version list, docs, imports, importers, vulnerabilities, or full-text search — use the **`pkg-go-dev` skill** (which calls the official `https://pkg.go.dev/v1beta` REST API). Do not fall back to `go doc`, `go list -m -versions`, or scraping pkg.go.dev HTML for this purpose.

`go doc` / `go list` remain the right tools for code that lives in the **local working tree or local module cache** (unpublished helpers, in-progress changes, vendored or replaced modules). Rule of thumb: if a `pkg.go.dev` URL would resolve to the answer, use the skill; if the answer lives only on disk, use `go doc`.

## Lint / formatting gates

For Go changes, check formatting of changed Go files and vet the affected packages before commit:

```bash
gofmt -l <changed-go-files>
go vet <affected-packages>
```

Expand checks when shared dependencies or build configuration change; use `go vet ./...` when the impact is repository-wide. Documentation-only changes do not require Go checks. Reuse passing results for the same source and relevant configuration state unless new evidence calls them into question.

If `staticcheck` / `golangci-lint` are available, apply them to the same scope. Fix findings introduced by this change, including modernize hints, without `//nolint`. Report pre-existing findings separately; do not expand the diff to fix them or treat them as new regressions. State which checks passed, failed, or could not run.

# Temporary Directory

Always use **${project_root_dir}/tmp** as temporary directory, never use **/tmp**.

# Environment & Pre-configuration Informations

