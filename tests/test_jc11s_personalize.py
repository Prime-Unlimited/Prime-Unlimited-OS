"""Regression tests for JC11S image personalization and safe failure."""
from pathlib import Path
from unittest.mock import patch

import pytest

from core.jc11s_personalize import IMAGE_SIZE, personalize


def test_missing_reference_image_fails_without_output(tmp_path: Path):
    target = tmp_path / "personalized.img"
    with pytest.raises(ValueError, match="reference image"):
        personalize(tmp_path / "missing.img", target, tmp_path / "client.pub")
    assert not target.exists()


def test_missing_checksum_fails_before_key_or_image_changes(tmp_path: Path):
    source = tmp_path / "base.img"
    with source.open("wb") as stream:
        stream.truncate(IMAGE_SIZE)
    output = tmp_path / "new.img"
    with pytest.raises(ValueError, match="SHA256 sidecar"):
        personalize(source, output, tmp_path / "client.pub")
    assert not output.exists()


def test_wrong_geometry_fails_closed(tmp_path: Path):
    source = tmp_path / "base.img"
    source.write_bytes(b"not a filesystem")
    with pytest.raises(ValueError, match="size"):
        personalize(source, tmp_path / "new.img", tmp_path / "client.pub")


def test_no_reference_image_overwrite(tmp_path: Path):
    source = tmp_path / "base.img"
    source.write_bytes(b"bad")
    with pytest.raises(ValueError):
        personalize(source, source, tmp_path / "client.pub", expected_size=3)
