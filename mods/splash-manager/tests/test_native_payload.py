import importlib.util
import json
from pathlib import Path
import struct
import zipfile

import pytest

SCRIPT = Path(__file__).resolve().parents[3] / '.github/scripts/compare-mod-payload.py'
spec = importlib.util.spec_from_file_location('compare_payload', SCRIPT)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
NATIVE = {'jc11s': ['splash/bin/arm'], 'jp24': ['splash/bin/arm64']}


def packages(directory, *, change_shared=False, wrong_elf=False, extra_native=False):
    for target, elf_class, machine in [('jc11s', 1, 40), ('jp24', 2, 183)]:
        elf = bytearray(64)
        elf[:6] = b'\x7fELF' + bytes([elf_class, 1])
        struct.pack_into('<H', elf, 18, 40 if wrong_elf else machine)
        with zipfile.ZipFile(directory / f'{target}.zip', 'w') as archive:
            archive.writestr('splash/platform.json', json.dumps({'model': target.upper()}))
            archive.writestr('splash/start.sh', b'changed' if change_shared and target == 'jp24' else b'#!/bin/sh\n')
            archive.writestr(NATIVE[target][0], elf)
            if extra_native and target == 'jp24':
                archive.writestr(NATIVE['jc11s'][0], elf)


def test_target_binaries_are_allowed(tmp_path):
    packages(tmp_path)
    module.compare(tmp_path, NATIVE)


@pytest.mark.parametrize('fault', ['change_shared', 'wrong_elf', 'extra_native'])
def test_payload_tampering_is_rejected(tmp_path, fault):
    packages(tmp_path, **{fault: True})
    with pytest.raises(ValueError):
        module.compare(tmp_path, NATIVE)
