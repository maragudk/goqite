# Diary: Bump `golang.org/x/text` to fix GO-2026-5970

The GitHub Security workflow (`govulncheck ./...`) was failing on main because of GO-2026-5970, an infinite-loop-on-invalid-input vulnerability in `golang.org/x/text` v0.31.0, held as an indirect dependency reached via `internal/testing/sqlite.go`'s `sql.Open` call. The fix ships in v0.39.0.

## Step 1: Bump `golang.org/x/text` and verify

**Author:** main

### Prompt Context

**Verbatim prompt:** "The GitHub Security workflow (runs `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`) is failing on main with vulnerability GO-2026-5970: infinite loop on invalid input in golang.org/x/text. We have v0.31.0 (indirect dependency, reached via internal/testing/sqlite.go's sql.Open call); fixed in v0.39.0. Task: 1. In this worktree, bump golang.org/x/text to v0.39.0 (`go get golang.org/x/text@v0.39.0 && go mod tidy`). 2. Verify the build and tests pass (`go build ./... && go test ./...`). 3. Run `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` locally and confirm it exits clean. 4. Commit on a branch (e.g. `security/bump-x-text`), following the fabrik:git skill conventions, push, and open a PR against main with a short description referencing GO-2026-5970 and the failing Security workflow. Report back the PR URL and govulncheck result."

**Interpretation:** A pure dependency-version bump to clear a known CVE flagged by CI, with no application code changes expected.

**Inferred intent:** Unblock the Security workflow on main and remove the vulnerable code path before it can be exploited or keep failing CI on future PRs.

### What I did
- Created branch `security/bump-x-text` off `main` (clean tree, at commit `0c03c05`).
- Ran `go get golang.org/x/text@v0.39.0 && go mod tidy` in `/Users/maragubot/Developer/goqite`. This bumped `golang.org/x/text` v0.31.0 → v0.39.0 and, as a side effect of `go mod tidy` resolving the module graph, also bumped `golang.org/x/sync` v0.18.0 → v0.21.0. Both are indirect dependencies; `go.mod` and `go.sum` are the only files touched.
- Ran `go build ./...` (passed) and `go test ./...` (all packages passed: `maragu.dev/goqite`, `maragu.dev/goqite/http`, `maragu.dev/goqite/jobs`; `internal/sql` and `internal/testing` have no test files).
- Ran `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`, which reported "No vulnerabilities found."
- Committed the change with message "Bump `golang.org/x/text` to v0.39.0" referencing GO-2026-5970, pushed the branch, and opened PR #79 against `main`: https://github.com/maragudk/goqite/pull/79 (reviewer: `markuswustenberg`, per the git skill's default).
- Checked PR CI status via `gh pr checks 79`; Lint, Test, and govulncheck workflows were queued/pending at the time of this entry (not yet confirmed green in CI, though all three passed locally).

### Why
`go mod tidy` is the standard way to bring the module graph to a minimal, consistent state after bumping a dependency, and it's what pulled in the `golang.org/x/sync` bump alongside the intended `golang.org/x/text` change — that's expected, not a mistake.

### What worked
Everything went smoothly: the bump, build, tests, and govulncheck all succeeded on the first try, with no code changes required beyond `go.mod`/`go.sum`.

### What didn't work
Nothing failed. No errors were encountered during the bump, build, test, or govulncheck runs.

### What I learned
`go mod tidy` after `go get` on a single indirect dependency can pull in unrelated transitive upgrades (here, `golang.org/x/sync`) to keep the module graph minimal and consistent — worth calling out explicitly in the commit/PR so a reviewer isn't surprised by an unrequested version bump.

### What was tricky
Nothing was tricky. This was a straightforward, low-risk dependency bump with no source code impacted.

### What warrants review
- `/Users/maragubot/Developer/goqite/go.mod` and `/Users/maragubot/Developer/goqite/go.sum`: confirm the `golang.org/x/text` v0.39.0 and `golang.org/x/sync` v0.21.0 bumps are acceptable, and that no other transitive changes crept in beyond what's shown in the diff.
- PR #79 CI (Lint, Test, govulncheck workflows) should be confirmed green before merging, since this diary entry was written while those checks were still pending.

### Future work
None identified — this closes out the GO-2026-5970 gap on the Security workflow.
