# -*- mode: python ; coding: utf-8 -*-
"""PyInstaller recipe for the standalone ``pychat`` binary.

``genai-prices`` and ``pydantic-ai-slim`` read their own distribution metadata
at import time, so it has to travel with the frozen application. ``ai-chat``'s
metadata backs the ``--version`` flag.
"""

import os

from PyInstaller.utils.hooks import copy_metadata

datas = [
    *copy_metadata("ai-chat"),
    *copy_metadata("genai-prices"),
    *copy_metadata("pydantic-ai-slim"),
]

analysis = Analysis(
    [os.path.join(SPECPATH, "packaging", "entry.py")],
    pathex=[],
    binaries=[],
    datas=datas,
    hiddenimports=[],
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=[],
    noarchive=False,
    optimize=0,
)
pyz = PYZ(analysis.pure)

exe = EXE(
    pyz,
    analysis.scripts,
    analysis.binaries,
    analysis.datas,
    [],
    name="pychat",
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=False,
    upx_exclude=[],
    runtime_tmpdir=None,
    console=True,
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
)
