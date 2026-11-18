import hashlib, json, subprocess, sys, zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def test_qml_touch_drag_and_monotonic_contract():
    qml = (ROOT / "src/ui/PrimeClock.qml").read_text()
    for value in ("pressAndHoldInterval: 600", "onPressAndHold", "mapToItem(overlay", "clampX", "clampY", "positionSaveRequested", "/api/v1/monotonic"):
        assert value in qml

def test_stopwatch_logic_with_monotonic_inputs():
    source = (ROOT / "src/common/Stopwatch.js").read_text().replace(".pragma library", "")
    program = source + "\nlet s=fresh(); play(s,1000); if(elapsed(s,2500)!==1500) process.exit(1); pause(s,3000); play(s,9000); pause(s,10000); if(elapsed(s,10000)!==3000||formatElapsed(3661000)!=='01:01:01') process.exit(2); if(reset().running) process.exit(3);\n"
    subprocess.run(["node", "-e", program], check=True)

def test_reproducible_packages(tmp_path):
    for target in ("jc11s", "jp24"):
        outputs = []
        for index in (1, 2):
            directory = tmp_path / str(index)
            subprocess.run([sys.executable, ROOT / "scripts/package.py", "--target", target, "--output", directory], check=True)
            package = next(directory.glob("*.zip")); outputs.append(hashlib.sha256(package.read_bytes()).hexdigest())
            subprocess.run([sys.executable, ROOT / "scripts/validate_package.py", str(package), "--target", target], check=True)
        assert outputs[0] == outputs[1]

def test_profiles_do_not_overclaim_hardware():
    jc = json.loads((ROOT / "src/platform/jc11s.json").read_text())
    jp = json.loads((ROOT / "src/platform/jp24.json").read_text())
    assert (jc["architecture"], jc["abi"], jc["firmware"]) == ("armv7l", "gnueabihf", ["5.3.0"])
    assert (jp["architecture"], jp["abi"]) == ("aarch64", "aarch64-linux-gnu.2.17")
    assert jc["hardwareVerified"] and not jp["hardwareVerified"] and jp["firmware"] == []
