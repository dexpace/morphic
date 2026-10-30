// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package atomicfile_test

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/internal/atomicfile"
)

// TestReplace_ADoneContextCreatesNothing pins a replace asked for after its
// context ended: it fails naming the destination and the context's error,
// runs no fill and creates no file, whether the destination exists or not.
func TestReplace_ADoneContextCreatesNothing(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dir := t.TempDir()
	dest, missing := filepath.Join(dir, "ir.json"), filepath.Join(dir, "missing.json")
	seed(t, dest, "PREVIOUS\n", 0o644)
	filled := false
	fill := func(io.Writer) error { filled = true; return nil }

	for _, path := range []string{dest, missing} {
		err := atomicfile.Replace(ctx, path, fill)

		require.ErrorIs(t, err, context.Canceled)
		assert.EqualError(t, err, fmt.Sprintf("replace output %q: context canceled", path))
	}
	assert.False(t, filled, "nothing is filled for a context already done")
	assert.Equal(t, "PREVIOUS\n", contentOf(t, dest))
	assert.Equal(t, []string{"ir.json"}, entriesIn(t, dir), "no temporary file and no new destination")
}

// TestReplace_AContextDoneDuringTheFillPublishesNothing pins the last point a
// cancellation stops the replace: a context that ends while fill runs, a fill
// that does not notice and succeeds included, leaves the destination as it
// was, or absent, and removes the temporary file holding the new content.
func TestReplace_AContextDoneDuringTheFillPublishesNothing(t *testing.T) {
	t.Parallel()
	for _, exists := range []bool{true, false} {
		t.Run(fmt.Sprintf("destination exists %t", exists), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dest := newDest(t)
			if exists {
				seed(t, dest, "PREVIOUS\n", 0o644)
			}
			fill := func(w io.Writer) error {
				if _, err := w.Write([]byte("new\n")); err != nil {
					return err
				}
				cancel()
				return nil
			}

			err := atomicfile.Replace(ctx, dest, fill)

			require.ErrorIs(t, err, context.Canceled)
			assert.EqualError(t, err, fmt.Sprintf("replace output %q: context canceled", dest))
			assert.Empty(t, otherEntries(t, dest), "the temporary file is removed")
			if !exists {
				assert.NoFileExists(t, dest)
				return
			}
			assert.Equal(t, "PREVIOUS\n", contentOf(t, dest))
		})
	}
}
