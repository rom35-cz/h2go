# Acceptance Criteria Traceability

This document maps PRD §10 acceptance criteria to the implemented test coverage in this repository.

## PRD §10 traceability

| PRD section | Criterion | Evidence |
|---|---|---|
| 10.1 | Driver connects to H2 2.4.240 in TCP server mode | `TestIntegration_Handshake` |
| 10.1 | Driver parses `jdbc:h2:tcp://localhost:9092/h2-go` correctly | `TestIntegration_ParseDSNRoundTrip` |
| 10.1 | Driver authenticates using `JDBC_USER` and `JDBC_PASSWORD` from environment | `TestIntegration_Handshake`, `TestIntegration_AuthFailure` |
| 10.1 | Driver rejects unsupported protocol versions with a clear error | `TestHandshake_VersionMismatch` |
| 10.2 | Driver works with `sql.Open` | `TestIntegration_DriverOpenDSN` |
| 10.2 | Driver works with `db.PingContext` | `TestIntegration_Ping` |
| 10.2 | Driver works with `db.QueryContext` | `TestIntegration_QueryContext`, `TestIntegration_QueryContextWithParams` |
| 10.2 | Driver works with `db.ExecContext` | `TestIntegration_ExecContext`, `TestIntegration_ExecContextWithParams` |
| 10.2 | Driver works with `db.PrepareContext` | `TestIntegration_PreparedStatements` |
| 10.2 | Driver works with `db.BeginTx` | `TestIntegration_TransactionCommit`, `TestIntegration_TransactionRollback` |
| 10.2 | Driver supports connection pooling without leaking open transactions or remote objects | `TestIntegration_ResetSessionRollsBackPendingTransaction`, `TestIntegration_ValidatorReportsLiveSession`, `TestIntegration_ConnectionPoolStress` |
| 10.3 | Can create a table | `TestIntegration_ExecContext`, `TestIntegration_ExecContextWithParams` |
| 10.3 | Can insert rows with parameters | `TestIntegration_ExecContextWithParams`, `TestIntegration_PreparedStatements` |
| 10.3 | Can query rows | `TestIntegration_QueryContext`, `TestIntegration_QuerySelect`, `TestIntegration_QueryLargeResult`, `TestIntegration_QueryContextWithParams`, `TestIntegration_PreparedStatements`, `TestIntegration_MaxRows` |
| 10.3 | Can scan common scalar values | `TestIntegration_ScalarTypeDecoding`, `TestIntegration_ScalarRoundTripTable`, `TestIntegration_NullDecoding`, `TestIntegration_TypeShowcaseFullSelect`, `TestIntegration_ComplexTypeDecoding` |
| 10.3 | Can update rows and return rows affected | `TestIntegration_ExecContext`, `TestIntegration_ExecContextWithParams` |
| 10.3 | Can delete rows and return rows affected | `TestIntegration_ExecContext`, `TestIntegration_ExecContextWithParams` |
| 10.3 | Can commit a transaction | `TestIntegration_TransactionCommit` |
| 10.3 | Can roll back a transaction | `TestIntegration_TransactionRollback` |
| 10.4 | Invalid SQL returns an H2 SQL error with SQLState and error code | `TestIntegration_SQLError` |
| 10.4 | Invalid credentials return a clear connection/authentication error | `TestIntegration_AuthFailure` |
| 10.4 | Unsupported types return clear errors or documented fallback values | `TestReadValue_UnsupportedType`, `TestWriteValue_UnsupportedType` |
| 10.4 | Context timeout returns a context-related error and leaves connection state safe | `TestDriverContextCancellation`, `TestSession_PrepareCommandContextTimeoutAbortsSession`, `TestIntegration_ResetSessionRollsBackPendingTransaction` |
| 10.5 | No PostgreSQL mode | `TestParseDSN_UnsupportedScheme`, `TestParseDSN_Native_UnsupportedScheme` |
| 10.5 | No JDBC bridge | Verified by implementation review: the driver talks directly to H2 native TCP and does not embed or invoke a JVM/JDBC bridge. |
| 10.5 | No copied Java code | Verified by implementation review: Go sources are original implementations, using H2 Java only as protocol reference/specification. |
| 10.5 | Pure Go driver code | `CGO_ENABLED=0 go build ./...` |
| 10.5 | H2 Java source used only as reference/specification | Verified by implementation review and repository policy; no runtime dependency on H2 Java sources. |

## Validation commands used

The following commands were used to confirm the acceptance matrix and the implementation constraints:

- `go test ./...`
- `go test -race ./...`
- `go test -tags integration ./...`
- `go test -tags integration -race ./...`
- `CGO_ENABLED=0 go build ./...`

## Notes

- Integration tests use the local `h2-data` environment and skip cleanly when the H2 server or credentials are unavailable.
- The matrix above intentionally references both unit and integration tests; PRD §10 acceptance spans both categories.
