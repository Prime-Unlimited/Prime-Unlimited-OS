"""Real JP24 (Prime 4 G2) /data bootstrap flash: build image, write it, reboot.

This is the one validated, destructive write path in the installer. It is
gated behind ``DeviceProfile.flash_enabled`` (JP24 only - JC11S remains
blocked) and the caller must still show an explicit confirmation before
invoking :func:`flash_bootstrap`, since it overwrites the device's /data
partition and there is no automated recovery path if the device is
disconnected mid-write.

The actual mechanism (ALSA boot hook forks and execs sshd directly on
port 2222, no shell script or systemd unit involved) is documented in
devices/jp24/README.md and was confirmed fully automatically - flash,
reboot, SSH in - on real hardware.
"""

from __future__ import annotations

import os
import platform
import subprocess
from dataclasses import dataclass
from pathlib import Path

_BOOTSTRAP_DIR = Path(__file__).resolve().parent.parent / "devices" / "jp24" / "ssh-bootstrap"
_CONFIRM_PHRASE = "FLASH JP24 /data"


class FlashError(RuntimeError):
    pass


@dataclass(frozen=True)
class FlashProgress:
    step: str
    message: str


def _run(
    command: list[str],
    *,
    timeout: float,
    use_admin: bool = False,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    if use_admin and platform.system() == "Darwin":
        # macOS: prompt for admin privileges through a native GUI dialog
        # rather than requiring the app itself to run elevated, or trying
        # to pipe a password through a terminal `sudo` that has nowhere
        # to prompt from a GUI app.
        script = " ".join(_shell_quote(part) for part in command)
        command = ["osascript", "-e", f'do shell script "{script}" with administrator privileges']
    try:
        return subprocess.run(command, capture_output=True, text=True, timeout=timeout, check=False, env=env)
    except (FileNotFoundError, subprocess.TimeoutExpired) as exc:
        raise FlashError(f"Command failed: {exc}") from exc


def _shell_quote(value: str) -> str:
    return "'" + value.replace("'", "'\\''") + "'"


def ensure_flash_tool(build_dir: Path) -> Path:
    """Compile the imbupd USB flash tool if a cached build isn't present."""
    if platform.system() != "Darwin":
        raise FlashError("The imbupd flash tool is currently only implemented for macOS.")
    build_dir.mkdir(parents=True, exist_ok=True)
    binary = build_dir / "imbupd_write_mac"
    source = _BOOTSTRAP_DIR / "imbupd_write_mac.c"
    if binary.exists() and binary.stat().st_mtime > source.stat().st_mtime:
        return binary
    for brew_prefix in ("/opt/homebrew", "/usr/local"):
        include = Path(brew_prefix) / "include"
        lib = Path(brew_prefix) / "lib"
        if (include / "libusb-1.0").exists():
            result = _run(
                [
                    "cc", "-o", str(binary), str(source),
                    f"-I{include}", f"-L{lib}", "-lusb-1.0",
                ],
                timeout=60,
            )
            if result.returncode == 0:
                return binary
            raise FlashError(f"Failed to compile the flash tool: {result.stderr.strip()}")
    raise FlashError("libusb was not found (expected via Homebrew). Run: brew install libusb")


def build_data_image(public_key: Path, output_image: Path, size_mb: int | None = None) -> None:
    """Build the /data ext4 image embedding the given SSH public key.

    size_mb overrides the script's default (256 MiB) - pass the real
    partition size (see core.ssh_service.SshService.query_data_partition_size)
    to build an image sized to the device's actual /data partition
    rather than a fixed guess that could be wrong on a different
    hardware revision.
    """
    script = _BOOTSTRAP_DIR / "build-data-image.sh"
    if not script.exists():
        raise FlashError(f"Missing bootstrap script: {script}")
    env = None
    timeout = 60.0
    if size_mb is not None:
        env = {**os.environ, "DATA_SIZE_MB": str(size_mb)}
        # dd + mke2fs on a multi-gigabyte image takes longer than the
        # default 60s budget; scale generously rather than guess tightly.
        timeout = max(60.0, size_mb / 5)
    result = _run(["bash", str(script), str(public_key), str(output_image)], timeout=timeout, env=env)
    if result.returncode != 0:
        raise FlashError(f"Failed to build the /data image: {result.stderr.strip() or result.stdout.strip()}")


def query_device(tool: Path) -> str:
    """Read-only: confirm a device is present in Firmware Update Mode."""
    result = _run(["sudo", "-n", str(tool), "query"], timeout=15)
    if result.returncode != 0:
        # -n (non-interactive) fails if sudo needs a password; retry with
        # the GUI admin prompt so the user is asked once, not twice.
        result = _run([str(tool), "query"], timeout=30, use_admin=True)
    if result.returncode != 0:
        raise FlashError(
            "No JP24 device was found in Firmware Update Mode. "
            f"({result.stderr.strip() or result.stdout.strip()})"
        )
    return result.stdout.strip()


def flash_bootstrap(
    tool: Path,
    image: Path,
    *,
    on_progress: object | None = None,
) -> None:
    """Write the /data image and reboot the device. Destructive and irreversible."""

    def report(step: str, message: str) -> None:
        if on_progress is not None:
            on_progress(FlashProgress(step, message))

    report("query", "Checking for a device in Firmware Update Mode…")
    query_device(tool)

    report("write", "Writing the /data partition (do not disconnect the device)…")
    result = _run(
        [str(tool), "write", "data", str(image), "--confirm", _CONFIRM_PHRASE],
        timeout=180,
        use_admin=True,
    )
    if result.returncode != 0:
        raise FlashError(f"Flashing failed: {result.stderr.strip() or result.stdout.strip()}")

    report("reboot", "Rebooting the device…")
    result = _run([str(tool), "reboot"], timeout=30, use_admin=True)
    if result.returncode != 0:
        raise FlashError(
            "The /data write succeeded, but the automatic reboot command failed. "
            f"Power-cycle the device manually. ({result.stderr.strip() or result.stdout.strip()})"
        )
    report("done", "Flash complete. The device is rebooting.")
