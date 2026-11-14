"""Orchestration for non-destructive USB diagnostics."""

from __future__ import annotations

from dataclasses import dataclass
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
        self.fastboot = fastboot or FastbootTransport()
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
            if devices:
                identifiers = ", ".join(redacted_identifier(item.identifier) for item in devices)
                log.append(f"{name}: found {len(devices)} interface(s): {identifiers}")
            else:
                log.append(f"{name}: no interface found")
        result = match_selected_profile(selected, evidence)
        log.append(result.message)
        return DiagnosticReport(result, tuple(log))
