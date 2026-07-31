package openapi

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// schemaRef is THE schema entry point: every schema position (property, items,
// params, bodies) flows through it, yielding a TypeRef into the type registry.
// It normalizes the two nullability dialects onto the single IR bit and never
// lowers a $ref target from the reference site.
//
// It defaults to annotation.HomeOwnNode deliberately: a position added later
// inherits the lossless behaviour, and only a caller that can prove it already
// carries the annotations opts out through carriedSchemaRef.
func (l *lowerer) schemaRef(js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string) ir.TypeRef {
	return l.schemaRefHomed(js, pointer, hint, annotation.HomeOwnNode)
}

// carriedSchemaRef lowers a schema whose annotations the calling position
// already carries — a model property, a header, a parameter. Those callers
// copy the declaration onto their own ir.Property/ir.Parameter, so the pointer
// must not also hoist a node to hold it: one home per declaration.
func (l *lowerer) carriedSchemaRef(js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string) ir.TypeRef {
	return l.schemaRefHomed(js, pointer, hint, annotation.HomeCarrier)
}

// schemaRefHomed is the shared body of the two entry points above; home only
// reaches the two places that can hoist an annotation-holding alias.
func (l *lowerer) schemaRefHomed(js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string, home annotation.Home) ir.TypeRef {
	l.depth++
	defer func() { l.depth-- }()
	if l.depth > maxSchemaDepth {
		l.diag(ir.SeverityError, diag.DegradedConstruct, pointer,
			"schema nesting exceeds %d; lowered as any", maxSchemaDepth)
		return l.types.PrimRef(ir.PrimAny)
	}
	if js == nil {
		return l.types.PrimRef(ir.PrimAny)
	}
	if js.IsBool() {
		if b := js.GetBool(); b != nil && !*b {
			id, diags := falseSchema(l.ctx, l.types, pointer, hint)
			l.diags.AppendAll(diags)
			return ir.TypeRef{Target: id}
		}
		return l.types.PrimRef(ir.PrimAny)
	}
	// Past the IsBool check, the either's left schema is always set (an empty
	// either reads as a bool), so GetSchema never returns nil here.
	schema := js.GetSchema()
	if resolve.IsRefSite(js, schema) {
		return l.refSiteRef(js, schema, pointer, hint, home)
	}
	return l.schemaBody(schema, pointer, hint, home)
}

// refSiteRef resolves a $ref position and keeps whatever is written beside the
// $ref. Those siblings bind the position, not the referent, so they cannot go
// on the target's node; when the position has no carrier to hold them either,
// an alias over the target becomes their home.
func (l *lowerer) refSiteRef(js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema, pointer, hint string, home annotation.Home) ir.TypeRef {
	return l.homeDeclaration(s, l.refTypeRef(js, pointer), pointer, hint, home)
}

// homeDeclaration gives what s writes at this position a home and returns the
// reference the position resolves to — an alias over target where the keywords
// need a node of their own, target itself where the position declares none.
//
// It is the one statement of that step, so every schema position keeps its
// declaration the same way: a $ref site, a lowered body, and an allOf branch's
// $ref, which reached none of it until it was routed here. A position that
// skips it drops what was written at it without a word — which is what both
// GitHub #116 and #143 were.
func (l *lowerer) homeDeclaration(s *oas3.Schema, target ir.TypeRef, pointer, hint string, home annotation.Home) ir.TypeRef {
	ref := l.hoistDeclarationHome(s, target, pointer, hint, home)
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
		l.diag(ir.SeverityError, diag.UnresolvedRef, pointer,
			"unresolved $ref %q", ref)
		return l.types.PrimRef(ir.PrimAny)
	}
	return ir.TypeRef{Target: id, Nullable: refNullable(js)}
}

// resolveSchemaRef resolves a schema-position $ref to an interned TypeID, never
// synthesizing an ID that nothing backs. A top-level component keeps its stable
// named ID; an already-interned target reuses it; an internal sub-schema the
// resolver library resolved is hoisted at its pointer-derived ID. It returns
// ok=false for a cross-document reference, a reference to an undeclared
// component, or a pointer the library could not resolve.
func (l *lowerer) resolveSchemaRef(js *oas3.JSONSchema[oas3.Referenceable], ref string) (ir.TypeID, bool) {
	pointer, ok := l.ctx.refScope().InternalPointer(ref)
	if !ok {
		return "", false
	}
	if id, resolved, handled := l.ctx.refScope().ComponentRef(pointer); handled {
		return id, resolved
	}
	if id, ok := resolve.InternedID(l.types, pointer); ok {
		return id, true
	}
	decl := annotation.DeclaredSchema(js)
	if decl == nil {
		return "", false
	}
	return l.hoistSubSchema(decl, pointer)
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
	s := annotation.At(decl)
	if s.Node == nil {
		return "", false
	}
	hint := subSchemaHint(decl, pointer)
	ref := l.schemaRef(decl, pointer, hint)
	if owned, ok := l.types.Lookup(pointer); ok {
		return owned, true
	}
	cons, consDiags := schemaConstraints(l.ctx, s.Node, pointer)
	l.diags.AppendAll(consDiags)
	id := internAlias(l.ctx, l.types, pointer, hint, ref, cons)
	// As in lowerComponentSchema: this alias is the first node the pointer owns,
	// so the annotations schemaRef had nowhere to put now have a home.
	l.attachDeclaredAnnotations(s.Node, pointer)
	return id, true
}

// subSchemaHint names the node a $ref'd sub-schema pointer owns: the target it
// aliases when the sub-schema is itself a $ref carrying siblings, the branch
// hint when the pointer addresses a composition branch, the pointer's last
// segment otherwise.
//
// The first two cases exist because a composition branch can own the same
// pointer and derives its hint that way (branchHint). Both lowerings reach the
// pointer — the branch through its composition, this one through an outside $ref
// naming it — and only the first to arrive interns the node, so a hint derived
// differently here makes the document depend on declaration order.
//
// The branch case is the second half of that agreement. Falling through to the
// last segment named an inline branch after its own ordinal — "0" — which is a
// hint an emitter cannot build an identifier from, and which disagreed with the
// composition's "variant_0" (GitHub #181).
func subSchemaHint(decl *oas3.JSONSchema[oas3.Referenceable], pointer string) string {
	if decl != nil && decl.IsReference() {
		if name := refLastSegment(decl.GetRef().String()); name != "" {
			return name
		}
	}
	if hint, ok := branchPointerHint(pointer); ok {
		return hint
	}
	return refLastSegment(pointer)
}

// refNullable reports whether a $ref usage admits null: the reference site or
// its resolved target admits null in any spelling. The ref site must recompute
// this because a target interned at its own ID (a model, a union) discards the
// TypeRef its definition produced, so the bit survives nowhere else.
func refNullable(js *oas3.JSONSchema[oas3.Referenceable]) bool {
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
