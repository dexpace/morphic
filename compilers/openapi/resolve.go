package openapi

import (
	"path"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/ir"
)

// siteKind distinguishes a position that declares a type from one that
// references another type and may carry annotations of its own.
type siteKind int

const (
	siteDeclaration siteKind = iota
	siteReference
)

// site is a position that owns an IR node. Node is the schema written at the
// position; Referent is the schema one hop away, set only for a reference site.
//
// The split is what keeps annotations attached where they were written: a
// site-only annotation (examples, constraints) reads Node alone, while a
// site-overrides-referent annotation (docs, deprecation, visibility, default)
// reads Node and falls back to Referent. Resolving this once here is what stops
// each attachment point from re-deciding it.
type site struct {
	Pointer  string
	Kind     siteKind
	Node     *oas3.Schema
	Referent *oas3.Schema
}

// siteAt builds the site for the position at pointer. A position carrying a
// $ref is a reference site and resolves its referent exactly one hop, never to
// the end of the chain: a sub-schema spelled {$ref: Other, minimum: 7} must read
// as this position, not as Other, or the bound written beside the $ref is lost
// before anything can record it.
func (l *lowerer) siteAt(js *oas3.JSONSchema[oas3.Referenceable], pointer string) site {
	st := site{Pointer: pointer, Kind: siteDeclaration, Node: siteSchema(js)}
	if js == nil || js.IsBool() {
		return st
	}
	if !js.IsReference() && (st.Node == nil || st.Node.Ref == nil) {
		return st
	}
	st.Kind = siteReference
	if decl := declaredSchema(js); decl != nil {
		st.Referent = siteSchema(decl)
	}
	return st
}

// schemaRef is THE schema entry point: every schema position (property, items,
// params, bodies) flows through it, yielding a TypeRef into the type registry.
// It normalizes the two nullability dialects onto the single IR bit and never
// lowers a $ref target from the reference site.
func (l *lowerer) schemaRef(js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string) ir.TypeRef {
	l.depth++
	defer func() { l.depth-- }()
	if l.depth > maxSchemaDepth {
		l.diag(ir.SeverityError, codeDegradedConstruct, pointer,
			"schema nesting exceeds %d; lowered as any", maxSchemaDepth)
		return l.primRef(ir.PrimAny)
	}
	if js == nil {
		return l.primRef(ir.PrimAny)
	}
	if js.IsBool() {
		if b := js.GetBool(); b != nil && !*b {
			return ir.TypeRef{Target: l.falseSchema(pointer, hint)}
		}
		return l.primRef(ir.PrimAny)
	}
	if js.IsReference() {
		return l.refTypeRef(js, pointer)
	}
	// Past the IsBool check, the either's left schema is always set (an empty
	// either reads as a bool), so GetSchema never returns nil here.
	schema := js.GetSchema()
	if schema.Ref != nil {
		return l.refTypeRef(js, pointer)
	}
	return l.schemaBody(schema, pointer, hint)
}

// refTypeRef resolves a $ref position to its target's stable ID, carrying the
// combined ref-site and target nullability. A top-level component target keeps
// its stable named ID (lowered where it is defined); an internal sub-schema
// target is hoisted at its pointer-derived ID so the reference never dangles. A
// genuinely external or unresolvable target is diagnosed and dropped to any.
func (l *lowerer) refTypeRef(js *oas3.JSONSchema[oas3.Referenceable], pointer string) ir.TypeRef {
	ref := js.GetRef().String()
	id, ok := l.resolveSchemaRef(js, ref)
	if !ok {
		l.diag(ir.SeverityError, codeUnresolvedRef, pointer,
			"unresolved $ref %q", ref)
		return l.primRef(ir.PrimAny)
	}
	return ir.TypeRef{Target: id, Nullable: l.refNullable(js)}
}

// resolveSchemaRef resolves a schema-position $ref to an interned TypeID, never
// synthesizing an ID that nothing backs. A top-level component keeps its stable
// named ID; an already-interned target reuses it; an internal sub-schema the
// resolver library resolved is hoisted at its pointer-derived ID. It returns
// ok=false for a cross-document reference, a reference to an undeclared
// component, or a pointer the library could not resolve.
func (l *lowerer) resolveSchemaRef(js *oas3.JSONSchema[oas3.Referenceable], ref string) (ir.TypeID, bool) {
	pointer, ok := l.internalPointer(ref)
	if !ok {
		return "", false
	}
	if id, resolved, handled := l.resolveComponentRef(pointer); handled {
		return id, resolved
	}
	if id, ok := l.internedID(pointer); ok {
		return id, true
	}
	decl := declaredSchema(js)
	if decl == nil {
		return "", false
	}
	return l.hoistSubSchema(decl, pointer)
}

// declaredSchema returns the schema written at the position js references — one
// hop, not the end of the chain. GetResolvedSchema follows a reference to a
// reference all the way through, which is the wrong node to hoist at that
// position: a sub-schema spelled {$ref: Other, minimum: 7} would be read as
// Other, and the bound written beside the $ref would be gone before anything
// could record it.
func declaredSchema(js *oas3.JSONSchema[oas3.Referenceable]) *oas3.JSONSchema[oas3.Referenceable] {
	info := js.GetReferenceResolutionInfo()
	if info == nil {
		return nil
	}
	return info.Object
}

// hoistSubSchema lowers the internal sub-schema declared at pointer and
// guarantees a node exists at the pointer-derived ID, aliasing when the body
// reduces to a shared target so a $ref to the sub-schema always resolves
// (invariants 1, 2). When the body reduces to an alias, the annotations written
// at that position — value constraints and examples — are carried onto the
// alias, exactly as for a named scalar component, so a $ref to a constrained
// scalar sub-schema ({type: number, minimum: 5}) does not silently drop them.
//
// decl is the declaration itself rather than its resolved form, so a sub-schema
// that is a $ref carrying siblings aliases its target and keeps them. schemaRef
// makes that distinction already: it peels a reference off to a TypeRef and
// lowers a concrete body in place, and either way leaves this to intern the node
// the pointer owns.
func (l *lowerer) hoistSubSchema(decl *oas3.JSONSchema[oas3.Referenceable], pointer string) (ir.TypeID, bool) {
	body := siteSchema(decl)
	if body == nil {
		return "", false
	}
	hint := refLastSegment(pointer)
	ref := l.schemaRef(decl, pointer, hint)
	if owned, ok := l.byPointer[pointer]; ok {
		return owned, true
	}
	id := l.internAlias(pointer, hint, ref, l.schemaConstraints(body, pointer))
	// As in lowerComponentSchema: this alias is the first node the pointer owns,
	// so the annotations schemaRef had nowhere to put now have a home.
	l.attachSchemaExamples(body, pointer)
	return id, true
}

// refNullable reports whether a $ref usage admits null: the reference site or
// its resolved target admits null in any spelling. The ref site must recompute
// this because a target interned at its own ID (a model, a union) discards the
// TypeRef its definition produced, so the bit survives nowhere else.
func (l *lowerer) refNullable(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	if s := js.GetSchema(); s != nil && schemaAdmitsNull(s) {
		return true
	}
	resolved := js.GetResolvedSchema()
	if resolved == nil {
		return false
	}
	target := resolved.GetSchema()
	return target != nil && schemaAdmitsNull(target)
}

// refTargetSchema returns the resolved target schema when js is a $ref, so
// use-site annotations can fall back to the referent; it returns nil otherwise.
func (l *lowerer) refTargetSchema(js *oas3.JSONSchema[oas3.Referenceable], ref *oas3.Schema) *oas3.Schema {
	if !js.IsReference() && ref.Ref == nil {
		return nil
	}
	resolved := js.GetResolvedSchema()
	if resolved == nil {
		return nil
	}
	return resolved.GetSchema()
}

// internalPointer returns the same-document JSON pointer a $ref (or discriminator
// mapping) target addresses, and ok=false for a genuine cross-document reference,
// a bare schema name, or a malformed ref. A document part naming this same source
// file (an OpenAPI self-reference) is treated as internal — Milestone 1 interns
// only same-file targets; genuinely external ones are diagnosed and dropped.
func (l *lowerer) internalPointer(ref string) (string, bool) {
	doc, pointer, found := strings.Cut(ref, "#")
	if !found || pointer == "" {
		return "", false
	}
	if doc != "" && !l.sameFile(doc) {
		return "", false
	}
	return pointer, true
}

// sameFile reports whether a $ref document part names this compilation's own
// source file, so the reference resolves back into the same document. An exact
// path match is internal; so is a bare filename (no directory) equal to our own
// basename, since self-references are conventionally spelled with just the
// file's own name (e.g. `m.yaml#/...` inside m.yaml). A doc part that carries
// its own directory is matched in full, never on basename alone — otherwise
// `dir2/m.yaml` referenced from `dir1/m.yaml` would misread as a self-reference.
func (l *lowerer) sameFile(doc string) bool {
	self := l.source.Path
	if self == "" {
		return false
	}
	if doc == self {
		return true
	}
	return !strings.Contains(doc, "/") && doc == path.Base(self)
}

// internedID returns the TypeID a node was interned under at pointer, when one
// already exists there — either a previously hoisted sub-schema (via byPointer)
// or a node registered directly under its pointer-derived ID.
func (l *lowerer) internedID(pointer string) (ir.TypeID, bool) {
	if id, ok := l.byPointer[pointer]; ok {
		return id, true
	}
	id := typeIDForPointer(pointer)
	if _, ok := l.out.Types[id]; ok {
		return id, true
	}
	return "", false
}

// resolveComponentRef resolves an internal pointer addressing a top-level
// component schema to its stable named ID, but only when that component is
// declared. It returns handled=true once the pointer is classified as a
// component pointer (declared or not), so callers can stop; a declared
// component yields ok=true, an undeclared one ok=false (a dangling reference to
// drop). The ID is rebuilt from the component's canonical name — unescaped, then
// re-escaped by ptr — rather than from the incoming pointer text, so a
// non-canonically escaped reference (e.g. `A~B` for a component named "A~B",
// interned under `A~0B`) still resolves to the interned node instead of an
// unbacked ID.
func (l *lowerer) resolveComponentRef(pointer string) (id ir.TypeID, ok, handled bool) {
	name, isComponent := componentSchemaName(pointer)
	if !isComponent {
		return "", false, false
	}
	if l.schemas[name] {
		return namedTypeID(ptr("components", "schemas", name)), true, true
	}
	return "", false, true
}
