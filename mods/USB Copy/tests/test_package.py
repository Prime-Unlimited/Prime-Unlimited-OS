import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess
import tempfile
import unittest
import zipfile

ROOT=Path(__file__).resolve().parents[1]
class PackageTest(unittest.TestCase):
    def test_preview_artifacts_for_both_architectures(self):
        with tempfile.TemporaryDirectory() as out:
            for target,machine,elfclass in [('jp24',183,2),('jc11s',40,1)]:
                with self.subTest(target=target):
                    result=subprocess.run(['python3',str(ROOT/'scripts/package.py'),'--target',target,'--output',out],capture_output=True,text=True,check=True)
                    info=json.loads(result.stdout)
                    self.assertFalse(info['signed']);self.assertTrue(info['previewOnly']);self.assertFalse(info['deviceValidated'])
                    self.assertEqual(hashlib.sha256(Path(info['path']).read_bytes()).hexdigest(),info['sha256'])
                    with zipfile.ZipFile(info['path']) as archive:
                        names=archive.namelist();self.assertEqual(len(names),len(set(names)))
                        self.assertTrue(all(n.startswith('usb-copy/') and '..' not in Path(n).parts for n in names))
                        self.assertFalse(any('lab-import' in n or 'rebuild' in n for n in names))
                        profile=json.loads(archive.read('usb-copy/platform.json'));self.assertEqual(profile['service'],info['service'])
                        binary=archive.read(info['service']['binary']);self.assertEqual(binary[:4],b'\x7fELF');self.assertEqual(binary[4],elfclass);self.assertEqual(struct.unpack_from('<H',binary,18)[0],machine)
                        self.assertEqual(hashlib.sha256(binary).hexdigest(),info['service']['sha256'])
                        qml=archive.read(info['overlay']);self.assertNotIn(b'__PRIME_UNLIMITED_AUTH_TOKEN__',qml)
                        self.assertIn('usb-copy/licenses/LICENSE-tcolgate-mp3',names)
if __name__=='__main__':unittest.main()
