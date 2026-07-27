package ir_test

import (
	"testing"

	"github.com/dexpace/morphic/ir"
)

// TestDocs_JSONContract pins Docs' omitempty contract — an entity with no
// documentation at all must marshal to an empty object, not to a payload full
// of empty-string noise — and that a fully populated Docs — summary,
// CommonMark description with a cross-reference token, and multiple external
// links — survives a JSON round trip.
func TestDocs_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Docs{}, `{}`, populatedDocs())
}

// TestLink_JSONContract pins Link's omitempty contract (both fields are
// optional) and that a populated Link round-trips.
func TestLink_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Link{}, `{}`, ir.Link{URL: "https://example.com", Description: "the example"})
}

// TestDeprecation_JSONContract pins Deprecation's omitempty contract — all
// three fields are optional, so an entity deprecated with no detail at all
// still marshals to an empty object rather than three empty strings — and
// that a fully populated Deprecation round-trips.
func TestDeprecation_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Deprecation{}, `{}`, *populatedDeprecation())
}

// TestErrorExample_JSONContract pins ErrorExample's contract that both Type
// and Content carry no omitempty (ir-design §12: an operation-scenario
// example's error arm always has a type and a content value, never an
// implicit one), while nested TypeRef/Value zero shapes are exercised in
// their own test files, and that a fully populated ErrorExample round-trips.
func TestErrorExample_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.ErrorExample{},
		`{"type":{"target":"","nullable":false},"content":{"kind":"","bytes":null,"list":null,"object":null}}`,
		ir.ErrorExample{Type: populatedTypeRef(), Content: populatedValue()})
}

// TestExample_JSONContract pins that every Example field is optional, since a
// legal Example populates only one contextual arm — Value/Headers or
// Input/Output/Error (ir-design §12). The fixture below sets both arms at
// once purely to exercise every field in one fixture; arm legality is
// validated elsewhere, this test only pins serialization.
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
		Extensions:  populatedExtensions(),
	})
}
