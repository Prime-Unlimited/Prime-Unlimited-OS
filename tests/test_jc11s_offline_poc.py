"""Real filesystem integration checks; no hardware, SSH or reference archive."""

import shutil
import struct
import subprocess

import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from core.android_sparse import convert, verify_set
from core.fastboot_protocol import FastbootCommandError, FastbootResponse, FastbootSession, ResponseKind
from core.jc11s_flash import (
    MockFlashTransport,
    ProbeEvidence,
    build_for_device,
    flash_hardware,
    prepare,
    probe,
    simulate,
    validate_geometry,
)
from core.jc11s_image import SOURCE, build, digest, public_key, run, validate_hook, verify, verify_integrity
from core.transports import WriteBlockedError
from core.windows_fastboot import (
    ExperimentalFirstFlashAuthorization,
    WindowsWinUsbFastbootTransport,
)


def evidence(size: int, limit: int, **overrides) -> ProbeEvidence:
    values = {
        "version": "0.4",
        "product": "rk3288-az01-jc11s",
        "version-bootloader": "U-Boot 2026.07-inmusic-20260721",
        "secure": "yes",
        "max-download-size": hex(limit),
        "partition-size:data": hex(size),
        "partition-type:data": "ext4",
        "is-userspace": "no",
        "current-slot": "a",
    }
    values.update(overrides)
    return ProbeEvidence(values, ("variant", "unlocked", "partition-size:userdata", "partition-type:userdata"))


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


def test_pure_python_windows_integrity_check(artifacts):
    image = artifacts[3]
    assert verify_integrity(image)["sha256"] == digest(image)


def test_simulated_download_flash_reboot(artifacts):
    image = artifacts[3]
    size = image.stat().st_size
    geometry = validate_geometry(evidence(size, 8 * 1024))
    plan = prepare(image, geometry, artifacts[0] / "sparse-simulation")
    mock = MockFlashTransport()
    simulate(plan, mock)
    assert mock.transferred == sum(path.stat().st_size for path in plan.transfers)
    assert all(path.stat().st_size < geometry.max_download_size for path in plan.transfers)
    assert len(plan.transfers) > 1
    assert mock.commands[-1] == "reboot"
    assert mock.commands.count("flash:data") == len(plan.transfers)
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
    geometry = validate_geometry(evidence(size + 512, 1024 * 1024))
    with pytest.raises(ValueError, match="exact partition-sized"):
        prepare(image, geometry, artifacts[0] / "wrong-size")
    with pytest.raises(WriteBlockedError):
        flash_hardware(verified=True, consent=True)


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("product", "rk3288-other"),
        ("version-bootloader", "U-Boot unknown"),
        ("secure", "no"),
        ("current-slot", "invalid"),
        ("partition-type:data", "raw"),
        ("partition-size:data", "missing"),
        ("partition-size:data", "0x10001"),
        ("max-download-size", "0"),
        ("max-download-size", "0x100000000"),
        ("is-userspace", "yes"),
    ],
)
def test_invalid_or_missing_device_geometry_blocks(field, value):
    item = evidence(80 * 1024**2, 32 * 1024**2)
    if value == "missing":
        del item.values[field]
    else:
        item.values[field] = value
    with pytest.raises(ValueError):
        validate_geometry(item)


@pytest.mark.parametrize(
    ("size", "limit"),
    [(64 * 1024**2, 512 * 1024**2), (64 * 1024**2 + 512, 2 * 1024**2), (96 * 1024**2, 768 * 1024)],
)
def test_geometry_is_per_device_and_never_defaulted(size, limit):
    geometry = validate_geometry(evidence(size, limit))
    assert geometry.partition_size == size
    assert geometry.max_download_size == limit
    assert geometry.partition == "data"


def test_sparse_converter_splits_dense_data_and_reconstructs(tmp_path):
    raw = tmp_path / "dense.raw"
    # Deterministic non-zero sectors force RAW chunks and multiple transfers.
    raw.write_bytes(bytes((index % 251) + 1 for index in range(3 * 1024**2)))
    directory = tmp_path / "sparse"
    manifest = convert(raw, directory, 512 * 1024)
    assert len(manifest["parts"]) > 1
    assert all(part["size"] < 512 * 1024 for part in manifest["parts"])
    assert verify_set(directory) == manifest


def test_sparse_zero_fill_reproduces_fresh_raw_not_old_storage(tmp_path):
    raw = tmp_path / "zeros.raw"
    raw.write_bytes(bytes(2 * 1024**2))
    manifest = convert(raw, tmp_path / "sparse", 64 * 1024)
    # FILL(0) makes the transmitted file tiny but the verifier requires every
    # logical sector to be explicitly reproduced across the set.
    assert sum(part["size"] for part in manifest["parts"]) < 1024
    verify_set(tmp_path / "sparse")


def test_complete_dynamic_build_uses_each_device_size(artifacts, tmp_path):
    _, key, hook, _ = artifacts
    results = []
    for index, (size, limit) in enumerate(((64 * 1024**2, 2 * 1024**2), (64 * 1024**2 + 512, 3 * 1024**2))):
        image = tmp_path / f"device-{index}.ext4"
        plan = build_for_device(evidence(size, limit), image, key, hook, tmp_path / f"sparse-{index}")
        results.append(plan)
        assert image.stat().st_size == size
        assert all(path.stat().st_size < limit for path in plan.transfers)
        assert verify(image)["device_geometry"]["partition_size"] == size
    assert results[0].size != results[1].size


def test_hardware_transport_failure_never_reboots(artifacts, monkeypatch):
    image = artifacts[3]
    size = image.stat().st_size
    geometry = validate_geometry(evidence(size, 1024 * 1024))
    plan = prepare(image, geometry, artifacts[0] / "hardware-failure-sparse")

    class Connection:
        closed = False
        timeouts = []

        def set_timeout(self, pipe, timeout):
            self.timeouts.append((pipe, timeout))

        def close(self):
            self.closed = True

    class Session:
        rebooted = False

        def getvar(self, name):
            value = evidence(size, 1024 * 1024).values.get(name)
            if value is None:
                raise FastbootCommandError("unsupported")
            return FastbootResponse(ResponseKind.OKAY, value)

        def download_file(self, path, maximum_size, progress=None):
            return FastbootResponse(ResponseKind.OKAY, "")

        def flash(self, partition, sent_callback=None):
            if sent_callback:
                sent_callback()
            raise FastbootCommandError("simulated bootloader rejection")

        def reboot(self, sent_callback=None):
            if sent_callback:
                sent_callback()
            self.rebooted = True
            return FastbootResponse(ResponseKind.OKAY, "")

    connection = Connection()
    session = Session()
    transport = WindowsWinUsbFastbootTransport(object())
    monkeypatch.setattr(transport, "_single_interface", lambda identifier=None: object())
    monkeypatch.setattr(transport, "_open_session", lambda interface: (connection, session, 0x81, 0x02))
    authorization = ExperimentalFirstFlashAuthorization(
        True,
        True,
        ExperimentalFirstFlashAuthorization.RECOVERY_PHRASE,
        ExperimentalFirstFlashAuthorization.EXPERIMENT_PHRASE,
        True,
    )
    with pytest.raises(FastbootCommandError, match="rejection"):
        transport.flash_sparse_plan(plan, authorization)
    assert not session.rebooted
    assert connection.closed


def test_experimental_transport_rejects_wrong_device_before_transfer(artifacts, monkeypatch):
    image = artifacts[3]
    size = image.stat().st_size
    geometry = validate_geometry(evidence(size, 1024 * 1024))
    plan = prepare(image, geometry, artifacts[0] / "wrong-device-sparse")
    wrong = evidence(size, 1024 * 1024)
    wrong.values["product"] = "rk3288-not-jc11s"

    class Connection:
        closed = False
        timeouts = []

        def set_timeout(self, pipe, timeout):
            self.timeouts.append((pipe, timeout))

        def close(self):
            self.closed = True

    class Session:
        downloaded = False
        rebooted = False

        def download_file(self, *args, **kwargs):
            self.downloaded = True

        def reboot(self):
            self.rebooted = True

    connection = Connection()
    session = Session()
    transport = WindowsWinUsbFastbootTransport(object())
    monkeypatch.setattr(transport, "_single_interface", lambda identifier=None: object())
    monkeypatch.setattr(transport, "_open_session", lambda interface: (connection, session, 0x81, 0x02))
    monkeypatch.setattr("core.jc11s_flash.probe", lambda current: wrong)
    authorization = ExperimentalFirstFlashAuthorization(
        True,
        True,
        ExperimentalFirstFlashAuthorization.RECOVERY_PHRASE,
        ExperimentalFirstFlashAuthorization.EXPERIMENT_PHRASE,
        True,
    )
    with pytest.raises(ValueError, match="not the measured JC11S"):
        transport.flash_sparse_plan(plan, authorization)
    assert not session.downloaded and not session.rebooted
    assert connection.closed


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
