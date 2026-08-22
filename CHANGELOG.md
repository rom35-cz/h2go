# Changelog

All notable changes to `h2go` will be documented in this file.

## [Unreleased] (v0.2.0 preview)

Post-MVP maturity fixes from the `MATURITY_MVP.md` review.

### Fixed

- Inline `CLOB` values are now decoded to `string` (previously returned
  `ErrUnsupportedType`), matching the documented supported-types matrix.
  Fetch-on-demand LOBs still return `ErrUnsupportedType`; LOB streaming is
  post-MVP.
- `sql.ColumnType.DecimalSize()` now reports "unknown" for scale-less
  `NUMERIC(a)` instead of falsely claiming scale 0.
- `HandshakeContext` now returns the wrapped context error (e.g.
  `context.DeadlineExceeded`) when the deadline fires mid-handshake, instead
  of the raw socket timeout.
- Broken sessions detected during `ResetSession` are aborted so the pool
  discards them (`driver.ErrBadConn`) instead of reusing a half-broken conn.
- `Session.Close()` bounds the final `STATUS_OK` read with a 2s deadline so a
  dead or half-open peer cannot stall pool teardown.

### Added

- `Config.MaxRows` (forwarded as protocol `maxRows`; 0 = unlimited) and
  `Config.FetchSize` (rows per fetch batch; 0 = 100), mirroring JDBC
  `setMaxRows` / `setFetchSize`.
- `TestIntegration_TypeShowcaseFullSelect`: full supported-type matrix
  against the seeded `type_showcase` table.
- `TestIntegration_MaxRows`: server-side row capping and fetch-size batching.
- Handshake-cancellation and dead-session hardening unit tests.

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
