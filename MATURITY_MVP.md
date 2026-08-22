# MATURITY_MVP.md — MVP Maturity Review

> Status: ☑ Resolved (2026-08-22) — all P1/P2/P3 findings fixed per `PLAN_MVP_FIX.md`. Additionally, the complex value types (JSON, ENUM, INTERVAL, ARRAY, ROW, GEOMETRY, JAVA_OBJECT) that were previously listed as unsupported now decode to documented Go representations. `DECFLOAT` remains the only unhandled complex type per the PRD. Per-finding resolution noted inline.
> Purpose: record **fiendings only** for later analysis. Per project policy no source changes are made during this review.
> Date: 2026-08-22. Reviewer: codebase audit against the H2 2.4.240 reference (`TcpServerThread.java`, `Transfer.java`, `ResultColumn.java`).

This document collects maturity and correctness findings from a full review of the MVP. Nothing here is a blocking
defect for the v0.1.0 scope as documented, but each item should be triaged before building further features on top.
Items are tagged by priority for that triage.

---

## P1 — Should fix before building on top (functional correctness / protocol fidelity)

### 1. Inline `CLOB` values are decoded as a hard error — but `CLOB` is listed as supported (P1)

> ☑ **Resolved (Task 1):** inline CLOBs now decode to `string` via `readInlineClob` (`value_read.go`), validated against H2 2.4.240 with live `type_showcase.col_clob` probe; fetch-on-demand LOBs still return `ErrUnsupportedType`.

- Reference: `value_read.go` `readLOBValue` (`value_read.go:229`), the `ValueTypeClob` inline branch returns
  `ErrUnsupportedType` ("unsupported H2 type: inline CLOB").
- Evidence: `type_showcase.col_clob` (primary source in `seed.sql`) is returned **inline** (length ≥ 0) by H2,
  so a plain `SELECT * FROM type_showcase` fails with
  `h2go: fetchRows: failed to read column 2: h2go: readLOBValue: unsupported H2 type: inline CLOB`.
  Verified live against H2 2.4.240.
- Impact: the PRD lists `CLOB` under **supported result types** (`PRD §7.14` table row "CLOB → `string`"), and
  `README.md` also lists `CLOB` → `string` in the supported-types table. The MVP runtime actually errors on **any**
  non-null inline CLOB. Two options, both documented here for triage:
  - (a) Decode inline CLOBs: they are written with `Data.copyString(reader, out)` followed by `LOB_MAGIC`.
    For an inline CLOB the data is a length-delimited string (see `Data.copyString` in H2 `org.h2.store.Data`);
    after the `charLength` long and the characters the code must consume `LOB_MAGIC` (int32 `0x1234`) to keep the
    stream aligned. An inline CLOB differs from an inline BLOB only in the payload encoding, not in the trailing magic.
  - (b) Mark `CLOB` as documented-unsupported in PRD and README, and return the error from a **known column** so a
    SELECT touching a CLOB column fails predictably rather than mid-stream. The current error is not actionable and
    can only be discovered at scan time.
- Regression risk: none for existing tests (none reads a non-null CLOB through the driver).
- Note: `type_showcase` itself is seeded but **no integration test uses it**, so this path is uncovered. Live probe
  confirmed the failure (see audit notes).

### 2. `Session.Close()` sends `SESSION_CLOSE` and waits for `STATUS_OK` on a socket that may already be dead (P1)

> ☑ **Resolved (Task 5):** `ResetSession` now aborts the session on transport error so the pool discards the conn (`ErrBadConn`); `Session.Close` bounds the `STATUS_OK` read with `closeStatusTimeout` (2s).

- Reference: `session.go:47` `Close()`. It writes `SessionClose`, flushes, then `ReadInt32()` for the reply.
- Context: `conn.ResetSession` calls `rollbackCurrentTransaction` / `setAutoCommit` on a conn that is being returned
  to the pool. If the session is already broken (e.g. the server closed the socket), those reads fail and are wrapped
  in `wrapError`, but the **conn is not marked dead**; the pool can reuse it. The subsequent `Close()` (pool teardown)
  does a best-effort `SESSION_CLOSE` handshake that may block on the read if the peer is half-open.
- Impact: a dead session can linger in the pool; pool churn may stall on the final close. Not a correctness bug in
  normal operation, but under load a broken server can cause stalls.
- Fix (for triage): in `ResetSession`, on transport error mark the session dead (`s.Abort()`) so the conn is
  reported `ErrBadConn` and discarded, and give `Session.Close()` a short deadline on the `STATUS_OK` read.

### 3. `ReadValue` for `DECFLOAT` returns a `string` but the type table in `README` says `DECFLOAT → string`; `NUMERIC` scale/metadata leakage (P1)

> ☑ **Resolved (Task 3):** `PrecisionScale` returns `ok=false` for scale-less `NUMERIC` (wire scale `-1`) instead of clamping to 0.

- Reference: `typeinfo.go` `PrecisionScale()` (`typeinfo.go:451`), and the value decoder `readStringValue` for
  `DECFLOAT` (`value_read.go:87`).
- `PrecisionScale()` handles DECFLOAT **correctly** (returns `(precision, 0, true)`), but `NUMERIC` scale when the
  wire sends `-1` (no fixed scale) is clamped to `0` and reported `ok=true` — same class of "claiming known-zero"
  that Phase-10 already fixed for time types. A `NUMERIC(a)` declared without scale yields `(a, 0, true)` instead
  of `(a, <unknown>, false)`.
- Impact: `sql.ColumnType.DecimalSize()` may mislead callers into assuming scale 0 for scale-less numerics.
- Fix (for triage): mirror the existing `Scale < 0 → ok=false` guard for `NUMERIC`.

### 4. `fetchSize` / `maxRows` are not surfaced through `database/sql`, and `maxRows=0` semantics (P1)

> ☑ **Resolved (Task 7):** new additive `Config.MaxRows` / `Config.FetchSize` fields thread through all query paths; documented in README/doc.go; covered by `TestIntegration_MaxRows`.

- Reference: query execution always passes `maxRows=0` (`rows.go:...` from `conn.go:239` / `260`), fetchSize
  `defaultFetchSize=100`.
- The wire field `maxRows` is sent as `WriteInt64(0)`. In H2, `maxRows=0` means "no limit" (server treats 0 as
  unlimited), so behavior is correct today; but no `database/sql` hook (e.g. `db.SetMaxRows`-style) exists to bound
  a server-side result, and the `maxRows` parameter is always 0. The `conn` never exposes a way to cap result size.
- Impact: queries that return very large result sets are bounded only by the fetch loop and client memory;
  a user cannot ask the server to limit rows (as they can via `SET MAX_ROWS` in JDBC). Feature gap, not a bug.
- Fix (for triage, later): expose a documented option (`Config` or `OpenDB` option) that forwards a `maxRows`
  value, and document that `database/sql` row buffers are per-fetch bounded.

---

## P2 — Fix before scaling / widening test coverage

### 5. `type_showcase` is seeded but never exercised by any integration test (P2)

> ☑ **Resolved (Task 2):** `TestIntegration_TypeShowcaseFullSelect` selects every MVP-supported column across all three seeded rows and asserts the documented Go representation.

- Reference: `seed.sql` group `type_showcase`, and `integration_test.go` (no reference to `type_showcase`).
- The seed includes multiple MVP-supported scalar columns (`CHAR`, `VARCHAR_IGNORECASE`, `BINARY`, `VARBINARY`,
  `TIME`, `TIME WITH TIME ZONE`, `TIMESTAMP WITH TIME ZONE`, `UUID`, plus DECIMAL, DECFLOAT, JSON, CLOB, BLOB)
  that are **not** covered by `TestIntegration_ScalarTypeDecoding` / `TestIntegration_ScalarRoundTripTable`
  (round-trip table only uses a handful of types). A full-where-1=1 SELECT against the showcase would be the
  strongest single integration test for the supported-type matrix — and it currently fails because of finding 1.

### 6. `Rows.Column()` metadata uses alias, not column label, for `Columns()` (P2)

> ☑ **Resolved (Task 8, docs):** `metadata.go` and README now document that `Columns()`/`GetColumnByName` operate on H2 labels (alias); expression-column labels may differ from the underlying column name.

- Reference: `ResultMeta.ColumnNames()` returns `col.Alias` (`metadata.go:142`). H2's `ResultColumn.alias` is the
  **display label** (what JDBC `getColumnLabel` returns) and is usually identical to the column name for plain
  columns. However, for expressions like `SELECT 1+1` or `SELECT col AS x`, H2 sends `alias` as the label and the
  `columnName` separately.
- Impact: `Columns()` labels match JDBC `getColumnLabel`, which is the common expectation. Note in docs: for
  expression columns the alias may differ from the column name; `GetColumnByName` also matches **alias** only,
  so lookups by original column name will miss expression columns. Low impact today, but worth documenting.

### 7. `Example` uses `panic` and mutable table names from `time.Now()`; examples run against live DB (P2)

> ☑ **Resolved (Task 8, docs):** README now notes examples run against the local H2 env and create throwaway `example_*_<nanotime>` tables.

- Reference: `example_test.go:95` `Example()`.
- `Example()` creates `example_exec_%d`/`example_tx_%d`/`example_stmt_%d` tables and panics on any error. Table
  names include `time.Now().UnixNano()` so **concurrent example runs** (e.g. `go test` in two processes) neither
  collide, nor does a stale table block re-runs (`DROP` only on the tx/stmt tables, executed at the end of the
  function). It runs whenever `go test ./...` runs **with a reachable `.env`** (see 8).
- Impact: examples can leave tables behind on failure; `panic` in an example is acceptable Go idiom, but the
  examples will now run on every local `go test` where `.env`/H2 is present — acceptable, but note in docs.

### 8. `--` example/driver reads `h2-data/.env` from `..` — relative path assumption (P2)

> ☐ **Accepted as-is:** `.env` lookup already walks `.`, `..`, `../..` and tests skip cleanly when it is absent. No automation exists today that would hit the skip path; revisit when CI is added.

- Reference: `example_test.go:55` `loadExampleEnvFile` tries `h2-data/.env` and `../h2-data/.env` (two levels).
- When tests run from a different cwd path, `.env` lookup silently fails and tests skip. For CI (not present today)
  this is fine, but for any future automation the lookup should also try an absolute path or use a documented env
  var override. Low priority but avoid surprising skips in tooling.

### 9. Example/DocSQL table-name interpolation in `Example` is safe today but hard to audit (P2)

> ☐ **Accepted as-is (documented):** table names derive from `time.Now().UnixNano()` (controlled, no injection). README documents the throwaway-table convention. No code change.

- Table names are built from `fmt.Sprintf("example_%d", time.Now().UnixNano())` — a controlled string, no injection.
- Still, `Example` is the only place in the codebase building SQL with string concatenation (`"CREATE TABLE "+name`)
  where a reader could mistake it for an injection vector. Document the rationale or switch to a `%q`-quoted table
  name. Not a bug.

### 10. `Rows.fetchRows` reads row bytes with `ReadByte` per flag and `ReadValue` may `ReadString` long payloads (P2)

> ☑ **Resolved (Task 4):** `ReadString`/`ReadBytes` and the inline CLOB decoder now reject length fields above `MaxWireLength` (512 MiB) before allocating.

- Reference: `rows.go:314` `fetchRows` (per-row `ReadByte` + per-value `ReadValue`), and `transfer.go:290` `ReadString`
  allocates `make([]byte, int(length)*2)` from a 32-bit length field read directly from the wire; a hostile/broken
  server could claim a huge length and force a large allocation. The driver talks to a trusted H2 server, so this is
  defense-in-depth only. Worth adding a sanity cap (e.g. reject lengths > some bound) before the allocation in
  `ReadString`/`ReadBytes`, and/or use `maxRows` to bound server-side.
- Not blocking for MVP; note for hardening.

---

## P3 — Minor / cosmetic / documentation

### 11. `Session.autoCommit` mirrors the session state but is also modified in `ExecuteUpdatePreparedWithParams` to reflect the server's ack each update; `ResetSession` relies on it (P3)

> ☐ **Accepted as-is (documented):** `database/sql` guarantees serialized access to a `driver.Conn`; the mutex use in `conn.Close` is documented at `conn.go:40`. No code change.

- Reference: `session.go:299` (`s.autoCommit = autoCommit`) and `conn.go:187` (`if !c.sess.autoCommit`).
- Because every `COMMAND_EXECUTE_UPDATE` returns the server's current `autoCommit` and the code stores it, the field
  is kept fresh. But `conn.Close()`'s `if !c.sess.autoCommit` guard (rollback-before-close) reads it without holding
  the conn mutex in the non-busy path (it does hold it under `c.mu`). Fine today because `database/sql` guarantees
  serialized access to a `driver.Conn`; document the invariant to avoid future data races if that guarantee is ever
  relaxed.

### 12. `ResultComment` / leftover `ErrNotYetSupported` (P3)

> ☑ **Resolved (Task 8):** `ErrNotYetSupported` removed (nothing returned it); its test deleted.

- `errors.go` still defines `ErrNotYetSupported` ("operation not yet supported") but nothing returns it anymore
  (Prepare/Begin implemented in Phase 6/8). It exported a user-facing error that can never be produced. Either
  remove it or keep as documentation of the MVP boundary.

### 13. `ErrUnsupportedType` error text in `ReadValue` for complex types bundles `typeCode` and the wire code, which is good, but `valueTypeName` has no case for `ValueTypeDecfloat` (`UNKNOWN(31)`) (P3)

> ☑ **Already fixed:** both `valueTypeName` copies (`value_read.go:555`, `typeinfo.go:430`) have the `DECFLOAT` case. Verified during Task 8; no code change needed.

- Reference: `value_read.go:429` `valueTypeName`. `DECFLOAT=31` hits the `default` branch → `"UNKNOWN(31)"`.
  Add a `DECFLOAT` case for consistent diagnostics.

### 14. `connector.Driver()` allocates a fresh `Driver{}` on every call (P3)

> ☑ **Resolved (Task 8):** returns shared `defaultDriver`.

- Reference: `connector.go:56`. `driver.Connector.Driver` is called by `database/sql` rarely (once per pool init),
  but the returned `Driver` is a new instance. `Driver` is stateless, so this is functionally fine; returning the
  same value (e.g. a package-level `var defaultDriver`) would be marginally cleaner.

### 15. Docs use `—`/em-dash in `README.md` ("H2 protocol 21 directly — no PostgreSQL ...") — minor style; not an issue (P3)

> ☐ **Accepted as-is:** prose docs are exempt from the no-em-dash source rule.

- The repo guideline avoids em dashes in source; `README.md`/docs are prose so this is only a style consistency note.

### 16. `handshake_test.go` never tests context cancellation of the handshake (P3)

> ☑ **Resolved (Task 6):** `TestHandshakeContext_CancelMidHandshake` covers deadline mid-handshake. The test exposed (and we fixed) a real bug: `HandshakeContext` now uses named returns so the context error (`context.DeadlineExceeded`) replaces the raw socket timeout instead of being swallowed.

- Reference: `handshake_test.go` has no `DeadlineExceeded`/`Canceled` test for `HandshakeContext`. The feature is
  wired (dial deadline + post-dial watcher) but only the prepare path has a unit timeout test. Add a mock-server test
  that holds the socket open past a short deadline and asserts `context.DeadlineExceeded` and no half-open session.

---

## Confirmed-correct (cross-checked against reference, no action)

These were verified against H2 2.4.240 and/or `Transfer.java`/`TcpServerThread.java`:

- Password hashing matches `SHA256.getKeyPasswordHash` exactly (UTF-16-BE of `upper(user) + "@" + password`;
  empty user+password → empty hash; file prefix `"file"` not uppercased).
- Handshake frame matches `TcpServerThread.run` (min/max version, db, originalURL, uppercased user, hashes,
  property count; then `SESSION_SET_ID` + timezone for protocol ≥20; then status + autocommit boolean).
- Value wire codes (0–31) and the TI table (incl. gaps at 18 and 23) match `Transfer.java` `addType` exactly.
- `COMMAND_EXECUTE_QUERY` / `COMMAND_EXECUTE_UPDATE` / `RESULT_FETCH_ROWS` framing (status, rowCount, columnCount,
  autocommit ack, generated-keys frame) matches `TcpServerThread.process`.
- `ReadTypeInfo` field modes (precision-byte vs int32 vs long; interval fractional-second rules; NUMERIC ext flag;
  ARRAY recursion; ENUM/GEOMETRY/ROW ext parsing) match `writeTypeInfo20`/`readTypeInfo20`.
- `ResultColumn` serialization order (alias, schema, table, column, TypeInfo, [displaySize if <20], identity,
  nullable) matches `ResultColumn.writeColumn`.
- `decfloat`/`numeric` value handling and UUID (two-int64) match the reference.
- `SELECT 1` via internal query path and the transaction lifecycle (`SESSION_SET_AUTOCOMMIT`,
  `COMMAND_COMMIT`, SQL `ROLLBACK`, `SESSION_HAS_PENDING_TRANSACTION`) all behave correctly against a live server
  (full integration suite green: 251 tests, incl. 27 integration tests, under `-race`).

---

## Audit method / evidence

- Full read of `dsn.go`, `transfer.go`, `protocol.go`, `auth.go`, `handshake.go`, `session.go`, `command.go`,
  `conn.go`, `connector.go`, `driver.go`, `rows.go`, `stmt.go`, `tx.go`, `result.go`, `value_read.go`,
  `value_write.go`, `typeinfo.go`, `metadata.go`, `errors.go`, `generated_keys.go`, `logging.go`,
  `context_io.go`, `doc.go`, all `*_test.go`, `seed.sql`, `README.md`, `docs/ACCEPTANCE.md`, `Makefile`,
  `.golangci.yml`.
- Live probe: `SELECT id, col_blob, col_clob, col_json FROM type_showcase WHERE id=1` via the driver reproduces
  the inline-CLOB decode failure; the same query via JDBC Shell succeeds, isolating the issue to the driver's
  `readLOBValue` CLOB branch.
- Reference sources cross-checked: `org/h2/server/TcpServerThread.java`, `org/h2/value/Transfer.java`,
  `org/h2/security/SHA256.java` (H2 2.4.240 tag).
- Test totals at review time: 251 tests total (224 unit + 27 integration), all green under `-race` with H2 running;
  `CGO_ENABLED=0 go build ./...` passes.

## Suggested follow-up (not part of this review)

1. Decide inline-CLOB policy (decode or document-unsupported) and add a live CLOB integration test.
2. Add a `type_showcase` full-select integration test (guarantees the whole supported-type matrix stays green).
3. Harden `Session.Close`/`ResetSession` against dead sockets (mark dead on transport error).
4. Fix `NUMERIC` scale `ok` reporting for scale-less `-1` scale.
5. Consider exposing a documented `maxRows`/fetch-size configuration and add a handshake-cancellation unit test.
6. Post-MVP backlog alignment: LOB streaming, non-numeric generated keys, JSON/ENUM/ARRAY/ROW/INTERVAL decoding
   remain listed in `IMPLEMENTATION_PLAN.md` as post-MVP and are intentionally untouched.
