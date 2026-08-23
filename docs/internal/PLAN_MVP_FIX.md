# MVP Fix Plan — `github.com/rom35-cz/h2go`

Date: 2026-08-22
Source: `MATURITY_MVP.md` findings (P1/P2/P3)
Target: H2 **2.4.240+**, native TCP protocol **21** only
Rule: Tasks run **strictly in order**; every task ends green (`go build ./...`, `go vet ./...`,
`go test ./...`, and integration tests with a live H2 server). No task may depend on a later one.

Reference material already gathered (H2 2.4.240):
- `TcpServerThread.java` (handshake/process framing, generated-keys frame)
- `Transfer.java` (value codec, TypeInfo20, LOB frames, `LOB_MAGIC=0x1234`)
- `Data.java` `copyString` (inline CLOB payload = UTF-8, **character length not byte length**)
- `DataReader.java` `readChar` (UTF-8 decoding rules used by the CLOB parser)
- H2 docs `commands.html` `SET MAX_LENGTH_INPLACE_LOB` / `advanced.html` ("Small LOB objects are stored in-place")

---

## Task 1 — Decode inline CLOB values (P1 finding 1)

**Goal:** Reading a non-null inline CLOB returns its text as `string`, matching PRD §7.14 and the README.

**Context (verified against H2 2.4.240 source):**
`Transfer.writeValue` for `CLOB` writes:
```
writeInt(CLOB)               // 16
writeLong(charLength)        // number of CHARACTERS, not bytes
Data.copyString(reader, out) // UTF-8 payload; length varies per char
writeInt(LOB_MAGIC)          // 0x1234
```
So the Go decoder must consume **exactly `charLength` UTF-8 code points**, then read and validate the
trailing `0x1234` magic (the same magic `readLOBValue` already validates for BLOB). The existing
`readLOBValue` (`value_read.go:229`) returns `ErrUnsupportedType` for the inline-CLOB path and never reads
the payload, which also leaves the stream misaligned after the error.

**Work (`value_read.go`):**
- In the `readLOBValue` inline-CLOB path: read the `charLength` value (already in `length`), then decode
  `length` UTF-8 code points from `t.r` using the same multi-byte decoding rules as `DataReader.readChar`
  (1-byte `<0x80`, 2-byte `0x80..0xDF` → `0xc0|.., 0x80|..`, 3-byte `>=0xE0`). Consecutive code points
  are concatenated into a Go `string`. Then read `int32` and verify it equals `lobMagic`; return the string.
- Guard against pathological `charLength` (see Task 4's alloc-cap rule) and return a clear error if the
  stream ends mid-payload.
- Keep the fetch-on-demand (`length == -1`) branch unchanged (still `ErrUnsupportedType`, stream correctly
  drained).
- Keep the inline-BLOB branch unchanged (it already verifies `lobMagic`).

**Tests (`value_read_test.go`):**
- Inline CLOB: ASCII text, multi-byte UTF-8 (e.g. `"héllo ☺"`), empty string, and `charLength` with a
  4-byte sequence where byte count ≠ char count, asserting `lobMagic` follows.
- Inline BLOB round-trip with magic (regression).
- `length == -1` CLOB/BLOB skip path returns `ErrUnsupportedType` and does not panic.
- Wrong magic returns a clear error.

**Docs:** In `PRD.md` §7.14 and `README.md` supported-types table: change `CLOB` note from "decoded as error"
(silent currently) to "inline CLOBs (≤ `MAX_LENGTH_INPLACE_LOB`) decoded as `string`; fetch-on-demand LOBs
return `ErrUnsupportedType` (LOB streaming is post-MVP)".

**Done when:** `go test ./...` green incl. new CLOB/BLOB tests; live probe of `type_showcase.col_clob`
previous failure now returns the CLOB text.

---

## Task 2 — Integration test: full `type_showcase` matrix (P2 finding 5)

**Goal:** Prove every MVP-supported type in the seeded showcase decodes correctly, all in one SELECT.

**Context:** `type_showcase` (seed.sql) already contains all MVP scalar types incl. `CHAR`, `VARCHAR_IGNORECASE`,
`BINARY`, `VARBINARY`, `TIME`, `TIME WITH TIME ZONE`, `TIMESTAMP WITH TIME ZONE`, `UUID`, `NUMERIC`, `DECFLOAT`,
plus `CLOB`/`BLOB`/`JSON`. No test reads it, so regressions like Task 1's CLOB bug go undetected.

**Work (`integration_test.go`):** add `TestIntegration_TypeShowcaseFullSelect`:
- `SELECT * FROM type_showcase WHERE id IN (1,2,3)` and scan every column into typed destinations.
- Row 1: assert each MVP scalar equals the seeded value (reuse the expected values from `seed.sql:190–200`).
- Row 2: assert zero/empty values (empty CLOB `''`, `X'00'` BLOB, empty JSON `'{}'`, epoch timestamps).
- Row 3: assert every column is `nil` (NULL decode path for every type).
- `JSON` and any still-unsupported type (`JSON` is decoded as `[]byte` today; `decfloat` as `string`) must
  assert the **documented** representation, not fail. If a column type is intentionally unsupported, select
  it explicitly and assert `ErrUnsupportedType` (do not let it break the rest of the row).

**Done when:** The full-matrix test passes against live H2 2.4.240 and is listed in
`docs/ACCEPTANCE.md` under §10.3 "scan common scalar values".

---

## Task 3 — Fix `NUMERIC` scale reporting for scale-less decimals (P1 finding 3)

**Goal:** `sql.ColumnType.DecimalSize()` reports "unknown" for `NUMERIC(a)` without declared scale instead of
falsely claiming scale 0.

**Context:** `TypeInfo.PrecisionScale` (`typeinfo.go:451`) clamps `Scale<0` to `0` for `NUMERIC`, the same
class of "known-zero leakage" Phase 10 already fixed for time types. The wire sends `scale=-1` when no scale
was declared.

**Work (`typeinfo.go`):**
- In the `ValueTypeNumeric` branch: if `ti.Scale < 0`, return `(precision, 0, false)`.
- Keep DECFLOAT and time-type branches unchanged (already correct).

**Tests (`typeinfo_test.go`):** add cases: `NUMERIC(12)` → `(12, 0, false)`; `NUMERIC(12,4)` → `(12,4,true)`;
`DECFLOAT(20)` → `(20,0,true)`; `TIMESTAMP` with unknown scale → `(0,0,false)`.

**Done when:** unit tests pass; live `ColumnTypes()` integration test still reports `AMOUNT NUMERIC(12,4)` as
`(12,4,true)`.

---

## Task 4 — Defensive caps for wire lengths before allocation (P2 finding 10)

**Goal:** A broken/hostile server cannot force a giant allocation from a 32-bit length field.

**Work (`transfer.go`):**
- In `ReadString` / `ReadBytes`, reject `length` larger than a documented cap for MVP string/bytes payloads
  (e.g. `MaxWireLength = 512 MiB` constant) with a clear `ErrUnsupportedType`-style error mentioning the cap.
  The H2 docs cap VARCHAR at 1_000_000_000 chars, but the driver does not need to pre-allocate that today;
  the cap is a DoS guard, not a semantic limit.
- Apply the same guard to `charLength` inside the Task 1 CLOB decoder.

**Tests (`transfer_test.go`):** feed a length slightly above the cap and assert a clear error, no allocation
of that size. Keep existing round-trip tests (all below the cap).

**Done when:** unit tests pass; existing integration suite still green.

---

## Task 5 — Harden `Session.Close()` and `ResetSession` against dead sockets (P1 finding 2)

**Goal:** A broken session is discarded from the pool (`ErrBadConn`) and never reused; `Close()` cannot
block indefinitely on a dead socket.

**Context:** `ResetSession` (`conn.go:163`) returns `ErrBadConn` on transport errors but does not abort the
session, so the broken conn can be handed to another borrower that re-attempts I/O; `Session.Close`
(`session.go:47`) waits for the `STATUS_OK` reply with no deadline.

**Work:**
- `conn.ResetSession`: on any transport/session error from `hasPendingTransaction` /
  `rollbackCurrentTransaction` / `setAutoCommit`, call `c.sess.Abort()` (marks session dead) before
  returning `ErrBadConn`.
- `session.go Close()`: wrap the `WriteInt32(SessionClose)+Flush+ReadInt32` sequence in a short deadline
  (e.g. 2s) via `tr.SetDeadline`, clearing it afterward, so a half-open peer cannot stall the close. Preserve
  the existing best-effort semantics (errors are discarded, transport always closed).
- Add unit tests: `Session.Close` on a transport that never answers the `STATUS_OK` read returns within the
  deadline; `ResetSession` on a failed probe marks the session dead (subsequent `acquire` → `ErrBadConn`).

**Done when:** unit tests green; live pool-reuse integration test plus `TestIntegration_ValidatorReportsLiveSession`
still pass.

---

## Task 6 — Handshake cancellation unit test (P3 finding 16)

**Goal:** Verify `HandshakeContext` aborts cleanly when the context deadline fires mid-handshake.

**Work (`handshake_test.go`):** add `TestHandshakeContext_CancelMidHandshake`:
- Start a `net.Listen` mock server that accepts, reads the credential frame, and deliberately stalls
  (never answers `STATUS_OK`).
- Call `HandshakeContext` with a `context.WithTimeout` of ~50ms; assert the returned error is
  `context.DeadlineExceeded` (wrapped) and that the session is not leaked: the underlying socket is closed
  (assert the accept loop sees EOF/RST).

**Done when:** test passes under `-race`; no goroutine leak (mock server goroutine terminates).

---

## Task 7 — Optional result-binding options: `maxRows` / `fetchSize` (P2 finding 4)

**Goal:** Applications can bound server-side result size and control prefetch batch size, matching
JDBC `Statement.setMaxRows(max)`.

**Work:**
- Add optional `Config` fields: `MaxRows int64` (0 = unlimited, current default) and `FetchSize int`
  (0 = `defaultFetchSize` 100). Document semantics: `MaxRows` is forwarded as the protocol `maxRows`,
  `FetchSize` as the fetch size; both default to current behavior.
- Thread them through `connector/conn/query paths` (`QueryContext`, `queryContextInternal`, `stmt.QueryContext`,
  `Ping`) so all query entry points use `cfg.MaxRows` / effective fetch size.
- Update `README.md` (new "Result options" subsection) and `doc.go` briefly.

**Tests:**
- Unit: connector captures `Config.MaxRows`/`FetchSize` (no mutation of caller's `Config`, mirroring
  `TestNewConnectorDoesNotMutateCfg`).
- Integration: `TestIntegration_MaxRows` — create a 10-row table, set `MaxRows=3`, assert exactly 3 rows
  return and the result set ends (no mid-stream error); `fetchSize` > row count yields all rows.

**Done when:** live test shows `MaxRows` limits server-side rows for both inline and prepared queries;
existing `TestIntegration_QueryLargeResult` still exercises multi-batch prefetch at default 100.

---

## Task 8 — Minor cleanup (P3 findings 12, 13, 14, 6/docs)

**Work (small, static-only changes):**
- `value_read.go` `valueTypeName`: add `ValueTypeDecfloat` → `"DECFLOAT"` case.
- `errors.go`: remove the now-dead exported `ErrNotYetSupported` (nothing returns it since Phase 6/8) and
  delete its test; or, if kept for API stability, mark it deprecated in a doc comment. Decision: remove —
  this is pre-v0.2 and the symbol was never usable.
- `connector.go` `Driver()`: return a package-level `var defaultDriver = &Driver{}` instead of allocating,
  to avoid churn on every pool setup.
- `metadata.go`/`README.md`: document that `Rows.Columns()` returns H2 **labels** (alias), and
  `GetColumnByName` matches aliases only (expression columns may differ from column name).
- `README.md`/`doc.go`: add a short "Examples run against the local H2 env; they use throwaway tables
  (`example_*_<nanotime>`)" note so a reader isn't surprised by tables appearing in the seed DB.

**Done when:** `go build`, `go vet`, `golangci-lint`, full unit + integration suite green; no dead symbol.

---

## Task 9 — Final validation, docs, and changelog

**Goal:** Acceptable post-fix state with full verification and recorded outcome.

**Work:**
- Update `docs/ACCEPTANCE.md`: add `TestIntegration_TypeShowcaseFullSelect` and `TestIntegration_MaxRows`
  rows; update CLOB row if behavior changed.
- Update `CHANGELOG.md` with a `v0.2.0`-preview "unreleased" section listing the fixes
  (CLOB decode, NUMERIC scale reporting, dead-session hardening, maxRows/fetchSize options, defensive caps).
- Mark `MATURITY_MVP.md` findings resolved as the tasks land (status line in each finding).

**Done when (full suite):**
```
go build ./...
go vet ./...
golangci-lint run ./...
go test ./...
go test -race ./...
go test -tags integration ./...
go test -tags integration -race ./...
CGO_ENABLED=0 go build ./...
make test-integration-race
```
All green against a freshly-seeded local H2 2.4.240.

---

## Out of scope for this plan (stays in the post-MVP backlog)

- Fetch-on-demand LOB streaming (`LOB_READ`) — still returns `ErrUnsupportedType`.
- JSON/DECFLOAT/ARRAY/ROW/ENUM/GEOMETRY/INTERVAL/JAVA_OBJECT decoding beyond the documented fallback
  (`JSON` → `[]byte`, `DECFLOAT` → `string`) — no behavior change in this plan.
- Multi-column / non-numeric generated keys, TLS, multiple result sets, benchmarks.
- Changing the public API shape beyond the additive `Config.MaxRows` / `Config.FetchSize` fields.

## Traceability

| Task | MATURITY_MVP.md finding(s) |
|---|---|
| 1 | 1 (inline CLOB) |
| 2 | 5 (type_showcase untested) |
| 3 | 3 (NUMERIC scale) |
| 4 | 10 (alloc cap) |
| 5 | 2 (dead-socket hardening) |
| 6 | 16 (handshake cancellation test) |
| 7 | 4 (maxRows/fetchSize) |
| 8 | 12, 13, 14, 6 (cleanup + docs) |
| 9 | — (validation + docs) |
