// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FilePathOf is filePathOf, for the external tests.
var FilePathOf = filePathOf

// ReadOpened is readOpened, for the external tests.
var ReadOpened = readOpened

// ReadSourceFrom is ReadSource over a file system a test scripts.
var ReadSourceFrom = readSource

// ReadRooted is readRooted, for the external tests.
var ReadRooted = readRooted

// ResolveWithin is resolveWithin, for the external tests.
var ResolveWithin = resolveWithin

// ResolvedDir returns a fresh temporary directory with its links resolved, so
// a path under it spells the directory the working directory reports where the
// system's temporary directory is itself a link.
func ResolvedDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

// VanishWorkingDirectory leaves the working directory something the system
// cannot name: a path deeper than any buffer getcwd fills, whose parent
// refuses a read, so the walk up that os.Getwd falls back to fails. Deleting
// the directory would not do: darwin names a deleted one. It skips where
// permissions do not bind, or the system names the directory regardless.
func VanishWorkingDirectory(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not deny a read here")
	}
	segment := strings.Repeat("a", 200)
	const levels = 25 // 5000 bytes, past Linux's 4096 and darwin's 1024
	for range levels {
		require.NoError(t, os.Mkdir(segment, 0o700))
		t.Chdir(segment)
	}
	require.NoError(t, os.Chmod("..", 0o100))
	t.Cleanup(func() { assert.NoError(t, os.Chmod("..", 0o700)) })
	if _, err := os.Getwd(); err == nil {
		t.Skip("this system names the working directory whatever its parents allow")
	}
}
