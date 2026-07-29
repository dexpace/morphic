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

// site is what a schema position declares: Node is the schema written there,
// and Referent — set only for a reference site — is the schema exactly one
// hop away, never the end of a $ref chain.
//
// The split lets an annotation be read from where it was written rather than
// wherever the $ref resolves to, with a fallback to Referent for annotations
// meant to inherit from the target. Only Node has a production reader: a
// declaration's annotations bind the position they are written at
// (attachDeclaredAnnotations), and a component that aliases another keeps the
// target's own annotations reachable through its Base rather than copying
// them. Kind and Referent are for a reader that does want to inherit.
// fillPropertyDetail (schema.go) instead falls back via refTargetSchema,
// which follows a $ref chain to its end (GetResolvedSchema) rather than one
// hop (GetReferenceResolutionInfo, what Referent uses). The two are not
// interchangeable: swapping one for the other would silently change
// property-default and description semantics on a ref-to-ref chain.
type site struct {
	Kind siteKind
	Node *oas3.Schema
	// Referent is nil when Kind is siteReference but the $ref does not
	// resolve; refTypeRef is what diagnoses that, not this.
	Referent *oas3.Schema
}

// siteAt builds the site for js. A $ref position resolves Referent exactly
// one hop through declaredSchema, never the full chain — see site and
// declaredSchema for why that distinction matters.
//
// A schema whose $ref pointer is present but empty ({$ref: ""}) is not a
// reference site: an empty ref resolves nowhere, so IsReference is false and
// it is classified as a declaration like any other schema body. That is what
// keeps Referent's nil guarantee true: a reference site's $ref was genuinely
// attempted, so an unresolved target is the only reason Referent is nil.
//
// siteAt trusts its caller that js is genuinely the schema at the position
// being modeled; nothing here can cross-check that from js alone.
func siteAt(js *oas3.JSONSchema[oas3.Referenceable]) site {
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
// that also carries a $ref — an example or bound written beside a $ref binds
// the position, not the referent, the same rule fillPropertyAnnotations and
// fillPropertyConstraints apply at a property. It returns nil only where no
// body is written: a nil either, or a boolean schema, which admits no
// annotations.
//
// Every position modeled as a site reads it through siteAt: a named
// component (lowerComponentSchema) and a $ref'd internal sub-schema
// (hoistSubSchema, fed one hop at a time by declaredSchema).
func siteSchema(js *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if js == nil || js.IsBool() {
		return nil
	}
	return js.GetSchema()
}

// annotationHome names where the annotations a schema position declares are
// kept. It is what decides whether a position that lowered to a shared node
// hoists one of its own, so the two homes can never both hold the same
// declaration (GitHub #116).
type annotationHome int

const (
	// homeOwnNode marks a position with no home but a type node: items,
	// additionalProperties, prefixItems, patternProperties, a union branch, a
	// media-type schema, a component, a $ref-hoisted sub-schema. A declaration
	// there hoists an alias rather than lose what it wrote.
	homeOwnNode annotationHome = iota
	// homeCarrier marks a position whose caller carries an ir.Property or
	// ir.Parameter that holds the declaration's annotations itself
	// (fillPropertyDetail, fillParamSchema): a model property, a response or
	// part header, an operation parameter. Hoisting there would give one
	// declaration two homes.
	homeCarrier
)

// schemaRef is THE schema entry point: every schema position (property, items,
// params, bodies) flows through it, yielding a TypeRef into the type registry.
// It normalizes the two nullability dialects onto the single IR bit and never
// lowers a $ref target from the reference site.
//
// It defaults to homeOwnNode deliberately: a position added later inherits the
// lossless behaviour, and only a caller that can prove it already carries the
// annotations opts out through carriedSchemaRef.
func (l *lowerer) schemaRef(js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string) ir.TypeRef {
	return l.schemaRefHomed(js, pointer, hint, homeOwnNode)
}

// carriedSchemaRef lowers a schema whose annotations the calling position
// already carries — a model property, a header, a parameter. Those callers
// copy the declaration onto their own ir.Property/ir.Parameter, so the pointer
// must not also hoist a node to hold it: one home per declaration.
func (l *lowerer) carriedSchemaRef(js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string) ir.TypeRef {
	return l.schemaRefHomed(js, pointer, hint, homeCarrier)
}

// schemaRefHomed is the shared body of the two entry points above; home only
// reaches the two places that can hoist an annotation-holding alias.
func (l *lowerer) schemaRefHomed(js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string, home annotationHome) ir.TypeRef {
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
		return l.refSiteRef(js, schema, pointer, hint, home)
	}
	return l.schemaBody(schema, pointer, hint, home)
}

// refSiteRef resolves a $ref position and keeps whatever is written beside the
// $ref. Those siblings bind the position, not the referent, so they cannot go
// on the target's node; when the position has no carrier to hold them either,
// an alias over the target becomes their home.
func (l *lowerer) refSiteRef(js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema, pointer, hint string, home annotationHome) ir.TypeRef {
	ref := l.hoistDeclarationHome(s, l.refTypeRef(js, pointer), pointer, hint, home)
	if s != nil {
		l.attachDeclaredAnnotations(s, pointer)
	}
	l.recordDeclarationResidue(s, pointer, home)
	return ref
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

// refNamesReferent reports whether ref names a schema this compilation can
// point a TypeRef at, answering the question resolveSchemaRef answers without
// interning anything on the way. A classifier needs that: deciding how to lower
// a schema must not hoist nodes as a side effect of asking.
//
// It sits beside resolveSchemaRef because it must stay in step with it, and
// mirrors it minus the two steps that are not pure lookups — hoistSubSchema,
// which interns (its own only failure is a target that declares no schema body,
// which is the last condition here), and the internedID cache hit, which would
// make the answer depend on which schema happened to lower first.
func (l *lowerer) refNamesReferent(js *oas3.JSONSchema[oas3.Referenceable], ref string) bool {
	pointer, ok := l.internalPointer(ref)
	if !ok {
		return false
	}
	if _, resolved, handled := l.resolveComponentRef(pointer); handled {
		return resolved
	}
	decl := declaredSchema(js)
	return decl != nil && siteAt(decl).Node != nil
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
// guarantees a node exists at its pointer-derived ID, aliasing when the body
// reduces to a shared target so a $ref to the sub-schema always resolves
// (invariants 1, 2). The annotations written at that position — value
// constraints and examples — are carried onto the alias exactly as for a
// named scalar component, so a $ref to a constrained scalar sub-schema
// ({type: number, minimum: 5}) does not silently drop them.
//
// decl is the declaration itself, not its resolved form, so a sub-schema that
// is a $ref carrying siblings aliases its target while keeping them. schemaRef
// already draws that distinction — peeling a $ref off to a TypeRef and
// lowering a concrete body in place — leaving this to intern whichever node
// the pointer ends up owning.
func (l *lowerer) hoistSubSchema(decl *oas3.JSONSchema[oas3.Referenceable], pointer string) (ir.TypeID, bool) {
	s := siteAt(decl)
	if s.Node == nil {
		return "", false
	}
	hint := refLastSegment(pointer)
	ref := l.schemaRef(decl, pointer, hint)
	if owned, ok := l.types.Lookup(pointer); ok {
		return owned, true
	}
	id := l.internAlias(pointer, hint, ref, l.schemaConstraints(s.Node, pointer))
	// As in lowerComponentSchema: this alias is the first node the pointer owns,
	// so the annotations schemaRef had nowhere to put now have a home.
	l.attachDeclaredAnnotations(s.Node, pointer)
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
// source file. An exact path match is internal; so is a bare filename (no
// directory) equal to our own basename, since self-references are
// conventionally spelled with just the file's own name (e.g. `m.yaml#/...`
// inside m.yaml). A doc part carrying its own directory is matched in full,
// never on basename alone — otherwise `dir2/m.yaml` referenced from
// `dir1/m.yaml` would misread as a self-reference.
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
	if id, ok := l.types.Lookup(pointer); ok {
		return id, true
	}
	id := typeIDForPointer(pointer)
	if _, ok := l.types.Node(id); ok {
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
