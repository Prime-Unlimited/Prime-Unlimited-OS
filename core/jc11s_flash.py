"""Read-only evidence collection and an isolated, in-memory flash rehearsal.

No production transport is accepted by the simulator. Hardware writes are
unconditionally disabled, independent of supplied evidence or user flags.
"""

from __future__ import annotations

from dataclasses import asdict, dataclass, field
from pathlib import Path

from core.android_sparse import convert, verify_set
from core.fastboot_protocol import FastbootCommandError, FastbootSession, ResponseKind
from core.jc11s_image import build, verify
from core.transports import WriteBlockedError


@dataclass(frozen=True)
class ProbeEvidence:
    values: dict[str, str]
    unsupported: tuple[str, ...]
    # Variables describe bootloader names only; neither Linux mapping nor write support.
    linux_mapping_verified: bool = False
    flashing_verified: bool = False


@dataclass(frozen=True)
class DeviceGeometry:
    product: str
    partition: str
    partition_size: int
    partition_type: str
    max_download_size: int
    protocol_version: str
    bootloader_version: str
    secure: bool
    current_slot: str


def _positive_integer(value: str, name: str) -> int:
    try:
        parsed = int(value, 0)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"Missing or invalid Fastboot {name}.") from exc
    if parsed <= 0:
        raise ValueError(f"Fastboot {name} must be positive.")
    return parsed


def validate_geometry(evidence: ProbeEvidence) -> DeviceGeometry:
    """Fail closed unless all per-device values needed for image creation are present."""
    values = evidence.values
    required = {
        "version",
        "product",
        "version-bootloader",
        "secure",
        "max-download-size",
        "partition-size:data",
        "partition-type:data",
        "is-userspace",
        "current-slot",
    }
    missing = sorted(required - values.keys())
    if missing:
        raise ValueError(f"Missing required Fastboot variables: {', '.join(missing)}")
    if values["version"] != "0.4":
        raise ValueError("Only the measured JC11S Fastboot protocol version 0.4 is supported by this PoC.")
    if values["product"] != "rk3288-az01-jc11s":
        raise ValueError("Fastboot product is not the measured JC11S product.")
    if values["version-bootloader"] != "U-Boot 2026.07-inmusic-20260721":
        raise ValueError("JC11S bootloader build is outside the currently measured PoC profile.")
    if values["partition-type:data"].lower() != "ext4":
        raise ValueError("The per-device data partition is not reported as ext4.")
    if values["is-userspace"].lower() != "no":
        raise ValueError("Expected bootloader Fastboot, not userspace Fastboot.")
    if values["secure"].lower() != "yes":
        raise ValueError("JC11S secure state differs from the currently measured PoC profile.")
    if values["current-slot"].lower() not in {"a", "b"}:
        raise ValueError("Fastboot current-slot is invalid.")
    partition_size = _positive_integer(values["partition-size:data"], "partition-size:data")
    max_download = _positive_integer(values["max-download-size"], "max-download-size")
    if partition_size % 512:
        raise ValueError("Fastboot data partition size is not sector aligned.")
    if partition_size < 64 * 1024**2:
        raise ValueError("Fastboot data partition is too small for the validated image builder.")
    if max_download > 0xFFFFFFFF:
        raise ValueError("Fastboot max-download-size exceeds the protocol's 32-bit download field.")
    return DeviceGeometry(
        product=values["product"],
        partition="data",
        partition_size=partition_size,
        partition_type="ext4",
        max_download_size=max_download,
        protocol_version=values["version"],
        bootloader_version=values["version-bootloader"],
        secure=values["secure"].lower() == "yes",
        current_slot=values["current-slot"],
    )


def probe(session: FastbootSession) -> ProbeEvidence:
    values = {}
    unsupported = []
    names = [
        "version",
        "product",
        "variant",
        "version-bootloader",
        "secure",
        "unlocked",
        "max-download-size",
        "is-userspace",
        "current-slot",
    ]
    for partition in ("data", "userdata"):
        names.extend((f"partition-size:{partition}", f"partition-type:{partition}"))
    for name in names:
        try:
            result = session.getvar(name)
        except FastbootCommandError:
            unsupported.append(name)
            continue
        if result.kind != ResponseKind.OKAY:
            raise ValueError(f"Unexpected response for {name}: {result.kind}")
        values[name] = result.payload
    return ProbeEvidence(values, tuple(unsupported))


@dataclass(frozen=True)
class FlashPlan:
    partition: str
    image: Path
    size: int
    sha256: str
    max_download: int
    sparse_directory: Path
    transfers: tuple[Path, ...]
    warnings: tuple[str, ...] = (
        "SIMULATION ONLY: partition-to-mmcblk0p11 mapping is not established.",
        "Fresh /data initialization and stock firmware boot remain unverified.",
        "Backup and tested stock recovery are required before any hardware write.",
    )


def build_for_device(
    evidence: ProbeEvidence,
    output: Path,
    key: Path,
    hook: Path,
    sparse_directory: Path,
    *,
    tool_directory: Path | None = None,
    mke2fs_config: Path | None = None,
) -> FlashPlan:
    """Measured Fastboot geometry is the sole source of image and transfer sizes."""
    geometry = validate_geometry(evidence)
    build(
        output,
        key,
        hook,
        geometry.partition_size,
        asdict(geometry),
        tool_directory=tool_directory,
        mke2fs_config=mke2fs_config,
    )
    return prepare(output, geometry, sparse_directory, tool_directory=tool_directory)


def prepare(
    image: Path, geometry: DeviceGeometry, sparse_directory: Path, *, tool_directory: Path | None = None
) -> FlashPlan:
    manifest = verify(image, tool_directory=tool_directory)
    size = manifest["size"]
    if size != geometry.partition_size:
        raise ValueError("PoC requires an exact partition-sized raw image.")
    sparse = convert(image, sparse_directory, geometry.max_download_size)
    transfers = tuple(sparse_directory / part["name"] for part in sparse["parts"])
    if not transfers or any(path.stat().st_size >= geometry.max_download_size for path in transfers):
        raise ValueError("Sparse transfer plan violates the measured per-device download limit.")
    return FlashPlan(
        geometry.partition,
        image,
        size,
        manifest["sha256"],
        geometry.max_download_size,
        sparse_directory,
        transfers,
    )


@dataclass
class MockFlashTransport:
    """An in-memory target, never wraps or forwards to a USB transport."""

    commands: list[str] = field(default_factory=list)
    transferred: int = 0
    fail_at: str | None = None
    state: str = "idle"
    expected: int = 0
    received: int = 0

    def command(self, command: str) -> str:
        self.commands.append(command)
        if self.fail_at == command.split(":")[0]:
            raise RuntimeError("Simulated target rejection")
        if command.startswith("download:"):
            if self.state != "idle":
                raise RuntimeError("Mock target is not idle")
            self.expected = int(command.split(":")[1], 16)
            self.state = "receiving"
            return "DATA" + command.split(":")[1]
        if command.startswith("flash:") and self.state == "downloaded":
            self.state = "flashed"
            return "OKAY"
        if command == "reboot" and self.state == "flashed":
            self.state = "rebooted"
            return "OKAY"
        raise RuntimeError("Unexpected command or target state")

    def write(self, data: bytes) -> int:
        if self.state != "receiving" or self.received + len(data) > self.expected:
            raise RuntimeError("Unexpected transfer or size overrun")
        if self.fail_at == "transfer":
            return max(0, len(data) - 1)
        self.received += len(data)
        self.transferred += len(data)
        return len(data)

    def finish_download(self) -> str:
        """Final target response, not another Fastboot command on the wire."""
        if self.fail_at == "download-complete":
            return "FAILdownload rejected"
        if self.state != "receiving" or self.received != self.expected:
            raise RuntimeError("Incomplete target download")
        self.state = "downloaded"
        return "OKAY"

    def next_download(self) -> None:
        if self.state != "flashed":
            raise RuntimeError("Previous sparse flash did not complete.")
        self.state = "idle"
        self.expected = 0
        self.received = 0


def simulate(plan: FlashPlan, target: MockFlashTransport) -> None:
    if type(target) is not MockFlashTransport:
        raise TypeError("Only the built-in memory mock is allowed.")
    # Re-validate immediately before transfer, including changed images/manifests.
    image_manifest = verify(plan.image)
    sparse_manifest = verify_set(plan.sparse_directory)
    if image_manifest["size"] != plan.size or image_manifest["sha256"] != plan.sha256:
        raise ValueError("Image changed since plan creation.")
    if sparse_manifest["expanded_size"] != plan.size or sparse_manifest["expanded_sha256"] != plan.sha256:
        raise ValueError("Sparse plan does not reconstruct the selected raw image.")
    if tuple(plan.sparse_directory / part["name"] for part in sparse_manifest["parts"]) != plan.transfers:
        raise ValueError("Sparse transfer list changed since plan creation.")
    for index, transfer in enumerate(plan.transfers):
        transfer_size = transfer.stat().st_size
        if transfer_size >= plan.max_download:
            raise ValueError("Sparse transfer reaches or exceeds max-download-size.")
        command = f"download:{transfer_size:08x}"
        if target.command(command) != f"DATA{transfer_size:08x}":
            raise RuntimeError("Target did not accept the exact download size.")
        with transfer.open("rb") as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                if target.write(chunk) != len(chunk):
                    raise RuntimeError("Partial download: flash and reboot aborted.")
        if target.finish_download() != "OKAY":
            raise RuntimeError("Download failed.")
        if target.command(f"flash:{plan.partition}") != "OKAY":
            raise RuntimeError("Flash failed; reboot aborted.")
        if index + 1 < len(plan.transfers):
            target.next_download()
    if target.command("reboot") != "OKAY":
        raise RuntimeError("Reboot failed.")


def flash_hardware(*args, **kwargs) -> None:
    raise WriteBlockedError("Hardware flash is disabled: mapping, firmware compatibility and recovery unverified.")


def load_evidence(path: Path) -> ProbeEvidence:
    import json

    payload = json.loads(path.read_text())
    values = payload.get("values")
    unsupported = payload.get("unsupported", [])
    if not isinstance(values, dict) or not all(
        isinstance(key, str) and isinstance(value, str) for key, value in values.items()
    ):
        raise ValueError("Probe JSON must contain a string-to-string 'values' object.")
    if not isinstance(unsupported, list) or not all(isinstance(value, str) for value in unsupported):
        raise ValueError("Probe JSON 'unsupported' must be a string list.")
    return ProbeEvidence(values, tuple(unsupported))


def main() -> None:
    import argparse
    import json

    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("probe-windows", help="Read-only getvar queries over native WinUSB")
    create = sub.add_parser("build-device", help="Build from saved per-device read-only probe evidence")
    create.add_argument("--probe", type=Path, required=True)
    create.add_argument("--key", type=Path, required=True)
    create.add_argument("--hook", type=Path, required=True)
    create.add_argument("--output", type=Path, required=True)
    create.add_argument("--sparse-output", type=Path, required=True)
    dry = sub.add_parser("simulate", help="Build the transfer plan and use ONLY an in-memory target")
    dry.add_argument("image", type=Path)
    dry.add_argument("--probe", type=Path, required=True)
    dry.add_argument("--sparse-output", type=Path, required=True)
    args = parser.parse_args()
    if args.command == "probe-windows":
        from core.windows_fastboot import create_windows_fastboot_transport

        print(json.dumps(asdict(create_windows_fastboot_transport().collect_partition_evidence()), indent=2))
    elif args.command == "build-device":
        plan = build_for_device(load_evidence(args.probe), args.output, args.key, args.hook, args.sparse_output)
        print(json.dumps(asdict(plan), indent=2, default=str))
    else:
        geometry = validate_geometry(load_evidence(args.probe))
        plan = prepare(args.image, geometry, args.sparse_output)
        target = MockFlashTransport()
        simulate(plan, target)
        print(
            json.dumps(
                {
                    "mode": "memory-mock",
                    "commands": target.commands,
                    "bytes": target.transferred,
                    "warnings": plan.warnings,
                },
                indent=2,
            )
        )


if __name__ == "__main__":
    main()
