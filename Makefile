.PHONY: build vet lint fmt fmt-check test test-race test-integration test-integration-race bench bench-integration soak db-seed db-tls clean

build:
	go build ./...

vet:
	go vet ./...

# Requires golangci-lint (https://golangci-lint.run); silently skipped when
# the binary is not on PATH. Run with --build-tags integration for full cover.
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed; skipping lint"; \
	fi

# fmt-check fails when any tracked Go file is not gofmt-formatted; fmt rewrites
# in place.
fmt:
	gofmt -w .

fmt-check:
	@files="$$(gofmt -l .)"; if [ -n "$$files" ]; then echo "gofmt needed:"; echo "$$files"; exit 1; fi

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	@set -a; \
	if [ -f h2-data/.env ]; then . ./h2-data/.env; fi; \
	set +a; \
	go test -tags=integration ./...

test-integration-race:
	@set -a; \
	if [ -f h2-data/.env ]; then . ./h2-data/.env; fi; \
	set +a; \
	go test -tags=integration -race ./...

## bench: pure/server-free microbenchmarks (wire codec, DSN, values).
bench:
	go test -run '^$$' -bench . -benchmem ./...

## bench-integration: live-server round-trip benchmarks.
## The server must be running first: cd h2-data && ./h2.sh
bench-integration:
	@set -a; \
	if [ -f h2-data/.env ]; then . ./h2-data/.env; fi; \
	set +a; \
	go test -tags=integration -run '^$$' -bench Integration -benchmem .

clean:
	go clean ./...
	rm -f coverage.out

## db-seed: (re)apply h2-data/seed.sql to the running H2 instance.
## The server must be running first: cd h2-data && ./h2.sh
db-seed:
	@set -a; . h2-data/.env; set +a; \
	java -cp h2-data/h2-2.4.240.jar org.h2.tools.RunScript \
	  -url "$$JDBC_URL" \
	  -user "$$JDBC_USER" \
	  -password "$$JDBC_PASSWORD" \
	  -script h2-data/seed.sql \
	  -showResults
## db-tls: generate local TLS test certs (idempotent) and start a second
## H2 TCP server with -tcpSSL on port 9093. Point the driver at it with:
##   export JDBC_TLS_URL="jdbc:h2:ssl://localhost:9093/h2-go"
db-tls:
	cd h2-data && ./gen-tls-certs.sh && ./h2-tls.sh

## soak: run the bounded soak test (default 60s; override via H2GO_SOAK_SECONDS).
## The server must be running first: cd h2-data && ./h2.sh
soak:
	@set -a; \
	if [ -f h2-data/.env ]; then . ./h2-data/.env; fi; \
	set +a; \
	H2GO_SOAK_SECONDS=$${H2GO_SOAK_SECONDS:-60} go test -tags=integration -count=1 -run 'TestIntegration_Soak' -v .
