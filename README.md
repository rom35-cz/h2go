# h2go — pure Go H2 Database driver for database/sql

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

## DSN formats

| Format | Example | Notes |
|---|---|---|
| JDBC-style | `jdbc:h2:tcp://localhost:9092/mydb` | Recommended for compatibility with existing H2 URLs |
| Native | `h2://user:pass@localhost:9092/mydb` | Userinfo is supported |
| Native (explicit TCP) | `h2+tcp://user:pass@localhost:9092/mydb` | Same wire protocol, explicit scheme |

Default TCP port: `9092`.

## Supported types

The driver focuses on the MVP scalar set used by the test suite:

| H2 type | Go scan / value form | Notes |
|---|---|---|
| `BOOLEAN` | `bool` | Direct round-trip |
| Integer widths (`TINYINT`, `SMALLINT`, `INTEGER`, `BIGINT`) | `int64` | Scanned as integers |
| `REAL`, `DOUBLE` | `float64` | IEEE-754 values |
| `NUMERIC` / `DECIMAL` | `string` | Exact decimal text to avoid precision loss |
| `UUID` | `string` | Canonical 36-character textual form |
| `DATE`, `TIME`, `TIMESTAMP`, `TIMESTAMP WITH TIME ZONE` | `time.Time` | Includes fractional-second precision |
| `CHAR`, `VARCHAR`, `CLOB` | `string` | Text values |
| `BINARY`, `VARBINARY`, `BLOB` | `[]byte` | Raw bytes |
| `NULL` | `nil` | Preserved as database null |

Unsupported H2 types return a clear error or a documented fallback value.

## Limitations

Current scope intentionally excludes:

- PostgreSQL compatibility mode
- JDBC bridge / embedded JVM integration
- TLS/SSL transport
- full LOB streaming
- multiple result sets
- exact-decimal helper APIs beyond string-based `NUMERIC`
- extended generated-key APIs beyond the MVP `LastInsertId()` path

These are tracked as post-MVP enhancements in the implementation plan.

## Examples and docs

See `doc.go` for a short package quick start and `example_test.go` for runnable examples covering:

- open / ping
- query
- exec
- transactions
- prepared statements

## Repository

[github.com/rom35-cz/h2go](https://github.com/rom35-cz/h2go)
