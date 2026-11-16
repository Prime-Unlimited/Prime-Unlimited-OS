"""Deterministic Android sparse images and offline reconstruction checks."""

from __future__ import annotations

import hashlib
import json
import re
import struct
from dataclasses import dataclass
from pathlib import Path

MAGIC = 0xED26FF3A
MAJOR = 1
MINOR = 0
FILE_HEADER = struct.Struct("<IHHHHIIII")
CHUNK_HEADER = struct.Struct("<HHII")
RAW = 0xCAC1
FILL = 0xCAC2
DONT_CARE = 0xCAC3
BLOCK_SIZE = 512
ZERO_BLOCK = bytes(BLOCK_SIZE)


@dataclass(frozen=True)
class SparsePart:
    path: Path
    size: int
    sha256: str
    chunks: int


@dataclass(frozen=True)
class _Chunk:
    kind: int
    start: int
    blocks: int

    @property
    def encoded_size(self) -> int:
        return CHUNK_HEADER.size + (self.blocks * BLOCK_SIZE if self.kind == RAW else 4)


def _digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for data in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(data)
    return value.hexdigest()


def _chunks(raw: Path) -> list[_Chunk]:
    """Represent every sector exactly once; zero sectors become FILL(0), never holes."""
    result: list[_Chunk] = []
    with raw.open("rb") as stream:
        block = 0
        current_kind = None
        current_start = 0
        current_blocks = 0
        while data := stream.read(BLOCK_SIZE):
            if len(data) != BLOCK_SIZE:
                raise ValueError("Raw image must be aligned to 512-byte sectors.")
            kind = FILL if data == ZERO_BLOCK else RAW
            if kind != current_kind:
                if current_kind is not None:
                    result.append(_Chunk(current_kind, current_start, current_blocks))
                current_kind, current_start, current_blocks = kind, block, 0
            current_blocks += 1
            block += 1
        if current_kind is not None:
            result.append(_Chunk(current_kind, current_start, current_blocks))
    return result


def _split_raw(chunk: _Chunk, maximum_payload: int) -> list[_Chunk]:
    if chunk.kind != RAW:
        return [chunk]
    blocks = max(1, (maximum_payload - CHUNK_HEADER.size) // BLOCK_SIZE)
    result = []
    offset = 0
    while offset < chunk.blocks:
        count = min(blocks, chunk.blocks - offset)
        result.append(_Chunk(RAW, chunk.start + offset, count))
        offset += count
    return result


def _groups(chunks: list[_Chunk], total_blocks: int, max_download: int) -> list[list[_Chunk]]:
    # Every part may need a leading and trailing DONT_CARE chunk. Strictly less
    # than max-download-size is intentional even though Fastboot normally allows equality.
    fixed = FILE_HEADER.size + 2 * CHUNK_HEADER.size
    payload_budget = max_download - fixed - 1
    if payload_budget < CHUNK_HEADER.size + BLOCK_SIZE:
        raise ValueError("Fastboot max-download-size is too small for a sparse RAW sector.")
    expanded = []
    for chunk in chunks:
        expanded.extend(_split_raw(chunk, payload_budget))
    groups: list[list[_Chunk]] = []
    current: list[_Chunk] = []
    size = fixed
    for chunk in expanded:
        if current and size + chunk.encoded_size >= max_download:
            groups.append(current)
            current, size = [], fixed
        if size + chunk.encoded_size >= max_download:
            raise ValueError("Sparse chunk cannot fit below the device download limit.")
        current.append(chunk)
        size += chunk.encoded_size
    if current:
        groups.append(current)
    if not groups or total_blocks <= 0:
        raise ValueError("Raw image is empty.")
    return groups


def _write_part(raw: Path, output: Path, chunks: list[_Chunk], total_blocks: int) -> int:
    encoded: list[tuple[int, int, int, int]] = []
    cursor = 0
    for chunk in chunks:
        if chunk.start > cursor:
            encoded.append((DONT_CARE, cursor, chunk.start - cursor, CHUNK_HEADER.size))
        total_size = CHUNK_HEADER.size + (chunk.blocks * BLOCK_SIZE if chunk.kind == RAW else 4)
        encoded.append((chunk.kind, chunk.start, chunk.blocks, total_size))
        cursor = chunk.start + chunk.blocks
    if cursor < total_blocks:
        encoded.append((DONT_CARE, cursor, total_blocks - cursor, CHUNK_HEADER.size))
    with raw.open("rb") as source, output.open("xb") as target:
        target.write(
            FILE_HEADER.pack(
                MAGIC, MAJOR, MINOR, FILE_HEADER.size, CHUNK_HEADER.size, BLOCK_SIZE, total_blocks, len(encoded), 0
            )
        )
        for kind, start, blocks, total_size in encoded:
            target.write(CHUNK_HEADER.pack(kind, 0, blocks, total_size))
            if kind == RAW:
                source.seek(start * BLOCK_SIZE)
                remaining = blocks * BLOCK_SIZE
                while remaining:
                    data = source.read(min(1024 * 1024, remaining))
                    if not data:
                        raise ValueError("Raw image ended while creating sparse data.")
                    target.write(data)
                    remaining -= len(data)
            elif kind == FILL:
                target.write(struct.pack("<I", 0))
    return len(encoded)


def convert(raw: Path, output_directory: Path, max_download: int) -> dict:
    """Create one or more independently flashable sparse parts below the measured limit."""
    raw = raw.resolve()
    size = raw.stat().st_size
    if size % BLOCK_SIZE:
        raise ValueError("Raw image must be aligned to 512-byte sectors.")
    if not 0 < max_download <= 0xFFFFFFFF:
        raise ValueError("Invalid Fastboot max-download-size.")
    total_blocks = size // BLOCK_SIZE
    if total_blocks > 0xFFFFFFFF:
        raise ValueError("Raw image exceeds Android sparse block-count capacity.")
    output_directory.mkdir(parents=True, exist_ok=False)
    groups = _groups(_chunks(raw), total_blocks, max_download)
    parts = []
    for index, group in enumerate(groups, 1):
        path = output_directory / f"data-{index:03d}-of-{len(groups):03d}.sparse"
        count = _write_part(raw, path, group, total_blocks)
        if path.stat().st_size >= max_download:
            raise AssertionError("Internal error: sparse part reached the download limit.")
        parts.append(SparsePart(path, path.stat().st_size, _digest(path), count))
    manifest = {
        "schema": 1,
        "format": "android-sparse-v1",
        "block_size": BLOCK_SIZE,
        "expanded_size": size,
        "expanded_sha256": _digest(raw),
        "max_download_size": max_download,
        "parts": [
            {"name": part.path.name, "size": part.size, "sha256": part.sha256, "chunks": part.chunks} for part in parts
        ],
    }
    (output_directory / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    verify_set(output_directory)
    return manifest


def _read_part(path: Path, expected_blocks: int):
    with path.open("rb") as stream:
        header = stream.read(FILE_HEADER.size)
        if len(header) != FILE_HEADER.size:
            raise ValueError("Truncated sparse header.")
        magic, major, minor, file_size, chunk_size, block_size, blocks, chunks, checksum = FILE_HEADER.unpack(header)
        if (magic, major, minor, file_size, chunk_size, block_size, blocks, checksum) != (
            MAGIC,
            MAJOR,
            MINOR,
            FILE_HEADER.size,
            CHUNK_HEADER.size,
            BLOCK_SIZE,
            expected_blocks,
            0,
        ):
            raise ValueError("Unsupported or inconsistent sparse header.")
        cursor = 0
        for _ in range(chunks):
            data = stream.read(CHUNK_HEADER.size)
            if len(data) != CHUNK_HEADER.size:
                raise ValueError("Truncated sparse chunk header.")
            kind, reserved, count, total_size = CHUNK_HEADER.unpack(data)
            if reserved or count <= 0 or cursor + count > expected_blocks:
                raise ValueError("Invalid sparse chunk extent.")
            if kind == RAW:
                expected = CHUNK_HEADER.size + count * BLOCK_SIZE
                if total_size != expected:
                    raise ValueError("Invalid sparse RAW chunk.")
                remaining = count * BLOCK_SIZE
                offset = cursor
                while remaining:
                    payload = stream.read(min(1024 * 1024, remaining))
                    if not payload:
                        raise ValueError("Truncated sparse RAW chunk.")
                    yield offset, payload
                    offset += len(payload) // BLOCK_SIZE
                    remaining -= len(payload)
            elif kind == FILL:
                value = stream.read(4)
                if total_size != CHUNK_HEADER.size + 4 or len(value) != 4:
                    raise ValueError("Invalid sparse FILL chunk.")
                remaining = count * BLOCK_SIZE
                offset = cursor
                unit = value * (1024 * 1024 // 4)
                while remaining:
                    payload = unit[: min(len(unit), remaining)]
                    yield offset, payload
                    offset += len(payload) // BLOCK_SIZE
                    remaining -= len(payload)
            elif kind == DONT_CARE:
                if total_size != CHUNK_HEADER.size:
                    raise ValueError("Invalid sparse DONT_CARE chunk.")
            else:
                raise ValueError("Unsupported sparse chunk type.")
            cursor += count
        if cursor != expected_blocks or stream.read(1):
            raise ValueError("Sparse part does not cover the full logical image or has trailing data.")


def verify_set(directory: Path) -> dict:
    manifest = json.loads((directory / "manifest.json").read_text())
    size = manifest["expanded_size"]
    limit = manifest["max_download_size"]
    parts = manifest["parts"]
    if (
        size <= 0
        or size % BLOCK_SIZE
        or manifest["block_size"] != BLOCK_SIZE
        or not 0 < limit <= 0xFFFFFFFF
        or not isinstance(parts, list)
        or not parts
        or not re.fullmatch(r"[0-9a-f]{64}", manifest["expanded_sha256"])
    ):
        raise ValueError("Invalid sparse manifest geometry.")
    blocks = size // BLOCK_SIZE
    reconstructed = hashlib.sha256()
    next_block = 0
    for index, part in enumerate(parts, 1):
        if part["name"] != f"data-{index:03d}-of-{len(parts):03d}.sparse":
            raise ValueError("Invalid sparse part name or ordering.")
        path = directory / part["name"]
        if path.stat().st_size != part["size"] or _digest(path) != part["sha256"]:
            raise ValueError("Sparse part size or SHA256 mismatch.")
        if path.stat().st_size >= limit:
            raise ValueError("Sparse part is not below max-download-size.")
        for start, payload in _read_part(path, blocks):
            count = len(payload) // BLOCK_SIZE
            if start != next_block:
                raise ValueError("Sparse parts overlap or leave a raw-sector gap.")
            reconstructed.update(payload)
            next_block += count
    if next_block != blocks:
        raise ValueError("Sparse parts do not reproduce every raw sector.")
    if reconstructed.hexdigest() != manifest["expanded_sha256"]:
        raise ValueError("Sparse reconstruction SHA256 mismatch.")
    return manifest
