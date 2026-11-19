"""Validated, pinned JC11S Modloader bootstrap payload."""

from __future__ import annotations

import base64
import hashlib
import json
import struct
import zipfile
from pathlib import Path

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

ROOT = Path(__file__).resolve().parents[1]
BOOTSTRAP = ROOT / "bootstrap" / "jc11s"
MODLOADER = ROOT / "modloader"
PIN = BOOTSTRAP / "modloader-bootstrap.json"

ARCHIVE_ENTRIES = {
    "VERSION.json": ("prime-unlimited/updater/version.json", 0o600),
    "bin/modloader-api-start.sh": ("prime-unlimited/bin/modloader-api-start.sh", 0o755),
    "bin/modloader-updater-start.sh": ("prime-unlimited/bin/modloader-updater-start.sh", 0o755),
    "bin/prime-unlimited-runtime-api": ("prime-unlimited/bin/prime-unlimited-runtime-api", 0o755),
    "bin/prime-unlimited-self-updater": ("prime-unlimited/bin/prime-unlimited-self-updater", 0o755),
    "ui/ModLoader.template.qml": ("prime-unlimited/ui/ModLoader.template.qml", 0o644),
    "ui/OverlayHost.template.qml": ("prime-unlimited/ui/OverlayHost.template.qml", 0o644),
}


def _sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _verify_arm_hook(data: bytes) -> None:
    if len(data) < 52 or data[:7] != b"\x7fELF\x01\x01\x01":
        raise ValueError("JC11S UI hook is not little-endian ELF32.")
    kind, machine = struct.unpack_from("<HH", data, 16)
    if (kind, machine) != (3, 40):
        raise ValueError("JC11S UI hook is not an ARM shared object.")


def _verify_catalog(catalog: bytes, signature: bytes, trust: bytes) -> None:
    sig = json.loads(signature)
    key = json.loads(trust)
    if sig.get("algorithm") != "ed25519" or key.get("algorithm") != "ed25519":
        raise ValueError("Unsupported bootstrap catalog signature algorithm.")
    if sig.get("keyId") != key.get("keyId"):
        raise ValueError("Bootstrap catalog signer does not match the trust root.")
    try:
        Ed25519PublicKey.from_public_bytes(base64.b64decode(key["publicKey"], validate=True)).verify(
            base64.b64decode(sig["signature"], validate=True), catalog
        )
    except (InvalidSignature, ValueError) as exc:
        raise ValueError("Bootstrap catalog signature is invalid.") from exc


def payload(
    archive: Path | None = None, ui_hook: Path | None = None
) -> tuple[dict[str, tuple[bytes, int]], dict]:
    """Return image paths and modes after verifying every pinned input."""
    pin = json.loads(PIN.read_text())
    archive = archive or BOOTSTRAP / pin["archive"]
    raw = archive.read_bytes()
    if len(raw) != pin["size"] or _sha256(raw) != pin["sha256"]:
        raise ValueError("Pinned Modloader bootstrap archive size or SHA-256 mismatch.")

    files: dict[str, tuple[bytes, int]] = {}
    with zipfile.ZipFile(archive) as bundle:
        names = set(bundle.namelist())
        if names != set(ARCHIVE_ENTRIES) or any(info.is_dir() for info in bundle.infolist()):
            raise ValueError("Unexpected Modloader bootstrap ZIP structure.")
        version = json.loads(bundle.read("VERSION.json"))
        if version != {"version": pin["version"], "revision": pin["revision"]}:
            raise ValueError("Bootstrap version does not match the committed pin.")
        for source, (target, mode) in ARCHIVE_ENTRIES.items():
            files[target] = (bundle.read(source), mode)

    hook = ui_hook or MODLOADER / "device/jc11s/ui-hook/artifacts/libprime-ui-hook.so"
    hook_data = hook.read_bytes()
    _verify_arm_hook(hook_data)
    catalog = (MODLOADER / "catalog/jc11s.json").read_bytes()
    signature = (MODLOADER / "catalog/jc11s.sig.json").read_bytes()
    trust = (MODLOADER / "catalog/trust/prime-unlimited-2026-1.json").read_bytes()
    _verify_catalog(catalog, signature, trust)
    extra = {
        "prime-ssh/activate.sh": (
            (MODLOADER / "device/jc11s/ui-hook/activate.sh").read_bytes(), 0o700
        ),
        "prime-unlimited/lib/libprime-ui-hook.so": (hook_data, 0o644),
        "prime-unlimited/bin/runengine-hooked.sh": (
            (MODLOADER / "device/jc11s/ui-hook/runengine-hooked.sh").read_bytes(), 0o755
        ),
        "prime-unlimited/bin/bootstrap-first-start.sh": (
            (MODLOADER / "device/jc11s/ui-hook/bootstrap-first-start.sh").read_bytes(), 0o755
        ),
        "prime-unlimited/ui/patches/ControlCenterTab.qml": (
            (MODLOADER / "device/jc11s/ui-hook/ControlCenterTab.qml").read_bytes(), 0o644
        ),
        "prime-unlimited/ui/patches/Version.qml": (
            (MODLOADER / "device/jc11s/ui-hook/Version.qml").read_bytes(), 0o644
        ),
        "prime-unlimited/ui/prime_unlimited_mod_loader.svg": (
            (MODLOADER / "ui/assets/prime_unlimited_mod_loader.svg").read_bytes(), 0o644
        ),
        "prime-unlimited/ui/splash/SplashScreen.png": (
            (MODLOADER / "device/common/splash/SplashScreen.png").read_bytes(), 0o644
        ),
        "prime-unlimited/catalog/catalog.json": (catalog, 0o600),
        "prime-unlimited/catalog/catalog.sig.json": (signature, 0o600),
        "prime-unlimited/trust/catalog-key.json": (trust, 0o600),
        "prime-unlimited/os-version": ((ROOT / "VERSION").read_bytes(), 0o644),
        "prime-unlimited/os-version.qml": ((ROOT / "os/OSVersion.qml").read_bytes(), 0o644),
    }
    files.update(extra)
    if set(pin["companionSha256"]) != set(extra):
        raise ValueError("Bootstrap companion pin does not cover the exact payload.")
    for name, expected in pin["companionSha256"].items():
        if _sha256(files[name][0]) != expected:
            raise ValueError(f"Pinned bootstrap companion changed: {name}")
    return files, pin
