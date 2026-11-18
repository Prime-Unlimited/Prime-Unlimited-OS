"""Read-only parsing of Fastboot output. No USB access or flash operations."""

from __future__ import annotations

import re
from collections.abc import Mapping
from dataclasses import dataclass

_LINE = re.compile(r"^(?:\(bootloader\)\s*)?([\w.:-]+):\s*(.*?)\s*$")


@dataclass(frozen=True)
class FastbootInfo:
    product: str | None
    partition_size_data: int | None
    partition_type_data: str | None
    current_slot: str | None
    is_userspace: bool | None


def parse_getvar_output(output: str) -> Mapping[str, str]:
    """Parse output from fastboot getvar all or individual getvar commands.

    Fastboot may write output to stderr and may prefix lines with '(bootloader)'.
    Unknown and malformed lines are ignored; duplicate keys with conflicting
    values are rejected rather than silently choosing one.
    """
    values: dict[str, str] = {}
    for raw in output.splitlines():
        line = raw.strip()
        match = _LINE.match(line)
        if not match:
            continue
        key, value = match.groups()
        if key in values and values[key] != value:
            raise ValueError(f"Conflicting Fastboot value for {key}")
        values[key] = value
    return values


def _partition_size(value: str | None) -> int | None:
    if value is None:
        return None
    try:
        result = int(value, 0)
    except ValueError:
        return None
    return result if result > 0 else None


def interpret_fastboot(output: str) -> FastbootInfo:
    values = parse_getvar_output(output)
    userspace = values.get("is-userspace", "").lower()
    return FastbootInfo(
        product=values.get("product"),
        partition_size_data=_partition_size(values.get("partition-size:data")),
        partition_type_data=values.get("partition-type:data"),
        current_slot=values.get("current-slot"),
        is_userspace={"yes": True, "no": False}.get(userspace),
    )


def is_jc11s_candidate(info: FastbootInfo) -> bool:
    """Informational only: not an authorization to flash.

    Partition size is deliberately not an identity signal: it is measured for
    each device and may differ across JC11S revisions.
    """
    return (
        info.product == "rk3288-az01-jc11s"
        and info.partition_type_data == "ext4"
        and info.partition_size_data is not None
        and info.is_userspace is False
    )
