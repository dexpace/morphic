package schema

import (
	"encoding/json/jsontext"

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
	// The hint this lowering names the position by stands for any node a $ref
	// later hoists here, where the position itself interned none (hoistSubSchema).
	c.DeclareHint(pointer, hint)
	if !annotation.Written(js) {
		// None, or a null, which states nothing: an entry written so — a
		// property, a branch, a component, items — lowers as the empty schema.
		// A keyword whose absence differs from the empty schema reads a null as
		// left out before it gets here (docs/compilers/openapi.md#null-schema).
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
		return refSiteRef(c, ts, anchors, js, schema, pointer, hint, home)
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
func refSiteRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema, pointer jsontext.Pointer, hint string, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
	target, diags := refTypeRef(c, ts, anchors, js, pointer)
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

// refTypeRef resolves a $ref position to its target's stable ID, carrying the
// combined ref-site and target nullability. A top-level component target keeps
// its stable named ID (lowered where it is defined); an internal sub-schema
// target is hoisted at its pointer-derived ID so the reference never dangles. A
// genuinely external or unresolvable target is diagnosed and dropped to any. A
// $ref on a loop of $refs is dropped to any too, with nothing of its own to
// report: the load phase reports the loop once.
func refTypeRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	ref := js.GetRef().String()
	id, ok := resolveSchemaRef(c, ts, anchors, js)
	if ok {
		return refNullRef(c, id, js, siteAt(pointer))
	}
	if onLoop(c, js) {
		return ts.PrimRef(ir.PrimAny), nil
	}
	return ts.PrimRef(ir.PrimAny), []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.UnresolvedRef, pointer,
		"unresolved $ref %q%s", ref, unresolvedWhy(c, js))}
}

// unresolvedWhy is the reason the $ref at js reports when it resolves to no
// type here: why its own hop lands on no schema (bind.Hop.Why), or why the
// table never read it (unreadWhy). It is empty for any other, a target
// declaring nothing to lower. The position may well exist, so without it the
// report reads as though it named nothing.
func unresolvedWhy(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) string {
	if e, ok := c.Bindings().Entry(js); ok {
		return hopWhy(e.Hop)
	}
	return unreadWhy(c.Bindings(), js)
}

// unreadWhy is why the table holds nothing for model, a schema or a
// discriminator: it lies in what a reference object resolved to where the
// table could not place it (typed.Bindings.Unplaced), or the budget or the
// merge bound stopped the reading (bind.Table.Unread).
func unreadWhy(refs *typed.Bindings, model any) string {
	if site, ok := refs.Unplaced(model); ok {
		return ": it is in what the reference at " + site + " resolved to, which the compiler " +
			"cannot read where the parser placed it"
	}
	if why := refs.Table().Unread(); why != "" {
		return ": " + why
	}
	return ""
}

// hopWhy is unresolvedWhy's reason for the hop h, or nothing for one that
// resolved. It names no document: a finding about a reference is placed in
// the document that writes it.
func hopWhy(h bind.Hop) string {
	if h.Status == bind.Resolved {
		return ""
	}
	return ": " + h.Why()
}

// resolveSchemaRef resolves a schema-position $ref to the TypeID of what it
// names, in whichever document (resolveAt). It returns ok=false for a $ref that
// lands on no schema (resolve.Scope.Target) and for a reference to an
// undeclared component.
func resolveSchemaRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, js *oas3.JSONSchema[oas3.Referenceable]) (ir.TypeID, bool) {
	at, ok := c.RefScope().Target(js)
	if !ok {
		return "", false
	}
	return resolveAt(c, ts, anchors, at, c.Bindings().Declared(js))
}

// resolveAt is resolvePointer at a position of the bindings' table, read at
// its own document's coordinates (lowering.InDocument).
func resolveAt(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, at bind.Position, decl *oas3.JSONSchema[oas3.Referenceable]) (ir.TypeID, bool) {
	c, ok := lowering.InDocument(c, at.Doc)
	if !ok {
		return "", false
	}
	return resolvePointer(c, ts, anchors, at.Pointer, decl)
}

// resolvePointer resolves a pointer in c's document to the ID of the schema it
// addresses: a source component by its stable ID, any other position by the ID
// its pointer derives there. A reference never builds what it names, so no node
// is built in a reference's context: it promises the node
// (compile.Types.Promise), which hoistSubSchema builds from decl, the schema
// the bindings hold there, only where no declaration interned one. A decl it
// cannot hoist resolves to nothing.
//
// A $ref and a discriminator mapping target meet here once each has its
// pointer, so they cannot drift apart.
func resolvePointer(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, pointer jsontext.Pointer, decl *oas3.JSONSchema[oas3.Referenceable]) (ir.TypeID, bool) {
	// The source's components alone are lowered whole and named by name;
	// another document's are hoisted where a reference reaches them, as any
	// other position of it is.
	if c.IDs() == (ids.Document{}) {
		if id, resolved, handled := c.RefScope().ComponentRef(pointer); handled {
			return id, resolved
		}
	}
	if !resolve.Hoistable(decl) {
		return "", false
	}
	ts.Promise(c.IDs().Coordinate(pointer), func() []ir.Diagnostic { return hoistSubSchema(c, ts, anchors, decl, pointer) })
	return c.IDs().ForPointer(pointer), true
}

// hoistSubSchema builds the node at pointer from decl, the schema written there,
// where a $ref names a position no declaration interned a node at, once every
// declaration has lowered (compile.Types.Finish). It aliases where the body
// reduces to a shared target, so a $ref always resolves (invariants 1, 2), a
// boolean schema included (GitHub #861), keeping the position's constraints
// and examples.
//
// It lowers in the position's own context, whichever reference promised it:
// from TopLevelDepth, as a walk of its own, in decl's document, and under the
// hint the position's lowering recorded, else subSchemaHint. A $ref with
// siblings is aliased, keeping them.
func hoistSubSchema(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, decl *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) []ir.Diagnostic {
	s := annotation.At(decl)
	// c reads the document that declares pointer already (resolveAt), which is
	// where the bindings place decl.
	hint, declared := c.HintAt(pointer)
	if !declared {
		hint = subSchemaHint(c.Bindings(), decl, pointer)
	}
	ref, diags := Ref(c, ts, anchors, TopLevelDepth, decl, pointer, hint)
	if _, ok := ts.Lookup(c.IDs().Coordinate(pointer)); ok {
		return diags
	}
	_, aliasDiags := internAlias(c, ts, s.Node, pointer, hint, ref)
	diags = append(diags, aliasDiags...)
	// As in lowerComponentSchema: this alias is the first node the pointer owns,
	// so the annotations Ref had nowhere to put now have a home.
	return append(diags, attachDeclaredAnnotations(c, ts, anchors, s.Node, pointer)...)
}

// onLoop reports whether the $ref at b lies on a loop of $refs, which the load
// phase reports once and the lowering drops to any without a report of its
// own.
func onLoop(c lowering.Ctx, b *oas3.JSONSchema[oas3.Referenceable]) bool {
	e, ok := c.Bindings().Entry(b)
	return ok && e.Chain.Member
}

// refSiteHomesNothing reports whether a $ref position written as s holds
// nothing an alias would: no keyword beside the $ref that refSiteRef keeps on
// one (refSiteUnhomedKeywords, declaresUnion), and none hoistDeclarationHome
// homes on one (declaresPositionScoped). Such a position lowers to its
// target's type alone.
func refSiteHomesNothing(s *oas3.Schema) bool {
	return len(refSiteUnhomedKeywords(s, nil)) == 0 && !declaresUnion(s) && !declaresPositionScoped(s)
}
