// Package pass stands for a package beside the compilers: the registry, name
// and fragment rules hold it, and the ID rule does not.
package pass

import (
	"net/url"

	"github.com/dexpace/morphic/ir"
)

// Rewrite writes the registry, reads a fragment and looks a node up by an ID
// spelled out.
func Rewrite(doc *ir.Document, def ir.TypeDef) error {
	doc.Types["t/pass/x"] = def      // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	_, err := url.QueryUnescape("x") // want "fragmentreadings: url.QueryUnescape reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_ = doc.Types[ir.TypeID("t/pass/y")]
	return err
}
