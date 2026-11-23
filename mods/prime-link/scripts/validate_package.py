#!/usr/bin/env python3
import argparse
import json
import stat
import zipfile
from pathlib import Path, PurePosixPath

REQUIRED = {"prime-link/start.sh", "prime-link/ui/DeckTelemetry.qml", "prime-link/platform.json"}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("package", type=Path)
    parser.add_argument("--target", required=True, choices=("jc11s", "jp24"))
    args = parser.parse_args()
    with zipfile.ZipFile(args.package) as archive:
        names = archive.namelist()
        assert set(names) == REQUIRED and len(names) == len(set(names))
        for info in archive.infolist():
            path = PurePosixPath(info.filename)
            assert not path.is_absolute() and ".." not in path.parts and not info.is_dir()
            assert stat.S_IMODE(info.external_attr >> 16) == 0o600
            assert info.file_size <= 1024 * 1024
        profile = json.loads(archive.read("prime-link/platform.json"))
        assert profile["model"].lower() == args.target
        qml = archive.read("prime-link/ui/DeckTelemetry.qml").decode()
        assert "import QtQuick 2.12" in qml and "import Planck 1.0" in qml
        assert "/GUI/Decks/ActiveDecks" in qml
    print(f"validated {args.package}")


if __name__ == "__main__":
    main()
