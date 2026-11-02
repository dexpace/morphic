// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/cache"
)

func TestBuildID_NamesTheExecutable(t *testing.T) {
	t.Parallel()
	id, err := cache.BuildID(t.Context())
	require.NoError(t, err)
	assert.Len(t, id, hex.EncodedLen(sha256.Size))

	dir := t.TempDir()
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	require.NoError(t, os.WriteFile(first, []byte("one build"), 0o600))
	require.NoError(t, os.WriteFile(second, []byte("another build"), 0o600))
	at := func(path string) func() (string, error) { return func() (string, error) { return path, nil } }

	a, err := cache.BuildIDOf(t.Context(), at(first))
	require.NoError(t, err)
	again, err := cache.BuildIDOf(t.Context(), at(first))
	require.NoError(t, err)
	b, err := cache.BuildIDOf(t.Context(), at(second))
	require.NoError(t, err)
	assert.Equal(t, a, again)
	assert.NotEqual(t, a, b, "two executables are two builds")
}

func TestBuildID_Failures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	_, err := cache.BuildIDOf(t.Context(), func() (string, error) { return "", boom })
	require.ErrorIs(t, err, boom)

	missing := filepath.Join(t.TempDir(), "gone")
	_, err = cache.BuildIDOf(t.Context(), func() (string, error) { return missing, nil })
	require.ErrorIs(t, err, fs.ErrNotExist)
}
