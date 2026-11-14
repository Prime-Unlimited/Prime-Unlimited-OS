"""Profile matching from read-only transport evidence."""

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum

from core.profiles import ProfileId


class DetectionState(StrEnum):
    NO_DEVICE = "no_device"
    MATCH = "match"
    WRONG_PROFILE = "wrong_profile"
    AMBIGUOUS = "ambiguous"
    UNVERIFIED = "unverified"


@dataclass(frozen=True)
class ProbeEvidence:
    transport: str
    count: int
    verified_profile: ProfileId | None = None
    details: str = ""


@dataclass(frozen=True)
class DetectionResult:
    state: DetectionState
    message: str
    may_continue: bool


def match_selected_profile(selected: ProfileId, evidence: list[ProbeEvidence]) -> DetectionResult:
    present = [item for item in evidence if item.count > 0]
    if not present:
        return DetectionResult(DetectionState.NO_DEVICE, "No supported USB device was found.", False)
    if any(item.count > 1 for item in present) or sum(item.count for item in present) > 1:
        return DetectionResult(
            DetectionState.AMBIGUOUS,
            "Multiple or ambiguous USB devices were found. Disconnect all but the selected device.",
            False,
        )
    item = present[0]
    expected_transport = "fastboot" if selected == ProfileId.JC11S else "imbupd"
    if item.transport.lower() != expected_transport:
        target = "Prime 4+" if selected == ProfileId.JP24 else "Prime 4 G2"
        return DetectionResult(
            DetectionState.WRONG_PROFILE,
            f"The connected device does not match the selection. Go back and select {target}.",
            False,
        )
    if item.verified_profile is None:
        return DetectionResult(
            DetectionState.UNVERIFIED,
            "The transport is present, but positive model identity is not verified. No write is allowed.",
            False,
        )
    if item.verified_profile != selected:
        return DetectionResult(
            DetectionState.WRONG_PROFILE,
            "The positively identified device does not match the selected model.",
            False,
        )
    return DetectionResult(DetectionState.MATCH, "Device identity verified.", True)
