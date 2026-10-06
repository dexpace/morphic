package resolve

import (
	"encoding/json/jsontext"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
)

// MaxRefChain exposes the walk's hop bound to this package's external tests.
//
// Only a real compile can build a chain long enough to cross it: the resolution
// info each hop reads is built by the parser's resolver, not by any value a test
// can construct. So the test that holds the walk to the bound lives in package
// resolve_test, where the compiler is importable — and restating 32 there would
// be a maintained number, which is what this avoids without putting a constant
// in the production API that no production caller reads.
const MaxRefChain = maxRefChain

// WholeModelAt exposes the reading ModelAt is held to, so the corpus test in
// package resolve_test compares against the same one as this package's tests:
// two copies could drift apart with every test still green.
func WholeModelAt(doc defs.Navigable, pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	return wholeModelAt(doc, pointer)
}
