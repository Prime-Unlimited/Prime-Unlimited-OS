"""Orchestration for non-destructive USB diagnostics."""

from __future__ import annotations

import platform
from dataclasses import dataclass, replace
from typing import Protocol

from core.device_detection import DetectionResult, DetectionState, ProbeEvidence, match_selected_profile
from core.profiles import ProfileId
from core.transports import FastbootTransport, ImbupdTransport, TransportError, redacted_identifier
from core.usb_discovery import DeviceFamily, DiscoveredUsbDevice, UsbDiscovery, create_usb_discovery
from core.windows_fastboot import WinUsbAmbiguousDevice, create_windows_fastboot_transport


class Probe(Protocol):
    def probe(self, identifier: str | None = None): ...


@dataclass(frozen=True)
class DiagnosticReport:
    result: DetectionResult
    log_lines: tuple[str, ...]
    fastboot_version: str | None = None
    reboot_available: bool = False
    candidates: tuple[DiscoveredUsbDevice, ...] = ()
    discovered_devices: tuple[DiscoveredUsbDevice, ...] = ()


class UsbDiagnosticService:
    """Runs both read-only probes so a wrong selected transport is reported safely."""

    def __init__(
        self,
        fastboot: Probe | None = None,
        imbupd: Probe | None = None,
        discovery: UsbDiscovery | None = None,
    ) -> None:
        self.discovery = discovery or create_usb_discovery()
        self.fastboot = fastboot or self._default_fastboot()
        self.imbupd = imbupd or ImbupdTransport(discovery=self.discovery)
        self._selected_device: str | None = None

    def probe(self, selected: ProfileId, selected_device: str | None = None) -> DiagnosticReport:
        self._selected_device = selected_device
        evidence: list[ProbeEvidence] = []
        log: list[str] = ["Starting read-only USB diagnostics."]
        fastboot_version: str | None = None
        reboot_available = False
        errors: dict[str, str] = {}
        probed_devices = []
        family = DeviceFamily(selected.value)
        try:
            discovered = self.discovery.discover()
        except Exception as exc:
            discovered = []
            log.append(f"usb discovery: unavailable ({exc})")
        candidates = tuple(device for device in discovered if family in device.candidate_families)
        log.append(f"usb discovery: {len(discovered)} device(s), {len(candidates)} relevant candidate(s)")
        for candidate in candidates:
            log.append(
                f"candidate: {candidate.display_name}; VID/PID {candidate.vid_pid}; "
                f"source {candidate.source}; model/revision unverified"
            )
        for name, transport in (("fastboot", self.fastboot), ("imbupd", self.imbupd)):
            try:
                identifier = (
                    selected_device if name == ("fastboot" if selected == ProfileId.JC11S else "imbupd") else None
                )
                devices = transport.probe(identifier)
                probed_devices.extend(devices)
            except WinUsbAmbiguousDevice as exc:
                evidence.append(ProbeEvidence(name, 2))
                log.append(f"{name}: ambiguous ({exc})")
                continue
            except TransportError as exc:
                errors[name] = str(exc)
                log.append(f"{name}: unavailable ({exc})")
                continue
            evidence.append(
                ProbeEvidence(
                    name,
                    len(devices),
                    transport_verified=bool(devices) and all(item.transport_verified for item in devices),
                )
            )
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
        candidates = tuple(self._enrich_candidate(candidate, probed_devices) for candidate in candidates)
        result = match_selected_profile(selected, evidence)
        if len(candidates) > 1 and selected_device is None:
            result = DetectionResult(
                DetectionState.AMBIGUOUS,
                "Multiple relevant USB candidates were discovered. Select one explicitly and run diagnostics again.",
                False,
            )
        elif result.state == DetectionState.NO_DEVICE and candidates:
            result = DetectionResult(
                DetectionState.CANDIDATE,
                "A possible device-family candidate was discovered, but no compatible transport "
                "was verified. It may be a new or unsupported revision. Installation is blocked.",
                False,
            )
        expected_transport = "fastboot" if selected == ProfileId.JC11S else "imbupd"
        if result.state == DetectionState.NO_DEVICE and expected_transport in errors:
            result = DetectionResult(
                DetectionState.TRANSPORT_ERROR,
                f"{expected_transport} diagnostic failed: {errors[expected_transport]}",
                False,
            )
        log.append(result.message)
        return DiagnosticReport(
            result,
            tuple(log),
            fastboot_version,
            reboot_available,
            candidates,
            tuple(discovered),
        )

    @staticmethod
    def _enrich_candidate(candidate: DiscoveredUsbDevice, devices) -> DiscoveredUsbDevice:
        matches = [device for device in devices if device.identifier.casefold() == candidate.discovery_key.casefold()]
        if not matches:
            return candidate
        protocol_verified = any(device.transport_verified and device.protocol_version for device in matches)
        transport_detected = any(device.transport_verified for device in matches)
        interfaces = candidate.interfaces + tuple(
            device.interface_details for device in matches if device.interface_details is not None
        )
        return replace(
            candidate,
            interfaces=interfaces,
            bootloader=next(
                (device.protocol_version for device in matches if device.protocol_version),
                candidate.bootloader,
            ),
            verification=replace(
                candidate.verification,
                candidate=True,
                transport_detected=transport_detected,
                protocol_verified=bool(protocol_verified),
                installation_authorized=False,
            ),
        )

    def reboot_jc11s(self) -> str:
        reboot = getattr(self.fastboot, "reboot", None)
        if reboot is None:
            raise TransportError("Native JC11S reboot is available only through Windows WinUSB.")
        return reboot(self._selected_device)

    @staticmethod
    def _default_fastboot() -> Probe:
        if platform.system() == "Windows":
            return create_windows_fastboot_transport()
        return FastbootTransport()
