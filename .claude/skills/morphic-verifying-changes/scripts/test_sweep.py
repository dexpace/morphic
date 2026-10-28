"""Tests for sweep.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-verifying-changes/scripts
"""
import io
import os
import subprocess
import sys
import tempfile
import unittest
import unittest.mock
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path

import sweep

HERE = Path(__file__).resolve().parent

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
        kinds = [("same", ""), ("changed", "changed"), ("diag", "diag"), ("error", "error")]
        for name, extra in kinds:
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

    def test_a_json_document_is_judged_by_its_top_level_keys(self):
        cases = {b'{"openapi":"3.0.3","info":{}}': True, b' {"swagger":"2.0"}': True,
                 b'{\n  "info": {\n    "openapi": "3.0.0"\n  }\n}\n': False,
                 b'{\n  "openapi:schema": {}\n}\n': False,
                 b'{\n  "openapi": "3.1.0",\n  "paths": {\n': True}
        for data, want in cases.items():
            with self.subTest(data=data):
                self.assertEqual(sweep.is_spec(data), want)

    def test_a_byte_order_mark_is_read_past(self):
        for data in (b'\xef\xbb\xbf{"openapi":"3.0.3"}', b"\xef\xbb\xbfopenapi: 3.0.3\n"):
            with self.subTest(data=data):
                self.assertTrue(sweep.is_spec(data))

    def test_a_yaml_document_is_judged_by_a_version_key_starting_a_line(self):
        cases = {b"openapi: 3.1.0\n": True, b"swagger: '2.0'\n": True, b"'openapi': 3.0.3\n": True,
                 b"openapi:schema: x\n": False, b"info:\n  title: openapi\n": False}
        for data, want in cases.items():
            with self.subTest(data=data):
                self.assertEqual(sweep.is_spec(data), want)

    def test_each_kind_of_difference_is_reported(self):
        code, results, text = self.sweep(sweep.find_specs([self.corpus]))
        self.assertEqual(code, 1)
        what = {r[3].name: r[4] for r in results}
        want = {"changed.yaml": "ir", "diag.yaml": "diagnostics", "error.yaml": "", "same.yaml": ""}
        self.assertEqual(what, want)
        self.assertIn("base exit codes: 0x3 1x1", text)
        self.assertIn("2 of 4 specs differ", text)

    def test_identical_builds_report_nothing_and_still_print_what_ran(self):
        self.bins["head"] = self.bins["base"]
        code, _, text = self.sweep(sweep.find_specs([self.corpus]))
        self.assertEqual(code, 0)
        self.assertIn("head exit codes: 0x3 1x1", text)
        self.assertIn("no spec differs", text)

    def test_a_compile_past_the_limit_is_killed_and_recorded(self):
        # Only the hanging side is timed: a healthy one under a one-second limit would make the
        # test depend on how fast an interpreter starts on a loaded machine.
        slow = self.corpus / "slow.yaml"
        slow.write_text("openapi: 3.1.0\n# slow\n")
        prefix = self.root / "slow"
        self.assertEqual(sweep.compile_one(self.bins["head"], slow, prefix, [], 1), sweep.KILLED)
        self.assertTrue(Path(f"{prefix}.ir").exists())

    def test_a_spec_finished_on_neither_side_is_not_compared(self):
        out = self.root / "out"
        for n in (1, 2):
            (out / "runs" / str(n)).mkdir(parents=True)
            for side in ("base", "head"):
                (out / "runs" / str(n) / f"{side}.ir").write_text("")
        # Partial output from two killed compiles may differ, and still compares nothing.
        stuck = (1, sweep.KILLED, sweep.KILLED, self.corpus / "same.yaml", "diagnostics")
        refused = (1, 2, 2, self.corpus / "same.yaml", "")
        changed = (2, 0, 0, self.corpus / "changed.yaml", "ir")
        cases = (([stuck], 2), ([stuck, changed], 1), ([refused], 2), ([refused, changed], 1))
        for results, want in cases:
            with self.subTest(want=want):
                buf = io.StringIO()
                with redirect_stdout(buf):
                    self.assertEqual(sweep.report(results, self.root, out), want)
                text = buf.getvalue()
                self.assertIn("1 spec(s) finished compiling on neither side", text)
                reason = ("failed to run (2)" if results[0] is refused
                          else "hit the time limit (137)")
                self.assertIn(f"same.yaml: {reason}", text)
                self.assertNotIn("no spec differs", text)
                self.assertEqual("1 of 2 specs differ" in text, want == 1, text)

    def test_a_limit_or_job_count_under_one_is_a_usage_error(self):
        for argv in (["-t", "0"], ["-j", "0"], ["-t", "-5"]):
            with self.subTest(argv=argv):
                with redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as e:
                    sweep.parse(argv)
                self.assertEqual(e.exception.code, 2)

    def test_flags_after_a_double_dash_reach_the_compile(self):
        args = sweep.parse(["-b", "x", "testdata", "--", "--opt", "a=b"])
        got = (args.base, args.corpus, args.flags)
        self.assertEqual(got, ("x", ["testdata"], ["--opt", "a=b"]))


class CrashTest(unittest.TestCase):
    def test_a_missing_tool_exits_2_never_a_verdict(self):
        env = {"PATH": "", "PYTHONDONTWRITEBYTECODE": "1"}
        run = subprocess.run([sys.executable, str(HERE / "sweep.py")], capture_output=True,
                             text=True, env=env)
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertEqual(run.stdout, "")


class UnheldBaseTest(unittest.TestCase):
    def test_a_base_the_branch_lacks_draws_a_warning(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            env = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@localhost",
                   "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@localhost"}

            def commit(name):
                (root / name).write_text(name)
                subprocess.run(["git", "-C", d, "add", name], check=True, env=env)
                subprocess.run(["git", "-C", d, "commit", "-qm", name], check=True, env=env)
                return subprocess.run(["git", "-C", d, "rev-parse", "HEAD"], check=True,
                                      capture_output=True, text=True).stdout.strip()

            subprocess.run(["git", "init", "-q", d], check=True)
            first = commit("a")
            later = commit("b")
            subprocess.run(["git", "-C", d, "checkout", "-q", "--detach", first], check=True)
            self.assertIsNone(sweep.unheld_base_warning(root, first))
            warning = sweep.unheld_base_warning(root, later)
            self.assertIsNotNone(warning)
            self.assertIn("does not hold", warning)


class DecodeTest(unittest.TestCase):
    def test_a_build_error_that_is_not_utf8_is_reported_not_crashed_on(self):
        with tempfile.TemporaryDirectory() as d:
            go = Path(d) / "go"
            go.write_text("#!/bin/sh\nprintf 'bad \\342\\202 byte\\n' >&2\nexit 1\n")
            go.chmod(0o755)
            env = {**os.environ, "PATH": d + os.pathsep + os.environ["PATH"]}
            with unittest.mock.patch.dict(os.environ, env):
                with self.assertRaises(subprocess.CalledProcessError) as caught:
                    sweep.build(Path(d), "./x", Path(d) / "out")
        self.assertIn("bad \ufffd byte", caught.exception.stderr)

if __name__ == "__main__":
    unittest.main()
