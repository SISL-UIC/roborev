# Bun Storage Refactor Design

## Summary

Replace roborev's handwritten storage access with Uptrace Bun while preserving
the current local-first architecture. SQLite remains the authoritative local
database, and PostgreSQL remains an optional central synchronization store.

The refactor introduces one canonical logical data model and shared persistence
building blocks without forcing SQLite and PostgreSQL to have identical physical
schemas. The existing `storage.DB` API remains stable for daemon, CLI, and TUI
callers. Targeted raw SQL remains allowed for migrations and operations whose
atomic semantics are clearer or safer in explicit SQL.

## Motivation

The storage package currently maintains two independent access styles:

- a large SQLite primary-store implementation built directly on `database/sql`;
- a smaller PostgreSQL synchronization implementation built directly on
  `pgxpool`.

This duplicates row scanning, nullable-value handling, timestamp conversion,
query assembly, upsert code, and model maintenance. It also makes schema drift
between the shared entities harder to see.

Bun can centralize model mapping and query construction while retaining
database-specific SQL where the databases have different responsibilities or
semantics.

## Goals

- Use Bun as the owner of all normal SQLite and PostgreSQL storage access.
- Preserve SQLite as the local source of truth and PostgreSQL as a sync store.
- Preserve the current `storage.DB` methods used by production callers.
- Define one canonical logical model for shared entities.
- Migrate the complete storage package in one branch through several reviewable
  commits.
- Retain explicit SQL for migrations and a small documented set of atomic or
  dialect-specific operations.
- Preserve existing behavior, data, sync convergence, query projections, and
  transaction boundaries.

## Non-goals

- Making PostgreSQL a selectable primary daemon backend.
- Making the SQLite and PostgreSQL schemas byte-for-byte identical.
- Replacing the existing migration histories with Bun auto-migrations.
- Exposing Bun queries or Bun models outside `internal/storage`.
- Adding compatibility adapters for old and new storage implementations to run
  side by side after the refactor.
- Removing every handwritten SQL fragment regardless of clarity or correctness.

## Current Constraints

SQLite and PostgreSQL store overlapping but different data.

SQLite contains local execution state such as worker claims, retry counters,
patches, checkout paths, command lines, prompt construction flags, local CI
state, and sync cursors. Its relationships primarily use local integer IDs.

PostgreSQL stores the replicated subset. Its relationships use stable UUIDs
where cross-machine identity matters, and its pull cursors depend on PostgreSQL
timestamps plus integer tie-breakers. Its conflict policy intentionally updates
only selected fields.

Those differences are part of the local-first design. They must be represented
explicitly rather than hidden behind a claim that both databases are equivalent.

## Architecture

### SQLite handle

`storage.DB` remains the public SQLite-facing type. During and after the
refactor, it continues to expose its current storage methods and retains access
to the underlying `*sql.DB` for the few production operations that require it,
including SQLite pragmas.

Internally, `storage.DB` also owns a `*bun.DB` configured with Bun's SQLite
dialect. Normal queries, inserts, updates, deletes, and transactions run through
the Bun handle.

The embedded `*sql.DB` compatibility surface remains available during this
refactor because production code and a large body of tests use it directly. New
production storage behavior should be added as a `storage.DB` method rather than
as SQL outside the storage package.

Production SQL outside `internal/storage` is part of the conversion scope. The
implementation must inventory direct `Exec`, `Query`, `QueryRow`, `Prepare`, and
transaction calls, including context-suffixed forms, in `internal/daemon` and
`cmd/roborev`. Non-allowlisted queries move behind Bun-backed `storage.DB`
methods. Direct access remains only for tests and documented native operations
such as the SQLite WAL checkpoint pragma.

### PostgreSQL handle

`PgPool` remains the synchronization-facing wrapper used by the sync worker and
CLI status checks. Its constructor, operational methods, and
`Pool() *pgxpool.Pool` accessor remain stable.

`PgPool` continues to own one `*pgxpool.Pool`. Bun's documented pgx v5
integration wraps that pool with `stdlib.OpenDBFromPool`, then constructs a
`*bun.DB` with Bun's PostgreSQL dialect. Bun queries and direct pgx batch
operations therefore share the same underlying connection pool.

The `*sql.DB` wrapper created by `OpenDBFromPool` has no independent idle pool,
and closing it does not close the underlying pgx pool. `PgPool.Close` closes the
Bun/SQL wrapper and the pgx pool in the correct order.

### Models

Public types such as `Repo`, `Commit`, `ReviewJob`, `Review`, and `Response`
remain domain and transport objects. They do not receive Bun tags or database
nullable types.

Private Bun row types represent persisted tables. They are split by persistence
responsibility:

- shared logical fields used by both stores;
- SQLite-only fields used for local execution and sync bookkeeping;
- PostgreSQL-only fields used for replicated identity and pull ordering;
- joined projection rows used by list, summary, export, and sync queries.

Small mapping functions convert between public models, Bun rows, and sync DTOs.
These functions own nullable-value handling, JSON encoding, timestamp
normalization, and boolean representation.

Persisted timestamps use a private scanner/value type rather than plain
`time.Time` fields. The scanner accepts native PostgreSQL `time.Time` values,
RFC3339 strings and bytes, and the historical bare SQLite datetime format. It
normalizes each valid value to `time.Time` without forcing PostgreSQL through a
string-only projection. Nullable timestamps carry an explicit validity bit.

### Dialect policy

A small private dialect policy captures only behavior that genuinely differs:

- current-time expressions and timestamp precision;
- UUID storage and generation;
- boolean representation where an explicit conversion is required;
- dialect-specific conflict clauses;
- SQLite pragmas and schema introspection;
- PostgreSQL SQLSTATE classification;
- keyset cursor expressions.

The policy is not a general database abstraction and does not mirror every Bun
operation. Shared code uses Bun directly unless a known semantic difference
requires a policy method.

## Schema Policy

The two databases share a canonical logical model, not an identical physical
schema.

Shared entities must use the same field names and meanings wherever practical.
Physical differences are allowed when they serve one of these documented
reasons:

- local-only execution state;
- sync-only identity or cursor state;
- native database types;
- constraints required by one store's role;
- relationships that use local IDs versus cross-machine UUIDs.

Existing SQLite migrations and versioned PostgreSQL schemas remain immutable.
Bun auto-migration is not used. A new forward migration is permitted only if a
specific model mismatch blocks the refactor or represents an independently
valuable parity fix. Such a migration must follow the repository's database
migration discipline and must not rewrite shipped migrations.

## Data Flow

### Local operations

Daemon, CLI, and TUI code continue calling the current `storage.DB` methods.
Each method maps inputs to private row values, builds a Bun query, executes it
against SQLite, and maps results back to public models.

Methods without a context parameter retain their existing signatures and use an
internal context. New APIs should accept a caller context when cancellation is
material to the operation.

### Sync push

The sync worker selects terminal local jobs, reviews, and responses through Bun
projection queries. The result maps to the existing sync DTOs and then to
PostgreSQL row models.

PostgreSQL writes use Bun inserts with explicit conflict clauses. The existing
field-by-field conflict policy remains authoritative. In particular, terminal
state, session, token usage, agent invocation, panel metadata, and timestamps
must converge exactly as they do before the refactor.

### Sync pull

PostgreSQL pull queries use Bun with the existing keyset order:

- jobs by `(updated_at, id)`;
- reviews by `(updated_at, id)`;
- responses by `(inserted_at, id)`.

Pulled rows map to sync DTOs and merge into SQLite inside explicit transactions.
Merge operations preserve local-only fields and retain the current stale-update
guards. Pull cursors advance only after the corresponding rows have been
processed successfully.

### Batch behavior

Bun bulk inserts replace pgx batches where they preserve the existing per-item
result contract. Existing pgx batch operations may remain when they provide
clearer per-item results or fallback behavior than a Bun bulk statement. Both
paths share the same pgx pool. When PostgreSQL transaction-abort semantics make
a bulk statement unsuitable, the implementation may use pgx batches,
individual Bun statements, or savepoints. It must preserve the current behavior
that identifies which items succeeded and allows job upserts to fall back to
individual writes when a batch fails.

## Raw SQL Policy

Raw SQL is permitted only in these categories:

- SQLite and PostgreSQL schema creation and forward migrations;
- schema introspection and migration guards;
- SQLite pragmas;
- guarded claims and state transitions whose correctness depends on one atomic
  statement;
- complex upserts whose conflict policy is clearer as explicit SQL;
- database-native maintenance or cursor expressions not represented clearly by
  Bun;
- pgx batch operations retained for per-item sync result semantics;
- tests that intentionally prepare legacy schemas or corrupt states.

Production raw SQL should execute through Bun raw queries or a Bun-managed
transaction when possible. Every remaining production raw query must have a
short comment identifying its allowlist category when that reason is not
obvious from the surrounding method.

## Transactions and Concurrency

Existing transaction boundaries are behavioral contracts. The refactor must
preserve atomicity for:

- job claiming and retry transitions;
- review creation and job completion;
- panel creation, synthesis, cancellation, posting, and cleanup;
- repo and commit remapping;
- sync pull merges and cursor updates;
- migration and backfill steps that already run transactionally.

Bun must not introduce implicit generic retries. Existing targeted retry or
fallback behavior remains explicit. Driver-specific serialization, busy, and
constraint errors are classified in one private helper and wrapped with the
same operation context callers receive today.

## Error Handling

- Preserve `sql.ErrNoRows` behavior where callers depend on it.
- Preserve duplicate detection and conflict outcomes.
- Preserve context cancellation and timeout errors.
- Keep existing operation-focused error wrapping unless a clearer stable
  message is required.
- Normalize PostgreSQL SQLSTATE handling without leaking driver types through
  the storage API.
- Close rows, statements, and transactions deterministically.

## Security and Logging

Bun query logging and debug hooks are disabled by default. Prompts, diffs,
database URLs, responses, and other sensitive values must not appear in normal
logs.

If an opt-in query hook is added later, it must redact arguments and connection
credentials before emitting output.

PostgreSQL text sanitation remains in place for invalid UTF-8 and NUL bytes.

## Performance Requirements

- Preserve metadata-only projections that omit prompts and other large text.
- Do not replace joined or aggregate queries with N+1 relation loading.
- Preserve current indexes, keyset pagination, and query limits.
- Avoid `SELECT *` for large or compatibility-sensitive models.
- Keep sync writes batched where batching preserves correctness.
- Wrap the existing pgx pool with `stdlib.OpenDBFromPool`; do not create a
  second independent PostgreSQL pool.

## Delivery Sequence

The refactor lands as several sequential commits in one branch:

1. Add Bun dependencies, connection plumbing, dialect configuration, and
   private row models without changing behavior.
2. Convert simple domains: repos, commits, daemon state, and sync state.
3. Convert jobs, reviews, responses, hydration, verdict, and cost operations.
4. Convert CI panels, CI attempts, summaries, exports, and other complex SQLite
   queries.
5. Convert PostgreSQL schema access, push, pull, batches, and sync integration.
6. Remove superseded helpers, confine remaining raw SQL to the allowlist,
   update documentation, and run full verification.

Each commit must leave the repository buildable and pass the focused tests for
the domains it changes. No long-lived dual implementation or runtime feature
flag is introduced.

## Testing Strategy

The existing storage, daemon, and CLI tests serve as characterization coverage.
New tests are added only for roborev behavior at risk during conversion:

- nullable and timestamp mapping;
- SQLite and PostgreSQL logical-model parity;
- conflict policies and stale-update guards;
- transaction atomicity and guarded state transitions;
- cursor ordering and advancement;
- projection behavior for large text fields;
- sync convergence across multiple machines.

Tests must not assert Bun-generated SQL strings or duplicate Bun library tests.
PostgreSQL behavior remains covered by tagged integration tests against a real
PostgreSQL server.

Verification proceeds from focused to broad:

1. affected `internal/storage` tests after each domain conversion;
2. affected daemon and CLI tests after caller-sensitive changes;
3. all untagged Go tests;
4. PostgreSQL-tagged storage integration tests;
5. build and non-mutating lint/pre-commit checks.

## Documentation

Update development and PostgreSQL sync documentation to describe Bun as the
storage access layer while keeping SQLite primary and PostgreSQL sync semantics
unchanged. User-facing configuration does not change.

## Acceptance Criteria

- Existing SQLite databases open and migrate successfully.
- Fresh SQLite and PostgreSQL databases initialize successfully.
- Existing daemon, CLI, TUI, and sync behavior remains unchanged.
- PostgreSQL sync converges with the same conflict and cursor semantics.
- Production storage access is owned by Bun, with remaining raw SQL limited to
  the documented allowlist.
- No sensitive query arguments are logged by default.
- Bun and direct pgx operations share one PostgreSQL connection pool.
- Focused, full, PostgreSQL integration, build, and lint checks pass.
