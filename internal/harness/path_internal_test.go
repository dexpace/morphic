// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

func TestIsSnapshot_Names(t *testing.T) {
	assert.True(t, isSnapshot("ir.golden.json"), "golden IR snapshots are skipped")
	assert.True(t, isSnapshot("unwitnessed.golden.txt"), "whatever their extension")
	assert.False(t, isSnapshot("api.yaml"))
	assert.False(t, isSnapshot("golden.yaml"), "the marker is a name part, not any name containing the word")
}

func TestCheckFile_ReadErrorOnDirectory(t *testing.T) {
	// os.ReadFile of a directory fails with EISDIR, which checkFile surfaces as a
	// Go error rather than a Result.
	_, err := checkFile(context.Background(), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harness: read")
}

func TestListDir_WalkErrorPropagates(t *testing.T) {
	// SpecFiles stats its path first, so a walk that fails is reached here, with
	// a directory that does not exist: WalkDir invokes the callback with a
	// non-nil error, which listDir wraps and returns.
	reg, err := newRegistry()
	require.NoError(t, err)

	files, err := listDir(context.Background(), reg, filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err)
	assert.Nil(t, files)
	assert.Contains(t, err.Error(), "harness: walk")
}

func TestCheckPath_UnreadableSpecInADirectoryErrors(t *testing.T) {
	// A broken symlink cannot be read to decide whether a compiler takes it. The
	// sweep aborts with that error rather than a silent skip, since a file it
	// passed over would be one it never checked.
	dir := t.TempDir()
	link := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.Symlink(filepath.Join(dir, "no-such-target"), link))

	results, err := CheckPath(context.Background(), dir)
	require.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "harness: read")
}

func TestSpecFiles_AnEmptyCompilerSetIsAnError(t *testing.T) {
	replaceRegistered(t, func() []compilers.Compiler { return nil })

	files, err := SpecFiles(context.Background(), t.TempDir())
	require.Error(t, err)
	assert.Nil(t, files)
	assert.Contains(t, err.Error(), "no compilers registered")
}

// TestSpecFiles_ACompilerTheRegistryRefusesIsAnError holds listing to the
// registry it asks: a set the registry refuses, here one carrying a nil
// compiler, is the harness's fault, named by position, and lists nothing.
func TestSpecFiles_ACompilerTheRegistryRefusesIsAnError(t *testing.T) {
	withCompilers(t, nil)

	files, err := SpecFiles(context.Background(), t.TempDir())
	require.Error(t, err)
	assert.Nil(t, files)
	assert.Contains(t, err.Error(), "harness: register compiler 1")
}

// namelessClaim recognizes every source and names no dialect for it, which the
// registry refuses as a breach of the compiler contract.
type namelessClaim struct{ stubCompiler }

func (namelessClaim) Detect(context.Context, compilers.Source, compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	return compilers.Recognition{}, nil, true
}

// TestSpecFiles_ABreachDetectionFindsIsAnError holds listing to the contract as
// compiling is held to it: a compiler whose claim names nothing is its own fault,
// reported rather than read as a file that is not a spec.
func TestSpecFiles_ABreachDetectionFindsIsAnError(t *testing.T) {
	withCompilers(t, namelessClaim{})
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "api.txt"), []byte("words"), 0o600))

	files, err := SpecFiles(context.Background(), dir)
	require.ErrorIs(t, err, compilers.ErrContract)
	assert.Nil(t, files)
	assert.Contains(t, err.Error(), "harness: detect")
}

func TestSpecFiles_AFileThatCannotBeReadIsAnError(t *testing.T) {
	orig := readSpecFile
	t.Cleanup(func() { readSpecFile = orig })
	readSpecFile = func(string) ([]byte, error) { return nil, errors.New("read boom") }
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "api.yaml"), []byte(testspec.Minimal), 0o600))

	files, err := SpecFiles(context.Background(), dir)
	require.Error(t, err)
	assert.Nil(t, files)
	assert.Contains(t, err.Error(), "harness: read")
	assert.Contains(t, err.Error(), "read boom")
}
