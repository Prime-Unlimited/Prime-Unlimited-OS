#!/usr/bin/env python3
"""Compare portable payloads; permit only declared, target-validated native ELFs."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import struct
import zipfile

ELF = {'jc11s': (1, 40), 'jp24': (2, 183)}


def compare(directory, native):
    if native and set(native) != set(ELF):
        raise ValueError('native-files must declare exactly jc11s and jp24')
    exclusions = set()
    for paths in native.values():
        if not isinstance(paths, list) or any(not isinstance(p, str) for p in paths):
            raise ValueError('native-files values must be lists of exact paths')
        if len(set(paths)) != len(paths):
            raise ValueError('duplicate native path')
        exclusions.update(paths)
    packages = sorted(Path(directory).glob('*.zip'))
    if len(packages) != 2:
        raise ValueError('expected exactly two packages')
    shared = None
    seen = set()
    for package in packages:
        with zipfile.ZipFile(package) as archive:
            names = archive.namelist()
            if len(names) != len(set(names)):
                raise ValueError('duplicate ZIP entries')
            profiles = [n for n in names if n.endswith('/platform.json')]
            if len(profiles) != 1:
                raise ValueError('expected exactly one platform profile')
            target = json.loads(archive.read(profiles[0]))['model'].lower()
            if target not in ELF or target in seen:
                raise ValueError('unexpected or duplicate target')
            seen.add(target)
            expected = set(native.get(target, []))
            if set(names) & exclusions != expected:
                raise ValueError('missing or unexpected target native files')
            for name in expected:
                data = archive.read(name)
                elf_class, machine = ELF[target]
                if (len(data) < 52 or data[:4] != b'\x7fELF'
                        or data[4] != elf_class or data[5] != 1
                        or struct.unpack_from('<H', data, 18)[0] != machine):
                    raise ValueError(f'wrong ELF architecture: {name}')
            payload = {n: hashlib.sha256(archive.read(n)).hexdigest()
                       for n in names if n not in expected and n != profiles[0]}
            if shared is not None and payload != shared:
                raise ValueError('portable payload differs across targets')
            shared = payload
    print('Portable payload identical; declared native ELFs match their targets')


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    compare(args.directory, json.loads(os.environ.get('NATIVE_FILES', '{}')))
