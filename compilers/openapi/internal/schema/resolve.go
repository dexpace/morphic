package schema

import (
	"encoding/json/jsontext"
	"slices"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
func Ref(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer, hint string) (ir.TypeRef, []ir.Diagnostic) {
	return schemaRefHomed(c, ts, anchors, depth, js, pointer, hint, annotation.HomeOwnNode)
}

// CarriedRef lowers a schema whose annotations the calling position
// already carries — a model property, a header, a parameter. Those callers
// copy the declaration onto their own ir.Property/ir.Parameter, so the pointer
// must not also hoist a node to hold it: one home per declaration.
func CarriedRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer, hint string) (ir.TypeRef, []ir.Diagnostic) {
	return schemaRefHomed(c, ts, anchors, depth, js, pointer, hint, annotation.HomeCarrier)
}

// schemaRefHomed is the shared body of the two entry points above; home only
// reaches the two places that can hoist an annotation-holding alias.
func schemaRefHomed(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer, hint string, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
	// depth counts the active frames of this function, which is where every
	// recursive descent re-enters. Incrementing on entry and passing the result
	// down is the parameter form of a counter that used to live on a shared
	// struct, back when one held the whole compile.
	depth++
	if depth > maxSchemaDepth {
		return ts.PrimRef(ir.PrimAny), []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.DegradedConstruct, pointer,
			"schema nesting exceeds %d; lowered as any", maxSchemaDepth)}
	}
	if !c.NamesByReference() {
		// This is the declaration reaching its own coordinate, so its hint is the
		// node's name even when a $ref interned the node there first (GitHub #372).
		// It sits here rather than only at intern because a position whose node
		// already exists resolves to it and returns before interning anything.
		ts.NameFromDeclaration(string(pointer), hint)
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
// set, no value and no encoding of its own (GitHub #283). oneOf/anyOf and
// allOf are the same conjunction and reached none of that — the union's one
// keeper (preserveUnionSiblingsAt) and the census (refSiteUnhomedKeywords) were
// each reachable only from the structural-body path — so the alias narrows to
// them too now (GitHub #406).
//
// At an annotation.HomeCarrier position no alias is hoisted — a description or a
// bound beside a property's $ref belongs on the property (GitHub #114) — so the
// carrier keeps them there too, through PreserveRefSiteKeywords.
func refSiteRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema, pointer jsontext.Pointer, hint string, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
	target, diags := refTypeRef(c, ts, anchors, depth, js, pointer)
	unhomed := refSiteUnhomedKeywords(s, nil)
	hasUnion := declaresUnion(s)
	ref, homeDiags := homeDeclaration(c, ts, anchors, s, target, pointer, hint, home, len(unhomed) > 0 || hasUnion)
	diags = append(diags, homeDiags...)
	if home != annotation.HomeOwnNode {
		return ref, diags
	}
	diags = append(diags, recordUnhomedKeywords(c, ts, ref.Target, s, unhomed, refSiteShape, pointer)...)
	if hasUnion {
		diags = append(diags, preserveUnionSiblings(c, ts, ref.Target, s, pointer, ir.ReasonDegradedLowering, refSiteUnionWhy)...)
	}
	return ref, diags
}

// PreserveRefSiteKeywords keeps, on a carrier's own Unmodeled, the keywords a
// $ref position declared that the alias it resolves to cannot hold. It is
// refSiteRef's other half: the same census, recorded where an
// annotation.HomeCarrier position keeps everything else its schema declared —
// oneOf/anyOf/allOf included, through the same keepers refSiteRef calls
// (GitHub #406).
//
// A schema that lowered to a node of its own already had the census recorded
// there, so the carrier adds nothing — one home per declaration, exactly as
// fillPropertyAnnotations draws the line.
func PreserveRefSiteKeywords(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled,
	js *oas3.JSONSchema[oas3.Referenceable], t ir.TypeRef, pointer jsontext.Pointer,
) []ir.Diagnostic {
	// GetSchema is nil-safe and yields nil for a boolean schema too, so the one
	// check covers an absent position, a boolean one, and a caller passing nil.
	s := js.GetSchema()
	if s == nil || !resolve.IsRefSite(js, s) || LoweredToOwnNode(ts, pointer, t) {
		return nil
	}
	diags := recordUnhomedAt(c, p, s, refSiteUnhomedKeywords(s, nil), refSiteShape, pointer)
	if !declaresUnion(s) {
		return diags
	}
	return append(diags, preserveUnionSiblingsAt(c, p, s, pointer, ir.ReasonDegradedLowering, refSiteUnionWhy)...)
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
func homeDeclaration(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, s *oas3.Schema, target ir.TypeRef, pointer jsontext.Pointer, hint string, home annotation.Home, unhomed bool) (ir.TypeRef, []ir.Diagnostic) {
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
func refTypeRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
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
func hoistSubSchema(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, decl *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) (ir.TypeID, bool, []ir.Diagnostic) {
	s := annotation.At(decl)
	if s.Node == nil {
		return "", false, nil
	}
	// Everything lowered from here is reached through a $ref that named this
	// coordinate, not through the declaration that owns it, so the names minted
	// below are placeholders the declaration replaces (GitHub #372). It covers the
	// subtree rather than this coordinate alone: a reference to an object body
	// interns its children too, and their names hang off this one.
	c = c.NamingByReference()

	hint := subSchemaHint(decl, pointer)
	ref, diags := Ref(c, ts, anchors, depth, decl, pointer, hint)
	if owned, ok := ts.Lookup(string(pointer)); ok {
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

// subSchemaHint names the node a $ref'd sub-schema pointer owns: the hint the
// declaration that owns the position gives it, as positionHint replays it from
// the pointer.
//
// Both lowerings reach the pointer — the enclosing schema through its own body,
// this one through an outside $ref naming it — and only the first to arrive
// interns the node. Beneath /components/schemas the replay is exact, so the
// reference names the node and everything it interns beneath it as the
// declaration would. Elsewhere it is a placeholder the declaration replaces
// when it arrives, or, for a position no declaration ever lowers, the name
// itself (positionHint): compile.Types rebuilds the subtree a reference
// interned first (GitHub #529), and names a coordinate the declaration
// reached before any node existed there (GitHub #519).
//
// A composition branch that is itself a $ref is the one position whose
// declaration hint depends on more than the pointer: the composition names it
// after its target (branchHint), and so does this.
func subSchemaHint(decl *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) string {
	hint, branch := positionHint(pointer)
	if branch && decl != nil && decl.IsReference() {
		if name := targetHint(decl); name != "" {
			return name
		}
	}
	return hint
}

// componentSchemaTokens is the length of /components/schemas/<name>, the
// pointer positionHint roots a component's walk at: the component itself.
const componentSchemaTokens = 3

// positionHint returns the hint the structural lowering gives the schema
// position at pointer, and whether that position is a composition branch.
//
// It replays the lowering token by token, because the hint is composed rather
// than positional: items is compile.SubHint(enclosing, "item"), so answering
// needs the enclosing node's hint, and only a walk from the root knows which
// tokens are keywords. Reading the pointer from its tail instead took a
// property, a pattern or a $defs entry literally named items for the items
// keyword (GitHub #518), and a branch's ordinal for the enclosing hint of what
// sits beneath the branch, where the composition says variant_0.
//
// Beneath /components/schemas the walk starts at the component, whose hint is
// its name, so the replay is exact. Anywhere else it starts at the pointer's
// first token, because the hint a declaration there gives its schema — an
// operationId, a response, a media-type key — is not in the pointer. What it
// replays there is a placeholder for any position a declaration lowers, which
// the declaration replaces (see subSchemaHint), and the name itself for a
// position only references reach, such as one beneath not. Those have no
// declaration to settle them, so the name has to be a function of the pointer
// alone: two references into one such region, the outer one and one beneath
// it, then name the inner node alike in either order. The walk resets its hint
// at every token it does not know, so whatever precedes a schema — the
// operation, the response, a header's name — leaves the positions within it
// named by the steps inside the schema only (GitHub #529).
//
// Positions the lowering does not walk — not, if, then, else and the rest — are
// named after their own token, as the tail reading named them. The walk takes
// one token per step or two, so it is bounded by the pointer's length.
func positionHint(pointer jsontext.Pointer) (hint string, branch bool) {
	tokens := slices.Collect(pointer.Tokens())
	start := min(1, len(tokens))
	if len(tokens) >= componentSchemaTokens && tokens[0] == "components" && tokens[1] == "schemas" {
		start = componentSchemaTokens
	}
	if start > 0 {
		hint = tokens[start-1]
	}
	for i := start; i < len(tokens); {
		var step int
		hint, branch, step = positionStep(hint, tokens[i:])
		i += step
	}
	return hint, branch
}

// positionStep applies the keyword at rest[0] to the enclosing hint, returning
// the hint of the position it leads to, whether that position is a composition
// branch, and how many tokens the step consumed: two for a keyword whose
// children are keyed or indexed, one otherwise.
func positionStep(enclosing string, rest []string) (hint string, branch bool, step int) {
	keyword := rest[0]
	if role, ok := structuralRoles[keyword]; ok {
		return compile.SubHint(enclosing, role), false, 1
	}
	if len(rest) < 2 {
		return keyword, false, 1
	}
	child := rest[1]
	switch {
	case keyedSchemaMaps[keyword]:
		return child, false, 2
	case keyword == "patternProperties":
		return compile.SubHint(enclosing, "pattern"), false, 2
	case keyword == "prefixItems" && isDecimalIndex(child):
		return compile.SubHint(enclosing, child), false, 2
	case compositionKeywords[keyword] && isDecimalIndex(child):
		return positionalBranchHint(child), true, 2
	}
	return keyword, false, 1
}

// structuralRoles maps each keyword whose one schema the structural lowering
// names by role to that role: the suffixes its compile.SubHint call sites pass. A
// change to one of them has to be made here too, and
// TestInlinePosition_HintIsTheSameInBothOrders is what fails when they drift,
// provided its row aims the outside $ref above the node it asserts (see the
// refAt column there): a reference aimed at the node itself is renamed by the
// declaration in either order and cannot see a role missing here.
var structuralRoles = map[string]string{
	"items":                "item",
	"additionalProperties": "value",
	"contentSchema":        "content",
}

// keyedSchemaMaps are the keywords whose value maps a key to a schema that is
// named by the key alone: a property by its name, and the $defs, definitions,
// dependentSchemas and dependencies entries by theirs, as the pointer's last
// token always named them. patternProperties is keyed too but names by role, so
// positionStep takes it separately.
var keyedSchemaMaps = map[string]bool{
	"properties": true, "$defs": true, "definitions": true, "dependentSchemas": true, "dependencies": true,
}

// refNullable reports whether a $ref usage admits null: the reference site or
// its resolved target admits null in any spelling. It is schemaAdmitsNull read
// across the reference (refNullVerdict), so the two cannot answer one schema
// differently.
func refNullable(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	budget := maxNullConjuncts
	return refNullVerdict(js, &budget) == nullAdmitted
}
