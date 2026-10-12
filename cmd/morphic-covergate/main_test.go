package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/cover"
)

// judge writes profile to a file, runs the gate over it, and returns the exit
// code and stdout. Nothing but a usage error may reach stderr.
func judge(t *testing.T, profile string) (int, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cover.out")
	require.NoError(t, os.WriteFile(path, []byte(profile), 0o600))
	var stdout, stderr bytes.Buffer
	code := run([]string{path}, &stdout, &stderr)
	assert.Empty(t, stderr.String())
	return code, stdout.String()
}

// TestRun_JudgesAProfile drives the gate over profiles a real suite rarely or
// never writes: repeated blocks, copies that disagree, and files that are empty
// or malformed. Each is a shape the merge or a refusal exists for.
func TestRun_JudgesAProfile(t *testing.T) {
	t.Parallel()
	const head = "mode: atomic\n"
	tests := []struct {
		name    string
		profile string
		want    int
		holds   []string
		lacks   []string
	}{
		{"one covered block", head + "x/a.go:1.1,2.2 1 1\n", 0,
			[]string{"Coverage gate passed: all 1 statements covered."}, nil},
		{"a repeated block counts once", head + "x/a.go:1.1,2.2 3 1\nx/a.go:1.1,2.2 3 1\n" +
			"x/a.go:1.1,2.2 3 1\nx/b.go:5.1,6.2 2 4\n", 0,
			[]string{"Coverage gate passed: all 5 statements covered."}, nil},
		{"a copy that ran covers the block", head + "x/a.go:1.1,2.2 3 0\nx/a.go:1.1,2.2 3 2\n", 0,
			[]string{"Coverage gate passed: all 3 statements covered."}, nil},
		{"copies that never ran stay uncovered", head + "x/a.go:1.1,2.2 3 0\nx/a.go:1.1,2.2 3 0\n", 1,
			[]string{"COVERAGE FAIL: x/a.go:1.1,2.2 (3 statement(s) uncovered)",
				"Coverage gate failed: 3 of 3 statements uncovered; 100% is required."}, nil},
		{"blocks of one file are told apart", head + "x/a.go:1.1,2.2 1 1\nx/a.go:3.1,4.2 1 0\n", 1,
			[]string{"COVERAGE FAIL: x/a.go:3.1,4.2 (1 statement(s) uncovered)",
				"Coverage gate failed: 1 of 2 statements uncovered; 100% is required."},
			[]string{"x/a.go:1.1,2.2"}},
		{"an empty-bodied block is not listed", head + "x/a.go:1.1,2.2 5 0\nx/b.go:9.1,9.9 0 0\n", 1,
			[]string{"COVERAGE FAIL: x/a.go:1.1,2.2 (5 statement(s) uncovered)",
				"Coverage gate failed: 5 of 5 statements uncovered; 100% is required."},
			[]string{"x/b.go", "more uncovered"}},
		{"copies disagreeing on statements are refused", head + "x/a.go:1.1,2.2 3 1\nx/a.go:1.1,2.2 4 1\n", 1,
			[]string{"COVERAGE FAIL: read profile", "inconsistent NumStmt"}, []string{"Coverage gate"}},
		{"a missing header is refused", "x/a.go:1.1,2.2 5 0\nx/b.go:1.1,2.2 5 1\n", 1,
			[]string{"COVERAGE FAIL: read profile", "bad mode line"}, []string{"Coverage gate"}},
		{"a short line is refused", head + "x/a.go:1.1,2.2 5\n", 1,
			[]string{"COVERAGE FAIL: read profile", "doesn't match expected format"}, []string{"Coverage gate"}},
		{"a header alone records nothing", head, 1,
			[]string{"COVERAGE FAIL: the profile records no statements"}, nil},
		{"an empty file records nothing", "", 1,
			[]string{"COVERAGE FAIL: the profile records no statements"}, nil},
		{"empty-bodied blocks alone record nothing", head + "x/a.go:1.1,2.2 0 0\n", 1,
			[]string{"COVERAGE FAIL: the profile records no statements"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, out := judge(t, tc.profile)
			assert.Equal(t, tc.want, code, out)
			for _, s := range tc.holds {
				assert.Contains(t, out, s)
			}
			for _, s := range tc.lacks {
				assert.NotContains(t, out, s)
			}
		})
	}
}

// TestRun_BoundsTheListing holds the failure listing to its first maxReported
// blocks by position, then a count of the rest. Lines 1 to 30 tell a numeric
// order from a textual one, which would list line 10 before line 2.
func TestRun_BoundsTheListing(t *testing.T) {
	t.Parallel()
	var profile strings.Builder
	profile.WriteString("mode: atomic\n")
	for line := range 30 {
		fmt.Fprintf(&profile, "x/c.go:%d.1,%d.9 1 0\n", line+1, line+1)
	}

	code, out := judge(t, profile.String())

	assert.Equal(t, 1, code)
	assert.Equal(t, maxReported, strings.Count(out, "COVERAGE FAIL:"))
	assert.Contains(t, out, "COVERAGE FAIL: x/c.go:25.1,25.9 (1 statement(s) uncovered)")
	assert.NotContains(t, out, "x/c.go:26.1")
	assert.Contains(t, out, "  ... and 5 more uncovered block(s)")
	assert.Contains(t, out, "Coverage gate failed: 30 of 30 statements uncovered; 100% is required.")
}

// TestTallyOf_ListsGapsInFileThenPositionOrder gives tallyOf profiles out of
// order. ParseProfiles happens to sort what it returns, so only a direct call
// shows that the listing's order is the gate's own rather than borrowed.
func TestTallyOf_ListsGapsInFileThenPositionOrder(t *testing.T) {
	t.Parallel()
	block := func(line, col int) cover.ProfileBlock {
		return cover.ProfileBlock{StartLine: line, StartCol: col, EndLine: line, EndCol: col + 1, NumStmt: 1}
	}
	profiles := []*cover.Profile{
		{FileName: "x/b.go", Blocks: []cover.ProfileBlock{block(1, 1)}},
		{FileName: "x/a.go", Blocks: []cover.ProfileBlock{block(9, 1), block(2, 5), block(2, 1)}},
	}

	got := tallyOf(profiles)

	positions := make([]string, 0, len(got.gaps))
	for _, g := range got.gaps {
		positions = append(positions, fmt.Sprintf("%s:%d.%d", g.file, g.block.StartLine, g.block.StartCol))
	}
	assert.Equal(t, []string{"x/a.go:2.1", "x/a.go:2.5", "x/a.go:9.1", "x/b.go:1.1"}, positions)
	assert.Equal(t, 4, got.total)
	assert.Equal(t, 4, got.missed)
}

func TestRun_UnreadableProfileFails(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "absent.out")
	var stdout, stderr bytes.Buffer

	assert.Equal(t, 1, run([]string{path}, &stdout, &stderr))
	assert.Contains(t, stdout.String(), "COVERAGE FAIL: read profile "+path)
	assert.Empty(t, stderr.String())
}

func TestRun_RefusesAnythingButOneProfile(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"a.out", "b.out"}} {
		var stdout, stderr bytes.Buffer
		assert.Equal(t, 2, run(args, &stdout, &stderr), "args %q", args)
		assert.Equal(t, usage+"\n", stderr.String())
		assert.Empty(t, stdout.String())
	}
}

func TestMain_ExitCode(t *testing.T) {
	origArgs, origExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = origArgs, origExit })

	var got int
	osExit = func(code int) { got = code }
	os.Args = []string{"morphic-covergate"} // no profile → usage → exit 2

	main()

	assert.Equal(t, 2, got)
}
