# Build from the repository root: pyinstaller packaging/prime-unlimited-os.spec
import os

from PyInstaller.utils.hooks import collect_data_files

project_root = os.path.abspath(os.path.join(SPECPATH, ".."))
datas = collect_data_files("app")
# Bundle the JC11S bootstrap sources and JP24 firmware-update helper sources.
# Compiled ARMv7 hook and Windows e2fsprogs must be supplied by a verified build step.
for folder in (
    "bootstrap/jc11s",
    "devices/jp24/ssh-bootstrap",
    "modloader/catalog/trust",
    "assets",
    "vendor/windows/e2fsprogs",
):
    source = os.path.join(project_root, folder)
    if os.path.isdir(source):
        datas.append((source, folder))

a = Analysis(
    [os.path.join(project_root, "app", "main.py")],
    pathex=[project_root],
    binaries=[],
    datas=datas,
    hiddenimports=["paramiko", "cryptography"],
    excludes=[],
    noarchive=False,
)
pyz = PYZ(a.pure)
exe = EXE(
    pyz,
    a.scripts,
    a.binaries,
    a.datas,
    [],
    name="Prime-Unlimited-OS",
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=True,
    console=False,
)
