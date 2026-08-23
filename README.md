# h2go — pure Go H2 Database driver for database/sql

[![CI](https://github.com/rom35-cz/h2go/actions/workflows/ci.yml/badge.svg)](https://github.com/rom35-cz/h2go/actions/workflows/ci.yml)

**Status:** Under development. Not yet ready for production use.

`h2go` is a pure Go [`database/sql`](https://pkg.go.dev/database/sql) driver for [H2 Database](https://www.h2database.com/) running in **native TCP server mode**. It speaks H2 **protocol 21** directly — no PostgreSQL compatibility mode, no JDBC bridge, no JVM embedding, and no CGO.

## Install

```bash
go get github.com/rom35-cz/h2go
```

## Quick start

Register the driver with a blank import and use a H2 JDBC-style DSN:

```go
import (
    "database/sql"
    _ "github.com/rom35-cz/h2go"
)

func main() {
    db, err := sql.Open("h2", "jdbc:h2:tcp://localhost:9092/mydb")
    if err != nil {
        panic(err)
    }
    defer db.Close()
}
```

For explicit configuration and separate credential injection, use `ParseDSN`, `MergeCredentials`, and `OpenDB`:

```go
cfg, err := h2go.ParseDSN(os.Getenv("JDBC_URL"))
if err != nil {
    panic(err)
}
h2go.MergeCredentials(cfg, os.Getenv("JDBC_USER"), os.Getenv("JDBC_PASSWORD"))
db, err := h2go.OpenDB(cfg)
if err != nil {
    panic(err)
}
```

## Local H2 test environment

Local integration tests expect these environment variables:

- `JDBC_URL`
- `JDBC_USER`
- `JDBC_PASSWORD`

A convenient local setup is `h2-data/.env` in this repository. Integration tests and examples skip cleanly when the variables are unavailable.

## Diagnostic logging

Logging is **off by default**. To enable diagnostics, set `Config.Logger` explicitly:

```go
cfg.Logger = h2go.NewTextLogger(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
```

Notes:
- output is text-based via `slog.NewTextHandler`
- passwords and password hashes are redacted
- no logging occurs unless you provide a logger

## Result options

Two optional `Config` fields tune result-set behavior (both default to current driver behavior when zero):

- `MaxRows` (`int64`): forwarded as the protocol `maxRows`, asking the H2 server to cap each result set. `0` means unlimited, matching the H2 server semantics. This mirrors JDBC `Statement.setMaxRows`.
- `FetchSize` (`int`): how many rows the driver requests per `RESULT_FETCH_ROWS` batch while streaming. `0` uses the driver default of `100`.

```go
cfg, _ := h2go.ParseDSN("jdbc:h2:tcp://localhost:9092/mydb")
cfg.MaxRows = 1000 // server returns at most 1000 rows per query
cfg.FetchSize = 50 // prefetch 50 rows per batch
db, err := h2go.OpenDB(cfg)
```

## Generated keys

By default (`GeneratedKeysAuto`) the driver asks H2 for the auto-detected
generated key of every update statement, so `res.LastInsertId()` works like
with most drivers.

The `Config.GeneratedKeysMode` family is **connection-level**: the mode (and
its columns/names) applies to every update statement on every connection
created from that `Config` — unlike JDBC, where the request is made per
statement. To mix modes (e.g. one handle requesting keys and another not),
create separate `*sql.DB` handles from separate configs — or attach a
**per-statement override** to the statement's context:

```go
// Request keys for this statement only (wins over Config for this call):
ctx := h2go.ContextWithGeneratedKeys(ctx, h2go.GeneratedKeysRequest{
    Mode:  h2go.GeneratedKeysColumnNames,
    Names: []string{"ID"},
})
res, err := db.ExecContext(ctx, "INSERT INTO t(name) VALUES (?)", name)

// Or suppress keys for a single statement (GeneratedKeysNone == 0 cannot be
// expressed by omission):
res, err = db.ExecContext(h2go.ContextWithoutGeneratedKeys(ctx),
    "INSERT INTO t(name) VALUES (?)", name)
```

Overrides win over `Config`; an unknown override mode falls back to the
connection configuration.

Turning keys off requires the escape hatch below — `GeneratedKeysNone` is the
zero value, so on its own it is indistinguishable from "default" (auto):

```go
cfg.GeneratedKeysMode = h2go.GeneratedKeysNone
cfg.GeneratedKeysModeSet = true // required, otherwise keys stay on (auto)
db, err := h2go.OpenDB(cfg)
```

For the full multi-column / multi-row key result, see the
`h2go.GeneratedKeysProvider` notes under [Limitations](#limitations).

## DSN formats

| Format | Example | Notes |
|---|---|---|
| JDBC-style | `jdbc:h2:tcp://localhost:9092/mydb` | Recommended for compatibility with existing H2 URLs |
| Native | `h2://user:pass@localhost:9092/mydb` | Userinfo is supported |
| Native (explicit TCP) | `h2+tcp://user:pass@localhost:9092/mydb` | Same wire protocol, explicit scheme |
| JDBC-style TLS | `jdbc:h2:ssl://localhost:9093/mydb` | TLS transport, matching H2's `ssl://` scheme |
| Native TLS | `h2+ssl://user:pass@localhost:9093/mydb` | TLS transport, native form |

Default TCP port: `9092`.

## TLS

Servers started with the `-tcpSSL` flag speak TLS on their TCP port. The
driver enables TLS automatically for `ssl://` DSNs (mirroring H2's own
client) or programmatically via `Config.TLS`. Verification follows
crypto/tls defaults and can be tuned per connection:

```go
cfg, _ := h2go.ParseDSN("jdbc:h2:ssl://localhost:9093/mydb")

// Trust a private/self-signed CA instead of system roots:
pool := x509.NewCertPool()
pool.AppendCertsFromPEM(pemBytes)
cfg.TLSRootCAs = pool

// Optional overrides:
//   cfg.TLSServerName        — verify against this name instead of Host
//   cfg.TLSInsecureSkipVerify — disable verification (development only!)
```

The statement-cancel side channel uses the same TLS settings. Local test
setup: `make db-tls` starts an H2 server with `-tcpSSL` on port 9093 using
a generated self-signed certificate (`h2-data/tls/`, git-ignored).

## DSN parameters

The parser accepts JDBC-style semicolon parameters
(`jdbc:h2:tcp://localhost:9092/mydb;IFEXISTS=TRUE`) and native URL query
parameters (`h2://localhost:9092/mydb?IFEXISTS=TRUE`). Of these:

- `USER` and `PASSWORD` are consumed — extracted into `Config.User` /
  `Config.Password` (never stored in `Params`).
- everything else lands in `Config.Params` and is currently **parsed but not
  applied**: the driver neither forwards these parameters to the server nor
  enforces them locally.

Risky examples of silently ignored parameters:

- `IFEXISTS=TRUE` — the connection still succeeds when the database does not
  exist (the server auto-creates it) because the flag is never enforced.
- `ACCESS_MODE_DATA=r` — read-only access mode is not requested.
- `AUTO_SERVER=TRUE`, file-lock settings, `TRACE_LEVEL_*`, ... — ignored.

If your deployment depends on any of these, validate them manually before
connecting (for example, check that the database file exists yourself). When
`Config.Logger` is configured at debug level, each new connection logs one
record naming which parameter keys were parsed but not applied — keys only;
parameter values are never logged.

## Supported types

The driver focuses on the MVP scalar set used by the test suite:

| H2 type | Go scan / value form | Notes |
|---|---|---|
| `BOOLEAN` | `bool` | Direct round-trip |
| Integer widths (`TINYINT`, `SMALLINT`, `INTEGER`, `BIGINT`) | `int64` | Scanned as integers |
| `REAL`, `DOUBLE` | `float64` | IEEE-754 values |
| `NUMERIC` / `DECIMAL` | `string` | Exact decimal text to avoid precision loss |
| `DECFLOAT` | `string` | Exact decimal text as H2 renders it (`BigDecimal.toString` semantics: scientific notation outside the plain window; special values `Infinity`, `-Infinity`, `NaN`). Scanned strings are validated against the wire grammar. Scan into `h2go.DecFloat` for an exact unscaled×10⁻ⁿ representation with `Scanner`/`Valuer` support |
| `UUID` | `string` | Canonical 36-character textual form |
| `DATE`, `TIME`, `TIMESTAMP`, `TIMESTAMP WITH TIME ZONE` | `time.Time` | Includes fractional-second precision |
| `CHAR`, `VARCHAR` | `string` | Text values |
| `CLOB` | `string` | Inline CLOBs (≤ `MAX_LENGTH_INPLACE_LOB`); larger CLOBs fetched on demand via `LOB_READ` |
| `BINARY`, `VARBINARY`, `BLOB` | `[]byte` | Raw bytes; inline BLOBs only, larger BLOBs fetched on demand via `LOB_READ` |
| `JSON`, `GEOMETRY`, `JAVA_OBJECT` | `[]byte` | Raw bytes exactly as H2 serializes them (H2's own text rendering of JSON includes outer double quotes) |
| `ENUM` | `int64` | Ordinal value |
| `INTERVAL` | `string` | Decoded as H2's canonical interval text |
| `ARRAY`, `ROW` | `string` | Elements/fields rendered with `%v`, comma-joined inside `[...]` / `(...)`; NULL elements render as `<nil>` |
| `NULL` | `nil` | Preserved as database null |

ENUM *parameters* are sent as VARCHAR and coerced by the server on arrival.

Unsupported H2 types return a clear error or a documented fallback value.

## Context cancellation

Context deadlines/cancellation are honored deeply: when a query or update is
interrupted, the driver fires H2's side-channel `SESSION_CANCEL_STATEMENT`
request so the **server** stops the running statement. The driver then waits
(up to an internal grace period) for the server's aligned "statement was
canceled" report on the main connection; the caller observes
`context.DeadlineExceeded` / `context.Canceled` while the session stays
usable — no connection teardown, unlike naive timeout handling. If the
server cannot be reached for the cancel, the operation falls back to the
Round II deterministic-discard behavior (session aborted, error reported).

Operations without server-side statement identity (prepare, handshake) still
respect contexts but cannot be interrupted mid-execution on the server.

## Limitations

Current scope intentionally excludes:

- PostgreSQL compatibility mode
- JDBC bridge / embedded JVM integration
- multiple result sets
- extended generated-key APIs beyond the `LastInsertId()` path: the full
  multi-column / multi-row keys are reachable only at the driver level,
  because `database/sql` wraps results (a plain `sql.Result` cannot be
  asserted):

  ```go
  sqlConn, _ := db.Conn(ctx)
  defer sqlConn.Close()
  var keys *h2go.GeneratedKeysResult
  _ = sqlConn.Raw(func(raw any) error {
      c := raw.(driver.Conn) // *h2go.conn, implements driver.ExecerContext
      res, err := c.(driver.ExecerContext).ExecContext(ctx,
          "INSERT INTO t(x) VALUES (1)", nil)
      if err != nil {
          return err
      }
      if gkp, ok := res.(h2go.GeneratedKeysProvider); ok {
          keys = gkp.GetGeneratedKeys()
      }
      return nil
  })
  ```

These are tracked as post-MVP enhancements in the implementation plan.

## Examples and docs

See `doc.go` for a short package quick start and `example_test.go` for runnable examples covering:

- open / ping
- query
- exec
- transactions
- prepared statements

Examples run against the local H2 environment (see above) and create throwaway
tables named `example_*_<nanotime>`, so you may see those tables appear in the
seed database while examples run.

Notes on column metadata:

- `Rows.Columns()` returns H2 **column labels** (the wire `alias` field), which
  match JDBC `getColumnLabel`: identical to the column name for plain columns,
  but the label for expression columns (`SELECT col AS x`, `SELECT 1+1`).
- `ResultMeta.GetColumnByName` matches those labels only; looking up an
  expression column by its underlying column name will not match.

## Repository

[github.com/rom35-cz/h2go](https://github.com/rom35-cz/h2go)
