package schema

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// maxSchemaDepth caps schema-lowering recursion (styleguide bounded-recursion
// rule). Interning by pointer is what terminates recursive and diamond schemas;
// this bound only guards pathologically deep inline nesting. It counts from where
// the lowering owning a position enters, and from TopLevelDepth for a node only
// a $ref's promise builds (hoistSubSchema): a $ref never lowers what it names
// (resolvePointer), so a position meets the bound at one depth, whichever
// reaches it first (GitHub #748).
const maxSchemaDepth = 256

// TopLevelDepth is the nesting a schema position outside the walk starts at.
// The walk counts its own frames, so every entry point into it begins at zero;
// naming it keeps a bare 0 out of the call sites that only pass it through.
const TopLevelDepth = 0

// registeredNode returns the node interning registered under id, and ok=false
// when there is none yet.
//
// A node the registry reports as Building is reached again only by a walk
// running below the frame building it, and that frame finishes the node, this
// declaration included, once its build returns (GitHub #749). Any other miss
// is a compiler bug no source can provoke, so it is reported as one: the caller
// was about to attach docs, examples or preserved constructs, which would
// otherwise vanish.
func registeredNode(c lowering.Ctx, ts *compile.Types, id ir.TypeID, pointer jsontext.Pointer) (ir.TypeDef, bool, []ir.Diagnostic) {
	td, ok := ts.Node(id)
	if ok {
		return td, true, nil
	}
	if ts.Building(id) {
		return nil, false, nil
	}
	return nil, false, []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
		"internal: type %q is named at this pointer but absent from the registry; its source constructs are dropped", id)}
}

// internNode is the single hoisting entry point: it derives pointer's stable
// TypeID (ids.Document.ForPointer) and shared TypeCommon (commonFor) once, then
// interns build's result under that ID. build receives the built TypeCommon, so
// it never re-derives pointer, hint or id.
//
// A $ref promises the node rather than building it (resolvePointer), so every
// lowering reaching pointer does so in the context its position gives it, and
// the first build stands for all. A node a $ref hoists where no lowering
// interned one takes the hint the position's own lowering recorded
// (hoistSubSchema), so hint is always the position's.
func internNode(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, hint string,
	build func(common ir.TypeCommon) ir.TypeDef,
) ir.TypeID {
	id := c.IDs().ForPointer(pointer)
	return ts.Intern(c.IDs().Coordinate(pointer), id, func() ir.TypeDef { return build(commonFor(c, id, pointer, hint)) })
}

// commonFor builds the TypeCommon shared by every hoisted node at pointer. A
// top-level component schema is named (source + canonical words); any deeper
// inline position is anonymous and carries only the context-derived hint.
func commonFor(c lowering.Ctx, id ir.TypeID, pointer jsontext.Pointer, hint string) ir.TypeCommon {
	common := ir.TypeCommon{
		ID:         id,
		Provenance: c.ProvenanceAt(pointer),
	}
	if name, ok := ids.ComponentSchemaName(pointer); ok {
		common.Name = compile.NamingFor(name)
	} else {
		common.Anonymous = true
		common.Name = compile.NamingHint(hint)
	}
	return common
}
