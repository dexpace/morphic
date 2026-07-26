package ir_test

import (
	"testing"

	"github.com/dexpace/morphic/ir"
)

// TestDocs_ZeroValueShape pins Docs' omitempty contract: an entity with no
// documentation at all must marshal to an empty object, not to a payload full
// of empty-string noise.
func TestDocs_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Docs{}, `{}`)
}

// TestDocs_PopulatedRoundTrip pins that a fully populated Docs — summary,
// CommonMark description with a cross-reference token, and multiple external
// links — survives a JSON round trip.
func TestDocs_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, populatedDocs())
}

// TestLink_ZeroValueShape pins Link's omitempty contract: both fields are
// optional.
func TestLink_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Link{}, `{}`)
}

// TestLink_PopulatedRoundTrip pins that a populated Link round-trips.
func TestLink_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.Link{URL: "https://example.com", Description: "the example"})
}

// TestDeprecation_ZeroValueShape pins Deprecation's omitempty contract: all
// three fields are optional, so an entity deprecated with no detail at all
// still marshals to an empty object rather than three empty strings.
func TestDeprecation_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Deprecation{}, `{}`)
}

// TestDeprecation_PopulatedRoundTrip pins that a fully populated Deprecation
// round-trips.
func TestDeprecation_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, *populatedDeprecation())
}

// TestErrorExample_ZeroValueShape pins ErrorExample's contract that both Type
// and Content carry no omitempty (ir-design §12: an operation-scenario
// example's error arm always has a type and a content value, never an
// implicit one), while nested TypeRef/Value zero shapes are exercised in
// their own test files.
func TestErrorExample_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.ErrorExample{},
		`{"type":{"target":"","nullable":false},"content":{"kind":"","bytes":null,"list":null,"object":null}}`)
}

// TestErrorExample_PopulatedRoundTrip pins that a fully populated ErrorExample
// round-trips.
func TestErrorExample_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.ErrorExample{
		Type:    populatedTypeRef(),
		Content: populatedValue(),
	}
	assertRoundTrip(t, want)
}

// TestExample_ZeroValueShape pins Example's omitempty contract: every field is
// optional, since an Example is legally constructed with only one of its
// contextual arms (Value vs Input/Output/Error) populated (ir-design §12).
func TestExample_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Example{}, `{}`)
}

// TestExample_PopulatedRoundTrip pins that a fully populated Example — both
// the type-example arm (Value/Headers) and the operation-scenario arm
// (Input/Output/Error) set simultaneously here purely to exercise every field
// in one fixture — survives a JSON round trip. Field legality across the two
// arms is validated elsewhere; this test only pins serialization.
func TestExample_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	val := ir.Value{Kind: ir.ValueString, Str: "example value"}
	want := ir.Example{
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
	}
	assertRoundTrip(t, want)
}
