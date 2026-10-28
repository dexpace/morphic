"""Tests for exported_api.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-shipping-prs/scripts
"""
import io
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path

import exported_api as api

HERE = Path(__file__).resolve().parent
# What `go doc -all` printed for a package holding each form the reader meets, its layout kept.
DOC = '''package p // import "example.com/m/p"

Package p is a probe. Its documentation sits at column 0, so a line of it can
type nothing the reader should take for a declaration.

CONSTANTS

const Quote = "one \\" quote // not a comment"
const URL = "http://x // y"

VARIABLES

var (
	ErrA       = io.EOF
	ErrB error = nil
)

FUNCTIONS

func Long(
	first int,
	second string,
) (res int, err error)
    Long has a long signature.

TYPES

type Alias = T

type I interface {
	io.Reader
	// M does m.
	M(ctx int, names ...string) (n int, err error)
}
    I is an interface.

type Kind int

const (
	KindA Kind = iota // first
	KindB
)
    The kinds.

type T struct {
	// Name is a name.
	Name       string
	LongerName int // trailing

	Fn     func(a int) error
	Nested struct {
		X int
	}
	// Has unexported fields.
}
    T is a type.

func New(a, b int, c string) *T

func (t *T) Method(x int)
'''


def edit(*pairs: tuple[str, str]) -> str:
    text = DOC
    for old, new in pairs:
        assert text.count(old) == 1, old
        text = text.replace(old, new)
    return text


class ReadTest(unittest.TestCase):
    def test_each_form_is_read_as_its_elements(self):
        want = {
            ('const Quote = "one \\" quote // not a comment"', ""),
            ("const URL = \"http://x // y\"", ""), ("var ErrA", ""), ("var ErrB error", ""),
            ("func Long(int, string) (int, error)", ""), ("type Alias = T", ""),
            ("type I interface", ""), ("type I interface", "io.Reader"),
            ("type I interface", "M(int, ...string) (int, error)"), ("type Kind int", ""),
            ("const KindA Kind = iota [0]", ""), ("const KindB [1]", ""), ("type T struct", ""),
            ("type T struct", "Name string"), ("type T struct", "LongerName int"),
            ("type T struct", "Fn func(a int) error"), ("type T struct", "Nested struct { X int }"),
            ("func New(int, int, string) *T", ""), ("func (*T) Method(int)", ""),
        }
        elements, methods = api.api(DOC)
        self.assertEqual(elements, want)
        self.assertEqual(methods, {e for e in want if e[0] == "type I interface" and e[1]})

    def test_a_signature_spanning_lines_on_both_sides_is_read_whole(self):
        doc = ("package q\n\nFUNCTIONS\n\nfunc Both(\n\ta int,\n) (\n\tn int,\n\terr error,\n)\n"
               "    Both spans lines.\n\nfunc After()\n")
        self.assertEqual(api.api(doc)[0],
                         {("func Both(int) (int, error)", ""), ("func After()", "")})

    def test_a_parameter_list_is_read_as_its_types(self):
        cases = {"": "", "a, b int": "int, int", "int, string": "int, string",
                 "chan int": "chan int",
                 "f func(a, b int) error, n int": "func(a, b int) error, int",
                 "ctx context.Context, names ...string": "context.Context, ...string",
                 "m map[string]int,": "map[string]int"}
        for params, want in cases.items():
            with self.subTest(params=params):
                self.assertEqual(api.types_of(params), want)


class ChangesTest(unittest.TestCase):
    def test_what_changes_no_api_is_not_listed(self):
        cases = {
            "parameter renamed": edit(("func New(a, b int, c string)",
                                       "func New(x, y int, z string)")),
            "receiver renamed": edit(("func (t *T) Method", "func (r *T) Method")),
            "results renamed": edit((") (res int, err error)", ") (n int, e error)")),
            "fields realigned": edit(("\tName       string", "\tName string")),
            "comment changed": edit(("KindA Kind = iota // first", "KindA Kind = iota // one")),
            "field added": edit(("\tFn     func", "\tAdded bool\n\tFn     func")),
            "initializer changed": edit(("ErrA       = io.EOF", "ErrA = io.ErrClosedPipe")),
            "signature reflowed": edit(("func Long(\n\tfirst int,\n\tsecond string,\n) (res",
                                        "func Long(first int, second string) (res")),
        }
        for name, head in cases.items():
            with self.subTest(name):
                self.assertEqual(api.changes("p", DOC, head), [])

    def test_each_removal_or_change_is_listed(self):
        cases = {
            "type changed": (edit(("func New(a, b int, c string)", "func New(a int, c string)")),
                             ["p: func New(int, int, string) *T"]),
            "field removed": (edit(("\tLongerName int // trailing\n", "")),
                              ["p: type T struct: LongerName int"]),
            "method removed": (edit(("\t// M does m.\n\tM(ctx int, names ...string) "
                                     "(n int, err error)\n", "")),
                               ["p: type I interface: M(int, ...string) (int, error)"]),
            "constants reordered": (edit(("KindA Kind = iota // first\n\tKindB",
                                          "KindB Kind = iota\n\tKindA")),
                                    ["p: const KindA Kind = iota [0]", "p: const KindB [1]"]),
            "variable removed": (edit(("\tErrB error = nil\n", "")), ["p: var ErrB error"]),
            "tag changed": (edit(("LongerName int // trailing", 'LongerName int `json:"l"`')),
                            ["p: type T struct: LongerName int"]),
            "value changed": (edit(('"http://x // y"', '"http://z"')),
                              ['p: const URL = "http://x // y"']),
        }
        for name, (head, want) in cases.items():
            with self.subTest(name):
                self.assertEqual(api.changes("p", DOC, head), want)

    def test_only_a_block_using_iota_is_compared_by_position(self):
        def doc(block: str) -> str:
            return f"package p\n\nCONSTANTS\n\nconst (\n{block})\n"
        iota, swapped = "\tA K = iota\n\tB\n\tC\n", "\tA K = iota\n\tC\n\tB\n"
        self.assertEqual(api.changes("p", doc(iota), doc(swapped)),
                         ["p: const B [1]", "p: const C [2]"])
        values, moved = "\tA = 1\n\tB = 2\n\tC = 3\n", "\tA = 1\n\tC = 3\n\tB = 2\n"
        self.assertEqual(api.changes("p", doc(values), doc(moved)), [])

    def test_a_removed_type_is_listed_once_not_with_each_member(self):
        start = DOC.index("type T struct {")
        head = DOC[:start] + DOC[DOC.index("func New("):]
        self.assertEqual(api.changes("p", DOC, head), ["p: type T struct"])

    def test_a_method_added_to_an_existing_interface_is_listed(self):
        head = edit(("\tio.Reader\n", "\tio.Reader\n\tClose() error\n"))
        want = ["p: type I interface: Close() error  (added: every implementation outside the "
                "repository breaks)"]
        self.assertEqual(api.changes("p", DOC, head), want)
        added = "type Alias = T\n\ntype J interface {\n\tClose() error\n}\n"
        new = edit(("type Alias = T\n", added))
        self.assertEqual(api.changes("p", DOC, new), [], "a new interface breaks nothing")

    def test_a_package_on_one_side_alone(self):
        gone = api.changes("p", DOC, None)
        self.assertIn("p: func New(int, int, string) *T", gone)
        self.assertNotIn("p: type T struct: Name string", gone, "its type stands for it")
        self.assertEqual(api.changes("p", None, DOC), [])


class TreeTest(unittest.TestCase):
    def test_only_public_packages_the_branch_changes_are_read(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            env = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
                   "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"}
            git = ["git", "-C", d]
            files = {"go.mod": "module example.com/m\n\ngo 1.25\n",
                     "pub/a.go": "package pub\n\nfunc F(a int) {}\n",
                     "pub/internal/x/x.go": "package x\n\nfunc G(a int) {}\n",
                     "cmd/tool/main.go": "package main\n\nfunc H(a int) {}\n\nfunc main() {}\n",
                     "gone/g.go": "package gone\n\nfunc Gone() {}\n",
                     "left/l.go": "package left\n\nfunc Kept() {}\n",
                     "left/moved.go": "package left\n\nfunc Moved() {}\n"}
            for rel, text in files.items():
                (root / rel).parent.mkdir(parents=True, exist_ok=True)
                (root / rel).write_text(text)
            subprocess.run(git + ["init", "-q"], check=True, env=env)
            subprocess.run(git + ["add", "-A"], check=True, env=env)
            subprocess.run(git + ["commit", "-qm", "base"], check=True, env=env)
            for rel in ("pub/a.go", "pub/internal/x/x.go", "cmd/tool/main.go"):
                text = (root / rel).read_text().replace("(a int)", "(a string)")
                (root / rel).write_text(text)
            (root / "gone" / "g.go").unlink()
            # A file moved to another package takes its API out of the one it left.
            subprocess.run(git + ["mv", "left/moved.go", "pub/moved.go"], check=True, env=env)
            moved = (root / "pub" / "moved.go").read_text().replace("package left", "package pub")
            (root / "pub" / "moved.go").write_text(moved)
            got = api.listed(root, "HEAD")
            worktrees = subprocess.run(git + ["worktree", "list"], check=True, env=env,
                                       capture_output=True, text=True).stdout.splitlines()
        self.assertEqual(got, ["gone: func Gone()", "left: func Moved()", "pub: func F(int)"])
        self.assertEqual(len(worktrees), 1, "the merge base's worktree is removed")


class CrashTest(unittest.TestCase):
    def test_a_missing_tool_exits_2_never_a_verdict(self):
        env = {"PATH": "", "PYTHONDONTWRITEBYTECODE": "1"}
        run = subprocess.run([sys.executable, str(HERE / "exported_api.py")], capture_output=True,
                             text=True, env=env)
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertEqual(run.stdout, "")


class UsageTest(unittest.TestCase):
    def test_help_prints_the_usage_and_any_other_option_is_refused(self):
        cases = [(["--help"], 0), (["-h"], 0), (["--octopus"], 2), (["origin/main", "extra"], 2)]
        for args, want in cases:
            with self.subTest(args=args):
                out, err = io.StringIO(), io.StringIO()
                with redirect_stdout(out), redirect_stderr(err):
                    self.assertEqual(api.main(args), want)
                self.assertIn("Usage:", (out if want == 0 else err).getvalue())


if __name__ == "__main__":
    unittest.main()
