"""Tests for affected_packages. Run: python3 -m unittest discover -s .claude/skills/morphic-verifying-changes/scripts"""
import unittest

import affected_packages as ap

M = "example.com/m"


class HitPackagesTest(unittest.TestCase):
    def test_a_file_maps_to_its_nearest_package(self):
        dirs = {"", "ir", "ir/irverify", "compilers/openapi"}
        got = ap.hit_packages(["ir/irverify/testdata/x.json", "compilers/openapi/internal/doc.txt"], dirs, M)
        self.assertEqual(got, {f"{M}/ir/irverify", f"{M}/compilers/openapi"})

    def test_go_and_markdown_changes_add_archtest(self):
        got = ap.hit_packages(["ir/a.go", "README.md"], {"ir"}, M)
        self.assertEqual(got, {f"{M}/ir", f"{M}/internal/archtest"})

    def test_skill_files_affect_no_package(self):
        self.assertEqual(ap.hit_packages([".claude/skills/s/SKILL.md"], {""}, M), set())

    def test_shared_inputs_mean_everything(self):
        for f in ["go.mod", "testdata/golden/a.json", "docs/ir-design.md", "Makefile", ".github/workflows/gate.yml"]:
            with self.subTest(f=f):
                self.assertTrue(ap.EVERYTHING.match(f))
        self.assertFalse(ap.EVERYTHING.match("ir/testdata/a.json"))


class DependentsTest(unittest.TestCase):
    def test_code_and_test_variants_that_import_a_hit_are_listed(self):
        listing = "\n".join([
            f"{M}/a {M}/hit fmt",
            f"{M}/b fmt",
            f"{M}/b [{M}/b.test] {M}/hit fmt",
            f"{M}/c_test [{M}/c.test] {M}/hit",
            f"{M}/c.test {M}/c_test",
            f"{M}/hit fmt",
            f"{M}/d fmt",
        ])
        self.assertEqual(ap.dependents(listing, {f"{M}/hit"}), [f"{M}/a", f"{M}/b", f"{M}/c", f"{M}/hit"])


if __name__ == "__main__":
    unittest.main()
