"""Orchestration for non-destructive USB diagnostics."""

from __future__ import annotations

import platform
from dataclasses import dataclass
from typing import Protocol

from core.device_detection import DetectionResult, DetectionState, ProbeEvidence, match_selected_profile
from core.profiles import ProfileId
from core.transports import FastbootTransport, ImbupdTransport, TransportError, redacted_identifier
from core.windows_fastboot import WinUsbAmbiguousDevice, create_windows_fastboot_transport


class Probe(Protocol):
    def probe(self): ...


@dataclass(frozen=True)
class DiagnosticReport:
    result: DetectionResult
    log_lines: tuple[str, ...]
    fastboot_version: str | None = None
    reboot_available: bool = False


class UsbDiagnosticService:
    """Runs both read-only probes so a wrong selected transport is reported safely."""

    def __init__(self, fastboot: Probe | None = None, imbupd: Probe | None = None) -> None:
        self.fastboot = fastboot or self._default_fastboot()
        self.imbupd = imbupd or ImbupdTransport()

    def probe(self, selected: ProfileId) -> DiagnosticReport:
        evidence: list[ProbeEvidence] = []
        log: list[str] = ["Starting read-only USB diagnostics."]
        fastboot_version: str | None = None
        reboot_available = False
        errors: dict[str, str] = {}
        for name, transport in (("fastboot", self.fastboot), ("imbupd", self.imbupd)):
            try:
                devices = transport.probe()
            except WinUsbAmbiguousDevice as exc:
                evidence.append(ProbeEvidence(name, 2))
                log.append(f"{name}: ambiguous ({exc})")
                continue
            except TransportError as exc:
                errors[name] = str(exc)
                log.append(f"{name}: unavailable ({exc})")
                continue
            evidence.append(ProbeEvidence(name, len(devices)))
            if devices:
                identifiers = ", ".join(redacted_identifier(item.identifier) for item in devices)
                log.append(f"{name}: found {len(devices)} interface(s): {identifiers}")
                for device in devices:
                    log.extend(f"{name}: {status}" for status in device.statuses)
                    if name == "fastboot" and device.protocol_version:
                        fastboot_version = device.protocol_version
                        reboot_available = hasattr(self.fastboot, "reboot")
                        log.append(f"fastboot: protocol version {fastboot_version}")
            else:
                log.append(f"{name}: no interface found")
        result = match_selected_profile(selected, evidence)
        expected_transport = "fastboot" if selected == ProfileId.JC11S else "imbupd"
        if result.state == DetectionState.NO_DEVICE and expected_transport in errors:
            result = DetectionResult(
                DetectionState.TRANSPORT_ERROR,
                f"{expected_transport} diagnostic failed: {errors[expected_transport]}",
                False,
            )
        log.append(result.message)
        return DiagnosticReport(result, tuple(log), fastboot_version, reboot_available)

    def reboot_jc11s(self) -> str:
        reboot = getattr(self.fastboot, "reboot", None)
        if reboot is None:
            raise TransportError("Native JC11S reboot is available only through Windows WinUSB.")
        return reboot()

    @staticmethod
    def _default_fastboot() -> Probe:
        if platform.system() == "Windows":
            return create_windows_fastboot_transport()
        return FastbootTransport()
