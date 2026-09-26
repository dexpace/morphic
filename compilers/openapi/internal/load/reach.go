package load

import (
	"context"
	"encoding/json/jsontext"
	"net/url"
	"strconv"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// maxReachNodes bounds every walk this file makes over the tree or the graph.
const maxReachNodes = sourceindex.MaxIndexedNodes

// reach over-approximates where the resolver can send each schema reference,
// and refuses a document in which any choice of targets closes a cycle
// (GitHub #526, #546).
//
// The library's schema resolver keeps state between references, and the target
// of a reference depends on it: a $defs pointer resolves against whichever
// schema the chain that reached it came from, and the first success is cached
// for every later reference spelling the same pointer; every pointer target is
// re-registered as a document of its own, which moves the base its $id and
// $anchor lookups are keyed by. The result depends on declaration order —
// a document can crash the resolver in one order and resolve in the other —
// so no reading of the model before resolution predicts it. What does not move
// is where a lookup can land: a pointer lands on the node its path names from
// some node of the document, a $anchor on a schema declaring that name within
// the schema document the reference sits in, an $id on a schema declaring one
// there. The graph below takes every such target as possible, so a cycle the
// resolver can enter in any order is refused in every order.
//
// Taking every possible target rather than the one true target for a given
// resolver run is also what makes this an over-approximation rather than a
// prediction: a document the real resolver survives can still be refused,
// when the union of targets closes a cycle no single resolver call actually
// takes. Measured against the JSON Schema Test Suite's own draft2020-12
// groups (every group embedded four ways) this costs nothing — 0 of 1736
// documents are false refusals — and against adversarial fuzzing of
// $ref/$anchor/$id/$defs combinations it costs roughly 9% of the documents
// the real resolver would have accepted (TestReachCycle_KnownFalseRefusals
// pins the mechanism behind a handful of concrete ones: a /$defs/ pointer's
// first token is looked up across the whole document rather than scoped to
// the component that declares it, a node with no $id-scoped ancestor of its
// own searches the whole tree, and declaring does not model which of two
// same-named anchors the registry actually keeps). That price buys the
// property no exact model of this resolver's state can have: the same
// verdict regardless of which order the document's components declare.
type reach struct {
	root *yaml.Node
	// byKey holds every mapping value and sequence element under the key or
	// index that reaches it, so a pointer's first token finds every node it can
	// start from.
	byKey map[string][]*yaml.Node
	// scope maps a schema node of the parsed model to the root of the schema
	// document it belongs to; a node outside the model is scoped to the whole
	// tree.
	scope map[*yaml.Node]*yaml.Node
	// defsRefs reports whether any reference spells a /$defs/ pointer: only
	// those resolve against a document other than the root, so without one every
	// other pointer has the root as its only starting node.
	defsRefs bool
	// parent is each node's parent in the tree, for the two walks upward below.
	parent map[*yaml.Node]*yaml.Node
	// decls holds, per schema-document root, every mapping under it declaring
	// $anchor or $id; nil keys the whole tree, which every lookup from a node
	// outside the model searches.
	decls map[*yaml.Node][]*yaml.Node
	// searched holds every node at or above a /$defs/ reference: the ancestors
	// the resolver searches for a definition, and so the documents an empty
	// fragment can be resolved against.
	searched map[*yaml.Node]bool
	// drifted holds every node a reference may be resolved from with a document
	// other than the root in hand: anything under the target of a /$defs/
	// reference, and anything under the target of a reference already drifted,
	// since the resolver hands each target's references the document it found
	// that target in.
	drifted map[*yaml.Node]bool
}

// reachCycle reports the first schema reference, in the library's walk order,
// from which some choice of targets returns to a node already on the chain.
func reachCycle(ctx context.Context, locate scan.Locator, root *yaml.Node, doc *soa.OpenAPI) (ir.Diagnostic, bool) {
	return recoverChains(locate, func() (ir.Diagnostic, bool) {
		r, starts := newReach(ctx, root, doc)
		state := map[*yaml.Node]int{}
		for _, start := range starts {
			if r.cycles(start, state) {
				return diag.Newf(ir.SeverityError, diag.CyclicRef, locate(start),
					"cyclic $ref: reference chain never reaches a node without a $ref"), true
			}
		}
		return ir.Diagnostic{}, false
	})
}

func newReach(ctx context.Context, root *yaml.Node, doc *soa.OpenAPI) (*reach, []*yaml.Node) {
	r := &reach{root: contentRoot(root), byKey: map[string][]*yaml.Node{}, scope: map[*yaml.Node]*yaml.Node{},
		parent: map[*yaml.Node]*yaml.Node{}, decls: map[*yaml.Node][]*yaml.Node{}, searched: map[*yaml.Node]bool{},
		drifted: map[*yaml.Node]bool{}}
	declared, defsRefs := r.index()
	var starts []*yaml.Node
	for item := range soa.Walk(ctx, doc) {
		_ = item.Match(soa.Matcher{Schema: func(js *schemaRef) error {
			s := js.GetSchema()
			if s == nil || s.GetRootNode() == nil {
				return nil
			}
			n := deref(s.GetRootNode())
			if owner, ok := s.GetOwningDocument().(*schemaRef); ok && owner.GetSchema() != nil {
				r.scope[n] = deref(owner.GetSchema().GetRootNode())
			}
			if js.IsReference() {
				starts = append(starts, n)
			}
			return nil
		}})
	}
	r.group(declared)
	r.markSearched(defsRefs)
	r.drift(defsRefs)
	return r, starts
}

// drift computes drifted to its fixed point: every node is marked at most
// once, so the worklist is bounded by the tree.
func (r *reach) drift(defsRefs []*yaml.Node) {
	work := append([]*yaml.Node(nil), defsRefs...)
	for steps := 0; len(work) > 0 && steps < maxReachNodes; steps++ {
		n := work[len(work)-1]
		work = work[:len(work)-1]
		for _, t := range r.targets(n) {
			work = append(work, r.markDrifted(t)...)
		}
	}
}

// markDrifted marks t's subtree and returns the references newly marked in it.
func (r *reach) markDrifted(t *yaml.Node) []*yaml.Node {
	var refs []*yaml.Node
	stack := []*yaml.Node{t}
	for visited := 0; len(stack) > 0 && visited < maxReachNodes; visited++ {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || n.Kind == yaml.AliasNode || r.drifted[n] {
			continue
		}
		r.drifted[n] = true
		if refValue(n) != "" {
			refs = append(refs, n)
		}
		stack = append(stack, n.Content...)
	}
	return refs
}

// group files each declaring mapping under the schema document it sits in, and
// every one under the whole tree too.
func (r *reach) group(declared []*yaml.Node) {
	for _, d := range declared {
		r.decls[nil] = append(r.decls[nil], d)
		if s := r.scopeOf(d); s != nil {
			r.decls[s] = append(r.decls[s], d)
		}
	}
}

// scopeOf is the schema document n belongs to: its own when n is a schema of
// the model, else that of the nearest schema above it.
func (r *reach) scopeOf(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && i < maxReachNodes; i++ {
		if s, ok := r.scope[n]; ok {
			return s
		}
		n = r.parent[n]
	}
	return nil
}

func (r *reach) markSearched(defsRefs []*yaml.Node) {
	for _, n := range defsRefs {
		for i := 0; n != nil && !r.searched[n] && i < maxReachNodes; i++ {
			r.searched[n] = true
			n = r.parent[n]
		}
	}
}

// index fills byKey and parent with one bounded walk that never follows an
// alias, so each node is entered once, and returns the mappings declaring
// $anchor or $id and those spelling a /$defs/ pointer.
func (r *reach) index() (declared, defsRefs []*yaml.Node) {
	stack := []*yaml.Node{r.root}
	for visited := 0; len(stack) > 0 && visited < maxReachNodes; visited++ {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || n.Kind == yaml.AliasNode {
			continue
		}
		if n.Kind == yaml.MappingNode {
			if child(n, "$anchor") != nil || child(n, "$id") != nil {
				declared = append(declared, n)
			}
			if strings.Contains(refValue(n), "#/$defs/") {
				defsRefs = append(defsRefs, n)
			}
		}
		r.indexChildren(n)
		stack = append(stack, n.Content...)
	}
	r.defsRefs = len(defsRefs) > 0
	return declared, defsRefs
}

func (r *reach) indexChildren(n *yaml.Node) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			v := deref(n.Content[i+1])
			r.byKey[n.Content[i].Value] = append(r.byKey[n.Content[i].Value], v)
			r.parent[n.Content[i+1]] = n
		}
	case yaml.SequenceNode:
		for i, v := range n.Content {
			r.byKey[strconv.Itoa(i)] = append(r.byKey[strconv.Itoa(i)], deref(v))
			r.parent[v] = n
		}
	}
}

// cycles is a bounded depth-first search from start over every possible target;
// state is 1 while a node is on the current path and 2 once it is known to
// reach no cycle.
func (r *reach) cycles(start *yaml.Node, state map[*yaml.Node]int) bool {
	type frame struct {
		n    *yaml.Node
		next []*yaml.Node
	}
	if state[start] != 0 {
		return false
	}
	stack := []frame{{n: start, next: r.targets(start)}}
	state[start] = 1
	for steps := 0; len(stack) > 0 && steps < maxReachNodes; steps++ {
		top := &stack[len(stack)-1]
		if len(top.next) == 0 {
			state[top.n] = 2
			stack = stack[:len(stack)-1]
			continue
		}
		t := top.next[0]
		top.next = top.next[1:]
		switch state[t] {
		case 1:
			return true
		case 0:
			if refValue(t) != "" {
				state[t] = 1
				stack = append(stack, frame{n: t, next: r.targets(t)})
			}
		}
	}
	return false
}

// targets is every node the reference at n can land on. The resolver asks the
// registries first, with the fragment exactly as written (oas3.ExtractAnchor),
// and falls back to a pointer read the way references.Reference trims and
// decodes one, so a reference can be tried both ways and both are kept.
func (r *reach) targets(n *yaml.Node) []*yaml.Node {
	raw := refValue(n)
	uriPart, fragment, hasFragment := strings.Cut(raw, "#")
	uri := strings.TrimSpace(uriPart)
	var out []*yaml.Node
	if hasFragment && fragment != "" && !strings.HasPrefix(fragment, "/") {
		out = append(out, r.declaring(n, "$anchor", fragment)...)
	}
	if uri != "" {
		return append(out, r.idTargets(n, uri, strings.TrimSpace(fragment))...)
	}
	if !hasFragment {
		return out
	}
	switch pointer := strings.TrimSpace(fragment); {
	case pointer == "" || pointer == "/":
		return append(out, r.documentTargets(n)...)
	case strings.HasPrefix(pointer, "/"):
		return append(out, r.pointerTargets(n, decodeFragment(pointer))...)
	}
	return out
}

// pointerTargets is every node the pointer names from a node it can be
// resolved against: the root alone unless the document spells a /$defs/
// pointer, and otherwise any node.
func (r *reach) pointerTargets(n *yaml.Node, pointer string) []*yaml.Node {
	tokens := pointerTokens(pointer)
	if len(tokens) == 0 {
		return nil
	}
	var starts []*yaml.Node
	if r.drifted[n] || strings.HasPrefix(pointer, "/$defs/") {
		starts = r.byKey[tokens[0]]
	} else if first := child(r.root, tokens[0]); first != nil {
		starts = []*yaml.Node{first}
	}
	var out []*yaml.Node
	for _, s := range starts {
		if t := walkTokens(s, tokens[1:]); t != nil {
			out = append(out, t)
		}
	}
	return out
}

// documentTargets is where an empty fragment can land: the document it is
// resolved against. That is the root — never a schema — unless a /$defs/
// pointer moved it, and then it is the schema a chain arrived from or an
// ancestor the resolver searched for the definition.
func (r *reach) documentTargets(n *yaml.Node) []*yaml.Node {
	if !r.drifted[n] {
		return nil
	}
	var out []*yaml.Node
	for n := range r.scope {
		if refValue(n) != "" || r.searched[n] {
			out = append(out, n)
		}
	}
	return out
}

// idTargets is where a URI part can land within this document: a schema in
// the reference's scope whose $id could resolve to the same URI, navigated by
// the fragment when it is a pointer. Which base each side is resolved against
// depends on the resolver's state, but resolving against any base keeps the
// last path segment, so an $id whose last segment differs can never match.
func (r *reach) idTargets(n *yaml.Node, uri, fragment string) []*yaml.Node {
	want := lastSegment(uri)
	var ids []*yaml.Node
	for _, d := range r.declaring(n, "$id", "") {
		if lastSegment(child(d, "$id").Value) == want {
			ids = append(ids, d)
		}
	}
	if fragment == "" || fragment == "/" {
		return ids
	}
	if !strings.HasPrefix(fragment, "/") {
		return nil // the anchor half is already among the targets
	}
	tokens := pointerTokens(decodeFragment(fragment))
	var out []*yaml.Node
	for _, id := range ids {
		if t := walkTokens(id, tokens); t != nil {
			out = append(out, t)
		}
	}
	return out
}

// lastSegment is the final path segment of a URI, the part no base can change.
func lastSegment(uri string) string {
	uri, _, _ = strings.Cut(uri, "#")
	uri, _, _ = strings.Cut(uri, "?")
	if i := strings.LastIndexByte(uri, '/'); i >= 0 {
		return uri[i+1:]
	}
	return uri
}

// declaring is every mapping in n's schema document with key set to value
// (any value when value is ""), whatever base it would be registered under.
// A node outside the model searches the whole tree.
func (r *reach) declaring(n *yaml.Node, key, value string) []*yaml.Node {
	var out []*yaml.Node
	for _, d := range r.decls[r.scope[n]] {
		if v := child(d, key); v != nil && v.Kind == yaml.ScalarNode && (value == "" || v.Value == value) {
			out = append(out, d)
		}
	}
	return out
}

func refValue(n *yaml.Node) string {
	v := child(n, "$ref")
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

func child(n *yaml.Node, token string) *yaml.Node {
	n = deref(n)
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		var found *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == token {
				found = deref(n.Content[i+1]) // the last spelling wins, as the parser reads it
			}
		}
		return found
	case yaml.SequenceNode:
		for i, v := range n.Content {
			if strconv.Itoa(i) == token {
				return deref(v)
			}
		}
	}
	return nil
}

func walkTokens(n *yaml.Node, tokens []string) *yaml.Node {
	for _, t := range tokens {
		if n = child(n, t); n == nil {
			return nil
		}
	}
	return n
}

func pointerTokens(pointer string) []string {
	var out []string
	for t := range jsontext.Pointer(pointer).Tokens() {
		out = append(out, t)
	}
	return out
}

func decodeFragment(fragment string) string {
	if d, err := url.QueryUnescape(fragment); err == nil {
		return d
	}
	return fragment
}

func deref(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && n.Kind == yaml.AliasNode && i < 64; i++ {
		n = n.Alias
	}
	return n
}

func contentRoot(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return deref(n.Content[0])
	}
	return deref(n)
}

// chainCycle runs the reach check when the tree can hold a lookup the
// pre-parse scan does not model: a $anchor or $id the registries hold, or a
// /$defs/ pointer resolved against something other than the root. Without
// either, every chain is root pointers, which that scan has already refused a
// cycle of.
func chainCycle(ctx context.Context, locate scan.Locator, root *yaml.Node, doc *soa.OpenAPI) (ir.Diagnostic, bool) {
	if !declaresRegistryKeys(root) && !spellsDefsPointer(root) {
		return ir.Diagnostic{}, false
	}
	return reachCycle(ctx, locate, root, doc)
}

func spellsDefsPointer(root *yaml.Node) bool {
	stack := []*yaml.Node{root}
	for visited := 0; len(stack) > 0 && visited < maxReachNodes; visited++ {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || n.Kind == yaml.AliasNode {
			continue
		}
		if n.Kind == yaml.MappingNode && strings.Contains(refValue(n), "#/$defs/") {
			return true
		}
		stack = append(stack, n.Content...)
	}
	return false
}
