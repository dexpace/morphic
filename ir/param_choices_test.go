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

// paramChoicesOperation is an operation declaring five query parameters and
// stating two choices among them: an optional choice between two single
// parameters and a required one whose second variant holds two. A parameter
// of one choice appears in no other, and the declaration order of the
// parameters differs from the order the choices list them in.
func paramChoicesOperation() ir.Operation {
	const prefix = "param/openapi/paths/~1resources/get/parameters/"
	id := func(name string) ir.ParamID { return ir.ParamID(prefix + name + "/query") }
	named := func(source string) ir.Naming { return ir.Naming{Source: source, Canonical: source} }
	params := make([]ir.Parameter, 0, 5)
	for _, name := range []string{"parent_id", "parent_slug", "parent_external_id", "before", "after"} {
		params = append(params, ir.Parameter{ID: id(name), Name: named(name)})
	}
	return ir.Operation{
		ID:     "op/openapi/paths/~1resources/get",
		Name:   named("listResources"),
		Params: params,
		ParamChoices: []ir.ParamChoice{
			{
				Name: named("cursor"),
				Variants: []ir.ParamVariant{
					{Name: named("backward"), Params: []ir.ParamID{id("before")}},
					{Name: named("forward"), Params: []ir.ParamID{id("after")}},
				},
			},
			{
				Name:     named("parent"),
				Required: true,
				Variants: []ir.ParamVariant{
					{Name: named("by_external_id"), Params: []ir.ParamID{id("parent_slug"), id("parent_external_id")}},
					{Name: named("by_id"), Params: []ir.ParamID{id("parent_id")}},
				},
			},
		},
	}
}

// TestOperation_ParamChoicesEncodeUnderTheirOwnKeys pins the encoded form of a
// choice: the operation's paramChoices member, a choice's name, required and
// variants, a variant's name and params, with the ID of each parameter a
// string, in the order the slices hold them. A false required is written, as
// the weaker claim it is.
func TestOperation_ParamChoicesEncodeUnderTheirOwnKeys(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(paramChoicesOperation().ParamChoices)
	require.NoError(t, err)

	const prefix = "param/openapi/paths/~1resources/get/parameters/"
	want := `[` +
		`{"name":{"source":"cursor","canonical":"cursor"},"required":false,"variants":[` +
		`{"name":{"source":"backward","canonical":"backward"},"params":["` + prefix + `before/query"]},` +
		`{"name":{"source":"forward","canonical":"forward"},"params":["` + prefix + `after/query"]}]},` +
		`{"name":{"source":"parent","canonical":"parent"},"required":true,"variants":[` +
		`{"name":{"source":"by_external_id","canonical":"by_external_id"},"params":["` +
		prefix + `parent_slug/query","` + prefix + `parent_external_id/query"]},` +
		`{"name":{"source":"by_id","canonical":"by_id"},"params":["` + prefix + `parent_id/query"]}]}]`
	assert.JSONEq(t, want, string(raw))
}

// TestOperation_ParamChoicesAreOmittedWhenNoneAreStated pins that an operation
// stating no choice writes no paramChoices member, so a document that states
// none reads as it did before the field existed.
func TestOperation_ParamChoicesAreOmittedWhenNoneAreStated(t *testing.T) {
	t.Parallel()
	op := paramChoicesOperation()
	op.ParamChoices = nil
	raw, err := json.Marshal(op)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "paramChoices")

	op.ParamChoices = []ir.ParamChoice{}
	raw, err = json.Marshal(op)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "paramChoices", "an empty list states no more than a nil one")
}

// TestDocument_ParamChoicesRoundTripThroughTheDocumentCodec encodes a document
// holding an operation with choices through the document's own codec and
// decodes it, requiring the same document back: each choice's name, its
// required flag, its variants and their parameters in order.
func TestDocument_ParamChoicesRoundTripThroughTheDocumentCodec(t *testing.T) {
	t.Parallel()
	want := ir.Document{
		IRVersion: ir.IRVersion,
		Name:      "choices",
		Version:   "1",
		Services: []ir.Service{{
			ID:     "s/openapi/s",
			Name:   ir.Naming{Source: "s", Canonical: "s"},
			Groups: []ir.OperationGroup{{ID: "g/openapi/g", Operations: []ir.Operation{paramChoicesOperation()}}},
		}},
	}
	raw, err := json.Marshal(&want, json.Deterministic(true))
	require.NoError(t, err)

	var got ir.Document
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Empty(t, cmp.Diff(want, got), "the document codec must reproduce the choices")

	choices := got.Services[0].Groups[0].Operations[0].ParamChoices
	require.Len(t, choices, 2)
	assert.Equal(t, "cursor", choices[0].Name.Source)
	assert.False(t, choices[0].Required)
	assert.Equal(t, "parent", choices[1].Name.Source)
	assert.True(t, choices[1].Required)
	require.Len(t, choices[1].Variants, 2)
	assert.Equal(t, "by_external_id", choices[1].Variants[0].Name.Source)
	assert.Len(t, choices[1].Variants[0].Params, 2, "a variant's parameters survive in order")
}
