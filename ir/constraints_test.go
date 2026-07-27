package ir_test

import (
	"testing"

	"github.com/dexpace/morphic/ir"
)

// TestConstraints_ZeroValueShape pins Constraints' omitempty contract: every
// bound is a pointer (nil = unconstrained) except ExclusiveMin, ExclusiveMax,
// and UniqueItems, which are plain bools that must always serialize — an
// unconstrained Constraints still asserts "not exclusive" and "not unique" as
// facts, not absences.
func TestConstraints_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Constraints{},
		`{"exclusiveMin":false,"exclusiveMax":false,"uniqueItems":false}`)
}

// TestConstraints_PopulatedRoundTrip pins that a fully populated Constraints —
// every numeric bound, string/collection/property-count bound, and both
// exclusivity flags — survives a JSON round trip with its BigVal decimal
// strings intact (invariant: no float64 anywhere in the IR).
func TestConstraints_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, *populatedConstraints())
}

// TestEncoding_ZeroValueShape pins Encoding's omitempty contract: all three
// fields are optional, so a Property/Scalar with no encoding override
// marshals to an empty object rather than an explicit "no encoding" tag.
func TestEncoding_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Encoding{}, `{}`)
}

// TestEncoding_PopulatedRoundTrip pins that a fully populated Encoding — name,
// a nested nullable WireType, and a media type — round-trips.
func TestEncoding_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, *populatedEncoding())
}

// TestXMLHints_ZeroValueShape pins XMLHints' contract that Wrapped carries no
// omitempty: "items are not wrapped" is a declared fact about the XML shape,
// not an absence, while the four string fields stay optional.
func TestXMLHints_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.XMLHints{}, `{"wrapped":false}`)
}

// TestXMLHints_PopulatedRoundTrip pins that a fully populated XMLHints round-
// trips.
func TestXMLHints_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, *populatedXMLHints())
}
