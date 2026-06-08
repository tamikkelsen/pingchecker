#!/usr/bin/env python3
# PingChecker — multi-host latency monitor
# Setup:  pip install -r requirements.txt
# Run:    python ping_checker.py
# Open:   http://localhost:8765

import asyncio
import csv
import io
import json
import math
import platform
import re
import sys
import threading
import time
import webbrowser
from collections import defaultdict
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Dict, List, Optional, Set, Tuple

import aiosqlite
import uvicorn
from fastapi import FastAPI, HTTPException, WebSocket, WebSocketDisconnect
from fastapi.responses import FileResponse, StreamingResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel

# ---------------------------------------------------------------------------
# Paths — work both as a plain script and as a PyInstaller frozen .exe
# ---------------------------------------------------------------------------
_FROZEN = getattr(sys, "frozen", False)

if _FROZEN:
    # Static files are bundled inside the exe (extracted to _MEIPASS at runtime)
    STATIC_DIR = Path(sys._MEIPASS) / "static"          # type: ignore[attr-defined]
    # Writable data (db, config) lives next to the .exe
    BASE = Path(sys.executable).parent
else:
    STATIC_DIR = Path(__file__).parent / "static"
    BASE = Path(__file__).parent

DB_PATH = BASE / "pings.db"
CONFIG  = BASE / "config.json"

# Runtime-adjustable settings — persisted in the DB, editable from the web UI.
# The background loop reads these live, so changes take effect without a restart.
DEFAULT_SETTINGS: Dict[str, int] = {
    "interval":    1,     # seconds between ping rounds
    "packet_size": 56,    # ICMP payload size in bytes (0 = OS default)
    "timeout_ms":  2000,  # per-ping timeout in milliseconds
    "paused":      0,     # 1 = pause the background ping loop
}
SETTINGS: Dict[str, int] = dict(DEFAULT_SETTINGS)
# ---------------------------------------------------------------------------


class _WSManager:
    def __init__(self) -> None:
        self._sockets: Set[WebSocket] = set()

    async def connect(self, ws: WebSocket) -> None:
        await ws.accept()
        self._sockets.add(ws)

    def disconnect(self, ws: WebSocket) -> None:
        self._sockets.discard(ws)

    async def broadcast(self, payload: dict) -> None:
        dead: Set[WebSocket] = set()
        for ws in list(self._sockets):
            try:
                await ws.send_json(payload)
            except Exception:
                dead.add(ws)
        self._sockets -= dead


manager = _WSManager()


async def ping_once(
    host: str,
    size: Optional[int] = None,
    timeout_ms: int = 2000,
) -> Tuple[bool, Optional[float]]:
    """Ping host once; return (reachable, rtt_ms or None)."""
    system = platform.system()
    if system == "Windows":
        cmd = ["ping", "-n", "1", "-w", str(timeout_ms)]
        if size and size > 0:
            cmd += ["-l", str(size)]          # -l sets the payload size on Windows
    elif system == "Darwin":                  # -W is milliseconds on macOS
        cmd = ["ping", "-c", "1", "-W", str(timeout_ms)]
        if size and size > 0:
            cmd += ["-s", str(size)]          # -s sets the payload size
    else:                                     # -W is seconds on Linux
        cmd = ["ping", "-c", "1", "-W", str(max(1, math.ceil(timeout_ms / 1000)))]
        if size and size > 0:
            cmd += ["-s", str(size)]
    cmd.append(host)

    try:
        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout, _ = await asyncio.wait_for(
            proc.communicate(), timeout=timeout_ms / 1000 + 3.0
        )
        text = stdout.decode(errors="replace")
        if proc.returncode == 0:
            m = re.search(r"time[=<](\d+\.?\d*)\s*ms", text)
            return True, (float(m.group(1)) if m else None)
    except Exception:
        pass
    return False, None


# ---------------------------------------------------------------------------
# Database
# ---------------------------------------------------------------------------

async def init_db() -> None:
    async with aiosqlite.connect(DB_PATH) as db:
        await db.executescript("""
            CREATE TABLE IF NOT EXISTS hosts (
                host      TEXT PRIMARY KEY,
                label     TEXT NOT NULL,
                added_at  INTEGER NOT NULL
            );
            CREATE TABLE IF NOT EXISTS pings (
                id         INTEGER PRIMARY KEY AUTOINCREMENT,
                host       TEXT    NOT NULL,
                timestamp  INTEGER NOT NULL,
                success    INTEGER NOT NULL,
                latency_ms REAL
            );
            CREATE INDEX IF NOT EXISTS idx_pt ON pings(host, timestamp);
            CREATE INDEX IF NOT EXISTS idx_t  ON pings(timestamp);
            CREATE TABLE IF NOT EXISTS settings (
                key   TEXT PRIMARY KEY,
                value TEXT NOT NULL
            );
        """)
        await db.commit()


async def seed_hosts() -> None:
    if not CONFIG.exists():
        return
    data = json.loads(CONFIG.read_text())
    async with aiosqlite.connect(DB_PATH) as db:
        async with db.execute("SELECT COUNT(*) FROM hosts") as cur:
            if (await cur.fetchone())[0] > 0:
                return  # already seeded; don't overwrite user changes
        for entry in data.get("hosts", []):
            if isinstance(entry, str):
                host, label = entry, entry
            else:
                host  = entry["host"]
                label = entry.get("label", host)
            await db.execute(
                "INSERT OR IGNORE INTO hosts (host,label,added_at) VALUES (?,?,?)",
                (host, label, int(time.time())),
            )
        await db.commit()


# ---------------------------------------------------------------------------
# Settings (DB-backed, edited live from the UI)
# ---------------------------------------------------------------------------

def _coerce_settings(data: Dict) -> Dict[str, int]:
    """Clamp/validate incoming settings; silently drop unknown or bad keys."""
    out: Dict[str, int] = {}
    if "interval" in data:
        out["interval"] = max(1, min(3600, int(data["interval"])))
    if "packet_size" in data:
        out["packet_size"] = max(0, min(65500, int(data["packet_size"])))
    if "timeout_ms" in data:
        out["timeout_ms"] = max(100, min(60000, int(data["timeout_ms"])))
    if "paused" in data:
        out["paused"] = 1 if data["paused"] else 0
    return out


async def persist_settings() -> None:
    async with aiosqlite.connect(DB_PATH) as db:
        await db.executemany(
            "INSERT OR REPLACE INTO settings (key,value) VALUES (?,?)",
            [(k, str(v)) for k, v in SETTINGS.items()],
        )
        await db.commit()


async def load_settings() -> None:
    """Populate SETTINGS from defaults ← config.json ← persisted DB rows."""
    merged = dict(DEFAULT_SETTINGS)

    if CONFIG.exists():
        try:
            cfg = json.loads(CONFIG.read_text())
            merged.update(_coerce_settings(cfg.get("settings", {})))
        except Exception:
            pass

    async with aiosqlite.connect(DB_PATH) as db:
        async with db.execute("SELECT key,value FROM settings") as cur:
            rows = {r[0]: r[1] async for r in cur}
    for k in DEFAULT_SETTINGS:
        if k in rows:
            try:
                merged[k] = int(rows[k])
            except ValueError:
                pass

    SETTINGS.update(merged)
    await persist_settings()


# ---------------------------------------------------------------------------
# Background ping loop
# ---------------------------------------------------------------------------

async def ping_loop() -> None:
    while True:
        t0 = time.monotonic()

        if SETTINGS["paused"]:
            await asyncio.sleep(0.25)         # idle — re-check the pause flag often
            continue

        interval   = SETTINGS["interval"]
        size       = SETTINGS["packet_size"]
        timeout_ms = SETTINGS["timeout_ms"]

        async with aiosqlite.connect(DB_PATH) as db:
            async with db.execute("SELECT host FROM hosts") as cur:
                hosts = [r[0] async for r in cur]

        if hosts:
            ts: int = int(time.time())
            results: List[Tuple[bool, Optional[float]]] = list(
                await asyncio.gather(
                    *[ping_once(h, size=size, timeout_ms=timeout_ms) for h in hosts]
                )
            )
            async with aiosqlite.connect(DB_PATH) as db:
                await db.executemany(
                    "INSERT INTO pings (host,timestamp,success,latency_ms) VALUES (?,?,?,?)",
                    [(h, ts, int(s), ms) for h, (s, ms) in zip(hosts, results)],
                )
                await db.commit()

            await manager.broadcast({
                "type": "ping",
                "ts":   ts,
                "data": {h: {"ok": s, "ms": ms} for h, (s, ms) in zip(hosts, results)},
            })

        await asyncio.sleep(max(0.1, interval - (time.monotonic() - t0)))


# ---------------------------------------------------------------------------
# App lifecycle
# ---------------------------------------------------------------------------

@asynccontextmanager
async def lifespan(app: FastAPI):
    if not _FROZEN:
        STATIC_DIR.mkdir(exist_ok=True)
    await init_db()
    await seed_hosts()
    await load_settings()
    task = asyncio.create_task(ping_loop())
    # Auto-open browser when running as a compiled exe
    if _FROZEN:
        def _open_browser():
            time.sleep(1.5)
            webbrowser.open("http://localhost:8765")
        threading.Thread(target=_open_browser, daemon=True).start()
    yield
    task.cancel()
    try:
        await task
    except asyncio.CancelledError:
        pass


app = FastAPI(title="PingChecker", lifespan=lifespan)


# ---------------------------------------------------------------------------
# REST API
# ---------------------------------------------------------------------------

class HostIn(BaseModel):
    host:  str
    label: Optional[str] = None


class SettingsIn(BaseModel):
    interval:    Optional[int]  = None
    packet_size: Optional[int]  = None
    timeout_ms:  Optional[int]  = None
    paused:      Optional[bool] = None


@app.get("/api/settings")
async def api_get_settings():
    return dict(SETTINGS)


@app.put("/api/settings")
async def api_put_settings(body: SettingsIn):
    raw: Dict = {}
    if body.interval    is not None: raw["interval"]    = body.interval
    if body.packet_size is not None: raw["packet_size"] = body.packet_size
    if body.timeout_ms  is not None: raw["timeout_ms"]  = body.timeout_ms
    if body.paused      is not None: raw["paused"]      = body.paused

    updates = _coerce_settings(raw)
    if updates:
        SETTINGS.update(updates)
        await persist_settings()
        await manager.broadcast({"type": "settings", "settings": dict(SETTINGS)})
    return dict(SETTINGS)


@app.get("/api/hosts")
async def api_hosts():
    async with aiosqlite.connect(DB_PATH) as db:
        async with db.execute("SELECT host,label FROM hosts ORDER BY added_at") as cur:
            return [{"host": r[0], "label": r[1]} async for r in cur]


@app.post("/api/hosts", status_code=201)
async def api_add_host(body: HostIn):
    label = body.label or body.host
    async with aiosqlite.connect(DB_PATH) as db:
        try:
            await db.execute(
                "INSERT INTO hosts (host,label,added_at) VALUES (?,?,?)",
                (body.host, label, int(time.time())),
            )
            await db.commit()
        except Exception:
            raise HTTPException(400, "Host already exists")
    return {"host": body.host, "label": label}


@app.delete("/api/hosts/{host:path}")
async def api_del_host(host: str):
    async with aiosqlite.connect(DB_PATH) as db:
        await db.execute("DELETE FROM hosts WHERE host=?", (host,))
        await db.commit()
    return {"ok": True}


@app.get("/api/history")
async def api_history(
    start: Optional[int] = None,
    end:   Optional[int] = None,
    host:  Optional[str] = None,
):
    now   = int(time.time())
    end   = end   or now
    start = start or (now - 3600)

    q = "SELECT host,timestamp,success,latency_ms FROM pings WHERE timestamp BETWEEN ? AND ?"
    p: List = [start, end]
    if host:
        q += " AND host=?"
        p.append(host)
    q += " ORDER BY timestamp"

    async with aiosqlite.connect(DB_PATH) as db:
        async with db.execute(q, p) as cur:
            rows = await cur.fetchall()

    return [{"host": r[0], "ts": r[1], "ok": bool(r[2]), "ms": r[3]} for r in rows]


@app.get("/api/drops")
async def api_drops(
    start: Optional[int] = None,
    end:   Optional[int] = None,
):
    now   = int(time.time())
    end   = end   or now
    start = start or (now - 3600)

    async with aiosqlite.connect(DB_PATH) as db:
        async with db.execute("SELECT host FROM hosts") as cur:
            all_hosts: Set[str] = {r[0] async for r in cur}
        async with db.execute(
            "SELECT timestamp,host,success FROM pings "
            "WHERE timestamp BETWEEN ? AND ? ORDER BY timestamp",
            [start, end],
        ) as cur:
            rows = await cur.fetchall()

    by_ts: Dict[int, Dict[str, bool]] = defaultdict(dict)
    for ts, h, ok in rows:
        by_ts[ts][h] = bool(ok)

    events = []
    for ts in sorted(by_ts):
        statuses = by_ts[ts]
        failed   = [h for h, ok in statuses.items() if not ok]
        if failed:
            events.append({
                "ts":       ts,
                "failed":   failed,
                "total":    len(all_hosts),
                "polled":   len(statuses),
                "all_down": len(failed) == len(statuses) and len(statuses) > 0,
            })
    return events


@app.get("/api/export/history.csv")
async def export_history_csv(
    start: Optional[int] = None,
    end:   Optional[int] = None,
):
    async with aiosqlite.connect(DB_PATH) as db:
        async with db.execute("SELECT host,label FROM hosts") as cur:
            labels: Dict[str, str] = {r[0]: r[1] async for r in cur}

        if start is not None or end is not None:
            now   = int(time.time())
            end   = end   or now
            start = start or (now - 3600)
            async with db.execute(
                "SELECT host,timestamp,success,latency_ms FROM pings "
                "WHERE timestamp BETWEEN ? AND ? ORDER BY timestamp,host",
                [start, end],
            ) as cur:
                rows = await cur.fetchall()
        else:
            async with db.execute(
                "SELECT host,timestamp,success,latency_ms FROM pings "
                "ORDER BY timestamp,host"
            ) as cur:
                rows = await cur.fetchall()

    buf = io.StringIO()
    w = csv.writer(buf)
    w.writerow(["timestamp", "datetime", "host", "label", "success", "latency_ms"])
    for host, ts, success, latency in rows:
        dt = time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(ts))
        w.writerow([ts, dt, host, labels.get(host, host),
                    "true" if success else "false",
                    round(latency, 3) if latency is not None else ""])
    buf.seek(0)

    fname = f"pingchecker_history_{time.strftime('%Y%m%d_%H%M%S')}.csv"
    return StreamingResponse(
        iter([buf.getvalue()]),
        media_type="text/csv",
        headers={"Content-Disposition": f'attachment; filename="{fname}"'},
    )


@app.get("/api/export/drops.csv")
async def export_drops_csv(
    start: Optional[int] = None,
    end:   Optional[int] = None,
):
    # No start/end = full history
    if start is None and end is None:
        async with aiosqlite.connect(DB_PATH) as db:
            async with db.execute("SELECT host FROM hosts") as cur:
                all_hosts: Set[str] = {r[0] async for r in cur}
            async with db.execute(
                "SELECT timestamp,host,success FROM pings ORDER BY timestamp"
            ) as cur:
                rows = await cur.fetchall()
        by_ts: Dict[int, Dict[str, bool]] = defaultdict(dict)
        for ts, h, ok in rows:
            by_ts[ts][h] = bool(ok)
        drops = []
        for ts in sorted(by_ts):
            statuses = by_ts[ts]
            failed = [h for h, ok in statuses.items() if not ok]
            if failed:
                drops.append({"ts": ts, "failed": failed,
                               "total": len(all_hosts),
                               "all_down": len(failed) == len(statuses) and len(statuses) > 0})
    else:
        drops = await api_drops(start=start, end=end)

    buf = io.StringIO()
    w = csv.writer(buf)
    w.writerow(["timestamp", "datetime", "severity", "failed_hosts", "hosts_down", "total_hosts"])
    for d in drops:
        dt  = time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(d["ts"]))
        sev = "TOTAL" if d["all_down"] else "PARTIAL"
        w.writerow([d["ts"], dt, sev, "; ".join(d["failed"]),
                    len(d["failed"]), d["total"]])
    buf.seek(0)

    fname = f"pingchecker_drops_{time.strftime('%Y%m%d_%H%M%S')}.csv"
    return StreamingResponse(
        iter([buf.getvalue()]),
        media_type="text/csv",
        headers={"Content-Disposition": f'attachment; filename="{fname}"'},
    )


@app.websocket("/ws")
async def ws_endpoint(ws: WebSocket):
    await manager.connect(ws)
    async with aiosqlite.connect(DB_PATH) as db:
        async with db.execute("SELECT host,label FROM hosts ORDER BY added_at") as cur:
            hosts = [{"host": r[0], "label": r[1]} async for r in cur]
    try:
        await ws.send_json({"type": "hosts", "hosts": hosts})
        await ws.send_json({"type": "settings", "settings": dict(SETTINGS)})
        while True:
            await ws.receive_text()   # keeps connection open; detects disconnect
    except WebSocketDisconnect:
        pass
    finally:
        manager.disconnect(ws)


app.mount("/static", StaticFiles(directory=str(STATIC_DIR)), name="static")


@app.get("/")
async def root():
    return FileResponse(STATIC_DIR / "index.html")


if __name__ == "__main__":
    print("\n  PingChecker running → http://localhost:8765\n")
    uvicorn.run(app, host="0.0.0.0", port=8765, reload=False)
