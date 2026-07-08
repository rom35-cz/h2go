.PHONY: build vet lint test test-race test-integration clean

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

clean:
	go clean ./...
	rm -f coverage.out