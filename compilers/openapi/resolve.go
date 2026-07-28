package openapi

import (
	"fmt"
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

// String renders k by name; an assertion failure or test diff over a bare
// siteKind would otherwise print the underlying int (0 or 1).
func (k siteKind) String() string {
	switch k {
	case siteDeclaration:
		return "siteDeclaration"
	case siteReference:
		return "siteReference"
	default:
		return fmt.Sprintf("siteKind(%d)", int(k))
	}
}

// site is a position that owns an IR node. Node is the schema written at the
// position; Referent is the schema exactly one hop away, set only for a
// reference site.
//
// The split exists so an annotation can be read from where it was written
// rather than from wherever a $ref happens to resolve: a site-only annotation
// (examples, constraints) reads Node alone, and a site-overrides-referent
// annotation (docs, deprecation, visibility, default) is meant to read Node
// and fall back to Referent. No caller does that yet: fillPropertyDetail
// (schema.go) still resolves its own fallback via refTargetSchema, which
// follows a $ref chain to its end (GetResolvedSchema) rather than one hop
// (GetReferenceResolutionInfo, what Referent uses here). The two are not
// interchangeable — swapping one for the other would silently change
// property-default and description semantics on a ref-to-ref chain, where the
// end of the chain and the schema one hop away can be different schemas.
type site struct {
	Kind siteKind
	Node *oas3.Schema
	// Referent is nil when Kind is siteReference but the $ref does not
	// resolve; refTypeRef is what diagnoses that, not this.
	Referent *oas3.Schema
}

// siteAt builds the site for the position at pointer. A position carrying a
// $ref is a reference site and resolves its referent exactly one hop, never to
// the end of the chain: a sub-schema spelled {$ref: Other, minimum: 7} must read
// as this position, not as Other, or the bound written beside the $ref is lost
// before anything can record it.
//
// A schema whose $ref pointer is present but empty ({$ref: ""}) is not a
// reference site: IsReference is false for it (by definition — an empty ref
// resolves nowhere), so it is classified as a declaration like any other
// schema body, and this narrower test is what keeps Referent's doc comment
// below true: a reference site's $ref was genuinely attempted, so "the $ref
// does not resolve" is the only reason left for Referent to be nil.
//
// siteAt cannot verify the one precondition that actually matters: that js is
// the schema written at pointer, not some other node. That pairing is the
// caller's responsibility — nothing here can cross-check it from js and
// pointer alone. What it can check is that pointer is at least shaped like a
// position: every caller derives it from a JSON pointer (RFC 6901), which is
// either empty (denoting the whole document — no caller here means that) or
// starts with "/".
func (l *lowerer) siteAt(js *oas3.JSONSchema[oas3.Referenceable], pointer string) site {
	if pointer == "" {
		panic("siteAt: empty pointer")
	}
	if pointer[0] != '/' {
		panic(fmt.Sprintf("siteAt: pointer %q is not a JSON pointer", pointer))
	}
	s := site{Kind: siteDeclaration, Node: siteSchema(js)}
	if js == nil || js.IsBool() {
		return s
	}
	if !js.IsReference() {
		return s
	}
	s.Kind = siteReference
	if decl := declaredSchema(js); decl != nil {
		s.Referent = siteSchema(decl)
	}
	return s
}

// siteSchema returns the schema body written at this position, including one
// that also carries a $ref. An example or a bound written beside a $ref applies
// to the position it is written at rather than to the referent, so it may not be
// read off the target — the rule fillPropertyExamples and fillPropertyConstraints
// already apply at a property. It returns nil only where no body is written at
// all: a nil either, or a boolean schema, which admits no annotations.
//
// Every position that owns a node reads it through siteAt: a named component
// (lowerComponentSchema, schema.go) and a $ref'd internal sub-schema
// (hoistSubSchema, which declaredSchema feeds one hop at a time for exactly
// this reason).
func siteSchema(js *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if js == nil || js.IsBool() {
		return nil
	}
	return js.GetSchema()
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
	// Past the IsBool check, the either's left schema is always set (an empty
	// either reads as a bool), so GetSchema never returns nil here.
	schema := js.GetSchema()
	if isRefSite(js, schema) {
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
	s := l.siteAt(decl, pointer)
	if s.Node == nil {
		return "", false
	}
	hint := refLastSegment(pointer)
	ref := l.schemaRef(decl, pointer, hint)
	if owned, ok := l.byPointer[pointer]; ok {
		return owned, true
	}
	id := l.internAlias(pointer, hint, ref, l.schemaConstraints(s.Node, pointer))
	// As in lowerComponentSchema: this alias is the first node the pointer owns,
	// so the annotations schemaRef had nowhere to put now have a home.
	l.attachSchemaExamples(s.Node, pointer)
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

// isRefSite reports whether a position is $ref-shaped: the resolver's own
// IsReference (a non-empty $ref), or a schema body that carries a Ref field of
// its own even when empty. That is deliberately broader than siteAt's
// classification: schemaRef, refTargetSchema, and bodySchemaPointer
// (content.go) all need "there is a $ref-carrying body here," not "there is a
// genuine, followable reference," so the degenerate {$ref: ""} shape counts
// for them even though it does not count as a siteReference. s is the schema
// body the caller already holds; a nil s (a boolean schema carries none) is
// never $ref-shaped.
func isRefSite(js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema) bool {
	return js.IsReference() || (s != nil && s.Ref != nil)
}

// refTargetSchema returns the resolved target schema when js is a $ref, so
// use-site annotations can fall back to the referent; it returns nil otherwise.
func (l *lowerer) refTargetSchema(js *oas3.JSONSchema[oas3.Referenceable], ref *oas3.Schema) *oas3.Schema {
	if !isRefSite(js, ref) {
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
