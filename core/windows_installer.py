"""Windows JC11S installer orchestration with a separate hardware-release policy."""

from __future__ import annotations

import platform
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

from core.jc11s_flash import FlashPlan, ProbeEvidence, validate_geometry
from core.ssh_service import SshService
from core.windows_fastboot import HardwareFlashAuthorization, WindowsWinUsbFastbootTransport


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


@dataclass
class InstallerArtifacts:
    root: Path
    image: Path
    sparse_directory: Path
    plan: FlashPlan

    def cleanup(self) -> None:
        shutil.rmtree(self.root, ignore_errors=True)


class WslImageBuilder:
    REQUIRED = ("python3", "mke2fs", "debugfs", "e2fsck", "arm-linux-gnueabihf-gcc")

    def __init__(self, distribution: str = "Debian", source_root: Path | None = None, runner=subprocess.run) -> None:
        self.distribution = distribution
        self.source_root = source_root or Path(getattr(sys, "_MEIPASS", Path(__file__).resolve().parents[1]))
        self.runner = runner

    def _run(self, arguments: list[str], timeout: int = 30) -> subprocess.CompletedProcess:
        try:
            return self.runner(arguments, capture_output=True, text=True, timeout=timeout, check=False)
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise InstallerError(f"Could not run the WSL build environment: {exc}") from exc

    def prerequisites(self) -> BuildPrerequisites:
        if platform.system() != "Windows":
            return BuildPrerequisites(False, ("Windows host",))
        if shutil.which("wsl.exe") is None:
            return BuildPrerequisites(False, ("Windows Subsystem for Linux (wsl.exe)",))
        distributions = self._run(["wsl.exe", "--list", "--quiet"])
        installed = {line.replace("\x00", "").strip() for line in distributions.stdout.splitlines() if line.strip()}
        if distributions.returncode or self.distribution not in installed:
            return BuildPrerequisites(False, (f"WSL distribution {self.distribution}",))
        missing = []
        for command in self.REQUIRED:
            result = self._run(["wsl.exe", "-d", self.distribution, "--", "sh", "-c", f"command -v {command}"])
            if result.returncode:
                missing.append(f"{command} in WSL distribution {self.distribution}")
        cryptography = self._run(["wsl.exe", "-d", self.distribution, "--", "python3", "-c", "import cryptography"])
        if cryptography.returncode:
            missing.append(f"Python cryptography package in WSL distribution {self.distribution}")
        return BuildPrerequisites(not missing, tuple(missing))

    def _linux_path(self, path: Path) -> str:
        result = self._run(["wsl.exe", "-d", self.distribution, "--", "wslpath", "-a", "-u", str(path.resolve())])
        if result.returncode or not result.stdout.strip():
            raise InstallerError(result.stderr.strip() or f"Could not translate Windows path: {path}")
        return result.stdout.strip()

    def build(self, evidence: ProbeEvidence, public_key: Path, progress=None) -> InstallerArtifacts:
        prerequisites = self.prerequisites()
        if not prerequisites.ready:
            raise InstallerError("Missing build prerequisites: " + ", ".join(prerequisites.missing))
        geometry = validate_geometry(evidence)
        root = Path(tempfile.mkdtemp(prefix="prime-unlimited-jc11s-"))
        try:
            probe_path = root / "probe.json"
            import json
            from dataclasses import asdict

            probe_path.write_text(json.dumps(asdict(evidence), indent=2) + "\n")
            hook = root / "libprime-ssh.so"
            image = root / "data.ext4"
            sparse = root / "sparse"
            source = self._linux_path(self.source_root)
            key = self._linux_path(public_key)
            probe = self._linux_path(probe_path)
            hook_linux = self._linux_path(hook)
            image_linux = self._linux_path(image)
            sparse_linux = self._linux_path(sparse)
            if progress:
                progress(10, "Building independent ARMv7 SSH hook…")
            result = self._run(
                [
                    "wsl.exe",
                    "-d",
                    self.distribution,
                    "--cd",
                    source,
                    "--",
                    "sh",
                    "tools/build-jc11s-hook.sh",
                    hook_linux,
                ],
                timeout=120,
            )
            if result.returncode:
                raise InstallerError(result.stderr.strip() or "ARMv7 hook build failed.")
            if progress:
                progress(25, f"Building exact {geometry.partition_size}-byte ext4 image…")
            result = self._run(
                [
                    "wsl.exe",
                    "-d",
                    self.distribution,
                    "--cd",
                    source,
                    "--",
                    "python3",
                    "-m",
                    "core.jc11s_flash",
                    "build-device",
                    "--probe",
                    probe,
                    "--key",
                    key,
                    "--hook",
                    hook_linux,
                    "--output",
                    image_linux,
                    "--sparse-output",
                    sparse_linux,
                ],
                timeout=1800,
            )
            if result.returncode:
                raise InstallerError(result.stderr.strip() or "Offline image build or validation failed.")
            # The WSL command already produced and verified the plan. Re-open it
            # without overwriting by validating manifests and constructing paths.
            from core.android_sparse import verify_set
            from core.jc11s_image import verify_integrity

            raw = verify_integrity(image)
            sparse_manifest = verify_set(sparse)
            transfers = tuple(sparse / item["name"] for item in sparse_manifest["parts"])
            plan = FlashPlan("data", image, raw["size"], raw["sha256"], geometry.max_download_size, sparse, transfers)
            if progress:
                progress(100, "Image and sparse transfer set verified.")
            return InstallerArtifacts(root, image, sparse, plan)
        except Exception:
            shutil.rmtree(root, ignore_errors=True)
            raise


class Jc11sInstallerService:
    def __init__(
        self,
        transport: WindowsWinUsbFastbootTransport,
        builder: WslImageBuilder | None = None,
        release_policy: HardwareReleasePolicy | None = None,
        ssh: SshService | None = None,
    ) -> None:
        self.transport = transport
        self.builder = builder or WslImageBuilder()
        self.release_policy = release_policy or HardwareReleasePolicy()
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

    def verify_ssh(self, host: str, private_key: Path, passphrase: str, known_hosts: Path, port: int = 22) -> str:
        return self.ssh.verify_authenticated(host, port, private_key, passphrase, known_hosts)
