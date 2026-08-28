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

## Running H2 in server mode

The driver speaks H2's native **TCP protocol**, so the database must be
running as a TCP server (`org.h2.tools.Server`); embedded/in-memory
connections are not supported. To start one:

```bash
# Download the H2 jar, or use the distribution zip from h2database.com:
curl -fsSL -o h2-2.4.240.jar \
  https://repo1.maven.org/maven2/com/h2database/h2/2.4.240/h2-2.4.240.jar

# Start a TCP server on the default port (9092):
java -cp h2-2.4.240.jar org.h2.tools.Server \
  -tcp -tcpPort 9092 \
  -tcpAllowOthers \
  -ifNotExists \
  -baseDir "$PWD/h2data"
```

The server prints a line like `TCP server running at tcp://...:9092` when
ready. Important flags:

- `-tcp` enables the native TCP server (default port `9092`).
- `-tcpAllowOthers` accepts connections from other hosts; omit it to serve
  localhost only.
- `-ifNotExists` auto-creates the database on first connect. Without it,
  connecting to a nonexistent database fails with H2 error `90149`
  ("Database ... not found") — see `FORBID_CREATION` in the DSN parameters
  section for the client-side counterpart.
- `-baseDir` is where database files are stored. Use an **absolute** path;
  a database name in the connection URL resolves relative to it
  (`mydb` → `<baseDir>/mydb.mv.db`).

When `-ifNotExists` auto-creates a database, the user from the first
connection becomes its initial (and only) user, so connect with the account
you want to own the database:

```go
db, err := sql.Open("h2",
    "jdbc:h2:tcp://localhost:9092/mydb;USER=myuser;PASSWORD=secret")
```

or pass credentials programmatically with `ParseDSN` + `MergeCredentials`
(see Quick start), or via the native URL userinfo (`h2://user:pass@host:9092/...`).
Switching to a username that does not exist yet, or a wrong password, fails
with H2 error `28000` ("Wrong user name or password") until the first
connection created the database.

For TLS, start the server with `-tcpSSL` and use an `ssl://` DSN — see the
TLS section below. The repository's own `h2-data/h2.sh` starts this exact
plain server for local testing.

## Local H2 test environment

Local integration tests expect these environment variables:

- `JDBC_URL`
- `JDBC_USER`
- `JDBC_PASSWORD`

A convenient local setup is `h2-data/.env` in this repository. Start the
server with `cd h2-data && ./h2.sh` (adds a TLS instance via `make db-tls`).
Integration tests and examples skip cleanly when the variables are
unavailable.

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

JDBC-style semicolon parameters (`...;IFEXISTS=TRUE`) and native query
parameters (`...?IFEXISTS=TRUE`) follow H2's own client policy:

| Class | Settings | Behavior |
|---|---|---|
| Consumed | `USER`, `PASSWORD` | Extracted into `Config.User` / `Config.Password` |
| **Forwarded to the server** | `IFEXISTS`, `ACCESS_MODE_DATA` (`r`/`rw`/`rws`), `INIT`, `MODE`, `LOCK_TIMEOUT`, `FORBID_CREATION` | Sent in the handshake property map; the **server enforces them** when opening the database — exactly like H2 JDBC |
| **Applied by the driver** | `QUERY_TIMEOUT` (ms) | Issued as `SET QUERY_TIMEOUT` on the session after connect (like H2 JDBC); over-long statements are canceled server-side with error 57014 while the session stays usable. Session-global for the pooled connection |
| Accepted, no effect | `AUTO_SERVER`, `AUTO_RECONNECT`, `OPEN_NEW`, `DB_CLOSE_DELAY`, `DB_CLOSE_ON_EXIT`, `FILE_LOCK`, `CIPHER`, `RECOVER`, `PAGE_SIZE`, `NETWORK_TIMEOUT`, `STATEMENT_CACHE_SIZE`, `TRACE_LEVEL_*`, `NON_KEYWORDS`, `JMX`, `OLD_INFORMATION_SCHEMA` | Embedded/JDBC-client-only settings; kept for URL compatibility, ignored by this pure-TCP driver |
| Unknown | anything else | **Rejected at parse time** unless the DSN also carries `IGNORE_UNKNOWN_SETTINGS=TRUE` (H2 semantics) |

Notes:

- Duplicate settings that differ only in case must carry identical values,
  otherwise parsing fails (H2's `DUPLICATE_PROPERTY` behavior).
- A setting without `=` (e.g. `;IFEXISTS`) is rejected (H2's URL format
  error 90046); write `IFEXISTS=TRUE` explicitly.
- A backslash escapes the next character in a value, so embed a literal
  semicolon as `\;` exactly like H2 JDBC (`arraySplit` semantics). Use it for
  multi-statement `INIT`, e.g.
  `jdbc:h2:tcp://localhost:9092/db;INIT=CREATE TABLE IF NOT EXISTS t(v INT)\;INSERT INTO t VALUES(1)`.
- `INIT=...` runs server-side SQL right after connect — treat it like any
  credential-bearing setting.
- With `Config.Logger` at debug level each connection logs which keys were
  accepted-but-ignored; keys only, never values.

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

## Benchmarks

Microbenchmarks for the wire codec and parsing live next to the code; run
them with `make bench` (server-free) or `make bench-integration`
(full round trips against a running H2, see `h2-data/h2.sh`). Example
baseline on an Intel i3-10110U laptop, Go 1.27:

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| ParseDSN | 856 | 768 | 5 |
| Tr.WriteString (ASCII) | 122 | 96 | 1 |
| Tr.ReadString (ASCII) | ~300 | 148–228 | 4 |
| ValueWriteInt64 / ReadInt64 | 55 / 67 | 24 | 2 / 3 |
| Integration SELECT 1 (ad-hoc) | ~148 µs | 1496 | 59 |
| Integration SELECT 1 (prepared) | ~108 µs | 1312 | 52 |
| Integration 100-row scan | ~216 µs | 13243 | 469 |

The UTF-16 string codec has ASCII fast paths (single allocation per write,
in-place compaction on read); non-ASCII text takes the general surrogate-
aware path. Numbers vary run to run — treat them as order-of-magnitude
baselines when profiling changes.

## Error handling and reconnection

- All SQL errors from the server decode into `*h2go.Error` (alias
  `*h2go.H2Error`) with `SQLState`, `Code`, `Message`, and the failing SQL;
  use `errors.As` to inspect. The stream stays usable after fully parsed
  server errors.
- A broken transport (mid-frame EOF, timeout) marks the pooled session dead;
  `database/sql` then discards it via `driver.ErrBadConn` and retries on a
  fresh connection automatically.
- Context cancellation during a running statement is forwarded to the server
  (side-channel cancel); the caller sees `context.DeadlineExceeded` /
  `context.Canceled` and the connection survives. See "Context cancellation"
  above.
- For a statement timeout independent of Go contexts, set `QUERY_TIMEOUT=<ms>`
  in the DSN: H2 itself cancels over-long statements (error 57014) without
  tearing down the session.
- Fault injection (`fault_test.go`) and a bounded soak test (`make soak`,
  `H2GO_SOAK_SECONDS` for longer runs) exercise server kills, mid-stream
  disconnects, restart recovery and pool churn with leak guards.

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

## Development

- `make build`, `make vet`, `make lint` (needs `golangci-lint`), `make test`,
  `make test-race`, and `make test-integration` (needs the local H2 server,
  see above).
- `make fuzz` runs the wire-decoder fuzz targets (`FuzzReadValue`,
  `FuzzReadTypeInfo`, `FuzzReadResultMeta`, `FuzzReadPrimitives`) for 60s
  each. They treat arbitrary bytes as a hostile server stream and fail on any
  panic, hang, or unbounded allocation in the protocol codec. Seed corpora
  also run as ordinary tests during `go test ./...`.

## Requirements

- Go 1.22 or later (module `github.com/rom35-cz/h2go`, package `h2go`, no
  CGO — pure Go, `CGO_ENABLED=0` supported).
- H2 Database **2.4.240 or later** running as a TCP server (`org.h2.tools.Server`),
  speaking native **protocol version 21**. Embedded/In-Memory JVM modes are
  out of scope by design.
- The only runtime dependency is `github.com/google/uuid` (connection IDs);
  the dependency tree stays tiny on purpose.

## License

[MIT](LICENSE) © rom35-cz
