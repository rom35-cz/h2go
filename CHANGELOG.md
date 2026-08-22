# Changelog

All notable changes to `h2go` will be documented in this file.

## [Unreleased] (v0.2.0 preview)

Post-MVP maturity fixes from the `MATURITY_MVP.md` review.

### Fixed

- Inline `CLOB` values are now decoded to `string` (previously returned
  `ErrUnsupportedType`), matching the documented supported-types matrix.
  Fetch-on-demand LOBs are fetched via the `LOB_READ` protocol.
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
  streaming for BLOB and CLOB values exceeding the inline threshold.
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

### Removed

- `ErrNotYetSupported` (unused since prepare/transactions landed).

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
