# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A self-hosted network latency monitor: pings a list of hosts on a loop, stores results in SQLite, serves a single-page web dashboard, and streams live pings to the browser over WebSocket. Ships as one self-contained binary with the UI embedded via `go:embed`.

## Commands

```bash
make run      # go run . — local dev, serves http://localhost:8765 and opens a browser
make build    # build ./pingchecker for this machine (version stamped from git)
make cross    # cross-compile windows-amd64, darwin-arm64, linux-amd64 into dist/
make clean
```

Build a single target manually (CGO is off, so no C toolchain is needed):

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o pingchecker.exe .
```

Requires Go 1.25+. **There is no test suite** — no `*_test.go` files exist. Validation is manual: run the server and exercise the UI / API.

## Architecture

Almost all Go logic lives in **`main.go`** (single `package main` file, ~1000 lines, organized by `// ---` banner sections). The other moving parts:

- **`static/index.html`** — the entire dashboard (HTML/CSS/JS in one file). Embedded into the binary with `//go:embed static`. Editing the UI means editing this file; rebuild to bake it in.
- **`legacy/`** — the original Python/FastAPI implementation (`ping_checker.py`). The Go version is a deliberate port of it, and **they share `static/`, `config.json`, and `pings.db` from the repo root.** Several Go comments call out behavioral parity ("match the Python server's openness", "match the Python ORDER BY"). Treat the two as twins: a deliberate behavior change to the HTTP/WS API should be reflected in both, or the divergence noted.

### Runtime model (main.go)

- **One background ping goroutine** (`pingLoop`) reads the host list from the DB each round, pings all hosts concurrently (one goroutine each, joined with a `WaitGroup`), writes a batch insert in a single transaction, then broadcasts results. Sleep is `interval - elapsed`, floored at 100ms. Honors the `paused` setting.
- **Pinging shells out to the system `ping`** (`pingOnce`) — no raw sockets, no root. Args are OS-specific (`runtime.GOOS` switch for windows/darwin/linux: timeout flags and payload-size flags differ); latency is scraped from stdout with `timeRe`. This is why behavior depends on the host OS's `ping` binary.
- **WebSocket hub** (`hub`, global `H`) does buffered fan-out: each client has a `send` channel and a dedicated writer goroutine; the broadcast loop drops a frame for any client whose buffer is full rather than blocking the ping loop. `CheckOrigin` returns true (open by design).
- **SQLite** is opened with `SetMaxOpenConns(1)` (single writer, serializes read/write to avoid "database is locked") in WAL mode. The pure-Go `modernc.org/sqlite` driver keeps CGO off so cross-compiles need no C toolchain.

### Settings precedence

Runtime settings (`interval`, `packet_size`, `timeout_ms`, `paused`) are resolved in `loadSettings` as: **built-in defaults → `config.json` `"settings"` block → persisted `settings` rows in the DB**. Once the UI saves a value it lives in the DB and wins. `PUT /api/settings` takes a partial payload (pointer fields), clamps each value (`applyUpdate`/`clampInt`), persists, and broadcasts the change to all clients. Hosts are seeded from `config.json` **only when the `hosts` table is empty** — after that, `config.json` is ignored and hosts are managed via the API/UI.

### Data location

`dataDir()` resolves where `pings.db` and `config.json` live: `PINGCHECKER_DATA` env var if set, else the binary's own directory (but **not** when running under `go run`, which builds into a `go-build` temp dir — it falls back to CWD so dev data isn't scattered). `pings.db` (and its `-wal`/`-shm`) are gitignored runtime data.

## Versioning & releases

The `version` var is stamped at build time via `-ldflags "-X main.version=..."` (git describe locally; CI sets `v1.<run_number>`). The `release` GitHub workflow (`.github/workflows/release.yml`) builds the three binaries and publishes a GitHub Release on every push to `main` that touches `**/*.go`, `go.mod`, `go.sum`, `static/**`, the `Makefile`, or the workflow itself. It deletes-and-recreates the tag, so it's idempotent on re-runs of the same build number.
