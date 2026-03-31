BINARY  := altcode
CMD     := ./cmd/altcode
DIST    := ./dist
VERSION ?= dev

LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

.PHONY: build test lint clean

build:
	@mkdir -p $(DIST)
	go build $(LDFLAGS) -o $(DIST)/$(BINARY) $(CMD)

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf $(DIST)
