#!/usr/bin/env python3
import argparse, json, stat, zipfile
from pathlib import Path, PurePosixPath

BINARY = {"jc11s": "splash-server-armhf", "jp24": "splash-server-aarch64"}

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("package", type=Path)
    parser.add_argument("--target", required=True)
    args = parser.parse_args()
    binary_name = BINARY[args.target]
    required = {
        "splash-manager/start.sh",
        f"splash-manager/bin/{binary_name}",
        "splash-manager/platform.json",
    }
    with zipfile.ZipFile(args.package) as archive:
        names = archive.namelist()
        assert set(names) == required and len(names) == len(set(names))
        for info in archive.infolist():
            path = PurePosixPath(info.filename)
            assert not path.is_absolute() and ".." not in path.parts and not info.is_dir()
            perms = stat.S_IMODE(info.external_attr >> 16)
            if info.filename.endswith(".sh") or "/bin/" in info.filename:
                assert perms == 0o755
            else:
                assert perms == 0o600
            assert info.file_size <= 10 * 1024 * 1024
        profile = json.loads(archive.read("splash-manager/platform.json"))
        assert profile["model"].lower() == args.target
    print(f"validated {args.package}")

if __name__ == "__main__":
    main()
