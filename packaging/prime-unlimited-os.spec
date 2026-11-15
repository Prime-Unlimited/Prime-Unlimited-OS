# Build from the repository root: pyinstaller packaging/prime-unlimited-os.spec
import os
import sys

from PyInstaller.utils.hooks import collect_data_files

project_root = os.path.abspath(os.path.join(SPECPATH, ".."))
datas = collect_data_files("app")
datas.append((os.path.join(project_root, "assets", "prime-unlimited-os-brand.png"), "assets"))
# WSL executes these source files directly; PyInstaller's Python archive is not
# addressable from the Linux environment.
for source_dir in ("core", "bootstrap", "tools"):
    for root, _, files in os.walk(os.path.join(project_root, source_dir)):
        destination = os.path.relpath(root, project_root)
        for filename in files:
            if filename.endswith((".py", ".sh", ".c", ".conf")):
                datas.append((os.path.join(root, filename), destination))
hiddenimports = ["paramiko", "cryptography"]
if sys.platform == "win32":
    hiddenimports.extend(["core.windows_usb_discovery", "core.windows_winusb_native"])

a = Analysis(
    [os.path.join(project_root, "app", "main.py")],
    pathex=[project_root],
    binaries=[],
    datas=datas,
    hiddenimports=hiddenimports,
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
