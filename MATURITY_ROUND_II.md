# MATURITY_ROUND_II.md — Second Maturity Review

> Purpose: record **findings only** for later analysis. Per project policy no source changes are made during this review.
> Date: 2026-08-22. Reviewer: codebase audit against H2 2.4.240 bytecode reference (`TcpServerThread.class`, `Transfer.class`, `ValueInterval`, `ValueJson` disassembled from `h2-data/h2-2.4.240.jar`) **plus live probes** against the running local H2 server.
> Scope: full re-audit after the Round I fix wave (`PLAN_MVP_FIX.md`, all tasks resolved) and the follow-up commit `8b8c492` ("decode complex value types, stream LOBs, expose extended generated keys"). That commit is the main source of new findings.

Baseline state at review time: `go build ./...`, `go vet ./...`, `go test ./...`,
`go test -race ./...`, `go test -tags=integration -race ./...` all green;
`CGO_ENABLED=0 go build ./...` passes. Test totals: 238 top-level unit test
functions; 269 with the `integration` tag (~31 integration-only). Three files
have `gofmt` drift (finding 12). All findings below were reproduced or
verified against the live server where noted.

---

## P1 — Must fix before building on top / before any release that claims these features

### 1. `fetchLobOnDemand` violates the sequential-command nature of the protocol — fetch-on-demand LOBs fail whenever anything follows them in the batch (P1)

- Reference: `value_read.go` `fetchLobOnDemand` (writes `LOB_READ` op onto the session stream mid-row-parse, then immediately reads status).
- Evidence (live probe, reproducible): with `SELECT c, b FROM probe_lob WHERE id=1`
  (200 KB CLOB + BLOB), reading column 0 fails with
  `h2go: fetchLobOnDemand: unexpected status 15`. `15` is not a status — it is the
  **BLOB type code of column 1**: the rest of the row is already in flight ahead of the
  LOB_READ response. With two rows of one CLOB column the same query fails with
  `unexpected status 16777216` (= row-2 flag byte `0x01` followed by the next frame's
  zero byte — pure stream desync).
- Root cause: the H2 TCP protocol processes client ops sequentially per connection.
  During `COMMAND_EXECUTE_QUERY` the server writes the whole first batch (all rows,
  all values) before returning to its read loop to see the `LOB_READ`. Therefore the
  LOB_READ response lands **after** everything else already buffered in the batch.
  Verified against `TcpServerThread.class`: the `sendRows` loop runs to completion
  inside the execute-query case; the LOB case (`op=17`) can only run afterwards.
  The driver's design of eagerly materializing fetch-on-demand LOBs *inside*
  `Tr.ReadValue` during row parsing can therefore only work when the LOB happens to
  be the very last value of the last row of the batch.
- Why tests missed it: `TestIntegration_FetchOnDemandLOB` uses exactly the
  coincidental shape — single column, single row (`SELECT large_clob FROM t WHERE id = 1`).
- Secondary defect (same root area): when `fetchRows` fails mid-batch, `Rows.Close()`
  still fire-and-forgets `RESULT_CLOSE`/`COMMAND_CLOSE` while stale LOB_READ response
  bytes remain unread on the socket; the sticky-error path never aborts the session,
  so the **desynced connection is returned to the pool** and the next borrower reads
  garbage. Any mid-batch decode error should mark the session dead (or drain/resync),
  not just stick the error on the Rows object.
- Fix directions (for triage): defer LOB materialization until the current batch has
  been fully consumed (collect lazy handles during row parse, issue LOB_READs at batch
  boundaries, splice values in) — this mirrors how H2's own JDBC client defers
  `LobDataFetchOnDemand` reads until application consumption; and/or mark the session
  dead on any mid-stream decode error.

### 2. ~~Negative compound INTERVAL values decode incorrectly (remaining field not negated)~~ — RETRACTED (2026-08-22) (P1 → no action)

> ⛔ **Retracted after re-verification.** Cross-checked the driver's output against H2's
> own canonical text (`SELECT CAST(-INTERVAL ... AS VARCHAR)` via H2 Shell on the live
> server). H2 renders negative intervals with the sign on the **leading field only**,
> and the Go driver produces byte-identical output for every probed qualifier:
>
> | Expression | H2 VARCHAR text | Driver text |
> |---|---|---|
> | `-INTERVAL '1-6' YEAR TO MONTH` | `INTERVAL '-1-6' YEAR TO MONTH` | identical |
> | `-INTERVAL '2 12:30' DAY TO MINUTE` | `INTERVAL '-2 12:30' DAY TO MINUTE` | identical |
> | `-INTERVAL '2 3' DAY TO HOUR` | `INTERVAL '-2 03' DAY TO HOUR` | identical (modulo padding, see finding 3) |
>
> In H2's value model the remaining field always carries the same sign as the leading
> field (both stored positive with the sign in the negated ordinal byte); sign-on-leading
> is therefore the correct canonical rendering — the driver's `leading = -leading` is
> right, and no negation of `remaining` is wanted. Padding and fractional-second
> rendering gaps remain and are tracked under finding 3.

### 3. `formatInterval` produces misleading/invalid interval text for second-bearing qualifiers (P1/P2 boundary)

- Reference: `value_read.go` `formatInterval`.
- Evidence (live probes):
  - `INTERVAL '5.25' SECOND` → `"INTERVAL '5 0.250000000' SECOND"`.
    H2 stores SECOND as `leading`=whole seconds + `remaining`=nanos. Rendering it as
    space-separated "5 0.25" invents a second field for a single-field qualifier and is
    not a valid INTERVAL literal. Expected canonical text: `INTERVAL '5.250000000' SECOND`.
  - `INTERVAL '1 02:03:04.5' DAY TO SECOND` → `"INTERVAL '1 7384.500000000' DAY TO SECOND"`.
    The remaining field for seconds-bearing compound intervals is nanos-within-day;
    printing raw seconds count loses the hh:mm:ss grouping entirely.
  - Same class: HOUR TO SECOND, MINUTE TO SECOND.
- Verified gaps against H2's canonical text (`CAST(... AS VARCHAR)` via H2 Shell):
  - `INTERVAL '5.25' SECOND` → driver `"INTERVAL '5 0.250000000' SECOND"`, H2 `INTERVAL '5.25' SECOND`
  - `INTERVAL '7.750000000' SECOND` → H2 trims trailing zeros: `INTERVAL '7.75' SECOND`
  - `INTERVAL '1 02:03:04.5' DAY TO SECOND` → driver `"... 7384.500000000"`, H2 `INTERVAL '1 02:03:04.5' DAY TO SECOND`
  - `HOUR TO SECOND` / `MINUTE TO SECOND`: driver drops the hh:mm:ss grouping entirely
  - `INTERVAL '2 3' DAY TO HOUR` → H2 zero-pads the hour: `INTERVAL '2 03' DAY TO HOUR`;
    driver prints `'2 3'`
  - `INTERVAL '2 03:05' DAY TO MINUTE`-style padding also missing (H2 pads minutes)
  - `INTERVAL '1 02:03:04.0' DAY TO SECOND` → H2 omits the fraction when nanos = 0
- Correctly matching already: YEAR TO MONTH (`'l-r'`, months unpadded), HOUR TO MINUTE
  (`'h:mm'`, hours unpadded), sign-on-leading for all negatives, DAY TO MINUTE for
  2-digit subfields.
- Impact: PRD §7.14 documents INTERVAL → "string (human-readable interval text)";
  these outputs are neither round-trippable nor reliably readable.

### 4. `GeneratedKeysResult` is unreachable through the public API (P1)

- Reference: `result.go` (unexported type `result` carries exported field
  `GeneratedKeys *GeneratedKeysResult`), README line "extended generated-key APIs ...
  accessible via `GeneratedKeysResult` on the driver's `Result` type", CHANGELOG claim.
- Problem: users receive `sql.Result`; there is no exported method, wrapper interface,
  or accessor anywhere in the package to obtain the `*GeneratedKeysResult`. The only
  working access path is what `TestIntegration_GeneratedKeysMultiColumn` does:
  `db.Conn` + `conn.Raw()` + type assertion to `*result` — impossible outside the
  package because the type is unexported (the test compiles only because it is an
  internal test).
- Impact: the headline feature of commit `8b8c492` (multi-column/non-numeric generated
  keys) is dead code for external users, while docs advertise it as available.
- Fix direction: export an interface, e.g. `type GeneratedKeysProvider interface {
  GetGeneratedKeys() *GeneratedKeysResult }`, implement it on `result`, and document
  the `sql.Result.(h2go.GeneratedKeysProvider)` assertion.

---

## P2 — Fix before widening scope or claiming hardening

### 5. Unbounded allocations from wire-controlled lengths remain in the new decoders (P2)

Round I finding 10 added `MaxWireLength` caps to `ReadString`/`ReadBytes`, but commit
`8b8c492` reintroduced the same class of exposure elsewhere:

- `value_read.go` `readArrayValue`: `elems := make([]string, 0, length)` where
  `length` is a raw int32 from the wire (16 bytes/string-header ⇒ up to ~32 GB claimed
  allocation from 4 attacker-chosen bytes).
- `value_read.go` `readRowValue`: same pattern for ROW fields.
- `generated_keys.go` `readGeneratedKeys`: `make([][]driver.Value, 0, rowCount)` from
  the advertised rowCount.
- `value_read.go` `readLOBValue` inline-BLOB branch: `data := make([]byte, length)`
  with **no cap at all** — length is an int64 straight off the wire (up to 2^63−1).
  This is the worst instance; note the inline-CLOB branch right below it does cap
  (`maxInlineClobChars`).
- Minor related nit: `maxInlineClobChars` (268 M chars) still permits a ~256 MB
  `strings.Builder.Grow` before any payload byte is read; consider capping nearer
  `MaxWireLength` or growing incrementally.
- Consistency suggestion: one shared guard helper used by every decoder that trusts a
  wire length.

### 6. Mid-batch decode errors poison the pooled connection without marking the session dead (P2)

- Reference: `rows.go` (`Rows.Next` sets sticky `r.err`, `Rows.Close` sends
  RESULT_CLOSE/COMMAND_CLOSE best-effort), `conn.go` (release via closeCallback).
- Any error inside `fetchRows`/`ReadValue` leaves unread bytes in flight (row flags,
  remaining columns, possibly queued LOB responses — see finding 1). The subsequent
  fire-and-forget closes cannot restore alignment, yet `ResetSession`/`IsValid` may
  still pass later if the leftover bytes happen to parse, and at best the next real
  command gets garbage. Today only transport errors trigger `Abort()`; protocol-desync
  errors do not.
- Fix direction: set a "stream unusable" flag on the Session (or call `Abort()`) when
  a result-read error occurs, so the pool discards the conn deterministically.

### 7. `Config.Params` is parsed but never consumed — JDBC parameters are silently ignored (P2)

- Reference: `dsn.go` populates `cfg.Params` (semicolon params for JDBC URLs, query
  params for native URLs); grep shows no reader anywhere in the driver.
- Impact: URLs like `jdbc:h2:tcp://host/db;IFEXISTS=TRUE` behave as if the parameter
  were absent (H2 will happily auto-create the DB), which can surprise users who rely
  on existence checks; `AUTO_SERVER`, `ACCESS_MODE_DATA` etc. likewise no-op silently.
- PRD §7.2 requires "Support H2 JDBC URL parameters where practical" — parsing-only
  may be acceptable for MVP, but the behavior must be **documented** (which params are
  ignored) or critical ones rejected/warned. Currently nothing in README/doc.go mentions it.

### 8. Generated-keys mode is connection-level configuration, not per-statement (P2, API shape)

- Reference: `Config.GeneratedKeysMode/Set/Columns/ColumnNames` live on the DSN config;
  `Session.generatedKeysMode()` reads them for every update on that session.
- JDBC semantics are per-statement (`Statement.RETURN_GENERATED_KEYS` /
  `NO_GENERATED_KEYS`). To vary modes today a user must open separate `*sql.DB`
  handles. Also `GeneratedKeysNone == 0` forces the `GeneratedKeysModeSet` escape
  hatch, which is easy to forget. Document the limitation explicitly or add a
  per-context/per-statement override mechanism later.

### 9. Integration tests assert far less than the documented behavior they claim to cover (P2)

- `TestIntegration_ComplexTypeDecoding` asserts only `strings.Contains(intervalStr, "1")`
  for INTERVAL — it passes with the broken negative-interval output (finding 2) and the
  malformed fractional-second formats (finding 3). No negative interval, no
  DAY TO SECOND / SECOND fractional assertions exist.
- `TestIntegration_FetchOnDemandLOB` covers only single-column/single-row selects
  (the coincidentally-working shape; finding 1). Multi-row batches, LOB-after-LOB,
  and LOB-followed-by-scalar are untested.
- `ARRAY` assertion does not pin the NULL-element rendering (see finding 10).
- These weak assertions are why three P1-class defects landed green. Tighten them when
  fixing findings 1–3.

---

## P3 — Minor / cosmetic / documentation

### 10. ARRAY NULL-element rendering is unspecified (P3)

Live probe: `ARRAY['a','b,c',NULL]` → `"[a,b,c,<nil>]"` (Go `%v` of nil). Fine as a
documented fallback once it is actually documented in README/PRD §7.14; currently the
docs just say "comma-separated elements in brackets".

### 11. `isLastInsertIDUnavailable` matches by error-string substring (P3)

`generated_keys.go`: `strings.Contains(err.Error(), ErrLastInsertIDUnavailable.Error())`.
Fragile if wording changes and defeats wrapping. Use `errors.Is(err, ErrLastInsertIDUnavailable)`
(the constructor already wraps with `%w`; the string check exists only because some
callers wrap without `%w` — fix the wrapping instead).

### 12. Dead code and gofmt drift introduced post-v0.1.0 (P3)

- `generated_keys.go`: `discardGeneratedKeyRows` and `readGeneratedKeysLastInsertID`
  have zero callers (not even tests). Remove or wire up.
- `gofmt -l`: `protocol.go`, `value_read.go`, `value_read_test.go` (comment
  indentation and struct-field alignment). Trivial, but note `make lint` silently
  skips when `golangci-lint` is not installed (as on this machine), so formatter
  regressions surface only manually.

### 13. JSON representation quirk worth documenting (P3)

Live probe: `SELECT '{"k":1}'::JSON` returns `[]byte("\"{\\\"k\\\":1}\"")` — i.e. the
payload includes H2's outer quoting. Cross-checked against H2's own tools: the H2 Shell
displays the identical string, so the driver faithfully returns what H2 sends. Add a
README note ("bytes are exactly what H2 serializes; quoting included") so nobody
files this as a driver bug.

### 14. ENUM parameters ride as VARCHAR (P3)

`WriteValue` maps string params to VARCHAR even when param metadata says ENUM
(no `ValueTypeEnum` case). Live probe confirms INSERT succeeds via server-side
coercion, so this is acceptable — but it is undocumented behavior; one sentence in
README ("ENUM parameters are sent as strings and coerced server-side") would close it.

### 15. Inline-CLOB surrogate edge decodes to U+FFFD (P3)

`readClobChar` implements H2's 1/2/3-byte scheme correctly (supplementary chars arrive
as surrogate-pair halves, each ≤ 3 bytes ✓ verified against bytecode). However a lone
surrogate half produces a rune in D800–DFFF which `sb.WriteRune` silently replaces
with U+FFFD — data substitution without any signal. Edge-case only; a comment or debug
log would suffice.

### 16. Fetch-on-demand BLOB frame field naming confusion (P3)

`readLOBValue` names the single trailing precision long `charLength` for BLOBs and
then treats it as total bytes in `fetchLobOnDemand` ("BLOB sends precision in
charLength field"). Functionally correct (verified: BLOB frame is
`tableId:int, lobId:long, hmac:bytes, octetLength:long`; CLOB additionally has
octetLength+charLength), but the naming invites future bugs. Rename/re-comment.

### 17. `readArrayValue` ignores the skip-path ReadString error (P3)

In the legacy `length < 0` branch, `_, _ = tr.ReadString()` discards the error; a
truncated stream then continues misaligned into element decoding. Fold into finding 6's
error handling or return the error.

### 18. `localTimeZoneID` trusts `TZ` verbatim (P3)

`handshake.go` sends `os.Getenv("TZ")` unchanged; a garbage TZ value is forwarded to
the server during SESSION_SET_ID. Harmless against lenient servers; consider
validating with `time.LoadLocation`.

---

## Confirmed-correct (cross-checked this round, no action)

These were specifically re-verified against H2 2.4.240 bytecode and/or live probes:

- **Interval wire format** (ordinal encoding): Java writes `~ordinal` (bitwise NOT)
  for negatives; Go's `^x & 0x7f` recovery is exact for ordinals 0–12. Remaining-long
  presence rule (ordinals ≥ 5, incl. YEAR TO MONTH / DAY TO HOUR / DAY TO MINUTE /
  HOUR TO MINUTE) matches the `writeValue` tableswitch (cases 22–26 vs 27–34).
  Negative rendering (sign on leading field only) byte-matches H2's own canonical text
  (see finding 2, retracted).
- **Fetch-on-demand LOB frame layout**: `length=-1, tableId:int, lobId:long,
  hmac:bytes[, octetLength:long if v≥20], charLength:long` — Go parse order is exact
  for both CLOB and BLOB. Manual wire replay of a LOB_READ (status=1, 64 KiB chunk)
  works, isolating finding 1 to interleaving, not framing.
- **LOB_READ request/response**: request `op:long lobId, hmac:bytes, offset:long,
  len:int`; response `status, actualLen:int, raw bytes`; server caps chunk at 64 KiB
  (`Math.min(65536, len)`), matching `lobReadChunkSize = 16*4096`. Loop-until-zero is
  valid (server returns readFully count, 0 at EOF).
- **Inline LOB frames**: BLOB `len:long + raw + magic(0x1234)`; CLOB
  `charLength:long + Data.copyString chars + magic` — Go decoders match, magic values
  confirmed (sipush 4660 = 0x1234).
- **Generated keys framing**: request mode int (+count+ints or count+strings for
  COLUMN_NUMBERS/COLUMN_NAMES) matches `readGeneratedKeysRequest`; response
  `status, rowCount, meta…, sendRows` matches `sendGeneratedKeys`; no keys frame when
  mode NONE (server skips via Boolean.FALSE identity check) — Go's conditional read is
  consistent.
- **sendRows terminator semantics**: terminator byte `0` is written only when
  `next()` exhausts early; when all promised rows were sent there is **no** trailing
  marker. Go's `fetchRows` counting and the `rowCount<=0 ⇒ no marker` comment in
  `readGeneratedKeys` are both correct.
- **Cancel side channel**: control connection `versions, null db, null url, sessionId,
  op=13, stmtId` matches the server's control-connection detection (db==null &&
  url==null branch) exactly.
- **SESSION_PREPARE vs SESSION_PREPARE_READ_PARAMS2**: shared handler; READ_PARAMS2
  adds cmdType int and per-param `TypeInfo + nullable:int`
  (`ParameterRemote.writeMetaData` order confirmed).
- **Update response frame**: `status(int), updateCount(long), autoCommit(bool)` ✓.
- **ENUM read**: ordinal int32 for v≥20 ✓; SMALLINT readShort for v≥20 ✓;
  TIMESTAMP_TZ/TIME_TZ offset in seconds for v≥19 ✓ (v<19 ×60 path unreachable since
  driver pins protocol 21).
- **JSON bytes** match H2's own rendering (finding 13 — doc-only).
- **DECFLOAT** round-trips as exact decimal string (live: 28-digit value preserved).
- **ENUM string parameter** coerces server-side (live INSERT OK).
- Round I fixes verified intact in passing: NUMERIC scale `-1 → ok=false`, dead-session
  abort in `ResetSession`, bounded `Session.Close`, `ReadString/ReadBytes` caps,
  handshake-cancellation test, `defaultDriver` singleton, alias-vs-name documentation.

---

## Audit method / evidence

- Full read of all `.go` sources and tests, `Makefile`, `.golangci.yml`, `README.md`,
  `doc.go`, `CHANGELOG.md`, `docs/ACCEPTANCE.md`, `PRD.md` diff of commit `8b8c492`,
  `IMPLEMENTATION_PLAN.md` backlog section, `PLAN_MVP_FIX.md`, `MATURITY_MVP.md`.
- Bytecode cross-check: `javap -p -c -constants` over
  `org/h2/value/Transfer.class` (`writeValue`, `readValue`, `writeTypeInfo20`,
  `readLob`, `readArrayElements`), `org/h2/server/TcpServerThread.class` (run/process
  switch incl. cases 0–20, `sendRows`, `sendGeneratedKeys`,
  `readGeneratedKeysRequest`, LOB case), `org/h2/value/Value.class` constants,
  `org/h2/value/ValueInterval`, `org/h2/util/IntervalUtils.appendInterval`,
  `org/h2/expression/ParameterRemote.writeMetaData`, `org/h2/value/ValueJson`.
- Live probes (temporary module replacing `github.com/rom35-cz/h2go` against the
  running local H2 2.4.240):
  - positive/negative intervals across five qualifiers (findings 2, 3);
  - ARRAY with NULL element, ROW, GEOMETRY, JSON, DECFLOAT, ENUM decode (findings 10, 13);
  - fetch-on-demand CLOB/BLOB single-column (OK) vs two-column (fail, status 15) vs
    multi-row (fail, status 16777216) (finding 1);
  - manual low-level LOB_READ replay proving framing correctness (isolates finding 1);
  - ENUM string-parameter insert (finding 14); multi-column generated keys behavior.
- Toolchain: `go build/vet/test/test -race`, integration suite under `-race` (green),
  `CGO_ENABLED=0 go build`, `gofmt -l` (three files), `go test -list` counts.
- Temporary diagnostic artifacts (probe module, throwaway tables `probe_lob`,
  `zz_lob`, `zz_lob2`, `zz_enum`) were removed after the audit; the review made no
  changes to tracked repository files other than this document.

## Addendum — new finding discovered during lint fix verification (2026-08-22, fixed in same session)

### 19. Context deadline races with the transport deadline — intermittent raw `i/o timeout` and missed session abort (P2, fixed)

- Reference: `context_io.go` `beginOperationContext` sets the transport deadline to
  `ctx.Deadline()`; `session.go` `finalizeContext` (and `handshake.go`'s error defer)
  replace the operation error with `ctx.Err()` only when `ctx.Err() != nil`.
- Evidence: `TestSession_PrepareCommandContextTimeoutAbortsSession` failed
  intermittently (~2 of ~20 full-suite runs, reproduced in a 12-run verbose loop):
  `PrepareCommand error = read pipe: i/o timeout, want context deadline exceeded`.
- Root cause: at the deadline instant the pipe's deadline timer and the context timer
  fire at the same target time; the transport read can fail a few microseconds before
  the context timer marks the context done, so `ctx.Err()` is still `nil` when
  `finalizeContext` runs. The raw timeout escapes **and `Abort()` is skipped**, leaving
  a half-dead session that could otherwise be returned to the pool.
- Fix: in `finalizeContext` (and `HandshakeContext`'s error defer), when the error is a
  timeout-kind `net.Error` and the context deadline has already passed
  (`time.Now().After(deadline)`, monotonic and therefore deterministic), report
  `context.DeadlineExceeded` and abort the session exactly as the `ctx.Err()` path does.
- Verification: the previously flaky tests pass 40/40 stress iterations
  (25x session + 15x session+handshake), full unit/race/integration/race-integration
  suites green, `golangci-lint` 0 issues.

## Suggested triage order (not part of this review)

1. Finding 1 (+6): rework fetch-on-demand LOBs to defer materialization to batch
   boundaries; mark sessions dead on mid-stream errors. Extend `TestIntegration_FetchOnDemandLOB`
   to multi-column/multi-row shapes first so the regression is observable.
2. Findings 2–3: interval **formatting** (finding 2 was retracted; only finding 3
   remains — fractional-second qualifiers and zero-padding); tighten
   `TestIntegration_ComplexTypeDecoding` with golden strings from the H2 Shell matrix.
3. Finding 4: expose `GeneratedKeysResult` via an exported interface; fix README/CHANGELOG wording.
4. Finding 5: shared wire-length guard across array/row/generated-keys/inline-BLOB paths.
5. Findings 7–9: document ignored JDBC params, generated-keys scoping; strengthen weak assertions.
6. P3 sweep: dead code, gofmt, error-wrapping hygiene, doc notes (JSON, ENUM params, ARRAY NULL).
