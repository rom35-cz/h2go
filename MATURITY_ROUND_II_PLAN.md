# MATURITY_ROUND_II_PLAN.md — Fix Plan for Round II Maturity Findings

Date: 2026-08-22 (reviewed and corrected same day)
Source: `MATURITY_ROUND_II.md` findings (P1/P2/P3, with finding 2 retracted by
re-verification against H2's canonical interval text). Finding 19
(context-deadline race) was discovered and fixed after this plan was drafted
(commit `7a8c6f7`); it needs no task here, only a resolution marker in Task 10.
Target: H2 **2.4.240+**, native TCP protocol **21** only
Rule: Tasks run **strictly in order**; every task ends green (`go build ./...`,
`go vet ./...`, `go test ./...`, and integration tests with a live H2 server).
No task may depend on a later one.

Reference material already gathered (H2 2.4.240):
- Bytecode disassembly of `Transfer.class`, `TcpServerThread.class`,
  `ValueInterval`, `IntervalUtils.appendInterval`, `ParameterRemote`,
  `ValueJson` (see `MATURITY_ROUND_II.md` audit method).
- Live-verified H2 canonical INTERVAL text matrix (`CAST(... AS VARCHAR)` via
  H2 Shell, 2026-08-22) — embedded verbatim in Task 2.
- Live-verified fetch-on-demand LOB frame layout and LOB_READ request/response
  framing, and the sequential-op server behavior (`sendRows` completes inside
  the execute-query case before the LOB case is serviced).

---

## Task 1 — Resolve fetch-on-demand LOBs at batch boundaries (finding 1; partial 6; 16)

**Goal:** Reading a fetch-on-demand (length == -1) BLOB/CLOB works regardless of
where the LOB appears in the result batch: followed by more columns, more rows,
or a terminator.

**Context (verified against H2 2.4.240 and live probes):**
`TcpServerThread.process` handles ops sequentially on one connection. During
`COMMAND_EXECUTE_QUERY` the server writes the entire first batch (all rows and
values via `sendRows`) before returning to its read loop, so a `LOB_READ`
issued *mid-row-parse* is answered only **after** everything else of the batch
already in flight. Today `fetchLobOnDemand` (`value_read.go`) writes `LOB_READ`
and immediately reads the response inside `Tr.ReadValue`, which desyncs the
stream whenever the LOB is not the very last value of the last row of the
batch. Live reproductions (Round II audit):
- `SELECT c, b FROM probe_lob` → `unexpected status 15` (15 = BLOB type code
  of the next column), and
- two rows of one CLOB column → `unexpected status 16777216` (row-2 flag 0x01
  plus next frame byte).
`TestIntegration_FetchOnDemandLOB` only covers the coincidental single-column
single-row shape.

**Work (`value_read.go`, `rows.go`, `generated_keys.go`):**
- Add an internal collector for lazy LOB handles. Introduce an unexported
  placeholder type (e.g. `pendingLob{typeCode, lobID, hmac, octetLength,
  charLength, row, col}`); the fetch-on-demand branch of `readLOBValue` stops
  fetching immediately and instead records a placeholder in the collector and
  returns it as the (internal) value.
- Keep the exported `Tr.ReadValue` signature; add an internal variant (e.g.
  `readValueInternal(colType, collector)`) used by the row readers, with the
  exported method delegating to a nil collector (immediate fetch) so existing
  unit tests keep working. Thread the collector through `fetchRows`,
  `readGeneratedKeyRow`, and the ARRAY/ROW recursive decoders.
- Resolve **after the batch boundary**: when `fetchRows` has consumed all
  promised flags of the batch (or received the terminator byte / `noMoreRows`),
  walk the pending list **in wire order** and run the existing chunk loop
  (`lobReadChunkSize = 16*4096`, status=1 → len → raw bytes, stop at 0) for
  each LOB. Splice resolved values back into the buffered rows. Resolution
  errors: make the error sticky on the Rows **and call `Session.Abort()`
  directly** — the existing API already sets the session's `dead` flag and
  aborts the transport, which is all that is needed for the pool to discard
  the conn. (Task 5 later generalizes this dead-marking to *all* mid-stream
  decode errors; this task must not wait on it.)
- `readGeneratedKeys`/`readGeneratedKeyRow`: same collect-then-resolve pattern
  after the keys frame (all rows) has been fully consumed.
- ARRAY/ROW containers: if a fetch-on-demand LOB appears *nested inside* an
  ARRAY/ROW element, return a documented `ErrUnsupportedType` ("fetch-on-demand
  LOB inside ARRAY/ROW is not supported") instead of embedding a placeholder in
  the rendered string. This is an explicit, documented limitation — no silent
  corruption. (H2's own client can defer nested reads; we deliberately do not.)
- While rewriting `readLOBValue`, fix finding 16: rename the BLOB fetch-on-demand
  single-long field (`charLength` today) to something truthful (the wire sends
  octetLength/precision only for BLOB; CLOB sends octetLength + charLength)
  with comment referencing `Transfer.writeValue` cases 15/16.

**Tests:**
- Unit: placeholder mechanics — `readValueInternal` with a synthetic stream
  records and resolves pending LOBs in order; nil-collector path unchanged.
- Integration (extend `TestIntegration_FetchOnDemandLOB`):
  - two LOB columns in one row (`SELECT c, b` — the "status 15" shape);
  - multiple rows of one CLOB column (the "16777216" shape);
  - LOB followed by a scalar column (`SELECT c, id`);
  - mixed inline + on-demand LOBs in one row;
  - batch boundary crossing: LOB in row N of a multi-batch result. There is
    no per-statement fetch-size control (`driver.Stmt` has no SetFetchSize
    here); build a dedicated handle with a small batch via
    `ParseDSN(...)` + `cfg.FetchSize = 1` + `sql.OpenDB(NewConnector(cfg))`
    and select more rows than one batch holds;
  - single-column single-row regression (existing case must stay green).
- Integration: generated keys frame containing a fetch-on-demand LOB returns
  the value (numeric default path unchanged).

**Done when:** all previously failing probe shapes pass; existing suite green.

**Status: DONE** (2026-08-22). Implementation notes:
- Collector machinery lives in `value_read.go`: `pendingLob` (frame fields +
  `row`/`col` position), `lobCollector` (`pending`, `reject`, `curRow`,
  `curCol`), and a `nestedLOBs` sentinel whose `reject` flag propagates through
  recursive ARRAY/ROW decoding.
- `Tr.ReadValue` delegates to `readValueInternal(colType, lc)`; `lc == nil`
  keeps the legacy immediate-fetch behaviour (unit tests, standalone reads).
  Row readers pass a fresh `lobCollector` per batch/frame and set `curRow`/
  `curCol` before each column decode so placeholders know their slot.
- Batch boundary = end of `Rows.fetchRows` (promised flags exhausted or
  terminator flag 0 seen). Placeholders are stored directly in row buffers and
  spliced in wire order by re-running the chunk loop (`fetchLob`). Aligned
  `case -1` H2 error frames skip resolution (rows are discarded anyway).
- Same pattern in `Session.readGeneratedKeys` after the keys frame is fully
  consumed (both rowCount > 0 and eager rowCount < 0 paths).
- Finding 16: BLOB frames carry a single long now named `precision`; CLOB
  carries `octetLength` + `precision` (char length); verified against
  `h2-src/org/h2/value/Transfer.java` readValue BLOB/CLOB cases.
- Nested rejection returns an error wrapping both `ErrUnsupportedType` and the
  internal `errNestedOnDemandLOB` sentinel; row readers call `Session.Abort()`
  on it because the partially parsed container leaves batch bytes unconsumed
  on the wire. Resolution failures abort too (finding 6 partial).
- Test-writing gotchas for net.Pipe mocks: a zero-length `net.Pipe.Write`
  blocks forever (skip payload writes for EOF chunks), and every LOB chunk
  sequence needs a terminating `actualLen == 0` response or the client chunk
  loop never ends.
- Tests: `lob_test.go` (6 unit tests incl. two-row/two-LOB batch simulation
  and abort-on-resolution-failure); integration subtests for all five shapes
  plus a `Config.FetchSize = 1` dedicated handle crossing every boundary, and
  `TestIntegration_GeneratedKeysWithLob`. Full matrix green (build, vet,
  gofmt, lint 0 issues, unit+race, integration+race, CGO-free build).

---

## Task 2 — Canonical INTERVAL text formatting (finding 3; retraction bookkeeping for finding 2)

**Goal:** `formatInterval` output matches H2's own canonical interval text for
every qualifier, including fractional seconds and zero-padding.

**Context:** `MATURITY_ROUND_II.md` finding 2 (negative intervals) was retracted:
H2 renders negative intervals with the sign on the leading field only, and the
driver already matches byte-for-byte. Finding 3 remains: the seconds-bearing
qualifiers and hour padding are wrong. Verified H2 canonical matrix
(`SELECT CAST(<expr> AS VARCHAR)` on live H2 2.4.240):

Wire layout reminder: single-field SECOND is ordinal 5, so it carries BOTH
longs on the wire — leading = whole seconds, remaining = nanos.

| Expression | H2 canonical text |
|---|---|
| `INTERVAL '5.25' SECOND` | `INTERVAL '5.25' SECOND` |
| `INTERVAL '7.750000000' SECOND` | `INTERVAL '7.75' SECOND` (trailing zeros trimmed) |
| `INTERVAL '7' SECOND` (probe to confirm) | `'7'` — nanos = 0 ⇒ plain integer, no fraction |
| `INTERVAL '0.5' SECOND` | `INTERVAL '0.5' SECOND` |
| `-INTERVAL '5.25' SECOND` | `INTERVAL '-5.25' SECOND` |
| `INTERVAL '1 02:03:04.5' DAY TO SECOND` | `INTERVAL '1 02:03:04.5' DAY TO SECOND` |
| `INTERVAL '1 02:03:04.0' DAY TO SECOND` | `INTERVAL '1 02:03:04' DAY TO SECOND` (nanos=0 ⇒ no fraction) |
| `INTERVAL '0 00:00:00' DAY TO SECOND` | `INTERVAL '0 00:00:00' DAY TO SECOND` (always 2-digit) |
| `-INTERVAL '1 02:03:04.5' DAY TO SECOND` | `INTERVAL '-1 02:03:04.5' DAY TO SECOND` |
| `INTERVAL '0 02:03:04.5' DAY TO SECOND` (probe to confirm) | zero days still prints `'0 ...'` leading field |
| `INTERVAL '23:59:59.999999999' HOUR TO SECOND` | `INTERVAL '23:59:59.999999999' HOUR TO SECOND` |
| `INTERVAL '0:03:04' HOUR TO SECOND` (probe to confirm) | zero hours unpadded: `'0:03:04'` |
| `INTERVAL '2:03:04.5' HOUR TO SECOND` | `INTERVAL '2:03:04.5' HOUR TO SECOND` |
| `-INTERVAL '2:03:04.5' HOUR TO SECOND` | `INTERVAL '-2:03:04.5' HOUR TO SECOND` |
| `INTERVAL '3:04.5' MINUTE TO SECOND` | `INTERVAL '3:04.5' MINUTE TO SECOND` |
| `-INTERVAL '3:04.5' MINUTE TO SECOND` | `INTERVAL '-3:04.5' MINUTE TO SECOND` |
| `INTERVAL '2 3' DAY TO HOUR` | `INTERVAL '2 03' DAY TO HOUR` (hour padded) |
| `-INTERVAL '2 3' DAY TO HOUR` | `INTERVAL '-2 03' DAY TO HOUR` |
| `INTERVAL '2 3:05' DAY TO MINUTE` (probe to confirm) | hours/minutes padded `%02d:%02d` |
| `INTERVAL '2:03' HOUR TO MINUTE` | `INTERVAL '2:03' HOUR TO MINUTE` (hours unpadded) |
| `INTERVAL '1-6' / '1-0' / '0-1' YEAR TO MONTH` | unpadded `'l-m'` |

Rules derived:
- SECOND: `'<leading>.<nanos-trimmed>'`; nanos = remaining; trailing zeros of
  the 9-digit nanos trimmed; nanos == 0 ⇒ no fraction.
- Seconds-bearing compounds (DAY TO SECOND, HOUR TO SECOND, MINUTE TO SECOND):
  decompose remaining nanos into hh/mm/ss(.fraction) with `%02d` fields and
  fraction trimmed as above; DAY TO SECOND prefixes `<days> `.
- DAY TO HOUR / DAY TO MINUTE: remaining is hours / minutes within day; pad
  subfields to 2 digits.
- HOUR TO MINUTE: hours unpadded, minutes padded.
- Sign: prefix `-` on the leading field only (current behavior — keep).

**Work (`value_read.go`):** rewrite `formatInterval` to implement the rules
above; add a small helpers (`formatIntervalNanos`, `trimTrailingZeros`).
Keep stream decoding (`readIntervalValue`) untouched (verified correct).

**Tests (`value_read_test.go`):** golden-string table covering the full matrix
above plus: negative variants of every seconds-bearing qualifier, nanos=0
variants, `0.5`/`0 00:00:00` zero cases, and large values
(hours ≥ 100 in HOUR TO SECOND — verify H2 behavior first via Shell probe and
match it).

**Docs:** PRD §7.14 + README INTERVAL row: "decoded as H2's canonical interval
text" (one sentence; no format dump needed).

**Done when:** unit matrix green; a live probe comparing driver output vs
`CAST(<same expression> AS VARCHAR)` for 10+ expressions is identical; finding
2 retraction noted (already in MATURITY_ROUND_II.md), finding 3 marked resolved.

**Status: DONE** (2026-08-22). Implementation notes:
- `formatInterval` now mirrors H2's exact algorithm, transcribed from the
  reference source (`h2-src/org/h2/util/IntervalUtils.java` `appendInterval` +
  `DateTimeUtils.java` `appendNanos`): sign prefixes the leading field only;
  leading fields are never padded (`'2:03'`, `'100:03:04.5'`); sub-fields of
  compound qualifiers are two-digit padded (`'2 03'`, `'0 00:00:00'`); every
  `* TO SECOND` qualifier carries its whole time part as nanos in `remaining`
  (not plain seconds); fraction rendering is a dot plus nine digits with
  trailing zeros trimmed, omitted entirely when nanos == 0.
- Helper `formatIntervalNanos` implements `appendNanos` as `%09d` +
  `strings.TrimRight("0")` — verified equivalent to Java's division-based
  trimming because leading zeros are emitted before trimming and never removed.
  A separate `trimTrailingZeros` helper proved unnecessary.
- `readIntervalValue` untouched, per plan. Known pre-existing caveat (now
  documented): the wire's negation flag is folded into `leading`'s sign at
  decode time, so a negative interval whose leading field is 0 (e.g.
  `-INTERVAL '0 00:00:01'`) would lose its sign; not reachable through any
  probed engine output path. Revisit only if a real query shape requires it.
- Golden matrix: 30 expressions live-probed via H2 Shell on 2026-08-22,
  confirming all previously unverified rows: `'7' SECOND` → `'7'`; zero-day
  DAY TO SECOND keeps its `'0 '` leading field; zero-hour HOUR TO SECOND →
  `'0:03:04'`; hours ≥ 100 stay unpadded (`'100:03:04.567890123'`).
- Tests: `interval_test.go` — `TestFormatIntervalCanonicalMatrix` (33 golden
  cases across every qualifier) and `TestReadIntervalValueWireSigns` (5
  synthetic frames including complement-encoded negative ordinals);
  `TestIntegration_IntervalCanonicalMatrix` performs the Done-when live check
  permanently (18 driver-vs-CAST comparisons, all identical).
- Docs: README supported-types row split so INTERVAL reads "Decoded as H2's
  canonical interval text"; PRD §7.14 row now says "H2's canonical interval
  text". Finding 3's inline resolution marker lands with the Task 10 sweep.
- Validation: build, vet, gofmt, lint (0 issues), unit+race,
  integration+race, CGO-free build — all green.

---

## Task 3 — Public access to full generated-keys results (finding 4)

**Goal:** external users can reach `*GeneratedKeysResult` through the public API.

**Context:** `GeneratedKeysResult` is stored on the unexported `result` type;
no exported method or interface exposes it. The only working access is an
internal test's `db.Conn` + `conn.Raw()` + type assertion to `*result`
(impossible outside the package). README/CHANGELOG advertise it as accessible
— misleading.

**Critical access constraint:** `database/sql` wraps every driver result in
its unexported `driverResult` type before returning it as `sql.Result`, so
`res.(h2go.GeneratedKeysProvider)` on a result from `db.Exec(...)` **can never
succeed**. The assertion works only on a driver-level `driver.Result` obtained
via `sql.Conn.Raw()`:

```go
sqlConn, _ := db.Conn(ctx)
defer sqlConn.Close()
dc := sqlConn.Raw() // driver.Conn is *h2go.conn (implements driver.ExecerContext)
res, err := dc.(driver.ExecerContext).ExecContext(ctx,
    "INSERT INTO t(x) VALUES (1)", nil)
if gkp, ok := res.(h2go.GeneratedKeysProvider); ok {
    keys := gkp.GetGeneratedKeys()
}
```

**Work (`result.go`):** add an exported interface implemented by `*result`,
with its doc comment steering users to the working pattern above:

```go
// GeneratedKeysProvider is implemented by driver.Result values returned by
// h2go when generated keys were requested. Obtain the driver.Result via
// sql.Conn.Raw() plus a direct ExecContext call — results from database/sql's
// Exec/ExecContext are wrapped and do not expose this interface.
type GeneratedKeysProvider interface {
    GetGeneratedKeys() *GeneratedKeysResult
}
```

Wire it through `conn.ExecContext` / `stmt.ExecContext` (already carried on
`result`). Behavior of `LastInsertId()` unchanged.

**Tests:** new package-external test (a `package h2go_test` file, e.g.
`generated_keys_external_test.go`, build-tagged `integration`) doing
`db.Conn` + `Raw()` + direct driver-level `ExecContext` +
`driver.Result.(h2go.GeneratedKeysProvider)` assertion, proving reachability
from outside the package. Unit-level interface compliance assertion
(`var _ GeneratedKeysProvider = (*result)(nil)`).

**Docs:** README "Limitations" bullet shows the working Raw()-based snippet
and explicitly warns that `sql.Result` from `db.Exec` does NOT expose the
interface (database/sql wraps results); CHANGELOG wording corrected (drop
"on the driver's Result type" phrasing).

**Done when:** external-package test passes; docs snippet compiles.

**Status: DONE** (2026-08-22). Implementation notes:
- `result.go` now defines `GeneratedKeysProvider` and the `(*result).
  GetGeneratedKeys` accessor; interface-compliance assertion lives in the
  same `var _` block as the driver.Result one. The godoc comment carries the
  full working `sql.Conn.Raw()` pattern so the exported docs are self-teaching.
- No wiring changes were required: both `conn.ExecContext` and
  `stmt.ExecContext` already populated `result.GeneratedKeys`; only the
  exported surface was missing.
- `generated_keys_external_test.go` (`package h2go_test`, build tag
  `integration`) proves reachability strictly through the public API:
  driver-level result implements the provider, `GetGeneratedKeys` returns the
  key column/value, a `sql.Result` from `db.Exec` does NOT implement it
  (asserts the database/sql wrapping behaviour so doc drift fails loudly),
  and `LastInsertId` keeps working unchanged.
- Because the new file only compiles under `-tags=integration`, lint runs for
  this repo should include `golangci-lint run --build-tags integration ./...`
  (both variants are clean).
- README "Limitations" bullet replaced with the compilable Raw()-based
  snippet plus the explicit warning about db.Exec wrapping; CHANGELOG Added
  entry corrected (also fixes its pre-existing `GenerateKeys` field-name typo
  — the real field is `GeneratedKeys`).
- Validation: build, vet (default + integration tags), gofmt, lint (0 issues,
  both tags), unit+race, integration+race (external test passes against live
  H2), CGO-free build — all green.

---

## Task 4 — Wire-length caps for collection/value decoders (findings 5, 17)

**Goal:** no decoder pre-allocates from an unbounded wire-controlled length.

**Context:** Round I capped `ReadString`/`ReadBytes` (512 MiB). The Round II
decoders reintroduced uncapped allocations: `readArrayValue` /
`readRowValue` (`make([]string, 0, length)` from raw int32),
`readGeneratedKeys` (`make([][]driver.Value, 0, rowCount)`), and the inline
BLOB branch (`make([]byte, length)` from an unbounded int64 — worst case).
`maxInlineClobChars` (1<<28) still permits ~256 MiB pre-Grow.

**Work (`value_read.go`, `generated_keys.go`):**
- Add one shared guard constant/helper, e.g. `maxWireCollectionElements = 1 << 20`
  (1,048,576 elements/rows — far beyond any legitimate ARRAY/ROW/generated-keys
  payload), with a clear error naming the cap and the field. Apply to:
  ARRAY length, ROW length, generated-keys rowCount. Compare against the cap
  **as int64 before any int conversion** (generated-keys rowCount arrives as
  int64; converting first would overflow on 32-bit platforms).
- Inline BLOB: reject `length > MaxWireLength` before `make`, mirroring
  `ReadBytes` (same cap, same error style).
- Inline CLOB: keep a cap but make it consistent with `MaxWireLength`, and do
  not `strings.Builder.Grow(int(length))` up front — grow incrementally
  (e.g. initial small grow, let Builder self-expand) so a hostile length cannot
  force a giant single allocation before any payload byte is read.
- `readArrayValue` legacy negative-length branch (finding 17): stop discarding
  the skip-path error — return it (`fmt.Errorf("...: %w", err)`) and remove the
  `_, _ =` discard. (This branch runs on H2 ≥ 1.4 legacy frames; protocol 21
  servers write positive lengths, but the path must be correct.)

**Tests:** unit tests feeding oversized ARRAY/ROW lengths, generated-keys
rowCount, inline BLOB length (slightly above cap) and CLOB char length —
asserting the cap error and that no huge allocation occurs (run under
`-race`; keep lengths just above the caps so a regression would OOM visibly
rather than silently pass). Regression: normal small arrays/rows/keys still
decode.

**Done when:** unit suite green incl. new cap tests; no `make([]T, wireLen)`
without a guard remains in `value_read.go`/`generated_keys.go` (grep audit).

**Status: DONE** (2026-08-22). Implementation notes:
- Shared constant `maxWireCollectionElements = 1 << 20` in `value_read.go`,
  documented with the rule the whole task follows: compare as int64 BEFORE any
  int conversion so an int64 count can never be truncated into passing.
- Guards are placed **fail-fast**, before further wire parsing, not just
  before the allocation:
  - ARRAY: complement legacy length → cap check → skip type name → zero check
    (a hostile count is rejected without consuming anything);
  - ROW: a negative field count is rejected outright (`invalid field count`) —
    unlike ARRAY, Transfer.readValue never writes negative ROW counts, so this
    is a broken frame, not a legacy encoding;
  - generated-keys rowCount: guarded immediately after `ReadRowCount`, before
    result metadata is even parsed.
- Finding 17: the legacy ARRAY type-name skip error is now wrapped and
  returned (`failed to skip legacy type name: ...`) instead of `_, _ =`.
- Inline BLOB: `length > MaxWireLength` rejected before `make`, mirroring
  `ReadBytes`' error style.
- `maxInlineClobChars` re-derived as `MaxWireLength` (each CLOB char occupies
  at least one payload byte, so larger char counts can never be legitimate;
  replaces the independent 1<<28 magic number). The upfront
  `strings.Builder.Grow(int(length))` was removed entirely — Builder grows
  amortized, so a hostile/truncated frame now allocates almost nothing before
  failing at stream EOF.
- Beyond the letter of the plan, `fetchLob`'s response chunk length is now
  guarded too (`0 < actualLen <= lobReadChunkSize`; the server never sends
  more than requested): previously a hostile `actualLen` drove
  `make([]byte, actualLen)` unguarded, and a negative value was silently
  treated as end-of-data.
- Grep audit of remaining `make(` sites: all five in-scope allocations are now
  behind guards. `generated_keys.go:155` (`make([]driver.Value,
  meta.ColumnCount)`) is bounded by the columnCount that already built the
  ResultMeta slice — the true intake point is `ReadResultMeta`
  (`metadata.go`, also unguarded), which is outside this task's declared
  file scope; flagged for the Task 9 P3 sweep or backlog.
- Tests (`wire_caps_test.go`, 9 tests): ARRAY count cap incl.
  legacy-complement path, ROW cap + negative rejection, inline BLOB/CLOB caps
  just above the limit, generated-keys rowCount cap, fetchLob chunk-range
  guard over net.Pipe, finding-17 error propagation, and small-collection
  decode regressions.
- Validation: build, vet, gofmt, lint 0 issues (default + integration tags),
  unit+race, integration+race against live H2, CGO-free build — all green.

---

## Task 5 — Deterministic session discard on mid-stream decode errors (finding 6)

**Goal:** a connection whose stream can no longer be trusted is never reused.

**Context:** today a `fetchRows`/`ReadValue` error makes the error sticky but
leaves unread bytes in flight (remaining columns, queued LOB responses, next
row flags); `Rows.Close` then fire-and-forgets RESULT_CLOSE/COMMAND_CLOSE and
the conn returns to the pool. Recovery today is indirect (ResetSession probing
fails later); make it deterministic and immediate.

**Work (`rows.go`, `session.go`, `conn.go`):**
- Add a `streamBroken` (or reuse `dead`) marking path on `Session` used by the
  row readers: when `fetchRows`/`fetchMoreRows` fails while parsing a column
  value or row flag (i.e., any error that is not the aligned `case 0`
  terminator and not the aligned `case -1` H2 error frame), mark the session
  dead **before** returning the sticky error.
- `Rows.Close` already guards on `session.tr != nil`; keep that (Abort nils the
  transport) and additionally skip RESULT_CLOSE/COMMAND_CLOSE writes when the
  session is dead.
- The next borrow of that conn then fails `acquire()` with `driver.ErrBadConn`
  and `database/sql` discards it. No behavior change for the aligned error
  paths (server-side SQL errors inside a batch stay non-fatal for the session,
  matching H2 semantics: the result ends, the session continues).
- Apply the same rule to the generated-keys read path (Task 1's resolver).

**Tests:**
- Unit (synthetic/mock `Tr`): a `Tr` that fails mid-column → session dead;
  subsequent borrow of the conn fails (`ResetSession` returns
  `driver.ErrBadConn`, so `database/sql` discards it); `Rows.Next` after error
  returns the sticky error, `Rows.Close` performs no transport writes.
- Unit: aligned H2-error frame (`case -1`) does **not** mark the session dead.
- Integration: after the (fixed, previously failing) multi-column LOB query,
  the pool continues serving correct results on other queries (behavioral
  smoke check for the discard path).

**Done when:** unit tests green; full integration suite green; no path returns
a desynced conn to the pool.

**Status: DONE** (2026-08-22). Implementation notes:
- Two new Session helpers carry the rule: `markStreamBroken()` (intent-named
  `Abort`) and `markUnlessAlignedH2Error(err)` — an error is "aligned" iff it
  is a fully parsed `*h2go.Error` (`errors.As`), because `readH2Error` then
  has consumed the entire exception frame. Everything else (EOF, timeout,
  truncated frame, unknown flag) means the stream position is unknown.
- Discard coverage now spans the whole result-read lifecycle:
  - `NewRows`: row-count / metadata parse failures;
  - `fetchMoreRows`: named-return + defer marks on ANY failure except aligned
    H2 errors — including partially written requests. This closes a real
    pre-existing hole: a context timeout during row streaming (the deadline
    watcher fires the transport deadline mid-response) used to return the
    conn to the pool with an unread partial batch in flight;
  - `fetchRows`: row-flag read failure, missing column metadata, ALL column
    parse failures (blanket rule — subsumes Task 1's special-cased nested-LOB
    rejection), unexpected flags; aligned `case -1` frames stay alive;
  - generated-keys path mirrors all of it; the Task 4 rowCount cap guard
    deliberately does NOT mark (its fields are fully consumed, stream still
    aligned), and `ioEOF` early-stop stays aligned by definition.
- `Rows.Close` additionally checks `session.dead.Load()` before writing
  RESULT_CLOSE/COMMAND_CLOSE (belt-and-braces on top of the existing
  `tr != nil` guard that Abort already provides).
- `conn.go` required no changes: `acquire`/`ResetSession` already report
  `driver.ErrBadConn` for dead sessions — pinned by
  `TestResetSessionRejectsDeadSession`.
- Over-marking is deliberate where semantics are unknown (e.g. `unexpected
  status code`): discarding a possibly-fine conn is safe; reusing a broken
  one is not. Aligned server-side SQL errors keep the session usable,
  matching H2 semantics.
- The integration criterion (pool keeps serving after a fixed multi-column
  LOB stream) was already asserted by Task 1's "fetch size one crosses every
  boundary" subtest's post-streaming `COUNT(*)` probe; no new integration
  test needed.
- Mock-authoring gotcha recorded: net.Pipe is synchronous — the mock must
  consume the RESULT_FETCH_ROWS request (12 bytes) before its response can
  flush, and sendRows responses begin with STATUS_OK before any row bytes.
- Tests: six unit tests in `stream_discard_test.go` (mid-column failure with
  sticky Next + write-free Close, transport failure, aligned frame survival
  for both result and generated-keys paths, ResetSession rejection).
- Validation: build, vet (both tags), gofmt, lint 0 issues (both tags),
  unit+race, integration+race against live H2, CGO-free build — all green.

---

## Task 6 — Document parsed-but-ignored DSN/JDBC parameters (finding 7)

**Goal:** users are not surprised that `Config.Params` entries are not applied.

**Context:** `dsn.go` populates `cfg.Params` (JDBC `;KEY=VAL` segments and
native `?k=v`), but nothing in the driver consumes it — `IFEXISTS=TRUE`,
`AUTO_SERVER`, file-lock, etc. silently no-op (e.g. a DB may be auto-created
despite `IFEXISTS=TRUE`).

**Work (docs only + one log line):**
- README new subsection "DSN parameters" listing: which are consumed
  (`USER`, `PASSWORD` — extracted into Config), and that all others are
  **parsed but not applied**; explicit examples of the risky ones
  (`IFEXISTS`, `AUTO_SERVER`, `ACCESS_MODE_DATA`); recommendation to validate
  manually for now. Same short note in `doc.go`.
- `connector.Connect` (or `HandshakeContext`): when `len(cfg.Params) > 0`,
  emit one `slog.LevelDebug` record listing parameter **keys only** — values
  are never logged, so no redaction machinery is needed.

**Tests:** none behavioral; run DSN unit tests (unchanged) and confirm the
debug log appears with keys only when a logger is configured.

**Done when:** docs committed; `make lint`-clean; suite green.

---

## Task 7 — Document generated-keys config scope (finding 8)

**Goal:** the connection-level nature of `GeneratedKeysMode` is explicit.

**Context:** `Config.GeneratedKeysMode/Set/Columns/ColumnNames` are per-DSN;
every update on that session uses the same mode. JDBC semantics are
per-statement. Also `GeneratedKeysNone == 0` requires `GeneratedKeysModeSet`
as an escape hatch, which is easy to forget.

**Work (docs only):** README + `dsn.go` doc comments:
- State that the mode applies to all statements on connections created from
  this Config; to mix modes, use separate `*sql.DB` handles (per-statement
  overrides are future work).
- Keep the `GeneratedKeysModeSet` note in `dsn.go` (already present) and add a
  sentence to README so it is discoverable before filing bugs about
  `GeneratedKeysMode = GeneratedKeysNone` "not working".

**Tests:** none; existing `TestIntegration_GeneratedKeys*` stay green.

**Done when:** docs consistent with behavior; suite green.

---

## Task 8 — Tighten integration assertions (finding 9)

**Goal:** the integration suite fails when the LOB and interval behaviors of
Tasks 1–3 regress.

**Context:** `TestIntegration_ComplexTypeDecoding` asserts only
`strings.Contains(intervalStr, "1")` (passes with broken formats);
`TestIntegration_FetchOnDemandLOB` covers only single-column/single-row;
ARRAY NULL-element rendering is unpinned.

**Work (`integration_test.go`):**
- `TestIntegration_ComplexTypeDecoding`: replace weak INTERVAL assertions with
  golden strings from the Task 2 matrix (positive/negative, fractional,
  zero-padding); assert ENUM ordinal exact value; assert ROW text exact;
  assert ARRAY output exact for a case including `NULL` — decision made HERE:
  lock `"[a,b,c,<nil>]"` as the pinned rendering (Task 9 documents it in the
  README, but the behavior contract is owned by this task's assertion).
- `TestIntegration_FetchOnDemandLOB`: keep the shapes added in Task 1 and add
  an assertion that a second query on the same `*sql.DB` after a large-LOB
  read still returns correct data (pool sanity after the fix).
- `TestIntegration_GeneratedKeysMultiColumn`: add the external-package
  `GeneratedKeysProvider` assertion from Task 3 (move/duplicate as needed so
  both internal and external views are covered).

**Done when:** full integration suite green with the tightened assertions; the
Round II failure shapes (see audit) are each covered by a named test.

---

## Task 9 — P3 sweep (findings 10–15, 18; gofmt; dead code)

**Goal:** hygiene and documentation closes for the remaining P3 items.

**Work, item by item:**
- **Finding 11** (`generated_keys.go`): replace `isLastInsertIDUnavailable`
  string matching with `errors.Is(err, ErrLastInsertIDUnavailable)`; ensure
  every construction site wraps with `%w` (already the case via
  `lastInsertIDUnavailableError`); delete the helper or keep it as a thin
  `errors.Is` wrapper.
- **Finding 12**: remove dead code — `Session.discardGeneratedKeyRows` and
  `Session.readGeneratedKeysLastInsertID` (verified zero callers; grep again
  before deleting in case Tasks 1–5 added one). The gofmt drift in
  `protocol.go` / `value_read.go` / `value_read_test.go` and the staticcheck
  findings were already fixed in commit `7a8c6f7` — this task only adds the
  guard rail: a `fmt` / `fmt-check` target in `Makefile` that fails on
  `gofmt -l` output, and a Makefile comment noting that `make lint` requires
  `golangci-lint` (silently skips when absent).
- **Finding 10** (README): document ARRAY rendering exactly — elements
  comma-joined with `%v`; NULL elements render as `<nil>`; document ROW
  similarly. (Task 8 pins it with an assertion.)
- **Finding 13** (README): JSON note — bytes are exactly what H2 serializes
  (H2's own rendering includes outer quoting, verified); one line.
- **Finding 14** (README): ENUM parameters are sent as VARCHAR and coerced by
  the server; one line.
- **Finding 15** (`value_read.go`): inline-CLOB lone-surrogate handling —
  add a comment explaining why U+FFFD substitution is used (Go strings cannot
  carry lone surrogates; H2 Java strings can) and an optional debug-level log;
  behavior unchanged.
- **Finding 18** (`handshake.go`): validate `localTimeZoneID()` result with
  `time.LoadLocation`; fall back to `"UTC"` when unparseable (log at debug);
  add unit test with `TZ` set to garbage / valid / empty.

**Done when:** `gofmt -l` clean; no dead exported-on-unexported symbols added;
unit suite green; docs updated.

---

## Task 10 — Final validation, changelog, acceptance docs (Round II closure)

**Goal:** post-fix state fully recorded and verified.

**Work:**
- Update `CHANGELOG.md` Unreleased section: add the Round II fix bullets
  (batch-boundary LOB resolution, canonical interval formatting, public
  generated-keys accessor, wire caps, session discard on decode errors, DSN
  parameter documentation, tightened integration matrix, P3 sweep).
- Update `docs/ACCEPTANCE.md`: adjust the §10.3 rows naming the new/changed
  tests (`TestIntegration_FetchOnDemandLOB` shapes, `TestIntegration_ComplexTypeDecoding`
  golden strings, generated-keys external access test); add a note about the
  documented limitations added in Tasks 6/7/9.
- Mark `MATURITY_ROUND_II.md` findings resolved inline (status line in each
  finding, mirroring the Round I convention), incl. the retraction already
  recorded for finding 2 and the addendum marker for finding 19 (already
  fixed in commit `7a8c6f7`).
- Final verification matrix (all green against a freshly seeded local H2):

```
go build ./...
go vet ./...
gofmt -l .            # empty
golangci-lint run ./...   # when installed; otherwise note
go test ./...
go test -race ./...
go test -tags integration ./...
go test -tags integration -race ./...
CGO_ENABLED=0 go build ./...
```

**Done when:** all commands green; CHANGELOG/ACCEPTANCE in sync; no open
findings in `MATURITY_ROUND_II.md` without a resolution marker.

---

## Out of scope for this plan (stays in the post-MVP backlog)

- Streaming LOB access API (an `io.Reader`-style incremental reader); the
  driver continues to return full `string`/`[]byte` values (documented in
  PRD §8.4).
- Per-statement generated-keys mode override on `database/sql` statements
  (config-level mode stays; documented in Task 7).
- Fetch-on-demand LOBs nested inside ARRAY/ROW (Task 1 returns a documented
  `ErrUnsupportedType`).
- TLS, multiple result sets, benchmarks (unchanged PRD post-MVP backlog).

## Traceability

| Task | MATURITY_ROUND_II.md finding(s) |
|---|---|
| 1 | 1 (LOB interleaving), 6 (session discard — partial), 16 (BLOB field naming) |
| 2 | 3 (interval formatting), 2 (retraction bookkeeping) |
| 3 | 4 (GeneratedKeys public access) |
| 4 | 5 (wire-length caps), 17 (array skip-path error) |
| 5 | 6 (deterministic session discard) |
| 6 | 7 (ignored DSN params) |
| 7 | 8 (generated-keys config scope) |
| 8 | 9 (integration assertion tightening) |
| 9 | 10, 11, 12, 13, 14, 15, 18 (P3 sweep incl. gofmt/dead code) |
| 10 | — (validation, changelog, acceptance, resolution markers) |