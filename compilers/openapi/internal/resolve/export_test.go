// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/jsonpointer"
	"github.com/speakeasy-api/openapi/jsonschema/oas3"
)

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

// WholeModelAt reads the whole pointer in one library call, as the library's
// resolver reads it: the schema found there, or nil.
func WholeModelAt(doc any, pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	target, err := jsonpointer.GetTarget(doc, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
	if js, ok := target.(*oas3.JSONSchema[oas3.Referenceable]); ok && err == nil && js != nil {
		return js
	}
	return nil
}
