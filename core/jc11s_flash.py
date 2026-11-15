"""Read-only evidence collection and an isolated, in-memory flash rehearsal.

No production transport is accepted by the simulator. Hardware writes are
unconditionally disabled, independent of supplied evidence or user flags.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path

from core.fastboot_protocol import FastbootCommandError, FastbootSession, ResponseKind
from core.jc11s_image import verify
from core.transports import WriteBlockedError


@dataclass(frozen=True)
class ProbeEvidence:
    values: dict[str, str]
    unsupported: tuple[str, ...]
    # Variables describe bootloader names only; neither Linux mapping nor write support.
    linux_mapping_verified: bool = False
    flashing_verified: bool = False


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
    warnings: tuple[str, ...] = (
        "SIMULATION ONLY: partition-to-mmcblk0p11 mapping is not established.",
        "Fresh /data initialization and stock firmware boot remain unverified.",
        "Backup and tested stock recovery are required before any hardware write.",
    )


def prepare(image: Path, partition: str, partition_size: int, max_download: int) -> FlashPlan:
    if partition not in {"data", "userdata"}:
        raise ValueError("Specify an observed bootloader name, never a Linux path.")
    manifest = verify(image)
    size = manifest["size"]
    if size != partition_size:
        raise ValueError("PoC requires an exact partition-sized raw image.")
    if max_download <= 0 or size > min(max_download, 0xFFFFFFFF):
        raise ValueError("Raw image exceeds download limit; sparse/chunked flashing is not verified.")
    return FlashPlan(partition, image, size, manifest["sha256"], max_download)


@dataclass
class MockFlashTransport:
    """An in-memory target, never wraps or forwards to a USB transport."""

    commands: list[str] = field(default_factory=list)
    transferred: int = 0
    fail_at: str | None = None
    state: str = "idle"
    expected: int = 0

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
        if self.state != "receiving" or self.transferred + len(data) > self.expected:
            raise RuntimeError("Unexpected transfer or size overrun")
        if self.fail_at == "transfer":
            return max(0, len(data) - 1)
        self.transferred += len(data)
        return len(data)

    def finish_download(self) -> str:
        """Final target response, not another Fastboot command on the wire."""
        if self.fail_at == "download-complete":
            return "FAILdownload rejected"
        if self.state != "receiving" or self.transferred != self.expected:
            raise RuntimeError("Incomplete target download")
        self.state = "downloaded"
        return "OKAY"


def simulate(plan: FlashPlan, target: MockFlashTransport) -> None:
    if type(target) is not MockFlashTransport:
        raise TypeError("Only the built-in memory mock is allowed.")
    # Re-validate immediately before transfer, including changed images/manifests.
    checked = prepare(plan.image, plan.partition, plan.size, plan.max_download)
    if checked.sha256 != plan.sha256:
        raise ValueError("Image changed since plan creation.")
    command = f"download:{plan.size:08x}"
    if target.command(command) != f"DATA{plan.size:08x}":
        raise RuntimeError("Target did not accept the exact download size.")
    with plan.image.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            if target.write(chunk) != len(chunk):
                raise RuntimeError("Partial download: flash and reboot aborted.")
    # Model target's final download acknowledgement separately from flash.
    if target.finish_download() != "OKAY":
        raise RuntimeError("Download failed.")
    if target.command(f"flash:{plan.partition}") != "OKAY":
        raise RuntimeError("Flash failed; reboot aborted.")
    if target.command("reboot") != "OKAY":
        raise RuntimeError("Reboot failed.")


def flash_hardware(*args, **kwargs) -> None:
    raise WriteBlockedError("Hardware flash is disabled: mapping, firmware compatibility and recovery unverified.")


def main() -> None:
    import argparse
    import json
    from dataclasses import asdict

    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("probe-windows", help="Read-only getvar queries over native WinUSB")
    dry = sub.add_parser("simulate", help="Use ONLY an in-memory target, no USB")
    dry.add_argument("image", type=Path)
    dry.add_argument("--partition", required=True, choices=["data", "userdata"])
    dry.add_argument("--partition-size", required=True, type=lambda value: int(value, 0))
    dry.add_argument("--max-download", required=True, type=lambda value: int(value, 0))
    args = parser.parse_args()
    if args.command == "probe-windows":
        from core.windows_fastboot import create_windows_fastboot_transport

        print(json.dumps(asdict(create_windows_fastboot_transport().collect_partition_evidence()), indent=2))
    else:
        plan = prepare(args.image, args.partition, args.partition_size, args.max_download)
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
