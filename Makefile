BINARY  := altcode
CMD     := ./cmd/altcode
DIST    := ./dist
VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildCommit=$(COMMIT) -X main.BuildDate=$(DATE)"
# vendor/ contains git submodules (codex, claude-code), not Go deps
GOFLAGS := -mod=mod

.PHONY: build test lint clean tui-eval tui-density tui-perf tui-coverage tui-latency verify verify-remote release

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

# Per-version pre-ship gate. Walks every documented command in
# README + altcode.io and confirms it actually works against the
# binary you're about to ship. Run BEFORE every release.
verify: build
	@bash scripts/verify-instructions.sh

# Same as `verify` but also pulls https://altcode.io/bin/<asset>
# and confirms the served binary matches what's in docs/bin/.
verify-remote: build
	@bash scripts/verify-instructions.sh --remote

# Full release workflow — version-bump → build all 4 platforms →
# verify the new build → tag for push.
#   make release VERSION=v0.10.6
release: verify
	@if [ -z "$(VERSION)" ]; then \
		echo "usage: make release VERSION=vX.Y.Z"; exit 2; \
	fi
	@echo "▸ release $(VERSION) — rebuilding 4 platforms"
	@for combo in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		GOOS=$${combo%/*}; GOARCH=$${combo#*/}; \
		out="docs/bin/altcode-$${GOOS}-$${GOARCH}"; \
		echo "  → $${out}"; \
		GOFLAGS=$(GOFLAGS) GOOS=$$GOOS GOARCH=$$GOARCH CGO_ENABLED=0 \
			go build -trimpath -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildCommit=$(COMMIT) -X main.BuildDate=$(DATE)" -o "$$out" ./cmd/altcode/ \
			|| { echo "  FAIL"; exit 1; }; \
	done
	@echo ""
	@echo "▸ updating docs/index.html version chip to $(VERSION)"
	@sed -i.bak -E 's|<div class="right">v[0-9]+\.[0-9]+\.[0-9]+</div>|<div class="right">$(VERSION)</div>|' docs/index.html && rm -f docs/index.html.bak
	@echo ""
	@echo "▸ next steps:"
	@echo "  1. git add docs/bin/ docs/index.html"
	@echo "  2. git commit -m 'release: $(VERSION)'"
	@echo "  3. git push origin main"
	@echo "  4. make verify-remote   (after GH Pages republishes ~1min)"
