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
        out.mkdir(exist_ok=True)
        results = sweep.compare(self.bins["base"], self.bins["head"], specs, out, [], limit, 2)
        buf = io.StringIO()
        # find_specs resolves each spec's path, so the root is resolved too: on macOS a temporary
        # directory is reached through a symlink, and the paths would not be relative to it.
        with redirect_stdout(buf):
            code = sweep.report(results, self.root.resolve(), out)
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

    def test_a_yaml_document_is_judged_by_a_version_key_anywhere_in_it(self):
        cases = {b"openapi: 3.1.0\n": True, b"swagger: '2.0'\n": True, b"'openapi': 3.0.3\n": True,
                 b"openapi:schema: x\n": False, b"info:\n  title: openapi\n": False,
                 b"x-openapi: 1\n": False, b'"openapi:schema": 1\n': False,
                 b"myopenapi: 1\n": False, b"x.openapi: 1\n": False, b"openapi:": True,
                 b'"openapi" : 3.1.0\n': True, b'{"swagger":"2.0", info: {}}\n': True,
                 b'{openapi: 3.1.0, info: {title: t, version: "1"}, paths: {}}\n': True,
                 b"---\n{openapi: 3.1.0, paths: {}}\n": True,
                 b"{info: {title: t}, swagger: '2.0'}\n": True,
                 b'{"openapi":"3.1.0", info: {}}\n': True,
                 b"&version openapi: 3.1.0\n": True, b"!!str openapi: 3.1.0\n": True,
                 b"? openapi\n: 3.1.0\n": True}
        for data, want in cases.items():
            with self.subTest(data=data):
                self.assertEqual(sweep.is_spec(data), want)

    def test_a_flow_mapping_of_any_length_is_a_spec(self):
        data = b'{openapi: 3.1.0, info: {title: t, version: "1"}, paths: {}, x-pad: "%s"}\n'
        self.assertTrue(sweep.is_spec(data % (b"a" * 70000)))

    def test_json_too_deep_to_parse_is_read_as_text_rather_than_ending_the_sweep(self):
        deep = b"[" * 200000 + b"]" * 200000
        self.assertTrue(sweep.is_spec(b'{"openapi": "3.1.0", "x-deep": ' + deep + b"}"))
        self.assertFalse(sweep.is_spec(b'{"x-deep": ' + deep + b"}"))

    def test_each_kind_of_difference_is_reported(self):
        code, results, text = self.sweep(sweep.find_specs([self.corpus]))
        self.assertEqual(code, 1)
        what = {r.spec.name: r.what for r in results}
        want = {"changed.yaml": "ir", "diag.yaml": "diagnostics", "error.yaml": "",
                "exits.yaml": "exit(0->1)", "same.yaml": ""}
        self.assertEqual(what, want)
        out = self.root / "out"
        # Each spec's IR is 17 bytes, bar head's 3 for changed.yaml. The hint compares the IR,
        # where diagnostics alone would show an IR change as nothing.
        self.assertEqual(text, "".join([
            f"sweep: 5 specs; a row per spec in {out}/results.tsv\n",
            "  base exit codes: 0x4 1x1; IR bytes: 85\n",
            "  head exit codes: 0x3 1x2; IR bytes: 71\n",
            "sweep: 3 of 5 specs differ (n, then what):\n",
            "  1  corpus/changed.yaml: ir\n",
            "  2  corpus/diag.yaml: diagnostics\n",
            "  4  corpus/exits.yaml: exit(0->1)\n",
            f"compare one with: diff {out}/runs/<n>/base.ir {out}/runs/<n>/head.ir, and the "
            ".diag pair for its diagnostics\n"]))
        rows = (out / "results.tsv").read_text().splitlines()
        self.assertEqual(rows, ["n\tbase\thead\tdiffers\tspec",
                                "1\t0\t0\tir\tcorpus/changed.yaml",
                                "2\t0\t0\tdiagnostics\tcorpus/diag.yaml",
                                "3\t1\t1\t-\tcorpus/error.yaml",
                                "4\t0\t1\texit(0->1)\tcorpus/exits.yaml",
                                "5\t0\t0\t-\tcorpus/same.yaml"])

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
        self.assertEqual(sweep.compile_one(self.bins["head"], slow, [], 1).code, sweep.KILLED)

    def test_a_killed_compile_keeps_what_it_wrote(self):
        slow = self.root / "partial"
        slow.write_text(f"#!{sys.executable}\nimport sys, time\nprint('part', flush=True)\n"
                        "sys.stderr.write('warned\\n')\nsys.stderr.flush()\ntime.sleep(30)\n")
        slow.chmod(0o755)
        spec = self.corpus / "same.yaml"
        self.assertEqual(sweep.compile_one(slow, spec, [], 1), (sweep.KILLED, b"part\n", b"warned\n"))

    def test_specs_that_fail_alike_agree_and_their_codes_are_printed(self):
        # The sweep judges nothing: a spec neither build finished agrees, and only its codes,
        # printed and written, show that it was compiled by neither.
        out = self.root / "out"
        out.mkdir()
        results = [sweep.Result(1, self.corpus / "same.yaml", (sweep.KILLED, sweep.KILLED), (0, 0),
                                "", None, ""),
                   sweep.Result(2, self.corpus / "diag.yaml", (2, 2), (0, 0), "", None, ""),
                   sweep.Result(3, self.corpus / "error.yaml", (-9, -9), (0, 0), "", None, "")]
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

    def test_help_prints_the_docstring_that_gives_each_option(self):
        out = io.StringIO()
        with redirect_stdout(out), self.assertRaises(SystemExit) as e:
            sweep.parse(["--help"])
        self.assertEqual(e.exception.code, 0)
        self.assertIn(sweep.__doc__.strip(), out.getvalue())

    def test_flags_after_a_double_dash_reach_the_compile(self):
        args = sweep.parse(["-b", "x", "testdata", "--", "--opt", "a=b"])
        got = (args.base, args.corpus, args.flags)
        self.assertEqual(got, ("x", ["testdata"], ["--opt", "a=b"]))
        # A build that prints the command line it was given, as its IR.
        echo = self.root / "echo"
        echo.write_text(f"#!{sys.executable}\nimport sys\nprint(' '.join(sys.argv[1:]))\n")
        echo.chmod(0o755)
        spec = self.corpus / "same.yaml"
        got = sweep.compile_one(echo, spec, args.flags, 20)
        self.assertEqual(got, (0, f"compile --opt a=b -- {spec}\n".encode(), b""))

    def test_only_a_spec_that_differs_keeps_its_outputs(self):
        self.sweep(sweep.find_specs([self.corpus]))
        runs = self.root / "out" / "runs"
        self.assertEqual(sorted(p.name for p in runs.iterdir()), ["1", "2", "4"])
        self.assertEqual((runs / "1" / "base.ir").read_text(), '{"side": "same"}\n')
        self.assertEqual((runs / "1" / "head.ir").read_text(), "{}\n")
        self.assertEqual((runs / "2" / "head.diag").read_text(), "warning\n")

    def test_a_rerun_leaves_no_outputs_of_an_earlier_one(self):
        stale = self.root / "out" / "runs" / "5" / "base.ir"  # same.yaml, which agrees
        stale.parent.mkdir(parents=True)
        stale.write_text("from an earlier run")
        self.sweep(sweep.find_specs([self.corpus]))
        self.assertFalse(stale.exists())

    def test_outputs_that_cannot_be_written_cost_the_diff_and_not_the_result(self):
        # A file where the directory must go fails each write as a full disk would.
        out = self.root / "out"
        out.mkdir()
        (out / "runs").write_text("")
        code, results, text = self.sweep(sweep.find_specs([self.corpus]))
        self.assertEqual(code, 1)
        self.assertEqual([r.what for r in results],
                         ["ir", "diagnostics", "", "exit(0->1)", ""])
        self.assertIn("sweep: 3 of 5 specs differ", text)
        self.assertIn("sweep: the outputs of 3 of them were not kept: ", text)
        self.assertEqual(len((out / "results.tsv").read_text().splitlines()), 6)

    def test_a_write_cut_short_leaves_no_part_of_a_pair(self):
        run = self.root / "run"
        run.mkdir()
        (run / "head.diag").mkdir()  # the last of the four writes fails
        why = sweep.keep(run, sweep.Output(0, b"a", b""), sweep.Output(0, b"b", b""))
        self.assertIn("head.diag", why)
        self.assertFalse(run.exists())

    def test_a_table_that_cannot_be_written_leaves_what_was_found(self):
        out = self.root / "out"
        out.mkdir()
        (out / "results.tsv").mkdir()
        code, _, text = self.sweep(sweep.find_specs([self.corpus]))
        self.assertEqual(code, 1)
        self.assertIn("sweep: 5 specs; no rows written: ", text)
        self.assertIn("  1  corpus/changed.yaml: ir\n", text)


# A stand-in that opens its document with a version of its own, as cmd/morphic does: "moved" changes
# the head's IR, "diag" its diagnostics, "late" puts the version past the first member, "gone" has
# the head write nothing, and "failed" has neither side write a document.
STAMPED = """#!{python}
import sys
spec = open(sys.argv[-1]).read()
head = "{version}" != "0.1.0"
if "failed" in spec or ("gone" in spec and head):
    sys.exit(1)
sys.stderr.write("warning\\n" if "diag" in spec and head else "")
x = 2 if "moved" in spec and head else 1
if "late" in spec:
    print('{{"x": 1, "irVersion": "{version}"}}')
else:
    print('{{\\n  "irVersion": "{version}",\\n  "x": %d\\n}}' % x)
"""


class StampTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.root = Path(self.dir.name).resolve()
        self.bins = {}
        for side, version in (("base", "0.1.0"), ("head", "0.2.0")):
            b = self.root / side
            b.write_text(STAMPED.format(python=sys.executable, version=version))
            b.chmod(0o755)
            self.bins[side] = b

    def tearDown(self):
        self.dir.cleanup()

    def sweep(self, *names):
        specs = []
        for name in names:
            specs.append(self.root / f"{name}.yaml")
            specs[-1].write_text(f"openapi: 3.1.0\n# {name}\n")
        out = self.root / "out"
        out.mkdir()
        results = sweep.compare(self.bins["base"], self.bins["head"], specs, out, [], 20, 2)
        buf = io.StringIO()
        with redirect_stdout(buf):
            code = sweep.report(results, self.root, out)
        return code, results, buf.getvalue()

    def test_a_spec_differing_in_its_stamp_alone_is_not_listed(self):
        code, results, text = self.sweep("plain", "other")
        self.assertEqual(code, 0)
        self.assertEqual([r.what for r in results], [sweep.STAMP_ALONE] * 2)
        self.assertEqual(text.splitlines()[3:], [
            "sweep: irVersion 0.1.0 -> 0.2.0 on 2 specs, set aside below; 2 of them differ in "
            "nothing else",
            "sweep: no spec's exit code, IR or diagnostics differ"])
        self.assertFalse((self.root / "out" / "runs").exists())
        rows = (self.root / "out" / "results.tsv").read_text().splitlines()
        self.assertEqual(rows[1], "1\t0\t0\tstamp\tplain.yaml")

    def test_a_stamp_hides_nothing_past_it(self):
        code, results, text = self.sweep("plain", "moved", "diag", "late", "gone", "failed")
        self.assertEqual(code, 1)
        self.assertEqual([r.what for r in results],
                         [sweep.STAMP_ALONE, "ir", "diagnostics", "ir", "exit(0->1) ir", ""])
        self.assertIn("on 3 specs, set aside below; 1 of them differ in nothing else\n", text)
        self.assertIn("sweep: 4 of 6 specs differ", text)
        self.assertNotIn("plain.yaml", text)
        self.assertEqual(sorted((self.root / "out" / "runs").iterdir()),
                         [self.root / "out" / "runs" / n for n in ("2", "3", "4", "5")])

    def test_a_stamp_is_read_only_where_a_document_opens(self):
        for ir, want in {b'{\n  "irVersion": "1",\n  "x": 1\n}\n': ("1", b',\n  "x": 1\n}\n'),
                         b'{"irVersion":"1"}': ("1", b"}"),
                         b'{"x": {"irVersion": "1"}}': ("", b'{"x": {"irVersion": "1"}}'),
                         b'[{"irVersion": "1"}]': ("", b'[{"irVersion": "1"}]'),
                         b"": ("", b"")}.items():
            with self.subTest(ir=ir):
                self.assertEqual(sweep.split_stamp(ir), want)

    def test_an_output_with_no_stamp_on_one_side_is_compared_whole(self):
        stamped = sweep.Output(0, b'{"irVersion": "1"}', b"")
        self.assertEqual(sweep.difference(stamped, sweep.Output(0, b"", b"")), ("ir", None))


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
