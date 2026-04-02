BINARY  := altcode
CMD     := ./cmd/altcode
DIST    := ./dist
VERSION ?= dev

LDFLAGS := -ldflags "-X main.Version=$(VERSION)"
# vendor/ contains git submodules (codex, claude-code), not Go deps
GOFLAGS := -mod=mod

.PHONY: build test lint clean

build:
	@mkdir -p $(DIST)
	GOFLAGS=$(GOFLAGS) go build $(LDFLAGS) -o $(DIST)/$(BINARY) $(CMD)

test:
	GOFLAGS=$(GOFLAGS) go test ./... -race -count=1 -timeout=180s -parallel=8

lint:
	GOFLAGS=$(GOFLAGS) go vet ./...

clean:
	rm -rf $(DIST)
