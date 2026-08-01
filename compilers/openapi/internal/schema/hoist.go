package schema

import (
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// maxSchemaDepth caps schema-lowering recursion (styleguide bounded-recursion
// rule). Interning by pointer is what terminates recursive and diamond schemas;
// this bound only guards pathologically deep inline nesting.
const maxSchemaDepth = 256

// TopLevelDepth is the nesting a schema position outside the walk starts at.
// The walk counts its own frames, so every entry point into it begins at zero;
// naming it keeps a bare 0 out of the call sites that only pass it through.
const TopLevelDepth = 0

// registeredNode returns the node interning registered under id, reporting a
// broken invariant instead of dropping in silence when there is none.
// compile.Types records a pointer's ID and its node together, so every ID
// reached through its pointer map resolves; a miss is a compiler bug no source can provoke, and the
// caller — which was about to attach docs, examples or preserved constructs to
// that node — would otherwise discard them without a trace.
func registeredNode(c lowering.Ctx, ts *compile.Types, id ir.TypeID, pointer string) (ir.TypeDef, bool, []ir.Diagnostic) {
	td, ok := ts.Node(id)
	if ok {
		return td, true, nil
	}
	return td, false, []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
		"internal: type %q is named at this pointer but absent from the registry; its source constructs are dropped", id)}
}

// internNode is the single hoisting entry point: it derives pointer's stable
// TypeID (ids.ForPointer) and shared TypeCommon (commonFor) once, then
// interns build's result under that ID. build receives the already-built
// TypeCommon (its ID field is the same id), so it never needs pointer, hint,
// or a bare id to re-derive it.
func internNode(c lowering.Ctx, ts *compile.Types, pointer, hint string,
	build func(common ir.TypeCommon) ir.TypeDef,
) ir.TypeID {
	id := ids.ForPointer(pointer)
	return ts.Intern(pointer, id, func() ir.TypeDef { return build(commonFor(c, id, pointer, hint)) })
}

// commonFor builds the TypeCommon shared by every hoisted node at pointer. A
// top-level component schema is named (source + canonical words); any deeper
// inline position is anonymous and carries only the context-derived hint.
func commonFor(c lowering.Ctx, id ir.TypeID, pointer, hint string) ir.TypeCommon {
	common := ir.TypeCommon{
		ID:         id,
		Provenance: c.ProvenanceAt(pointer),
	}
	if name, ok := ids.ComponentSchemaName(pointer); ok {
		common.Name = compile.NamingFor(name)
	} else {
		common.Anonymous = true
		common.Name = ir.Naming{Hint: hint}
	}
	return common
}
