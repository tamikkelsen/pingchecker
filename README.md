# PingChecker

A lightweight, self-hosted network latency monitor with a real-time web dashboard. Pings any number of hosts every second, stores results in a local SQLite database, and streams live data to the browser over WebSocket.

## Features

- **Live chart** — rolling 5-minute latency view updated every second
- **Historical charts** — 1 h / 6 h / 24 h / 7 d time ranges with automatic downsampling
- **Drop events table** — lists every failure with PARTIAL / TOTAL severity
- **CSV export** — download full or range-filtered history and drop logs
- **Dynamic host management** — add or remove hosts from the UI without restarting
- **Cross-platform** — runs on macOS, Linux, and Windows (Python or standalone `.exe`)

## Quick Start

### macOS / Linux

```bash
git clone https://github.com/tamikkelsen/pingchecker.git
cd pingchecker
chmod +x run.sh
./run.sh
```

The script creates a virtual environment, installs dependencies, starts the server, and opens `http://localhost:8765` in your browser.

### Windows (Python)

```bat
run.bat
```

Same behavior as the shell script — requires Python 3.9+ on `PATH`.

### Windows (standalone `.exe`)

Build a single-file executable that requires no Python installation on the target machine:

```bat
build_windows.bat
```

Output: `dist\PingChecker.exe` — copy it anywhere and double-click to run. The database (`pings.db`) and config (`config.json`) are created next to the `.exe` on first launch.

## Manual Setup

```bash
python3 -m venv .venv
source .venv/bin/activate        # Windows: .venv\Scripts\activate
pip install -r requirements.txt
python ping_checker.py
```

Open `http://localhost:8765`.

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

To change the ping interval, edit `PING_INTERVAL` at the top of `ping_checker.py` (default: `1` second).

## Requirements

- Python 3.9+
- [`fastapi`](https://fastapi.tiangolo.com/) — REST API + WebSocket server
- [`uvicorn[standard]`](https://www.uvicorn.org/) — ASGI server
- [`aiosqlite`](https://github.com/omnilib/aiosqlite) — async SQLite access

All listed in `requirements.txt`.

## API Reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/hosts` | List all monitored hosts |
| `POST` | `/api/hosts` | Add a host `{"host": "…", "label": "…"}` |
| `DELETE` | `/api/hosts/{host}` | Remove a host |
| `GET` | `/api/history` | Ping records (`start`, `end`, `host` query params) |
| `GET` | `/api/drops` | Drop events (`start`, `end` query params) |
| `GET` | `/api/export/history.csv` | Download history as CSV |
| `GET` | `/api/export/drops.csv` | Download drop events as CSV |
| `WS` | `/ws` | WebSocket — streams live ping results |

All timestamps are Unix epoch seconds. `start` and `end` default to the last hour when omitted.

## Data Storage

Ping results are stored in `pings.db` (SQLite) in the same directory as the script (or next to the `.exe`). The database is created automatically on first run.

Schema:

```sql
hosts (host TEXT PRIMARY KEY, label TEXT, added_at INTEGER)
pings (id INTEGER, host TEXT, timestamp INTEGER, success INTEGER, latency_ms REAL)
```

## License

MIT
