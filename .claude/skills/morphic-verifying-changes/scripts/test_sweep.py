"""Tests for sweep.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-verifying-changes/scripts
"""
import contextlib
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
sys.exit(1 if "error" in spec or ("{side}" == "head" and "exits" in spec) else 0)
"""


class SweepTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.root = Path(self.dir.name)
        self.corpus = self.root / "corpus"
        self.corpus.mkdir()
        kinds = [("same", ""), ("changed", "changed"), ("diag", "diag"), ("error", "error"),
                 ("exits", "exits")]
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
        self.assertEqual(names, ["changed.yaml", "diag.yaml", "error.yaml", "exits.yaml",
                                 "same.yaml"])

    def test_a_corpus_file_is_read_as_one_document(self):
        specs = [self.corpus / "same.yaml", self.corpus / "notes.txt", self.corpus / "golden.json"]
        self.assertEqual(sweep.find_specs(specs), [specs[0].resolve()])

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

    def test_a_yaml_document_is_judged_by_a_version_key_first_on_a_line(self):
        cases = {b"openapi: 3.1.0\n": True, b"swagger: '2.0'\n": True, b"'openapi': 3.0.3\n": True,
                 b"openapi:schema: x\n": False, b"info:\n  title: openapi\n": False}
        for data, want in cases.items():
            with self.subTest(data=data):
                self.assertEqual(sweep.is_spec(data), want)

    def test_each_kind_of_difference_is_reported(self):
        code, results, text = self.sweep(sweep.find_specs([self.corpus]))
        self.assertEqual(code, 1)
        what = {r[3].name: r[4] for r in results}
        want = {"changed.yaml": "ir", "diag.yaml": "diagnostics", "error.yaml": "",
                "exits.yaml": "exit(0->1)", "same.yaml": ""}
        self.assertEqual(what, want)
        self.assertIn("base exit codes: 0x4 1x1", text)
        self.assertIn("head exit codes: 0x3 1x2", text)
        self.assertIn("3 of 5 specs differ", text)
        # The hint compares the IR, where diagnostics alone would show an IR change as nothing.
        self.assertIn("/runs/<n>/base.ir ", text)
        self.assertIn("/runs/<n>/head.ir, and the .diag pair", text)
        rows = (self.root / "out" / "results.tsv").read_text().splitlines()
        self.assertEqual(rows[0], "n\tbase\thead\tdiffers\tspec")
        table = {row.split("\t")[4]: row.split("\t")[1:4] for row in rows[1:]}
        self.assertEqual(table, {"corpus/changed.yaml": ["0", "0", "ir"],
                                 "corpus/diag.yaml": ["0", "0", "diagnostics"],
                                 "corpus/error.yaml": ["1", "1", "-"],
                                 "corpus/exits.yaml": ["0", "1", "exit(0->1)"],
                                 "corpus/same.yaml": ["0", "0", "-"]})

    def test_identical_builds_report_nothing_and_still_print_what_ran(self):
        self.bins["head"] = self.bins["base"]
        code, _, text = self.sweep(sweep.find_specs([self.corpus]))
        self.assertEqual(code, 0)
        self.assertIn("head exit codes: 0x4 1x1", text)
        self.assertIn("no spec's exit code, IR or diagnostics differ", text)

    def test_a_compile_past_the_limit_is_killed_and_recorded(self):
        # Only the hanging side is timed: a healthy one under a one-second limit would make the
        # test depend on how fast an interpreter starts on a loaded machine.
        slow = self.corpus / "slow.yaml"
        slow.write_text("openapi: 3.1.0\n# slow\n")
        prefix = self.root / "slow"
        self.assertEqual(sweep.compile_one(self.bins["head"], slow, prefix, [], 1), sweep.KILLED)
        self.assertTrue(Path(f"{prefix}.ir").exists())

    def test_specs_that_fail_alike_agree_and_their_codes_are_printed(self):
        # The sweep judges nothing: a spec neither build finished agrees, and only its codes,
        # printed and written, show that it was compiled by neither.
        out = self.root / "out"
        for n in (1, 2, 3):
            (out / "runs" / str(n)).mkdir(parents=True)
            for side in ("base", "head"):
                (out / "runs" / str(n) / f"{side}.ir").write_text("")
        results = [(1, sweep.KILLED, sweep.KILLED, self.corpus / "same.yaml", ""),
                   (2, 2, 2, self.corpus / "diag.yaml", ""),
                   (3, -9, -9, self.corpus / "error.yaml", "")]
        buf = io.StringIO()
        with redirect_stdout(buf):
            self.assertEqual(sweep.report(results, self.root, out), 0)
        self.assertIn("base exit codes: -9x1 2x1 137x1", buf.getvalue())
        rows = (out / "results.tsv").read_text().splitlines()[1:]
        self.assertEqual([row.split("\t")[1:4] for row in rows],
                         [["137", "137", "-"], ["2", "2", "-"], ["-9", "-9", "-"]])

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
        # A build that prints the command line it was given, as its IR.
        echo = self.root / "echo"
        echo.write_text(f"#!{sys.executable}\nimport sys\nprint(' '.join(sys.argv[1:]))\n")
        echo.chmod(0o755)
        spec, prefix = self.corpus / "same.yaml", self.root / "echoed"
        self.assertEqual(sweep.compile_one(echo, spec, prefix, args.flags, 20), 0)
        self.assertEqual(Path(f"{prefix}.ir").read_text(), f"compile --opt a=b -- {spec}\n")


# A go that fails every build, and one whose build writes a stand-in for cmd/morphic that prints
# the same IR for every spec.
FAILING_GO = "#!/bin/sh\necho 'no build here' >&2\nexit 1\n"
BUILDING_GO = ('#!/bin/sh\nwhile [ "$1" != "-o" ]; do shift; done\n'
               "printf '#!/bin/sh\\necho {}\\n' > \"$2\"\nchmod +x \"$2\"\n")


class MainTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        d = Path(self.dir.name)
        self.root, self.empty, self.specs = d / "repo", d / "empty", d / "specs"
        for p in (self.root, self.empty, self.specs):
            p.mkdir()
        (self.specs / "a.yaml").write_text("openapi: 3.1.0\n")
        self.env = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
                    "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"}
        self.git("init", "-q")
        self.first = self.commit("first")

    def tearDown(self):
        self.dir.cleanup()

    def git(self, *args: str) -> str:
        return subprocess.run(["git", "-C", str(self.root), *args], check=True, env=self.env,
                              capture_output=True, text=True).stdout.strip()

    def commit(self, message: str) -> str:
        self.git("commit", "-q", "--allow-empty", "-m", message)
        return self.git("rev-parse", "HEAD")

    def sweep(self, go: str, *argv: str) -> tuple[int, str]:
        """Run the sweep from the repository, with go ahead of the real one."""
        tool = Path(self.dir.name) / "bin" / "go"
        tool.parent.mkdir(exist_ok=True)
        tool.write_text(go)
        tool.chmod(0o755)
        out = io.StringIO()
        path = {"PATH": f"{tool.parent}{os.pathsep}{os.environ['PATH']}"}
        with contextlib.chdir(self.root), redirect_stdout(out), redirect_stderr(out), \
                unittest.mock.patch.dict(os.environ, path):
            code = sweep.main(["-o", str(Path(self.dir.name) / "out"), *argv])
        return code, out.getvalue()

    def test_a_sweep_that_cannot_compare_exits_2(self):
        cases = {"a base that names no commit": ["-b", "no-such-ref", str(self.specs)],
                 "a corpus with no document": ["-b", "HEAD", str(self.empty)],
                 "a build that fails": ["-b", "HEAD", str(self.specs)]}
        for case, argv in cases.items():
            with self.subTest(case=case):
                code, out = self.sweep(FAILING_GO, *argv)
                self.assertEqual(code, 2, out)

    def test_a_base_the_branch_lacks_is_named_before_the_build(self):
        later = self.commit("later")
        self.git("checkout", "-q", "--detach", self.first)
        code, out = self.sweep(FAILING_GO, "-b", later, str(self.specs))
        self.assertEqual(code, 2, out)
        self.assertIn(f"HEAD does not hold {later[:12]}", out)

    def test_both_sides_are_built_and_compared_and_the_base_worktree_removed(self):
        code, out = self.sweep(BUILDING_GO, "-b", "HEAD", str(self.specs))
        self.assertEqual(code, 0, out)
        self.assertIn("sweep: no spec's exit code, IR or diagnostics differ", out)
        self.assertEqual(len(self.git("worktree", "list").splitlines()), 1,
                         "the base worktree outlived the sweep")


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
