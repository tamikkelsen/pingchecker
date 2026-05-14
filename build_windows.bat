@echo off
:: ============================================================
:: PingChecker — Windows build script
:: Run this ONCE on a Windows machine to produce PingChecker.exe
:: Requires Python 3.9+ installed and on PATH
:: Output: dist\PingChecker.exe  (single file, no install needed)
:: ============================================================

echo.
echo  PingChecker — Windows build
echo  ============================

:: Create/activate virtual environment
if not exist .venv (
    echo  Creating virtual environment...
    python -m venv .venv
)

echo  Installing dependencies...
.venv\Scripts\pip install -r requirements.txt pyinstaller --quiet

echo  Building PingChecker.exe ...
.venv\Scripts\pyinstaller ping_checker.spec --clean --noconfirm

echo.
if exist dist\PingChecker.exe (
    echo  SUCCESS — dist\PingChecker.exe is ready.
    echo  Copy PingChecker.exe anywhere and double-click to run.
    echo  pings.db and config.json will be created next to the .exe.
) else (
    echo  BUILD FAILED — check the output above for errors.
)

echo.
pause
