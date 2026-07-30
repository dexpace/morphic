package openapi

import (
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/ir"
)

// maxSchemaDepth caps schema-lowering recursion (styleguide bounded-recursion
// rule). Interning by pointer is what terminates recursive and diamond schemas;
// this bound only guards pathologically deep inline nesting.
const maxSchemaDepth = 256

// A lowerer performs lowering: the translation of one source-shaped OpenAPI
// document into Morphic's spec-agnostic IR (OpenAPI schemas -> ir.TypeDef
// nodes, one step per lower* method). This is the lossless source -> IR sense
// of "lowering" — not the lossy, target-shaped "lowered late" of invariant #2,
// which happens only in emitter refiners, never here.
//
// The struct is the single mutable context of one Compile call (local, never
// a package global): it threads the interning table, diagnostics, and
// recursion depth through every schema position.
type lowerer struct {
	// ctx is the immutable half: the document, options and source identity, plus
	// the indexes derived from them at entry. Everything below it is mutable.
	ctx lowerCtx
	out *ir.Document
	// types owns the registry and the coordinate map; see compilers/compile.
	types *compile.Types
	// diags accumulates lowering diagnostics, deduped by full identity.
	diags compile.Diags
	// merge reconciles properties declared by more than one allOf branch.
	merge merge.Merger
	// dynamicAnchors indexes the document's $dynamicAnchor declarations by name,
	// built by dynamicAnchorIndex on the first $dynamicRef reached. It stays nil
	// until then: the index costs a walk of the whole raw source tree, and almost
	// no document writes either keyword.
	dynamicAnchors map[string][]string
	// operationIDs maps each operationId already lowered to the mount pointer
	// that claimed it, so a second claim can name the first in its diagnostic.
	operationIDs map[string]string
	depth        int
}

// newLowerer allocates a lowerer over one loaded document, with an empty IR
// document and interning table ready for schema lowering.
//
//nolint:unparam // srcIndex varies once Compile drives the multi-source loop
func newLowerer(srcIndex int, doc *load.Document, opts Options) *lowerer {
	types := compile.NewTypes(srcIndex)
	l := &lowerer{
		ctx: newLowerCtx(srcIndex, doc, opts),
		// The Document shares the framework's live registry rather than being
		// handed a copy at the end: compile.Types owns every write to it, and the
		// architecture test is what keeps that true.
		out:          &ir.Document{Types: types.Registry()},
		types:        types,
		operationIDs: make(map[string]string),
	}
	l.merge = merge.Merger{Resolve: types.Node, Report: l.diag}
	return l
}

// registeredNode returns the node interning registered under id, reporting a
// broken invariant instead of dropping in silence when there is none.
// compile.Types records a pointer's ID and its node together, so every ID
// reached through its pointer map resolves; a miss is a compiler bug no source can provoke, and the
// caller — which was about to attach docs, examples or preserved constructs to
// that node — would otherwise discard them without a trace.
func registeredNode(c lowerCtx, ts *compile.Types, id ir.TypeID, pointer string) (ir.TypeDef, bool, []ir.Diagnostic) {
	td, ok := ts.Node(id)
	if ok {
		return td, true, nil
	}
	return td, false, []ir.Diagnostic{c.diagAt(ir.SeverityError, diag.InternalInvariant, pointer,
		"internal: type %q is named at this pointer but absent from the registry; its source constructs are dropped", id)}
}

// internNode is the single hoisting entry point: it derives pointer's stable
// TypeID (ids.ForPointer) and shared TypeCommon (commonFor) once, then
// interns build's result under that ID. build receives the already-built
// TypeCommon (its ID field is the same id), so it never needs pointer, hint,
// or a bare id to re-derive it.
func internNode(c lowerCtx, ts *compile.Types, pointer, hint string,
	build func(common ir.TypeCommon) ir.TypeDef,
) ir.TypeID {
	id := ids.ForPointer(pointer)
	return ts.Intern(pointer, id, func() ir.TypeDef { return build(commonFor(c, id, pointer, hint)) })
}

// commonFor builds the TypeCommon shared by every hoisted node at pointer. A
// top-level component schema is named (source + canonical words); any deeper
// inline position is anonymous and carries only the context-derived hint.
func commonFor(c lowerCtx, id ir.TypeID, pointer, hint string) ir.TypeCommon {
	common := ir.TypeCommon{
		ID:         id,
		Provenance: c.provenanceAt(pointer),
	}
	if name, ok := ids.ComponentSchemaName(pointer); ok {
		common.Name = compile.NamingFor(name)
	} else {
		common.Anonymous = true
		common.Name = ir.Naming{Hint: hint}
	}
	return common
}
