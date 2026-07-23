package openapi

import (
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/ir"
)

// maxSchemaDepth caps schema-lowering recursion (styleguide bounded-recursion
// rule). Interning by pointer is what terminates recursive and diamond schemas;
// this bound only guards pathologically deep inline nesting.
const maxSchemaDepth = 256

// A lowerer performs lowering: the translation of one source-shaped OpenAPI
// document into Morphic's spec-agnostic IR. "Lowering" is the standard compiler
// term for moving from a high-level, format-specific representation to a lower,
// more uniform internal one (LLVM lowers its IR to machine code, Rust lowers HIR
// to MIR); here the descent is OpenAPI schemas -> ir.TypeDef nodes, and every
// lower* method is one step of it.
//
// The word carries two senses in this repo, and this is the first: the faithful,
// lossless source -> IR translation. It is deliberately NOT the lossy,
// target-shaped "lowered late" of invariant #2 (e.g. collapsing a union to
// optional fields for a language without sum types) — that happens far
// downstream in emitter refiners, never in a compiler.
//
// The struct itself is the single mutable context of one Compile call — a local,
// never a package global — threading the interning table, accumulated
// diagnostics, and the recursion-depth counter through every schema position.
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
	depth                int
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
func componentSchemaName(pointer string) (string, bool) {
	const prefix = "/components/schemas/"
	if !strings.HasPrefix(pointer, prefix) {
		return "", false
	}
	rest := pointer[len(prefix):]
	if rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	return unescapeSegment(rest), true
}
