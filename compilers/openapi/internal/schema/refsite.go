// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"maps"
	"slices"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// refSiteRef resolves a $ref position, a schema writing its reference beside
// nothing it reads but what a reference leaves silent: any key read beside a
// $ref makes a conjunction (statement.MemoizingSchemaReader), lowered as its
// allOf spelling is, and an allOf entry whose reference names nothing, read
// whole, is no position a reference reaches, as one to it reads on through its
// own. So such a site writes nothing beside its target for a node to keep.
func refSiteRef(loweringContext lowering.Ctx, ts *compile.Types, js *statement.Statement, schema *statement.Schema, pointer jsontext.Pointer, home annotation.Home,
) (ir.TypeRef, []ir.Diagnostic) {
	target, diags := refTypeRef(loweringContext, ts, js, pointer)
	ref, homeDiags := homeDeclaration(loweringContext, ts, schema, target, pointer, home, false, nil)
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
func PreserveRefSiteKeywords(loweringContext lowering.Ctx, ts *compile.Types, unmodeled *ir.Unmodeled,
	js *statement.Statement, typeRef ir.TypeRef, pointer jsontext.Pointer,
) []ir.Diagnostic {
	// The one check covers an absent position, a boolean one, a $ref site and a
	// caller passing nil.
	if js == nil || !stated(js.Schema) || js.Ref != nil || LoweredToOwnNode(loweringContext, pointer, typeRef) {
		return nil
	}
	return keepsAtOutcome(loweringContext, js.Schema, typeRef, pointer).record(loweringContext, ts, unmodeled, js.Schema, pointer, nil, nil)
}

// refTypeRef resolves a $ref position to its target's stable ID, carrying the
// combined ref-site and target nullability. A top-level component target keeps
// its stable named ID (lowered where it is defined); a sub-schema target, in
// whichever document it lies, is hoisted at its pointer-derived ID so the
// reference never dangles. A target in no document this compilation read, or
// one landing on no schema, is diagnosed and dropped to any. A $ref on a loop of $refs is
// dropped to any too, with nothing of its own to report: the load phase reports
// the loop once.
func refTypeRef(loweringContext lowering.Ctx, ts *compile.Types, js *statement.Statement, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	id, ok := resolveSchemaRef(loweringContext, ts, js)
	if ok {
		return refNullRef(loweringContext, id, js, siteAt(pointer))
	}
	if onLoop(js) {
		return ts.PrimitiveRef(ir.PrimitiveAny), nil
	}
	keyword, ref := refSpelling(js)
	return ts.PrimitiveRef(ir.PrimitiveAny), []ir.Diagnostic{loweringContext.DiagAt(ir.SeverityError, diag.UnresolvedRef, pointer,
		"unresolved %s %q%s", keyword, ref, unresolvedWhy(loweringContext, js))}
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
// type here: why its own hop lands on no schema (reftable.Hop.Why), or why the
// table never read it (unreadWhy). It is empty for any other, a target
// declaring nothing to lower. The position may well exist, so without it the
// report reads as though it named nothing.
func unresolvedWhy(loweringContext lowering.Ctx, js *statement.Statement) string {
	if js.Ref != nil && js.Ref.Bound {
		return hopWhy(js.Ref.Entry.Hop)
	}
	return unreadWhy(loweringContext.Bindings())
}

// unreadWhy is why the table holds nothing for a schema or a discriminator it
// read: the budget or the merge bound stopped the reading
// (reftable.ReferenceTargetTable.Unread).
func unreadWhy(refs *typed.Bindings) string {
	if why := refs.Table().Unread(); why != "" {
		return ": " + why
	}
	return ""
}

// hopWhy is the reason unresolvedWhy and mappingWhy give for the hop hop, or
// nothing for one that resolved. It names no document: a finding about a
// reference is placed in the document that writes it.
func hopWhy(hop reftable.Hop) string {
	if hop.Status == reftable.Resolved {
		return ""
	}
	return ": " + hop.Why()
}

// resolveSchemaRef resolves a schema-position $ref to the TypeID of what it
// names, in whichever document (resolveAt). It returns ok=false for a $ref that
// lands on no schema (landsAt) and for a reference to an
// undeclared component.
func resolveSchemaRef(loweringContext lowering.Ctx, ts *compile.Types, js *statement.Statement) (ir.TypeID, bool) {
	at, ok := landsAt(js)
	if !ok {
		return "", false
	}
	decl, _ := declaredOf(loweringContext, js)
	return resolveAt(loweringContext, ts, at, decl)
}

// resolveAt is resolvePointer at a position of the bindings' table, read at
// its own document's coordinates (lowering.InDocument).
func resolveAt(loweringContext lowering.Ctx, ts *compile.Types, at reftable.DocumentPointer, decl *statement.Statement) (ir.TypeID, bool) {
	loweringContext, ok := lowering.InDocument(loweringContext, at.Doc)
	if !ok {
		return "", false
	}
	return resolvePointer(loweringContext, ts, at.Pointer, decl)
}

// runTrackers holds run-scoped tracking state for post-lowering attachment and
// seeded pruning: kept verbatim discriminator routes, explicitly reached schema
// positions (hoisted targets and external schemas), and TypeIDs explicitly
// superseded or dropped during lowering and settlement.
type runTrackers struct {
	keptDiscRoutes map[LandingSite][]Landing
	reachedSchemas map[reftable.DocumentPointer]bool
	discarded      map[ir.TypeID]bool
}

// runTrackersOf returns the run's runTrackers store.
func runTrackersOf(loweringContext lowering.Ctx) *runTrackers {
	return lowering.Settled(loweringContext, func() *runTrackers {
		return &runTrackers{
			keptDiscRoutes: map[LandingSite][]Landing{},
			reachedSchemas: map[reftable.DocumentPointer]bool{},
			discarded:      map[ir.TypeID]bool{},
		}
	})
}

// recordReachedSchema marks pos as a reached position for this compile.
func recordReachedSchema(loweringContext lowering.Ctx, pos reftable.DocumentPointer) {
	runTrackersOf(loweringContext).reachedSchemas[pos] = true
}

// RecordReached marks pointer in loweringContext's document as a position
// preserved verbatim by lowering (such as a kept encoding, callback, parameter,
// or content entry) so ReserveNodesAtReferenceTargets reserves schema $ref
// targets reachable beneath it without consulting unvisited components.
func RecordReached(loweringContext lowering.Ctx, pointer jsontext.Pointer) {
	recordReachedSchema(loweringContext, loweringContext.Position(pointer))
}

// recordDiscarded records id as explicitly superseded or dropped during this
// compile so post-settlement pruning removes only seeded unreachable anonymous
// nodes and leaves any accidental orphan for irverify to catch.
func recordDiscarded(loweringContext lowering.Ctx, id ir.TypeID) {
	runTrackersOf(loweringContext).discarded[id] = true
}

// DiscardedTypes returns the set of TypeIDs explicitly superseded or dropped
// during lowering and settlement. Call after compile.Types.Finish.
func DiscardedTypes(loweringContext lowering.Ctx) map[ir.TypeID]bool {
	return maps.Clone(runTrackersOf(loweringContext).discarded)
}

// ReserveNodesAtReferenceTargets promises a node at each position a reachable
// schema $ref of the bindings' table lands on, hop by hop, so positions named
// only by verbatim keywords (`not`, `if`) or alias chains lower where their
// hops land (reftable.ReferenceSite.Lands). Reachability starts from positions
// visited or kept verbatim by lowering and propagates along a linear worklist
// without entering unreferenced $defs or definitions tables, so unvisited
// reusable components and unreferenced definitions never mint orphan nodes.
func ReserveNodesAtReferenceTargets(loweringContext lowering.Ctx, ts *compile.Types) {
	table := loweringContext.Bindings().Table()
	var sites []reftable.ReferenceSite
	for _, entry := range table.Entries() {
		if entry.Lands() {
			sites = append(sites, entry)
		}
	}
	if len(sites) == 0 {
		return
	}
	byAncestor := indexSitesByAncestor(table, sites)
	visited := maps.Clone(runTrackersOf(loweringContext).reachedSchemas)
	worklist := make([]reftable.DocumentPointer, 0, len(visited))
	for pos := range visited {
		worklist = append(worklist, pos)
	}
	slices.SortFunc(worklist, reftable.PositionOrder(table))
	reserved := make([]bool, len(sites))
	for head := 0; head < len(worklist); head++ {
		for _, i := range byAncestor[worklist[head]] {
			if reserved[i] {
				continue
			}
			reserved[i] = true
			target := sites[i].Hop.Target
			resolveAt(loweringContext, ts, target, loweringContext.Statements().At(target))
			if !visited[target] {
				visited[target] = true
				worklist = append(worklist, target)
			}
		}
	}
}

// indexSitesByAncestor maps each ancestor DocumentPointer at or beneath a
// site's definitions ceiling (reftable.DefinitionsCeiling) to the indices in
// sites that reaching that ancestor activates.
func indexSitesByAncestor(table *reftable.ReferenceTargetTable, sites []reftable.ReferenceSite) map[reftable.DocumentPointer][]int {
	byAncestor := make(map[reftable.DocumentPointer][]int, len(sites))
	for i, site := range sites {
		ceil := reftable.DefinitionsCeiling(table, site.Site)
		for ptr := site.Site.Pointer; ; ptr = ptr.Parent() {
			pos := reftable.DocumentPointer{Doc: site.Site.Doc, Pointer: ptr}
			byAncestor[pos] = append(byAncestor[pos], i)
			if ptr == ceil {
				break
			}
		}
	}
	return byAncestor
}

// LandingSite locates a source construct carrying type references: the input
// document's index in Document.Sources and the JSON Pointer within it.
type LandingSite struct {
	Source  int
	Pointer jsontext.Pointer
}

// Landing is one resolved type target written at a LandingSite: ID is the
// lowered ir.TypeID, Path is the RFC 6901 JSON Pointer from LandingSite.Pointer
// to the reference string (such as "/$ref", "/mapping/cat", "/defaultMapping"),
// and Ref is the reference text for a schema $ref/$dynamicRef/$recursiveRef
// (empty for a kept discriminator route).
type Landing struct {
	ID   ir.TypeID
	Path jsontext.Pointer
	Ref  string
}

// recordKeptDiscriminatorRoutes saves every type ID resolved for a verbatim
// discriminator at pointer so post-settlement attachment records them on its
// UnmodeledEntry.
func recordKeptDiscriminatorRoutes(loweringContext lowering.Ctx, pointer jsontext.Pointer, mapping map[string]ir.TypeID, def ir.TypeID) {
	if len(mapping) == 0 && def == "" {
		return
	}
	site := LandingSite{Source: loweringContext.Place().Source, Pointer: pointer}
	routes := runTrackersOf(loweringContext).keptDiscRoutes
	for _, tag := range slices.Sorted(maps.Keys(mapping)) {
		routes[site] = append(routes[site], Landing{ID: mapping[tag], Path: ids.Ptr("mapping", tag)})
	}
	if def != "" {
		routes[site] = append(routes[site], Landing{ID: def, Path: ids.Ptr("defaultMapping")})
	}
}

// ReferenceLandings returns every resolved schema $ref landing in the run's
// bindings table together with every kept discriminator's resolved routes,
// keyed by the writing position's LandingSite. Call it after
// compile.Types.Finish so every deferred route is recorded.
func ReferenceLandings(loweringContext lowering.Ctx) map[LandingSite][]Landing {
	out := map[LandingSite][]Landing{}
	for _, referenceSite := range loweringContext.Bindings().Table().Entries() {
		if !referenceSite.Lands() {
			continue
		}
		fromCtx, fromOK := lowering.InDocument(loweringContext, referenceSite.Site.Doc)
		targetCtx, targetOK := lowering.InDocument(loweringContext, referenceSite.Hop.Target.Doc)
		if fromOK && targetOK {
			decl := loweringContext.Statements().At(referenceSite.Hop.Target)
			if id, _, ok := declaredIn(targetCtx, referenceSite.Hop.Target.Pointer, decl); ok {
				site := LandingSite{Source: fromCtx.Place().Source, Pointer: referenceSite.Site.Pointer}
				out[site] = append(out[site], Landing{
					ID:   id,
					Path: ids.Ptr(referenceSite.Keyword),
					Ref:  referenceSite.Hop.Ref,
				})
			}
		}
	}
	for site, routes := range runTrackersOf(loweringContext).keptDiscRoutes {
		out[site] = append(out[site], routes...)
	}
	return out
}

// resolvePointer resolves a pointer in loweringContext's document to the ID of
// the schema it addresses: a source component by its stable ID, any other
// position by the ID its pointer derives there. A reference never builds what
// it names, so no node is built in a reference's context: it promises the node
// (compile.Types.ReserveNodeAtCoordinate), which hoistSubSchema builds from
// decl, the schema the bindings hold there, only where no declaration interned
// one. A decl it cannot hoist resolves to nothing.
//
// A $ref and a discriminator mapping target meet here once each has its
// pointer, so they cannot drift apart.
func resolvePointer(loweringContext lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, decl *statement.Statement) (ir.TypeID, bool) {
	id, hoisted, ok := declaredIn(loweringContext, pointer, decl)
	if hoisted {
		recordReachedSchema(loweringContext, loweringContext.Position(pointer))
		ts.ReserveNodeAtCoordinate(loweringContext.IDs().Coordinate(pointer), func() []ir.Diagnostic { return hoistSubSchema(loweringContext, ts, decl, pointer) })
	}
	return id, ok
}

// declaredIn returns the ID of the type declared at pointer in
// loweringContext's document, holding decl, as resolvePointer resolves it, and
// whether it is a position hoisted where a reference reaches it, which a
// reference promises a node. It reads no registry, so no answer turns on which
// schema lowered first.
func declaredIn(loweringContext lowering.Ctx, pointer jsontext.Pointer, decl *statement.Statement) (id ir.TypeID, hoisted, ok bool) {
	// The source's components alone are lowered whole and named by name;
	// another document's are hoisted where a reference reaches them, as any
	// other position of it is.
	if loweringContext.IDs() == (ids.DocumentIDNamespace{}) {
		if named, state := loweringContext.RefScope().ComponentRef(pointer); state.ByName() {
			return named, false, state == ids.NamedComponent
		}
	}
	// Any value the model holds is hoisted: a body, a boolean one (GitHub
	// #861), and one written as no schema, which a $ref names as it names `{}`
	// (docs/compilers/openapi.md#null-schema).
	if decl == nil || !decl.Held() {
		return "", false, false
	}
	return loweringContext.IDs().ForPointer(pointer), true, true
}

// resolvesAt reports whether a reference to at, a position of the bindings'
// table, resolves, as resolveAt resolves one, without promising its node
// (declaredAt).
func resolvesAt(loweringContext lowering.Ctx, at reftable.DocumentPointer) bool {
	_, ok := declaredAt(loweringContext, at, loweringContext.Statements().At(at))
	return ok
}

// namesReferent reports whether the reference st writes resolves, as
// resolveSchemaRef resolves it, without promising its node.
func namesReferent(loweringContext lowering.Ctx, st *statement.Statement) bool {
	at, ok := landsAt(st)
	return ok && resolvesAt(loweringContext, at)
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
func hoistSubSchema(loweringContext lowering.Ctx, ts *compile.Types, decl *statement.Statement, pointer jsontext.Pointer) []ir.Diagnostic {
	schemaPositionAnnotationSources := annotation.At(decl)
	// loweringContext reads the document that declares pointer already
	// (resolveAt), which is where the bindings place decl.
	ref, diags := Ref(loweringContext, ts, TopLevelDepth, decl, pointer)
	if LoweredToOwnNode(loweringContext, pointer, ref) {
		return diags
	}
	// As in lowerComponentSchema: this node is the first the pointer owns, so
	// the annotations Ref had nowhere to put now have a home, which its build
	// gives them.
	var annotated finished
	diags = append(diags, internOwnNode(loweringContext, ts, decl, pointer, ref, annotated.on(func(td ir.TypeDef) []ir.Diagnostic {
		return attachDeclaredAnnotations(loweringContext, ts, schemaPositionAnnotationSources.Node, pointer, ref, td)
	}))...)
	return append(diags, annotated.diags...)
}

// onLoop reports whether the $ref at b lies on a loop of $refs, which the load
// phase reports once and the lowering drops to any without a report of its
// own.
func onLoop(b *statement.Statement) bool {
	return b.Ref != nil && b.Ref.Bound && b.Ref.Entry.Chain.PositionLiesOnReferenceCycle
}
