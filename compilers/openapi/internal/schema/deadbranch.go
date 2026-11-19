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

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
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
func deadBranches(c lowering.Ctx, body, holder *statement.Schema) []bool {
	return lowering.ReadOncePerRun(c, deadBranchesOf{body, holder}, func() []bool {
		branches, _, _ := unionBranches(c, body, holder)
		out := make([]bool, len(branches))
		if !restAdmitsObjects(c, body, holder) {
			return out
		}
		for i, b := range branches {
			st := c.Statements().Subschema(b)
			out[i] = !nullBranch(c, st) && deadBranch(c, body, holder, st)
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
func restAdmitsObjects(c lowering.Ctx, body, holder *statement.Schema) bool {
	at, _ := siteOf(body)
	joint := anyInstance
	for _, a := range conjunctAtoms(c, body, allOfBranches(c, body, at.Pointer)) {
		switch {
		case a.cut:
			return false
		case a.st != nil && a.st.Schema == holder:
			joint &= declaredInstanceTypes(holder) & valueKinds(c, holder)
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
func deadBranch(c lowering.Ctx, body, holder *statement.Schema, b *statement.Statement) bool {
	kinds, inferred := lowersTo(c, b)
	if inferred || kinds&kindObject != 0 || bodyKindInferred(c, body) {
		return false
	}
	if !bodyAdmitsNull(c, body, holder) {
		return true
	}
	// lowersTo reads a cut read as every kind, so b's reaches no cycle through
	// body. Past the caps b's null is not read, as the rule says.
	if !admissionOf(c, b).within() {
		return false
	}
	return !mayAdmitNull(c, b, nil)
}

// bodyAdmitsNull reports whether body may admit null by what it writes but the
// union of holder, whose branches are read against it. The union holder passes
// over is a conjunct of that rest wherever it is written, keeping null out
// where every branch does within the read's budget, as an entry writing it is
// read (docs/compilers/openapi.md#union-beside-structure).
func bodyAdmitsNull(c lowering.Ctx, body, holder *statement.Schema) bool {
	if !mayAdmitNull(c, c.Statements().Read(body), holder) {
		return false
	}
	passed, _ := passedOver(c, body, holder)
	if passed == nil {
		return true
	}
	n := newNulls(c)
	n.variants = true
	return !n.branchesNull(passed).forbids(c) || n.conjuncts.Exhausted()
}

// deadBranchAt is one branch of u that dead marks, with where it is written.
type deadBranchAt struct {
	st *statement.Statement
	at jsontext.Pointer
}

// deadOf returns the branches of u that dead marks, in branch order.
func deadOf(c lowering.Ctx, u unionAt, dead []bool) []deadBranchAt {
	branches, key, _ := u.branches(c)
	var out []deadBranchAt
	for i, b := range branches {
		if dead[i] {
			out = append(out, deadBranchAt{st: c.Statements().Subschema(b), at: u.at + ids.Ptr(key, strconv.Itoa(i))})
		}
	}
	return out
}

// keepDeadBranches keeps each branch of u, a union distributed over body,
// written at pointer, that no instance of body satisfies (u.dead) verbatim
// under p, the Union's Unmodeled (unionBranchKey), and reports them once, at
// body (diagDeadBranches). Such a branch is no variant and lowers nowhere, so
// this is its one home.
func keepDeadBranches(c lowering.Ctx, p *ir.Unmodeled, body *statement.Schema, u unionAt, pointer jsontext.Pointer,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	all := true
	for _, d := range deadOf(c, u, u.dead) {
		kept, keptDiags := annotation.PreserveNodeInto(p, unionBranchKey(d.at), d.st.Node,
			ir.ReasonDegradedLowering, c.Place().At(d.at))
		diags = append(diags, keptDiags...)
		all = all && kept
	}
	return append(diags, diagDeadBranches(c, body, u, u.dead, pointer, all)...)
}

// diagDeadBranches is the one warning body, written at pointer, draws whose
// union u holds branches no instance of it satisfies, as dead marks them: it
// names each by its path from the body, with the kinds it admits, and says
// whether the union lives on in the others, keeping them where kept says each
// was stored, or the body lowers alone.
func diagDeadBranches(c lowering.Ctx, body *statement.Schema, u unionAt, dead []bool, pointer jsontext.Pointer,
	kept bool,
) []ir.Diagnostic {
	found := deadOf(c, u, dead)
	if len(found) == 0 {
		return nil
	}
	names := make([]string, 0, len(found))
	for _, d := range found {
		kinds, _ := lowersTo(c, d.st)
		word := noValue
		if kinds != 0 {
			word = instanceWords(kinds)
		}
		names = append(names, pathFrom(pointer, d.at)+": "+word)
	}
	r := deadReport{listed: strings.Join(names, ", "), dead: len(found)}
	r.branches, r.key, _ = u.branches(c)
	if u.branch {
		r.on = pathFrom(pointer, u.at)
	}
	if hasLiveBranch(c, u, dead) {
		r.routes = routesClause(u, "it")
		if r.dead > 1 {
			r.routes = routesClause(u, "them")
		}
		return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer, "%s",
			r.some(!bodyAdmitsNull(c, body, u.holder), kept))}
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer, "%s",
		r.none(c, body, u.holder))}
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
	opening, _, neither := r.subject(forbids)
	return fmt.Sprintf("%s %d of its branches %s %s (%s), so no instance satisfies %s: %s",
		opening, r.dead, verb, neither, r.listed, pronoun, rest)
}

// none is the report of a union no branch of which lives, each but its null
// ones dead: the schema lowers as its body alone, and admits null alone where
// the body admits null and the union of holder takes it, by its one null
// branch or by any of them where it is an anyOf.
func (r deadReport) none(c lowering.Ctx, body, holder *statement.Schema) string {
	nulls := len(r.branches) - r.dead
	forbids := !bodyAdmitsNull(c, body, holder)
	if r.on != "" {
		// What the schema lowers as is the rest of its allOf, whose null a null
		// branch of the union on the entry takes: the schema read whole.
		forbids = !mayAdmitNull(c, c.Statements().Read(body), nil)
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

// diagDeadEntryUnion is the one warning s, written at pointer, draws where u,
// the union on an entry of its allOf, keeps no branch beside the rest of it
// (unionNoLiveBranch) and the report of conjuncts sharing no value
// (docs/compilers/openapi.md#allof-disjoint-conjuncts) is silent, as where a
// null branch of u takes a null the rest admits, which the two then share.
func diagDeadEntryUnion(c lowering.Ctx, s *statement.Schema, u unionAt, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(disjointNames(c, s, allOfBranches(c, s, pointer))) > 0 {
		return nil
	}
	return diagDeadBranches(c, s, u, deadBranches(c, s, u.holder), pointer, false)
}

// deadRoute is a route the discriminator standing with a union takes to a
// dead branch of it, which routes to no variant: the mapping entry keyed tag,
// written to target, the defaultMapping, tag empty, or, keyword empty, the
// implicit route by tag, the name of the component the branch names.
type deadRoute struct {
	keyword, tag, target string
}

// deadRoutesOf returns the routes the discriminator standing with u takes to
// a dead branch: each value of its mapping and its defaultMapping whose chain
// a dead branch meets first, every branch but a null one indexed as
// indexBranches indexes a variant's, and the name of each dead branch's
// component that no entry keys and no other branch names, which OpenAPI
// routes by. A union with no dead branch has none.
func deadRoutesOf(c lowering.Ctx, u unionAt) []deadRoute {
	if u.disc == nil || !slices.Contains(u.dead, true) {
		return nil
	}
	branches, key, _ := u.branches(c)
	x := newBranchIndex(nil)
	named := map[string][]int{}
	for n, b := range branches {
		st := c.Statements().Subschema(b)
		if nullBranch(c, st) {
			continue
		}
		x.key(c, st, u.at+ids.Ptr(key, strconv.Itoa(n)), n)
		if name, _, ok := componentNamed(c.RefScope(), referenceOf(c, st)); ok {
			named[name] = append(named[name], n)
		}
	}
	meets := func(value string) bool {
		chain, ok := chainOf(c, u.disc, value)
		if !ok {
			return false
		}
		n, _, met := x.route(chain)
		return met && u.dead[n]
	}
	var out []deadRoute
	for _, e := range u.disc.Mapping {
		if meets(e.Target) {
			out = append(out, deadRoute{keyword: "mapping", tag: e.Value, target: e.Target})
		}
		delete(named, e.Value) // an entry keying the name overrides its route
	}
	if dm := u.disc.DefaultMapping; dm != "" && meets(dm) {
		out = append(out, deadRoute{keyword: "defaultMapping", target: dm})
	}
	for _, name := range slices.Sorted(maps.Keys(named)) {
		if ns := named[name]; len(ns) == 1 && u.dead[ns[0]] {
			out = append(out, deadRoute{tag: name})
		}
	}
	return out
}

// keepDeadRoutes keeps on n, the Union of u, each route its discriminator
// writes to a dead branch of u, which n's Discriminator leaves out: verbatim,
// keyed by its path from n's position as the discriminator's extensions are
// (discriminatorScope), `openapi:discriminator/mapping/<tag>` beside the
// union. Where n holds no discriminator the census keeps it whole.
func keepDeadRoutes(c lowering.Ctx, n *ir.Union, u unionAt) []ir.Diagnostic {
	if n.Discriminator == nil {
		return nil
	}
	owner, scope := discriminatorScope(u, n.Provenance.Pointer)
	var diags []ir.Diagnostic
	for _, r := range u.deadRoutes {
		if r.keyword == "" {
			continue // an implicit route writes nothing to keep
		}
		tokens := []string{r.keyword}
		if r.keyword == "mapping" {
			tokens = append(tokens, r.tag)
		}
		at := ids.Ptr(tokens...)
		node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: r.target}
		_, keptDiags := annotation.PreserveNodeInto(&n.Unmodeled, "openapi:"+scope+string(at), node,
			ir.ReasonDegradedLowering, c.Place().At(owner+at))
		diags = append(diags, keptDiags...)
	}
	return diags
}

// routesClause is what the warning of u's dead branches says of the routes
// its discriminator takes to them (deadRoutesOf), pronoun naming the branches:
// none routes, and those it writes are kept. Each route is listed by the byte
// order of what the clause says of it, which puts the defaultMapping first,
// then the mapping keys and then the names, as no order of the mapping is the
// document's.
func routesClause(u unionAt, pronoun string) string {
	if len(u.deadRoutes) == 0 {
		return ""
	}
	tags := make([]string, 0, len(u.deadRoutes))
	written := false
	for _, r := range u.deadRoutes {
		switch r.keyword {
		case "mapping":
			tags = append(tags, "mapping "+strconv.Quote(r.tag))
		case "defaultMapping":
			tags = append(tags, "defaultMapping")
		default:
			tags = append(tags, "the name "+strconv.Quote(r.tag))
		}
		written = written || r.keyword != ""
	}
	slices.Sort(tags)
	clause := "; no tag of the discriminator routes to " + pronoun + " (" + strings.Join(tags, ", ") + ")"
	if written {
		clause += ", the routes it writes kept verbatim under Unmodeled"
	}
	return clause
}

// hasLiveBranch reports whether some branch of u is neither dead nor null.
func hasLiveBranch(c lowering.Ctx, u unionAt, dead []bool) bool {
	branches, _, _ := u.branches(c)
	for i, b := range branches {
		if !dead[i] && !nullBranch(c, c.Statements().Subschema(b)) {
			return true
		}
	}
	return false
}

// pathFrom is the path of at below the body written at pointer.
func pathFrom(pointer, at jsontext.Pointer) string {
	return strings.TrimPrefix(string(at), string(pointer)+"/")
}

// keepsDeadUnion reports whether s, read at the position its bindings place it
// at, writes a union beside its object body none of whose branches an
// instance of the body satisfies, which s reports where it is written
// (diagDeadBranches), as distributesUnion reads a distribution there.
func keepsDeadUnion(c lowering.Ctx, s *statement.Schema) bool {
	at, placed := siteOf(s)
	if !placed || !declaresUnion(s) || !hasUnionSiblings(c, s) {
		return false
	}
	in, _ := lowering.InDocument(c, at.Doc)
	return classifyUnionSiblings(in, s, at.Pointer) == unionNoLiveBranch
}

// unionBranchKey is the Unmodeled key a branch of a distributed union, written
// at at, is kept under: `openapi:<keyword>/<index>`, the last two tokens of at,
// its provenance saying where it is written, beside the body or on an allOf
// entry, as an entry's own keywords are kept. A position distributes one union
// (findBranchUnion), so no two branches it keeps share a key.
func unionBranchKey(at jsontext.Pointer) string {
	index := strings.LastIndexByte(string(at), '/')
	keyword := strings.LastIndexByte(string(at[:index]), '/')
	return "openapi:" + string(at[keyword+1:])
}
