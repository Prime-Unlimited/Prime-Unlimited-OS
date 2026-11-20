#!/usr/bin/env python3
import argparse, hashlib, json, os, stat, subprocess, tempfile, zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

BINARY = {"jc11s": "splash-server-armhf", "jp24": "splash-server-aarch64"}

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", required=True, choices=("jc11s", "jp24"))
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    version = (ROOT / "VERSION").read_text().strip()
    profile = json.loads((ROOT / "src/platform" / f"{args.target}.json").read_text())
    name = f"splash-manager-v{version}-{args.target}.zip"
    args.output.mkdir(parents=True, exist_ok=True)
    destination = args.output / name
    binary_name = BINARY[args.target]
    with tempfile.TemporaryDirectory(prefix="splash-build-") as build_dir:
        binary = Path(build_dir) / binary_name
        env = dict(os.environ, CGO_ENABLED="0", GOOS="linux",
                   GOARCH="arm" if args.target == "jc11s" else "arm64", GOARM="7")
        subprocess.run(["go", "build", "-trimpath", "-buildvcs=false",
                        "-ldflags=-s -w -buildid=", "-o", str(binary), "."],
                       cwd=ROOT / "src/server", env=env, check=True)
        binary_data = binary.read_bytes()
    # Candidate signed catalog metadata. The runtime trusts only the signed
    # catalog service declaration, never platform.json alone.
    profile["service"] = {"binary": f"splash-manager/bin/{binary_name}",
                          "sha256": hashlib.sha256(binary_data).hexdigest(),
                          "capability": "splash-image"}
    files = {
        "splash-manager/start.sh": (ROOT / "src/entrypoint.sh").read_bytes(),
        f"splash-manager/bin/{binary_name}": binary_data,
        "splash-manager/platform.json": (json.dumps(profile, sort_keys=True, separators=(",", ":")) + "\n").encode(),
    }
    with zipfile.ZipFile(destination, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path, data in sorted(files.items()):
            info = zipfile.ZipInfo(path, (2026, 1, 1, 0, 0, 0))
            info.create_system = 3
            if path.endswith(".sh") or "/bin/" in path:
                info.external_attr = (stat.S_IFREG | 0o755) << 16
            else:
                info.external_attr = (stat.S_IFREG | 0o600) << 16
            archive.writestr(info, data, compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    digest = hashlib.sha256(destination.read_bytes()).hexdigest()
    print(json.dumps({"path": str(destination), "size": destination.stat().st_size, "sha256": digest, "profile": profile}, sort_keys=True))

if __name__ == "__main__":
    main()
