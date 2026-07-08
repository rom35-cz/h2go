# Feasibility Study: Pure Go `database/sql` Driver for H2 Server Mode

Date: 2026-07-08

## Scope

Build a **pure Go** driver for **H2 Database in native TCP server mode**, not H2 PostgreSQL mode and not a JDBC bridge. The driver should integrate with Go's `database/sql` package and implement the required and practical optional interfaces from `database/sql/driver`.

Local H2 setup inspected in this repository:

- JDBC URL shape: `jdbc:h2:tcp://localhost:9092/h2-go`
- H2 jar: `h2-data/h2-2.4.240.jar`
- Server script: `h2-data/h2.sh`
- Database file: `h2-data/data/h2-go.mv.db`

Credentials are in `h2-data/.env` and should be loaded from environment, not hard-coded.

## Executive summary

This project is **feasible**, but it is not a small wrapper project. H2's native TCP protocol is implemented in H2's Java source and is not a widely documented, independent wire protocol like PostgreSQL or MySQL. A mature driver will need to track H2 protocol versions and value encoding details directly from H2 source.

Recommendation:

1. **Do not use PostgreSQL mode.** It solves connection availability but not the requirement for a native H2 driver.
2. **Use existing Go H2 drivers only as prototypes/references**, not as the production foundation without major refactoring.
3. For a maintainable driver, **start a clean driver implementation** with a small amount of protocol knowledge validated against H2 source and integration tests. Borrow ideas/tests from existing drivers where licensing permits.
4. Target H2 **2.4.240 and later only**, using native TCP protocol **21**. Do not spend project effort on backward compatibility with older H2 protocol versions.

Estimated effort for one experienced Go developer:

- Basic usable alpha: **2-4 weeks**
- Good beta with `database/sql` context interfaces, transactions, metadata, and common H2 types: **4-8 weeks**
- Production-quality driver with broad type support, cancellation, LOB streaming, and CI against supported H2 versions only: **8-12+ weeks**

## Existing options found

### 1. `github.com/jmrobles/h2go`

Status: pure Go, native H2 TCP driver using `database/sql`.

Positive:

- Already implements the basic native H2 TCP handshake.
- Implements basic `database/sql` pieces: driver registration, connection, query, exec, statement, transaction, rows, result.
- Supports simple queries and prepared statements after small fixes.
- Useful as proof that native H2 TCP from Go is possible.

Limitations observed:

- README says **"VERY experimental"** and **"NOT use for production yet"**.
- Negotiates max H2 TCP protocol version **19**, while H2 2.4.240 supports up to **21**.
- Missing many modern `database/sql/driver` behaviours: robust context cancellation, reset session, proper validator, named value checking, complete column metadata, batch support, multiple result sets, generated keys, streaming LOBs.
- Incomplete H2 type support: TODOs include UUID, JSON, Decimal, etc.
- `Rows.Close()` and `Stmt.Close()` do not free remote server objects.
- Current DSN path handling sends `/h2-go` as the database name. With this local server base directory, H2 rejects it as an absolute path outside the base directory. It should send `h2-go` for the inspected JDBC URL.
- Error handling during handshake can misinterpret server error payloads as version/status values.

Local test result:

- Java JDBC connection to the local H2 server works.
- `jmrobles/h2go` failed against the local JDBC-style database path.
- After locally patching the DSN database path from `/h2-go` to `h2-go`, basic `SELECT 1`, `CREATE TABLE`, `MERGE`, and `SELECT` worked against H2 2.4.240 using protocol 19.

Conclusion: **good proof-of-concept and reference**, but not a mature base without significant rewrite.

### 2. `github.com/CodinGame/h2go`

Status: fork of `jmrobles/h2go`.

Positive:

- Adds Unix socket support.
- Fixes string reads with `io.ReadFull`, which is important for TCP correctness.
- Adjusts query row/fetch limits.

Limitations:

- Still based on the same experimental architecture.
- Still lacks complete `database/sql` behaviour and H2 type support.
- Still failed against the local JDBC-style path without database-name adjustment.

Conclusion: slightly better prototype than upstream for I/O correctness, but still not a mature driver.

### 3. H2 PostgreSQL mode + Go PostgreSQL driver

Rejected for this project.

It can be useful for simple connectivity, but it is not a pure native H2 driver. It hides H2-specific types/semantics, gives PostgreSQL-oriented errors/protocol behaviour, and does not satisfy the stated requirement.

### 4. JDBC bridge / running Java from Go

Rejected for this project.

A JDBC bridge may be quick, but it is not pure Go, adds JVM lifecycle/packaging complexity, and defeats the purpose of a native Go driver.

## H2 native TCP protocol facts relevant to implementation

H2's Java source is the practical protocol specification:

- Server-side TCP processing: `org.h2.server.TcpServerThread`
- Java remote client: `org.h2.engine.SessionRemote`
- Value and type encoding: `org.h2.value.Transfer`
- Protocol versions: `org.h2.engine.Constants`

For H2 2.4.240:

- Minimum supported TCP protocol: **17**
- Maximum supported TCP protocol: **21**
- H2 Java client sends min/max versions, database name, original URL, username, password hash, file password hash, and connection properties.
- Server chooses a compatible protocol version and returns status + version.
- For this project, the driver should request and require protocol **21**. If the server cannot negotiate protocol 21, the driver should fail with a clear "unsupported H2 server version" error.
- Protocol 21 uses the modern protocol-20+ behaviours, including `SESSION_SET_ID` with timezone information, `int64` row counts, and modern `TypeInfo` encoding.
- Older protocol branches should be intentionally omitted to keep implementation scope small.

Important H2 operations exposed by the native protocol include:

- `SESSION_PREPARE`
- `SESSION_PREPARE_READ_PARAMS2`
- `COMMAND_EXECUTE_QUERY`
- `COMMAND_EXECUTE_UPDATE`
- `COMMAND_CLOSE`
- `RESULT_FETCH_ROWS`
- `RESULT_CLOSE`
- `SESSION_SET_AUTOCOMMIT`
- `SESSION_HAS_PENDING_TRANSACTION`
- `SESSION_CANCEL_STATEMENT`
- `LOB_READ`
- `GET_JDBC_META`
- `COMMAND_EXECUTE_BATCH_UPDATE`

These operations are enough to implement a mature `database/sql` driver.

## Desired Go driver interface coverage

### Required `database/sql/driver` interfaces

A minimally valid driver needs:

- `driver.Driver`
  - `Open(name string) (driver.Conn, error)`
- `driver.Conn`
  - `Prepare(query string) (driver.Stmt, error)`
  - `Close() error`
  - `Begin() (driver.Tx, error)`
- `driver.Stmt`
  - `Close() error`
  - `NumInput() int`
  - `Exec(args []driver.Value) (driver.Result, error)`
  - `Query(args []driver.Value) (driver.Rows, error)`
- `driver.Tx`
  - `Commit() error`
  - `Rollback() error`
- `driver.Rows`
  - `Columns() []string`
  - `Close() error`
  - `Next(dest []driver.Value) error`
- `driver.Result`
  - `LastInsertId() (int64, error)`
  - `RowsAffected() (int64, error)`

### Recommended modern interfaces

For a serious driver, implement these from the start:

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
- `driver.RowsColumnTypeDatabaseTypeName`
- `driver.RowsColumnTypeLength`
- `driver.RowsColumnTypeNullable`
- `driver.RowsColumnTypePrecisionScale`
- `driver.RowsColumnTypeScanType`
- `driver.RowsNextResultSet` if multiple result sets are supported later

The context variants are important because applications expect query timeouts and cancellation to work.

## Proposed architecture

```text
h2driver/
  driver.go          registration, connector, DSN parsing
  conn.go            database/sql Conn implementation
  stmt.go            prepared statement lifecycle
  tx.go              transactions/autocommit/isolation
  rows.go            row fetching, metadata, result close
  result.go          rows affected, generated keys
  protocol/
    transfer.go      binary read/write, flush, deadlines
    session.go       H2 session operations
    value.go         H2 value encoding/decoding
    typeinfo.go      H2 type metadata encoding/decoding
    errors.go        SQLState/error decoding
    lob.go           LOB descriptors and LOB_READ
  h2type/
    decimal.go       optional exact decimal support
    uuid.go          optional UUID helpers
    json.go          JSON wrappers if needed
  internal/testh2/   integration test server helpers
```

Design principles:

- Keep H2 wire protocol isolated from `database/sql` plumbing.
- One physical TCP connection must not be used concurrently by multiple active commands unless internally synchronized. `database/sql` normally handles connection pooling, but rows can remain open while the connection is in use, so state management matters.
- Always send remote close operations for prepared commands and result sets.
- Use connection deadlines and H2 `SESSION_CANCEL_STATEMENT` for context cancellation.
- Keep protocol version as a connection property for diagnostics, but implement only protocol 21 value/type encoders initially.
- Prefer exact SQL metadata from H2 `ResultColumn` / `TypeInfo` to populate Go column type metadata.

## DSN proposal

Support JDBC-style input because users already have `JDBC_URL`:

```text
jdbc:h2:tcp://localhost:9092/h2-go
```

Also support native Go DSNs:

```text
h2://user:pass@localhost:9092/h2-go
h2+tcp://user:pass@localhost:9092/h2-go
h2s://user:pass@host:port/db      # optional SSL later
```

For the local inspected `.env`, the database name that should be sent to H2 is:

```text
h2-go
```

not:

```text
/h2-go
```

because H2 server was started with a base directory and treats `/h2-go` as an absolute filesystem path outside the allowed base directory.

Connection options should map to H2 properties where appropriate:

```text
h2://user:pass@localhost:9092/h2-go?mode=...&schema=...&network_timeout=...
```

But avoid inventing too many options initially. A robust parser for JDBC URL parameters (`;KEY=VALUE`) and Go query parameters (`?key=value`) is needed.

## Type support plan

### MVP types

- `NULL` -> `nil`
- `BOOLEAN` -> `bool`
- `TINYINT`, `SMALLINT`, `INTEGER`, `BIGINT` -> Go integer values accepted by `database/sql` (`int64` on scan where appropriate)
- `REAL`, `DOUBLE` -> `float64` / `float32` where appropriate
- `CHAR`, `VARCHAR`, `VARCHAR_IGNORECASE` -> `string`
- `BINARY`, `VARBINARY` -> `[]byte`
- `DATE`, `TIME`, `TIME WITH TIME ZONE`, `TIMESTAMP`, `TIMESTAMP WITH TIME ZONE` -> `time.Time` or documented custom wrappers
- `NUMERIC` -> initially string or `[]byte`; later exact decimal support

### Full/advanced types

- `UUID` -> `[16]byte`, string, or optional UUID package interop
- `JSON` -> `[]byte` / `json.RawMessage`
- `DECFLOAT` -> string/custom decimal representation
- `ENUM` -> string or ordinal+label wrapper
- `INTERVAL` -> custom interval type or string fallback
- `ARRAY` -> `[]any` where feasible
- `ROW` -> custom composite representation or string fallback
- `BLOB`, `CLOB` -> streaming or `[]byte`/string fallback with size controls
- `JAVA_OBJECT`, `GEOMETRY` -> `[]byte` fallback unless explicit support is added

A mature driver should not silently corrupt unsupported types. It should return clear errors or documented fallback encodings.

## Transaction behaviour

Use H2 protocol support instead of SQL text where possible:

- `BeginTx`:
  - send `SESSION_SET_AUTOCOMMIT false`
  - apply supported isolation level if provided
  - handle read-only option if possible
- `Commit`:
  - send `COMMAND_COMMIT` or execute `COMMIT`
  - restore autocommit
- `Rollback`:
  - execute `ROLLBACK`
  - restore autocommit
- `ResetSession`:
  - rollback any pending transaction
  - reset autocommit true
  - clear session state if necessary

H2's protocol has `SESSION_HAS_PENDING_TRANSACTION`; use it for safe pool reuse.

## Context cancellation and timeouts

A mature Go driver should support:

- `QueryContext`, `ExecContext`, `PrepareContext`, and `BeginTx` respecting `context.Context`.
- Per-operation network deadlines.
- If a context is canceled while a statement is running, open the special H2 cancellation connection and send `SESSION_CANCEL_STATEMENT` for the active statement ID.
- Return Go context errors when appropriate, but preserve H2 SQLState and error code for server-side SQL errors.

This is one of the larger gaps in existing Go H2 prototypes.

## Testing strategy

### Local integration tests

Use the existing local H2 jar and server mode:

```bash
cd h2-data
./h2.sh
```

Tests should load `.env`, parse the JDBC URL, and connect natively.

### Automated CI target

This project will **not** maintain backward compatibility with older H2 versions. CI should run H2 server containers or Java processes only for:

- H2 **2.4.240**
- Later H2 releases when we intentionally choose to support them

The driver should require native TCP protocol **21**. Avoid CI jobs for H2 1.4.x, 2.1.x, or 2.2.x because they add implementation branches and maintenance work that are outside the project scope.

Test categories:

- Handshake and authentication
- DSN parsing from JDBC and native Go formats
- `Ping`
- Query/exec without parameters
- Prepared statements with parameters
- Transactions commit/rollback
- Autocommit reset and pool reuse
- Result fetching across multiple fetch batches
- Remote close of statements/results
- Common type round-trips
- Error decoding: SQLState, message, SQL, vendor code, trace
- Context timeout and cancellation
- Concurrent use via `database/sql` connection pool
- Race detector

## Main risks

### 1. Protocol is internal to H2

H2 native TCP protocol is not an independent public standard. Future H2 releases can change details. Mitigation: target protocol 21 only, fail clearly on unsupported protocol negotiation, and maintain CI against H2 2.4.240 plus any later H2 release we explicitly decide to support.

### 2. Type encoding complexity

H2 has many types and protocol-version-specific encodings. Mitigation: implement staged type support and clear unsupported-type errors.

### 3. LOB streaming

BLOB/CLOB support needs special handling with `LOB_READ`, HMAC verification data, and streaming semantics. Mitigation: defer full streaming to a later milestone; initially support small LOBs safely or return clear errors.

### 4. Cancellation and connection state

Correct cancellation requires a second connection and session/statement IDs. Bad cancellation can leave pooled connections broken. Mitigation: design session state carefully and mark bad connections with `driver.ErrBadConn` when uncertain.

### 5. Licensing

H2 source is available and should be treated as the protocol reference/specification for this independent Go implementation. We should not copy or translate Java source code into Go. Instead, use H2 source to understand the wire protocol, then write original Go code and tests that verify behaviour against a real H2 server. Existing Go drivers may be consulted as prototypes, but production code should remain independently implemented.

## Suggested milestones

### Milestone 0: Protocol spike and project skeleton

Deliverables:

- New Go module
- DSN parser for JDBC and Go-style DSNs
- TCP connection and H2 2.4.240 handshake
- `Ping` using `SELECT 1`
- Integration test using local `.env`

### Milestone 1: Basic `database/sql` driver

Deliverables:

- Required `database/sql/driver` interfaces
- `QueryContext` and `ExecContext`
- Prepared statements
- Basic rows/result implementation
- Common scalar type decoding/encoding
- Remote `COMMAND_CLOSE` and `RESULT_CLOSE`

### Milestone 2: Transactions and pool safety

Deliverables:

- `BeginTx`, `Commit`, `Rollback`
- Autocommit state management
- `ResetSession`, `IsValid`, `Ping`
- Pending transaction check
- Better error mapping

### Milestone 3: Metadata and context cancellation

Deliverables:

- Column type metadata interfaces
- Context deadlines
- H2 statement cancellation support
- Fetch batching for large result sets

### Milestone 4: Type completeness

Deliverables:

- Decimal/NUMERIC exact handling
- UUID
- JSON
- Time zone correctness
- Array/interval/enum fallback or native support
- Initial BLOB/CLOB strategy

### Milestone 5: Hardening

Deliverables:

- CI against H2 2.4.240 and any later explicitly supported H2 release
- Race detector clean
- Fuzz tests for DSN and value decoder
- Benchmarks
- Documentation and examples

## Final recommendation

Create a new driver rather than trying to make the current experimental `h2go` code production-ready in-place.

Use `jmrobles/h2go` and `CodinGame/h2go` as:

- Proof that the native TCP approach works.
- A source of initial test cases and behavioural comparison.
- A quick reference for password hashing and basic protocol flow.

But build the actual driver around:

- H2 2.4.240 protocol version 21 support.
- Clean separation between protocol and `database/sql` interfaces.
- Full lifecycle management for remote commands/results.
- Modern context-aware Go driver interfaces.
- A comprehensive integration-test matrix.

This is a realistic project and can produce a useful native H2 Go driver, but maturity depends heavily on disciplined protocol tests and staged type support.
