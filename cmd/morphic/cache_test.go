// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

// compileCounter is the shipped compiler, counting its compiles.
type compileCounter struct {
	compilers.Compiler
	compiles atomic.Int32
}

func (c *compileCounter) Compile(ctx context.Context, sources []compilers.Source,
	opts compilers.Options,
) (*ir.Document, []ir.Diagnostic, error) {
	c.compiles.Add(1)
	return c.Compiler.Compile(ctx, sources, opts)
}

// countCompiles makes the commands compile through the returned counter for the
// rest of the test, on top of whatever options the flags asked for.
func countCompiles(t *testing.T) *compileCounter {
	t.Helper()
	counter := &compileCounter{Compiler: openapi.New()}
	useEngine(t, engine.WithCompilers(counter))
	return counter
}

// runCommand runs args and returns the exit code and both streams.
func runCommand(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(t.Context(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// specFile writes a spec the shipped compiler lowers cleanly.
func specFile(t *testing.T) string {
	t.Helper()
	spec := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(testspec.Tiny), 0o600))
	return spec
}

// TestRun_CacheDirASecondRunWritesWhatTheFirstDid runs each output a command has
// twice, the counter telling a hit from a compile, and requires the same bytes.
func TestRun_CacheDirASecondRunWritesWhatTheFirstDid(t *testing.T) {
	for name, args := range map[string][]string{
		"compile":  {"compile"},
		"explain":  {"compile", "--explain", ""},
		"validate": {"validate"},
	} {
		t.Run(name, func(t *testing.T) {
			counter := countCompiles(t)
			args := append(args, specFile(t), "--cache-dir", t.TempDir())

			code, stdout, stderr := runCommand(t, args...)
			require.Equal(t, 0, code, stderr)
			again, stdoutAgain, stderrAgain := runCommand(t, args...)

			assert.EqualValues(t, 1, counter.compiles.Load(), "the second run compiled")
			assert.Equal(t, code, again)
			assert.Equal(t, stdout, stdoutAgain)
			assert.Equal(t, stderr, stderrAgain)
		})
	}
}

// TestRun_CacheDirEitherCommandAnswersTheOther shares one entry between the commands,
// in both orders, as their help says: they build the same pipeline.
func TestRun_CacheDirEitherCommandAnswersTheOther(t *testing.T) {
	for name, order := range map[string][2]string{
		"validate after compile": {"compile", "validate"},
		"compile after validate": {"validate", "compile"},
	} {
		t.Run(name, func(t *testing.T) {
			counter := countCompiles(t)
			spec, dir := specFile(t), t.TempDir()
			for _, command := range order {
				code, _, stderr := runCommand(t, command, spec, "--cache-dir", dir)
				require.Equal(t, 0, code, stderr)
			}
			assert.EqualValues(t, 1, counter.compiles.Load())
		})
	}
}

// TestRun_CacheDirOutputFileIsTheSameEitherWay writes the compact form, the one
// that passes through the document's own encoder after a hit decodes it.
func TestRun_CacheDirOutputFileIsTheSameEitherWay(t *testing.T) {
	counter := countCompiles(t)
	spec, dir := specFile(t), t.TempDir()
	plain := filepath.Join(t.TempDir(), "plain.json")
	cached := filepath.Join(t.TempDir(), "cached.json")

	code, _, stderr := runCommand(t, "compile", spec, "-o", plain)
	require.Equal(t, 0, code, stderr)
	code, _, stderr = runCommand(t, "compile", spec, "--cache-dir", dir)
	require.Equal(t, 0, code, stderr)
	code, _, stderr = runCommand(t, "compile", spec, "--cache-dir", dir, "-o", cached)
	require.Equal(t, 0, code, stderr)

	assert.EqualValues(t, 2, counter.compiles.Load(), "the plain run and the first cached one")
	want, err := os.ReadFile(plain)
	require.NoError(t, err)
	got, err := os.ReadFile(cached)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

// TestRun_CacheDirNoFlagNoCache leaves the directory the flag would have named
// empty, and compiles each run.
func TestRun_CacheDirNoFlagNoCache(t *testing.T) {
	counter := countCompiles(t)
	spec := specFile(t)
	for range 2 {
		code, _, stderr := runCommand(t, "compile", spec)
		require.Equal(t, 0, code, stderr)
	}
	assert.EqualValues(t, 2, counter.compiles.Load())
}

// TestRun_CacheDirVerifyRunsItsStages holds --verify, which exists to run them, to
// compiling though an entry would answer.
func TestRun_CacheDirVerifyRunsItsStages(t *testing.T) {
	counter := countCompiles(t)
	spec, dir := specFile(t), t.TempDir()
	for range 2 {
		code, _, stderr := runCommand(t, "compile", spec, "--cache-dir", dir)
		require.Equal(t, 0, code, stderr)
	}
	code, _, stderr := runCommand(t, "compile", spec, "--cache-dir", dir, "--verify")
	require.Equal(t, 0, code, stderr)
	assert.EqualValues(t, 2, counter.compiles.Load())
}

// TestRun_CacheDirAnUnusableDirectoryWarns names a path that cannot be a directory,
// which fails the lookup and the store both. The run keeps its output and its
// exit status, and says why on one line of stderr, which is all that may differ.
func TestRun_CacheDirAnUnusableDirectoryWarns(t *testing.T) {
	t.Parallel()
	blocked := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocked, nil, 0o600))
	spec := specFile(t)

	code, stdout, stderr := runCommand(t, "compile", spec)
	require.Equal(t, 0, code, stderr)
	codeBlocked, stdoutBlocked, stderrBlocked := runCommand(t, "compile", spec, "--cache-dir", blocked)

	assert.Equal(t, code, codeBlocked)
	assert.Equal(t, stdout, stdoutBlocked)
	assert.Regexp(t, `^morphic: warning: engine: cache: load: [^\n]+; engine: cache: store: [^\n]+\n$`, stderrBlocked)
}

// failBuildID makes naming this executable fail with err for the rest of the
// test.
func failBuildID(t *testing.T, err error) {
	t.Helper()
	orig := buildID
	t.Cleanup(func() { buildID = orig })
	buildID = func(context.Context) (string, error) { return "", err }
}

// TestRun_CacheDirAnUnreadableBuildWarns names a build no one can name, as an
// executable of mode 0111 is. The run is a plain one and says why on one line.
func TestRun_CacheDirAnUnreadableBuildWarns(t *testing.T) {
	spec, dir := specFile(t), t.TempDir()
	code, stdout, stderr := runCommand(t, "compile", spec)
	require.Equal(t, 0, code, stderr)

	failBuildID(t, os.ErrPermission)
	codeFailed, stdoutFailed, stderrFailed := runCommand(t, "compile", spec, "--cache-dir", dir)

	assert.Equal(t, code, codeFailed)
	assert.Equal(t, stdout, stdoutFailed)
	assert.Equal(t, "morphic: warning: "+os.ErrPermission.Error()+"\n", stderrFailed)
	listed, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, listed, "no key was sound, so nothing was kept")
}

// TestCacheOptions_AStopWhileNamingTheBuildEndsTheRun stops the run there, and
// no warning blames the cache for it.
func TestCacheOptions_AStopWhileNamingTheBuildEndsTheRun(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stderr bytes.Buffer
	cached, err := cacheOptions(ctx, t.TempDir(), &stderr)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, cached)
	assert.Empty(t, stderr.String())
}

// TestRunPipeline_AStopWhileNamingTheBuildEndsTheRun reports it as any stop
// is: exit 2.
func TestRunPipeline_AStopWhileNamingTheBuildEndsTheRun(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	res, _, err := runPipeline(ctx, specFile(t), specOptions{cacheDir: t.TempDir()}, io.Discard)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, res)
	assert.Equal(t, exitFailure, exitCode(err))
}
