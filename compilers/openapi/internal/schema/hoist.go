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
// a lowering enters, so a $ref and its target's declaration reach it at different
// coordinates, and the declaration's rebuild leaves what the reference built past
// it (GitHub #748).
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
// TypeID (ids.ForPointer) and shared TypeCommon (commonFor) once, then
// interns build's result under that ID. build receives the already-built
// TypeCommon (its ID field is the same id), so it never needs pointer, hint,
// or a bare id to re-derive it.
func internNode(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, hint string,
	build func(common ir.TypeCommon) ir.TypeDef,
) ir.TypeID {
	id := ids.ForPointer(pointer)
	mint := func() ir.TypeDef { return build(commonFor(c, id, pointer, hint)) }
	if c.NamesByReference() {
		// A $ref named this coordinate, so the name is a placeholder until the
		// declaration that owns it arrives (GitHub #372).
		return ts.InternProvisional(string(pointer), id, mint)
	}
	// The declaration reaching its own coordinate. A reference that got here
	// first built the node and everything beneath it under its own names, so the
	// declaration builds it again rather than taking the reference's (GitHub #529).
	interned := ts.InternDeclared(string(pointer), id, mint)
	// On a first visit or after a rebuild the node already has this name; only
	// where a rebuild was refused is there a reference's placeholder to replace.
	ts.NameFromDeclaration(string(pointer), hint)
	return interned
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
