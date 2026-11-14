"""Secret-redacting diagnostic log support."""

from __future__ import annotations

import re

_PATTERNS = [
    re.compile(r"(?i)(passphrase|password|token)\s*[:=]\s*\S+"),
    re.compile(r"-----BEGIN OPENSSH PRIVATE KEY-----.*?-----END OPENSSH PRIVATE KEY-----", re.DOTALL),
]


def redact(message: str) -> str:
    result = message
    for pattern in _PATTERNS:
        result = pattern.sub(
            lambda match: f"{match.group(1)}=[REDACTED]" if match.lastindex else "[PRIVATE KEY REDACTED]", result
        )
    return result
