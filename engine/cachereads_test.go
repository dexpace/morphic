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
