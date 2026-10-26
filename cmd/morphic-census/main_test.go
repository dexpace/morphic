package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir/irtest"
)

// unwitnessedGolden holds the facts no spec under the repository's testdata
// sets: the lines of a census of it counted 0 0.
const unwitnessedGolden = "../../testdata/unwitnessed.golden.txt"

// lines returns the lines of a report.
func lines(report string) []string {
	return strings.Split(strings.TrimSuffix(report, "\n"), "\n")
}

func TestRun_ReportsTheFactsOfADirectoryOfSpecs(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"testdata/specs"}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())
	got := lines(stdout.String())
	// Three specs: the golden snapshot and the text file beside them are not
	// specs, and a snapshot read as one would be a run with no document.
	assert.Equal(t, []string{"documents 3", "no-document 0", "truncated 0"}, got[:3])
	assert.Contains(t, got, "member 1 1 Model.properties", "one spec declares a model")
	assert.Contains(t, got, `value 1 1 TypeKind "model"`)
	assert.Contains(t, got, `value 1 1 TypeKind "enum"`, "the JSON spec declares an enum")
	assert.Contains(t, got, `value 0 0 TypeKind "tuple"`, "no spec produces one, and the report says so")
	assert.Contains(t, got, "member 3 3 Service.name", "the nested spec is found too")
}

func TestRun_CountsASpecThatLowersToNoDocument(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"testdata/nodoc"}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	got := lines(stdout.String())
	assert.Equal(t, []string{"documents 0", "no-document 1", "truncated 0"}, got[:3])
	assert.Contains(t, got, "member 0 0 Document.name", "the report still lists every fact")
}

func TestRun_TakesFilesAndDirectoriesTogether(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	args := []string{"testdata/specs/pets.yaml", "testdata/nodoc"}
	code := run(context.Background(), args, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	got := lines(stdout.String())
	assert.Equal(t, []string{"documents 1", "no-document 1", "truncated 0"}, got[:3])
}

func TestRun_IsDeterministic(t *testing.T) {
	t.Parallel()
	var first, second, stderr bytes.Buffer
	require.Equal(t, 0, run(context.Background(), []string{"testdata"}, &first, &stderr))
	require.Equal(t, 0, run(context.Background(), []string{"testdata"}, &second, &stderr))

	assert.Equal(t, first.String(), second.String())
}

func TestRun_MissingPathExitsTwo(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{filepath.Join(t.TempDir(), "nope")}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Empty(t, stdout.String(), "no report is written for a run that failed")
	assert.Contains(t, stderr.String(), "morphic-census: harness: stat")
}

func TestRun_NoArgsExitsTwoWithUsage(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), nil, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "usage:\n  morphic-census <path>...")
}

// TestRun_CanceledRunExitsTwo gives run a spec it is listed as whatever it is,
// and a context that is done by the time the engine asks for it: the failure
// count returns rather than counts as a spec that lowered to no document.
func TestRun_CanceledRunExitsTwo(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout, stderr bytes.Buffer
	code := run(ctx, []string{"testdata/specs/pets.yaml"}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "morphic-census: engine: ", "the engine's own error")
	assert.Contains(t, stderr.String(), context.Canceled.Error())
}

func TestRun_OutsideTheModuleExitsTwo(t *testing.T) {
	// Not parallel: t.Chdir moves the whole process, and the census reads the
	// IR's declarations from the working directory's module.
	spec, err := filepath.Abs("testdata/specs/pets.yaml")
	require.NoError(t, err)
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{spec}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "morphic-census: census:")
}

func TestRun_EngineFailureExitsTwo(t *testing.T) {
	// Not parallel: it replaces the package's engine constructor.
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func() (*engine.Engine, error) { return nil, errors.New("no compilers") }

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"testdata/specs"}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "morphic-census: engine: no compilers")
}

func TestMain_ExitCode(t *testing.T) {
	origArgs, origExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = origArgs, origExit })

	var got int
	osExit = func(code int) { got = code }
	os.Args = []string{"morphic-census"} // no path args → usage → exit 2

	main()

	assert.Equal(t, 2, got)
}

// TestRun_HoldsTheFactsNoCommittedSpecSets compiles every committed spec and
// holds the facts none sets to a golden, which -update rewrites. A case that
// sets a fact removes its line, and a new IR fact, or a compiler that stops
// setting one, adds a line. A fact a compiler sets only to its zero value
// stays listed, since a census cannot see the assignment, and so does one only
// a non-default option produces, since every spec compiles with the defaults.
func TestRun_HoldsTheFactsNoCommittedSpecSets(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(context.Background(), []string{"../../testdata"}, &stdout, &stderr), stderr.String())

	var unset strings.Builder
	set := 0
	for _, line := range lines(stdout.String()) {
		switch f := strings.Fields(line); {
		case len(f) < 4: // the counts of runs
		case f[1] == "0" && f[2] == "0":
			unset.WriteString(line + "\n")
		default:
			set++
		}
	}
	require.NotZero(t, unset.Len(), "the census lists the facts it found unset")
	require.NotZero(t, set, "and found some set")

	if irtest.Update() {
		require.NoError(t, os.WriteFile(unwitnessedGolden, []byte(unset.String()), 0o644))
		return
	}
	want, err := os.ReadFile(unwitnessedGolden)
	require.NoError(t, err, "read %s (run with -update to create it)", unwitnessedGolden)
	if diff := cmp.Diff(string(want), unset.String()); diff != "" {
		t.Errorf("facts no committed spec sets (-golden +got):\n%s", diff)
	}
}
