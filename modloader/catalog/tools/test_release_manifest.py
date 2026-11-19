import copy
import hashlib
import tempfile
import unittest
from pathlib import Path

from release_manifest import ManifestError, create_candidate, finalize


class ReleaseManifestTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.packages = self.root / "packages"; self.packages.mkdir()
        self.bases = self.root / "bases"; self.bases.mkdir()
        self.repo, self.sha, self.version = "Prime-Unlimited/Prime-Unlimited-OS", "a" * 40, "1.2.3"
        for target in ("jc11s", "jp24"):
            (self.packages / f"mod-v{self.version}-{target}.zip").write_bytes(target.encode())
            (self.bases / f"{target}.json").write_text(f"{target}\n")
        self.candidate = create_candidate(self.repo, self.sha, "mod-v1.2.3", "mod", self.version, self.packages)
        self.contracts = {
            "jc11s": {"catalogId":"c1","model":"JC11S","architecture":"armv7l","abi":"gnueabihf","firmware":["5.3.0"],"qtQuick":"2.12","overlayHostAvailable":True},
            "jp24": {"catalogId":"c2","model":"JP24","architecture":"aarch64","abi":"gnu","firmware":["5.3.0"],"qtQuick":"2.12","overlayHostAvailable":False},
        }
        self.approval = {"schemaVersion":1,"modId":"mod","version":self.version,"targets":{}}
        for target, contract in self.contracts.items():
            self.approval["targets"][target] = {"technicallyCompatible":target=="jc11s","hardwareVerified":target=="jc11s","officialCatalogApproved":target=="jc11s","catalog":{"id":f"mod-{target}","name":"Mod","version":self.version,"revision":1,"entrypoint":"mod/start.sh","approval":{"maintainer":"Prime Unlimited","devices":[contract["model"]]}}}
        self.assets = {target:f"https://api.github.com/repos/{self.repo}/releases/assets/{index}" for index,target in enumerate(("jc11s","jp24"),1)}

    def tearDown(self): self.temp.cleanup()

    def finish(self, candidate=None, approval=None, contracts=None, assets=None):
        return finalize(candidate or self.candidate, approval or self.approval, contracts or self.contracts, self.packages, assets or self.assets, self.bases)

    def test_final_manifest_preserves_provenance_and_hashes(self):
        result = self.finish()
        self.assertEqual((result["repository"], result["commitSha"], result["tag"], result["modId"], result["version"]), (self.repo,self.sha,"mod-v1.2.3","mod",self.version))
        self.assertEqual(result["expectedBaseCatalogSha256"]["jc11s"], hashlib.sha256(b"jc11s\n").hexdigest())

    def test_duplicate_or_unsafe_candidate_targets_fail(self):
        candidate = copy.deepcopy(self.candidate); candidate["targets"][1] = copy.deepcopy(candidate["targets"][0])
        with self.assertRaises(ManifestError): self.finish(candidate=candidate)
        candidate = copy.deepcopy(self.candidate); candidate["targets"][0]["filename"] = "../escape.zip"
        with self.assertRaises(ManifestError): self.finish(candidate=candidate)

    def test_non_boolean_approval_and_wrong_repository_asset_fail(self):
        approval = copy.deepcopy(self.approval); approval["targets"]["jc11s"]["hardwareVerified"] = 1
        with self.assertRaises(ManifestError): self.finish(approval=approval)
        assets = dict(self.assets); assets["jc11s"] = "https://api.github.com/repos/attacker/repo/releases/assets/1"
        with self.assertRaises(ManifestError): self.finish(assets=assets)

    def test_changed_package_and_malformed_contract_fail(self):
        (self.packages / "mod-v1.2.3-jc11s.zip").write_bytes(b"changed")
        with self.assertRaises(ManifestError): self.finish()
        (self.packages / "mod-v1.2.3-jc11s.zip").write_bytes(b"jc11s")
        contracts = copy.deepcopy(self.contracts); contracts["jp24"]["overlayHostAvailable"] = "yes"
        with self.assertRaises(ManifestError): self.finish(contracts=contracts)

    def test_candidate_rejects_bad_tag_sha_and_repository(self):
        for repo, sha, tag in (("bad",self.sha,"mod-v1.2.3"),(self.repo,"a"*64,"mod-v1.2.3"),(self.repo,self.sha,"wrong")):
            with self.subTest(repo=repo,sha=sha,tag=tag), self.assertRaises(ManifestError):
                create_candidate(repo, sha, tag, "mod", self.version, self.packages)


if __name__ == "__main__": unittest.main()
