// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"testing"

	"github.com/dexpace/morphic/ir"
)

// TestDocs_JSONContract pins Docs' omitempty contract — an entity with no
// documentation at all must marshal to an empty object, not to a payload full
// of empty-string noise — and that a fully populated Docs — summary,
// CommonMark description, a span of it naming a type, and multiple external
// links — survives a JSON round trip.
func TestDocs_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Docs{}, `{}`, populatedDocs())
}

// TestDocRef_JSONContract pins that a DocRef keeps every field: a span opening
// the description starts at 0, which omitting the key would leave a reader to
// assume.
func TestDocRef_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.DocRef{}, `{"start":0,"end":0,"type":""}`, ir.DocRef{Start: 1, End: 2, Type: "t/x"})
}

// TestLink_JSONContract pins Link's omitempty contract (both fields are
// optional) and that a populated Link round-trips.
func TestLink_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Link{}, `{}`, ir.Link{URL: "https://example.com", Description: "the example"})
}

// TestDeprecation_JSONContract pins Deprecation's omitempty contract — all
// four fields are optional, so an entity deprecated with no detail at all
// still marshals to an empty object rather than four empty strings — and
// that a fully populated Deprecation round-trips, RemovalVersion and
// RemovalDate included, since a consumer tells one from the other by which key
// it arrived under.
func TestDeprecation_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Deprecation{}, `{}`, *populatedDeprecation())
}

// TestErrorExample_JSONContract pins ErrorExample's contract that both Type
// and Content carry no omitempty
// (docs/ir/annotations.md#docs-deprecation-examples: an operation-scenario
// example's error arm always has a type and a content value, never an implicit
// one), while nested TypeRef/Value zero shapes are exercised in their own test
// files, and that a fully populated ErrorExample round-trips.
func TestErrorExample_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.ErrorExample{},
		`{"type":{"target":"","nullable":false},"content":{"kind":""}}`,
		ir.ErrorExample{Type: populatedTypeRef(), Content: populatedValue()})
}

// TestExample_JSONContract pins that every Example field is optional, since a
// legal Example populates only one contextual arm — Value/Headers or
// Input/Output/Error (docs/ir/annotations.md#docs-deprecation-examples). The
// fixture below sets both arms at once purely to exercise every field in one
// fixture; nothing currently checks arm legality, so this test only pins
// serialization.
func TestExample_JSONContract(t *testing.T) {
	t.Parallel()
	val := ir.Value{Kind: ir.ValueString, Str: "example value"}
	assertJSONContract(t, ir.Example{}, `{}`, ir.Example{
		Name:        "example-1",
		Summary:     "an example",
		Description: "a longer description",
		Value:       &val,
		Headers:     &val,
		Input:       &val,
		Output:      &val,
		Error: &ir.ErrorExample{
			Type:    populatedTypeRef(),
			Content: val,
		},
		ExternalURL: "https://example.com/examples/1",
		Unmodeled:   populatedUnmodeled(),
	})
}
