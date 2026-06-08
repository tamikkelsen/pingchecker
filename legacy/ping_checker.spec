# -*- mode: python ; coding: utf-8 -*-
# PyInstaller spec for PingChecker
# Build on Windows: run build_windows.bat

from PyInstaller.utils.hooks import collect_all, collect_submodules

# Collect every submodule from packages that use dynamic imports
datas_extra, binaries_extra, hiddenimports_extra = [], [], []
for pkg in ('uvicorn', 'fastapi', 'starlette', 'anyio', 'websockets',
            'aiosqlite', 'pydantic', 'h11', 'click', 'httptools'):
    d, b, h = collect_all(pkg)
    datas_extra     += d
    binaries_extra  += b
    hiddenimports_extra += h

a = Analysis(
    ['ping_checker.py'],
    pathex=[],
    binaries=binaries_extra,
    datas=[
        ('../static',      'static'),   # dashboard HTML/JS (lives in repo root)
        ('../config.json', '.'),        # default host list (lives in repo root)
    ] + datas_extra,
    hiddenimports=hiddenimports_extra + [
        'anyio._backends._asyncio',
        'uvicorn.logging',
        'uvicorn.loops',
        'uvicorn.loops.auto',
        'uvicorn.loops.asyncio',
        'uvicorn.protocols',
        'uvicorn.protocols.http',
        'uvicorn.protocols.http.auto',
        'uvicorn.protocols.http.h11_impl',
        'uvicorn.protocols.websockets',
        'uvicorn.protocols.websockets.auto',
        'uvicorn.protocols.websockets.websockets_impl',
        'uvicorn.lifespan',
        'uvicorn.lifespan.on',
        'uvicorn.lifespan.off',
    ],
    hookspath=[],
    runtime_hooks=[],
    excludes=['tkinter', 'matplotlib', 'numpy', 'PIL'],
    noarchive=False,
)

pyz = PYZ(a.pure)

exe = EXE(
    pyz,
    a.scripts,
    a.binaries,
    a.zipfiles,
    a.datas,
    [],
    name='PingChecker',
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=True,
    upx_exclude=[],
    runtime_tmpdir=None,
    console=True,          # keep console so users see the server log
    icon=None,
)
