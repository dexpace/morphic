// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/cache"
	"github.com/dexpace/morphic/internal/exectest"
)

// hashOf is the SHA-256 of the file at path, as BuildID spells it.
func hashOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestBuildID_NamesTheExecutable(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	require.NoError(t, err)

	id, err := cache.BuildID(t.Context())

	require.NoError(t, err)
	assert.Equal(t, hashOf(t, exe), id)
}

// TestBuildID_EachSystemsWayToTheImage pins where the image is read from on
// each kind of system: on Linux the file serving the running image, which no
// executable path names; elsewhere the executable's path, and nothing when that
// cannot be found or read.
func TestBuildID_EachSystemsWayToTheImage(t *testing.T) {
	t.Parallel()
	exe := filepath.Join(t.TempDir(), "exe")
	require.NoError(t, os.WriteFile(exe, []byte("one build"), 0o600))
	at := func(path string) func() (string, error) { return func() (string, error) { return path, nil } }
	boom := errors.New("boom")
	lost := func() (string, error) { return "", boom }

	id, err := cache.BuildIDOn(t.Context(), "darwin", at(exe))
	require.NoError(t, err)
	assert.Equal(t, hashOf(t, exe), id)

	_, err = cache.BuildIDOn(t.Context(), "darwin", lost)
	require.ErrorIs(t, err, boom)
	assert.EqualError(t, err, "cache: find the executable: boom")

	_, err = cache.BuildIDOn(t.Context(), "freebsd", at(filepath.Join(t.TempDir(), "gone")))
	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.ErrorContains(t, err, "cache: read the executable: ")

	// Linux never asks for the executable's path, and reads what exists on
	// Linux alone.
	id, err = cache.BuildIDOn(t.Context(), "linux", lost)
	if runtime.GOOS == "linux" {
		require.NoError(t, err)
		assert.Len(t, id, hex.EncodedLen(sha256.Size))
	} else {
		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.ErrorContains(t, err, "/proc/self/exe")
	}
}

// copyExecutable copies this test binary to a fresh directory and returns
// where, with what is appended added to its end.
func copyExecutable(t *testing.T, name, appended string) string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(exe)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, append(data, appended...), 0o700))
	return path
}

// nameBuildOf starts this test binary, copied to app, as a child naming its
// build, calls replace once the child has started and before it reads its
// executable, and returns the line the child writes.
func nameBuildOf(t *testing.T, app string, replace func()) string {
	t.Helper()
	cmd := exectest.Command(t.Context(), t, []string{asBuildNamer + "=1"}, app)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	lines := bufio.NewScanner(stdout)
	require.True(t, lines.Scan())
	require.Equal(t, "started", lines.Text())

	replace()
	_, err = stdin.Write([]byte("go\n"))
	require.NoError(t, err)
	require.True(t, lines.Scan(), "the child names its build or says why not: %v", lines.Err())
	named := lines.Text()
	require.NoError(t, cmd.Wait())
	return named
}

// TestBuildID_ABuildInstalledAsARunStartsIsNotNamed is the race a build
// installed over the executable's path opens: a process started from one build
// and naming its build once the other is at the path. Linux serves the image
// running, so the process names its own build, and not the one whose key would
// then hold what it compiles. BuildID reads the image on Linux alone (#1318).
func TestBuildID_ABuildInstalledAsARunStartsIsNotNamed(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("BuildID reads the image a process runs on Linux alone (#1318)")
	}
	app := copyExecutable(t, "app", "")
	running := hashOf(t, app)
	installed := copyExecutable(t, "installed", "another build")

	named := nameBuildOf(t, app, func() { require.NoError(t, os.Rename(installed, app)) })

	assert.Equal(t, running, named)
	assert.NotEqual(t, hashOf(t, app), named, "the installed build is named")
}

// TestBuildID_AnExecutableLeftAloneIsNamed is the control the race needs: the
// same child, its executable left as it was, names the build it runs.
func TestBuildID_AnExecutableLeftAloneIsNamed(t *testing.T) {
	t.Parallel()
	app := copyExecutable(t, "app", "")

	assert.Equal(t, hashOf(t, app), nameBuildOf(t, app, func() {}))
}
