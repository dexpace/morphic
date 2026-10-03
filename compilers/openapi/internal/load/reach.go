package load

import (
	"context"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// maxReachWork bounds every step the check takes: each node and edge it reads,
// builds or visits. Work grows linearly with the document as its aliases and
// merge keys expand it; only a pointer that can start at any node also costs
// about its length times the nesting depth it repeats through, so a document
// thousands of levels deep can reach the bound.
const maxReachWork = 1 << 24

// reach over-approximates where the resolver can send each schema reference,
// and finds a cycle through any choice of targets (GitHub #526, #546).
//
// What the resolver keeps between references decides where a pointer lands, so
// whether a document crashes it can turn on declaration order. No reading of
// the model predicts that. Where a lookup can land does not move, so every
// target is taken, and a cycle is refused in every order. A document the
// resolver survives can be refused. A "#/$defs/..." reference load holds out of
// the resolver resolves to one definition, and is exact (GitHub #557).
type reach struct {
	tree   *tree
	budget *budget
	// scope maps a schema node of the parsed model to the roots of the schema
	// documents it belongs to: more than one when an alias makes one node the
	// root of several schemas. A node outside the model has none.
	scope map[*yaml.Node][]*yaml.Node
	// owned holds every (node, root) pair scope records, so an alias that makes
	// one node a schema of many documents adds each owner without a scan.
	owned map[[2]*yaml.Node]bool
	// schemas lists the keys of scope in the library's walk order, so nothing
	// that visits them depends on map order.
	schemas []*yaml.Node
	// decls indexes every mapping declaring $anchor or $id under the schema
	// document it sits in; a nil scope is the whole tree, which every lookup
	// from a node outside the model searches.
	decls decls
	// searched holds every node at or above a /$defs/ reference: the ancestors
	// the resolver searches for a definition, and so the documents an empty
	// fragment can be resolved against.
	searched map[*yaml.Node]bool
	// drifted holds every node a reference may be resolved from with a document
	// other than the root in hand: anything under the target of a /$defs/
	// reference, and anything under the target of a reference already drifted.
	drifted map[*yaml.Node]bool
	// held holds, for each node that is a "#/$defs/..." reference of the model,
	// the definitions its schemas resolve to (defs.Target); none when the rule
	// names none, which is still no pointer read. load holds exactly these
	// references out of the resolver's own pass (withDefsHeld), so each is an
	// edge the resolver takes, not an over-approximation of one.
	held map[*yaml.Node]*posSet
	// scopes memoizes scopesOf, and climbed marks a node whose parents it has
	// already queued, so every climb together reads each node once.
	scopes  map[*yaml.Node][]*yaml.Node
	climbed map[*yaml.Node]bool
	// docs is the set documents builds on first use.
	docs   *posSet
	static map[*yaml.Node]refClass
	rawIDs map[string]int
	raws   []string
}

// reachCycle refuses a document when some choice of targets closes a chain from
// one of its schema references, and names the first such reference in the
// library's walk order. A document the model could not finish is not refused,
// as for every bound in this stage: it is reported as diag.CycleScanFailed and
// the compile goes on.
func reachCycle(ctx context.Context, locate scan.Locator, root *yaml.Node, doc *soa.OpenAPI) (ir.Diagnostic, bool) {
	return reachWithin(ctx, maxReachWork, locate, root, doc)
}

// reachWithin is reachCycle under a work budget of limit, so a test can exhaust
// it without a document that costs maxReachWork.
func reachWithin(ctx context.Context, limit int, locate scan.Locator, root *yaml.Node, doc *soa.OpenAPI) (ir.Diagnostic, bool) {
	return recoverChains(locate, func() (ir.Diagnostic, bool) {
		r, starts := newReach(ctx, limit, root, doc)
		state := map[vertex]int{}
		for _, start := range starts {
			if r.cycles(start, state) {
				return diag.Newf(ir.SeverityError, diag.CyclicRef, locate(start),
					"cyclic $ref: reference chain never reaches a node without a $ref"), true
			}
		}
		if r.incomplete() {
			return diag.Newf(ir.SeverityWarning, diag.CycleScanFailed, locate(nil),
				"reference-chain scan stopped at its %d-step or %d-level merge-key bound; "+
					"reference-cycle protection is incomplete for this source", limit, nodeview.MergeDepthLimit), true
		}
		return ir.Diagnostic{}, false
	})
}

// incomplete reports whether the model stopped short of the whole document: its
// work budget ran out, or the merge keys it reads nested past the view's bound.
func (r *reach) incomplete() bool {
	return r.budget.exhausted() || r.tree.view.Exhausted()
}

// newReach reads root and doc in the order each phase needs the last: the tree,
// the model's schemas and the definitions its held references name, the
// declarations' scopes, the searched ancestors and the drift. It returns the
// references a search starts from.
func newReach(ctx context.Context, limit int, root *yaml.Node, doc *soa.OpenAPI) (*reach, []*yaml.Node) {
	r := emptyReach(root, &budget{limit: limit})
	declared, defsRefs := r.tree.index()
	starts := r.collect(ctx, doc)
	r.group(declared)
	defsRefs = r.unheld(defsRefs)
	r.markSearched(defsRefs)
	r.drift(defsRefs)
	return r, starts
}

// unheld is the references of defsRefs the resolver reads for itself: those
// load did not hold out of its pass. A held one resolves to its own definition,
// so it searches no ancestor and moves no document.
func (r *reach) unheld(defsRefs []*yaml.Node) []*yaml.Node {
	var out []*yaml.Node
	for _, n := range defsRefs {
		if _, held := r.held[n]; !held {
			out = append(out, n)
		}
	}
	return out
}

// emptyReach is a check over root that has read nothing yet.
func emptyReach(root *yaml.Node, b *budget) *reach {
	return &reach{tree: newTree(root, b), budget: b, scope: map[*yaml.Node][]*yaml.Node{},
		owned: map[[2]*yaml.Node]bool{}, decls: newDecls(), held: map[*yaml.Node]*posSet{},
		searched: map[*yaml.Node]bool{}, drifted: map[*yaml.Node]bool{},
		scopes: map[*yaml.Node][]*yaml.Node{}, climbed: map[*yaml.Node]bool{},
		static: map[*yaml.Node]refClass{},
		rawIDs: map[string]int{"": 0}, raws: []string{""}}
}

// collect records the schema document each schema of the model belongs to, and
// returns the schemas that are references, in the library's walk order.
func (r *reach) collect(ctx context.Context, doc *soa.OpenAPI) []*yaml.Node {
	var starts []*yaml.Node
	matchSchemas(soa.Walk(ctx, doc), func(js *schemaRef) error {
		r.budget.spend(1)
		s := js.GetSchema()
		if s == nil || s.GetRootNode() == nil {
			return nil
		}
		n := nodeview.Deref(s.GetRootNode())
		if owner, ok := s.GetOwningDocument().(*schemaRef); ok && owner.GetSchema() != nil {
			r.addScope(n, nodeview.Deref(owner.GetSchema().GetRootNode()))
		}
		if js.IsReference() {
			starts = append(starts, n)
			r.hold(doc, js, n)
		}
		return nil
	})
	return starts
}

// hold records the definition a "#/$defs/..." reference js at n resolves to, as
// load resolves it (resolveHeld). A node several schemas share, as an alias
// does, is held to each one's definition.
func (r *reach) hold(doc *soa.OpenAPI, js *schemaRef, n *yaml.Node) {
	pointer, ok := heldDefsPointer(js)
	if !ok {
		return
	}
	set := r.held[n]
	if set == nil {
		set = &posSet{}
		r.held[n] = set
	}
	if t, _, found := defs.Target(doc, js, pointer); found && t.GetSchema() != nil {
		r.budget.spend(1)
		set.members = append(set.members, nodeview.Deref(t.GetSchema().GetRootNode()))
	}
}

// addScope records that n belongs to the schema document rooted at root.
func (r *reach) addScope(n, root *yaml.Node) {
	if _, seen := r.scope[n]; !seen {
		r.schemas = append(r.schemas, n)
	}
	if pair := [2]*yaml.Node{n, root}; !r.owned[pair] {
		r.owned[pair] = true
		r.scope[n] = append(r.scope[n], root)
	}
}

// ownerOf is the one schema document n belongs to. A node in several, or in
// none, is searched against the whole tree, which holds every one of them.
func (r *reach) ownerOf(n *yaml.Node) *yaml.Node {
	if owners := r.scope[n]; len(owners) == 1 {
		return owners[0]
	}
	return nil
}

// classOf is the class of the reference at n. A node with no $ref is class 0,
// which has no targets, so every chain ends there.
func (r *reach) classOf(n *yaml.Node) refClass {
	if _, held := r.held[n]; held {
		return refClass{held: n} // resolved to its definition, wherever the resolver's documents drifted to
	}
	c, ok := r.static[n]
	if !ok {
		raw := r.tree.refValue(n)
		c = refClass{id: r.intern(raw)}
		if raw != "" && namesRegistryEntry(raw) {
			c.scope = r.ownerOf(n)
		}
		r.static[n] = c
	}
	c.drifted = r.drifted[n]
	return c
}

func (r *reach) intern(raw string) int {
	if id, ok := r.rawIDs[raw]; ok {
		return id
	}
	r.rawIDs[raw] = len(r.raws)
	r.raws = append(r.raws, raw)
	return len(r.raws) - 1
}

// drift computes drifted to its fixed point. Each class and each set is
// applied once and each node marked once, so the worklist is bounded by the
// tree.
func (r *reach) drift(defsRefs []*yaml.Node) {
	work := append([]*yaml.Node(nil), defsRefs...)
	seen, done := map[refClass]bool{}, map[*posSet]bool{}
	for len(work) > 0 && r.budget.spend(1) {
		n := work[len(work)-1]
		work = work[:len(work)-1]
		c := r.classOf(n)
		if seen[c] {
			continue
		}
		seen[c] = true
		for _, s := range r.lookups(c) {
			if done[s] {
				continue
			}
			done[s] = true
			for _, t := range s.members {
				work = append(work, r.markDrifted(t)...)
			}
		}
	}
}

// markDrifted marks t's subtree and returns the references newly marked in it.
// It follows the same effective children the resolver does, aliases included.
func (r *reach) markDrifted(t *yaml.Node) []*yaml.Node {
	var refs []*yaml.Node
	stack := []*yaml.Node{t}
	for len(stack) > 0 && r.budget.spend(1) {
		n := nodeview.Deref(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		if n == nil || r.drifted[n] {
			continue
		}
		r.drifted[n] = true
		if r.tree.refValue(n) != "" {
			refs = append(refs, n)
		}
		for _, p := range r.tree.pairs(n) {
			stack = append(stack, p.Val)
		}
	}
	return refs
}

// group files each declaring mapping under every schema document it sits in,
// and under the whole tree too.
func (r *reach) group(declared []*yaml.Node) {
	for _, d := range declared {
		scopes := slices.Concat(r.scopesOf(d), []*yaml.Node{nil})
		r.budget.spend(len(scopes))
		if a := r.tree.child(d, "$anchor"); a != nil && a.Kind == yaml.ScalarNode {
			r.decls.addAnchor(scopes, a.Value, d)
		}
		if id := r.tree.child(d, "$id"); id != nil && id.Kind == yaml.ScalarNode {
			r.decls.addID(scopes, keyOfURI(id.Value), d)
		}
	}
}

// scopesOf is every schema document n belongs to: its own when n is a schema of
// the model, else those of the nearest schemas above it. An alias or merge key
// can put one mapping in several. It climbs iteratively and memoizes each node
// it resolves, so declarations nested in one another share their climb.
func (r *reach) scopesOf(n *yaml.Node) []*yaml.Node {
	stack := []*yaml.Node{n}
	for len(stack) > 0 && r.budget.spend(1) {
		m := stack[len(stack)-1]
		if _, done := r.scopes[m]; done {
			stack = stack[:len(stack)-1]
			continue
		}
		if owners, ok := r.scope[m]; ok {
			r.scopes[m] = owners
			continue
		}
		if !r.climbed[m] {
			r.climbed[m] = true
			stack = append(stack, r.tree.parents[m]...)
			continue
		}
		r.scopes[m] = r.parentScopes(m)
	}
	return r.scopes[n]
}

// parentScopes is the union of the scopes of m's parents, each once. A parent
// still unresolved here, which only a cycle of aliases could leave, adds none.
func (r *reach) parentScopes(m *yaml.Node) []*yaml.Node {
	var out []*yaml.Node
	seen := map[*yaml.Node]bool{}
	for _, p := range r.tree.parents[m] {
		r.budget.spend(len(r.scopes[p]))
		for _, owner := range r.scopes[p] {
			if !seen[owner] {
				seen[owner] = true
				out = append(out, owner)
			}
		}
	}
	return out
}

func (r *reach) markSearched(defsRefs []*yaml.Node) {
	stack := append([]*yaml.Node(nil), defsRefs...)
	for len(stack) > 0 && r.budget.spend(1) {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if r.searched[n] {
			continue
		}
		r.searched[n] = true
		stack = append(stack, r.tree.parents[n]...)
	}
}

// vertex is one node of the graph the search walks: a reference class, or a set
// of nodes several classes land in. Taking a shared set as one vertex is what
// keeps the walk linear: its members are read once, however many classes reach
// it. A vertex with a nil set is its class.
type vertex struct {
	class refClass
	set   *posSet
}

// cycles is a depth-first search from start over classes and the sets they land
// in; state is 1 for a vertex on the current path and 2 once it is known to
// reach no cycle. It stops without a verdict when the budget runs out, which
// reachWithin then reports as incomplete protection.
func (r *reach) cycles(start *yaml.Node, state map[vertex]int) bool {
	type frame struct {
		v     vertex
		sets  []*posSet    // a class's sets not yet entered
		nodes []*yaml.Node // a set's members not yet entered
	}
	enter := func(v vertex) frame {
		state[v] = 1
		if v.set != nil {
			return frame{v: v, nodes: v.set.members}
		}
		return frame{v: v, sets: r.lookups(v.class)}
	}
	v0 := vertex{class: r.classOf(start)}
	if state[v0] != 0 {
		return false
	}
	stack := []frame{enter(v0)}
	for len(stack) > 0 && r.budget.spend(1) {
		top := &stack[len(stack)-1]
		var next vertex
		switch {
		case len(top.sets) > 0:
			next, top.sets = vertex{set: top.sets[0]}, top.sets[1:]
		case len(top.nodes) > 0:
			next, top.nodes = vertex{class: r.classOf(top.nodes[0])}, top.nodes[1:]
		default:
			state[top.v] = 2
			stack = stack[:len(stack)-1]
			continue
		}
		switch state[next] {
		case 1:
			return true
		case 0:
			stack = append(stack, enter(next))
		}
	}
	return false
}
