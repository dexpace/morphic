"""Tests for sweep. Run: python3 -m unittest discover -s .claude/skills/morphic-verifying-changes/scripts"""
import io
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

import sweep

# A stand-in for cmd/morphic: prints IR naming its side, and differs or hangs where told to.
FAKE = """#!{python}
import sys, time
spec = open(sys.argv[-1]).read()
if "{side}" == "head" and "slow" in spec:
    time.sleep(30)
print("{{\\"side\\": \\"same\\"}}" if "{side}" == "base" or "changed" not in spec else "{{}}")
sys.stderr.write("warning\\n" if "diag" in spec and "{side}" == "head" else "")
sys.exit(1 if "error" in spec else 0)
"""


class SweepTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.root = Path(self.dir.name)
        self.corpus = self.root / "corpus"
        self.corpus.mkdir()
        for name, extra in [("same", ""), ("changed", "changed"), ("diag", "diag"), ("error", "error")]:
            (self.corpus / f"{name}.yaml").write_text(f"openapi: 3.1.0\n# {extra}\n")
        (self.corpus / "golden.json").write_text('{"irVersion": "0.1.0"}\n')
        (self.corpus / "notes.txt").write_text("openapi: 3.1.0\n")
        self.bins = {}
        for side in ("base", "head"):
            b = self.root / side
            b.write_text(FAKE.format(python=sys.executable, side=side))
            b.chmod(0o755)
            self.bins[side] = b

    def tearDown(self):
        self.dir.cleanup()

    def sweep(self, specs, limit=20):
        out = self.root / "out"
        results = sweep.compare(self.bins["base"], self.bins["head"], specs, out, [], limit, 2)
        buf = io.StringIO()
        with redirect_stdout(buf):
            code = sweep.report(results, self.root, out)
        return code, results, buf.getvalue()

    def test_only_openapi_and_swagger_documents_are_swept(self):
        names = [p.name for p in sweep.find_specs([self.corpus])]
        self.assertEqual(names, ["changed.yaml", "diag.yaml", "error.yaml", "same.yaml"])

    def test_each_kind_of_difference_is_reported(self):
        code, results, text = self.sweep(sweep.find_specs([self.corpus]))
        self.assertEqual(code, 1)
        what = {r[3].name: r[4] for r in results}
        self.assertEqual(what, {"changed.yaml": "ir", "diag.yaml": "diagnostics", "error.yaml": "", "same.yaml": ""})
        self.assertIn("base exit codes: 0x3 1x1", text)
        self.assertIn("2 of 4 specs differ", text)

    def test_identical_builds_report_nothing_and_still_print_what_ran(self):
        self.bins["head"] = self.bins["base"]
        code, _, text = self.sweep(sweep.find_specs([self.corpus]))
        self.assertEqual(code, 0)
        self.assertIn("head exit codes: 0x3 1x1", text)
        self.assertIn("no spec differs", text)

    def test_a_compile_past_the_limit_is_killed_and_recorded(self):
        slow = self.corpus / "slow.yaml"
        slow.write_text("openapi: 3.1.0\n# slow\n")
        _, results, _ = self.sweep([slow], limit=1)
        self.assertEqual(results[0][1:3], (0, sweep.KILLED))

    def test_flags_after_a_double_dash_reach_the_compile(self):
        args = sweep.parse(["-b", "x", "testdata", "--", "--opt", "a=b"])
        self.assertEqual((args.base, args.corpus, args.flags), ("x", ["testdata"], ["--opt", "a=b"]))


if __name__ == "__main__":
    unittest.main()
