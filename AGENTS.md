# Pure Go H2 Native TCP Driver

This repository is for a pure Go `database/sql` driver for H2 Database running in **native TCP server mode**.

Module/source path: `github.com/rom35-cz/h2go`  
Package name: `h2go`  
Registered SQL driver name: `h2`

## Required Reading

Before planning or implementing project work, read these files in order:

1. `PRD.md` — product requirements, scope, non-goals, accepted decisions, and acceptance criteria.
2. `IMPLEMENTATION_PLAN.md` — serial implementation phases and tasks. Work should follow this plan unless the user explicitly changes it.
3. `FEASIBILITY_STUDY.md` — background research and protocol feasibility details. Read when protocol context, rationale, or tradeoffs are needed.

## Project Structure

Current repository layout:

- `AGENTS.md` — agent instructions for this repository.
- `PRD.md` — product requirements document. Treat as the authoritative requirements source.
- `IMPLEMENTATION_PLAN.md` — detailed serial implementation plan. Treat as the authoritative task sequencing source.
- `FEASIBILITY_STUDY.md` — feasibility study and research notes.
- `h2-data/` — local H2 test environment.
  - `.env` — local credentials/config; sensitive, do not commit secrets elsewhere and do not copy values into docs.
  - `h2-2.4.240.jar` — local H2 server jar for integration testing.
  - `h2.sh` — local helper script to start H2 TCP server.
  - `data/` — local H2 database files; generated/local state.
- `h2-src/` — local H2 source/reference material if present.

Expected future Go project layout, unless changed in `IMPLEMENTATION_PLAN.md`:

- `go.mod` — module `github.com/rom35-cz/h2go`.
- `doc.go` — package docs for `package h2go`.
- `dsn.go` / `dsn_test.go` — DSN parsing and config handling.
- `transfer.go` / `transfer_test.go` — H2 wire primitive codec.
- `protocol.go` — H2 protocol constants for TCP protocol 21.
- `auth.go` / `auth_test.go` — H2-compatible password hashing.
- `handshake.go` — TCP handshake and session initialization.
- `driver.go`, `connector.go`, `conn.go` — `database/sql/driver` integration.
- `command.go`, `rows.go`, `stmt.go`, `tx.go`, `result.go` — query/exec/prepared/transaction/result implementation.
- `value_read.go`, `value_write.go`, `typeinfo.go`, `metadata.go` — value and metadata encoding/decoding.
- `errors.go` — structured H2 SQL errors.
- `logging.go` — optional `log/slog` diagnostics.
- `*_test.go` — unit tests; integration tests should use the `integration` build tag.

## Implementation Rules

- Follow `IMPLEMENTATION_PLAN.md` task order. Complete one task at a time.
- At the end of every implementation task, the repository should be in a consistent state:
  - `go build ./...` passes.
  - `go vet ./...` passes.
  - `go test ./...` passes.
  - Integration tests may skip when H2 env/server is unavailable, but must pass when available.
- Use H2 Java source only as a protocol reference/specification. Do **not** copy or translate Java code into Go.
- Implement only H2 **2.4.240 and later** using native TCP protocol **21**.
- Do **not** implement PostgreSQL compatibility mode, JDBC bridge, JVM embedding, or CGO.
- Preserve the module path/package naming decision: module `github.com/rom35-cz/h2go`, package `h2go`, registered driver name `h2`.
- Do not add support for older H2 protocol versions unless the user explicitly changes the PRD.
- Do not put real credentials/passwords into source, docs, examples, commits, or final responses. Refer to `.env` variable names only.

## Local Integration Testing

Local H2 setup lives in `h2-data/`.

Start local H2 server:

```bash
cd h2-data
./h2.sh
```

Integration tests should read these environment variables, usually from `h2-data/.env` or the process environment:

- `JDBC_URL`
- `JDBC_USER`
- `JDBC_PASSWORD`
- `JDBC_TLS_URL` (optional; enables the TLS transport test — start the TLS
  server first with `make db-tls`)

Integration tests should skip cleanly when required env vars are absent.

## Git and Release Rules

- Use Conventional Commits for commit messages.
- Use semantic versioning for release tags.
- Before any release, update `CHANGES.txt` or `CHANGELOG.md` consistently with the release process chosen for the repository.
- **GitHub account check before pushing:** this is a home project owned by
  the `rom35-cz` account. The machine also has a work account
  (`roman-majer_o2cz`) logged in via `gh`. Before every push, run
  `gh auth status` and check which account is active; if it is
  `roman-majer_o2cz`, run `gh auth switch --user rom35-cz` (no sudo needed)
  so the push has access to the remote repository.
