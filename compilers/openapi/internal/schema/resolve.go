package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
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
		ts.NameFromDeclaration(c.IDs().Coordinate(pointer), hint)
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
// $ref. In JSON Schema 2020-12 those siblings are conjoined with the $ref, so
// they bind the position, not the referent, and cannot go on the target's
// node; an alias over the target becomes their home. The alias carries the
// position's constraints and annotations, and keeps verbatim the census
// keywords, allOf and oneOf/anyOf written beside the $ref, because it has no
// property set, member set, value or encoding of its own (GitHub #283, #406).
//
// At an annotation.HomeCarrier position no alias is hoisted: the carrier keeps
// them through PreserveRefSiteKeywords (GitHub #114).
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
// $ref position declared that the alias it resolves to cannot hold, and those a
// collapsed union declared, which resolves to a reference the same way. It is
// refSiteRef's and schemaBody's other half: the same census, recorded where an
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
	if s == nil || LoweredToOwnNode(c, ts, pointer, t) {
		return nil
	}
	if !resolve.IsRefSite(js, s) {
		// A collapsed union lowers to a reference just as a $ref site does.
		return recordUnhomedAt(c, p, s, collapsedUnhomedKeywords(s), collapsedShape, pointer)
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
// genuinely external or unresolvable target is diagnosed and dropped to any. A
// $ref on a loop of $refs is dropped to any too, with nothing of its own to
// report: the load phase reports the loop once.
func refTypeRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	ref := js.GetRef().String()
	id, ok, diags := resolveSchemaRef(c, ts, anchors, depth, js)
	if ok {
		return ir.TypeRef{Target: id, Nullable: refNullable(c, js)}, diags
	}
	if onLoop(c, js) {
		return ts.PrimRef(ir.PrimAny), diags
	}
	return ts.PrimRef(ir.PrimAny), append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef, pointer,
		"unresolved $ref %q%s", ref, unresolvedWhy(c, js)))
}

// unresolvedWhy is the reason the $ref at js reports when it resolves to no
// type here: why its own hop lands on no schema (bind.Hop.Why), or why the
// table never read it (unreadWhy). It is empty for any other, a target
// declaring nothing to lower. The position may well exist, so without it the
// report reads as though it named nothing.
func unresolvedWhy(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) string {
	if e, ok := c.Bindings().Entry(js); ok {
		return hopWhy(c.Bindings().Table(), e.Hop, e.Site)
	}
	return unreadWhy(c.Bindings(), js)
}

// unreadWhy is why the table holds nothing for model, a schema or a
// discriminator: it lies in what a reference object resolved to where the
// table could not place it (typed.Bindings.Unplaced), or the budget or the
// merge bound stopped the reading (bind.Table.Unread).
func unreadWhy(refs *typed.Bindings, model any) string {
	if site, ok := refs.Unplaced(model); ok {
		return ": it is in what the reference at " + string(site) + " resolved to, which the compiler " +
			"cannot read where the parser placed it"
	}
	if why := refs.Table().Unread(); why != "" {
		return ": " + why
	}
	return ""
}

// hopWhy is unresolvedWhy's reason for the hop h of a reference written at
// site. A failure in another document's content names that document, since
// the use site in the source says nothing of it.
func hopWhy(table *bind.Table, h bind.Hop, site bind.Position) string {
	switch {
	case h.Status == bind.Resolved:
		return ""
	case site.Doc != bind.Source:
		return ": in " + table.Document(site.Doc).Name + ", " + h.Why()
	default:
		return ": " + h.Why()
	}
}

// resolveSchemaRef resolves a schema-position $ref to an interned TypeID, never
// synthesizing an ID that nothing backs. A top-level component keeps its stable
// named ID; an already-interned target reuses it; any other schema the $ref
// lands on, in whichever document, is hoisted at its pointer-derived ID there.
// It returns ok=false for a $ref that lands on no schema
// (resolve.Scope.Target) and for a reference to an undeclared component.
func resolveSchemaRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, js *oas3.JSONSchema[oas3.Referenceable]) (ir.TypeID, bool, []ir.Diagnostic) {
	at, ok := c.RefScope().Target(js)
	if !ok {
		return "", false, nil
	}
	return resolveAt(c, ts, anchors, depth, at, c.Bindings().Declared(js))
}

// resolveAt is resolvePointer at a position of the bindings' table, read at
// its own document's coordinates (lowering.InDocument).
func resolveAt(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, at bind.Position, decl *oas3.JSONSchema[oas3.Referenceable]) (ir.TypeID, bool, []ir.Diagnostic) {
	c, ok := lowering.InDocument(c, at.Doc)
	if !ok {
		return "", false, nil
	}
	return resolvePointer(c, ts, anchors, depth, at.Pointer, decl)
}

// resolvePointer resolves a same-document pointer to the ID of the schema it
// addresses: a component by its stable ID, an interned node by its own, and
// otherwise decl, the schema the bindings hold there, hoisted at the pointer.
// A nil decl declares nothing to hoist.
//
// It is where a $ref and a discriminator mapping target meet once each has its
// pointer: they differ only in how decl is found, so they cannot drift apart.
func resolvePointer(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, pointer jsontext.Pointer, decl *oas3.JSONSchema[oas3.Referenceable]) (ir.TypeID, bool, []ir.Diagnostic) {
	// The source's components alone are lowered whole and named by name;
	// another document's are hoisted where a reference reaches them, as any
	// other position of it is.
	if c.IDs() == (ids.Document{}) {
		if id, resolved, handled := c.RefScope().ComponentRef(pointer); handled {
			return id, resolved, nil
		}
	}
	if id, ok := resolve.InternedID(ts, c.IDs(), pointer); ok {
		return id, true, nil
	}
	if decl == nil {
		return "", false, nil
	}
	return hoistSubSchema(c, ts, anchors, depth, decl, pointer)
}

// hoistSubSchema lowers the internal sub-schema declared at pointer and
// guarantees a node exists at its pointer-derived ID, aliasing when the body
// reduces to a shared target, so a $ref to the sub-schema always resolves
// (invariants 1, 2), a boolean one included (GitHub #861). The alias carries
// the position's value constraints and examples, as a named scalar component
// does, so a $ref to {type: number, minimum: 5} keeps them.
//
// decl is the declaration itself, not its resolved form, so a sub-schema that
// is a $ref carrying siblings aliases its target while keeping them.
func hoistSubSchema(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, decl *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) (ir.TypeID, bool, []ir.Diagnostic) {
	if !resolve.Hoistable(decl) {
		return "", false, nil
	}
	s := annotation.At(decl)
	// Everything lowered from here is reached through a $ref that named this
	// coordinate, not through the declaration that owns it, so the names minted
	// below are placeholders the declaration replaces (GitHub #372). It covers the
	// subtree rather than this coordinate alone: a reference to an object body
	// interns its children too, and their names hang off this one. It is read
	// where the bindings place decl, whichever document's content held the
	// reference: the source's own, or another document's past a $ref the pointer
	// passes.
	c = c.InScope(c.RefScope().InDocumentOf(decl)).NamingByReference()

	hint := subSchemaHint(c.Bindings(), decl, pointer)
	ref, diags := Ref(c, ts, anchors, depth, decl, pointer, hint)
	if owned, ok := ts.Lookup(c.IDs().Coordinate(pointer)); ok {
		return owned, true, diags
	}
	id, aliasDiags := internAlias(c, ts, s.Node, pointer, hint, ref)
	diags = append(diags, aliasDiags...)
	// As in lowerComponentSchema: this alias is the first node the pointer owns,
	// so the annotations Ref had nowhere to put now have a home.
	return id, true, append(diags, attachDeclaredAnnotations(c, ts, anchors, s.Node, pointer)...)
}

// subSchemaHint names the node a $ref'd sub-schema pointer owns: the hint the
// position takes for where it is (ownHint), or, at a position that takes its
// target's name, the hint of the node its own $ref resolves to (targetHint).
//
// The enclosing schema's own body and an outside $ref both lower this pointer,
// and only the first to arrive interns the node. Beneath /components/schemas
// positionHint replays the declaration's hint exactly; elsewhere the reference's
// names are placeholders that InternDeclared replaces when the declaration
// arrives.
func subSchemaHint(refs *typed.Bindings, decl *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) string {
	hint, follow := ownHint(decl, pointer)
	if !follow {
		return hint
	}
	return cmp.Or(targetHint(refs, decl), hint)
}

// ownHint returns the hint the position at pointer takes for where it is, and
// whether a $ref written there names it after its target instead. decl is the
// schema written there, if known.
//
// Only a composition branch holding a $ref is named after its target, as the
// composition names it (branchHint); every other position is named for where it
// is, as positionHint replays it.
func ownHint(decl *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) (hint string, follow bool) {
	hint, branch := positionHint(pointer)
	return hint, branch && decl != nil && decl.IsReference()
}

// componentSchemaTokens is the length of /components/schemas/<name>, the
// pointer positionHint roots a component's walk at: the component itself.
const componentSchemaTokens = 3

// positionHint returns the hint the structural lowering gives the schema
// position at pointer, and whether it is a composition branch.
//
// It replays the lowering because the hint is composed: items is
// compile.SubHint(enclosing, "item"), so only a walk from the root tells the
// keyword from a property named items (GitHub #518). Beneath /components/schemas
// the replay is exact. Elsewhere the enclosing hint is not in the pointer, so
// the walk resets at every token it does not know: a placeholder the declaration
// replaces, or the name itself for a position only references reach (GitHub
// #529). Each step consumes a token, bounding the walk.
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
// children are keyed or indexed, one otherwise. A keyword the lowering does not
// walk (not, if, then, else, an unknown one) is named after its own token: only
// a reference reaches it, so any stable spelling keeps its name independent of
// declaration order.
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
func refNullable(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return newNulls(c.Bindings()).refVerdict(js) == nullAdmitted
}
