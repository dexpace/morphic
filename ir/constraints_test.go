package ir_test

import (
	"testing"

	"github.com/dexpace/morphic/ir"
)

// TestConstraints_JSONContract pins that every bound is a pointer (nil =
// unconstrained) except ExclusiveMin, ExclusiveMax, and UniqueItems, which
// are plain bools that always serialize — an unconstrained Constraints still
// asserts "not exclusive" and "not unique" as facts, not absences. It also
// pins that a fully populated Constraints round-trips with its BigVal decimal
// strings intact (no float64 anywhere in the IR).
func TestConstraints_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Constraints{},
		`{"exclusiveMin":false,"exclusiveMax":false,"uniqueItems":false}`,
		*populatedConstraints())
}

// TestEncoding_JSONContract pins Encoding's omitempty contract — all three
// fields are optional, so a Property/Scalar with no encoding override
// marshals to an empty object rather than an explicit "no encoding" tag —
// and that a fully populated Encoding — name, a nested nullable WireType, and
// a media type — round-trips.
func TestEncoding_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Encoding{}, `{}`, *populatedEncoding())
}

// TestXMLHints_JSONContract pins XMLHints' contract that Wrapped carries no
// omitempty — "items are not wrapped" is a declared fact about the XML shape,
// not an absence, while the four string fields stay optional — and that a
// fully populated XMLHints round-trips.
func TestXMLHints_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.XMLHints{}, `{"wrapped":false}`, *populatedXMLHints())
}
