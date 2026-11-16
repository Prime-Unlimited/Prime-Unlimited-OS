import unittest

from registry import ModError
from resolver import Resolver, satisfies


DEVICE = {"model": "SYNTH", "firmware": "1.5.0", "architecture": "arm64", "abi": "gnu"}


def record(ident, version, **extra):
    return {"id": ident, "version": version, "models": ["SYNTH"],
            "firmwareConstraint": ">=1.0,<2.0", "architecture": "arm64", "abi": "gnu", **extra}


class ResolverTests(unittest.TestCase):
    def test_constraints(self):
        self.assertTrue(satisfies("1.2.3", ">=1.2,<2.0"))
        self.assertTrue(satisfies("1.4.0", "^1.2"))
        self.assertFalse(satisfies("2.0.0", "^1.2"))

    def test_transitive_deterministic_order(self):
        catalog = {"mods": [record("app", "1.0", dependencies=[{"id": "helper", "constraint": "^2.0"}]),
                            record("helper", "2.1")], "libraries": []}
        result = Resolver(catalog, DEVICE).resolve([{"id": "app"}])
        self.assertEqual(result.order, (("mod", "helper"), ("mod", "app")))

    def test_library_dependencies(self):
        sha = "a" * 64
        catalog = {"mods": [record("app", "1", libraries=[{"id": "top", "constraint": "1"}])],
                   "libraries": [record("top", "1", sha256=sha,
                                                dependencies=[{"type": "library", "id": "base", "version": "1"}]),
                                 record("base", "1", sha256="b" * 64)]}
        result = Resolver(catalog, DEVICE).resolve([{"id": "app"}])
        self.assertEqual([x["id"] for x in result.libraries], ["base", "top"])

    def test_cycle_missing_conflict_and_abi(self):
        with self.assertRaisesRegex(ModError, "cyclic"):
            Resolver({"mods": [record("a", "1", dependencies=[{"id": "b"}]),
                               record("b", "1", dependencies=[{"id": "a"}])], "libraries": []}, DEVICE).resolve([{"id": "a"}])
        with self.assertRaisesRegex(ModError, "cannot be satisfied"):
            Resolver({"mods": [record("a", "1", dependencies=[{"id": "gone"}])], "libraries": []}, DEVICE).resolve([{"id": "a"}])
        with self.assertRaisesRegex(ModError, "cannot be satisfied"):
            Resolver({"mods": [record("a", "1", conflicts=["old"])], "libraries": []}, DEVICE).resolve([{"id": "a"}], {"old": {}})
        with self.assertRaisesRegex(ModError, "cannot be satisfied"):
            Resolver({"mods": [record("a", "1", abi="musl")], "libraries": []}, DEVICE).resolve([{"id": "a"}])

    def test_incompatible_combined_versions(self):
        catalog = {"mods": [record("a", "1", dependencies=[{"id": "dep", "constraint": "<2"}]),
                            record("b", "1", dependencies=[{"id": "dep", "constraint": ">=2"}]),
                            record("dep", "1"), record("dep", "2")], "libraries": []}
        with self.assertRaisesRegex(ModError, "cannot be satisfied"):
            Resolver(catalog, DEVICE).resolve([{"id": "a"}, {"id": "b"}])


if __name__ == "__main__":
    unittest.main()
