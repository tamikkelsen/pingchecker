@echo off
:: ============================================================
:: PingChecker — run with Python (no build required)
:: Requires Python 3.9+ installed and on PATH
:: ============================================================

echo  Starting PingChecker...

if not exist .venv (
    echo  Creating virtual environment...
    python -m venv .venv
)

echo  Checking dependencies...
.venv\Scripts\pip install -r requirements.txt --quiet

echo  Opening http://localhost:8765 ...
start "" http://localhost:8765

.venv\Scripts\python ping_checker.py
pause
