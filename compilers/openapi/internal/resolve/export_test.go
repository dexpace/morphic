package resolve

import "encoding/json/jsontext"

// MaxRefChain exposes the package's hop bound to its external tests.
//
// The tests that drive ObjectAt, Hops and ScopeOf past it need a chain of
// references the parser's resolver built, whose resolution info no value a test
// constructs reproduces, so they read documents through the loader or the
// compiler, which only package resolve_test can import. Restating 32 there
// would be a maintained number, which this avoids without putting a constant in
// the production API that no production caller reads.
const MaxRefChain = maxRefChain

// Found exposes the answer ObjectAt hands Scope.refuses, whose nil case no
// resolution a test can build reaches.
func Found[S any](obj *S) any { return found(obj) }

// At exposes atPointer, the scope reached reads a position of the source's
// model in, to this package's external tests.
func At(s Scope, pointer jsontext.Pointer) Scope { return atPointer(s, pointer) }
