# PingChecker

[![Release](https://img.shields.io/github/v/release/tamikkelsen/pingchecker?display_name=tag&sort=semver&label=release)](https://github.com/tamikkelsen/pingchecker/releases/latest)
[![Build](https://github.com/tamikkelsen/pingchecker/actions/workflows/release.yml/badge.svg)](https://github.com/tamikkelsen/pingchecker/actions/workflows/release.yml)
[![Downloads](https://img.shields.io/github/downloads/tamikkelsen/pingchecker/total?label=downloads)](https://github.com/tamikkelsen/pingchecker/releases)

A lightweight, self-hosted network latency monitor with a real-time web dashboard. Pings any number of hosts, stores results in a local SQLite database, and streams live data to the browser over WebSocket.

It ships as a **single self-contained binary** — no Python, no runtime, nothing to install. Download the build for your OS, run it, and open the dashboard. The web UI is embedded in the binary; the database (`pings.db`) and config (`config.json`) are created next to it on first launch.

## Features

- **Live chart** — rolling 5-minute latency view updated every second
- **Historical charts** — 1 h / 6 h / 24 h / 7 d time ranges with automatic downsampling
- **Per-host stats** — rolling avg / min / max / jitter / packet-loss % in the sidebar
- **Adjustable settings** — change ping interval, packet size, and timeout live from the UI (no restart)
- **Pause / resume** — halt and resume monitoring without stopping the server
- **Drop alerts** — optional desktop notification + sound when a host goes down
- **Drop events table** — lists every failure with PARTIAL / TOTAL severity
- **CSV export** — download full or range-filtered history and drop logs
- **Dynamic host management** — add or remove hosts from the UI without restarting
- **Cross-platform** — runs on macOS, Linux, and Windows (Python or standalone `.exe`)

## Quick Start

Grab the binary for your platform, then run it:

| Platform | Binary |
|----------|--------|
| Windows (x64) | `pingchecker-windows-amd64.exe` |
| macOS (Apple Silicon) | `pingchecker-darwin-arm64` |
| Linux (x64) | `pingchecker-linux-amd64` |

**Windows** — double-click `pingchecker-windows-amd64.exe`.

**macOS / Linux:**

```bash
chmod +x pingchecker-darwin-arm64
./pingchecker-darwin-arm64
```

The server starts on `http://localhost:8765` and opens it in your browser. On macOS the first run may be blocked by Gatekeeper — right-click → **Open**, or run `xattr -d com.apple.quarantine ./pingchecker-darwin-arm64`.

## Build from Source

Requires [Go](https://go.dev/dl/) 1.25+. The build uses pure-Go SQLite (`CGO_ENABLED=0`), so cross-compiling needs no C toolchain.

```bash
git clone https://github.com/tamikkelsen/pingchecker.git
cd pingchecker

make run      # run locally
make build    # build ./pingchecker for this machine
make cross    # cross-compile all targets into dist/
```

`make cross` produces the three binaries above. To build a single target manually:

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o pingchecker.exe .
```

## Legacy (Python) Version

The original Python/FastAPI implementation lives in [`legacy/`](legacy/) and still works — it shares the same `static/`, `config.json`, and `pings.db` in the repo root.

```bash
cd legacy
./run.sh           # macOS / Linux  (run.bat on Windows)
```

See [`legacy/`](legacy/) for the PyInstaller build (`build_windows.bat`) as well.

## Configuration

Edit `config.json` to set the default hosts loaded on first run. Once the database exists, hosts are managed from the UI and `config.json` is ignored.

```json
{
  "hosts": [
    { "host": "8.8.8.8",  "label": "Google DNS" },
    { "host": "1.1.1.1",  "label": "Cloudflare DNS" },
    { "host": "9.9.9.9",  "label": "Quad9 DNS" }
  ]
}
```

Each entry can be a plain string (IP or hostname) or an object with `host` and an optional `label`.

### Monitoring settings

Ping interval, packet size, and timeout are adjustable at runtime from the **⚙ Settings** panel in the web UI and are persisted in the database — no restart required. The **⏸ Pause** button stops and resumes the background ping loop, and **🔔 Alerts** toggles a desktop notification + sound when a host drops.

| Setting | Default | Range | Notes |
|---------|---------|-------|-------|
| `interval` | `1` | 1–3600 s | Seconds between ping rounds |
| `packet_size` | `56` | 0–65500 bytes | ICMP payload size (`0` = OS default) |
| `timeout_ms` | `2000` | 100–60000 ms | Per-ping timeout |

To override the first-run defaults, add an optional `"settings"` object to `config.json` (used only until values are saved from the UI):

```json
{
  "hosts": [ ... ],
  "settings": { "interval": 2, "packet_size": 120, "timeout_ms": 1500 }
}
```

## Requirements

**To run:** nothing — the binary is fully self-contained. It shells out to the system `ping` command (present on Windows, macOS, and Linux), so no elevated privileges or raw sockets are needed.

**To build:** [Go](https://go.dev/dl/) 1.25+. Two dependencies, both pure Go:

- [`gorilla/websocket`](https://github.com/gorilla/websocket) — WebSocket server
- [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — CGO-free SQLite driver

The dashboard (`static/index.html`) is embedded into the binary at build time via `go:embed`.

## API Reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/hosts` | List all monitored hosts |
| `POST` | `/api/hosts` | Add a host `{"host": "…", "label": "…"}` |
| `DELETE` | `/api/hosts/{host}` | Remove a host |
| `GET` | `/api/settings` | Current monitoring settings |
| `PUT` | `/api/settings` | Update settings `{"interval": …, "packet_size": …, "timeout_ms": …, "paused": …}` (any subset) |
| `GET` | `/api/history` | Ping records (`start`, `end`, `host` query params) |
| `GET` | `/api/drops` | Drop events (`start`, `end` query params) |
| `GET` | `/api/export/history.csv` | Download history as CSV |
| `GET` | `/api/export/drops.csv` | Download drop events as CSV |
| `WS` | `/ws` | WebSocket — streams live ping results and settings changes |

All timestamps are Unix epoch seconds. `start` and `end` default to the last hour when omitted. All `/api/*` and `/ws` requests require the auth token (see below).

## Security & Access

PingChecker is meant to run on the machine you're using. It is hardened accordingly:

- **Loopback only.** The server binds `127.0.0.1:8765`, so it is not reachable from other machines. To deliberately expose it on a trusted LAN, change `addr` in `main.go` to `0.0.0.0:8765` and rebuild (you then rely on the token for access control).
- **Bearer token.** A 32-byte random token is generated on first run, printed at startup, and saved to `pings_token.txt` next to the database. Every `/api/*` and `/ws` request must present it. The **browser dashboard works automatically** — loading the page sets a `SameSite=Strict`, `HttpOnly` cookie. For `curl`/scripts, pass the token explicitly:

  ```bash
  TOKEN=$(cat pings_token.txt)
  curl -H "Authorization: Bearer $TOKEN" http://localhost:8765/api/hosts
  # WebSocket clients that can't set headers may use ?token=$TOKEN
  ```

- **Origin & Host checks.** Cross-site browser requests are rejected (Origin allowlist), and non-loopback `Host` headers are refused to defeat DNS-rebinding attacks.

Delete `pings_token.txt` and restart to rotate the token.

## Data Storage

Ping results are stored in `pings.db` (SQLite, WAL mode) next to the binary. The database, `config.json`, and `pings_token.txt` all live in the same directory. The database is created automatically on first run. Set the `PINGCHECKER_DATA` environment variable to use a different directory.

Schema:

```sql
hosts    (host TEXT PRIMARY KEY, label TEXT, added_at INTEGER)
pings    (id INTEGER, host TEXT, timestamp INTEGER, success INTEGER, latency_ms REAL)
settings (key TEXT PRIMARY KEY, value TEXT)
```

## Credits

Created by **Tommy Mikkelsen** in collaboration with **Claude** (Anthropic's [Claude Code](https://claude.com/claude-code)).

## License

Released under the [MIT License](LICENSE) — © 2026 Tommy Mikkelsen.

The compiled binary bundles several permissive (MIT / BSD) Go dependencies; their
copyright notices and license texts are reproduced in [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
