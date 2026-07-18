# Bun Storage Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace roborev's duplicated raw SQLite and PostgreSQL access with a
Bun-backed storage layer while preserving SQLite-primary, PostgreSQL-sync
behavior and the existing storage API.

**Architecture:** `storage.DB` embeds `*bun.DB`, which itself retains the
existing `*sql.DB` compatibility surface. `PgPool` keeps its
`*pgxpool.Pool`, wraps it with `stdlib.OpenDBFromPool`, and gives the wrapper to
Bun so Bun queries and retained pgx batches share one pool. Private canonical
row models and mapping helpers are shared by the local and sync paths, while
migrations and a narrow set of atomic/dialect-specific statements remain raw.

**Tech Stack:** Go 1.26.3, Uptrace Bun v1.2.18, Bun SQLite and PostgreSQL
dialects, modernc SQLite, pgx v5/pgxpool, testify.

## Global Constraints

- SQLite remains the authoritative local database.
- PostgreSQL remains an optional synchronization store, not a primary backend.
- Preserve production `storage.DB` and `PgPool` method signatures, including
  `PgPool.Pool()`.
- Preserve shipped SQLite migrations and versioned PostgreSQL schemas.
- Use one pgx pool shared with Bun through `stdlib.OpenDBFromPool`.
- Keep Bun query logging disabled by default.
- Preserve metadata-only projections, keyset cursor ordering, sync conflict
  policies, transaction boundaries, and existing error behavior.
- Raw SQL is limited to migrations, schema inspection, pragmas, guarded atomic
  transitions, complex conflict updates, database-native cursor expressions,
  and pgx batches with per-item result semantics.
- Use testify assertions and the repository's test-isolation conventions.
- Do not add tests for Bun library behavior or generated SQL strings.
- Do not modify database schemas unless a concrete blocker is found; if one is
  found, stop and apply the database migration discipline before editing it.

---

### Task 1: Add Bun connection plumbing

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/storage/db.go`
- Modify: `internal/storage/postgres.go`
- Create: `internal/storage/bun_test.go`

**Interfaces:**

- Produces: `type DB struct { *bun.DB }`
- Produces: `func newSQLiteBunDB(*sql.DB) *bun.DB`
- Produces: `func newPostgresBunDB(*sql.DB) *bun.DB`
- Preserves: `func Open(string) (*DB, error)`
- Preserves: `func NewPgPool(context.Context, string, PgPoolConfig) (*PgPool, error)`
- Preserves: `func (p *PgPool) Pool() *pgxpool.Pool`

- [ ] **Step 1: Add a SQLite ownership test**

Add `TestOpenBunHandleSharesSQLiteDatabase` to `internal/storage/bun_test.go`.
The test opens a temporary database, creates a repo through `GetOrCreateRepo`,
and reads that same row with a Bun raw query:

```go
func TestOpenBunHandleSharesSQLiteDatabase(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	repo, err := db.GetOrCreateRepo("/tmp/bun-shared")
	require.NoError(t, err)

	var count int
	err = db.NewRaw("SELECT COUNT(*) FROM repos WHERE id = ?", repo.ID).Scan(t.Context(), &count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
```

- [ ] **Step 2: Run the focused test and confirm the pre-change compile failure**

Run:

```bash
go test ./internal/storage -run TestOpenBunHandleSharesSQLiteDatabase -count=1
```

Expected: FAIL because `DB` does not expose Bun query construction yet.

- [ ] **Step 3: Add Bun dependencies**

Run:

```bash
go get github.com/uptrace/bun@v1.2.18 \
  github.com/uptrace/bun/dialect/pgdialect@v1.2.18 \
  github.com/uptrace/bun/dialect/sqlitedialect@v1.2.18
```

- [ ] **Step 4: Wrap SQLite with Bun**

Change the SQLite handle and constructor in `internal/storage/db.go`:

```go
type DB struct {
	*bun.DB
}

func newSQLiteBunDB(sqldb *sql.DB) *bun.DB {
	return bun.NewDB(sqldb, sqlitedialect.New())
}
```

In `Open`, construct `sqldb`, wrap it once, and use `wrapped.Close()` on every
error path. Existing promoted `Exec`, `Query`, `QueryRow`, `Begin`, and
`Prepare` calls continue to resolve through Bun's embedded `*sql.DB`.

- [ ] **Step 5: Wrap the existing pgx pool with Bun**

Extend `PgPool` in `internal/storage/postgres.go`:

```go
type PgPool struct {
	pool  *pgxpool.Pool
	sqldb *sql.DB
	db    *bun.DB
}

func newPostgresBunDB(sqldb *sql.DB) *bun.DB {
	return bun.NewDB(sqldb, pgdialect.New())
}
```

After `pgxpool.NewWithConfig`, use:

```go
sqldb := stdlib.OpenDBFromPool(pool)
db := newPostgresBunDB(sqldb)
```

Keep `Pool()` unchanged. Update `Close()` to close `db` first and `pool`
second; `OpenDBFromPool` guarantees closing the SQL wrapper does not close the
pgx pool.

- [ ] **Step 6: Run focused and package tests**

Run:

```bash
go test ./internal/storage -run 'TestOpenBunHandleSharesSQLiteDatabase|TestDB' -count=1
go test ./internal/storage -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit the plumbing**

```bash
git add go.mod go.sum internal/storage/db.go internal/storage/postgres.go \
  internal/storage/bun_test.go
git commit -m "Adopt Bun storage handles"
```

### Task 2: Define canonical persisted row models

**Files:**

- Create: `internal/storage/bun_models_core.go`
- Create: `internal/storage/bun_models_jobs.go`
- Create: `internal/storage/bun_models_ci.go`
- Create: `internal/storage/bun_models_test.go`

**Interfaces:**

- Produces: `repoRow`, `commitRow`, `jobRow`, `reviewRow`, `responseRow`
- Produces: `ciPRReviewRow`, `ciPanelRow`, `ciReviewAttemptRow`
- Produces: `jobRowFromModel(ReviewJob) jobRow`
- Produces: `func (jobRow) applyToModel(*ReviewJob)`
- Produces: explicit `sqliteJobColumns` and `postgresJobColumns`

- [ ] **Step 1: Write mapping tests for the logical model**

Add table-driven tests that create a `ReviewJob` with nullable timestamps,
dirty files, panel metadata, sync identity, and local-only fields. Round-trip it
through `jobRowFromModel` and `applyToModel`, then compare the behavior-bearing
fields with `assert.Equal`.

Also add `TestJobColumnSetsDocumentStoreRoles`, asserting only roborev's own
column policy:

```go
assert.Contains(t, sqliteJobColumns, "worker_id")
assert.NotContains(t, postgresJobColumns, "worker_id")
assert.Contains(t, postgresJobColumns, "source_machine_id")
assert.Contains(t, sqliteJobColumns, "synced_at")
```

- [ ] **Step 2: Run the mapping tests and confirm they fail to compile**

```bash
go test ./internal/storage -run 'TestReviewJobRow|TestJobColumnSets' -count=1
```

Expected: FAIL because the private rows and mappings do not exist.

- [ ] **Step 3: Implement core rows**

In `bun_models_core.go`, define explicit table aliases and nullable database
fields. The core shapes are:

```go
type repoRow struct {
	bun.BaseModel `bun:"table:repos,alias:r"`
	ID            int64     `bun:"id,pk,autoincrement"`
	RootPath      string    `bun:"root_path"`
	Name          string    `bun:"name"`
	Identity      *string   `bun:"identity"`
	CreatedAt     time.Time `bun:"created_at"`
}

type commitRow struct {
	bun.BaseModel `bun:"table:commits,alias:c"`
	ID            int64     `bun:"id,pk,autoincrement"`
	RepoID        int64     `bun:"repo_id"`
	SHA           string    `bun:"sha"`
	Author        string    `bun:"author"`
	Subject       string    `bun:"subject"`
	Timestamp     time.Time `bun:"timestamp"`
	CreatedAt     time.Time `bun:"created_at"`
}
```

Use pointer fields for nullable persisted values and retain raw string timestamp
fields only where SQLite's historical timestamp formats require custom parsing.

- [ ] **Step 4: Implement job/review/response rows and mappings**

Define one superset `jobRow` with every logical field and explicit column
lists for each store. Add `reviewRow` and `responseRow` with both local integer
relations and sync UUID relations. Mapping functions must call existing helpers
such as `encodeDirtyFiles`, `decodeDirtyFiles`, and `parseSQLiteTime` rather than
duplicating behavior.

- [ ] **Step 5: Implement CI rows**

Define private rows for `ci_pr_reviews`, `ci_pr_panels`,
`ci_pr_review_attempts`, `daemon_state`, and `sync_state`. Keep their fields
flat; do not add Bun relations that would encourage implicit N+1 loading.

- [ ] **Step 6: Run mapping and storage tests**

```bash
go test ./internal/storage -run 'TestReviewJobRow|TestJobColumnSets' -count=1
go test ./internal/storage -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit the canonical models**

```bash
git add internal/storage/bun_models_core.go \
  internal/storage/bun_models_jobs.go internal/storage/bun_models_ci.go \
  internal/storage/bun_models_test.go
git commit -m "Define canonical Bun storage models"
```

### Task 3: Convert core SQLite repositories and state

**Files:**

- Modify: `internal/storage/repos.go`
- Modify: `internal/storage/commits.go`
- Modify: `internal/storage/daemon_state.go`
- Modify: `internal/storage/sync.go`
- Test: existing `internal/storage/repos_test.go`
- Test: existing `internal/storage/daemon_state_test.go`
- Test: existing `internal/storage/sync_state_test.go`

**Interfaces:**

- Consumes: `repoRow`, `commitRow`, `daemonStateRow`, `syncStateRow`
- Preserves all existing exported method signatures in the modified files.

- [ ] **Step 1: Run characterization tests before conversion**

```bash
go test ./internal/storage -run 'Test.*Repo|Test.*DaemonState|Test.*SyncState' -count=1
```

Expected: PASS before editing.

- [ ] **Step 2: Convert repo reads and writes to Bun**

Use explicit columns and conflict clauses. For example, replace the repo lookup
with:

```go
var row repoRow
err := db.NewSelect().
	Model(&row).
	Column("id", "root_path", "name", "created_at", "identity").
	Where("root_path = ?", rootPath).
	Scan(context.Background())
```

Preserve duplicate-identity detection and `PreferAutoClone`. Do not use Bun
relations for commits or jobs.

- [ ] **Step 3: Convert commit methods to Bun**

Use `NewInsert().On("CONFLICT ...")` for idempotent commit creation and
`NewSelect` for SHA/repo lookups. Keep repo remapping transactions explicit.

- [ ] **Step 4: Convert daemon and sync key/value state to Bun**

Use a shared private helper:

```go
func upsertKeyValue(ctx context.Context, db bun.IDB, table, key, value string) error
```

The helper builds an identifier-safe table expression from a fixed internal
enum, not from caller input. Preserve SQLite's concurrency-safe machine/database
ID creation with `ON CONFLICT DO NOTHING` followed by a select.

- [ ] **Step 5: Run focused tests**

```bash
go test ./internal/storage -run 'Test.*Repo|Test.*DaemonState|Test.*SyncState' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit core repository conversion**

```bash
git add internal/storage/repos.go internal/storage/commits.go \
  internal/storage/daemon_state.go internal/storage/sync.go
git commit -m "Move core storage queries to Bun"
```

### Task 4: Convert jobs, reviews, responses, and hydration

**Files:**

- Modify: `internal/storage/jobs.go`
- Modify: `internal/storage/reviews.go`
- Modify: `internal/storage/review_attempts.go`
- Modify: `internal/storage/hydration.go`
- Modify: `internal/storage/verdict.go`
- Modify: `internal/storage/cost.go`
- Test: existing job, review, attempt, verdict, and cost tests.

**Interfaces:**

- Consumes: `jobRow`, `reviewRow`, `responseRow` and mapping helpers.
- Preserves: `JobQuery` filtering and `WithoutPrompt()` projection behavior.
- Preserves all exported storage method signatures.

- [ ] **Step 1: Run characterization tests**

```bash
go test ./internal/storage -run \
  'Test.*Job|Test.*Review|Test.*Response|Test.*Verdict|Test.*Cost' -count=1
```

Expected: PASS before editing.

- [ ] **Step 2: Convert job inserts and simple lookups**

Use `jobRowFromModel`, explicit SQLite column lists, and `Returning("id")` where
supported. Preserve UUID generation, machine ID assignment, dirty-file JSON,
and time parsing. Use explicit select projections rather than `SELECT *`.

- [ ] **Step 3: Convert `JobQuery` construction**

Build one `*bun.SelectQuery`, append each current filter with `Where`, and keep
the current ordering and limits. Implement `WithoutPrompt()` by selecting the
same explicit column list minus `prompt`; never hydrate then discard it.

- [ ] **Step 4: Convert guarded job transitions**

Use Bun updates for ordinary transitions. Keep raw Bun queries for atomic claims
and compare-and-set updates where affected-row counts are part of correctness.
Add an allowlist comment above each retained raw statement.

- [ ] **Step 5: Convert review and response persistence**

Use Bun inserts and selects with explicit columns. Preserve one-review-per-job,
closed-state updates, append-only response semantics, sync timestamps, and
review verdict backfills.

- [ ] **Step 6: Convert hydration, verdict, and cost queries**

Keep aggregate work in SQL/Bun query builders; do not load rows and aggregate in
Go. Preserve current `COALESCE`, timing, and agent-invocation eligibility
semantics.

- [ ] **Step 7: Run focused and package tests**

```bash
go test ./internal/storage -run \
  'Test.*Job|Test.*Review|Test.*Response|Test.*Verdict|Test.*Cost' -count=1
go test ./internal/storage -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit primary domain conversion**

```bash
git add internal/storage/jobs.go internal/storage/reviews.go \
  internal/storage/review_attempts.go internal/storage/hydration.go \
  internal/storage/verdict.go internal/storage/cost.go
git commit -m "Move review persistence to Bun"
```

### Task 5: Convert CI, panels, summaries, and exports

**Files:**

- Modify: `internal/storage/ci.go`
- Modify: `internal/storage/ci_panels.go`
- Modify: `internal/storage/summary.go`
- Modify: `internal/storage/export.go`
- Modify: `internal/storage/export_ci.go`
- Modify: `internal/daemon/server.go`
- Modify: `internal/daemon/ci_poller.go`
- Test: existing CI, panel, summary, export, and daemon tests.

**Interfaces:**

- Consumes: CI row models and the Bun-enabled `storage.DB`.
- Produces: `func (db *DB) CountReviews() (int, error)`
- Produces: `func (db *DB) ListReposWithIdentity() ([]Repo, error)`

- [ ] **Step 1: Run characterization tests**

```bash
go test ./internal/storage -run \
  'Test.*CI|Test.*Panel|Test.*Summary|Test.*Export' -count=1
go test ./internal/daemon -run 'Test.*CI|Test.*Panel|Test.*Telemetry' -count=1
```

Expected: PASS before editing.

- [ ] **Step 2: Convert CI review and panel CRUD**

Use Bun for ordinary selects/inserts/updates. Keep raw Bun statements for panel
creation, posting claims, retirement, and retry claims whose guarded affected
row count is the state-machine transition.

- [ ] **Step 3: Convert summaries and exports**

Use Bun select builders with explicit table expressions and projections. Keep
SQLite-native duration expressions and keyset predicates as allowlisted raw
expressions inside Bun queries. Preserve cursor order and metadata-only export
profiles.

- [ ] **Step 4: Move production SQL callers into storage methods**

Replace the direct review count in `internal/daemon/server.go` with
`CountReviews`. Replace the direct repo query in `internal/daemon/ci_poller.go`
with `ListReposWithIdentity`. Leave the SQLite WAL checkpoint pragma in daemon
lifecycle code as an explicitly allowed pragma.

- [ ] **Step 5: Run focused tests**

```bash
go test ./internal/storage -run \
  'Test.*CI|Test.*Panel|Test.*Summary|Test.*Export' -count=1
go test ./internal/daemon -run 'Test.*CI|Test.*Panel|Test.*Telemetry' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit reporting and CI conversion**

```bash
git add internal/storage/ci.go internal/storage/ci_panels.go \
  internal/storage/summary.go internal/storage/export.go \
  internal/storage/export_ci.go internal/daemon/server.go \
  internal/daemon/ci_poller.go
git commit -m "Move CI and reporting storage to Bun"
```

### Task 6: Convert PostgreSQL schema access and sync persistence

**Files:**

- Modify: `internal/storage/postgres.go`
- Modify: `internal/storage/sync.go`
- Modify: `internal/storage/syncworker.go`
- Modify: PostgreSQL tests only when behavior-focused coverage is missing.

**Interfaces:**

- Consumes: canonical row models and existing sync DTOs.
- Preserves all public `PgPool` and sync-worker method signatures.
- Preserves pgx batches when needed for per-item results.

- [ ] **Step 1: Read the mandatory migration discipline if a schema change is
  discovered**

No schema change is planned. If model conversion exposes a required schema
change, stop this task before editing `db.go`, `postgres.go` migration steps, or
`schemas/postgres_v*.sql`, then invoke the database migration discipline.

- [ ] **Step 2: Run PostgreSQL characterization tests**

With the repository's PostgreSQL test DSN configured, run:

```bash
go test -tags=postgres ./internal/storage -run \
  'TestPostgres|TestPg|Test.*Sync|Test.*Panel' -count=1
```

Expected: PASS before editing.

- [ ] **Step 3: Convert schema metadata and simple PostgreSQL operations**

Use Bun for database ID, machine registration, repo/commit upserts, and ordinary
schema-version reads. Keep schema statement execution and migration DDL raw.

- [ ] **Step 4: Convert sync upserts**

Use canonical rows plus explicit PostgreSQL columns. Express the existing
field-by-field `ON CONFLICT` policies with Bun's `On` and `Set` APIs when clear;
otherwise retain the statement as a commented allowlisted Bun raw query.

- [ ] **Step 5: Convert pull queries**

Build Bun selects for jobs, reviews, and responses with the exact existing
keyset predicates and ordering. Scan directly into projection rows, then map to
`PulledJob`, `PulledReview`, and `PulledResponse`. Preserve known-job filtering
for reviews and timestamp/ID cursors.

- [ ] **Step 6: Convert SQLite sync extraction and merges**

Use Bun selects for local push candidates and Bun transactions for pulled-row
merges. Preserve local-only fields, placeholder repo behavior, stale-update
guards, and synced-at updates.

- [ ] **Step 7: Retain or simplify pgx batches**

Keep `pgx.Batch` for review, response, or job operations where per-item results
and fallback behavior are clearer than a Bun bulk insert. Route all non-batch
operations through Bun. Ensure both paths use `PgPool.pool` and the Bun wrapper
created from that same pool.

- [ ] **Step 8: Run untagged and PostgreSQL tests**

```bash
go test ./internal/storage -count=1
go test -tags=postgres ./internal/storage -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit PostgreSQL and sync conversion**

```bash
git add internal/storage/postgres.go internal/storage/sync.go \
  internal/storage/syncworker.go internal/storage/*postgres*_test.go \
  internal/storage/sync*_test.go
git commit -m "Unify PostgreSQL sync access with Bun"
```

### Task 7: Audit raw SQL and document the storage layer

**Files:**

- Modify: remaining production files reported by the audit.
- Modify: `docs/development.md`
- Modify: `docs/advanced/postgres-sync.md`
- Delete: superseded private scan/query helpers only after all callers move.

**Interfaces:**

- Preserves all user-facing configuration and CLI behavior.
- Produces a documented, reviewed raw-SQL allowlist in code comments.

- [ ] **Step 1: Audit production raw database access**

Run:

```bash
rg -n '\.(Exec|Query|QueryRow|Prepare|Begin)\(' internal/storage \
  internal/daemon cmd/roborev -g '*.go' -g '!*_test.go'
```

For every result, either convert it to a Bun query or confirm it belongs to the
design's raw-SQL allowlist and add a short reason comment when not obvious.

- [ ] **Step 2: Remove superseded helpers and imports**

Delete manual row scanners, placeholder builders, and duplicated nullable/time
conversion code only when no callers remain. Keep `parseSQLiteTime` if legacy
timestamp formats still require it.

- [ ] **Step 3: Update documentation**

In `docs/development.md`, describe Bun as the storage access layer over modernc
SQLite and pgx PostgreSQL. In `docs/advanced/postgres-sync.md`, state that Bun
and direct pgx batches share one pgx pool and that SQLite remains primary.

- [ ] **Step 4: Format and run focused checks**

```bash
gofmt -w internal/storage internal/daemon cmd/roborev
make markdown
go test ./internal/storage ./internal/daemon ./cmd/roborev -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit cleanup and documentation**

```bash
git add internal/storage internal/daemon cmd/roborev docs/development.md \
  docs/advanced/postgres-sync.md
git commit -m "Document and enforce Bun storage boundaries"
```

### Task 8: Full verification and branch handoff

**Files:**

- Modify only files required to fix verified regressions.

**Interfaces:** None; this task proves the completed behavior.

- [ ] **Step 1: Run all untagged tests**

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 2: Run PostgreSQL integration tests**

```bash
go test -tags=postgres -v ./internal/storage/... -run Integration -count=1
```

Expected: PASS with a configured PostgreSQL test database.

- [ ] **Step 3: Run build and non-mutating quality gates**

```bash
go build ./...
make lint-ci
make markdown-ci
prek run --all-files
```

Expected: PASS.

- [ ] **Step 4: Inspect the final diff and history**

```bash
git status --short
git diff origin/main...HEAD --stat
git log --oneline origin/main..HEAD
```

Expected: only Bun refactor, tests, and documentation changes; each staged
domain has its own commit.

- [ ] **Step 5: Create a final regression-fix commit only if needed**

If verification required source changes, stage only those fixes and commit:

```bash
git add internal/storage internal/daemon cmd/roborev go.mod go.sum \
  docs/development.md docs/advanced/postgres-sync.md
git commit -m "Fix Bun storage integration regressions"
```

If no source changes were required, do not create an empty commit.
