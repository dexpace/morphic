// Package fragment stands for the one package that reads a $ref fragment, and
// so may use what the others may not.
package fragment

import (
	"net/url"

	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/ir"
)

// Pointer reads a fragment both ways, which draws nothing here.
func Pointer(r references.Reference) (string, error) {
	_ = r.GetJSONPointer()
	return url.QueryUnescape(string(r))
}

// Registry builds a registry, which the exemption from the fragment rule does
// not excuse.
func Registry() ir.TypeRegistry {
	return ir.TypeRegistry{} // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
}
