BINARY  := altcode
CMD     := ./cmd/altcode
DIST    := ./dist
VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildCommit=$(COMMIT) -X main.BuildDate=$(DATE)"
# vendor/ contains git submodules (codex, claude-code), not Go deps
GOFLAGS := -mod=mod

.PHONY: build test lint clean tui-eval tui-density tui-perf tui-coverage tui-latency

build:
	@mkdir -p $(DIST)
	GOFLAGS=$(GOFLAGS) go build $(LDFLAGS) -o $(DIST)/$(BINARY) $(CMD)

test:
	GOFLAGS=$(GOFLAGS) go test ./... -race -count=1 -timeout=180s -parallel=8

lint:
	GOFLAGS=$(GOFLAGS) go vet ./...

clean:
	rm -rf $(DIST)

tui-eval:
	@bash scripts/tui_eval.sh

tui-density:
	@bash scripts/tui_density.sh

tui-perf:
	@bash scripts/tui_perf.sh

tui-coverage:
	@bash scripts/tui_coverage.sh

tui-latency:
	@bash scripts/tui_latency.sh
