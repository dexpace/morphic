package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"maps"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// reached holds to the table what a reference reaches that the source's own
// check never read as that kind: an object in another document (GitHub #545),
// or one in the source outside its model, such as under an x-* key (GitHub
// #759, #872). The reference names the kind it expects, and the version is the
// source's.
//
// Only what a reference reaches is checked, so a shared file's unread content
// fails no compile. Each finding is reported once (see checkAll), and what a
// check spans is charged to a budget (see charge).
type reached struct {
	checker oas.Checker
	version string
	limit   int
	// esc holds the $refs rewritten for the resolver to read, which a message
	// names as the source writes them (GitHub #911).
	esc *escapes
	// tree places a finding about a node of the source (see reachedLocator).
	tree sourceTree
	// seen holds the objects the source's own check read as their kind.
	seen map[objectKey]bool
	// reported holds every finding already reported, the source's own included.
	reported map[ir.Diagnostic]bool
	// spent is the node count charged to each document so far.
	spent map[string]int
	// over names the documents whose budget has been reported crossed.
	over map[string]bool
}

// reachedTarget is an object the source's references reach: the least pointer
// among the $refs reaching it, the kind they expect it to be, the node it is
// written as, the document it is in, as findingPlace names it, and whether that
// is the source. place places a finding at site where another document writes
// that $ref, named siteDoc, and is nil where the source does.
type reachedTarget struct {
	site     jsontext.Pointer
	siteDoc  string
	kind     oas.Kind
	node     *yaml.Node
	doc      string
	inSource bool
	place    func(jsontext.Pointer) ir.Provenance
}

// at is the site of the $ref t is placed at.
func (t reachedTarget) at() refSite { return refSite{doc: t.siteDoc, pointer: t.site} }

// validateReached checks every object a resolved reference reaches that the
// source's own check did not read as that kind (see dropSeen), in doc, walked
// with the fields beside its path item $refs, or in away, another document's
// (GitHub #751), and every schema built for a landing of b. Each finding is
// reported once, where it is placed (see reachedLocator), and none the
// source's own check made.
func validateReached(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, b bound, doc *soa.OpenAPI,
	sibs Siblings, opts Options, self sourceDocument, away []awayRef,
) []ir.Diagnostic {
	checks := newReached(doc, opts, self.tree, self.judged)
	checks.esc = self.escapes
	table := b.bindings.Table()
	targets := builtTargets(b.built, b.placer(at), func(id bind.DocID) string { return table.Document(id).Name })
	for _, a := range away {
		if t, ok := targetOf(a.site.pointer, a.r, checks.esc); ok {
			t.siteDoc, t.place = a.site.doc, a.place
			targets = append(targets, t)
		}
	}
	return checkReached(at, walkSource(ctx, doc, sibs), targets, checks)
}

// builtTargets returns the targets the schemas built for landings make, each
// placed at the least position whose chain reaches it, in whichever document
// (place), as name names it. One no position reaches, which only a budget
// cutting the reading leaves, is no target.
func builtTargets(built []typed.Built, place func(bind.Position) ir.Provenance, name func(bind.DocID) string,
) []reachedTarget {
	var out []reachedTarget
	for _, b := range built {
		if b.Schema == nil || !b.Landing.Reached || b.Schema.GetSchema() == nil {
			continue
		}
		node := nodeview.Deref(b.Schema.GetSchema().GetRootNode())
		t := reachedTarget{site: b.Landing.Site.Pointer, kind: oas.Schema, node: node,
			doc: b.Landing.Name, inSource: b.Landing.At.Doc == bind.Source}
		if site := b.Landing.Site; site.Doc != bind.Source {
			t.siteDoc = name(site.Doc)
			t.place = func(p jsontext.Pointer) ir.Provenance { return place(bind.Position{Doc: site.Doc, Pointer: p}) }
		}
		out = append(out, t)
	}
	return out
}

// checkReached is validateReached over a walk the caller supplies, so a test
// can hand it one yielding a reference no real document builds. built are the
// targets the schema bindings' landings make (see builtTargets) and those
// another document's references make.
//
// A panic reading the walk is reported at the model being read, or at the root,
// and nothing is checked, since the targets read so far need not hold each
// one's least $ref.
func checkReached(at func(jsontext.Pointer) ir.Provenance,
	items iter.Seq[soa.WalkItem], built []reachedTarget, checks *reached,
) []ir.Diagnostic {
	targets := map[objectKey]reachedTarget{}
	add := func(t reachedTarget) {
		key := objectKey{node: t.node, kind: t.kind}
		if prev, seen := targets[key]; !seen || compareSites(t.at(), prev.at()) < 0 {
			targets[key] = t
		}
	}
	site, err := eachReference(items, "validation", func(site jsontext.Pointer, r resolvable) error {
		if t, ok := targetOf(site, r, checks.esc); ok {
			add(t)
		}
		return nil
	})
	if err != nil {
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.Validation, at(site), "%s", err.Error())}
	}
	for _, t := range built {
		add(t)
	}
	checks.dropSeen(targets)
	return checks.checkAll(at, targets)
}

// kindOf returns the kind of object a reference object expects to name, or false
// for one the table has no kind for.
func kindOf(r resolvable) (oas.Kind, bool) {
	switch r.(type) {
	case *soa.ReferencedPathItem:
		return oas.PathItem, true
	case *soa.ReferencedParameter:
		return oas.Parameter, true
	case *soa.ReferencedHeader:
		return oas.Header, true
	case *soa.ReferencedRequestBody:
		return oas.RequestBody, true
	case *soa.ReferencedResponse:
		return oas.Response, true
	case *soa.ReferencedExample:
		return oas.Example, true
	case *soa.ReferencedLink:
		return oas.Link, true
	case *soa.ReferencedCallback:
		return oas.Callback, true
	case *soa.ReferencedSecurityScheme:
		return oas.SecurityScheme, true
	default:
		return 0, false
	}
}

// targetOf returns the object the reference at site reaches, or false when its
// chain ends on no object (reachedNode) or the reference expects a kind the
// table lacks. A chain that ends in the source is a target too, whatever its
// $refs spell: it can reach raw YAML, such as an x-* key's value, or a node read
// as another kind, neither of which the source's check read as that kind. What
// it did read, dropSeen drops.
func targetOf(site jsontext.Pointer, r resolvable, esc *escapes) (reachedTarget, bool) {
	t := esc.trailOf(resolutionChain(r))
	node, ok := reachedNode(r)
	kind, known := kindOf(r)
	if !ok || !known {
		return reachedTarget{}, false
	}
	return reachedTarget{site: site, kind: kind, node: node, doc: findingPlace(t), inSource: t.endsInSource}, true
}

// dropSeen drops each target in the source that the source's own check read as
// the kind the reference expects.
func (v *reached) dropSeen(targets map[objectKey]reachedTarget) {
	maps.DeleteFunc(targets, func(key objectKey, t reachedTarget) bool { return t.inSource && v.seen[key] })
}

// newReached returns the checker for the objects doc's references reach, under
// opts' node and alias budgets; either one unbounded leaves the charge
// unbounded. It holds them to the version doc declares. tree is the source's,
// and judged what the table found in it, none of which is reported again.
func newReached(doc *soa.OpenAPI, opts Options, tree sourceTree, judged judgment) *reached {
	limit := 0
	if opts.MaxSourceNodes > 0 && opts.MaxAliasSurplus > 0 {
		limit = opts.MaxSourceNodes + opts.MaxAliasSurplus
	}
	reported := make(map[ir.Diagnostic]bool, len(judged.diags))
	for _, d := range judged.diags {
		reported[oas.AsPlaced(d)] = true
	}
	return &reached{
		checker:  oas.NewChecker(),
		tree:     tree,
		version:  doc.OpenAPI,
		limit:    limit,
		seen:     judged.seen,
		reported: reported,
		spent:    map[string]int{},
		over:     map[string]bool{},
	}
}

// checkAll checks each target in the order of its site, and reports its
// findings. A finding already reported was reported for a lesser $ref, so one
// placed at a $ref lands at the least whose object's check draws it, and a
// budget is crossed at the same object, whatever the declaration order.
//
// An object inside one checked before it is checked, and charged, again
// (GitHub #571): a container spans more than its model reads, such as an
// extension's content.
func (v *reached) checkAll(at func(jsontext.Pointer) ir.Provenance, targets map[objectKey]reachedTarget,
) []ir.Diagnostic {
	order := slices.SortedFunc(maps.Values(targets), func(a, b reachedTarget) int {
		return compareSites(a.at(), b.at())
	})
	diags := make([]ir.Diagnostic, 0, len(order))
	for _, t := range order {
		place := at
		if t.place != nil {
			place = t.place
		}
		diags = append(diags, v.check(place(t.site), t)...)
	}
	return diags
}

// check judges t's object and reports what it finds at site, the $ref that
// reached it, or where the source's tree holds the node a finding is about. One
// already reported is not reported again, as the source reports a finding once
// however many references share its node.
func (v *reached) check(site ir.Provenance, t reachedTarget) []ir.Diagnostic {
	if d, over := v.charge(site, t.doc, t.node); over {
		return d
	}
	found := v.checker.Check(t.node, v.version, oas.Options{Start: t.kind, Locate: v.tree.reachedLocator(t.node)})
	var diags []ir.Diagnostic
	for _, d := range found {
		if key := identity(d, t.doc); !v.reported[key] {
			v.reported[key] = true
			diags = append(diags, placeAt(d, site, t.doc))
		}
	}
	return diags
}

// charge counts what checking the object at node spans against doc's budget,
// each alias counted as a copy of what it names, as a check walks it. A
// document may be charged what the source may be checked over: the node
// budget plus the alias budget. Objects nest, so without the charge a document
// could be checked many times over its size.
//
// It reports true when the object is not to be checked: doc's budget was
// crossed before, or this object would cross it, which is reported once, here.
func (v *reached) charge(site ir.Provenance, doc string, node *yaml.Node) ([]ir.Diagnostic, bool) {
	if v.over[doc] {
		return nil, true
	}
	span, fits := spanned(node, v.remaining(doc))
	if !fits {
		v.over[doc] = true
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.BudgetExceeded, site,
			"the objects references reach in %s span more than the %d nodes a document is validated over; "+
				"its validation stops at this one", doc, v.limit)}, true
	}
	v.spent[doc] += span
	return nil, false
}

// remaining is how many more nodes doc may be charged before its budget is
// crossed, and so how far a count need go to tell; an unbounded budget leaves
// the count unbounded too.
func (v *reached) remaining(doc string) int {
	if v.limit <= 0 {
		return -1
	}
	return v.limit - v.spent[doc]
}

// reachedNode returns the node the object a reference names was built from, or
// false for one with none to check: an unresolved reference names none, and a
// chain that loops, or stops at a reference it could not resolve, ends on no
// object. The reference reads the node itself: asked of a missing object, a
// model's own GetRootNode faults.
func reachedNode(r resolvable) (*yaml.Node, bool) {
	holder, ok := r.(interface{ GetRootNode() *yaml.Node })
	if !ok {
		return nil, false
	}
	node := nodeview.Deref(holder.GetRootNode())
	return node, node != nil
}

// spanned counts the nodes under root as a model built from it holds them,
// each alias counted as a copy of what it names, and reports whether the count
// stays within limit, negative meaning none. It stops one node past limit, so
// refusing an object costs the budget, not the object. No alias leads back up:
// a document whose anchor names one of its own ancestors was refused.
//
// root and every node under it are real: yaml.v3 leaves no nil in Content, and
// a well-formed alias's target is never nil, or the cycle refusals ahead
// would have refused the document.
func spanned(root *yaml.Node, limit int) (int, bool) {
	count := 0
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		if limit >= 0 && count > limit {
			return count, false
		}
		if n.Kind == yaml.AliasNode {
			stack = append(stack, n.Alias)
			continue
		}
		stack = append(stack, n.Content...)
	}
	return count, true
}
