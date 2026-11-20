import hashlib, json, subprocess, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def test_entrypoint_has_start_stop():
    sh = (ROOT / "src/entrypoint.sh").read_text()
    assert "start)" in sh and "stop)" in sh
    assert "splash-server-aarch64" in sh and "splash-server-armhf" in sh

def test_binaries_exist_and_are_elf():
    for name in ("splash-server-armhf", "splash-server-aarch64"):
        binary = ROOT / "src/bin" / name
        assert binary.exists()
        header = binary.read_bytes()[:4]
        assert header == b"\x7fELF"

def test_reproducible_packages(tmp_path):
    for target in ("jc11s", "jp24"):
        outputs = []
        for index in (1, 2):
            directory = tmp_path / target / str(index)
            subprocess.run(
                [sys.executable, ROOT / "scripts/package.py", "--target", target, "--output", directory],
                check=True,
            )
            package = next(directory.glob("*.zip"))
            outputs.append(hashlib.sha256(package.read_bytes()).hexdigest())
            subprocess.run(
                [sys.executable, ROOT / "scripts/validate_package.py", str(package), "--target", target],
                check=True,
            )
        assert outputs[0] == outputs[1]

def test_profiles_do_not_overclaim_hardware():
    jc = json.loads((ROOT / "src/platform/jc11s.json").read_text())
    jp = json.loads((ROOT / "src/platform/jp24.json").read_text())
    assert (jc["architecture"], jc["abi"]) == ("armv7l", "gnueabihf")
    assert (jp["architecture"], jp["abi"]) == ("aarch64", "gnu")
    assert not jc["hardwareVerified"] and not jp["hardwareVerified"]


def test_restricted_launcher_fails_before_any_service_action():
    import os
    result = subprocess.run(
        ['sh', '-s'], input=(ROOT / 'src/entrypoint.sh').read_text(),
        env=dict(os.environ, PRIME_UNLIMITED_MOD_ID='splash-manager'),
        capture_output=True, text=True,
    )
    assert result.returncode != 0
    assert 'trusted service adapter' in result.stderr


def test_managed_server_security_contract():
    subprocess.run(["go", "test", "./..."], cwd=ROOT / "src/server", check=True)
