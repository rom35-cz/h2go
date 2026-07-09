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
- **Phase 1 review fixes (2026-07-08):**
  - **Port range validation**: `url.Parse` accepts out-of-range numeric ports (e.g. `0`, `65536`) without error. Added `1 ≤ port ≤ 65535` range check to `validate()`; added 4 test cases covering JDBC and native variants.
  - **Dead guard removed**: `parseJDBC` had an unreachable `!strings.HasPrefix(input, prefix)` guard (ParseDSN guarantees the prefix before dispatching). Removed.
  - **Dead `errors.Is` in test**: `TestParseDSN_EmptyDSN` used `errors.Is(err, errors.New(…))` which always returns false (distinct pointer). Replaced with a plain `err.Error()` check; removed the unused `errors` import.
  - **Doc comment formatting**: `ParseDSN` godoc mixed tab+space indentation, causing labels to render inside code blocks. Rewritten to proper godoc format (prose labels, single-tab code blocks). Verified with `go doc ParseDSN`.
  - Verified: all 32 tests pass, `make lint` clean. ✅
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
- **Implementation notes:**
  - `Tr` wraps `bufio.Reader`/`bufio.Writer` over `io.ReadWriteCloser`. Multi-byte primitives use `binary.BigEndian` encoding.
  - String encoding matches H2 `Transfer.writeString`: `WriteString` writes int32 UTF-16 code-unit length (Java `String.length()`), then each code unit as big-endian uint16. Null is length `-1`. `utf16Encode`/`utf16Decode` handle surrogate pairs for non-BMP characters.
  - `ReadString` returns `*string` to distinguish `null` vs empty `""`. `ReadStringPtr` is a convenience wrapper returning the string or empty.
  - `WriteNullString`/`WriteBytes(nil)` write a length `-1` marker.
  - `WriteRowCount`/`ReadRowCount` use int64 (protocol 21 always uses long).
  - Adjacent write methods on the same `Tr` are buffered; call `Flush()` to send.
  - The reference Java source (`Transfer.java`) shows `writeString` using `out.writeChars(s)`, which writes each Java char (16-bit) in big-endian order. This matches `binary.BigEndian.PutUint16` per code unit.
  - `gofmt` and `staticcheck` an unparam (`utf16Valid(s)` never uses `s`; renamed to `_`), and a `[]rune` conversion (ranging over `s` directly avoids it). Fixed.
  - Verification: `go build ./...`, `go vet ./...`, `go test -v ./...` (89 tests across DSN+Transfer), `make lint` all pass. ✅
- **Status:** ✅ Done — 2026-07-08

### T2.2 Protocol constants
- **Goal:** Central, documented constant set.
- **Work:** Reference `Constants`, `SessionRemote`, `TcpServerThread`.
  - `protocol.go`: TCP protocol version `21`; status codes (`STATUS_ERROR`, `STATUS_OK`, `STATUS_CLOSED`, `STATUS_OK_STATE_CHANGED`); operation constants from `SessionRemote`: `SESSION_PREPARE=0`, `SESSION_CLOSE=1`, `COMMAND_EXECUTE_QUERY=2`, `COMMAND_EXECUTE_UPDATE=3`, `COMMAND_CLOSE=4`, `RESULT_FETCH_ROWS=5`, `RESULT_RESET=6`, `RESULT_CLOSE=7`, `COMMAND_COMMIT=8`, `CHANGE_ID=9`, `COMMAND_GET_META_DATA=10`, `SESSION_SET_ID=12`, `SESSION_CANCEL_STATEMENT=13`, `SESSION_CHECK_KEY=14`, `SESSION_SET_AUTOCOMMIT=15`, `SESSION_HAS_PENDING_TRANSACTION=16`, `LOB_READ=17`, `SESSION_PREPARE_READ_PARAMS2=18`, `GET_JDBC_META=19`, `COMMAND_EXECUTE_BATCH_UPDATE=20`.
  - Generated-key mode constants from H2 `GeneratedKeysMode`: `NONE`, `AUTO`, `COLUMN_NUMBERS`, `COLUMN_NAMES`.
  - Value type codes (NULL=0 … DECFLOAT=31) as named constants.
- **Deliverables:** `protocol.go`.
- **Done when:** Compiles; constants documented with their H2 source names.
- **Implementation notes:**
  - Protocol version constants (`TCPProtocolVersion21`, min/max supported) from `org.h2.engine.Constants`.
  - Status codes (`StatusOK`, `StatusError`, `StatusClosed`, `StatusOKStateChanged`) and operation codes (`SessionPrepare=0` … `CommandExecuteBatchUpdate=20`) from `org.h2.engine.SessionRemote`.
  - Generated keys mode constants from `org.h2.engine.GeneratedKeysMode`.
  - Value type codes (`ValueTypeNull=0` … `ValueTypeDecfloat=31`) from `org.h2.value.Transfer`, with source enum names in doc comments.
  - `DefaultTCPPort` constant added for use by DSN parsing (replaces a hardcoded string).
- **Phase 2 review fixes (2026-07-08):**
  - **Bug — nil panic on read from write-only `Tr`**: `NewWriter`-created `Tr` has `t.r == nil`; all read methods called on it would panic. Added `checkReader()` helper called at the top of `ReadByte`, `ReadInt16`, `ReadInt32`, `ReadInt64`; all higher-level reads propagate that error.
  - **Wrong test `TestTr_ReadOnWriteOnly`**: The test created `NewReader` (read-only), then read from an empty buffer (getting EOF). It did not test the nil-reader case it claimed to. Renamed the existing test to `TestTr_ReadFromEmptyBuffer`; added a new correct `TestTr_ReadOnWriteOnly` using `NewWriter` that asserts the 'write-only' error message.
  - **`DefaultTCPPort` in wrong const block**: It was inside the value-type (`ValueType*`) block. Moved into the TCP version/port block. Added `DefaultTCPPortStr = "9092"` alongside it.
  - **`dsn.go` hardcoded `"9092"`**: Both `parseJDBC` and `parseNative` used `Port: "9092"` instead of the protocol constant. Changed to `DefaultTCPPortStr`.
  - **`ReadString` doc comment backwards**: "Use `ReadStringPtr` for a `*string` return" was wrong — `ReadString` already returns `*string`; `ReadStringPtr` returns `string`. Fixed to clarify the null-vs-empty distinction.
  - **Dead `utf16Valid` function and guard**: `utf16Valid` always returned `true`; the `if !utf16Valid(s)` guard in `WriteString` was unreachable dead code. Removed the function and the guard.
  - **Per-code-unit I/O in `WriteString`/`ReadString`**: Both previously called `t.w.Write`/`io.ReadFull` once per UTF-16 code unit (2 bytes at a time). Replaced with a single batch operation in each direction.
  - Verified: all 91 tests pass, `make lint` clean. ✅
- **Status:** ✅ Done — 2026-07-08

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
- **Implementation notes:**
  - `auth.go` provides three functions: `userPasswordHash`, `filePasswordHash`, and the internal `keyPasswordHash`.
  - `keyPasswordHash` mirrors `SHA256.getKeyPasswordHash` exactly: it concatenates `userName + "@" + password`, encodes as UTF-16 code units via `unicode/utf16.Encode`, serialises each code unit in big-endian order (`byte(c>>8), byte(c)`), and takes SHA-256.
  - `userPasswordHash` uppercases the user name with Go's `strings.ToUpper` before calling `keyPasswordHash`. This matches Java's `toUpperCase(Locale.ENGLISH)` for ASCII; documented in the doc comment. Returns a zero-length slice for empty user + empty password (matching H2's `hashPassword` fast path).
  - `filePasswordHash` returns nil for empty file passwords. For non-empty passwords it calls `keyPasswordHash("file", filePassword)` — **the "file" prefix is deliberately not uppercased**, matching Java's `ConnectionInfo.convertPasswords` which passes literal `"file"` to `hashPassword` without going through `setUserName`.
  - `auth_test.go` contains 14 tests: 8 user-password tests (empty, 5 golden vectors, uppercase normalisation, different passwords, length, deterministic), 5 file-password tests (empty→nil, 2 golden vectors, not-uppercased, deterministic), and 1 byte-layout test that verifies the raw UTF16-BE encoding of `"SA@"` before hashing.
  - Test count: 105 total (32 DSN + 57 Transfer + 14 Auth + 2 error tests). All pass.
  - Verified: `go build`, `go vet`, `go test`, `make lint` all pass. ✅
- **Status:** ✅ Done — 2026-07-08

### T3.2 Handshake exchange
- **Goal:** Establish an authenticated session over TCP for protocol 21.
- **Work:** Reference `TcpServerThread.run` handshake + `SessionRemote.connectServer`.
  - `handshake.go`: dial TCP, write min=max=`21`, database name, original URL, username, user password hash, file password hash, and connection properties (count + key/value strings). Do not invent client-sent protocol-21 trailing fields; H2 2.4.240 creates network connection info server-side.
  - Read status; on `STATUS_ERROR` decode into a structured error (defer full decoding to Phase 7 but capture code/message/SQLState now).
  - Read negotiated protocol version; **fail clearly** if not `21` ("unsupported H2 server version; require protocol 21 / H2 2.4.240+").
  - Generate a client session id, then send `SESSION_SET_ID`, the session id, and a timezone id (required for protocol 20+); read status and the server autocommit boolean.
- **Deliverables:** `handshake.go`, `session.go` (holds `transfer`, session id, negotiated version, autocommit state).
- **Done when:** Unit test drives the handshake against a scripted in-memory server (recorded byte sequence) and asserts correct framing + version-mismatch rejection.
- **Implementation notes:**
  - `session.go`: `Session` struct holds `*Tr`, `id` (hex string), `version` (int32), `autoCommit` (bool). Provides `Close()`.
  - `handshake.go`: `Handshake(cfg *Config) (*Session, error)` dials TCP via `net.JoinHostPort`, wraps the connection in `Tr` via `NewReadWriter`.
  - The handshake sequence follows `SessionRemote.initTransfer` and `TcpServerThread.run` exactly:
    1. Write `minVer=21, maxVer=21, dbName, originalURL, uppercasedUser, userPwHash, filePwHash(nil), nProps=0`.
    2. Flush; read status. On `StatusError`, call `readH2Error` and return.
    3. Read negotiated version; fail clearly if not 21.
    4. Write `SessionSetID`, 64-char hex `sessionID` (32 random bytes, matching Java `MathUtils.secureRandomBytes(32)`), and `localTimeZoneID()` (checks `TZ` env → `time.Local.String()` → `"UTC"` fallback).
    5. Flush; read status. On error, read and return `H2Error`.
    6. Read `autoCommit` boolean.
  - `errors.go`: Minimal `H2Error` struct (SQLState, Message, SQL, Code, StackTrace) and `readH2Error(tr)` to decode the `STATUS_ERROR` wire format.
  - User name is uppercased with `strings.ToUpper` before sending (matching `ConnectionInfo.setUserName` + `StringUtils.toUpperEnglish` for ASCII). Verified by wire-format test.
  - `handshake_test.go`: 7 tests + `TestSendCredentials_WireFormat` + `TestGenerateSessionID`. Tests use real TCP listeners (`net.Listen("tcp", "127.0.0.1:0")`) because `Handshake` dials itself.
    - `TestHandshake_Success`: full round-trip, asserts all wire values and session state.
    - `TestHandshake_VersionMismatch`: server returns version 20, client rejects with clear error.
    - `TestHandshake_AuthError`: server returns `StatusError`, client decodes into `*H2Error` with SQLState "28000".
    - `TestHandshake_ErrorAfterSessionSetID`: error flow after SESSION_SET_ID.
    - `TestHandshake_CorrectUserNameUppercasingOnWire`: verifies "root" in Config becomes "ROOT" on the wire.
  - Test count: 158 total (32 DSN + 57 Transfer + 14 Auth + 55 Handshake). All pass.
  - Verified: `go build`, `go vet`, `go test`, `make lint` all pass. ✅
- **Status:** ✅ Done — 2026-07-08

### T3.3 Connectivity integration test
- **Goal:** Real login against H2 2.4.240.
- **Work:**
  - `integration_test.go` (build tag `integration`): load `.env`, dial, handshake, assert session established, close cleanly.
  - Confirm the T3.1 hash vectors against the live server (auth success/failure).
- **Deliverables:** integration test + `.env` loader helper.
- **Done when:** With H2 running, the test connects and authenticates; without env, it skips.
- **Implementation notes:**
  - `integration_test.go` uses `//go:build integration` tag so it only builds/runs when explicitly requested.
  - `loadEnvFromFile` and `integrationEnv` helpers read `h2-data/.env` (walking `.`, `..`, `../..` from the test binary) as a fallback when process env vars are not set. This lets `go test -tags integration` work from the module root without pre-exporting variables.
  - `TestIntegration_Handshake`: parses the JDBC URL from `.env`, merges credentials, calls `Handshake`, asserts version=21, session id length=64, and autoCommit state. Confirmed against live H2 2.4.240: version=21, autoCommit=true.
  - `TestIntegration_AuthFailure`: uses deliberately wrong credentials, asserts `*H2Error` is returned (confirmed live — wrong password produces SQLState 28000).
  - `TestIntegration_ParseDSNRoundTrip`: verifies the `.env` JDBC URL parses correctly (host, port, database non-empty, OriginalURL preserved).
  - `TestIntegration_CredentialsMerge`: verifies `MergeCredentials` overlays env credentials only when DSN omits them, with DSN credentials taking precedence.
  - `TestIntegration_MultipleHandshakes`: 3 sequential independent handshakes to the same server — all succeed. Validates connection reuse is not required and each session is independent.
  - Integration test skips cleanly with `t.Skip` when env is unavailable.
  - Verified live against H2 2.4.240 on port 9092: all 5 integration tests pass. ✅
- **Status:** ✅ Done — 2026-07-08

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
- **Implementation notes:**
  - `driver.go`: `Driver` implements both `driver.Driver` (`Open`) and `driver.DriverContext` (`OpenConnector`). The driver is registered in `init()` under the name `"h2"`.
  - `NewConnector(cfg)` validates the config (host required, port defaults to 9092) and returns a `*connector`. `OpenDB(cfg)` is a convenience that creates a connector and opens a `*sql.DB`.
  - `connector.Connect(ctx)` checks for context cancellation before and after the handshake. It creates a `*conn` wrapping the established `*Session`.
  - `conn` embeds `*Session` and uses a `sync.Mutex` + `busy` flag to prevent concurrent use. `acquire()` returns `driver.ErrBadConn` if closed or already busy.
  - `conn.Close()` calls `sess.Close()` which implements the graceful SESSION_CLOSE protocol.
  - Prepare/Begin/BeginTx/PrepareContext return `ErrNotYetSupported` wrapped with context indicating which phase will implement them (Phase 6 for prepared statements, Phase 8 for transactions).
  - Tests: `driver_test.go` (12 tests) covers registration, invalid DSN handling, connector construction with defaults, context cancellation, and interface compliance.
  - Tests: `conn_test.go` (8 tests) covers the busy flag, double-acquire protection, closed connection handling, and not-yet-supported errors.
  - Integration tests: `TestIntegration_DriverOpenDB` uses `OpenDB(cfg)` to create a `*sql.DB`, get a raw connection via `db.Conn()`, and close it. `TestIntegration_DriverOpenDSN` uses `sql.Open("h2", dsn)` with credentials in the DSN to achieve the same.
  - Test count: 177 total (+22 new: 12 driver + 8 conn + 2 integration). All pass with `-race`.
  - Verified: `go build`, `go vet`, `make lint` (0 issues), `go test -race`, live integration tests pass. ✅
- **Status:** ✅ Done — 2026-07-08

### T4.2 Ping (interim) + Pinger
- **Goal:** `db.PingContext` round-trips.
- **Work:**
  - Implement `driver.Pinger` on `conn`. Interim implementation validates the live session with an existing lightweight protocol round-trip (for example `SESSION_HAS_PENDING_TRANSACTION`), then re-point it to real `SELECT 1` in Phase 5 (T5.3) once query execution exists.
  - Map dead connection to `driver.ErrBadConn`.
- **Deliverables:** `ping.go` (or method in `conn.go`).
- **Done when:** `db.PingContext` succeeds against live H2; returns error on a closed conn.
- **Implementation notes:**
  - Added `Ping(ctx context.Context) error` method to `conn` implementing `driver.Pinger`.
  - Uses `SESSION_HAS_PENDING_TRANSACTION` as a lightweight round-trip: send the operation code, flush, read status (expect `STATUS_OK`), read boolean result.
  - Any I/O error returns `driver.ErrBadConn` so the pool discards dead connections.
  - Uses `acquire()`/`release()` pattern for safe concurrent use; returns `ErrBadConn` for closed connections and error for busy connections.
  - Tests: `conn_test.go` — `TestConnPingClosedConnection` verifies `ErrBadConn` for nil session; `TestConnPingBusy` verifies error when connection is already busy.
  - Integration test: `TestIntegration_Ping` opens a `*sql.DB` and calls `PingContext` successfully against live H2.
  - Test count: 182 total (+5 new: 2 conn + 1 integration + Pinger interface assertion). All pass with `-race`.
  - Verified: `go build`, `go vet`, `make lint` (0 issues), `go test -race`, live integration tests pass including Ping. ✅
- **Status:** ✅ Done — 2026-07-08

### Phase 4 review
- **Review date:** 2026-07-08
- **Four bugs found and repaired:**
  1. **Bug A (critical) — `Ping` reads `ReadBool` (1 byte) instead of `ReadInt32` (4 bytes) for the `SESSION_HAS_PENDING_TRANSACTION` result** (`conn.go`): `TcpServerThread` responds with `writeInt(STATUS_OK)` + `writeInt(0 or 1)`. Reading with `ReadBool()` consumed only 1 of the 4 result bytes, leaving 3 stale bytes in the read buffer that would silently corrupt every subsequent protocol read on the same connection. Fixed to `ReadInt32()`. Regression test `TestConnPingRoundTrip` confirmed the failure under the old code and passes with the fix.
  2. **Bug B (minor) — `Ping` returns `driver.ErrBadConn` for `StatusOKStateChanged`** (`conn.go`): When the server's modification counter changes (e.g. a remote DDL), any response can carry `STATUS_OK_STATE_CHANGED = 3` instead of `STATUS_OK = 1`. The `if status != StatusOK` guard rejected this valid response. Fixed with a `switch` that accepts both `StatusOK` and `StatusOKStateChanged` as success.
  3. **Bug C (design) — `NewConnector` mutated the caller's `*Config` and stored the raw pointer** (`driver.go`): Port defaulting wrote `cfg.Port = DefaultTCPPortStr` directly on the caller's struct, and the connector stored the original pointer, so post-creation mutations to the caller's Config silently affected the connector. Fixed by shallow-copying the Config before any mutation or storage. Tests `TestNewConnectorDoesNotMutateCfg` and `TestNewConnectorCfgIsolated` pin the behaviour.
  4. **Bug D (test) — `TestDriverRegistration` passed even when the driver was not registered** (`driver_test.go`): An unknown-driver error from `sql.Open` was handled with `t.Logf` (non-fatal), so a missing `sql.Register` call would not fail the test. Fixed to `t.Fatalf`.
- **Test count after fixes:** 185 total (+5 new regression tests: `TestConnPingRoundTrip`, `TestNewConnectorDoesNotMutateCfg`, `TestNewConnectorCfgIsolated`, and two new assertions in `TestNewConnectorDefaultPort`/`TestDriverRegistration`). All pass with `-race`.
- **Verified:** `go build`, `go vet`, `make lint` (0 issues), `go test -race`, 8 integration tests pass. ✅

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
- **Implementation notes:**
  - `typeinfo.go`: `TypeInfo` struct captures H2 column/parameter type metadata: `ValueType` (from protocol constants), `Precision`, `Scale`, `Nullable`, and `ExtTypeInfo` for complex types (ARRAY element type). Protocol-21 `ReadTypeInfo()` decoder handles all 25+ MVP types with correct precision/scale reading per type category:
    - Simple types (NULL, BOOLEAN, integer types, DATE, UUID): no additional fields.
    - String/binary types (CHAR, VARCHAR, BINARY, etc.): read int32 precision.
    - LOB types (BLOB, CLOB): read int64 precision.
    - NUMERIC: read int32 precision, int32 scale, boolean has-ext-flag.
    - Floating point (REAL, DOUBLE): read byte precision (-1 = default).
    - Time/timestamp types: read byte scale (fractional seconds precision).
    - ARRAY: read precision + recursive element type info.
    - Complex types (ENUM, GEOMETRY, ROW): ext type info parsed/skipped for MVP.
  - `metadata.go`: `ResultColumn` and `ResultMeta` structs for result set metadata. `ReadResultMeta()` reads alias, schema, table, column name, TypeInfo, identity flag, and nullable. Handles protocol < 20 displaySize skip. Helper methods: `ColumnNames()`, `GetColumn()`, `GetColumnByName()`.
  - `command.go`: `PreparedCommand` struct holds command ID, SQL, IsQuery, ReadOnly, CmdType, ParamCount, and Params slice. `Session.PrepareCommand()` sends `SESSION_PREPARE` (op=0) and reads isQuery/readOnly/paramCount. `Session.PrepareCommandReadParams()` sends `SESSION_PREPARE_READ_PARAMS2` (op=18) and additionally reads cmdType and per-parameter TypeInfo+nullable. `PreparedCommand.Close()` sends `COMMAND_CLOSE` to release server-side command. Added `nextCommandID()` with mutex for thread-safe ID generation. Command type constants (CmdSelect=1, CmdInsert=2, etc.) from CommandRemote.
  - `session.go`: Added `mu sync.Mutex` and `nextID int32` for command ID generation.
  - `protocol.go`: Added `TCPProtocolVersion20 = 20` constant for protocol version checks.
  - Test coverage: `typeinfo_test.go` (8 tests for TypeInfo reading of various types), `command_test.go` (8 tests for command structures and ID generation), `metadata_test.go` (10 tests for result metadata reading). Total tests: 211 (previous 185 + 26 new). All pass with `-race`.
  - Verified: `go build ./...`, `go vet ./...`, `make lint` (0 issues), `go test -race`, `make test-integration` (8 integration tests pass against H2 2.4.240). ✅
- **Status:** ✅ Done — 2026-07-08

### T5.2 Rows: fetch, decode scalars, close
- **Goal:** Execute a query and stream rows into `driver.Rows`.
- **Work:** Reference `COMMAND_EXECUTE_QUERY` + `RESULT_FETCH_ROWS` + `Transfer.readValue`.
  - `rows.go`: `driver.Rows` with `Columns()`, `Next()` (fetch in batches, request more via `RESULT_FETCH_ROWS`), `Close()` (send `RESULT_CLOSE` and free the remote result), return `io.EOF` at end.
  - `value_read.go`: decode wire values for the **MVP scalar set** (NULL, BOOLEAN, TINYINT/SMALLINT/INTEGER/BIGINT→int64, REAL/DOUBLE→float64, CHAR/VARCHAR/VARCHAR_IGNORECASE→string, BINARY/VARBINARY→[]byte, DATE/TIME/TIME_TZ/TIMESTAMP/TIMESTAMP_TZ→time.Time, NUMERIC→string, UUID→canonical string). All results are one of the permitted `driver.Value` types (PRD §7.14).
  - Implement H2 date/time decoding from its wire representation (`dateValue`, nanoseconds-of-day, and timezone offset seconds for protocol 21), rather than assuming Unix timestamps.
  - Ensure `Rows.Close` frees the remote result; close the remote command too only when the rows object owns an ad-hoc command, not when it is backed by a reusable prepared statement.
- **Deliverables:** `rows.go`, `value_read.go`, unit tests (scripted rows) + type decode tests.
- **Done when:** Unit tests decode each MVP scalar; rows close frees server objects.
- **Implementation notes:**
  - `value_read.go`: `ReadValue()` decodes H2 wire values to `driver.Value` types per protocol 21:
    - NULL → nil
    - BOOLEAN → bool
    - TINYINT/SMALLINT/INTEGER/BIGINT → int64 (smallint uses ReadInt16 per protocol 20+)
    - REAL → float64 (reads float32, converts to float64)
    - DOUBLE → float64
    - NUMERIC → string (H2 sends as string)
    - VARCHAR/CHAR/VARCHAR_IGNORECASE → string
    - BINARY/VARBINARY → []byte
    - DATE → time.Time (from dateValue = days since epoch)
    - TIME → time.Time (from nanoseconds since midnight)
    - TIME_TZ → time.Time with location (nanoseconds + offset seconds)
    - TIMESTAMP → time.Time (dateValue + nanoseconds)
    - TIMESTAMP_TZ → time.Time with location (dateValue + nanoseconds + offset)
    - UUID → canonical string "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
    - BLOB/CLOB → MVP returns error for fetch-on-demand LOBs (length == -1)
    - DECFLOAT → string
    - Complex types (ARRAY, ROW, ENUM, GEOMETRY, INTERVAL) → error for MVP
  - Date/time conversion uses H2's native representation:
    - dateValue = days since 1970-01-01 (proleptic Gregorian)
    - Time = nanoseconds since midnight
    - Timezone offset = seconds from UTC
  - `rows.go`: `Rows` struct implements `driver.Rows`:
    - `Columns()` returns column aliases from result metadata
    - `Next(dest)` reads next row into dest slice; returns `io.EOF` at end
    - Row fetching: initial rows prefetched on creation; additional rows fetched via `RESULT_FETCH_ROWS`
    - Wire row format: byte flag (1=row data, 0=end, -1=error) followed by column values
    - `Close()` sends `RESULT_CLOSE`; if `ownsCommand` is true, also sends `COMMAND_CLOSE`
    - `ownsCommand` is true for ad-hoc queries, false for prepared statement results (reusable)
    - `Next` returns `io.EOF` at end-of-result and on a closed result set (never `driver.ErrBadConn`, which would falsely mark the pool connection dead); fetch errors are recorded on `Rows.err` so they stay sticky across subsequent `Next` calls (re-review 2026-07-09).
  - Optional interface implementations for metadata:
    - `ColumnTypeDatabaseTypeName()` → type name (INTEGER, VARCHAR, etc.)
    - `ColumnTypeNullable()` → nullability
    - `ColumnTypeLength()` → precision for variable-length types
    - `ColumnTypePrecisionScale()` → precision/scale for numeric types
  - Execution helpers on Session:
    - `ExecuteQuery()` for ad-hoc queries (prepares, executes, owns command)
    - `ExecuteQueryPrepared()` for prepared commands (reuses command)
  - `transfer.go`: Added `ReadFull()` helper for reading exact byte counts (used for BLOB data).
  - Test coverage: `value_read_test.go` (11 tests for all value types), `rows_test.go` (10 tests for rows behavior). Total tests: 231 (+20 new from T5.1 baseline of 211, though plan shows 185; actual count is 231). All pass with `-race`.
  - Verified: `go build ./...`, `go vet ./...`, `make lint` (0 issues), `go test -race`, `make test-integration` (8 integration tests pass against H2 2.4.240). Note: T5.3 will add end-to-end integration tests for actual SELECT queries.
- **Status:** ✅ Done — 2026-07-08

### T5.3 QueryerContext + real Ping
- **Goal:** `db.QueryContext(ctx, "SELECT ...")` works end to end.
- **Work:**
  - Implement `driver.QueryerContext` on `conn` for parameterless queries initially. If args are supplied before Phase 6 parameter support is complete, return a clear unsupported error or `driver.ErrSkip` only where `database/sql` can legitimately fall back.
  - Re-point `Ping` to execute `SELECT 1` and drain the result.
  - Integration tests: `SELECT 1`, `SELECT` over a temp table with multiple rows/columns, batch-boundary fetch (result larger than one fetch block).
- **Deliverables:** query path in `conn.go`, integration tests.
- **Done when:** Live `SELECT` queries return correct typed values; large result sets paginate correctly.
- **Implementation notes:**
  - **Root cause of the T5.2/T5.3 hang**: Three concurrent protocol framing bugs discovered during investigation:
    1. `COMMAND_EXECUTE_QUERY` (rows.go): missing `writeInt(paramCount=0)` after `fetchSize`; server blocked in `setParameters()` waiting for the count.
    2. `COMMAND_EXECUTE_UPDATE` (session.go): missing `readInt(status)` before `readRowCount()`; client consumed the status int as the first 4 bytes of the row count.
    3. `RESULT_FETCH_ROWS` (rows.go): missing `readInt(STATUS_OK)` before reading row byte-flags; the STATUS_OK written by the server went unread.
    4. `SESSION_PREPARE` (command.go): status int was read but discarded without error checking; now checked via `readStatus()`.
  - Root pattern: every H2 operation follows flush → `session.done()` → response-payload reads. `session.done()` does flush + read-status in one call. Our Go code had been missing either the paramCount write or the status read on several paths.
  - Added `readStatus(tr *Tr) error` helper to errors.go: reads status int, returns nil for STATUS_OK/STATUS_OK_STATE_CHANGED, `*H2Error` for STATUS_ERROR, plain error for STATUS_CLOSED/unknown.
  - `conn.go`: added `QueryContext` (driver.QueryerContext) and `ExecContext` (driver.ExecerContext) to conn; returns `driver.ErrSkip` when args are non-empty so `database/sql` falls back to the Prepare path in Phase 6. Connection lock released via `closeCallback` on `Rows.Close()`.
  - `result.go`: `driver.Result` with `RowsAffected()` returning int64; `LastInsertId()` returns unsupported error (Phase 10).
  - `session.go`: `ExecuteUpdate(ctx, sql)` prepares + executes + closes ad-hoc update; `ExecuteUpdatePrepared(cmd)` for reusable prepared commands.
  - Ping re-pointed to `SELECT 1`: executes a full query round-trip and drains the result, proving the connection is alive.
  - Integration tests added: `TestIntegration_QueryContext` (SELECT 1), `TestIntegration_QuerySelect` (5-row people table), `TestIntegration_ExecContext` (CREATE/INSERT/UPDATE/DELETE/DROP), `TestIntegration_QueryLargeResult` (250 rows, 3 fetch batches).
  - Also incorporates ExecerContext + result.go work originally planned as T6.2 (since it was naturally implemented alongside QueryerContext).
  - Test count: 249 total. All pass with `-race`. 13 integration tests pass against H2 2.4.240.
  - Verified: `go build ./...`, `go vet ./...`, `make lint` (0 issues), `go test -race`, `make test-integration` all green.
- **Status:** ✅ Done — 2026-07-08

### Phase 5 review
- **Review date:** 2026-07-08
- **Nine bugs found and repaired:**
  1. **Bug A (critical) — `dateValueToTime` treated H2's packed date as days-since-epoch** (`value_read.go`): H2 stores dates as `(year << 9) | (month << 5) | day` (confirmed via `DateTimeUtils.SHIFT_YEAR=9`, `SHIFT_MONTH=5`). The implementation used a wrong arithmetic formula based on days since epoch, producing completely wrong date values for any DATE or TIMESTAMP column. Fixed to use bit-unpacking: `day = dv & 0x1F`, `month = (dv >> 5) & 0x0F`, `year = dv >> 9`. Added helper `unpackDateValue`. Regression tests `TestReadValue_Date_Epoch` (1970-01-01 using H2's `EPOCH_DATE_VALUE = 1008673`) and `TestReadValue_Date_Known` pin correct decoding.
  2. **Bug B (bug) — `timestampToTimeTZ` and `nanosOfDayToTimeTZ` applied timezone as display-only** (`value_read.go`): H2 TIMESTAMP WITH TIME ZONE and TIME WITH TIME ZONE wire values carry *local* time (dateValue + nanos represent the wall-clock time in the given zone), not UTC. The old code decoded dateValue+nanos as UTC and then called `.In(loc)` which only changes the display timezone without correcting the UTC instant. Fixed to call `time.Date(year, month, day, h, m, s, ns, loc)` directly, so Go's `time.Time` carries both the correct wall-clock local representation and the correct UTC instant. Regression tests `TestReadValue_TimestampTZ_LocalTime` and `TestReadValue_TimeTZ_LocalTime` confirm correct UTC and local time.
  3. **Bug C (critical) — TI constants `TIJavaObject` through `TIIntervalMinSec` were all wrong** (`typeinfo.go`): The TI code table in `typeinfo.go` had a systematic offset error starting at `TIJavaObject = 18` (H2 uses 19) through `TIIntervalMinSec = 36` (H2 uses 38), caused by missing the gap at TI code 18 and again at 23. Consequence: TI code 24 (H2's TIMESTAMP_TZ) was mapped to `ValueTypeInterval`, and TI code 22 (H2's GEOMETRY) was mapped to `ValueTypeTimestampTZ`. Corrected all 13 affected constants: `TIJavaObject=19`, `TIUUID=20`, `TIChar=21`, `TIGeometry=22`, `TITimestampTZ=24`, `TIEnum=25`, intervals 26–38. Test `TestTIConstants_MatchH2` pins all corrected values.
  4. **Bug D (bug) — `ReadTypeInfo` for intervals always read 2 bytes** (`typeinfo.go`): H2 writes a scale byte only for fractional-second interval types (INTERVAL SECOND, DAY TO SECOND, HOUR TO SECOND, MINUTE TO SECOND). All other interval types write only a leading-precision byte. The implementation always read both bytes, consuming a stray byte for non-fractional-second intervals and corrupting the stream. Fixed by adding `intervalHasFractionalSeconds(ti int) bool` that checks the corrected TI code, and reading the scale byte only when that returns true. Regression tests `TestReadTypeInfo_IntervalYear_OneByte` and `TestReadTypeInfo_IntervalSecond_TwoBytes` pin the correct behaviour.
  5. **Bug E (minor) — `PrepareCommandReadParams` missing `readStatus`** (`command.go`): TcpServerThread sends `writeInt(getState(old))` (the status code) before the response payload for both `SESSION_PREPARE` and `SESSION_PREPARE_READ_PARAMS2`. `PrepareCommand` already called `readStatus`; `PrepareCommandReadParams` was missing it entirely, so the 4-byte status integer would have been consumed as the first byte of `isQuery`. Fixed to call `readStatus` before reading `isQuery`. Regression test `TestPrepareCommandReadParams_ReadsStatus` uses a net.Pipe mock server.
  6. **Bug F (minor) — `ExecuteQueryPrepared` missing `readStatus`** (`rows.go`): The same pattern — TcpServerThread sends status before `columnCount` for `COMMAND_EXECUTE_QUERY`. `ExecuteQuery` already read the status; `ExecuteQueryPrepared` jumped directly to `ReadInt32()` for column count, consuming the status integer. For single-column queries (STATUS_OK=1), this accidentally gave the right answer; for 2+ columns it would give wrong column count. Fixed with a `readStatus` call before `ReadInt32`. Regression test `TestExecuteQueryPrepared_ReadsStatus` uses a 2-column mock to expose the failure mode.
  7. **Bug G (minor) — `ExecuteQueryPrepared` missing paramCount write** (`rows.go`): TcpServerThread's `setParameters()` always reads a paramCount int before parameters. `ExecuteQuery` explicitly wrote `WriteInt32(0)` for paramCount; `ExecuteQueryPrepared` did not. This would cause the server's `setParameters` to hang waiting for the count, deadlocking the protocol. Fixed by adding `WriteInt32(0)` before `Flush`. Caught by the same regression test.
  8. **Bug H (minor) — `lobMagic = 0xFACE` wrong** (`value_read.go`): H2 Transfer.java defines `LOB_MAGIC = 0x1234`. Any inline LOB read would fail the magic check. Fixed to `0x1234`.
  9. **Dead code removed** (`value_read.go`): `daysToDate`, `boolInt`, and `isLeapYear` were never called (artefacts of the incorrect days-since-epoch approach). Removed.
  10. **Latent edge-case: `noMoreRows` guard** (`rows.go`): When the server sends end-of-result flag (byte 0) during the initial fetch batch, the result is immediately closed server-side. For queries where the server-reported row count is not known (lazy result, `rowCount = Long.MAX_VALUE`), subsequent calls to `Next` would send a `RESULT_FETCH_ROWS` request for an already-closed result. Fixed by adding `noMoreRows bool` to `Rows`; `fetchRows` sets it on flag=0; `Next` checks it before calling `fetchMoreRows`. Test `TestRows_NoMoreRows_PreventsExtraFetch` pins the guard.
- **Test count after fixes:** 267 total (+18 regression tests). All pass with `-race`. 13 integration tests pass. ✅
- **Verified:** `go build`, `go vet`, `make lint` (0 issues), `go test -race`, `make test-integration` all green. ✅

### Phase 5 re-review
- **Review date:** 2026-07-09
- **Method:** Re-audited every Phase 5 wire path (`command.go`, `typeinfo.go`, `metadata.go`, `rows.go`, `value_read.go`, `session.go`, `errors.go`) line-by-line against the H2 2.4.240 reference source (`h2-src/org/h2/value/Transfer.java`, `server/TcpServerThread.java`, `result/ResultColumn.java`, `expression/ParameterRemote.java`, `util/DateTimeUtils.java`, `engine/SessionRemote.java`).
- **Cross-checks that confirmed the prior fixes are correct (no change needed):**
  - Value wire codes vs TI codes are two distinct code spaces. Verified `value_read.go`/`protocol.go` use Transfer's value constants (NULL=0…DECFLOAT=31) while `typeinfo.go` uses the TI table codes (ROW=39, JSON=40, TIME_TZ=41, BINARY=42, DECFLOAT=43). Both mappings match the H2 static `addType(...)` table exactly.
  - `readTypeInfo20` field-by-field layout (precision as int32 vs int64 vs byte-with-default; NUMERIC ext-flag; interval leading precision byte + conditional fractional-seconds scale byte; ARRAY recursion; ENUM/GEOMETRY/ROW ext info) matches `Transfer.writeTypeInfo20`.
  - `ReadResultMeta` field order matches `ResultColumn` (alias, schema, table, column, TypeInfo, [displaySize if <20], identity, nullable).
  - Query/update framing matches `TcpServerThread`: `SESSION_PREPARE`/`_READ_PARAMS2` (status, isQuery, readOnly, [cmdType], paramCount, [param meta]); `COMMAND_EXECUTE_QUERY` (paramCount write; status, columnCount, rowCount, columns, first batch); `RESULT_FETCH_ROWS` (status then rows); `COMMAND_EXECUTE_UPDATE` (paramCount + generated-keys mode; status, updateCount, autoCommit, and no generated-keys frame when mode=NONE); `RESULT_CLOSE`/`COMMAND_CLOSE` send no response.
  - `unpackDateValue` matches `DateTimeUtils` (`SHIFT_YEAR=9`, `SHIFT_MONTH=5`, day mask 0x1F, month mask 0x0F) for the supported year range.
  - `STATUS_OK_STATE_CHANGED` consumes no extra wire bytes (confirmed in `SessionRemote.done()`), so treating it like `STATUS_OK` for framing is correct.
- **Two flaws found and repaired (`rows.go`):**
  1. **Bug (pool safety) — `Rows.Next` returned `driver.ErrBadConn` on a closed result set.** Returning `ErrBadConn` from `Next` signals a dead connection to `database/sql`, which can trigger a spurious whole-query retry on another connection. A closed result simply has no more rows. Fixed to return `io.EOF`. Regression test `TestRows_Closed` updated to assert `io.EOF`.
  2. **Bug (latent) — the `Rows.err` guard was never armed.** `Next` began with `if r.err != nil { return r.err }`, but no code path ever set `r.err`, so a mid-stream `fetchMoreRows` failure (misaligned read stream) was not sticky: a second `Next` would issue another `RESULT_FETCH_ROWS` against a corrupt stream. Fixed to record `r.err = err` on `fetchMoreRows` failure. Regression test `TestRows_Next_StickyError` pins the behaviour.
- **Live validation added (2 new integration tests):** The prior review fixed several decode bugs (Bug A DATE unpacking, Bug B TIMESTAMP_TZ local-time, Bug H LOB magic) but no live test exercised those paths. Added `TestIntegration_ScalarTypeDecoding` (BOOLEAN, TINYINT/SMALLINT/INTEGER/BIGINT, REAL, DOUBLE, NUMERIC, VARCHAR, VARBINARY, DATE, TIME, TIMESTAMP, TIMESTAMP WITH TIME ZONE, UUID — asserting `2021-03-14` decodes correctly and the `+05:00` timestamp resolves to the correct UTC instant `2021-03-14 10:09:26.535Z`) and `TestIntegration_NullDecoding` (NULL→nil across INTEGER/VARCHAR/TIMESTAMP). Both pass against live H2 2.4.240, directly confirming the earlier review fixes.
- **Test count after re-review:** 268 unit tests (+1 sticky-error regression; `TestRows_Closed` modified in place) and 15 integration tests (+2). All pass with `-race`.
- **Verified:** `go build`, `go vet`, `golangci-lint run` (0 issues), `go test -race`, and `go test -tags integration -race` (15/15 live against H2 2.4.240) all green. ✅

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
- **Implementation notes:**
  - Added `value_write.go` with `(*Tr).WriteValue(v driver.Value, paramType *TypeInfo)` mirroring H2 `Transfer.writeValue` for the MVP set: `nil`, `bool`, `int64`, `float64`, `string`, `[]byte`, `time.Time`.
  - Wire mappings implemented:
    - `nil` → `ValueTypeNull`
    - `bool` → `ValueTypeBoolean`
    - `int64` → `ValueTypeBigint`
    - `float64` → `ValueTypeDouble` (or `ValueTypeReal` when parameter metadata says REAL)
    - `string` → `ValueTypeVarchar` by default
    - `[]byte` → `ValueTypeVarbinary` by default (`ValueTypeBinary` when metadata says BINARY)
    - `time.Time` → temporal wire frames using H2 representation (`dateValue`, nanos-of-day, offset seconds), defaulting to `TIMESTAMP` when metadata is absent.
  - Added metadata-aware string encoding required by PRD §7.9:
    - For `NUMERIC`/`DECFLOAT` parameter metadata, strings are encoded as numeric wire values (not VARCHAR).
    - For `UUID` parameter metadata, string UUIDs are parsed and encoded as two `int64` words (`high`, `low`).
  - Added helper functions:
    - `packDateValue(year, month, day)` with H2 bit layout `(year<<9)|(month<<5)|day`
    - `nanosOfDay(time.Time)`
    - `parseUUIDString` (canonical 36-char with hyphens or compact 32-char hex)
  - Added `value_write_test.go` with round-trip tests through `ReadValue` for all MVP parameter types, metadata-driven NUMERIC/UUID behavior, temporal encoding by parameter type (`DATE`, `TIME`, `TIME_TZ`, `TIMESTAMP`, `TIMESTAMP_TZ`), typed-nil `[]byte`, unsupported-type errors, and UUID parser coverage.
  - Verification: `go build ./...`, `go vet ./...`, `go test -race ./...` all pass. ✅
- **Status:** ✅ Done — 2026-07-09

### T6.2 ExecerContext and Result
- **Goal:** `db.ExecContext` performs updates and returns affected rows.
- **Work:** Reference `COMMAND_EXECUTE_UPDATE`.
  - `result.go`: `driver.Result` with `RowsAffected()` (int64 count from protocol 21) and `LastInsertId()` returning a documented unsupported/unavailable error for now (generated keys in Phase 10).
  - Implement `driver.ExecerContext` on `conn`; bind positional `?` params via T6.1.
  - Even before generated-key support, encode and send H2's generated-keys request mode as `GeneratedKeysMode.NONE` for every `COMMAND_EXECUTE_UPDATE`; protocol 21 expects this field.
  - Integration: `CREATE TABLE`, `INSERT`, `UPDATE`, `DELETE`, `MERGE` with affected-row assertions.
- **Deliverables:** `result.go`, exec path, integration tests.
- **Done when:** DDL/DML execute; affected counts correct.
- **Implementation notes:**
  - `conn.ExecContext` now supports positional parameters end-to-end (no `ErrSkip` for arg-bearing exec calls).
  - Added argument conversion helper `convertNamedValues` in `conn.go`:
    - Uses `driver.DefaultParameterConverter` so `database/sql` compatible primitives and `driver.Valuer` outputs are normalized.
    - Enforces positional-only semantics (`Name == ""`, ordinal continuity).
    - Accepts only MVP wire-supported converted types: `nil`, `bool`, `int64`, `float64`, `string`, `[]byte`, `time.Time`.
  - Added parameterized update execution path in `session.go`:
    - `ExecuteUpdateWithParams(ctx, sql, []driver.Value)` prepares with `SESSION_PREPARE_READ_PARAMS2` to obtain parameter metadata.
    - `ExecuteUpdatePreparedWithParams(cmd, params)` writes `COMMAND_EXECUTE_UPDATE`, parameter count, each value via `Tr.WriteValue(...)`, then generated-keys mode `GeneratedKeysNone`.
    - Validates parameter arity (`len(params)` equals prepared `ParamCount`) before writing to wire.
  - Existing parameterless path remains unchanged:
    - `ExecuteUpdate(ctx, sql)` still uses `SESSION_PREPARE` and delegates to `ExecuteUpdatePrepared(cmd)`.
    - `ExecuteUpdatePrepared(cmd)` now delegates to `ExecuteUpdatePreparedWithParams(cmd, nil)`.
  - Added/updated tests:
    - `conn_test.go`: updated parameterized exec behavior test (no `ErrSkip`), added `TestConvertNamedValues` coverage (happy path + named/ordinal/conversion failures).
    - `integration_test.go`: added `TestIntegration_ExecContextWithParams` validating INSERT/UPDATE/DELETE with positional params including metadata-sensitive values (`NUMERIC` string, `UUID` string, `TIMESTAMP WITH TIME ZONE`, `VARBINARY`) and row-count assertions.
  - Live verification against H2 2.4.240 confirms parameterized `ExecContext` works for DML and preserves expected typed values on read-back.
- **Status:** ✅ Done — 2026-07-09

### T6.3 Prepared statements
- **Goal:** Reusable `driver.Stmt` with param metadata.
- **Work:**
  - `stmt.go`: `driver.Stmt` (`NumInput`, `Exec`/`Query` deprecated shims), `driver.StmtExecContext`, `driver.StmtQueryContext`, `driver.ConnPrepareContext`.
  - `NumInput()` from prepared parameter count; `Close()` sends `COMMAND_CLOSE` to free the remote command.
  - Support repeated execution of one prepared statement.
- **Deliverables:** `stmt.go`, integration tests (prepare once, exec/query many; stmt close frees server command).
- **Done when:** Prepared exec + query work repeatedly; no leaked commands.
- **Implementation notes:**
  - Added `stmt.go` implementing:
    - `driver.Stmt` (`Close`, `NumInput`, legacy `Exec`/`Query` shims)
    - `driver.StmtExecContext`
    - `driver.StmtQueryContext`
  - `conn.Prepare` / `conn.PrepareContext` now create real server-side prepared commands via `SESSION_PREPARE_READ_PARAMS2` and return `*stmt`.
  - Statement parameter count is sourced from server metadata (`PreparedCommand.ParamCount`) and returned from `NumInput()`.
  - `Stmt.Close()` now sends `COMMAND_CLOSE` on the wire to free the remote command; close is idempotent.
  - Added parameterized prepared execution paths:
    - `Session.ExecuteQueryPreparedWithParams(ctx, cmd, maxRows, fetchSize, params)` in `rows.go`
    - Existing `ExecuteQueryPrepared(...)` now delegates to the new method with `nil` params.
    - (T6.2 path reused) `Session.ExecuteUpdatePreparedWithParams(...)` for prepared updates.
  - Parameter handling for statement calls uses shared `convertNamedValues` conversion:
    - positional-only placeholders (`?`) enforced,
    - conversion via `driver.DefaultParameterConverter`,
    - wire encoding via T6.1 `WriteValue` with parameter `TypeInfo`.
  - Connection single-flight guard preserved for prepared operations:
    - `StmtExecContext` acquires/releases around execution.
    - `StmtQueryContext` acquires and releases on `Rows.Close()` via `closeCallback`.
  - Tests added/updated:
    - `stmt_test.go` (interface compliance, `NumInput`, close idempotence, closed-statement error paths).
    - `conn_test.go` updated for implemented `Prepare`/`PrepareContext` behavior.
    - Integration: `TestIntegration_PreparedStatements` covers prepare-once, repeated exec/query, parameter binding, and statement close.
  - Verification: `go build ./...`, `go vet ./...`, `go test -race ./...`, `go test -tags integration ./...`, and `golangci-lint run` all green. ✅
- **Status:** ✅ Done — 2026-07-09

### T6.4 NamedValueChecker
- **Goal:** Custom arg conversion and clear rejection of named params.
- **Work:**
  - Implement `driver.NamedValueChecker` on `conn`/`stmt`: accept `driver.Valuer` (incl. `github.com/google/uuid.UUID`), map supported Go types, and reject named params with a clear error because the MVP supports positional `?` parameters only.
- **Deliverables:** conversion logic + tests.
- **Done when:** `uuid.UUID` and standard types pass; unsupported/named args rejected clearly.
- **Implementation notes:**
  - Added `CheckNamedValue(*driver.NamedValue) error` on both `conn` and `stmt`.
  - Introduced shared normalization helper in `conn.go` (`normalizeNamedValue`) used by both checkers and by `convertNamedValues`:
    - rejects non-empty `NamedValue.Name` with a clear positional-only error,
    - validates `Ordinal >= 1`,
    - converts via `driver.DefaultParameterConverter` (thereby accepting `driver.Valuer` values),
    - restricts accepted converted types to MVP wire-supported set: `nil`, `bool`, `int64`, `float64`, `string`, `[]byte`, `time.Time`.
  - `convertNamedValues` now reuses normalization and additionally enforces contiguous ordinal ordering (`1..N`) before execution.
  - Compile-time interface assertions added:
    - `var _ driver.NamedValueChecker = (*conn)(nil)`
    - `var _ driver.NamedValueChecker = (*stmt)(nil)`
  - Unit tests:
    - `TestConnCheckNamedValue` verifies `driver.Valuer` conversion (custom valuer type → string).
    - `TestConvertNamedValues` expanded to cover valuer happy path and named/ordinal/unsupported failures.
    - `stmt_test.go` now asserts `NamedValueChecker` compliance and tests `stmt.CheckNamedValue`.
  - Integration validation strengthened with `github.com/google/uuid`:
    - `TestIntegration_ExecContextWithParams` now passes `uuid.UUID` directly as a parameter and verifies round-trip UUID text in the database.
  - Module dependency added for integration validation: `github.com/google/uuid v1.6.0`.
  - Verification: `go build ./...`, `go vet ./...`, `go test -race ./...`, `go test -tags integration ./...`, `golangci-lint run` all green. ✅
- **Status:** ✅ Done — 2026-07-09

### Phase 6 review
- **Review date:** 2026-07-09
- **Method:** Line-by-line audit of all Phase 6 files (`value_write.go`, `session.go`, `rows.go`, `conn.go`, `stmt.go`, `conn_test.go`, `stmt_test.go`, `value_write_test.go`, `integration_test.go`) against the H2 2.4.240 source (`Transfer.writeValue`, `TcpServerThread.process`) and the `database/sql/driver` contract.
- **Cross-checks confirmed correct (no change needed):**
  - `WriteValue` wire encoding matches `Transfer.writeValue` exactly for all MVP types: NULL=0, BOOLEAN=1, BIGINT=5, DOUBLE=7, NUMERIC string, VARCHAR, VARBINARY, UUID two-int64, DATE/TIME/TIMESTAMP/TIMESTAMP_TZ/TIME_TZ date+nanos+offset layout.
  - `packDateValue` uses `(year<<9)|(month<<5)|day` — symmetric with `unpackDateValue`; arithmetic shifts handle negative (BCE) years correctly.
  - `nanosOfDay` uses the wall-clock representation of the `time.Time` for both TIMESTAMP (local wall clock as UTC-like on the wire, consistent with JDBC behaviour) and TIMESTAMP_TZ (local wall clock + offset). This matches H2's `ValueTimestamp.getDateValue()` / `getTimeNanos()` semantics.
  - `ExecuteUpdatePreparedWithParams` sends paramCount + each value encoded with metadata, then `GeneratedKeysNone` — matches `CommandRemote.sendParameters` + `TcpServerThread.setParameters`.
  - `convertNamedValues` and `normalizeNamedValue` correctly validate positional-only args and reject named params with a clear error.
  - `stmt.ExecContext`/`QueryContext` release the connection lock via `closeCallback`/`defer c.release()` — no lock leak.
- **Two bugs found and repaired:**
  1. **Bug (functional inconsistency) — `conn.QueryContext` returned `driver.ErrSkip` for all parameterised queries** (`conn.go`): The stale T5.3 comment and `ErrSkip` return were never removed when T6.2 added inline parameterised exec and T6.3 added prepared statements. With `NamedValueChecker` now implemented, returning `ErrSkip` causes `database/sql` to create a temporary `Stmt` (via `PrepareContext`) on every parameterised `db.QueryContext` call — correct but wasteful (extra Prepare+Close round-trip). Fixed by implementing the inline path in `QueryContext`, mirroring `ExecContext`, using the new `Session.ExecuteQueryWithParams` method. Regression test `TestConnQueryContextWithArgs` updated to assert no `ErrSkip` is returned.
  2. **Bug (latent leak) — `stmt.Close()` returned a hard error and abandoned the server command when the connection was busy** (`stmt.go`): `s.closed` was set to `true` before `c.acquire()` was called, so if acquire failed (e.g. rows still open on the same connection), the command ID was discarded client-side but the server command was never freed. Fixed with best-effort semantics: if acquire fails, `Close()` returns `nil` and the server command is reclaimed when the session eventually closes (H2 GC behaviour).
- **Code quality improvement — eliminated COMMAND_EXECUTE_QUERY wire duplication** (`rows.go`): The ~50-line COMMAND_EXECUTE_QUERY wire-write + response-read block was identical in `ExecuteQuery`, `ExecuteQueryPreparedWithParams`, and the new `ExecuteQueryWithParams`. Extracted into a private helper `executeQueryWire(ctx, cmd, maxRows, fetchSize, params, ownsCommand)`. All three entry points now delegate to it; the `ownsCommand` flag controls whether `Rows.Close()` will also send `COMMAND_CLOSE`.
- **Live validation:** Added `TestIntegration_QueryContextWithParams` (direct `db.QueryContext` with `?` args, verifying inline path is used and results are correct). 18/18 integration tests pass against live H2 2.4.240.
- **Test count after review:** 300 unit tests (+1 updated regression). All pass with `-race`.
- **Verified:** `go build ./...`, `go vet ./...`, `golangci-lint run` (0 issues), `go test -race ./...`, `go test -tags integration -race ./...` (18/18 live) all green. ✅

### Phase 6 re-review / critical repair check
- **Review date:** 2026-07-09
- **Method:** Fresh-context parallel reviewer pass plus parent re-audit of Phase 6 code paths (`conn.Ping`, `stmt` lifecycle/locking, `Rows.Close` callback contract, `WriteValue` numeric metadata encoding). The review explicitly checked: protocol framing, parameter metadata use, prepared statement close/reuse, `database/sql/driver` error classification, and regression coverage.
- **Three flaws found and repaired:**
  1. **Bug (pool safety) — `Ping` mapped all query failures to `driver.ErrBadConn` and ignored `Rows.Close` errors** (`conn.go`). A busy connection, canceled context, or server SQL error is not necessarily a dead socket/session, and swallowing close errors could report success after a failed result/command close. Fixed `Ping` to execute `SELECT 1`, drain rows, preserve the original operation error, and return `Rows.Close()` errors. Closed sessions still return `driver.ErrBadConn` via `acquire()`. Regression test `TestConnPingBusy` now asserts busy ping is not `ErrBadConn`.
  2. **Bug (prepared statement lifecycle/race) — `stmt.Close()` marked the statement closed and nilled `cmd`/`conn` before it knew whether `COMMAND_CLOSE` was sent** (`stmt.go`). If the connection was busy, the server command ID was abandoned; if `Close` raced between `snapshotOpen` and connection acquisition, an exec/query could run against a command already closed on the server. Reworked `stmt` state handling: operation startup acquires the connection while holding the statement mutex, `Close` blocks new operations immediately, defers `COMMAND_CLOSE` when an in-flight operation owns the connection, and `Rows.Close()` / exec completion performs the deferred close before releasing the connection. `Rows.closeCallback` now returns an error so deferred close failures propagate. Regression tests `TestStmtCloseSendsCommandClose` and `TestStmtCloseWhileBusyDefersCommandClose` pin both direct and deferred close behavior.
  3. **Bug (parameter encoding) — NUMERIC/DECFLOAT metadata was honored only for string inputs** (`value_write.go`). `database/sql` numeric inputs converted to `int64`/`float64` were encoded as BIGINT/DOUBLE even when H2 parameter metadata requested NUMERIC/DECFLOAT, which could lose exact decimal semantics or rely on server-side coercion. Fixed `WriteValue` to encode `int64` and `float64` as the H2 numeric/decfloat string wire format when metadata requests those types (`FormatInt`, `FormatFloat('g', -1, 64)`). Regression test `TestWriteValue_NumericTypeInfo` now covers string, int64, and float64 for NUMERIC/DECFLOAT.
- **Review check added:** Phase 6 review now has explicit regression tests for every accepted finding: `TestConnPingBusy`, `TestStmtCloseSendsCommandClose`, `TestStmtCloseWhileBusyDefersCommandClose`, and `TestWriteValue_NumericTypeInfo`.
- **Verified after repairs:** `go test ./...`, `go vet ./...`, `go test -race ./...`, `golangci-lint run` (0 issues), and `go test -tags integration -race ./...` against a freshly started local H2 2.4.240 server all pass. ✅

---

## Phase 7 — Error handling

### T7.1 Structured H2 errors
- **Goal:** Rich, typed SQL errors.
- **Work:** Reference `TcpServerThread` error frame + `DbException`.
  - `errors.go`: `type Error struct { SQLState string; Message string; SQL string; Code int32; StackTrace string }` implementing `error` and `fmt.Formatter`; `type H2Error = Error` kept as a compatibility alias for existing tests/callers.
  - Sentinel errors: `ErrUnsupportedServerVersion`, `ErrUnsupportedType`, `ErrClosed`.
  - Preserve SQLState, vendor code, original message, SQL text, and trace fields, but keep server trace out of the default `Error()` string. `%+v` formatting appends the trace for debug output.
  - `readH2Error` decodes the full H2 `STATUS_ERROR` payload into `*Error`; `wrapError` preserves H2 server errors unchanged so command/handshake callers can still type-assert them directly.
  - Handshake now uses `ErrUnsupportedServerVersion` for protocol mismatch; `readStatus` uses `ErrClosed` for session-closed responses; unsupported type decoding/encoding paths use `ErrUnsupportedType`.
- **Deliverables:** `errors.go`, `errors_test.go`.
- **Implementation notes:**
  - Added `TestReadH2Error` to pin field decoding plus `Error()` / `%+v` formatting.
  - Added `TestReadStatusClosedReturnsErrClosed`, `TestWrapErrorPreservesH2Error`, and sentinel coverage for the exported error values.
  - Retrofitted `PrepareCommand`, `PrepareCommandReadParams`, `Rows.fetchMoreRows`, `executeQueryWire`, and `ExecuteUpdatePreparedWithParams` to use `wrapError`, so `*Error` values are preserved instead of being hidden behind extra context wrapping.
  - Integration and handshake tests now assert `ErrUnsupportedServerVersion` for protocol mismatch and `ErrUnsupportedType` for unsupported value round-trips.
- **Done when:** Server errors expose SQLState + H2 code; unit tests decode a scripted error frame; integration test asserts a real syntax error surfaces code/state.
- **Status:** ✅ Done — 2026-07-09

### Phase 7 review
- **Review date:** 2026-07-09
- **Method:** Fresh-context parallel reviewer pass (two independent angles). Reviewed `errors.go`, `errors_test.go`, `handshake.go`, `conn.go`, `value_read.go`, `value_write.go`, `command.go`, `rows.go`, `session.go`, `integration_test.go` against IMPLEMENTATION_PLAN.md and PRD §7.13/§8.2.
- **Four flaws found and repaired:**
  1. **Bug (type visibility) — `ExecuteQuery` and `ExecuteQueryWithParams` wrapped server `*Error` values in `fmt.Errorf`** (`rows.go:423`, `rows.go:446`). A SQL error from `PrepareCommand`/`PrepareCommandReadParams` (already a bare `*Error` thanks to `wrapError` inside those functions) was then immediately re-wrapped with `fmt.Errorf("...prepare failed: %w", err)`, burying the `*Error` so callers could no longer do `err.(*Error)` directly. Fixed both sites to use `wrapError("...", err)` so the server error is returned unchanged. Verified with the new `TestIntegration_SQLError` test: `errors.As` and direct type assertion both work.
  2. **Gap (test coverage) — no unit test for `readStatus` + `STATUS_ERROR` path** (`errors_test.go`). The `TestReadH2Error` test called `readH2Error` directly; no test exercised `readStatus(buf)` on a `STATUS_ERROR` frame to confirm the `*Error` comes back bare (not wrapped). Added `TestReadStatus_StatusError` which writes a full STATUS_ERROR frame, calls `readStatus`, and asserts both `errors.As` and direct `err.(*Error)` work.
  3. **Gap (test coverage) — missing integration test for real SQL syntax error** (`integration_test.go`). T7.1's "done-when" required an integration test asserting a real syntax error surfaces SQLState/Code; none existed. Added `TestIntegration_SQLError`: executes deliberately invalid SQL, asserts `errors.As(err, &h2err)` succeeds and `h2err.SQLState` / `h2err.Code` are non-zero. Confirmed live against H2 2.4.240 (SQLState=42000, Code=42000).
  4. **Cohesion — `localTimeZoneID` lived in `errors.go`** despite being a handshake timezone utility only used in `handshake.go`. Moved to `handshake.go`; removed the now-unnecessary `os` and `time` imports from `errors.go`.
- **Verified:** `go build ./...`, `go vet ./...`, `golangci-lint run` (0 issues), `go test -race ./...`, `go test -tags integration -race ./...` (20/20 live against H2 2.4.240) all green. ✅

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
- **Implementation notes:**
  - Added `tx` as a small driver-owned state object with `done` guarding double Commit/Rollback and `restoreIsolation` tracking whether a transaction requested a non-default isolation level.
  - `BeginTx` now validates read-only and isolation options up front, flips H2 autocommit off first, then applies requested isolation via `SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL ...` for supported levels (`READ UNCOMMITTED`, `REPEATABLE READ`, `SNAPSHOT`, `SERIALIZABLE`). Unsupported `WRITE COMMITTED` and `LINEARIZABLE` return a clear error.
  - `Commit` sends `COMMAND_COMMIT`; `Rollback` uses the SQL `ROLLBACK` path. On success they restore isolation (when needed) and autocommit back to `true` on the same raw H2 connection before returning. If the commit/rollback opcode itself fails, the driver now avoids a potentially unsafe autocommit flip so a later reset can clean up the session instead of accidentally committing a failed rollback.
  - Added unit coverage for session-closed begin, active-transaction rejection, read-only rejection, and unsupported-isolation rejection.
  - Added live integration coverage on a pinned `*sql.Conn`: commit persists changes, rollback discards them, and nested begin is rejected on the same physical connection.
- **Done when:** Commit/rollback behave correctly; autocommit restored afterward.
- **Status:** ✅ Done — 2026-07-09

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
