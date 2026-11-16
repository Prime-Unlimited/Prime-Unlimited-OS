"""Orchestration for non-destructive USB diagnostics."""

from __future__ import annotations

import platform
from dataclasses import dataclass, replace
from typing import Protocol

from core.device_detection import DetectionResult, ProbeEvidence, match_selected_profile
from core.profiles import ProfileId
from core.transports import FastbootTransport, ImbupdTransport, TransportError, redacted_identifier


class Probe(Protocol):
    def probe(self): ...


@dataclass(frozen=True)
class DiagnosticReport:
    result: DetectionResult
    log_lines: tuple[str, ...]


class UsbDiagnosticService:
    """Runs both read-only probes so a wrong selected transport is reported safely."""

    def __init__(self, fastboot: Probe | None = None, imbupd: Probe | None = None) -> None:
        self.fastboot = fastboot if fastboot is not None else self._default_fastboot()
        self.imbupd = imbupd or ImbupdTransport()

    def probe(self, selected: ProfileId) -> DiagnosticReport:
        evidence: list[ProbeEvidence] = []
        log: list[str] = ["Starting read-only USB diagnostics."]
        for name, transport in (("fastboot", self.fastboot), ("imbupd", self.imbupd)):
            try:
                devices = transport.probe()
            except TransportError as exc:
                log.append(f"{name}: unavailable ({exc})")
                continue
            evidence.append(ProbeEvidence(name, len(devices)))
            if name == "fastboot" and selected == ProfileId.JC11S and len(devices) == 1:
                # A USB VID/PID or a Fastboot interface alone never authorizes writing.
                # Require the device's actual bootloader product identity.
                getvar = getattr(transport, "getvar", None)
                if getvar is not None:
                    try:
                        product = getvar("product", devices[0].identifier).strip()
                        log.append(f"fastboot: product {product or '<empty>'}")
                        if product == "rk3288-az01-jc11s":
                            evidence[-1] = replace(evidence[-1], verified_profile=ProfileId.JC11S)
                        elif product:
                            log.append("fastboot: unexpected product; installation blocked")
                    except (TransportError, OSError, ValueError) as exc:
                        log.append(f"fastboot: product verification failed ({exc})")
                else:
                    log.append("fastboot: product verification unavailable")
            if devices:
                identifiers = ", ".join(redacted_identifier(item.identifier) for item in devices)
                log.append(f"{name}: found {len(devices)} interface(s): {identifiers}")
            else:
                log.append(f"{name}: no interface found")
        result = match_selected_profile(selected, evidence)
        log.append(result.message)
        return DiagnosticReport(result, tuple(log))

    @staticmethod
    def _default_fastboot():
        if platform.system() == "Windows":
            from core.windows_fastboot import create_windows_fastboot_transport

            return create_windows_fastboot_transport()
        return FastbootTransport()
