#!/bin/bash
# PingChecker — run on macOS / Linux
cd "$(dirname "$0")"

if [ ! -d ".venv" ]; then
    echo "Creating virtual environment..."
    python3 -m venv .venv
fi

echo "Checking dependencies..."
.venv/bin/pip install -r requirements.txt --quiet

echo "Starting PingChecker → http://localhost:8765"
# Open browser in background (macOS: open, Linux: xdg-open)
(sleep 2 && (open http://localhost:8765 2>/dev/null || xdg-open http://localhost:8765 2>/dev/null)) &

.venv/bin/python ping_checker.py
