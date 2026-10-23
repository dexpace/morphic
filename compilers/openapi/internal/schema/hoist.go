package schema

import (
	"encoding/json/jsontext"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

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
// it never re-derives pointer, name or id.
//
// A $ref promises the node rather than building it (resolvePointer), so every
// lowering reaching pointer does so in the context its position gives it, and
// the first build stands for all.
func internNode(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, build func(common ir.TypeCommon) ir.TypeDef,
) ir.TypeID {
	id := c.IDs().ForPointer(pointer)
	return ts.Intern(c.IDs().Coordinate(pointer), id, func() ir.TypeDef { return build(commonFor(c, id, pointer)) })
}

// commonFor builds the TypeCommon shared by every hoisted node at pointer. A
// top-level component schema is named (source + canonical words); any deeper
// inline position is anonymous and carries only the hint its coordinate names
// it by (lowering.NameAt), whichever lowering builds it.
func commonFor(c lowering.Ctx, id ir.TypeID, pointer jsontext.Pointer) ir.TypeCommon {
	common := ir.TypeCommon{
		ID:         id,
		Provenance: c.ProvenanceAt(pointer),
	}
	if name, ok := ids.ComponentSchemaName(pointer); ok {
		common.Name = compile.NamingFor(name)
	} else {
		common.Anonymous = true
		common.Name = compile.NamingHint(lowering.NameAt(c, pointer))
	}
	return common
}

// internAlias interns a Scalar at pointer whose Base is target, so a component
// (or a sibling-carrying schema) whose body lowered to a shared or referenced
// target still owns a resolvable node at its own TypeID. It carries the value
// constraints s writes, and the exclusive modifier no Constraints field holds,
// for the reason hoistScalar records.
func internAlias(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, target ir.TypeRef,
) (ir.TypeID, []ir.Diagnostic) {
	var kept ir.Unmodeled
	cons, diags := ConstraintsOf(c, &kept, s, pointer)
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		common.Unmodeled = annotation.MergeUnmodeled(common.Unmodeled, kept)
		return &ir.Scalar{TypeCommon: common, Base: new(target), Constraints: cons}
	})
	return id, diags
}
