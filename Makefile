.PHONY: build vet lint test test-race test-integration test-integration-race db-seed clean

build:
	go build ./...

vet:
	go vet ./...

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed; skipping lint"; \
	fi

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