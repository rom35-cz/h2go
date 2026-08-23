# Product Requirements Document: Pure Go H2 Native TCP Driver

Date: 2026-07-08  
Status: Draft  
Source document: `FEASIBILITY_STUDY.md`  
Repository: `github.com/rom35-cz/h2go` initially private, planned for later open source release  
Root package name: `h2go`

## 1. Product overview

This project will create a **pure Go database driver for H2 Database running in native TCP server mode**. The driver must integrate with Go's standard `database/sql` package and provide the required driver methods and practical modern optional interfaces expected by Go applications.

The driver will connect directly to H2's native TCP server protocol. It will **not** use H2 PostgreSQL mode, JDBC, a JVM bridge, CGO, or another database driver's protocol.

The initial supported H2 target is:

- H2 version: **2.4.240 and later only**
- Native TCP protocol: **21**
- Server mode: `jdbc:h2:tcp://...`

Backward compatibility with older H2 versions or older TCP protocol versions is explicitly out of scope.

## 2. Problem statement

Go does not currently have a mature, production-quality pure Go driver for H2 native TCP server mode. Existing options are experimental, incomplete, or rely on PostgreSQL compatibility mode / JDBC-style workarounds.

The project needs a native Go driver that can:

- Connect to an existing H2 server-mode database.
- Use the real H2 TCP protocol.
- Work with Go's `database/sql` package.
- Support common SQL operations, prepared statements, transactions, errors, metadata, and common H2 data types.
- Be implemented independently in Go while using H2 Java source only as a protocol reference/specification.

## 3. Goals

### 3.1 Primary goals

1. Create a clean Go module implementing a native H2 TCP driver.
2. Support H2 **2.4.240+** using native TCP protocol **21**.
3. Provide idiomatic integration with `database/sql`.
4. Support JDBC-style H2 TCP URLs from `.env`, such as:

   ```text
   jdbc:h2:tcp://localhost:9092/h2-go
   ```

5. Support native Go DSNs for applications that do not want JDBC-style URLs.
6. Implement query, exec, prepared statement, row scanning, transactions, connection pooling safety, and common scalar type handling.
7. Provide reliable integration tests against a real H2 server.
8. Keep protocol logic isolated from `database/sql` plumbing for maintainability.

### 3.2 Quality goals

- Pure Go implementation.
- No PostgreSQL mode.
- No JDBC bridge.
- No copied Java code.
- Clear errors for unsupported server versions, unsupported types, and protocol failures.
- Deterministic test setup using H2 `2.4.240`.

## 4. Non-goals

The following are explicitly out of scope for the initial product:

1. H2 PostgreSQL compatibility mode.
2. JDBC bridge or JVM embedding from Go.
3. Backward compatibility with H2 1.4.x, 2.1.x, or 2.2.x.
4. Supporting native TCP protocol versions below 21.
5. Embedded H2 database mode.
6. Reusing or translating H2 Java source code.
7. Full support for every H2-specific advanced type in the first release.
8. ORM-specific features beyond standard `database/sql` compatibility.
9. Distributed/clustered H2 support.
10. ODBC or non-Go driver support.

## 5. Target users

### 5.1 Primary users

- Go developers who need to connect to H2 in server mode.
- Internal project developers who need a native Go interface for an existing H2 database.
- Test/integration environments where H2 is already running as a TCP server.

### 5.2 Secondary users

- Go projects that currently use H2 through PostgreSQL compatibility mode and want native H2 semantics.
- Developers who need H2-specific error codes, types, and behaviour instead of PostgreSQL-compatible approximations.

## 6. Assumptions

1. The H2 database is already running in TCP server mode.
2. The database already exists or the H2 server allows database creation according to its own configuration.
3. Connection values can be loaded from `.env`.
4. The initial local test environment provides:

   ```text
   h2-data/.env
   h2-data/h2-2.4.240.jar
   h2-data/h2.sh
   ```

5. H2 Java source is available and can be used as protocol reference/specification.
6. The Go driver implementation will be original code.

## 7. Functional requirements

### 7.1 Driver registration

The driver must register with `database/sql` using a stable driver name.

Required driver registration name:

```go
h2
```

The root Go package should be named:

```go
h2go
```

The repository/module path should be:

```text
github.com/rom35-cz/h2go
```

The package name matches the last element of the module path, so it can be imported with no alias:

```go
import "github.com/rom35-cz/h2go"
```

The repository will start as a private GitHub project under `rom35-cz/h2go` and may be opened as an open source repository later.

Example usage:

```go
import (
    "database/sql"
    _ "github.com/rom35-cz/h2go"
)

func main() {
    db, err := sql.Open("h2", dsn)
    if err != nil {
        panic(err)
    }
    defer db.Close()
}
```

### 7.2 Connection string support

The driver must support JDBC-style H2 TCP URLs from existing environment configuration.

Required supported format:

```text
jdbc:h2:tcp://host:port/database
```

Example:

```text
jdbc:h2:tcp://localhost:9092/h2-go
```

The driver must also support credentials supplied separately:

```text
JDBC_URL=jdbc:h2:tcp://localhost:9092/h2-go
JDBC_USER=...
JDBC_PASSWORD=...
```

The driver should also support native Go DSN formats, for example:

```text
h2://user:password@localhost:9092/h2-go
h2+tcp://user:password@localhost:9092/h2-go
```

#### DSN parsing requirements

- Parse host.
- Parse port, defaulting to `9092` if omitted.
- Parse database name correctly for H2 server mode.
- Preserve user and password.
- Support H2 JDBC URL parameters where practical.
- Support Go query parameters where practical.
- Reject unsupported schemes with clear errors.
- Reject non-TCP H2 URLs in the initial release.

Important database-name rule:

- For `jdbc:h2:tcp://localhost:9092/h2-go`, the database name sent to H2 should be `h2-go`, not `/h2-go`.

### 7.3 Native TCP protocol handshake

The driver must implement the H2 native TCP handshake for protocol 21.

Requirements:

- Open a TCP connection to the H2 server.
- Send min/max protocol version as **21**.
- Send database name.
- Send original JDBC-style URL where appropriate.
- Send username.
- Send H2-compatible user password hash.
- Send file password hash when required; otherwise send `nil`/empty according to protocol requirements.
- Send connection properties.
- Read server status.
- Read negotiated protocol version.
- Fail clearly if protocol 21 is not negotiated.
- Decode server-side handshake errors into useful Go errors.

### 7.4 Required `database/sql/driver` interfaces

The driver must implement the required standard driver interfaces:

- `driver.Driver`
- `driver.Conn`
- `driver.Stmt`
- `driver.Tx`
- `driver.Rows`
- `driver.Result`

Required methods include:

- Open connection.
- Prepare statement.
- Close connection.
- Begin transaction.
- Execute statement.
- Query statement.
- Close statement.
- Read rows.
- Close rows.
- Commit transaction.
- Rollback transaction.
- Return rows affected.
- Return last insert ID or a documented unsupported error.

### 7.5 Modern `database/sql/driver` interfaces

The driver should implement the following modern/context-aware interfaces:

- `driver.DriverContext`
- `driver.Connector`
- `driver.ConnPrepareContext`
- `driver.QueryerContext`
- `driver.ExecerContext`
- `driver.StmtQueryContext`
- `driver.StmtExecContext`
- `driver.ConnBeginTx`
- `driver.Pinger`
- `driver.Validator`
- `driver.SessionResetter`
- `driver.NamedValueChecker`

The driver should also implement column metadata interfaces:

- `driver.RowsColumnTypeDatabaseTypeName`
- `driver.RowsColumnTypeLength`
- `driver.RowsColumnTypeNullable`
- `driver.RowsColumnTypePrecisionScale`
- `driver.RowsColumnTypeScanType`

### 7.6 Query execution

The driver must support direct query execution through `database/sql`.

Required examples:

```go
rows, err := db.QueryContext(ctx, "SELECT * FROM TEST")
```

```go
row := db.QueryRowContext(ctx, "SELECT 1")
```

Requirements:

- Prepare or execute query using H2 native protocol.
- Fetch result column metadata.
- Fetch rows.
- Decode H2 values into valid `driver.Value` values.
- Support result sets larger than one fetch batch.
- Close remote result objects when rows are closed.
- Return `io.EOF` correctly when rows are exhausted.

### 7.7 Exec execution

The driver must support non-query SQL execution.

Required examples:

```go
result, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS T(ID INT PRIMARY KEY)")
```

```go
result, err := db.ExecContext(ctx, "INSERT INTO T(ID) VALUES(?)", 1)
```

Requirements:

- Execute statements using H2 native protocol.
- Support positional `?` parameters.
- Return affected row count.
- Request generated keys using H2's native generated-keys support where applicable.
- Expose the first generated key through `Result.LastInsertId()` when H2 returns a single numeric generated key.
- Return a clear unsupported/unavailable error from `LastInsertId()` when no generated key is available or when the generated key is not numeric.
- Preserve H2 SQL errors.

### 7.8 Prepared statements

The driver must support prepared statements.

Required example:

```go
stmt, err := db.PrepareContext(ctx, "INSERT INTO T(ID, NAME) VALUES(?, ?)")
```

Requirements:

- Prepare statement using H2 native protocol.
- Read parameter metadata.
- Implement `NumInput()`.
- Execute prepared query and exec operations.
- Support repeated statement execution.
- Close remote command objects when `Stmt.Close()` is called.

### 7.9 Parameters

The driver must support positional `?` parameters.

MVP parameter types:

- `nil`
- `bool`
- integer types accepted by `database/sql`
- floating point types accepted by `database/sql`
- `string`
- `[]byte`
- `time.Time`
- values implementing `driver.Valuer`, including `github.com/google/uuid.UUID` through its standard SQL value support

When H2 parameter metadata is available, the driver should use it to encode string values correctly for `NUMERIC` and `UUID` parameters. The driver should return clear errors for unsupported parameter types.

### 7.10 Transactions

The driver must support transactions through `database/sql`.

Required examples:

```go
tx, err := db.BeginTx(ctx, nil)
```

```go
err = tx.Commit()
```

```go
err = tx.Rollback()
```

Requirements:

- Disable autocommit when a transaction begins.
- Commit changes on `Commit()`.
- Roll back changes on `Rollback()`.
- Restore autocommit after commit or rollback.
- Respect supported transaction options where possible.
- Return clear errors for unsupported isolation levels or read-only options.
- Prevent pooled connections from being reused with an open transaction.

### 7.11 Connection pool safety

The driver must behave correctly with Go's `database/sql` connection pool.

Requirements:

- Implement `Ping`.
- Implement `IsValid` using real connection/session validation.
- Implement `ResetSession`.
- Roll back pending transactions when needed before returning a connection to the pool.
- Close broken connections and return `driver.ErrBadConn` where appropriate.
- Avoid concurrent unsynchronized use of one TCP connection.

### 7.12 Context cancellation and timeouts

The driver should respect `context.Context` in context-aware operations.

Requirements:

- Respect context cancellation in connect, prepare, query, exec, and transaction begin operations.
- Use network deadlines where appropriate.
- If a statement is running and context is canceled, use H2's native cancellation mechanism where possible.
- Mark connection state safely after cancellation.
- Return context errors where appropriate.

### 7.13 Error handling

The driver must decode H2 SQL errors into useful Go errors.

Error information should include where available:

- SQLState.
- H2 error code.
- Error message.
- SQL text related to error.
- Server trace, optionally hidden behind debug formatting or a field.

Requirements:

- Do not collapse all server errors into generic network errors.
- Preserve H2 SQLState and vendor error code.
- Return clear unsupported-feature errors.
- Return clear unsupported-server-version errors.

### 7.14 Data type support

#### MVP supported result types

The first production target must support these common H2 types. All result values must be returned as one of the permitted `database/sql/driver.Value` types (`int64`, `float64`, `bool`, `[]byte`, `string`, `time.Time`, or `nil`); narrower Go types are widened accordingly.

| H2 type | Go scan representation |
|---|---|
| `NULL` | `nil` |
| `BOOLEAN` | `bool` |
| `TINYINT` | `int64` |
| `SMALLINT` | `int64` |
| `INTEGER` | `int64` |
| `BIGINT` | `int64` |
| `REAL` | `float64` (H2 `REAL` is 32-bit and is widened to `float64`) |
| `DOUBLE` | `float64` |
| `CHAR` | `string` |
| `VARCHAR` | `string` |
| `VARCHAR_IGNORECASE` | `string` |
| `BINARY` | `[]byte` |
| `VARBINARY` | `[]byte` |
| `DATE` | `time.Time` |
| `TIME` | `time.Time` or documented time-only representation |
| `TIME WITH TIME ZONE` | `time.Time` or documented custom representation |
| `TIMESTAMP` | `time.Time` |
| `TIMESTAMP WITH TIME ZONE` | `time.Time` |
| `NUMERIC` | `string` preserving H2 `BigDecimal.toPlainString()` exact value |
| `UUID` | canonical UUID `string`; interoperability tested with `github.com/google/uuid` |
| `CLOB` | `string` for inline CLOBs (≤ server `MAX_LENGTH_INPLACE_LOB`); fetch-on-demand CLOBs fetched via `LOB_READ` |
| `BLOB` | `[]byte` for inline BLOBs; fetch-on-demand BLOBs fetched via `LOB_READ` |
| `JSON` | `[]byte` (raw JSON bytes) |
| `GEOMETRY` | `[]byte` (raw WKB-like bytes) |
| `JAVA_OBJECT` | `[]byte` (raw serialized bytes) |
| `ENUM` | `int64` (ordinal value) |
| `INTERVAL` | `string` (H2's canonical interval text) |
| `ARRAY` | `string` (comma-separated elements in brackets) |
| `ROW` | `string` (comma-separated fields in parentheses) |

#### Advanced type support

The driver should handle these types in later iterations or via documented fallback/error behaviour:

- `DECFLOAT`

Unsupported or partially supported types must not be silently corrupted.

### 7.15 Metadata

The driver should expose column metadata via `database/sql` where possible.

Requirements:

- Column names.
- H2 database type names.
- Nullable information where available.
- Length for string/binary types where available.
- Precision and scale for numeric/time types where available.
- Reasonable Go scan type hints.

### 7.16 Logging and diagnostics

The driver should provide optional diagnostics using Go's standard `log/slog` package.

Requirements:

- Do not log by default.
- Use `log/slog` for driver diagnostics when logging is enabled.
- Use text output, not JSON output. The default documented handler should be `slog.NewTextHandler`.
- Allow callers to provide their own `*slog.Logger` or logging configuration.
- Redact passwords and password hashes from logs.
- Include protocol version and server target in diagnostic errors where useful.

## 8. Non-functional requirements

### 8.1 Runtime and dependencies

- Pure Go.
- No CGO required.
- No JVM required by the driver.
- H2 server still requires Java externally, but this is outside the driver runtime.
- Minimize third-party dependencies.

### 8.2 Security

- Do not log passwords.
- Do not log password hashes.
- Use H2-compatible password hashing for authentication.
- Support TLS/SSL only if explicitly added later; plain TCP is sufficient for initial local/server use.
- Treat `.env` as sensitive local configuration.

### 8.3 Compatibility

- Must support H2 **2.4.240**.
- Must require native TCP protocol **21**.
- May support later H2 versions only after testing and explicit decision.
- Must fail clearly for unsupported H2 server/protocol versions.

### 8.4 Performance

- Avoid unnecessary allocations in value encoding/decoding.
- Fetch rows in batches.
- Do not load arbitrarily large result sets into memory.
- Do not load large LOBs into memory without explicit documented behaviour.

### 8.5 Reliability

- Remote statements and result sets must be closed.
- Broken connections must not return to the pool.
- Context cancellation must not leave the driver in an unknown reusable state.
- Integration tests must use a real H2 server.

## 9. Testing requirements

### 9.1 Local integration testing

The test suite must support the existing local setup:

```bash
cd h2-data
./h2.sh
```

Tests should load:

```text
JDBC_URL
JDBC_USER
JDBC_PASSWORD
```

from `h2-data/.env` or equivalent environment variables.

### 9.2 Local build system target

The project uses a **local build system driven by a `Makefile`**. There is no remote CI pipeline; all build, vet, lint, and test steps run on the developer's local machine.

The `Makefile` must provide targets covering at least:

- `build` — `go build ./...`
- `vet` — `go vet ./...`
- `lint` — local linter run (for example `golangci-lint`) when installed
- `test` — unit tests (`go test ./...`)
- `test-race` — unit tests under `-race`
- `test-integration` — integration tests (`-tags integration`) against a locally running H2 server

Local integration builds and tests should run against:

- H2 **2.4.240**
- Later H2 versions only when explicitly supported

The local build system should not run compatibility jobs for older H2 versions.

Integration targets should use the local `h2-data` environment and skip cleanly when the H2 server or required env vars are unavailable.

### 9.3 Required test categories

- DSN parsing.
- Password hashing compatibility.
- Protocol 21 handshake.
- Unsupported protocol/server error.
- `Ping`.
- `SELECT 1`.
- Query without parameters.
- Query with parameters.
- Exec without parameters.
- Exec with parameters.
- Prepared statements.
- Statement close.
- Rows close.
- Transactions commit.
- Transactions rollback.
- Connection pool reuse.
- Session reset.
- Common scalar type round trips.
- H2 SQL error decoding.
- Context timeout/cancellation.
- Race detector.

## 10. Acceptance criteria

The first usable release is accepted when all criteria below are met.

### 10.1 Connectivity

- Driver connects to H2 `2.4.240` in TCP server mode.
- Driver parses `jdbc:h2:tcp://localhost:9092/h2-go` correctly.
- Driver authenticates using `JDBC_USER` and `JDBC_PASSWORD` from environment.
- Driver rejects unsupported protocol versions with a clear error.

### 10.2 `database/sql` compatibility

- Driver works with `sql.Open`.
- Driver works with `db.PingContext`.
- Driver works with `db.QueryContext`.
- Driver works with `db.ExecContext`.
- Driver works with `db.PrepareContext`.
- Driver works with `db.BeginTx`.
- Driver supports connection pooling without leaking open transactions or remote objects.

### 10.3 SQL operations

- Can create a table.
- Can insert rows with parameters.
- Can query rows.
- Can scan common scalar values.
- Can update rows and return rows affected.
- Can delete rows and return rows affected.
- Can commit a transaction.
- Can roll back a transaction.

### 10.4 Error behaviour

- Invalid SQL returns an H2 SQL error with SQLState and error code.
- Invalid credentials return a clear connection/authentication error.
- Unsupported types return clear errors or documented fallback values.
- Context timeout returns a context-related error and leaves connection state safe.

### 10.5 Implementation constraints

- No PostgreSQL mode.
- No JDBC bridge.
- No copied Java code.
- Pure Go driver code.
- H2 Java source used only as protocol reference/specification.

## 11. Release scope

### 11.1 MVP release

The MVP should include:

- Go module skeleton.
- Driver registration.
- JDBC-style DSN parser.
- Native Go DSN parser.
- TCP connection.
- Protocol 21 handshake.
- Required `database/sql/driver` interfaces.
- Context-aware query/exec/prepare where practical.
- Prepared statements.
- Transactions.
- Basic scalar type support.
- UUID scan/parameter interoperability using canonical strings and `github.com/google/uuid` tests.
- Generated key support for single numeric `LastInsertId()` values.
- Error decoding.
- Local integration tests.
- Local `Makefile` build system targeting H2 2.4.240.

### 11.2 Post-MVP enhancements

Post-MVP may include:

- Full context cancellation using H2 `SESSION_CANCEL_STATEMENT`.
- Extended generated keys support for non-numeric or multi-column generated keys, if needed.
- LOB streaming.
- Additional UUID helper APIs, if needed.
- JSON native support.
- Exact decimal type support.
- Array and interval support.
- Multiple result sets.
- TLS/SSL TCP support.
- Benchmarks and performance tuning.

## 12. Product decisions

The following product decisions are resolved and should be treated as requirements for project planning:

1. **Go module path:** `github.com/rom35-cz/h2go`.
2. **Repository visibility:** initially private under `rom35-cz/h2go`; later planned to be opened as an open source repository.
3. **Package name:** root package name `h2go`, matching the last element of the module path so no import alias is required.
4. **Registered `database/sql` driver name:** `h2`.
5. **NUMERIC handling:** H2 implements `NUMERIC` with Java `java.math.BigDecimal` (`ValueNumeric` extends H2's `ValueBigDecimalBase`). Go has no standard-library equivalent of `BigDecimal`, and `database/sql/driver.Value` must use standard driver value types. Therefore MVP result scanning should return `NUMERIC` as an exact decimal `string`, based on H2's plain string representation, to avoid precision loss. Future optional decimal helpers may be added later.
6. **UUID handling:** H2 implements UUID using `java.util.UUID` semantics backed by two 64-bit values (`high` and `low`) and a canonical 36-character textual form. Go has no standard-library UUID type. The driver should return UUID values as canonical strings by default and support interoperability with the mature external package `github.com/google/uuid` for tests and optional helper APIs. Parameter input through `driver.Valuer` should allow `google/uuid.UUID` values to work naturally.
7. **TLS/SSL:** defer to a later phase; not part of the first release.
8. **Generated keys:** MVP should request H2 generated keys where applicable and expose the first single numeric generated key through `Result.LastInsertId()`. If no key exists, multiple/non-numeric keys are returned, or H2 does not provide a usable generated key, `LastInsertId()` should return a clear unavailable/unsupported error. Extended multi-column generated-key APIs are post-MVP.
9. **Debug tracing:** use standard `log/slog` with text output, not JSON.

## 13. Reference sources

Primary H2 source files to use as protocol reference/specification:

- `org.h2.server.TcpServerThread`
- `org.h2.engine.SessionRemote`
- `org.h2.value.Transfer`
- `org.h2.engine.Constants`
- `org.h2.value.ValueNumeric`
- `org.h2.value.ValueBigDecimalBase`
- `org.h2.value.ValueUuid`

Existing Go prototypes to consult as references only:

- `github.com/jmrobles/h2go`
- `github.com/CodinGame/h2go`

These sources should guide behaviour and tests, but the implementation should remain original Go code.
