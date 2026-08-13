package schema

import (
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// Ref is THE schema entry point: every schema position (property, items,
// params, bodies) flows through it, yielding a TypeRef into the type registry.
// It normalizes the two nullability dialects onto the single IR bit and never
// lowers a $ref target from the reference site.
//
// It defaults to annotation.HomeOwnNode deliberately: a position added later
// inherits the lossless behaviour, and only a caller that can prove it already
// carries the annotations opts out through CarriedRef.
func Ref(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string) (ir.TypeRef, []ir.Diagnostic) {
	return schemaRefHomed(c, ts, anchors, depth, js, pointer, hint, annotation.HomeOwnNode)
}

// CarriedRef lowers a schema whose annotations the calling position
// already carries — a model property, a header, a parameter. Those callers
// copy the declaration onto their own ir.Property/ir.Parameter, so the pointer
// must not also hoist a node to hold it: one home per declaration.
func CarriedRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string) (ir.TypeRef, []ir.Diagnostic) {
	return schemaRefHomed(c, ts, anchors, depth, js, pointer, hint, annotation.HomeCarrier)
}

// schemaRefHomed is the shared body of the two entry points above; home only
// reaches the two places that can hoist an annotation-holding alias.
func schemaRefHomed(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
	// depth counts the active frames of this function, which is where every
	// recursive descent re-enters. Incrementing on entry and passing the result
	// down is the parameter form of a counter that used to live on a shared
	// struct, back when one held the whole compile.
	depth++
	if depth > maxSchemaDepth {
		return ts.PrimRef(ir.PrimAny), []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.DegradedConstruct, pointer,
			"schema nesting exceeds %d; lowered as any", maxSchemaDepth)}
	}
	if js == nil {
		return ts.PrimRef(ir.PrimAny), nil
	}
	if js.IsBool() {
		if b := js.GetBool(); b != nil && !*b {
			id, diags := falseSchema(c, ts, pointer, hint)
			return ir.TypeRef{Target: id}, diags
		}
		return ts.PrimRef(ir.PrimAny), nil
	}
	// Past the IsBool check, the either's left schema is always set (an empty
	// either reads as a bool), so GetSchema never returns nil here.
	schema := js.GetSchema()
	if resolve.IsRefSite(js, schema) {
		return refSiteRef(c, ts, anchors, depth, js, schema, pointer, hint, home)
	}
	return schemaBody(c, ts, anchors, depth, schema, pointer, hint, home)
}

// refSiteRef resolves a $ref position and keeps whatever is written beside the
// $ref. Those siblings bind the position, not the referent, so they cannot go
// on the target's node; when the position has no carrier to hold them either,
// an alias over the target becomes their home.
//
// In JSON Schema 2020-12, and so in OpenAPI 3.1, `$ref` is an ordinary keyword
// and its siblings are conjoined with it. The alias carries the position's
// constraints and annotations; every census keyword written beside the $ref is
// kept verbatim on it instead, because an alias has no property set, no member
// set, no value and no encoding of its own (GitHub #283).
//
// At an annotation.HomeCarrier position no alias is hoisted — a description or a
// bound beside a property's $ref belongs on the property (GitHub #114) — so the
// carrier keeps them there too, through PreserveRefSiteKeywords.
func refSiteRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema, pointer, hint string, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
	target, diags := refTypeRef(c, ts, anchors, depth, js, pointer)
	unhomed := unhomedKeywords(s, nil, nil)
	ref, homeDiags := homeDeclaration(c, ts, anchors, s, target, pointer, hint, home, len(unhomed) > 0)
	diags = append(diags, homeDiags...)
	if home != annotation.HomeOwnNode {
		return ref, diags
	}
	return ref, append(diags, recordUnhomedKeywords(c, ts, ref.Target, s, unhomed, refSiteShape, pointer)...)
}

// PreserveRefSiteKeywords keeps, on a carrier's own Unmodeled, the keywords a
// $ref position declared that the alias it resolves to cannot hold. It is
// refSiteRef's other half: the same census, recorded where an
// annotation.HomeCarrier position keeps everything else its schema declared.
//
// A schema that lowered to a node of its own already had the census recorded
// there, so the carrier adds nothing — one home per declaration, exactly as
// fillPropertyAnnotations draws the line.
func PreserveRefSiteKeywords(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled,
	js *oas3.JSONSchema[oas3.Referenceable], t ir.TypeRef, pointer string,
) []ir.Diagnostic {
	// GetSchema is nil-safe and yields nil for a boolean schema too, so the one
	// check covers an absent position, a boolean one, and a caller passing nil.
	s := js.GetSchema()
	if s == nil || !resolve.IsRefSite(js, s) || LoweredToOwnNode(ts, pointer, t) {
		return nil
	}
	return recordUnhomedAt(c, p, s, unhomedKeywords(s, nil, nil), refSiteShape, pointer)
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
func homeDeclaration(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, s *oas3.Schema, target ir.TypeRef, pointer, hint string, home annotation.Home, unhomed bool) (ir.TypeRef, []ir.Diagnostic) {
	ref, diags := hoistDeclarationHome(c, ts, s, target, pointer, hint, home, unhomed)
	if s != nil {
		diags = append(diags, attachDeclaredAnnotations(c, ts, anchors, s, pointer)...)
	}
	return ref, append(diags, recordDeclarationResidue(c, ts, s, pointer, home)...)
}

// refTypeRef resolves a $ref position to its target's stable ID, carrying the
// combined ref-site and target nullability. A top-level component target keeps
// its stable named ID (lowered where it is defined); an internal sub-schema
// target is hoisted at its pointer-derived ID so the reference never dangles. A
// genuinely external or unresolvable target is diagnosed and dropped to any.
func refTypeRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], pointer string) (ir.TypeRef, []ir.Diagnostic) {
	ref := js.GetRef().String()
	id, ok, diags := resolveSchemaRef(c, ts, anchors, depth, js, ref)
	if !ok {
		return ts.PrimRef(ir.PrimAny), append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef, pointer,
			"unresolved $ref %q", ref))
	}
	return ir.TypeRef{Target: id, Nullable: refNullable(js)}, diags
}

// resolveSchemaRef resolves a schema-position $ref to an interned TypeID, never
// synthesizing an ID that nothing backs. A top-level component keeps its stable
// named ID; an already-interned target reuses it; an internal sub-schema the
// resolver library resolved is hoisted at its pointer-derived ID. It returns
// ok=false for a cross-document reference, a reference to an undeclared
// component, or a pointer the library could not resolve.
func resolveSchemaRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], ref string) (ir.TypeID, bool, []ir.Diagnostic) {
	pointer, ok := c.RefScope().InternalPointer(ref)
	if !ok {
		return "", false, nil
	}
	if id, resolved, handled := c.RefScope().ComponentRef(pointer); handled {
		return id, resolved, nil
	}
	if id, ok := resolve.InternedID(ts, pointer); ok {
		return id, true, nil
	}
	decl := annotation.DeclaredSchema(js)
	if decl == nil {
		return "", false, nil
	}
	return hoistSubSchema(c, ts, anchors, depth, decl, pointer)
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
// is a $ref carrying siblings aliases its target while keeping them. Ref
// already draws that distinction — peeling a $ref off to a TypeRef and
// lowering a concrete body in place — leaving this to intern whichever node
// the pointer ends up owning.
func hoistSubSchema(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, decl *oas3.JSONSchema[oas3.Referenceable], pointer string) (ir.TypeID, bool, []ir.Diagnostic) {
	s := annotation.At(decl)
	if s.Node == nil {
		return "", false, nil
	}
	hint := subSchemaHint(decl, pointer)
	ref, diags := Ref(c, ts, anchors, depth, decl, pointer, hint)
	if owned, ok := ts.Lookup(pointer); ok {
		return owned, true, diags
	}
	var kept ir.Unmodeled
	cons, consDiags := schemaConstraints(c, &kept, s.Node, pointer)
	diags = append(diags, consDiags...)
	id := internAlias(c, ts, pointer, hint, ref, cons, kept)
	// As in lowerComponentSchema: this alias is the first node the pointer owns,
	// so the annotations Ref had nowhere to put now have a home.
	return id, true, append(diags, attachDeclaredAnnotations(c, ts, anchors, s.Node, pointer)...)
}

// subSchemaHint names the node a $ref'd sub-schema pointer owns: the target it
// aliases when the sub-schema is itself a $ref carrying siblings, the branch
// hint when the pointer addresses a composition branch, the structural hint when
// it addresses an inline structural position, the pointer's last segment
// otherwise.
//
// Every case but the last exists because another lowering can own the same
// pointer and derives its hint that way. Both lowerings reach the pointer — the
// enclosing schema through its own body, this one through an outside $ref naming
// it — and only the first to arrive interns the node, so a hint derived
// differently here makes the document depend on declaration order.
//
// The branch case is the second half of that agreement for a composition branch.
// Falling through to the last segment named an inline branch after its own
// ordinal — "0" — which is a hint an emitter cannot build an identifier from,
// and which disagreed with the composition's "variant_0" (GitHub #181).
//
// The structural case is the same agreement for items, additionalProperties, a
// patternProperties entry and a prefixItems slot (GitHub #353), where the last
// segment named the node after the keyword that holds it — "items" — or after
// the pattern text or the slot ordinal, none of which distinguish it from the
// same position on any other schema.
func subSchemaHint(decl *oas3.JSONSchema[oas3.Referenceable], pointer string) string {
	if decl != nil && decl.IsReference() {
		if name := refLastSegment(decl.GetRef().String()); name != "" {
			return name
		}
	}
	if hint, ok := branchPointerHint(pointer); ok {
		return hint
	}
	if hint, ok := structuralPointerHint(pointer); ok {
		return hint
	}
	return refLastSegment(pointer)
}

// componentSchemasPrefix is the pointer root under which a structural position's
// enclosing hint is the enclosing pointer's own last segment. See
// structuralPointerHint for why the derivation is confined to it.
const componentSchemasPrefix = "/components/schemas/"

// structuralPointerHint returns the hint the inline structural position at
// pointer takes, for a caller holding only the pointer, and whether the pointer
// addresses one it can answer.
//
// It is branchPointerHint's counterpart for the four positions whose hint is
// composed rather than positional: the structural lowering builds them as
// compile.SubHint(enclosing, role), so answering requires the enclosing node's
// hint, which a bare pointer walk does not carry. Under /components/schemas it
// does: the enclosing hint there is the enclosing pointer's own last segment —
// the component's name, or a property's key — so the composition can be replayed
// by peeling roles off the tail and rebuilding from what is left.
//
// It is confined to that root because the derivation is not total, and the
// remainder is a naming decision rather than a bug to paper over. A position
// under /paths takes its enclosing hint from an operationId, a response, or a
// media-type key, and the pointer records none of them: the same items position
// is "response_item" to the structural lowering and has no pointer spelling that
// reproduces it. Answering those with a pointer-derived name would replace one
// disagreement with a different one, so they keep the last-segment fallback.
// That leaves no order dependence there — components lower before paths, so a
// reference from one always interns first — but it does leave a name that
// depends on whether an unrelated schema points at the position. GitHub #372
// holds that remainder.
//
// The walk is bounded by construction: each step consumes at least one segment
// and the loop runs only while segments remain.
func structuralPointerHint(pointer string) (string, bool) {
	segments := strings.Split(pointer, "/")

	var roles []string // innermost first
	for len(segments) > 1 {
		role, consumed, ok := structuralRole(segments)
		if !ok {
			break
		}
		roles = append(roles, role)
		segments = segments[:len(segments)-consumed]
	}
	if len(roles) == 0 {
		return "", false
	}

	enclosing := strings.Join(segments, "/")
	if !strings.HasPrefix(enclosing, componentSchemasPrefix) {
		return "", false
	}

	hint := ids.UnescapeSegment(segments[len(segments)-1])
	for i := len(roles) - 1; i >= 0; i-- {
		hint = compile.SubHint(hint, roles[i])
	}
	return hint, true
}

// structuralRole reports the role the structural lowering names the position at
// the tail of segments by, and how many segments that position spells. The roles
// are the suffixes the four compile.SubHint call sites pass, and a change to one
// of them has to be made here too — TestInlinePosition_HintIsTheSameInBothOrders
// is what fails when they drift.
//
// segments holds at least two entries: its only caller reads the tail of a
// pointer, which always starts with the empty segment before the first token, and
// stops looping once one segment is left.
func structuralRole(segments []string) (role string, consumed int, ok bool) {
	last := segments[len(segments)-1]
	switch last {
	case "items":
		return "item", 1, true
	case "additionalProperties":
		return "value", 1, true
	}
	switch segments[len(segments)-2] {
	case "patternProperties":
		return "pattern", 2, true
	case "prefixItems":
		if isDecimalIndex(last) {
			return last, 2, true
		}
	}
	return "", 0, false
}

// refNullable reports whether a $ref usage admits null: the reference site or
// its resolved target admits null in any spelling. It is schemaAdmitsNull read
// across the reference (refNullVerdict), so the two cannot answer one schema
// differently.
func refNullable(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	budget := maxNullConjuncts
	return refNullVerdict(js, &budget) == nullAdmitted
}
