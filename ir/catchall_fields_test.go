// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestContent_CatchAllEncodingEncodesByTypeID holds the field's JSON form: a
// map keyed by the holding type's ID, each entry a value and patterns by their
// text, absent where the source states none.
func TestContent_CatchAllEncodingEncodesByTypeID(t *testing.T) {
	t.Parallel()
	content := ir.Content{
		MediaType: "multipart/form-data",
		CatchAllEncoding: map[ir.TypeID]ir.CatchAllParts{
			"type/m": {
				Value:    &ir.PartEncoding{ContentTypes: []string{"text/plain"}},
				Patterns: map[string]ir.PartEncoding{"^f_": {HasFilename: true}},
			},
		},
	}
	got, err := json.Marshal(content, json.Deterministic(true))
	require.NoError(t, err)
	assert.JSONEq(t, `{"mediaType":"multipart/form-data","type":{"target":"","nullable":false},`+
		`"catchAllEncoding":{"type/m":{"value":{"contentTypes":["text/plain"],"repeatsPerArrayItem":false,"hasFilename":false,"allowReserved":false},`+
		`"patterns":{"^f_":{"repeatsPerArrayItem":false,"hasFilename":true,"allowReserved":false}}}}}`, string(got))

	var back ir.Content
	require.NoError(t, json.Unmarshal(got, &back))
	assert.Empty(t, cmp.Diff(content, back))

	none, err := json.Marshal(ir.Content{MediaType: "application/json"}, json.Deterministic(true))
	require.NoError(t, err)
	assert.NotContains(t, string(none), "catchAllEncoding", "no key means the source states none")
}
