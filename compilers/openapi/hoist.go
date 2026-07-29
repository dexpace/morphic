package openapi

import (
	soa "github.com/speakeasy-api/openapi/openapi"

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
	srcIndex  int
	doc       *soa.OpenAPI
	source    ir.SourceInfo // identity of the loaded source, stamped into Document.Sources
	out       *ir.Document
	opts      Options
	diags     []ir.Diagnostic
	byPointer map[string]ir.TypeID // pointer -> hoisted/interned TypeID
	schemas   map[string]bool      // declared component-schema names (for ref resolution)
	// diagnosedConstraints records pointers whose constraint diagnostics were
	// already emitted, so a sub-schema read from two positions (its owning
	// property and a $ref that hoists it) reports a malformed bound only once.
	diagnosedConstraints map[string]bool
	// emitted records the identity of every diagnostic already appended, so one
	// defect is reported once however many times lowering reaches it. Lowering a
	// referenced component at its declaration rather than at each use site made
	// this observable: severity, code, provenance and message are then identical
	// per referencing operation, and a second copy tells a reader nothing the
	// first did not. Unlike diagnosedConstraints — which silences a whole pointer
	// once any constraint diagnostic lands there — this compares the full
	// diagnostic, so two different defects at one pointer both still surface.
	emitted map[string]bool
	// operationIDs maps each operationId already lowered to the mount pointer
	// that claimed it, so a second claim can name the first in its diagnostic.
	operationIDs map[string]string
	depth        int
}

// newLowerer allocates a lowerer over one loaded document, with an empty IR
// document and interning table ready for schema lowering.
//
//nolint:unparam // srcIndex varies once Compile drives the multi-source loop
func newLowerer(srcIndex int, doc *loaded, opts Options) *lowerer {
	return &lowerer{
		srcIndex:             srcIndex,
		doc:                  doc.Doc,
		source:               doc.Source,
		out:                  &ir.Document{Types: ir.TypeRegistry{}},
		opts:                 opts,
		byPointer:            make(map[string]ir.TypeID),
		diagnosedConstraints: make(map[string]bool),
		emitted:              make(map[string]bool),
		operationIDs:         make(map[string]string),
	}
}

// intern returns the TypeID for pointer, lowering the schema on first visit.
// Registering the ID before descending is what terminates recursive schemas:
// a self-reference reached during build hits byPointer and returns the ID
// without re-entering build.
func (l *lowerer) intern(pointer string, id ir.TypeID, build func() ir.TypeDef) ir.TypeID {
	if existing, ok := l.byPointer[pointer]; ok {
		return existing
	}
	l.byPointer[pointer] = id
	l.out.Types[id] = build() // build may recurse; self-references hit byPointer
	return id
}

// registeredNode returns the node interning registered under id, reporting a
// broken invariant instead of dropping in silence when there is none. intern
// records a pointer's ID and its node together, so every ID reached through
// byPointer resolves; a miss is a compiler bug no source can provoke, and the
// caller — which was about to attach docs, examples or preserved constructs to
// that node — would otherwise discard them without a trace.
func (l *lowerer) registeredNode(id ir.TypeID, pointer string) (ir.TypeDef, bool) {
	td, ok := l.out.Types[id]
	if !ok {
		l.diag(ir.SeverityError, codeInternalInvariant, pointer,
			"internal: type %q is named at this pointer but absent from the registry; its source constructs are dropped", id)
	}
	return td, ok
}

// internNode is the single hoisting entry point: it derives pointer's stable
// TypeID (typeIDForPointer) and shared TypeCommon (commonFor) once, then
// interns build's result under that ID. build receives the already-built
// TypeCommon (its ID field is the same id), so it never needs pointer, hint,
// or a bare id to re-derive it.
func (l *lowerer) internNode(pointer, hint string, build func(common ir.TypeCommon) ir.TypeDef) ir.TypeID {
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef { return build(l.commonFor(id, pointer, hint)) })
}

// primRef interns the primitive of kind k under its shared ID on first use and
// returns a reference to it. Primitives are leaves, so they never enter the
// pointer-keyed interning table.
func (l *lowerer) primRef(k ir.PrimKind) ir.TypeRef {
	id := primTypeID(k)
	if _, ok := l.out.Types[id]; !ok {
		l.out.Types[id] = &ir.Primitive{
			TypeCommon: ir.TypeCommon{ID: id, Provenance: ir.Provenance{Source: l.srcIndex}},
			Prim:       k,
		}
	}
	return ir.TypeRef{Target: id}
}

// primID interns the primitive of kind k and returns its TypeID.
func (l *lowerer) primID(k ir.PrimKind) ir.TypeID {
	return l.primRef(k).Target
}

// commonFor builds the TypeCommon shared by every hoisted node at pointer. A
// top-level component schema is named (source + canonical words); any deeper
// inline position is anonymous and carries only the context-derived hint.
func (l *lowerer) commonFor(id ir.TypeID, pointer, hint string) ir.TypeCommon {
	common := ir.TypeCommon{
		ID:         id,
		Provenance: ir.Provenance{Source: l.srcIndex, Pointer: pointer},
	}
	if name, ok := componentSchemaName(pointer); ok {
		common.Name = ir.Naming{Source: name, Canonical: canonicalWords(name)}
	} else {
		common.Anonymous = true
		common.Name = ir.Naming{Hint: hint}
	}
	return common
}

// typeIDForPointer returns the stable TypeID for a schema hoisted at pointer:
// the named-component ID for a top-level component schema, the anonymous
// (hoisted-inline) ID otherwise.
func typeIDForPointer(pointer string) ir.TypeID {
	if _, ok := componentSchemaName(pointer); ok {
		return namedTypeID(pointer)
	}
	return anonTypeID(pointer)
}

// componentSchemaName reports whether pointer addresses a top-level component
// schema (/components/schemas/<name> with no deeper path) and returns its name.
// Only this kind of component declares a named type in OpenAPI, which is why it
// alone gates namedTypeID.
func componentSchemaName(pointer string) (string, bool) {
	kind, name, ok := componentEntry(pointer)
	return name, ok && kind == "schemas"
}
