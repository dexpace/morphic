// Package pass stands for a package beside the compilers: the registry, tree,
// name and fragment rules hold it, and the ID rule does not.
package pass

import (
	"net/url"

	"github.com/dexpace/morphic/ir"
)

// Rewrite writes the registry, reads a fragment and looks a node up by an ID
// spelled out.
func Rewrite(doc *ir.Document, def ir.TypeDef) error {
	doc.Types["t/pass/x"] = def      // want "registrywrites: writes a type registry outside ir and compilers/compile; edit a document's registry through an ir.Editor"
	_, err := url.QueryUnescape("x") // want "fragmentreadings: url.QueryUnescape reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_ = doc.Types[ir.TypeID("t/pass/y")]
	return err
}

// Overwrite writes every part of the document an edit returns but the fields
// of its own, each of which the edited document shares until the edit writes
// it.
func Overwrite(doc *ir.Document) *ir.Document {
	out, _ := ir.NewEditor(doc).Finish()
	out.Types["t/a"] = nil                                                          // want "registrywrites: writes a type registry outside ir and compilers/compile; edit a document's registry through an ir.Editor"
	out.Services[0].Groups[0].Operations[0].Docs.Summary = "changed"                // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	out.Services[0].Groups[0].Operations = out.Services[0].Groups[0].Operations[:0] // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	out.Auth["a/a"] = ir.AuthScheme{}                                               // want "registrywrites: writes an auth registry outside ir and the compilers; edit a document's registry through an ir.Editor"
	out.Channels["c/a"] = ir.Channel{}                                              // want "registrywrites: writes a channel registry outside ir and the compilers; edit a document's registry through an ir.Editor"
	delete(out.Messages, "m/a")                                                     // want "registrywrites: writes a message registry outside ir and the compilers; edit a document's registry through an ir.Editor"
	out.Name = "renamed"
	return out
}

// Prune removes, changes and replaces every part of a document through an
// ir.Editor, the way a pass writes one, which draws nothing.
func Prune(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	e := ir.NewEditor(doc)
	e.RemoveType("t/pass/unused")
	e.UpdateType("t/pass/kept", func(td ir.TypeDef) { td.Common().Docs.Summary = "kept" })
	e.ReplaceType(&ir.Any{TypeCommon: ir.TypeCommon{ID: "t/pass/any"}})
	e.RemoveService("s/pass/unused")
	e.UpdateGroup("g/pass/kept", func(g *ir.OperationGroup) { g.Docs.Summary = "kept" })
	e.UpdateOperation("o/pass/kept", func(op *ir.Operation) { op.Params = op.Params[:0] })
	e.RemoveOperation("o/pass/unused")
	e.UpdateChannel("c/pass/kept", func(c *ir.Channel) { c.Bindings = nil })
	e.RemoveMessage("m/pass/unused")
	e.UpdateAuthScheme("a/pass/kept", func(a *ir.AuthScheme) { a.Flows = nil })
	e.UpdateDocument(func(d *ir.Document) { d.Servers = nil })
	return e.Finish()
}
