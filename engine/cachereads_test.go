// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/engine/reader"
)

func TestRecorder_ListsEveryReadInOneOrder(t *testing.T) {
	t.Parallel()
	docs, err := reader.NewMemory([]compilers.Source{
		{Path: "b.yaml", Data: []byte("b")}, {Path: "a.yaml", Data: []byte("a")},
	})
	require.NoError(t, err)
	rec := engine.NewRecorder(docs)
	for _, req := range []compilers.DocumentRequest{
		{Location: "b.yaml"}, {Location: "a.yaml"}, {Location: "a.yaml", Limit: 9}, {Location: "a.yaml"},
	} {
		_, err := rec.ReadDocument(t.Context(), req)
		require.NoError(t, err)
	}
	locations, sound := engine.SettledLocations(rec)
	assert.True(t, sound)
	assert.Equal(t, []string{"a.yaml", "a.yaml", "b.yaml"}, locations, "one entry per request, in sorted order")
}

// fickleReader answers each read from the next of its answers.
type fickleReader struct {
	answers []string
	failing bool
}

func (r *fickleReader) ReadDocument(context.Context, compilers.DocumentRequest) ([]byte, error) {
	if r.failing {
		return nil, errors.New("unreadable")
	}
	answer := r.answers[0]
	r.answers = r.answers[1:]
	return []byte(answer), nil
}

func TestRecorder_ARunItCannotVouchForIsUnsound(t *testing.T) {
	t.Parallel()
	req := compilers.DocumentRequest{Location: "a.yaml"}

	t.Run("a repeat answering with the same bytes is sound", func(t *testing.T) {
		t.Parallel()
		rec := engine.NewRecorder(&fickleReader{answers: []string{"one", "one"}})
		_, _ = rec.ReadDocument(t.Context(), req)
		_, _ = rec.ReadDocument(t.Context(), req)
		_, sound := engine.SettledLocations(rec)
		assert.True(t, sound)
	})
	t.Run("a repeat answering with other bytes is not", func(t *testing.T) {
		t.Parallel()
		rec := engine.NewRecorder(&fickleReader{answers: []string{"one", "two"}})
		_, _ = rec.ReadDocument(t.Context(), req)
		_, _ = rec.ReadDocument(t.Context(), req)
		_, sound := engine.SettledLocations(rec)
		assert.False(t, sound)
	})
	t.Run("a third read does not restore it", func(t *testing.T) {
		t.Parallel()
		rec := engine.NewRecorder(&fickleReader{answers: []string{"one", "two", "two"}})
		for range 3 {
			_, _ = rec.ReadDocument(t.Context(), req)
		}
		_, sound := engine.SettledLocations(rec)
		assert.False(t, sound)
	})
	t.Run("a failed read is not, and the failure reaches the compile", func(t *testing.T) {
		t.Parallel()
		rec := engine.NewRecorder(&fickleReader{failing: true})
		_, err := rec.ReadDocument(t.Context(), req)
		require.Error(t, err)
		_, sound := engine.SettledLocations(rec)
		assert.False(t, sound)
	})
}

// placingReader places each directory where its places says, as a reader
// following links would, and reads nothing.
type placingReader struct {
	fickleReader
	places map[string]string
}

func (r *placingReader) LocateDirectory(dir string) string { return r.places[dir] }

// TestRecorder_PlacesADirectoryAsTheReaderBeneath pins a recorder's
// compilers.DirectoryLocator: it answers as the reader beneath does, or with
// the directory as spelled for one placing none, and notes each answer, which
// an entry then holds a later run to.
func TestRecorder_PlacesADirectoryAsTheReaderBeneath(t *testing.T) {
	t.Parallel()
	docs := &placingReader{places: map[string]string{"/b": "/real/b", "/a": "/real/a"}}
	rec := engine.NewRecorder(docs)
	assert.Equal(t, "/real/b", rec.LocateDirectory("/b"))
	assert.Equal(t, "/real/a", rec.LocateDirectory("/a"))
	assert.Equal(t, [][2]string{{"/a", "/real/a"}, {"/b", "/real/b"}}, engine.SettledDirectories(rec),
		"each answer noted, in one order")

	assert.Equal(t, "/a", engine.NewRecorder(&fickleReader{}).LocateDirectory("/a"),
		"a reader placing none leaves the directory as spelled")
}
