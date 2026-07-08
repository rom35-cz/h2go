# Implementation Plan: Pure Go H2 Native TCP Driver (`github.com/rom35-cz/h2go`)

Date: 2026-07-08  
Source: `PRD.md`  
Target: H2 **2.4.240+**, native TCP protocol **21** only  
Module/package: `github.com/rom35-cz/h2go` (package `h2go`)

## How to use this plan

- Tasks are executed **strictly in order**, one after another.
- Every task ends in a **consistent, compiling, test-passing state**. `go build ./...`, `go vet ./...`, and `go test ./...` must succeed at the end of each task (integration tests may be skipped when no H2 server is available, but must be present and green when it is).
- Each task lists: **Goal**, **Work**, **Deliverables**, **Done when**.
- "Reference" means the H2 Java source is consulted only as a protocol specification. No Java code is copied or translated. All Go code is original.
- H2 reference tree: `/tmp/pi-github-repos/h2database/h2database@version-2.4.240/h2/src/main/org/h2`. Go prototypes for behaviour cross-check only: `jmrobles/h2go`, `CodinGame/h2go`.

## Test environment (shared by integration tasks)

- Start server: `cd h2-data && ./h2.sh` (H2 2.4.240, TCP port 9092).
- Config from `h2-data/.env`: `JDBC_URL=jdbc:h2:tcp://localhost:9092/h2-go`, `JDBC_USER`, `JDBC_PASSWORD`.
- Integration tests read these env vars and **skip** (via `t.Skip`) when unset, so unit-only runs stay green.

---

## Phase 0 — Project skeleton and tooling

### T0.1 Module bootstrap
- **Goal:** Compilable, importable empty module.
- **Work:**
  - `go mod init github.com/rom35-cz/h2go` (target current Go stable; require ≥ 1.22).
  - Create `doc.go` with `package h2go` and a package-level doc comment.
  - Add `README.md` (short: purpose, H2 2.4.240+/protocol 21 scope, status "under development").
  - Add `.gitignore` (Go template + `.env`, `*.mv.db`, `*.trace.db`).
  - Do **not** add a placeholder license. Add `LICENSE` only when the project license is explicitly decided before public release.
  - Add a `Makefile` with targets: `build`, `vet`, `lint`, `test`, `test-race`, `test-integration`.
- **Deliverables:** `go.mod`, `doc.go`, `README.md`, `.gitignore`, build tooling.
- **Done when:** `go build ./...`, `go vet ./...`, and `go test ./...` pass; module path is `github.com/rom35-cz/h2go`.
- **Implementation notes:**
  - The `.gitignore` already existed from an earlier project setup pass; it was kept as-is.
  - `go.mod`: set `go 1.22` (the plan requires ≥ 1.22; the host toolchain is Go 1.26.4 which is fully compatible).
  - `doc.go`: package doc covers quick-start, DSN formats, and under-development status.
  - `README.md`: concise with table of DSN formats, scope, and license note.
  - `Makefile` and `.golangci.yml` were created here (T0.1 + T0.2 merged) so tooling is complete from the start.
  - Verified: `go build ./...`, `go vet ./...`, `go test ./...` all pass. ✅
- **Status:** ✅ Done — 2026-07-08

### T0.2 Local build system (Makefile)
- **Goal:** Local `Makefile` runs build/vet/lint/unit tests on demand.
- **Work:**
  - Author a `Makefile` with targets: `build` (`go build ./...`), `vet` (`go vet ./...`), `lint`, `test` (`go test ./...`), `test-race` (`go test -race ./...`), and `test-integration` (`-tags integration`).
  - The `test-integration` target uses the local `h2-data` environment and must skip cleanly when H2 is not running or env vars are unset. No older-H2 targets (per PRD §9.2).
  - Add `golangci-lint` config (govet, staticcheck, errcheck, ineffassign, revive minimal) invoked from the `lint` target when the tool is installed locally.
  - There is **no remote CI pipeline**; all steps run on the local machine.
- **Deliverables:** `Makefile`, `.golangci.yml`.
- **Done when:** `make build`, `make vet`, and `make test` succeed locally.
- **Implementation notes:**
  - Makefile and `.golangci.yml` were created in T0.1; this task verified and hardened them.
  - The installed `golangci-lint` v2.x requires `version: "2"` at the top of config. The `.golangci.yml` was rewritten to v2 format with `linters.default: none`, explicit enable list, and `formatters.gofmt` enabled.
  - `doc.go` was missing a trailing newline (`gofmt` issue); fixed with `gofmt -w`.
  - `test-integration` now optionally sources `h2-data/.env` before running, so local integration runs pick up the expected environment when present.
  - Verified: `make build`, `make vet`, `make test`, `make lint`, `make test-race`, `make test-integration` all succeed. ✅
- **Status:** ✅ Done — 2026-07-08

---

## Phase 1 — Connection string parsing (pure, no I/O)

### T1.1 DSN model and JDBC URL parser
- **Goal:** Parse `jdbc:h2:tcp://host:port/database` into a config struct.
- **Work:**
  - `dsn.go`: `type Config struct { Host, Port, Database, User, Password string; Params map[string]string; OriginalURL string }`.
  - Parser for `jdbc:h2:tcp://...`: extract host, port (default `9092`), database, and H2 JDBC URL parameters.
  - Parse H2 JDBC semicolon parameters such as `;USER=...;PASSWORD=...;IFEXISTS=TRUE`. Go-style query parameters may be supported for native Go DSNs, but semicolon parsing is required for JDBC-style URLs.
  - Implement the **database-name rule** (PRD §7.2): for `.../h2-go`, database sent to H2 is `h2-go`, not `/h2-go`.
  - Preserve the original JDBC URL string in `Config.OriginalURL` (needed by handshake).
  - Reject non-`tcp` H2 URLs (`mem:`, `file:`, `ssl:`) with clear errors.
- **Deliverables:** `dsn.go`, `dsn_test.go`.
- **Done when:** Unit tests cover host/port/db/semicolon-param extraction, the `h2-go` name rule, default port, and rejection cases.
- **Implementation notes:**
  - `ParseDSN` strips `jdbc:h2:` prefix, validates the inner protocol is `tcp:`, then delegates to `url.Parse` for host/port/path extraction.
  - The path is split on `;` — first segment is the database name (leading `/` stripped via `strings.TrimPrefix`), remaining segments are key=value parameters.
  - `USER` and `PASSWORD` parameters are extracted case-insensitively using `strings.EqualFold`.
  - Port defaults to "9092". Invalid (non-numeric) ports and missing hosts return clear errors.
  - `staticcheck` S1017 flagged a conditional string prefix check; replaced with unconditional `strings.TrimPrefix`.
  - `gofmt` flagged missing trailing newline; fixed with `gofmt -w`.
  - 12 tests cover: basic parse, default port, semicolon params with credentials, database-name rule, empty DSN, unsupported modes (mem/file/ssl/unknown), missing host, invalid port, multiple params, param without value, nested database path, case-insensitive USER/PASSWORD.
  - Verified: `go build ./...`, `go vet ./...`, `go test ./...`, `make lint` all pass. ✅
- **Status:** ✅ Done — 2026-07-08

### T1.2 Native Go DSN parser + credential merge
- **Goal:** Support `h2://` / `h2+tcp://` DSNs and separate credential injection.
- **Work:**
  - Parse `h2://user:password@host:port/database?k=v` and `h2+tcp://...` into the same `Config`.
  - For native Go DSNs, support normal URL query parameters (`?k=v`) and percent-decoding rules.
  - Single entry `ParseDSN(string) (*Config, error)` that dispatches on prefix (`jdbc:h2:` vs `h2://`/`h2+tcp://`).
  - Helper to overlay `JDBC_USER`/`JDBC_PASSWORD`-style credentials when the URL omits them; JDBC `;USER=` / `;PASSWORD=` values, native URL userinfo, and explicit environment overrides must have documented precedence.
  - Reject unknown schemes with actionable errors.
- **Deliverables:** extended `dsn.go`, expanded `dsn_test.go`.
- **Done when:** Both DSN styles parse to equivalent `Config`; credential overlay tested; unsupported schemes rejected.
- **Implementation notes:**
  - `ParseDSN` rewritten to dispatch on prefix: `jdbc:h2:` → `parseJDBC`, `h2://`/`h2+tcp://` → `parseNative`, anything else → clear error.
  - Extracted shared host/port validation into `validate(cfg)` helper.
  - `parseNative` uses `url.Parse` giving standard percent-decoding on userinfo and query parameters for free.
  - `MergeCredentials` fills only empty `User`/`Password` fields, preserving DSN-provided values. Tested precedence: DSN credentials always win over environment overrides.
  - Verification: `go build ./...`, `go vet ./...`, `go test ./...` (28 tests, 0 failures), `make lint` all pass. ✅
- **Status:** ✅ Done — 2026-07-08

---

## Phase 2 — Wire transfer primitives (pure, buffer-based)

### T2.1 Low-level Transfer codec
- **Goal:** Encode/decode the scalar wire primitives H2 uses on the TCP stream.
- **Work:** Reference `org.h2.value.Transfer`.
  - `transfer.go`: core codec over `io.Reader` / `io.Writer` with a `net.Conn` wrapper for deadlines and closing.
  - Implement `writeBool/readBool`, `writeByte/readByte`, `writeShort/readShort` (int16 BE; needed for protocol-20+ `SMALLINT`), `writeInt/readInt` (int32 BE), `writeLong/readLong` (int64 BE), `writeFloat/readFloat` (32-bit IEEE; needed for `REAL`), `writeDouble/readDouble`, `writeString/readString`, `writeBytes/readBytes`, and `writeRowCount/readRowCount`.
  - String encoding must match H2 `Transfer.writeString`: int32 length in Java UTF-16 code units, followed by big-endian 16-bit code units; `-1` means null. Tests must cover non-BMP characters/surrogate pairs.
  - Protocol 21 uses `writeRowCount/readRowCount` for row counts and update counts.
  - `flush()` and buffered read helpers.
- **Deliverables:** `transfer.go`, `transfer_test.go` (round-trip via `bytes.Buffer` and/or `net.Pipe`).
- **Done when:** Every primitive round-trips in unit tests, including edge cases (empty string, null string/bytes, nil bytes, negative ints, large int64, non-ASCII strings, non-BMP strings, float32/float64 values).

### T2.2 Protocol constants
- **Goal:** Central, documented constant set.
- **Work:** Reference `Constants`, `SessionRemote`, `TcpServerThread`.
  - `protocol.go`: TCP protocol version `21`; status codes (`STATUS_ERROR`, `STATUS_OK`, `STATUS_CLOSED`, `STATUS_OK_STATE_CHANGED`); operation constants from `SessionRemote`: `SESSION_PREPARE=0`, `SESSION_CLOSE=1`, `COMMAND_EXECUTE_QUERY=2`, `COMMAND_EXECUTE_UPDATE=3`, `COMMAND_CLOSE=4`, `RESULT_FETCH_ROWS=5`, `RESULT_RESET=6`, `RESULT_CLOSE=7`, `COMMAND_COMMIT=8`, `CHANGE_ID=9`, `COMMAND_GET_META_DATA=10`, `SESSION_SET_ID=12`, `SESSION_CANCEL_STATEMENT=13`, `SESSION_CHECK_KEY=14`, `SESSION_SET_AUTOCOMMIT=15`, `SESSION_HAS_PENDING_TRANSACTION=16`, `LOB_READ=17`, `SESSION_PREPARE_READ_PARAMS2=18`, `GET_JDBC_META=19`, `COMMAND_EXECUTE_BATCH_UPDATE=20`.
  - Generated-key mode constants from H2 `GeneratedKeysMode`: `NONE`, `AUTO`, `COLUMN_NUMBERS`, `COLUMN_NAMES`.
  - Value type codes (NULL=0 … DECFLOAT=31) as named constants.
- **Deliverables:** `protocol.go`.
- **Done when:** Compiles; constants documented with their H2 source names.

---

## Phase 3 — Handshake and authentication

### T3.1 Password hashing
- **Goal:** Produce H2-compatible auth hashes.
- **Work:** Reference `SHA256.getKeyPasswordHash` + `ConnectionInfo`.
  - `auth.go`: `userPasswordHash(user, password string) []byte` = `SHA256( UTF16-BE( UPPERCASE_ENGLISH(user) + "@" + password ) )` (H2 normalizes usernames with English/ASCII uppercase semantics before hashing).
  - `filePasswordHash(...)` returning nil when no file password.
  - Use the Go standard library (`unicode/utf16` plus explicit big-endian encoding) or a small original helper; document the exact Java UTF-16 code-unit byte layout.
- **Deliverables:** `auth.go`, `auth_test.go` with known-answer vectors (cross-checked against a live H2 login in T3.3).
- **Done when:** Deterministic hash output; unit tests pin the byte layout.

### T3.2 Handshake exchange
- **Goal:** Establish an authenticated session over TCP for protocol 21.
- **Work:** Reference `TcpServerThread.run` handshake + `SessionRemote.connectServer`.
  - `handshake.go`: dial TCP, write min=max=`21`, database name, original URL, username, user password hash, file password hash, and connection properties (count + key/value strings). Do not invent client-sent protocol-21 trailing fields; H2 2.4.240 creates network connection info server-side.
  - Read status; on `STATUS_ERROR` decode into a structured error (defer full decoding to Phase 7 but capture code/message/SQLState now).
  - Read negotiated protocol version; **fail clearly** if not `21` ("unsupported H2 server version; require protocol 21 / H2 2.4.240+").
  - Generate a client session id, then send `SESSION_SET_ID`, the session id, and a timezone id (required for protocol 20+); read status and the server autocommit boolean.
- **Deliverables:** `handshake.go`, `session.go` (holds `transfer`, session id, negotiated version, autocommit state).
- **Done when:** Unit test drives the handshake against a scripted in-memory server (recorded byte sequence) and asserts correct framing + version-mismatch rejection.

### T3.3 Connectivity integration test
- **Goal:** Real login against H2 2.4.240.
- **Work:**
  - `integration_test.go` (build tag `integration`): load `.env`, dial, handshake, assert session established, close cleanly.
  - Confirm the T3.1 hash vectors against the live server (auth success/failure).
- **Deliverables:** integration test + `.env` loader helper.
- **Done when:** With H2 running, the test connects and authenticates; without env, it skips.

---

## Phase 4 — `database/sql` driver skeleton

### T4.1 Driver, Connector, Conn wiring
- **Goal:** `sql.Open("h2", dsn)` yields a live connection.
- **Work:**
  - `driver.go`: `type Driver struct{}` implementing `driver.Driver` + `driver.DriverContext`; `func init(){ sql.Register("h2", &Driver{}) }`.
  - `connector.go`: `driver.Connector` from a parsed `Config`; `Connect(ctx)` performs Phase 3 handshake; `Driver()` returns the driver.
  - Export a config-based API for callers with credentials supplied separately from the URL, because `database/sql.Driver.Open` only receives one DSN string. For example: `NewConnector(cfg Config, opts ...Option) (driver.Connector, error)` and/or `OpenDB(cfg Config, opts ...Option) *sql.DB`.
  - `conn.go`: `type conn struct { sess *session; ... }` implementing `driver.Conn`. Until later phases, `Prepare` and `Begin` should return explicit not-yet-supported errors, not `driver.ErrSkip` (which is intended for optional-interface fallbacks). `Close` sends `SESSION_CLOSE` and handles the returned `STATUS_OK` when possible.
  - Guard against concurrent use of one TCP conn with a mutex/busy flag.
- **Deliverables:** `driver.go`, `connector.go`, `conn.go`.
- **Done when:** Integration test opens `sql.DB`, gets a conn, closes it; unit tests cover registration, DSN-based connector construction, and config-based construction with URL/user/password supplied separately.

### T4.2 Ping (interim) + Pinger
- **Goal:** `db.PingContext` round-trips.
- **Work:**
  - Implement `driver.Pinger` on `conn`. Interim implementation validates the live session with an existing lightweight protocol round-trip (for example `SESSION_HAS_PENDING_TRANSACTION`), then re-point it to real `SELECT 1` in Phase 5 (T5.3) once query execution exists.
  - Map dead connection to `driver.ErrBadConn`.
- **Deliverables:** `ping.go` (or method in `conn.go`).
- **Done when:** `db.PingContext` succeeds against live H2; returns error on a closed conn.

---

## Phase 5 — Query execution and row decoding

### T5.1 Command preparation and protocol-21 metadata readers
- **Goal:** Prepare SQL commands and implement the metadata readers needed by later query/statement work.
- **Work:** Reference `SessionRemote.prepareCommand`, `TcpServerThread` query path, `ResultColumn`, and `TypeInfo`.
  - `command.go`: send `SESSION_PREPARE` for basic command preparation and receive command id, `isQuery`, read-only flag, and parameter count.
  - Also implement `SESSION_PREPARE_READ_PARAMS2` for prepared statements that need parameter metadata (command type + parameter metadata); use it in Phase 6 for `Stmt.NumInput` and parameter encoding decisions.
  - Do **not** expect result column metadata from `SESSION_PREPARE`: H2 2.4.240 returns result column metadata from `COMMAND_EXECUTE_QUERY` (or from `COMMAND_GET_META_DATA` when explicitly requested).
  - Implement reusable result-column metadata reader: column count, and per column name, type (`TypeInfo` protocol-21 encoding), precision, scale, nullability, table/schema where provided.
  - `TypeInfo` decoder for protocol 21 (modern encoding).
- **Deliverables:** `command.go`, `typeinfo.go`, `metadata.go`, unit tests against recorded frames.
- **Done when:** Preparation and metadata parsing are verified with scripted server bytes for representative parameter and result types.

### T5.2 Rows: fetch, decode scalars, close
- **Goal:** Execute a query and stream rows into `driver.Rows`.
- **Work:** Reference `COMMAND_EXECUTE_QUERY` + `RESULT_FETCH_ROWS` + `Transfer.readValue`.
  - `rows.go`: `driver.Rows` with `Columns()`, `Next()` (fetch in batches, request more via `RESULT_FETCH_ROWS`), `Close()` (send `RESULT_CLOSE` and free the remote result), return `io.EOF` at end.
  - `value_read.go`: decode wire values for the **MVP scalar set** (NULL, BOOLEAN, TINYINT/SMALLINT/INTEGER/BIGINT→int64, REAL/DOUBLE→float64, CHAR/VARCHAR/VARCHAR_IGNORECASE→string, BINARY/VARBINARY→[]byte, DATE/TIME/TIME_TZ/TIMESTAMP/TIMESTAMP_TZ→time.Time, NUMERIC→string, UUID→canonical string). All results are one of the permitted `driver.Value` types (PRD §7.14).
  - Implement H2 date/time decoding from its wire representation (`dateValue`, nanoseconds-of-day, and timezone offset seconds for protocol 21), rather than assuming Unix timestamps.
  - Ensure `Rows.Close` frees the remote result; close the remote command too only when the rows object owns an ad-hoc command, not when it is backed by a reusable prepared statement.
- **Deliverables:** `rows.go`, `value_read.go`, unit tests (scripted rows) + type decode tests.
- **Done when:** Unit tests decode each MVP scalar; rows close frees server objects.

### T5.3 QueryerContext + real Ping
- **Goal:** `db.QueryContext(ctx, "SELECT ...")` works end to end.
- **Work:**
  - Implement `driver.QueryerContext` on `conn` for parameterless queries initially. If args are supplied before Phase 6 parameter support is complete, return a clear unsupported error or `driver.ErrSkip` only where `database/sql` can legitimately fall back.
  - Re-point `Ping` to execute `SELECT 1` and drain the result.
  - Integration tests: `SELECT 1`, `SELECT` over a temp table with multiple rows/columns, batch-boundary fetch (result larger than one fetch block).
- **Deliverables:** query path in `conn.go`, integration tests.
- **Done when:** Live `SELECT` queries return correct typed values; large result sets paginate correctly.

---

## Phase 6 — Exec, parameters, prepared statements

### T6.1 Value encoding (parameters)
- **Goal:** Encode Go `driver.Value` args to H2 wire values.
- **Work:** Reference `Transfer.writeValue`.
  - `value_write.go`: encode nil→NULL, bool, int64, float64, string, []byte, time.Time.
  - Implement H2 date/time encoding to the protocol representation (`dateValue`, nanoseconds-of-day, and timezone offset seconds for protocol 21), rather than sending Unix timestamps.
  - Use parameter `TypeInfo` when available to encode strings correctly for NUMERIC and UUID params (PRD §7.9); UUID wire values are two int64 values (`high`, `low`).
  - Clear error for unsupported Go types.
- **Deliverables:** `value_write.go`, round-trip unit tests with `value_read.go`.
- **Done when:** Each MVP param type encodes and decodes back correctly.

### T6.2 ExecerContext and Result
- **Goal:** `db.ExecContext` performs updates and returns affected rows.
- **Work:** Reference `COMMAND_EXECUTE_UPDATE`.
  - `result.go`: `driver.Result` with `RowsAffected()` (int64 count from protocol 21) and `LastInsertId()` returning a documented unsupported/unavailable error for now (generated keys in Phase 10).
  - Implement `driver.ExecerContext` on `conn`; bind positional `?` params via T6.1.
  - Even before generated-key support, encode and send H2's generated-keys request mode as `GeneratedKeysMode.NONE` for every `COMMAND_EXECUTE_UPDATE`; protocol 21 expects this field.
  - Integration: `CREATE TABLE`, `INSERT`, `UPDATE`, `DELETE`, `MERGE` with affected-row assertions.
- **Deliverables:** `result.go`, exec path, integration tests.
- **Done when:** DDL/DML execute; affected counts correct.

### T6.3 Prepared statements
- **Goal:** Reusable `driver.Stmt` with param metadata.
- **Work:**
  - `stmt.go`: `driver.Stmt` (`NumInput`, `Exec`/`Query` deprecated shims), `driver.StmtExecContext`, `driver.StmtQueryContext`, `driver.ConnPrepareContext`.
  - `NumInput()` from prepared parameter count; `Close()` sends `COMMAND_CLOSE` to free the remote command.
  - Support repeated execution of one prepared statement.
- **Deliverables:** `stmt.go`, integration tests (prepare once, exec/query many; stmt close frees server command).
- **Done when:** Prepared exec + query work repeatedly; no leaked commands.

### T6.4 NamedValueChecker
- **Goal:** Custom arg conversion and clear rejection of named params.
- **Work:**
  - Implement `driver.NamedValueChecker` on `conn`/`stmt`: accept `driver.Valuer` (incl. `github.com/google/uuid.UUID`), map supported Go types, and reject named params with a clear error because the MVP supports positional `?` parameters only.
- **Deliverables:** conversion logic + tests.
- **Done when:** `uuid.UUID` and standard types pass; unsupported/named args rejected clearly.

---

## Phase 7 — Error handling

### T7.1 Structured H2 errors
- **Goal:** Rich, typed SQL errors.
- **Work:** Reference `TcpServerThread` error frame + `DbException`.
  - `errors.go`: `type Error struct { SQLState string; Code int; Message, SQL, Trace string }` implementing `error`; constructor decoding the H2 error status frame.
  - Sentinel errors: `ErrUnsupportedServerVersion`, `ErrUnsupportedType`, `ErrClosed`.
  - Preserve SQLState, vendor code, original message, SQL text, and trace fields, but never add passwords or password hashes to client-side error text/logs. Keep server trace out of the default `Error()` string unless explicitly formatted for debugging.
  - Retrofit handshake (T3.2) and command paths to return `*Error` where the server supplied an H2 SQL error frame.
- **Deliverables:** `errors.go`, `errors_test.go`.
- **Done when:** Server errors expose SQLState + H2 code; unit tests decode a scripted error frame; integration test asserts a real syntax error surfaces code/state.

---

## Phase 8 — Transactions

### T8.1 Begin/Commit/Rollback
- **Goal:** Full transaction lifecycle via `database/sql`.
- **Work:** Reference `SESSION_SET_AUTOCOMMIT`, `SESSION_HAS_PENDING_TRANSACTION`, `COMMAND_COMMIT`, and SQL `ROLLBACK` execution.
  - `tx.go`: `driver.Tx`; `driver.ConnBeginTx` on `conn`.
  - Begin: set autocommit off. Commit: use `COMMAND_COMMIT` or a verified SQL `COMMIT` path. Rollback: execute SQL `ROLLBACK` through the command path because H2 `SessionRemote` has no dedicated rollback opcode. After either operation, restore autocommit.
  - Reject unsupported isolation levels / read-only options with clear errors; map supported ones.
  - Prevent pooled reuse while a tx is open.
- **Deliverables:** `tx.go`, integration tests (commit persists, rollback discards, nested-begin rejected).
- **Done when:** Commit/rollback behave correctly; autocommit restored afterward.

---

## Phase 9 — Connection pool safety and context

### T9.1 Validator + SessionResetter + ErrBadConn
- **Goal:** Safe pooling behaviour.
- **Work:**
  - Implement `driver.Validator.IsValid()` (real session liveness), `driver.SessionResetter.ResetSession(ctx)` (rollback pending tx via `SESSION_HAS_PENDING_TRANSACTION`, clear per-conn state).
  - Return `driver.ErrBadConn` on broken sockets so the pool discards them; never return a half-open conn.
  - Enforce single-flight use of the TCP conn (busy guard) with a clear error on concurrent misuse.
- **Deliverables:** validator/reset code, tests.
- **Done when:** Pool reuse test (many goroutines, `db.SetMaxOpenConns`) passes under `-race`; dirty conns are rolled back on reset.

### T9.2 Context cancellation and deadlines
- **Goal:** Respect `context.Context`.
- **Work:** Reference `SESSION_CANCEL_STATEMENT`.
  - Apply context deadlines to socket read/write in connect/prepare/query/exec/begin.
  - On cancellation of a running statement, send `SESSION_CANCEL_STATEMENT` on a side channel where feasible; otherwise mark the conn bad safely.
  - Return context errors; never leave the conn in an unknown-but-reused state.
- **Deliverables:** context plumbing, tests (timeout mid-query, cancel before/after send).
- **Done when:** Timeouts/cancels return context errors and leave conn state safe (bad conns discarded).

---

## Phase 10 — Generated keys and metadata

### T10.1 Generated keys → LastInsertId
- **Goal:** Single numeric generated key via `Result.LastInsertId()`.
- **Work:** Reference generated-keys request in the execute-update path.
  - Request generated keys on inserts where applicable; read the returned result.
  - Expose the first key as `LastInsertId()` when it is a single numeric value; otherwise return a clear unavailable/unsupported error (PRD §7.7, §12.8).
- **Deliverables:** generated-keys code, integration tests (auto-increment insert returns id; composite/non-numeric returns documented error).
- **Done when:** `LastInsertId()` returns the id for numeric single-key inserts; documented error otherwise.

### T10.2 Column type metadata interfaces
- **Goal:** Expose H2 column metadata to `database/sql`.
- **Work:**
  - Implement on `rows`: `RowsColumnTypeDatabaseTypeName`, `RowsColumnTypeLength`, `RowsColumnTypeNullable`, `RowsColumnTypePrecisionScale`, `RowsColumnTypeScanType`.
  - Map H2 type codes → database type names and Go scan types.
- **Deliverables:** metadata methods, tests via `sql.Rows.ColumnTypes()`.
- **Done when:** `ColumnTypes()` reports correct names/nullability/precision/scale/scan types for MVP types.

---

## Phase 11 — Logging and diagnostics

### T11.1 slog integration
- **Goal:** Optional `log/slog` text diagnostics, off by default.
- **Work:**
  - `logging.go`: accept an optional `*slog.Logger` via explicit connector/options API; default no-op. A DSN string cannot carry a logger object. If a DSN flag such as `trace=true` is later added, it may only enable a documented default text logger.
  - Documented default handler is `slog.NewTextHandler` (text, not JSON).
  - **Redact** passwords and password hashes everywhere; include protocol version + server target in diagnostic records.
- **Deliverables:** `logging.go`, tests asserting no secret leakage and no logs when disabled.
- **Done when:** Enabling a logger emits text records; disabled path is silent; secrets never logged.

---

## Phase 12 — Hardening, docs, release

### T12.1 Full integration matrix + race
- **Goal:** Exercise all PRD §9.3 test categories against live H2 2.4.240.
- **Work:**
  - Fill gaps: DSN parsing, password hashing, handshake, unsupported-version, Ping, SELECT 1, query/exec ±params, prepared, stmt/rows close, tx commit/rollback, pool reuse, session reset, scalar round-trips, error decoding, context cancel, `-race`.
  - Add a small connection-pool stress test and a scalar round-trip table test.
- **Deliverables:** complete `*_test.go` set; `make test-integration` green locally.
- **Done when:** All categories present and passing under `-race` with H2 running.

### T12.2 Acceptance-criteria verification
- **Goal:** Prove PRD §10 acceptance criteria.
- **Work:**
  - Map each §10 bullet to a test; add a `docs/ACCEPTANCE.md` traceability table (criterion → test name).
  - Verify implementation constraints (§10.5): no PG mode, no JDBC bridge, no CGO (`CGO_ENABLED=0` build), pure Go, no copied Java.
- **Deliverables:** `docs/ACCEPTANCE.md`, any missing tests.
- **Done when:** Every §10 criterion has a passing, referenced test.

### T12.3 Documentation and examples
- **Goal:** Usable public docs.
- **Work:**
  - Expand `README.md`: install, DSN formats (JDBC + native), `.env` usage, supported types table, limitations (post-MVP list), logging setup.
  - `example_test.go` runnable examples (open, query, exec, tx, prepared).
  - Package doc in `doc.go` with a quick-start.
- **Deliverables:** README, examples, doc.go.
- **Done when:** `go test` runs examples; docs match implemented behaviour.

### T12.4 v0.1.0 (MVP) tag
- **Goal:** Cut the first MVP release.
- **Work:**
  - Verify MVP scope (PRD §11.1) complete; finalize `CHANGELOG.md`; ensure `go vet`, lint, unit+integration all green.
  - Tag `v0.1.0`.
- **Deliverables:** `CHANGELOG.md`, git tag.
- **Done when:** Tagged release; local `make` targets green; MVP acceptance criteria satisfied.

---

## Post-MVP backlog (not scheduled here; from PRD §11.2)

- Full statement cancellation via `SESSION_CANCEL_STATEMENT` (deep integration).
- Extended generated keys (multi-column / non-numeric).
- LOB streaming (`LOB_READ`) for BLOB/CLOB.
- JSON, DECFLOAT (exact decimal), ENUM, INTERVAL, ARRAY, ROW, GEOMETRY, JAVA_OBJECT decoding.
- Multiple result sets.
- TLS/SSL TCP transport.
- Benchmarks and allocation tuning.

## Phase → PRD traceability

| Phase | PRD sections |
|---|---|
| 0 | §7.1, §8.1, §9.2 (local build system) |
| 1 | §7.2 |
| 2 | §7.3 (framing), §7.14 (codes) |
| 3 | §7.3, §8.2 |
| 4 | §7.1, §7.4, §7.11 (Ping) |
| 5 | §7.4, §7.6, §7.14, §7.15 |
| 6 | §7.7, §7.8, §7.9 |
| 7 | §7.13 |
| 8 | §7.10 |
| 9 | §7.11, §7.12 |
| 10 | §7.7 (keys), §7.15 (metadata) |
| 11 | §7.16, §8.2 |
| 12 | §9, §10, §11.1 |
