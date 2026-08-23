# Contributing to h2go

Thanks for considering a contribution! This is a pure Go `database/sql`
driver speaking H2's **native TCP protocol 21** (H2 2.4.240+). Scope rules:
no PostgreSQL compatibility mode, no JDBC bridge, no JVM embedding, no CGO,
no older protocol versions.

## Development setup

Requirements: Go (see `go.mod`) and, for integration tests, a JDK 17+.

```bash
cd h2-data && ./h2.sh          # start the local H2 TCP server (:9092)
make db-tls                    # optional: second server with -tcpSSL on :9093
```

Integration tests read `JDBC_URL`, `JDBC_USER`, `JDBC_PASSWORD` from the
environment (`h2-data/.env` is loaded automatically by the make targets) and
skip cleanly when absent.

## The green matrix

Every change must keep all of the following green before it is pushed:

```bash
go build ./...
go vet ./...            && go vet -tags=integration ./...
gofmt -l .              # empty output
golangci-lint run ./... && golangci-lint run --build-tags integration ./...
go test ./...           && go test -race ./...
go test -tags=integration ./...            # against the running server
CGO_ENABLED=0 go build ./...
```

CI (`.github/workflows/ci.yml`) enforces the same matrix plus live-H2
integration with a zero-skip guard.

## Ground rules

- Conventional Commits for commit messages; semantic versioning for tags.
- Use the H2 Java sources only as a protocol reference — never translate
  Java code into Go.
- Never commit credentials; refer to environment variable names only.
- Protocol behavior changes belong in `CHANGELOG.md`; user-visible features
  in `README.md`.

## Pull requests

1. Fork / branch from `main`.
2. Keep changes focused; update tests (unit + integration where applicable).
3. Run the green matrix locally first.
4. Describe *what* and *why*; link issues if any.
