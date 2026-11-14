// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"errors"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestRunRecovered_QuotesAPanicOnOneBoundedLine pins that every walk and build
// load runs behind runRecovered reports a panic as diag.Catch quotes it,
// whatever it panicked with, after the name of what was running.
func TestRunRecovered_QuotesAPanicOnOneBoundedLine(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("the walk faulted here\n", 1<<10)

	err := runRecovered("validation", func() error { panic(huge) })

	require.Error(t, err)
	assert.Equal(t, "validation panicked: "+diag.OneLine(errors.New(huge)), err.Error())
	assert.NotContains(t, err.Error(), "\n")
	assert.Less(t, len(err.Error()), len(huge)/4, "the bound cut the value")
}

// TestRunRecovered_ReturnsWhatTheWorkReturned pins the barrier as a pass-through for
// work that returns.
func TestRunRecovered_ReturnsWhatTheWorkReturned(t *testing.T) {
	t.Parallel()
	refused := errors.New("refused")

	require.NoError(t, runRecovered("walk", func() error { return nil }))
	assert.Same(t, refused, runRecovered("walk", func() error { return refused }))
}

// TestEachSighting_StopsAtTheModelAVisitRefuses pins that an error from visit
// stops the walk at the model being visited, and is returned as it is.
func TestEachSighting_StopsAtTheModelAVisitRefuses(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a: {get: {responses: {'200': {description: ok}}}}\n")
	refused := errors.New("refused")
	site, err := eachSighting(walk(t.Context(), doc), "walk", func(jsontext.Pointer, soa.Locations, *soa.Operation) error {
		return refused
	})
	assert.Same(t, refused, err)
	assert.Equal(t, jsontext.Pointer("/paths/~1a/get"), site)
}

// TestEachSighting_APanicBetweenModelsNamesNone pins that a panic in the walk
// after a visit returned is blamed on no model: the site of the model visited
// before it is cleared once its visit returns, so the report is not placed
// where nothing faulted.
func TestEachSighting_APanicBetweenModelsNamesNone(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a: {get: {responses: {'200': {description: ok}}}}\n")
	visited := 0
	items := func(yield func(soa.WalkItem) bool) {
		for item := range walk(t.Context(), doc) {
			if !yield(item) {
				return
			}
			if visited > 0 {
				panic("the walk faulted between models")
			}
		}
	}
	site, err := eachSighting(items, "walk", func(jsontext.Pointer, soa.Locations, *soa.Operation) error {
		visited++
		return nil
	})
	require.Equal(t, 1, visited, "the operation is visited before the walk faults")
	require.ErrorContains(t, err, "walk panicked")
	assert.Empty(t, site, "no model is blamed for a fault between models")
}

// TestDecodeObject_BuildsNothingWhereTheDocumentHoldsNothing pins the build of
// a landing at a position its document holds no node at.
func TestDecodeObject_BuildsNothingWhereTheDocumentHoldsNothing(t *testing.T) {
	t.Parallel()
	_, _, err := decodeObject(t.Context(), bind.RefParameter, nil)
	assert.ErrorIs(t, err, errNothingThere)
}
