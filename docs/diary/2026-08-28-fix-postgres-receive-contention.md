# Diary: Fix Postgres receive contention (serialization errors, alternative to PR 72)

goqite users on Postgres see SQLSTATE 40001 serialization failures under concurrent `Receive`/`Delete` load (reported and analyzed in PR 72 by adityaj-amagi). This task fixes the root cause with minimal Postgres-specific machinery, keeping SQLite (the primary audience) provably untouched.

## Step 1: Requirements and design (brainstorm with Markus)

**Author:** main (lead)

### Prompt Context

**Verbatim prompt:** "Yes, let's figure out the best way to fix the root cause. My primary audience is still SQLite database users, Postgres is a nice add-on. So I want to keep Postgres-specific machinery to a minimum. Let's figure out an approach and end up in our own PR fixing the issue, but linking to PR 72."
**Interpretation:** Design an alternative fix to PR 72's (read committed + skip locked + retry loop), biased toward less machinery, and ship it as our own PR crediting/linking PR 72.
**Inferred intent:** Fix the Postgres 40001 storms permanently without complicating the library or risking any change to SQLite behavior.

### What I did

Brainstormed the design with Markus, then dispatched a research sub-agent to verify driver behavior. Agreed design:

1. Keep `sql.LevelSerializable` for SQLite; use driver-default isolation (`BeginTx(ctx, nil)`, i.e. read committed) for Postgres only. `InTx` becomes flavor-aware.
2. Add `FOR UPDATE SKIP LOCKED` (CTE form) to the Postgres receive query.
3. No retry loops, no backoff/jitter, no pgx import into library code, no new exported API, no isolation config option.
4. Deterministic red/green Postgres-gated test: lock the head message's row in a raw uncommitted transaction, call `Receive` with a short context timeout, assert it returns the *second* message. Red on main (blocks until timeout), green with skip locked. The 50-worker repro from PR 72 stays a manual pre-merge check, not committed.

### Why

Serializable isolation is what converts ordinary receive contention into 40001 errors on Postgres, and it protects nothing there once the receive query uses skip locked (single-statement send/delete/extend need no isolation help). Read committed + `FOR UPDATE SKIP LOCKED` is the canonical Postgres job-queue pattern.

### What worked

The research agent (Fable) proved the key safety facts from driver source:

- mattn/go-sqlite3 v1.14.28 discards `TxOptions` entirely (`sqlite3_go18.go:41-44` — `opts` never read), so isolation changes are a provable no-op there; same for modernc.org/sqlite.
- ncruces/go-sqlite3 *does* honor `LevelSerializable` (maps it to `BEGIN IMMEDIATE`); dropping the option globally would downgrade ncruces users to deferred `BEGIN` and risk `BUSY_SNAPSHOT` errors. This is why the change is Postgres-flavor-only rather than a global `BeginTx(ctx, nil)`.
- Independently confirmed the double-delivery race: at read committed *without* skip locked, the receive query's `update ... where id = (select ... limit 1)` can deliver the same message twice (subquery is an InitPlan, EvalPlanQual re-check still matches). The isolation change and the skip-locked query change must land together, never separately.

### What didn't work

Considered and rejected: an in-process mutex (doesn't cross processes, kills concurrency), `pg_advisory_xact_lock` (serializes globally), keeping serializable + retry loop (carries retry/backoff/error-sniffing machinery forever to guard an error we chose to keep possible), PR 72 as-is (right direction, but its retry machinery is dead weight at read committed).

### What I learned

`internal/sql/tx.go:10` is the only isolation mention in the entire repo; nothing in README, docs, examples, or tests assumes serializable. Users composing their own serializable transactions via the `*Tx` methods own their own retries — unchanged from today.

### What was tricky

The ncruces driver caveat: "SQLite is always serializable so the option is decorative" is true for mattn and modernc but false for ncruces, where the option selects the BEGIN variant. Caught only by reading driver source.

### What warrants review

That the flavor-aware `InTx` stays minimal (one branch, no new exported API), and that the red/green test genuinely fails on main before the fix.

### Future work

Respond on PR 72 crediting the analysis and linking our PR (Markus to decide wording; no comments posted without instruction).

## Step 2: Implement the fix, red/green test, and validation

**Author:** builder-pg-contention

### Prompt Context

**Verbatim prompt:** "You are building a fix in the goqite repository. […] 1. **Flavor-aware isolation in `InTx`.** Keep `sql.LevelSerializable` for SQLite (must remain byte-for-byte the same BeginTx call […]). For Postgres, use driver-default isolation: `db.BeginTx(ctx, nil)` […] 2. **`FOR UPDATE SKIP LOCKED` in the Postgres receive query** […] 3. **These two changes must land together** […] 4. **Explicitly forbidden:** retry loops, backoff/jitter, importing pgx […], error-code/SQLState sniffing, mutexes, config options for isolation, any new exported API."
**Interpretation:** Build exactly the design from Step 1 — two coupled changes, one deterministic Postgres test proven red on main and green after, plus a manual stress run against PR 72's repro — with no retry machinery and no new exported API.
**Inferred intent:** Ship the smallest change that removes 40001 storms for Postgres users, with evidence a reviewer can re-run, and leave SQLite provably untouched.

### What I did

Wrote the test first, in `/goqite_test.go` as a subtest of `TestQueue_Receive` (Postgres-only, so it sits alongside the existing table-driven "does not receive a message from a different queue" subtest rather than going through `internaltesting.Run`). It sends two messages, gives the first the higher priority so it is unambiguously the head of the queue, locks that row with `select … for update` in a raw transaction on a second connection that is never committed, and then calls `Receive` with a two-second context. Ran it against main's code and confirmed red.

Then made the two coupled changes:

- `/internal/sql/tx.go`: `InTx` grew an `opts *sql.TxOptions` parameter passed straight to `BeginTx`, so nil leaves the isolation level to the connection. The package is internal, so no exported surface leaked.
- `/goqite.go`: added an unexported `(*Queue).inTx` that picks `&sql.TxOptions{Isolation: sql.LevelSerializable}` for `SQLFlavorSQLite` and nil for `SQLFlavorPostgreSQL`, and rewired all five `internalsql.InTx` call sites to it. The SQLite `BeginTx` call is byte-for-byte what it was.
- `/goqite.go`: rewrote the `SQLFlavorPostgreSQL` receive query into the CTE form with `for update skip locked`, leaving parameter numbering and the `returning id, body` clause as they were. The SQLite query is untouched.
- Updated the three remaining `InTx` callers for the new signature. All three pass serializable, so their behaviour is exactly what it was.

Validated with the full suite, `go vet ./...`, `golangci-lint run`, and two manual Postgres runs described below.

### Why

`InTx` had no way to vary isolation, and forcing serializable is what turned ordinary head-of-queue contention on Postgres into 40001. Passing `*sql.TxOptions` through rather than a flavor or a bool keeps `internal/sql` ignorant of queue concepts, and it puts the decision where the flavor already lives. Putting the branch in a `Queue` method keeps the five call sites in `/goqite.go` as short as they were.

### What worked

Red was unambiguous. Against main's code, with the fix stashed:

```
=== RUN   TestQueue_Receive/postgresql_skips_a_message_locked_by_another_transaction_and_receives_the_next_one
    goqite_test.go:251: Expected nil error, but got "cannot roll back tx after error (tx error: sql: transaction has already been committed or rolled back), original error: timeout: context deadline exceeded" (type *fmt.wrapError)
--- FAIL: TestQueue_Receive/postgresql_skips_… (2.22s)
```

Receive blocks on the held lock for the full two seconds. With the fix:

```
--- PASS: TestQueue_Receive/postgresql_skips_a_message_locked_by_another_transaction_and_receives_the_next_one (0.14s)
```

The stress repro from PR 72 (fetched with `gh pr diff 72`, run as a scratch file, then deleted) is stark. 50 workers looping send/receive/delete for 20 seconds against a real Postgres 17, on main's code:

```
delivered=3600 serialization errors=79880 other errors=0
```

and with the fix:

```
delivered=78182 serialization errors=0 other errors=0
```

Zero 40001, and a 21x throughput improvement — the serialization failures were not merely noisy, they were consuming almost all the work the queue did.

A second scratch check, also deleted, guarded the double-delivery risk from Step 1: 2000 messages, a one-minute visibility timeout so nothing is legitimately redelivered, 50 concurrent receivers draining the queue. Result `unique=2000 total=2000 duplicates=0`.

The full suite passes for both flavors: `go test -shuffle on ./...` gives `ok maragu.dev/goqite`, `ok maragu.dev/goqite/http`, `ok maragu.dev/goqite/jobs`. `go vet ./...` is clean and `golangci-lint run` reports `0 issues.`

### What didn't work

Nothing failed in a way that required rework. Two environmental snags: `docker compose up -d` refused with `Bind for 0.0.0.0:5433 failed: port is already allocated` because another project's identical `postgres:17` test container already holds 5433, which the suite is hardcoded to use — its `template1` already carried the current `goqite` schema, so the tests ran against it unchanged. And `go test -bench 'BenchmarkQueue/send'` panics with `panic: db cannot be nil` from `/goqite_test.go:374`, where the benchmark builds a queue with an empty `NewOpts`. That is pre-existing on main and untouched by this change.

### What I learned

The double-delivery window that makes these two changes inseparable is narrower than the Step 1 note suggests, and it took some thought to see why a scratch test could not easily demonstrate it. When a second receiver's EvalPlanQual re-check runs against the row the first receiver just updated, the re-check evaluates the whole `where` clause — including `$3 >= timeout`, and `timeout` has just been pushed into the future. So with any non-trivial queue timeout the losing receiver filters the row out and returns nothing. The duplicate needs a near-zero timeout to land. That does not weaken the rule (ship both or neither), but it explains why the failure mode reported in the wild is 40001 rather than duplicates.

### What was tricky

Getting the ordering guarantee out of the test's control. My first version relied on the two messages' `created` timestamps to decide which was the head of the queue, which is true in practice but leaves the test passing vacuously if it ever were not. Giving the locked message `Priority: 1` makes `order by priority desc, created` decide it outright.

The other subtlety is that `is.NotError` on `Receive` is what catches the regression, not the body assertion — on main the call never returns a message at all, it returns a wrapped `context deadline exceeded`. Worth knowing when reading the failure.

### Self-review

Ran two competing reviewers over the diff, both with a live Postgres. They independently confirmed the parts that matter: `EXPLAIN` on the new query gives `Update → CTE next → Limit → LockRows → Sort`, with `LockRows` *below* `Limit`, which is the property the whole fix rests on — skipped rows do not consume the limit. Both also confirmed no double delivery under concurrency, that the SQLite `BeginTx` call is value-identical, and that the test is genuinely red without the fix.

They then converged on the same defects, which I fixed:

- The comment on `inTx` reached behind `database/sql` into driver internals ("which some drivers use to pick the BEGIN variant"), claimed something false ("the driver default, read committed" — see below), and carried the justification for code that is no longer there. Rewritten to describe only what the code does.
- `/internal/testing/postgres.go` had gratuitously dropped to nil options while its SQLite twin kept serializable. Nothing in the fix needed that, so it now passes serializable and the diff stays mechanical.
- The test used `ExecContext` for a row-returning `select … for update`, which is driver-dependent. It now uses `QueryRowContext` and asserts the scanned id is the one it meant to lock, turning a dead variable into a real assertion.
- The test never checked that the skipped message survives. It now rolls the blocking transaction back and receives "held hostage", proving the message was skipped rather than lost.
- `if q.flavor == SQLFlavorSQLite` became a `switch`, matching every other flavor branch in the file, so a future third flavor has to make a deliberate choice instead of silently inheriting.
- Both flagged two user-visible semantics with no documentation. `ReceiveTx` does not go through `inTx`, so it runs at the caller's isolation and now holds an explicit row lock for the caller's whole transaction; its doc comment says so. And `skip locked` lets a consumer step over rows its peers hold, so `Priority` is best-effort under concurrency — one reviewer measured 57 out-of-order deliveries out of 200 with 16 consumers. The field comment says so now.

### What didn't work in review

One finding I did not act on, because acting on it would contradict an explicit instruction, and it is the main thing I need Markus to decide. Both reviewers independently proved that `BeginTx(ctx, nil)` does *not* mean read committed: it inherits `default_transaction_isolation`, which Postgres allows setting per-server, per-database, per-role, or through connection parameters. One reviewer ran `alter database … set default_transaction_isolation = 'serializable'` and got 15 serialization failures from 16 concurrent receivers over 300 messages — `skip locked` skips *locked* rows but does not save you from SSI's read/write dependency checks. A user with a hardened Postgres would get exactly the bug this change closes, with no signal as to why.

The one-line alternative is `opts = &sql.TxOptions{Isolation: sql.LevelReadCommitted}`, which breaks none of the constraints and makes the intent independent of server configuration. I left the nil as specified and made the comment honest about what nil actually means, rather than quietly overriding a user's deliberate global setting. Markus decided to pin it; see Step 3.

### What warrants review

The isolation question above, first. Then the `inTx` switch in `/goqite.go:86-101`, confirming the SQLite arm is what `InTx` used to do unconditionally, and the rewritten query at `/goqite.go:209-225` diffed against the untouched SQLite one above it.

The coverage gap is worth knowing: revert `inTx` to unconditional serializable tomorrow and the whole suite stays green, because the new test exercises only the skip-locked half — `skip locked` skips a locked row under serializable too. Pinning the isolation half needs a concurrency test, which was deliberately kept out of scope.

### Future work

Three things fell out of review, none of them blockers.

The README's "Using PostgreSQL" section says nothing about concurrency; a sentence on read committed plus `for update skip locked`, and on what overriding `default_transaction_isolation` does, would save a support round-trip. It is worth writing once the isolation question is settled, since its wording depends on the answer.

At read committed, a stale owner's `Extend` or `Delete` racing another consumer's `Receive` now silently succeeds where it used to abort with 40001. `jobs.Runner` keeps extending a message it may have lost, and its late `Delete` can drop one another runner is working on. Neither call carries an ownership token. The race is pre-existing and always live on SQLite; this change turns a loud error into quiet misbehaviour on Postgres, which belongs in the PR description.

Two pre-existing bugs, unrelated to this change: `BenchmarkQueue` panics with `db cannot be nil` from `/goqite_test.go:374`, and `rollback` in `/internal/sql/tx.go` treats `sql.ErrTxDone` as a rollback failure, which is why the red output above reads "cannot roll back tx after error" when the real error is the deadline.

## Step 3: Pin the PostgreSQL isolation level explicitly

**Author:** builder-pg-contention

### Prompt Context

**Verbatim prompt:** "Decision from the product owner: pin the isolation explicitly. For SQLFlavorPostgreSQL, use &sql.TxOptions{Isolation: sql.LevelReadCommitted} instead of nil — deterministic regardless of default_transaction_isolation. SQLite side stays exactly as is. […] If cheap, re-run your 16-receiver check against a database with default_transaction_isolation=serializable to confirm the pin holds; note the result in the diary either way."
**Interpretation:** Replace the nil options in the PostgreSQL arm of `inTx` with an explicit read committed, rewrite the comment to say why, and prove the pin holds on a hardened database.
**Inferred intent:** Close the last way this bug can reach a user — a Postgres they hardened themselves — instead of leaving the fix contingent on server configuration.

### What I did

Changed the `SQLFlavorPostgreSQL` arm of `inTx` in `/goqite.go` to `opts = &sql.TxOptions{Isolation: sql.LevelReadCommitted}` and rewrote its comment around the pairing: the receive query claims its row with `for update skip locked` rather than leaning on isolation, and pinning the level is what keeps that pairing intact where nil would inherit a server default that may have been raised. The SQLite arm is untouched.

Then built the check the decision deserves. Created a scratch database with `alter database goqite_hardened set default_transaction_isolation = 'serializable'`, had the test assert `show default_transaction_isolation` really returns `serializable` before doing anything else — so a green result cannot be green for the wrong reason — and ran 16 concurrent receivers draining 300 messages.

### Why

The Step 2 finding was that `BeginTx(ctx, nil)` promises nothing: it inherits whatever the server, database, role, or connection string sets. A fix that works only on a default-configured Postgres is a fix with a silent hole in it, and the hole opens for exactly the security-conscious operator least likely to suspect the queue library.

### What worked

The pin holds, and the check has teeth. On the hardened database:

```
server default_transaction_isolation = serializable
unique=300 duplicates=0 serialization errors=0 other errors=0
--- PASS: TestScratchHardenedPostgreSQL (0.19s)
```

Flipping the one line back to `opts = nil` and re-running against the same database:

```
receive: ERROR: could not serialize access due to concurrent update (SQLSTATE 40001)
unique=300 duplicates=0 serialization errors=3653 other errors=0
--- FAIL: TestScratchHardenedPostgreSQL (0.83s)
```

3,653 serialization failures against zero, same database, same code otherwise. The reviewers' finding was not theoretical.

Re-ran PR 72's 50-worker repro on a normally-configured database to confirm nothing regressed: `delivered=76058 serialization errors=0 other errors=0`. Full suite green for both flavors, `go vet` clean, `golangci-lint run` reports `0 issues.`

Both scratch files were deleted and both scratch databases dropped.

### What didn't work

Nothing. The change was one line and the validation behaved exactly as the reviewers predicted.

### What I learned

The failure mode on a hardened database is `could not serialize access due to concurrent update`, not the `read/write dependencies among transactions` that the original bug report and the unhardened repro produce. Both are 40001, but they come from different machinery — the first is a plain write conflict surfacing under serializable, the second is SSI's dependency tracking. Worth knowing when reading a user's bug report: the wording hints at whether their server is hardened.

### What was tricky

Only the discipline of making the negative case explicit. A test that passes on a hardened database proves nothing unless you also confirm the database is hardened and that the old code fails there — otherwise a misconfigured `alter database` yields a cheerful green that means nothing. Asserting `show default_transaction_isolation` inside the test, and running the nil variant against the same database, is what turned the result into evidence.

### What warrants review

Just the one line and its comment in `/goqite.go:94-99`. The pin also makes the Step 2 doc comment on `ReceiveTx` more pointed: `Receive` now guarantees read committed, while `ReceiveTx` still runs at whatever the caller chose, so the two genuinely differ and the comment saying so is load-bearing.

### Future work

Unchanged from Step 2, minus the isolation question, which is now settled. The README sentence is being written separately, and its wording can now be definite: goqite pins read committed for its own transactions on PostgreSQL.

## Step 4: Documentation corrections from external review

**Author:** builder-pg-contention

### Prompt Context

**Verbatim prompt:** "Three small doc fixes from an external review (treat as extended self-review), then commit and push. No behavior changes. 1. README.md "Using PostgreSQL" paragraph + the ReceiveTx doc comment: they name only serializable as the isolation level where callers can still hit 40001, but repeatable read has the same failure mode (for update on a row updated after the snapshot also raises 40001). […] 2. ReceiveTx doc comment: phrase the contract in terms of the supplied transaction ("the transaction's isolation level applies") rather than "the caller's" […] 3. In goqite_test.go, the comment above the 2-second context […] explains red/green via internal SQL […]. Reword to observable behavior […]"
**Interpretation:** Three prose corrections across the README, the `ReceiveTx` doc comment, and one test comment. No code changes.
**Inferred intent:** Make the documented contract accurate and correctly scoped before the PR merges.

### What I did

Widened both isolation warnings from "at serializable" to "at repeatable read or serializable", in the README's "Using PostgreSQL" paragraph and on `ReceiveTx` in `/goqite.go`. Rephrased the `ReceiveTx` comment from "the isolation level is the caller's" to "its isolation level applies", where "it" is the supplied transaction. Rewrote the comment above the two-second context in `/goqite_test.go` to describe what a receiver does — blocks on the locked message until the deadline — instead of naming the SQL clause that prevents it.

### Why

The repeatable read point is a real correctness fix, not a wording preference: `for update` against a row updated since the transaction's snapshot raises 40001 under repeatable read exactly as it does under serializable, so naming only serializable understated where the caller needs a retry.

The other two are scoping. A doc comment that says "the caller's isolation level" describes the package's importers rather than its own contract; phrasing it as the supplied transaction's level says the same thing without the package looking outward. And a test comment that explains itself through `for update skip locked` describes the fix rather than the behaviour under test, which stops being true the moment the implementation changes.

### What worked

All three were one-line edits. `gofmt` clean on both touched Go files, `go vet ./...` clean, full suite green for both flavors, `golangci-lint run` reports `0 issues.`

### What didn't work

Nothing. No behaviour changed, so no re-validation against Postgres was warranted beyond the suite.

### What I learned

The failure mode I measured in Step 3 on a hardened database — `could not serialize access due to concurrent update` — is precisely the one that reaches repeatable read too. That error comes from the write conflict, not from SSI's dependency tracking, and repeatable read raises it just as readily. Having seen the error text made the review point immediately recognisable rather than something to take on faith.

### What was tricky

Nothing.

### What warrants review

That the two isolation warnings now agree with each other, and that neither claims goqite retries on the caller's behalf — it does not.

### Future work

Unchanged.

## Step 5: Regression test for the isolation pin, and a correction to Step 2

**Author:** builder-pg-contention

### Prompt Context

**Verbatim prompt:** "Two fixes from a second external review […] 1. Add a committed regression test for the isolation pin. Today, reverting inTx to serializable leaves the whole suite green (you flagged this yourself; the reviewer confirmed […]). Add a small deterministic Postgres-gated test that proves goqite's own transactions run at read committed […] 2. Correct the diary. The reviewer disproved (by repro) the diary claim that the old Postgres query double-delivered only with near-zero timeouts via EvalPlanQual rechecking timeout eligibility. Actual mechanics: the id subquery is an InitPlan evaluated once; after the lock wait, EvalPlanQual rechecks only the outer `id = <id>` condition […] Verify this against your own understanding […]"
**Interpretation:** Close the coverage gap I reported in Step 2 with a committed test, and correct the mechanism I got wrong in Step 2's "What I learned" — after checking it myself rather than taking the correction on faith.
**Inferred intent:** Make the isolation half of the fix defended by CI, and leave the diary's technical account accurate for whoever reads it next.

### What I did

Added `/goqite_internal_test.go` (`package goqite`, so it can reach the unexported `inTx`). It opens its own pool, pins it to a single connection, raises that connection's `default_transaction_isolation` to serializable the way a hardened server would, then runs `show transaction_isolation` inside `q.inTx` and asserts `read committed`.

Then I checked the correction with `explain (verbose)` on the old query and with a two-session repro.

### Why

The single connection and the raised session default are what give the test teeth. Asserting `read committed` against a default-configured server would pass with `opts = nil` too, which is exactly the regression Step 3 was about; raising the default first means the test fails unless the level is pinned deliberately.

The test connects directly instead of using `internaltesting.NewPostgreSQLDB`, because `internal/testing` imports this package and an in-package test file cannot import it back. It needs no table, only a connection.

### What worked

The test catches both ways the pin can be lost. Reverting to `sql.LevelSerializable`:

```
goqite_internal_test.go:36: Expected "read committed", but got "serializable" (type string)
--- FAIL: TestQueue_inTx (0.02s)
```

and reverting to `nil` gives the identical failure, since the connection's raised default takes over. Restored, it passes in 0.02s. Full suite green for both flavors, `go vet` clean, `golangci-lint run` reports `0 issues.`

### What didn't work

My Step 2 account of the double-delivery mechanism was wrong, and the reviewer is right. I claimed EvalPlanQual re-evaluates the whole `where` clause against the updated row, so `$3 >= timeout` would filter the losing receiver out unless the queue timeout was near zero. That is not what the plan does. `explain (verbose)` on the old query shows why:

```
Update on public.goqite
  InitPlan 1
    ->  Limit
          ->  Sort
                ->  Bitmap Heap Scan on public.goqite goqite_1
                      Filter: ((goqite_1.received < 3) AND (now() >= goqite_1.timeout))
  ->  Index Scan using goqite_pkey on public.goqite
        Index Cond: (goqite.id = (InitPlan 1).col1)
```

The timeout and received predicates live inside `InitPlan 1`, which is uncorrelated and evaluated exactly once. The outer scan's only condition is `id = (InitPlan 1).col1`, a cached constant. So the EvalPlanQual recheck asks only "is this still the row with that id", which is trivially true, and the losing receiver updates it again.

Two sessions against the old query, with the visibility timeout set five minutes ahead — nowhere near zero — confirm it. Session A takes the head message and holds its transaction open; session B blocks, then commits after A:

```
 body  | received
-------+----------
 msg-1 |        2
 msg-2 |        0
```

Both receivers took `msg-1`, and `msg-2` was never delivered at all. The same scenario against the fixed query gives `msg-1 | 1` and `msg-2 | 1`. Since the `received < 3` guard also lives in the InitPlan, repeated collisions can push `received` past `MaxReceive`, so a message can be delivered more times than configured.

### What I learned

An uncorrelated subquery in a `where` clause is not a guard that holds for the life of the statement. It is a constant computed once, and everything expressed inside it — visibility, receive count, queue name — stops being enforced the moment another transaction changes the row. The predicates *look* like they protect the update, and the plan is where you find out they do not. Reading the plan would have corrected me in Step 2 had I looked instead of reasoning from how I assumed EvalPlanQual worked.

This also raises how serious the pre-fix bug was. The reported symptom was 40001 storms under serializable, but the same query at read committed loses messages and over-delivers, silently. Landing the two changes together was not merely tidy sequencing, as Step 1 framed it; either one alone is a real bug.

### What was tricky

Making the new test fail for the right reason. An assertion of `read committed` on an ordinary server is satisfied by three different implementations — the pin, nil options, and anything else that happens to inherit the default — so it would have been a test that looks like coverage while proving nothing. Raising the session default first is the whole point of it.

### What warrants review

That `/goqite_internal_test.go` is the only file in `package goqite` under test, and whether reaching into the unexported `inTx` is acceptable versus asserting the level through a public call. I chose the internal test because it names the thing being defended.

### Future work

Unchanged. The Step 2 text is left as written, with this step as its correction.

## Step 6: Markus's PR review

**Author:** builder-pg-contention

### Prompt Context

**Verbatim prompt:** "Markus reviewed PR #84; three agreed changes to apply in one batch […] 1. goqite.go ReceiveTx doc comment: delete both lines we added […] It goes back to exactly "// ReceiveTx is like Receive, but within an existing transaction." 2. goqite.go Priority field comment, Markus's wording verbatim […] 3. goqite_internal_test.go: inside the inTx callback, also query `show default_transaction_isolation` and assert it returns "serializable" […] Product guidance from Markus to carry forward: SQLite is the primary audience — keep Postgres-specific detail out of general doc comments; it belongs in the README's Postgres section or nowhere."

**Interpretation:** Pull the Postgres prose back out of the general doc comments, take Markus's `Priority` wording exactly as written, and strengthen the isolation test's premise.
**Inferred intent:** Keep the public API's documentation aimed at the SQLite majority, and make the new test prove its own setup rather than assume it.

### What I did

Reverted the `ReceiveTx` doc comment to its original single line, replaced the `Priority` comment with Markus's wording verbatim, and added a second query inside the `inTx` callback in `/goqite_internal_test.go` asserting `show default_transaction_isolation` returns `serializable`.

### Why

The third change is the one that earns its place. The test raises the connection's default and then asserts the transaction reports read committed, but nothing proved the raised default actually survived to that transaction — if the pool had handed over a fresh connection, or the session setting had been reset, the assertion would have passed for the wrong reason and the test would have quietly stopped guarding anything. Asserting both values makes the override visible: the default is serializable *and* the transaction is read committed, so the pin is demonstrably what separates them.

### What worked

Green, and both red paths still hold. Reverting the pin to nil:

```
goqite_internal_test.go:43: Expected "read committed", but got "serializable" (type string)
```

with the `serializable` session-default assertion passing in that run, which is exactly right — it confirms the setup held and the pin is what was missing. Full suite green for both flavors, `gofmt` clean, `go vet` clean, `golangci-lint run` reports `0 issues.`

### What didn't work

Nothing.

### What I learned

The guidance to keep Postgres detail out of general doc comments is a sharper rule than "don't over-document". A `Priority` comment that quietly means something different depending on flavor is worse than one that names the condition, which is why Markus's wording puts the condition in — concurrent *and not serialized* — and then says SQLite always is. The reader on the primary path gets an unqualified guarantee; the Postgres reader gets the caveat without the SQLite reader paying for it. The `ReceiveTx` lines I had added failed that test: they described Postgres locking behaviour to an audience that mostly has none.

### What was tricky

Nothing.

### What warrants review

That `ReceiveTx` is now byte-identical to what it was on main — confirmed with `git diff main -- goqite.go`, which shows no change to that line. The README carries the isolation and retry half of what the deleted lines said, in wider form. The other half, that the message row stays locked until the caller's transaction ends, is now documented nowhere; that follows from "the README's Postgres section or nowhere", but it is the one fact a reader holding a long transaction might want, so it is worth a deliberate yes or no rather than an oversight.

### Future work

Unchanged.
