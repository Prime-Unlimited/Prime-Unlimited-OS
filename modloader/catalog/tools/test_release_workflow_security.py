import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
OFFICIAL = ROOT / ".github/workflows/official-mod-release.yml"
REUSABLE = ROOT / ".github/workflows/reusable-mod-release.yml"
PRIME_CLOCK = ROOT / ".github/workflows/prime-clock.yml"


class ReleaseWorkflowSecurityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.official = OFFICIAL.read_text()
        cls.reusable = REUSABLE.read_text()
        cls.prime_clock = PRIME_CLOCK.read_text()

    def test_secret_exists_only_in_protected_workflow(self):
        workflows = list((ROOT / ".github/workflows").glob("*.yml"))
        users = [path for path in workflows if "PRIME_UNLIMITED_CATALOG_SIGNING_KEY" in path.read_text()]
        self.assertEqual(users, [OFFICIAL])
        self.assertEqual(self.official.count("environment: prime-unlimited-release"), 1)
        self.assertNotIn("pull_request_target", self.official)
        self.assertNotIn("secrets: inherit", self.official)

    def test_official_releases_are_serialized_and_branch_guarded(self):
        self.assertIn("group: prime-unlimited-official-catalog-release", self.official)
        self.assertIn("cancel-in-progress: false", self.official)
        self.assertGreaterEqual(self.official.count("github.ref == 'refs/heads/main'"), 2)
        self.assertIn('rev-parse "origin/$TRUSTED_BRANCH"', self.official)

    def test_secret_job_has_no_mutable_actions_or_archive_execution(self):
        secret_job = self.official.split("  sign-and-publish:", 1)[1]
        self.assertNotRegex(secret_job, r"(?m)^\s*- uses:")
        self.assertNotIn("unzip ", secret_job)
        self.assertNotIn("-generate", secret_job)
        self.assertIn("unset SIGNING_KEY", secret_job)
        self.assertIn("umask 077", secret_job)
        self.assertIn("gh attestation verify", secret_job)
        self.assertIn(".repository==$repo", secret_job)
        self.assertIn(".commitSha==$sha", secret_job)
        self.assertIn("recomputed-SHA256SUMS.txt", secret_job)
        self.assertIn("recomputed-SIZES.txt", secret_job)
        self.assertIn("-verify -catalog", secret_job)
        self.assertIn("cmp \"$work/output/$target.trust.json\"", secret_job)

    def test_candidate_is_read_only_and_attested(self):
        self.assertNotIn("contents: write", self.reusable)
        self.assertNotIn("gh release create", self.reusable)
        self.assertIn("attestations: write", self.reusable)
        self.assertRegex(
            self.reusable,
            r"actions/attest-build-provenance@[0-9a-f]{40}",
        )
        self.assertIn("contents: read", self.prime_clock)
        mutable = re.findall(r"(?m)^\s*- uses:\s*([^\s]+)@([^\s]+)", self.reusable)
        self.assertTrue(mutable)
        for action, ref in mutable:
            with self.subTest(action=action):
                self.assertRegex(ref, r"^[0-9a-f]{40}$")

    def test_provenance_and_transaction_gates_are_present(self):
        for required in (
            ".head_sha==$sha",
            ".head_repository.full_name==$repo",
            ".path==$path",
            "gh attestation verify",
            "--source-digest \"$INPUT_SHA\"",
            "artifact_digest",
            "merge-base --is-ancestor",
            "git -C \"$work/trusted\" push",
            "gh release edit \"$INPUT_TAG\"",
        ):
            self.assertIn(required, self.official)
        self.assertLess(
            self.official.index('gh release edit "$INPUT_TAG"'),
            self.official.index('git -C "$work/trusted" push'),
        )
        self.assertIn('test "$(jq -r .draft "$work/release.json")" = false', self.official)


if __name__ == "__main__":
    unittest.main()
