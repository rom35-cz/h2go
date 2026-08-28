# Changelog

All notable changes to `h2go` will be documented in this file.

## [v0.4.0] - 2026-08-28

### Changed

- **Breaking:** unknown DSN settings are now **rejected at parse time**
  instead of being silently ignored, mirroring H2 JDBC's
  `IGNORE_UNKNOWN_SETTINGS` semantics. URLs carrying unrecognized parameters
  fail with an error naming them; add `IGNORE_UNKNOWN_SETTINGS=TRUE` to keep
  accepting them.
- **Breaking:** DSN settings without `=` (bare keys such as `;IFEXISTS`) are
  rejected with H2's URL format error semantics (90046), matching the real
  JDBC client. Accepting them silently forwarded an empty value
  (`IFEXISTS` without a value parses as `FALSE`), defeating the setting.
- DSN parsing now honors H2's backslash escaping for setting values: `\;`
  inside a value is a literal semicolon (`StringUtils.arraySplit` parity),
  so multi-statement `INIT=...\;...` URLs work as they do with JDBC. Empty
  settings segments (e.g. a trailing `;`) are skipped like H2 does.

### Added

- Server-side enforcement of forwarded DSN settings: `IFEXISTS`,
  `ACCESS_MODE_DATA` (`r`/`rw`/`rws`), `INIT`, `MODE`, `LOCK_TIMEOUT` and
  `FORBID_CREATION` now travel to the server in the handshake property map
  (the same channel H2's own JDBC client uses), so read-only sessions,
  missing-database failures and compatibility modes behave as JDBC users
  expect.
- Live integration coverage for the forwarded settings: `FORBID_CREATION`
  on missing databases (90149), `MODE=Oracle` concatenation semantics,
  multi-statement `INIT` execution, and `LOCK_TIMEOUT` row-contention
  timeouts (50200), plus the 90046 format-error rejection.
- Case-insensitive duplicate DSN settings with conflicting values are a parse
  error (`DUPLICATE_PROPERTY` semantics); identical repeats collapse.
- CI runs the live-H2 integration matrix against both H2 2.4.240 and the
  latest Maven release, substantiating the "2.4.240 and later" claim.
- Query-timeout parity: the `QUERY_TIMEOUT` DSN setting (milliseconds) is
  applied as `SET QUERY_TIMEOUT` on the session after connect, like H2's own
  JDBC client. Over-long statements are canceled server-side (57014) while
  the session remains usable.
- Fault-injection suite against real spawned H2 server processes: kill -9
  mid-query, kill during result streaming, graceful restart recovery with
  persistence checks (`fault_test.go`).
- Bounded soak test with leak guards on goroutines, heap and file
  descriptors across mixed pool churn including deep-canceled queries
  (`soak_test.go`; `make soak`, duration via `H2GO_SOAK_SECONDS`). CI runs a
  25-second soak on every push.
- README production notes: error handling and reconnection semantics.

## [v0.3.1] - 2026-08-23

### Changed

- Repository published as **public** (was private during the v0.3.0 window).
  No code changes — retagged so fresh module proxy/checksum lookups resolve
  cleanly for everyone.

## [v0.3.0] - 2026-08-23

### Added

- MIT LICENSE, CONTRIBUTING.md and SECURITY.md — the module is now
  packaged for public consumption as a `database/sql` driver.
- Benchmarks: server-free microbenchmarks for the wire codec, DSN parsing,
  value encoding, INTERVAL formatting and DECFLOAT parsing
  (`make bench`), plus live round-trip benchmarks against a running H2
  (`make bench-integration`). First tuning pass from the baselines: the
  UTF-16 string codec gained ASCII fast paths — string writes drop from 3
  to 1 allocation on both the ASCII and general paths (the general path now
  encodes and serializes in a single fused pass), ASCII reads compact in
  place and skip the UTF-16 decoder.
- Per-statement generated-keys overrides: `ContextWithGeneratedKeys` /
  `ContextWithoutGeneratedKeys` attach a `GeneratedKeysRequest` to an
  `ExecContext` context; that request wins over the connection-level
  `Config.GeneratedKeysMode` family for exactly one statement (unknown
  override modes fall back to the configuration). This removes the previous
  limitation of mixing generated-key behavior only via separate `*sql.DB`
  handles.
- Deep statement cancellation: context cancellation during a running query
  or update now fires H2's side-channel `SESSION_CANCEL_STATEMENT` and waits
  for the server's aligned "statement was canceled" report (vendor code
  57014 / SQLState HY008). The caller gets `context.DeadlineExceeded` /
  `context.Canceled` while the session survives — previously any mid-query
  cancellation aborted the connection. If the cancel cannot reach the
  server, the deterministic-discard abort behavior applies as before.
- TLS transport support: `ssl://` DSNs (`jdbc:h2:ssl://...`, `h2+ssl://...`)
  and programmatic `Config.TLS` now wrap the connection in crypto/tls after
  dialing, targeting servers started with H2's `-tcpSSL` flag. Verification
  is configurable via `Config.TLSRootCAs`, `Config.TLSServerName`, and
  `Config.TLSInsecureSkipVerify`; the statement-cancel side channel honors
  the same settings. Local test tooling: `make db-tls` (self-signed cert +
  TLS server on port 9093), integration test `TestIntegration_TLSTransport`
  (skips without `JDBC_TLS_URL`).

- Exact DECFLOAT support: `h2go.DecFloat` — an exact unscaled×10⁻ⁿ decimal
  type mirroring java.math.BigDecimal plus the DECFLOAT specials
  (`Infinity`, `-Infinity`, `NaN`) — with `ParseDecFloat`, `Scanner`/`Valuer`
  integration and byte-exact rendering of H2's textual form. DECFLOAT wire
  strings are now validated eagerly on read (mirroring the reference
  client's `new BigDecimal(s)`), so broken frames fail at decode time.
  Documented H2's assignment-time normalization: trailing zeros stripped,
  zero collapses to plain "0".

### Fixed

- Fail-fast caps on the last two wire-supplied allocation counts: result-set
  metadata column count (`ReadResultMeta`) and prepared-statement parameter
  count now reject negative values as broken frames and refuse counts above
  1,048,576 before pre-allocating, instead of attempting the allocation from
  a hostile or corrupted frame.

## [v0.2.0] - 2026-08-22

Post-MVP maturity fixes from the `MATURITY_MVP.md` and `MATURITY_ROUND_II.md`
reviews.

### Fixed

- Inline `CLOB` values are now decoded to `string` (previously returned
  `ErrUnsupportedType`), matching the documented supported-types matrix.
  Fetch-on-demand LOBs are fetched via the `LOB_READ` protocol.
- Fetch-on-demand LOBs resolve at batch boundaries: a deferred LOB anywhere
  in a result batch (extra columns, extra rows, across fetch-size batches,
  inside generated-keys frames) resolves correctly instead of desynchronizing
  the stream (`unexpected status 15` / `16777216` failure shapes).
- `formatInterval` renders H2's canonical INTERVAL text exactly (sign on the
  leading field only, two-digit sub-fields, trimmed fractions), verified
  against `CAST(... AS VARCHAR)` on live H2.
- Mid-stream decode errors (truncated frames, timeouts during row streaming)
  deterministically mark the session dead so database/sql discards the
  connection instead of reusing one whose stream position is unknown.
- Context deadlines during command preparation/streaming now report
  `context.DeadlineExceeded` deterministically even when the transport timeout
  surfaces microseconds before the context timer fires.
- Wire-controlled counts and lengths are capped before allocation: ARRAY/ROW
  element counts, generated-keys row counts, inline BLOB lengths, CLOB char
  lengths (derived from `MaxWireLength`), and LOB chunk sizes; the legacy ARRAY
  type-name skip error is returned instead of discarded.
- `sql.ColumnType.DecimalSize()` now reports "unknown" for scale-less
  `NUMERIC(a)` instead of falsely claiming scale 0.
- `HandshakeContext` now returns the wrapped context error (e.g.
  `context.DeadlineExceeded`) when the deadline fires mid-handshake, instead
  of the raw socket timeout.
- Broken sessions detected during `ResetSession` are aborted so the pool
  discards them (`driver.ErrBadConn`) instead of reusing a half-broken conn.
- `Session.Close()` bounds the final `STATUS_OK` read with a 2s deadline so a
  dead or half-open peer cannot stall pool teardown.
- `Rows.NextResultSet()` now returns `io.EOF` (standard Go convention) instead
  of an error string, so `database/sql` treats it as "no more result sets"
  rather than an unexpected failure.

### Added

- `Config.MaxRows` (forwarded as protocol `maxRows`; 0 = unlimited) and
  `Config.FetchSize` (rows per fetch batch; 0 = 100), mirroring JDBC
  `setMaxRows` / `setFetchSize`.
- `Config.GeneratedKeysMode` and `GeneratedKeysModeSet`: control whether
  generated keys are requested and in which mode (auto, none, column numbers,
  column names). `Config.GeneratedKeysColumns` and
  `Config.GeneratedKeysColumnNames` specify the target columns.
- `GeneratedKeysResult` type with `Rows` and `Columns` fields. Provides
  multi-column and multi-row generated key access beyond the single
  `LastInsertId()` path.
- `GeneratedKeysProvider` interface: assert the driver-level `driver.Result`
  obtained via `sql.Conn.Raw()` to reach `GetGeneratedKeys()` from outside the
  package. A `sql.Result` returned by `db.Exec` intentionally does not expose
  it (`database/sql` wraps driver results in its own type).
- `GeneratedKeysResult.SingleInt64()`: convenience method equivalent to
  `LastInsertId()`.
- `TestIntegration_TypeShowcaseFullSelect`: full supported-type matrix
  against the seeded `type_showcase` table.
- `TestIntegration_MaxRows`: server-side row capping and fetch-size batching.
- `TestIntegration_ComplexTypeDecoding`: end-to-end validation of ENUM,
  INTERVAL, ARRAY, and ROW value decoding.
- `TestIntegration_FetchOnDemandLOB`: end-to-end validation of LOB_READ
  streaming for BLOB and CLOB values exceeding the inline threshold, extended
  with multi-column/multi-row/mixed/batch-boundary shapes and pool sanity.
- `TestIntegration_IntervalCanonicalMatrix`: driver INTERVAL decoding compared
  against H2's own canonical text for 18 expressions.
- `TestIntegration_ComplexTypeDecoding`: exact golden assertions for ENUM
  ordinal, INTERVAL text, ARRAY rendering incl. NULL elements (`<nil>`), and
  ROW text.
- `TestIntegration_GeneratedKeysWithLob`: fetch-on-demand LOB inside a
  generated-keys frame.
- `TestIntegration_GeneratedKeysProviderExternal`: package-external proof of
  generated-keys reachability through `sql.Conn.Raw()`.
- `TestIntegration_GeneratedKeysMultiColumn`: multi-column generated keys
  via `GeneratedKeysColumnNumbers`.
- `TestIntegration_GeneratedKeysNoKeys`: `GeneratedKeysMode=none` disables
  key generation.
- Handshake-cancellation and dead-session hardening unit tests.

### Value type decoding

Previously unsupported types now decode to documented Go representations:

| H2 type | Go representation |
|---|---|
| `JSON` | `[]byte` |
| `GEOMETRY` | `[]byte` |
| `JAVA_OBJECT` | `[]byte` |
| `ENUM` | `int64` (ordinal) |
| `INTERVAL` | `string` (e.g. `INTERVAL '1-2' YEAR TO MONTH`) |
| `ARRAY` | `string` (JSON-like `[elem1,elem2]`) |
| `ROW` | `string` (parenthesized `(field1,field2)`) |

### Hardening

- Defensive wire caps: `ReadString`/`ReadBytes` and the inline CLOB decoder
  reject length fields above `MaxWireLength` (512 MiB) before allocating.
- `localTimeZoneID` validates its candidate with `time.LoadLocation` and falls
  back to UTC with a debug log instead of sending an unparseable `TZ` value to
  the server.

### Documentation

- New "DSN parameters" docs (README + godoc): only `USER`/`PASSWORD` are
  consumed; everything else is parsed into `Config.Params` but not applied,
  with the risky examples called out (`IFEXISTS`, `ACCESS_MODE_DATA`,
  `AUTO_SERVER`). Each connection logs the ignored keys at debug level.
- Generated-keys configuration documented as connection-level (JDBC is per-
  statement), including the separate-handles workaround and the
  `GeneratedKeysModeSet` escape hatch required to turn keys off.
- README supported-types table now documents exact ARRAY/ROW rendering
  (NULL elements as `<nil>`), that JSON/GEO/OBJECT bytes are exactly what H2
  serializes, and that ENUM parameters ride as VARCHAR.
- Inline-CLOB lone-surrogate handling documented (U+FFFD substitution; Go
  strings cannot carry lone surrogates).

### Removed

- `ErrNotYetSupported` (unused since prepare/transactions landed).
- Unused `Session.readGeneratedKeysLastInsertID` and
  `Session.discardGeneratedKeyRows`.

## [v0.1.0] - 2026-07-10

Initial MVP release of the pure Go H2 native TCP driver for `database/sql`.

### Highlights

- Driver registration under the name `h2`
- JDBC-style and native Go DSN parsing
- Native TCP protocol 21 handshake with H2 2.4.240+
- `database/sql` connectivity, `Ping`, query, exec, prepared statements, and transactions
- Connection pool safety and `SessionResetter` / `Validator` support
- Context-aware connect, prepare, query, exec, and transaction operations
- H2 SQL error decoding with SQLState and error codes
- Generated key support for single numeric `LastInsertId()` values
- Column metadata scan-type hints and precision/scale metadata
- Optional `log/slog` diagnostics with text output and sensitive-data redaction
- Local unit and integration tests against a real H2 TCP server

### Notes

- The driver is intentionally limited to H2 native TCP protocol 21 and H2 2.4.240+.
- PostgreSQL compatibility mode, JDBC bridge integration, and CGO are out of scope.
- Additional LOB, JSON, array, interval, and multi-result-set support remains post-MVP.
