"""Windows JC11S installer orchestration with a separate hardware-release policy."""

from __future__ import annotations

import hashlib
import json
import shutil
import sys
import tempfile
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path

from core.jc11s_flash import FlashPlan, ProbeEvidence, build_for_device, validate_geometry
from core.jc11s_image import validate_hook
from core.ssh_service import SshService
from core.windows_fastboot import (
    ExperimentalFirstFlashAuthorization,
    HardwareFlashAuthorization,
    WindowsWinUsbFastbootTransport,
)


class InstallerError(RuntimeError):
    pass


@dataclass(frozen=True)
class BuildPrerequisites:
    ready: bool
    missing: tuple[str, ...]


@dataclass(frozen=True)
class HardwareReleasePolicy:
    partition_mapping_verified: bool = False
    firmware_compatible: bool = False
    recovery_tested: bool = False
    multipart_sparse_verified: bool = False

    @property
    def blockers(self) -> tuple[str, ...]:
        result = []
        if not self.partition_mapping_verified:
            result.append("Bootloader data → Linux mmcblk0p11 mapping is not independently verified")
        if not self.firmware_compatible:
            result.append("Fresh-data boot and ALSA bootstrap are not validated on this firmware")
        if not self.recovery_tested:
            result.append("No tested recovery path has been demonstrated")
        return tuple(result)


@dataclass(frozen=True)
class ExperimentalFirstFlashPolicy:
    """Narrow JC11S bench exception, intentionally unrelated to release readiness."""

    available: bool = True
    bootloader_partition: str = "data"
    linux_device: str = "/dev/mmcblk0p11"
    mount_point: str = "/data"
    evidence_scope: str = "tester-observed JC11S stock system and matching Fastboot data label"
    mapping_evidence_verified: bool = True

    @property
    def mapping_statement(self) -> str:
        return f"{self.bootloader_partition} = {self.linux_device} = {self.mount_point}"

    def authorize(
        self,
        experimental_confirmation: bool,
        confirmed_data_loss: bool,
    ) -> ExperimentalFirstFlashAuthorization:
        return ExperimentalFirstFlashAuthorization(
            enabled=self.available,
            mapping_evidence_verified=self.mapping_evidence_verified,
            recovery_confirmation=(
                ExperimentalFirstFlashAuthorization.RECOVERY_PHRASE if experimental_confirmation else ""
            ),
            experiment_confirmation=(
                ExperimentalFirstFlashAuthorization.EXPERIMENT_PHRASE if experimental_confirmation else ""
            ),
            user_confirmed_data_loss=confirmed_data_loss,
        )


@dataclass
class InstallerArtifacts:
    root: Path
    image: Path
    sparse_directory: Path
    plan: FlashPlan

    def cleanup(self) -> None:
        shutil.rmtree(self.root, ignore_errors=True)


class NativeWindowsImageBuilder:
    """Build with bundled Win32 e2fsprogs and a reproducibly built ARMv7 hook."""

    REQUIRED_TOOLS = ("mke2fs.exe", "debugfs.exe", "e2fsck.exe")

    def __init__(self, source_root: Path | None = None) -> None:
        self.source_root = source_root or Path(getattr(sys, "_MEIPASS", Path(__file__).resolve().parents[1]))
        self.tool_directory = self.source_root / "vendor" / "windows" / "e2fsprogs" / "bin"
        self.mke2fs_config = self.source_root / "vendor" / "windows" / "e2fsprogs" / "etc" / "mke2fs.conf"
        self.hook = self.source_root / "bootstrap" / "jc11s" / "libprime-ssh.so"
        self.hook_digest = self.hook.with_suffix(self.hook.suffix + ".sha256")
        self.catalog_trust_root = self.source_root / "modloader" / "catalog" / "trust" / "prime-unlimited-2026-1.json"

    def prerequisites(self) -> BuildPrerequisites:
        required = [
            *(self.tool_directory / name for name in self.REQUIRED_TOOLS),
            self.mke2fs_config,
            self.hook,
            self.hook_digest,
            self.catalog_trust_root,
        ]
        missing = tuple(path.relative_to(self.source_root).as_posix() for path in required if not path.is_file())
        if missing:
            return BuildPrerequisites(False, missing)
        try:
            validate_hook(self.hook)
            expected = self.hook_digest.read_text(encoding="ascii").split()[0].lower()
            actual = hashlib.sha256(self.hook.read_bytes()).hexdigest()
            if expected != actual:
                return BuildPrerequisites(False, ("ARMv7 hook SHA-256 mismatch",))
        except (OSError, ValueError, IndexError) as exc:
            return BuildPrerequisites(False, (f"invalid ARMv7 hook: {exc}",))
        return BuildPrerequisites(True, ())

    def build(self, evidence: ProbeEvidence, public_key: Path, progress=None) -> InstallerArtifacts:
        prerequisites = self.prerequisites()
        if not prerequisites.ready:
            raise InstallerError("Missing bundled build prerequisites: " + ", ".join(prerequisites.missing))
        geometry = validate_geometry(evidence)
        root = Path(tempfile.mkdtemp(prefix="prime-unlimited-jc11s-"))
        image = root / "data.ext4"
        sparse = root / "sparse"
        try:
            if progress:
                progress(10, "Validated bundled ARMv7 hook and native filesystem tools.")
                progress(25, f"Building exact {geometry.partition_size}-byte ext4 image natively…")
            plan = build_for_device(
                evidence,
                image,
                public_key,
                self.hook,
                sparse,
                tool_directory=self.tool_directory,
                mke2fs_config=self.mke2fs_config,
            )
            if progress:
                progress(100, "Native ext4 image and sparse transfer set fully verified.")
            return InstallerArtifacts(root, image, sparse, plan)
        except Exception:
            shutil.rmtree(root, ignore_errors=True)
            raise


class Jc11sInstallerService:
    def __init__(
        self,
        transport: WindowsWinUsbFastbootTransport,
        builder: NativeWindowsImageBuilder | None = None,
        release_policy: HardwareReleasePolicy | None = None,
        bench_policy: ExperimentalFirstFlashPolicy | None = None,
        ssh: SshService | None = None,
    ) -> None:
        self.transport = transport
        self.builder = builder or NativeWindowsImageBuilder()
        self.release_policy = release_policy or HardwareReleasePolicy()
        self.bench_policy = bench_policy or ExperimentalFirstFlashPolicy()
        self.ssh = ssh or SshService()

    def prepare(self, public_key: Path, identifier: str | None = None, progress=None) -> InstallerArtifacts:
        evidence = self.transport.collect_partition_evidence(identifier)
        validate_geometry(evidence)
        return self.builder.build(evidence, public_key, progress)

    def flash(
        self, artifacts: InstallerArtifacts, confirmed_data_loss: bool, identifier: str | None = None, progress=None
    ):
        policy = self.release_policy
        authorization = HardwareFlashAuthorization(
            policy.partition_mapping_verified,
            policy.firmware_compatible,
            policy.recovery_tested,
            confirmed_data_loss,
            policy.multipart_sparse_verified,
        )
        return self.transport.flash_sparse_plan(artifacts.plan, authorization, identifier, progress)

    def flash_bench(
        self,
        artifacts: InstallerArtifacts,
        experimental_confirmation: bool,
        confirmed_data_loss: bool,
        identifier: str | None = None,
        progress=None,
    ):
        authorization = self.bench_policy.authorize(
            experimental_confirmation,
            confirmed_data_loss,
        )
        return self.transport.flash_sparse_plan(artifacts.plan, authorization, identifier, progress)

    @staticmethod
    def record_bench_result(artifacts: InstallerArtifacts, log_path: Path, outcome: str, details: str) -> None:
        allowed = {"flash_succeeded", "first_boot_ssh_succeeded", "first_boot_or_ssh_failed"}
        if outcome not in allowed:
            raise ValueError("Unknown first-flash bench outcome.")
        log_path.parent.mkdir(parents=True, exist_ok=True)
        record = {
            "schema": 1,
            "timestamp_utc": datetime.now(UTC).isoformat(),
            "mode": "experimental-jc11s-first-flash",
            "outcome": outcome,
            "partition": artifacts.plan.partition,
            "raw_size": artifacts.plan.size,
            "raw_sha256": artifacts.plan.sha256,
            "transfer_count": len(artifacts.plan.transfers),
            "details": details[:2000],
        }
        with log_path.open("a", encoding="utf-8", newline="\n") as stream:
            stream.write(json.dumps(record, sort_keys=True) + "\n")

    def verify_ssh(self, host: str, private_key: Path, passphrase: str, known_hosts: Path, port: int = 22) -> str:
        return self.ssh.verify_authenticated(host, port, private_key, passphrase, known_hosts)
