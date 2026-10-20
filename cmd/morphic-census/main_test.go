package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
)

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
	assert.Equal(t, "documents 3 truncated 0", got[0])
	assert.Equal(t, "no-document 0", got[1])
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
	assert.Equal(t, "documents 0 truncated 0", got[0])
	assert.Equal(t, "no-document 1", got[1])
	assert.Contains(t, got, "member 0 0 Document.name", "the report still lists every fact")
}

func TestRun_TakesFilesAndDirectoriesTogether(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	args := []string{"testdata/specs/pets.yaml", "testdata/nodoc"}
	code := run(context.Background(), args, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	got := lines(stdout.String())
	assert.Equal(t, "documents 1 truncated 0", got[0])
	assert.Equal(t, "no-document 1", got[1])
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

// TestRun_UnreadableSpecExitsTwo gives run a spec the directory walk lists and
// the engine cannot read: a link whose target is gone. It needs no permission
// bits, which root ignores.
func TestRun_UnreadableSpecExitsTwo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "spec.yaml")))

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{dir}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "morphic-census: engine: read spec")
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
