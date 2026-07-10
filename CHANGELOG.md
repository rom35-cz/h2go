# Changelog

All notable changes to `h2go` will be documented in this file.

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
