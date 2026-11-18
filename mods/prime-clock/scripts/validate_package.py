#!/usr/bin/env python3
import argparse, json, stat, zipfile
from pathlib import Path, PurePosixPath

REQUIRED = {"prime-clock/start.sh", "prime-clock/ui/PrimeClock.qml", "prime-clock/common/Stopwatch.js", "prime-clock/platform.json"}
def main():
    parser = argparse.ArgumentParser(); parser.add_argument("package", type=Path); parser.add_argument("--target", required=True)
    args = parser.parse_args()
    with zipfile.ZipFile(args.package) as archive:
        names = archive.namelist()
        assert set(names) == REQUIRED and len(names) == len(set(names))
        for info in archive.infolist():
            path = PurePosixPath(info.filename)
            assert not path.is_absolute() and ".." not in path.parts and not info.is_dir()
            assert stat.S_IMODE(info.external_attr >> 16) == 0o600
            assert info.file_size <= 1024 * 1024
        profile = json.loads(archive.read("prime-clock/platform.json"))
        assert profile["model"].lower() == args.target
        qml = archive.read("prime-clock/ui/PrimeClock.qml").decode()
        assert "QtQuick 2.12" in qml and "/api/v1/monotonic" in qml
    print(f"validated {args.package}")
if __name__ == "__main__": main()
