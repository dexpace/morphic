// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// deadBranchesOf keys what deadBranches read of one body and union.
type deadBranchesOf struct {
	body, holder *statement.Schema
}

// deadBranches marks, by index, each branch of the union holder writes that no
// instance of body satisfies (deadBranch), read once a run, as every reader of
// the position asks. None is marked where what body writes but the union
// admits anything but objects, or is cut short (restAdmitsObjects): a rest
// sharing no kind is the warning's that
// docs/compilers/openapi.md#allof-disjoint-conjuncts gives of the schema
// whole. That reading leaves null out: the nulls walk would classify body's
// union again.
func deadBranches(loweringContext lowering.Ctx, body, holder *statement.Schema) []bool {
	return lowering.ReadOncePerRun(loweringContext, deadBranchesOf{body, holder}, func() []bool {
		branches, _, _ := unionBranches(loweringContext, body, holder)
		out := make([]bool, len(branches))
		if !restAdmitsObjects(loweringContext, body, holder) {
			return out
		}
		for i, b := range branches {
			st := loweringContext.Statements().Subschema(b)
			out[i] = !nullBranch(loweringContext, st) && deadBranch(loweringContext, body, holder, st)
		}
		return out
	})
}

// restAdmitsObjects reports whether what body writes but the union holder
// writes, beside it or on an entry of its allOf whose own type and value set
// stay, admits objects alone, null aside, as its conjuncts are read for a
// disjoint report (conjunctAtoms): the premise deadBranch reads a branch
// against. A rest of another kind leaves the contradiction to that report,
// and one cut short, as by a $ref naming no schema, decides nothing.
func restAdmitsObjects(loweringContext lowering.Ctx, body, holder *statement.Schema) bool {
	at, _ := siteOf(body)
	joint := anyInstance
	for _, a := range conjunctAtoms(loweringContext, body, allOfEntries(loweringContext, body, at.Pointer)) {
		switch {
		case a.cut:
			return false
		case a.st != nil && a.st.Schema == holder:
			firm, _ := ownKinds(loweringContext, holder)
			joint &= firm
		default:
			joint &= a.admits
		}
	}
	return joint == kindObject
}

// deadBranch reports whether no instance satisfies both body, which admits
// objects alone, null aside, as a union distributing it requires, and the
// union branch b of the union holder writes: the kinds b lowers to (lowersTo)
// hold no object, and b or body admits no null (mayAdmitNull). Kinds the
// kind-inference policy read, on either side, and a read cut short decide
// nothing, so such a branch lives. A read past the admission's caps keeps its
// kinds, which are whole, and beside a body that may admit null it lives, its
// null reading not asked (docs/compilers/openapi.md#union-beside-structure).
func deadBranch(loweringContext lowering.Ctx, body, holder *statement.Schema, b *statement.Statement) bool {
	kinds, inferred := lowersTo(loweringContext, b)
	if inferred || kinds&kindObject != 0 || bodyKindInferred(loweringContext, body) {
		return false
	}
	if !bodyAdmitsNull(loweringContext, body, holder) {
		return true
	}
	// lowersTo reads a cut read as every kind, so b's reaches no cycle through
	// body. Past the caps b's null is not read, as the rule says.
	if !admissionOf(loweringContext, b).within() {
		return false
	}
	return !mayAdmitNull(loweringContext, b, nil)
}

// bodyAdmitsNull reports whether body may admit null by what it writes but the
// union of holder, whose branches are read against it. The union holder passes
// over is a conjunct of that rest wherever it is written, keeping null out
// where every branch does within the read's budget, as an entry writing it is
// read (docs/compilers/openapi.md#union-beside-structure).
func bodyAdmitsNull(loweringContext lowering.Ctx, body, holder *statement.Schema) bool {
	if !mayAdmitNull(loweringContext, loweringContext.Statements().Read(body), holder) {
		return false
	}
	passed, _ := passedOver(loweringContext, body, holder)
	if passed == nil {
		return true
	}
	nulls := newNulls(loweringContext)
	nulls.variants = true
	return !nulls.branchesNull(passed).forbids(loweringContext) || nulls.conjuncts.Exhausted()
}

// deadBranchAt is one branch of u that dead marks, with where it is written.
type deadBranchAt struct {
	st *statement.Statement
	at jsontext.Pointer
}

// deadOf returns the branches of unionAtValue that dead marks, in branch order.
func deadOf(loweringContext lowering.Ctx, unionAtValue unionAt, dead []bool) []deadBranchAt {
	branches, key, _ := unionAtValue.branches(loweringContext)
	var out []deadBranchAt
	for i, b := range branches {
		if dead[i] {
			out = append(out, deadBranchAt{st: loweringContext.Statements().Subschema(b), at: unionAtValue.at + ids.Ptr(key, strconv.Itoa(i))})
		}
	}
	return out
}

// keepDeadBranches keeps each branch of unionAtValue, a union distributed over
// body, written at pointer, that no instance of body satisfies
// (unionAtValue.dead) verbatim under unmodeled, the Union's Unmodeled
// (unionBranchKey), and reports them once, at body (diagDeadBranches). Such a
// branch is no variant and lowers nowhere, so this is its one home.
func keepDeadBranches(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, body *statement.Schema, unionAtValue unionAt, pointer jsontext.Pointer,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	all := true
	for _, deadBranchAt := range deadOf(loweringContext, unionAtValue, unionAtValue.dead) {
		kept, keptDiags := preserveNode(loweringContext, unmodeled, unionBranchKey(deadBranchAt.at), deadBranchAt.st.Node,
			ir.ReasonDegradedLowering, deadBranchAt.at)
		diags = append(diags, keptDiags...)
		all = all && kept
	}
	return append(diags, diagDeadBranches(loweringContext, body, unionAtValue, unionAtValue.dead, pointer, all)...)
}

// diagDeadBranches is the one warning body, written at pointer, draws whose
// union unionAtValue holds branches no instance of it satisfies, as dead marks
// them: it names each by its path from the body, with the kinds it admits, and
// says whether the union lives on in the others, keeping them where kept says
// each was stored, or the body lowers alone.
func diagDeadBranches(loweringContext lowering.Ctx, body *statement.Schema, unionAtValue unionAt, dead []bool, pointer jsontext.Pointer,
	kept bool,
) []ir.Diagnostic {
	found := deadOf(loweringContext, unionAtValue, dead)
	if len(found) == 0 {
		return nil
	}
	names := make([]string, 0, len(found))
	for _, deadBranchAt := range found {
		kinds, _ := lowersTo(loweringContext, deadBranchAt.st)
		word := noValue
		if kinds != 0 {
			word = instanceWords(kinds)
		}
		names = append(names, pathFrom(pointer, deadBranchAt.at)+": "+word)
	}
	deadReportValue := deadReport{listed: strings.Join(names, ", "), dead: len(found), nulls: unionAtValue.nulls}
	deadReportValue.branches, deadReportValue.key, _ = unionAtValue.branches(loweringContext)
	if unionAtValue.entry {
		deadReportValue.on = pathFrom(pointer, unionAtValue.at)
	}
	if hasLiveBranch(loweringContext, unionAtValue, dead) {
		deadReportValue.routes = routesClause(unionAtValue, "it")
		if deadReportValue.dead > 1 {
			deadReportValue.routes = routesClause(unionAtValue, "them")
		}
		return []ir.Diagnostic{loweringContext.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer, "%s",
			deadReportValue.some(!bodyAdmitsNull(loweringContext, body, unionAtValue.holder), kept))}
	}
	return []ir.Diagnostic{loweringContext.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer, "%s",
		deadReportValue.none(loweringContext, body, unionAtValue.holder))}
}

// deadReport is what diagDeadBranches says of a union: its keyword and
// branches, the dead ones listed, and on, the path from the body of the allOf
// entry it is written on, empty for a union beside the body.
type deadReport struct {
	branches []statement.Subschema
	key, on  string
	listed   string
	routes   string
	dead     int
	nulls    bool
}

// subject opens a report of a union beside its body, which admits objects
// alone, and null where it does not keep it out, in which case a dead branch
// admits no null either (deadBranch): it returns the opening and how to say
// that a branch admits some of what the body admits, and none of it.
func (r deadReport) subject(forbids bool) (opening, either, neither string) {
	if forbids {
		return "the body beside this " + r.key + " admits objects alone, and", "an object", "no object"
	}
	return "the body beside this " + r.key + " admits objects alone, null aside, and", "an object or null",
		"no object and no null"
}

// some is the report of dead branches beside live ones: they are no variants,
// kept verbatim where kept says so. A union on an entry is judged against the
// rest of the allOf, which may admit more kinds than an object, none a
// distributed branch admits.
func (r deadReport) some(forbids, kept bool) string {
	verb, pronoun, rest := "admit", "them", "they are no variants of the union"
	if r.dead == 1 {
		verb, pronoun, rest = "admits", "it", "it is no variant of the union"
	}
	if kept {
		rest += ", kept verbatim under Unmodeled"
	}
	rest += r.routes
	if r.on != "" {
		return fmt.Sprintf("%d of the %d branches of the %s on %s %s no value the rest of the allOf admits (%s), "+
			"so no instance satisfies %s: %s", r.dead, len(r.branches), r.key, r.on, verb, r.listed, pronoun, rest)
	}
	if r.nulls {
		return fmt.Sprintf("%d of the %d branches of this %s %s nothing but null, which the rest of this schema keeps "+
			"out (%s), so no instance satisfies %s: %s", r.dead, len(r.branches), r.key, verb, r.listed, pronoun, rest)
	}
	opening, _, neither := r.subject(forbids)
	return fmt.Sprintf("%s %d of its branches %s %s (%s), so no instance satisfies %s: %s",
		opening, r.dead, verb, neither, r.listed, pronoun, rest)
}

// none is the report of a union no branch of which lives, each but its null
// ones dead: the schema lowers as its body alone, and admits null alone where
// the body admits null and the union of holder takes it, by its one null
// branch or by any of them where it is an anyOf.
func (r deadReport) none(loweringContext lowering.Ctx, body, holder *statement.Schema) string {
	nulls := len(r.branches) - r.dead
	forbids := !bodyAdmitsNull(loweringContext, body, holder)
	if r.on != "" {
		// What the schema lowers as is the rest of its allOf, whose null a null
		// branch of the union on the entry takes: the schema read whole.
		forbids = !mayAdmitNull(loweringContext, loweringContext.Statements().Read(body), nil)
	}
	but, none := "", "no instance"
	if nulls > 0 && !forbids {
		but = " but its null ones"
		if nulls == 1 {
			but = " but a null one"
		}
		if nulls == 1 || r.key == "anyOf" {
			none = "no instance but null"
		}
	}
	if r.on != "" {
		return fmt.Sprintf("none of the %d branches of the %s on %s%s admits a value the rest of the allOf admits "+
			"(%s), so %s satisfies this schema, which lowers as the rest of the allOf",
			len(r.branches), r.key, r.on, but, r.listed, none)
	}
	opening, either, _ := r.subject(forbids)
	return fmt.Sprintf("%s none of its %d branches%s admits %s (%s), so %s satisfies this schema, which lowers as "+
		"its body alone", opening, len(r.branches), but, either, r.listed, none)
}

// diagDeadEntryUnion is the one warning schema, written at pointer, draws where
// unionAtValue, the union on an entry of its allOf, keeps no branch beside the
// rest of it (unionNoLiveBranch) and the report of conjuncts sharing no value
// (docs/compilers/openapi.md#allof-disjoint-conjuncts) is silent, as where a
// null branch of unionAtValue takes a null the rest admits, which the two then
// share.
func diagDeadEntryUnion(loweringContext lowering.Ctx, schema *statement.Schema, unionAtValue unionAt, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(disjointNames(loweringContext, schema, allOfEntries(loweringContext, schema, pointer))) > 0 {
		return nil
	}
	return diagDeadBranches(loweringContext, schema, unionAtValue, deadBranches(loweringContext, schema, unionAtValue.holder), pointer, false)
}

// deadRoute is a route the discriminator standing with a union takes to a
// dead branch of it, which routes to no variant: the mapping entry keyed tag,
// written to target, the defaultMapping, tag empty, or, keyword empty, the
// implicit route by tag, the name of the component the branch names.
type deadRoute struct {
	keyword, tag, target string
}

// deadRoutesOf returns the routes the discriminator standing with unionAtValue
// takes to a dead branch: each value of its mapping and its defaultMapping
// whose chain a dead branch meets first, every branch but a null one indexed as
// indexBranches indexes a variant's, and the name of each dead branch's
// component that no entry keys and no other branch names, which OpenAPI routes
// by. A union with no dead branch has none.
func deadRoutesOf(loweringContext lowering.Ctx, unionAtValue unionAt) []deadRoute {
	if unionAtValue.disc == nil || !slices.Contains(unionAtValue.dead, true) {
		return nil
	}
	branches, key, _ := unionAtValue.branches(loweringContext)
	branchIndex := newBranchIndex(nil)
	named := map[string][]int{}
	for n, b := range branches {
		st := loweringContext.Statements().Subschema(b)
		if nullBranch(loweringContext, st) {
			continue
		}
		branchIndex.key(loweringContext, st, unionAtValue.at+ids.Ptr(key, strconv.Itoa(n)), n)
		if name, _, ok := componentNamed(loweringContext.RefScope(), referenceOf(loweringContext, st)); ok {
			named[name] = append(named[name], n)
		}
	}
	meets := func(value string) bool {
		chain, ok := chainOf(loweringContext, unionAtValue.disc, value)
		if !ok {
			return false
		}
		n, _, met := branchIndex.route(chain)
		return met && unionAtValue.dead[n]
	}
	var out []deadRoute
	for _, mappingEntry := range unionAtValue.disc.Mapping {
		if meets(mappingEntry.Target) {
			out = append(out, deadRoute{keyword: "mapping", tag: mappingEntry.Value, target: mappingEntry.Target})
		}
		delete(named, mappingEntry.Value) // an entry keying the name overrides its route
	}
	if dm := unionAtValue.disc.DefaultMapping; dm != "" && meets(dm) {
		out = append(out, deadRoute{keyword: "defaultMapping", target: dm})
	}
	for _, name := range slices.Sorted(maps.Keys(named)) {
		if ns := named[name]; len(ns) == 1 && unionAtValue.dead[ns[0]] {
			out = append(out, deadRoute{tag: name})
		}
	}
	return out
}

// keepDeadRoutes keeps on n, the Union of u, each route its discriminator
// writes to a dead branch of u, which n's Discriminator leaves out: verbatim,
// keyed by its path from n's position as the discriminator's extensions are
// (discriminatorScope), `openapi:discriminator/mapping/<tag>` beside the
// union. Where n holds no discriminator it is kept whole, unread.
func keepDeadRoutes(loweringContext lowering.Ctx, union *ir.Union, unionAtValue unionAt) []ir.Diagnostic {
	if union.Discriminator == nil {
		return nil
	}
	owner, scope := discriminatorScope(unionAtValue, union.Provenance.Pointer)
	var diags []ir.Diagnostic
	for _, deadRoute := range unionAtValue.deadRoutes {
		if deadRoute.keyword == "" {
			continue // an implicit route writes nothing to keep
		}
		tokens := []string{deadRoute.keyword}
		if deadRoute.keyword == "mapping" {
			tokens = append(tokens, deadRoute.tag)
		}
		at := ids.Ptr(tokens...)
		node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: deadRoute.target}
		_, keptDiags := preserveNode(loweringContext, &union.Unmodeled, "openapi:"+scope+string(at), node,
			ir.ReasonDegradedLowering, owner+at)
		diags = append(diags, keptDiags...)
	}
	return diags
}

// routesClause is what the warning of unionAtValue's dead branches says of the
// routes its discriminator takes to them (deadRoutesOf), pronoun naming the
// branches: none routes, and those it writes are kept. Each route is listed by
// the byte order of what the clause says of it, which puts the defaultMapping
// first, then the mapping keys and then the names, as no order of the mapping
// is the document's.
func routesClause(unionAtValue unionAt, pronoun string) string {
	if len(unionAtValue.deadRoutes) == 0 {
		return ""
	}
	tags := make([]string, 0, len(unionAtValue.deadRoutes))
	written := false
	for _, deadRoute := range unionAtValue.deadRoutes {
		switch deadRoute.keyword {
		case "mapping":
			tags = append(tags, "mapping "+strconv.Quote(deadRoute.tag))
		case "defaultMapping":
			tags = append(tags, "defaultMapping")
		default:
			tags = append(tags, "the name "+strconv.Quote(deadRoute.tag))
		}
		written = written || deadRoute.keyword != ""
	}
	slices.Sort(tags)
	clause := "; no tag of the discriminator routes to " + pronoun + " (" + strings.Join(tags, ", ") + ")"
	if written {
		clause += ", the routes it writes kept verbatim under Unmodeled"
	}
	return clause
}

// hasLiveBranch reports whether some branch of unionAtValue is neither dead nor
// null.
func hasLiveBranch(loweringContext lowering.Ctx, unionAtValue unionAt, dead []bool) bool {
	branches, _, _ := unionAtValue.branches(loweringContext)
	for i, b := range branches {
		if !dead[i] && !nullBranch(loweringContext, loweringContext.Statements().Subschema(b)) {
			return true
		}
	}
	return false
}

// pathFrom is the path of at below the body written at pointer.
func pathFrom(pointer, at jsontext.Pointer) string {
	return strings.TrimPrefix(string(at), string(pointer)+"/")
}

// keepsDeadUnion reports whether schema, read at the position its bindings
// place it at, writes a union beside its object body none of whose branches an
// instance of the body satisfies, which schema reports where it is written
// (diagDeadBranches), as distributesUnion reads a distribution there.
func keepsDeadUnion(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	at, placed := siteOf(schema)
	if !placed || !declaresUnion(schema) || !hasUnionSiblings(loweringContext, schema) {
		return false
	}
	in, _ := lowering.InDocument(loweringContext, at.Doc)
	return classifyUnionSiblings(in, schema, at.Pointer) == unionNoLiveBranch
}

// unionBranchKey is the Unmodeled key a branch of a distributed union, written
// at at, is kept under: `openapi:<keyword>/<index>`, the last two tokens of at,
// its provenance saying where it is written, beside the body or on an allOf
// entry, as an entry's own keywords are kept. A position distributes one union
// (findEntryUnion), so no two branches it keeps share a key.
func unionBranchKey(at jsontext.Pointer) string {
	index := strings.LastIndexByte(string(at), '/')
	keyword := strings.LastIndexByte(string(at[:index]), '/')
	return "openapi:" + string(at[keyword+1:])
}
