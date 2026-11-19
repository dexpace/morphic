// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// refSiteRef resolves a $ref position, a schema writing its reference beside
// nothing it reads but what a reference leaves silent: any key read beside a
// $ref makes a conjunction (statement.Table), lowered as its allOf spelling
// is, and an allOf entry whose reference names nothing, read whole, is no
// position a reference reaches, as one to it reads on through its own. So
// such a site writes nothing beside its target for a node to keep.
func refSiteRef(c lowering.Ctx, ts *compile.Types, js *statement.Statement, s *statement.Schema, pointer jsontext.Pointer, home annotation.Home,
) (ir.TypeRef, []ir.Diagnostic) {
	target, diags := refTypeRef(c, ts, js, pointer)
	ref, homeDiags := homeDeclaration(c, ts, s, target, pointer, home, false, nil)
	return ref, append(diags, homeDiags...)
}

// PreserveRefSiteKeywords keeps, on a carrier's own Unmodeled, the keywords a
// position lowered to a reference as a $ref site is declared that the reference
// cannot hold (referenceOutcomeKeywords): a collapsed union, an expanded
// $dynamicRef and a refinement. It is schemaBody's other half, the same unread
// keeping recorded where an annotation.HomeCarrier position keeps everything
// else its schema declared (GitHub #406). A $ref site writes nothing to keep
// (refSiteRef), and a schema that lowered to a node of its own had its unread
// keywords recorded there, so the carrier adds nothing: one home per
// declaration, as fillPropertyAnnotations draws the line.
func PreserveRefSiteKeywords(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled,
	js *statement.Statement, t ir.TypeRef, pointer jsontext.Pointer,
) []ir.Diagnostic {
	// The one check covers an absent position, a boolean one, a $ref site and a
	// caller passing nil.
	if js == nil || !stated(js.Schema) || js.Ref != nil || LoweredToOwnNode(c, pointer, t) {
		return nil
	}
	return keepsAtOutcome(c, js.Schema, t, pointer).record(c, ts, p, js.Schema, pointer, nil, nil)
}

// refTypeRef resolves a $ref position to its target's stable ID, carrying the
// combined ref-site and target nullability. A top-level component target keeps
// its stable named ID (lowered where it is defined); a sub-schema target, in
// whichever document it lies, is hoisted at its pointer-derived ID so the
// reference never dangles. A target in no document this compilation read, or
// one landing on no schema, is diagnosed and dropped to any. A $ref on a loop of $refs is
// dropped to any too, with nothing of its own to report: the load phase reports
// the loop once.
func refTypeRef(c lowering.Ctx, ts *compile.Types, js *statement.Statement, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	id, ok := resolveSchemaRef(c, ts, js)
	if ok {
		return refNullRef(c, id, js, siteAt(pointer))
	}
	if onLoop(js) {
		return ts.PrimRef(ir.PrimAny), nil
	}
	keyword, ref := refSpelling(js)
	return ts.PrimRef(ir.PrimAny), []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.UnresolvedRef, pointer,
		"unresolved %s %q%s", keyword, ref, unresolvedWhy(c, js))}
}

// refSpelling is the keyword and the value of the reference js writes, as the
// load phase reports one landing nowhere: the table's entry's, else the
// model's $ref, which the budget left unread.
func refSpelling(js *statement.Statement) (keyword, value string) {
	switch {
	case js.Ref == nil:
		return "$ref", ""
	case js.Ref.Bound:
		return js.Ref.Entry.Keyword, js.Ref.Entry.Hop.Ref
	default:
		return "$ref", js.Ref.Text
	}
}

// unresolvedWhy is the reason the $ref at js reports when it resolves to no
// type here: why its own hop lands on no schema (bind.Hop.Why), or why the
// table never read it (unreadWhy). It is empty for any other, a target
// declaring nothing to lower. The position may well exist, so without it the
// report reads as though it named nothing.
func unresolvedWhy(c lowering.Ctx, js *statement.Statement) string {
	if js.Ref != nil && js.Ref.Bound {
		return hopWhy(js.Ref.Entry.Hop)
	}
	return unreadWhy(c.Bindings())
}

// unreadWhy is why the table holds nothing for a schema or a discriminator it
// read: the budget or the merge bound stopped the reading (bind.Table.Unread).
func unreadWhy(refs *typed.Bindings) string {
	if why := refs.Table().Unread(); why != "" {
		return ": " + why
	}
	return ""
}

// hopWhy is the reason unresolvedWhy and mappingWhy give for the hop h, or
// nothing for one that resolved. It names no document: a finding about a
// reference is placed in the document that writes it.
func hopWhy(h bind.Hop) string {
	if h.Status == bind.Resolved {
		return ""
	}
	return ": " + h.Why()
}

// resolveSchemaRef resolves a schema-position $ref to the TypeID of what it
// names, in whichever document (resolveAt). It returns ok=false for a $ref that
// lands on no schema (landsAt) and for a reference to an
// undeclared component.
func resolveSchemaRef(c lowering.Ctx, ts *compile.Types, js *statement.Statement) (ir.TypeID, bool) {
	at, ok := landsAt(js)
	if !ok {
		return "", false
	}
	decl, _ := declaredOf(c, js)
	return resolveAt(c, ts, at, decl)
}

// resolveAt is resolvePointer at a position of the bindings' table, read at
// its own document's coordinates (lowering.InDocument).
func resolveAt(c lowering.Ctx, ts *compile.Types, at bind.Position, decl *statement.Statement) (ir.TypeID, bool) {
	c, ok := lowering.InDocument(c, at.Doc)
	if !ok {
		return "", false
	}
	return resolvePointer(c, ts, at.Pointer, decl)
}

// PromiseLandings promises a node at each position a schema $ref of the
// bindings' table lands on, hop by hop, so a position only a keyword kept
// verbatim names, a `not` or an `if`, or only an alias a chain passes, is
// lowered as one a lowered keyword names is, where its hop lands
// (bind.Entry.Lands): a member of a loop of $refs lands nowhere. A position a lowering already
// reached keeps the node built there (compile.Types.Promise). It reports
// nothing itself: a promise reports when it is kept.
func PromiseLandings(c lowering.Ctx, ts *compile.Types) {
	b := c.Bindings()
	for _, e := range b.Table().Entries() {
		if !e.Lands() {
			continue
		}
		resolveAt(c, ts, e.Hop.Target, c.Statements().At(e.Hop.Target))
	}
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
func resolvePointer(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, decl *statement.Statement) (ir.TypeID, bool) {
	id, hoisted, ok := declaredIn(c, pointer, decl)
	if hoisted {
		ts.Promise(c.IDs().Coordinate(pointer), func() []ir.Diagnostic { return hoistSubSchema(c, ts, decl, pointer) })
	}
	return id, ok
}

// declaredIn returns the ID of the type declared at pointer in c's document,
// holding decl, as resolvePointer resolves it, and whether it is a position
// hoisted where a reference reaches it, which a reference promises a node. It
// reads no registry, so no answer turns on which schema lowered first.
func declaredIn(c lowering.Ctx, pointer jsontext.Pointer, decl *statement.Statement) (id ir.TypeID, hoisted, ok bool) {
	// The source's components alone are lowered whole and named by name;
	// another document's are hoisted where a reference reaches them, as any
	// other position of it is.
	if c.IDs() == (ids.Document{}) {
		if named, state := c.RefScope().ComponentRef(pointer); state.ByName() {
			return named, false, state == ids.NamedComponent
		}
	}
	// Any value the model holds is hoisted: a body, a boolean one (GitHub
	// #861), and one written as no schema, which a $ref names as it names `{}`
	// (docs/compilers/openapi.md#null-schema).
	if decl == nil || !decl.Held() {
		return "", false, false
	}
	return c.IDs().ForPointer(pointer), true, true
}

// resolvesAt reports whether a reference to at, a position of the bindings'
// table, resolves, as resolveAt resolves one, without promising its node
// (declaredAt).
func resolvesAt(c lowering.Ctx, at bind.Position) bool {
	_, ok := declaredAt(c, at, c.Statements().At(at))
	return ok
}

// namesReferent reports whether the reference st writes resolves, as
// resolveSchemaRef resolves it, without promising its node.
func namesReferent(c lowering.Ctx, st *statement.Statement) bool {
	at, ok := landsAt(st)
	return ok && resolvesAt(c, at)
}

// hoistSubSchema builds the node at pointer from decl, the schema written there,
// where a $ref names a position no declaration interned a node at, once every
// declaration has lowered (compile.Types.Finish). It aliases where the body
// reduces to a shared target, so a $ref always resolves (invariants 1, 2), a
// boolean schema included (GitHub #861), keeping the position's constraints
// and examples.
//
// It lowers in the position's own context, whichever reference promised it:
// from TopLevelDepth, as a walk of its own, in decl's document. A $ref with
// siblings is aliased, keeping them.
func hoistSubSchema(c lowering.Ctx, ts *compile.Types, decl *statement.Statement, pointer jsontext.Pointer) []ir.Diagnostic {
	s := annotation.At(decl)
	// c reads the document that declares pointer already (resolveAt), which is
	// where the bindings place decl.
	ref, diags := Ref(c, ts, TopLevelDepth, decl, pointer)
	if LoweredToOwnNode(c, pointer, ref) {
		return diags
	}
	// As in lowerComponentSchema: this node is the first the pointer owns, so
	// the annotations Ref had nowhere to put now have a home, which its build
	// gives them.
	var annotated finished
	diags = append(diags, internOwnNode(c, ts, decl, pointer, ref, annotated.on(func(td ir.TypeDef) []ir.Diagnostic {
		return attachDeclaredAnnotations(c, ts, s.Node, pointer, ref, td)
	}))...)
	return append(diags, annotated.diags...)
}

// onLoop reports whether the $ref at b lies on a loop of $refs, which the load
// phase reports once and the lowering drops to any without a report of its
// own.
func onLoop(b *statement.Statement) bool {
	return b.Ref != nil && b.Ref.Bound && b.Ref.Entry.Chain.Member
}
