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
	doc.Types["t/pass/x"] = def      // want "registrywrites: writes a type registry outside ir and compilers/compile; edit a document's registry through ir.Edit"
	_, err := url.QueryUnescape("x") // want "fragmentreadings: url.QueryUnescape reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_ = doc.Types[ir.TypeID("t/pass/y")]
	return err
}

// Prune removes, changes and replaces types through ir.Edit, the way a pass
// writes the registry, which draws nothing.
func Prune(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	e := ir.Edit(doc)
	e.RemoveType("t/pass/unused")
	e.UpdateType("t/pass/kept", func(td ir.TypeDef) { td.Common().Docs.Summary = "kept" })
	e.ReplaceType(&ir.Any{TypeCommon: ir.TypeCommon{ID: "t/pass/any"}})
	return e.Done()
}
