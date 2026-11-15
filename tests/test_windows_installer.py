import sys
from pathlib import Path

import pytest

from core.jc11s_flash import FlashPlan, ProbeEvidence
from core.keys import generate_keypair
from core.windows_installer import (
    ExperimentalFirstFlashPolicy,
    InstallerArtifacts,
    InstallerError,
    Jc11sInstallerService,
    NativeWindowsImageBuilder,
)


def evidence(size: int = 64 * 1024**2, limit: int = 16 * 1024**2) -> ProbeEvidence:
    return ProbeEvidence(
        {
            "version": "0.4",
            "product": "rk3288-az01-jc11s",
            "version-bootloader": "U-Boot 2026.07-inmusic-20260721",
            "secure": "yes",
            "max-download-size": hex(limit),
            "partition-size:data": hex(size),
            "partition-type:data": "ext4",
            "is-userspace": "no",
            "current-slot": "a",
        },
        ("userdata", "variant", "unlocked"),
    )


def test_native_builder_reports_every_missing_bundled_component(tmp_path: Path):
    result = NativeWindowsImageBuilder(tmp_path).prerequisites()
    assert not result.ready
    assert "vendor/windows/e2fsprogs/bin/mke2fs.exe" in result.missing
    assert "bootstrap/jc11s/libprime-ssh.so" in result.missing


def test_native_builder_stops_before_temp_build_when_prerequisites_missing(tmp_path: Path):
    builder = NativeWindowsImageBuilder(tmp_path)
    with pytest.raises(InstallerError, match="Missing bundled build prerequisites"):
        builder.build(evidence(), tmp_path / "key.pub")


def test_bench_policy_does_not_open_normal_release_policy():
    policy = ExperimentalFirstFlashPolicy()
    authorization = policy.authorize(
        True,
        True,
    )
    authorization.require(2)
    from core.windows_installer import HardwareReleasePolicy

    assert HardwareReleasePolicy().blockers


def test_bench_result_log_records_failed_first_boot_with_image_identity(tmp_path: Path):
    image = tmp_path / "data.ext4"
    image.write_bytes(b"fixture")
    sparse = tmp_path / "sparse"
    sparse.mkdir()
    part = sparse / "part.sparse"
    part.write_bytes(b"part")
    plan = FlashPlan("data", image, 7, "a" * 64, 1024, sparse, (part,))
    artifacts = InstallerArtifacts(tmp_path, image, sparse, plan)
    log = tmp_path / "bench.jsonl"
    Jc11sInstallerService.record_bench_result(
        artifacts, log, "first_boot_or_ssh_failed", "device remained in failsafe"
    )
    import json

    record = json.loads(log.read_text().strip())
    assert record["outcome"] == "first_boot_or_ssh_failed"
    assert record["partition"] == "data"
    assert record["raw_sha256"] == "a" * 64


@pytest.mark.native_windows
def test_bundled_windows_tools_build_and_fully_validate_ext4_and_sparse(tmp_path: Path):
    source_root = Path(__file__).resolve().parents[1]
    builder = NativeWindowsImageBuilder(source_root)
    prerequisites = builder.prerequisites()
    if not prerequisites.ready:
        if sys.platform == "win32":
            pytest.fail("Windows bundle is incomplete: " + ", ".join(prerequisites.missing))
        pytest.skip("native Windows bundle is created by the Windows packaging job")
    key = generate_keypair(tmp_path / "keys", "test passphrase")
    artifacts = builder.build(evidence(), key.public_key)
    try:
        assert artifacts.image.stat().st_size == 64 * 1024**2
        assert artifacts.plan.sha256
        assert artifacts.plan.transfers
        assert all(part.stat().st_size < 16 * 1024**2 for part in artifacts.plan.transfers)
    finally:
        artifacts.cleanup()
