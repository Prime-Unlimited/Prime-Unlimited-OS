"""Real filesystem integration checks; no hardware, SSH or reference archive."""

import shutil
import struct
import subprocess

import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from core.fastboot_protocol import FastbootSession
from core.jc11s_flash import MockFlashTransport, flash_hardware, prepare, probe, simulate
from core.jc11s_image import SOURCE, build, digest, public_key, run, validate_hook, verify
from core.transports import WriteBlockedError


@pytest.fixture(scope="module")
def artifacts(tmp_path_factory):
    # Fail rather than silently skip the PoC readiness suite when tools are missing.
    for tool in ("mke2fs", "debugfs", "e2fsck", "arm-linux-gnueabihf-gcc", "arm-linux-gnueabihf-readelf"):
        assert shutil.which(tool), f"Required offline PoC tool missing: {tool}"
    root = tmp_path_factory.mktemp("offline-poc")
    key = root / "user.pub"
    key.write_bytes(
        Ed25519PrivateKey.generate()
        .public_key()
        .public_bytes(serialization.Encoding.OpenSSH, serialization.PublicFormat.OpenSSH)
        + b" selected user\n"
    )
    hook = root / "hook.so"
    subprocess.run(["sh", "tools/build-jc11s-hook.sh", str(hook)], check=True)
    image = root / "data.ext4"
    build(image, key, hook, 64 * 1024**2 + 512)
    return root, key, hook, image


def test_hook_abi_and_no_runtime_dependencies(artifacts):
    _, _, hook, _ = artifacts
    validate_hook(hook)
    result = run(["arm-linux-gnueabihf-readelf", "-dWs", str(hook)])
    assert "(NEEDED)" not in result
    assert "_prime_ssh_hook_dlsym_config_hook_001" in result
    assert "prime_ssh_hook" in result
    assert " UND " not in "\n".join(
        line for line in result.splitlines() if line.strip() and not line.strip().startswith("0:")
    )


def test_ext4_payload_permissions_and_reproducibility(artifacts):
    root, key, hook, image = artifacts
    manifest = verify(image)
    assert not manifest["boot_validated"]
    for name, mode in (
        ("prime-ssh", "0700"),
        ("prime-ssh/authorized_keys", "0600"),
        ("prime-ssh/activate.sh", "0700"),
        ("system/var/overlay/lib/alsa/conf.d", "0755"),
    ):
        stat = run(["debugfs", "-R", f"stat /{name}", str(image)])
        assert f"Mode:  {mode}" in stat
        assert "User:     0   Group:     0" in stat
    second = root / "second.ext4"
    build(second, key, hook, image.stat().st_size)
    assert digest(second) == digest(image)
    assert all("enginex" not in name.lower() for name in manifest["files"])
    assert len(manifest["files"]) == 5
    config = (SOURCE / "sshd_config").read_text()
    assert "AuthenticationMethods publickey" in config
    assert "PasswordAuthentication no" in config
    assert "ChallengeResponseAuthentication no" in config
    assert "Include" not in config


def test_image_corruption_detected(artifacts, tmp_path):
    _, _, _, source = artifacts
    image = tmp_path / "corrupt.ext4"
    shutil.copyfile(source, image)
    shutil.copyfile(str(source) + ".json", str(image) + ".json")
    with image.open("r+b") as stream:
        stream.seek(4096)
        stream.write(b"corrupt")
    with pytest.raises(ValueError, match="SHA256"):
        verify(image)


def test_simulated_download_flash_reboot(artifacts):
    image = artifacts[3]
    size = image.stat().st_size
    plan = prepare(image, "data", size, size)
    mock = MockFlashTransport()
    simulate(plan, mock)
    assert mock.transferred == size
    assert mock.commands == [f"download:{size:08x}", "flash:data", "reboot"]
    for failure in ("transfer", "flash", "download", "download-complete"):
        broken = MockFlashTransport(fail_at=failure)
        with pytest.raises(RuntimeError):
            simulate(plan, broken)
        assert "reboot" not in broken.commands
    with pytest.raises(TypeError):
        simulate(plan, object())


def test_limits_and_hardware_lock(artifacts):
    image = artifacts[3]
    size = image.stat().st_size
    for partition, partition_size, limit in (
        ("/data", size, size),
        ("userdata", size + 4096, size),
        ("data", size, size - 1),
    ):
        with pytest.raises(ValueError):
            prepare(image, partition, partition_size, limit)
    with pytest.raises(WriteBlockedError):
        flash_hardware(verified=True, consent=True)


def test_readonly_probe_keeps_names_separate():
    class Io:
        def __init__(self):
            self.commands = []

        def write(self, data):
            self.commands.append(data)
            return len(data)

        def read(self, size=64):
            return {
                b"getvar:version": b"OKAY0.4",
                b"getvar:partition-type:data": b"OKAYext4",
                b"getvar:partition-size:data": b"OKAY0x8fd7be00",
            }.get(self.commands[-1], b"FAILunknown")

    io = Io()
    evidence = probe(FastbootSession(io))
    assert evidence.values["partition-type:data"] == "ext4"
    assert "partition-type:userdata" in evidence.unsupported
    assert not evidence.linux_mapping_verified and not evidence.flashing_verified
    assert all(command.startswith(b"getvar:") for command in io.commands)


@pytest.mark.parametrize("value", [b"not a key", b"ssh-ed25519 garbage", b"ssh-ed25519 x\nssh-ed25519 y"])
def test_invalid_public_keys(tmp_path, value):
    key = tmp_path / "key"
    key.write_bytes(value)
    with pytest.raises(ValueError):
        public_key(key)


def test_refuse_wrong_arch_and_existing_output(artifacts, tmp_path):
    _, key, hook, image = artifacts
    wrong = tmp_path / "wrong.so"
    data = bytearray(hook.read_bytes())
    struct.pack_into("<H", data, 18, 183)  # ARM64 is explicitly out of scope.
    wrong.write_bytes(data)
    with pytest.raises(ValueError, match="ARM"):
        validate_hook(wrong)
    with pytest.raises(FileExistsError):
        build(image, key, hook, image.stat().st_size)


@pytest.mark.parametrize("configuration_valid", [True, False])
def test_activation_isolated_service_and_failure_closed(tmp_path, configuration_valid):
    """Execute actual shell logic in a sandbox; never invoke host systemctl/sshd."""
    import os

    base = tmp_path / "prime-ssh"
    (base / "hostkeys").mkdir(parents=True)
    (base / "hostkeys/ssh_host_ed25519_key").write_text("fixture-existing-host-key")
    (base / "authorized_keys").write_text("fixture-selected-public-key")
    runtime = tmp_path / "run"
    runtime.mkdir()
    comm = tmp_path / "comm"
    comm.write_text("systemd\n")
    log = tmp_path / "commands"
    binaries = tmp_path / "bin"
    binaries.mkdir()
    for command, body in {
        "id": "echo 0",
        "chown": "exit 0",
        "systemctl": 'printf "%s\\n" "$*" >> "$POC_TEST_LOG"',
        "sshd": 'printf "sshd %s\\n" "$*" >> "$POC_TEST_LOG"; exit ' + ("0" if configuration_valid else "1"),
    }.items():
        executable = binaries / command
        executable.write_text("#!/bin/sh\n" + body + "\n")
        executable.chmod(0o755)
    script = (SOURCE / "activate.sh").read_text()
    script = script.replace("/data/prime-ssh", str(base)).replace("/run/", str(runtime) + "/")
    script = script.replace("/proc/1/comm", str(comm)).replace("/usr/sbin/sshd", str(binaries / "sshd"))
    activation = tmp_path / "activate.sh"
    activation.write_text(script)
    environment = dict(os.environ, PATH=str(binaries) + ":" + os.environ["PATH"], POC_TEST_LOG=str(log))
    result = subprocess.run(["sh", str(activation)], env=environment, check=False)
    assert result.returncode == (0 if configuration_valid else 1)
    commands = log.read_text().splitlines()
    assert commands[0] == f"sshd -t -f {base}/sshd_config"
    if configuration_valid:
        assert commands[1:] == ["daemon-reload", "restart sshd.service"]
        subprocess.run(["sh", str(activation)], env=environment, check=True)
        assert log.read_text().splitlines() == commands  # idempotent per boot
        dropin = (runtime / "systemd/system/sshd.service.d/99-prime-ssh.conf").read_text()
        assert f"ExecStart={binaries}/sshd -D -e -f {base}/sshd_config" in dropin
    else:
        assert len(commands) == 1
        assert not (runtime / "systemd/system/sshd.service.d/99-prime-ssh.conf").exists()
    assert not (runtime / "prime-ssh.lock").exists()
