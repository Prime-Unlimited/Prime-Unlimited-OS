#!/usr/bin/env python3
import argparse, hashlib, json, shutil, stat, zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", required=True, choices=("jc11s", "jp24"))
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    version = (ROOT / "VERSION").read_text().strip()
    profile = json.loads((ROOT / "src/platform" / f"{args.target}.json").read_text())
    name = f"prime-clock-v{version}-{args.target}.zip"
    args.output.mkdir(parents=True, exist_ok=True)
    destination = args.output / name
    files = {
        "prime-clock/start.sh": (ROOT / "src/entrypoint.sh").read_bytes(),
        "prime-clock/ui/PrimeClock.qml": (ROOT / "src/ui/PrimeClock.qml").read_bytes(),
        "prime-clock/common/Stopwatch.js": (ROOT / "src/common/Stopwatch.js").read_bytes(),
        "prime-clock/platform.json": (json.dumps(profile, sort_keys=True, separators=(",", ":")) + "\n").encode(),
    }
    with zipfile.ZipFile(destination, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path, data in sorted(files.items()):
            info = zipfile.ZipInfo(path, (2026, 1, 1, 0, 0, 0))
            info.create_system = 3; info.external_attr = (stat.S_IFREG | 0o600) << 16
            archive.writestr(info, data, compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    digest = hashlib.sha256(destination.read_bytes()).hexdigest()
    print(json.dumps({"path": str(destination), "size": destination.stat().st_size, "sha256": digest, "profile": profile}, sort_keys=True))

if __name__ == "__main__": main()
