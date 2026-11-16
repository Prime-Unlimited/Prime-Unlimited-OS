#!/usr/bin/env python3
"""Validated startup-manifest runner. It performs no network operations."""
import argparse
import json
import os
import subprocess
import sys
from pathlib import Path


def contained(root, relative, require_file=False):
    if not isinstance(relative, str) or not relative or relative.startswith("/") or "\\" in relative:
        raise ValueError("invalid relative path")
    target = (root / relative).resolve()
    if not target.is_relative_to(root.resolve()) or target.is_symlink():
        raise ValueError("path escapes managed root")
    if require_file and not target.is_file():
        raise ValueError("entrypoint is not a file")
    return target


def activate(root, safe_mode=False, timeout=15, runner=subprocess.run, log=None):
    root = Path(root).resolve()
    output = log or sys.stderr
    if safe_mode:
        print("modloader: safe mode; activation skipped", file=output)
        return []
    manifest = root / "registry" / "startup.json"
    data = json.loads(manifest.read_text(encoding="utf-8"))
    if data.get("schemaVersion") != 1 or not isinstance(data.get("enabled"), list):
        raise ValueError("invalid startup manifest")
    results = []
    for item in data["enabled"]:
        ident = item.get("id", "unknown")
        try:
            modroot = contained(root, item["root"])
            entry = contained(modroot, item["entrypoint"], True)
            libraries = [str(contained(root, path)) for path in item.get("libraries", [])]
            env = os.environ.copy()
            env["LD_LIBRARY_PATH"] = os.pathsep.join(libraries)
            result = runner([str(entry)], cwd=str(modroot), env=env, timeout=timeout,
                            check=False, stdin=subprocess.DEVNULL)
            code = int(result.returncode)
            results.append((ident, code))
            if code:
                print("modloader: %s exited %d" % (ident, code), file=output)
        except Exception as exc:
            results.append((ident, None))
            print("modloader: %s activation failed: %s" % (ident, exc), file=output)
    return results


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=os.environ.get("MODLOADER_ROOT", "/data/prime-unlimited"))
    parser.add_argument("--safe-mode", action="store_true",
                        default=os.environ.get("MODLOADER_SAFE_MODE") == "1")
    parser.add_argument("--timeout", type=int, default=15)
    args = parser.parse_args()
    activate(args.root, args.safe_mode, max(1, min(args.timeout, 60)))


if __name__ == "__main__":
    main()
