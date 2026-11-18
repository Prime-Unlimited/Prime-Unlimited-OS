#!/usr/bin/env python3
import argparse, json, os, pathlib, subprocess, zipfile

ROOT = pathlib.Path(__file__).resolve().parents[2]

def build(target, output):
    cfg = {"jc11s": ("arm", "7"), "jp24": ("arm64", "")}[target]
    version = json.loads((ROOT / "modloader/VERSION.json").read_text())
    work = output / "work"
    (work / "bin").mkdir(parents=True, exist_ok=True)
    env = os.environ.copy() | {"GOOS":"linux", "GOARCH":cfg[0], "CGO_ENABLED":"0", "GO111MODULE":"off"}
    if cfg[1]: env["GOARM"] = cfg[1]
    subprocess.run(["go","build","-trimpath","-ldflags=-s -w -buildid=","-o",str(work/"bin/prime-unlimited-runtime-api"),"."],cwd=ROOT/"modloader/device/jc11s/runtime-api",env=env,check=True)
    env.pop("GO111MODULE",None)
    subprocess.run(["go","build","-trimpath","-ldflags=-s -w -buildid=","-o",str(work/"bin/prime-unlimited-self-updater"),"."],cwd=ROOT/"modloader/updater",env=env,check=True)
    files = {
      ROOT/f"modloader/device/{target}/runtime-api/start.sh": work/"bin/modloader-api-start.sh",
      ROOT/f"modloader/device/{target}/updater/start.sh": work/"bin/modloader-updater-start.sh",
      ROOT/"modloader/ui/ModLoader.qml": work/"ui/ModLoader.template.qml",
      ROOT/"modloader/ui/OverlayHost.qml": work/"ui/OverlayHost.template.qml",
      ROOT/"modloader/VERSION.json": work/"VERSION.json",
    }
    for source,dest in files.items(): dest.parent.mkdir(parents=True,exist_ok=True); dest.write_bytes(source.read_bytes())
    archive=output/f"prime-unlimited-modloader-v{version['version']}-r{version['revision']}-{target}.zip"
    with zipfile.ZipFile(archive,"w",zipfile.ZIP_DEFLATED,compresslevel=9) as z:
      for path in sorted(p for p in work.rglob("*") if p.is_file()):
        name=path.relative_to(work).as_posix(); info=zipfile.ZipInfo(name,(2020,1,1,0,0,0)); info.compress_type=zipfile.ZIP_DEFLATED; info.external_attr=((0o755 if name.startswith("bin/") else 0o644)&0xFFFF)<<16; z.writestr(info,path.read_bytes())
    return archive

if __name__ == "__main__":
    p=argparse.ArgumentParser();p.add_argument("--target",choices=("jc11s","jp24"),required=True);p.add_argument("--output",type=pathlib.Path,required=True);a=p.parse_args();a.output.mkdir(parents=True,exist_ok=True);print(build(a.target,a.output))
