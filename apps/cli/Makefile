# staqly CLI. `make build` → bin/staqly, `make install` → $(GOBIN) or ~/go/bin.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
LIVE_URL ?= http://127.0.0.1:18790

.PHONY: build test test-live vet install snapshot clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/staqly ./cmd/staqly

test: vet
	go test -race ./...

vet:
	go vet ./...

# Needs a running local-mode API, e.g.:
#   cd ../api && DATA_DIR=/tmp/staqly-cli ADDR=127.0.0.1:18790 go run ./cmd/server
test-live:
	STAQLY_LIVE=1 STAQLY_LIVE_URL=$(LIVE_URL) go test -race -count=1 -run TestLive -v ./internal/cli

install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/staqly

# Local cross-compile check with goreleaser (no publishing).
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin dist
