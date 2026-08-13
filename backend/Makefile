.PHONY: build generate test test-unit test-integration test-integration-tls-live test-e2e test-e2e-contract test-e2e-live test-e2e-local

VERSION ?= $(shell tr -d '\r\n' < ./cmd/server/VERSION)
LDFLAGS ?= -s -w -X main.Version=$(VERSION)

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/server ./cmd/server

generate:
	go generate ./ent
	go generate ./cmd/server

test:
	go test ./...
	golangci-lint run ./...

test-unit:
	go test -tags=unit ./...

test-integration:
	go test -tags=integration ./...

# Explicit external TLS capture smoke. It is intentionally excluded from the
# standard integration suite; an unavailable or invalid capture service fails.
test-integration-tls-live:
	go test -tags='integration,tlslive' -count=1 -v \
		-run '^TestDialerAgainstCaptureServer$$' ./internal/pkg/tlsfingerprint

test-e2e: test-e2e-contract

# Provider-free, mutation-safe contract suite. The script owns an isolated
# PostgreSQL/Redis/application stack and always tears it down on exit.
test-e2e-contract:
	sh ./scripts/e2e-test.sh contract

# Explicit external-provider smoke. BASE_URL is mandatory; at least one
# configured provider must actually execute (override with E2E_LIVE_MIN_ATTEMPTS).
test-e2e-live:
	sh ./scripts/e2e-test.sh live

# Backward-compatible local alias. It intentionally means live smoke now;
# contract E2E must always use the isolated stack above.
test-e2e-local:
	sh ./scripts/e2e-test.sh live
