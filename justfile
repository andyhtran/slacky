default:
    @just --list --unsorted

# Run binary slacky via go run
[group('dev')]
slacky *args:
    go run ./cmd/slacky {{args}}

# Build the binary
[group('build')]
build:
    mkdir -p dist
    CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=dev" -o dist/slacky ./cmd/slacky

# Install the binary into GOBIN, falling back to GOPATH/bin when GOBIN is unset
[group('build')]
install:
    #!/usr/bin/env bash
    set -euo pipefail
    gobin="$(go env GOBIN)"
    if [[ -z "$gobin" ]]; then
      gobin="$(go env GOPATH)/bin"
    fi
    mkdir -p "$gobin"
    CGO_ENABLED=0 GOBIN="$gobin" go install -trimpath -ldflags "-X main.version=dev" ./cmd/slacky
    printf 'installed %s\n' "$gobin/slacky"

# Remove build artifacts
[group('build')]
clean:
    rm -rf dist coverage.out

# Run tests
[group('check')]
test:
    go test ./...

# Run go vet
[group('check')]
vet:
    go vet ./...

# Run golangci-lint
[group('check')]
lint:
    golangci-lint run

# Format code
[group('dev')]
fmt:
    gofumpt -w .

# Check formatting
[group('check')]
fmt-check:
    test -z "$(gofumpt -l .)"

# Run the full local gate
[group('check')]
ci: fmt-check vet lint test smoke


# Run a cheap command matrix with isolated local state
[group('smoke')]
smoke:
    ./scripts/smoke.sh

# Run authenticated Slack read smoke; set SLACKY_LIVE_SEND=1 to create test messages through slacking
[group('smoke')]
smoke-live:
    ./scripts/smoke-live.sh
