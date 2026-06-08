# PingChecker — build & cross-compile
#
#   make run     # run locally (go run .)
#   make build   # build a binary for THIS machine -> ./pingchecker
#   make cross   # cross-compile all release targets -> dist/
#   make clean

BINARY  := pingchecker
DIST    := dist
# Version is derived from git (tag/commit) for local builds; CI overrides it
# with VERSION=v1.<build-number>.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# CGO is off so cross-compilation needs no C toolchain (pure-Go SQLite).
GOFLAGS := CGO_ENABLED=0

.PHONY: run build cross clean

run:
	go run .

build:
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

cross: clean
	@mkdir -p $(DIST)
	$(GOFLAGS) GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-windows-amd64.exe .
	$(GOFLAGS) GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-darwin-arm64 .
	$(GOFLAGS) GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-amd64 .
	@echo
	@echo "Built release binaries:"
	@ls -lh $(DIST)

clean:
	rm -rf $(DIST) $(BINARY)
