package load

import (
	"context"
	"encoding/json/jsontext"
	"net/url"
	"strconv"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
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
// The resolver's registry lookups keep state between references: every pointer
// target is re-registered as a document of its own, which moves the base its
// $id and $anchor lookups are keyed by, and of two equal anchors the first
// registered wins. The result depends on declaration order, so no reading of
// the model before resolution predicts it. What does not move is where a lookup
// can land: a $anchor on a schema declaring that name within the schema
// document the reference sits in, an $id on a schema declaring one there. The
// graph below takes every such target as possible, so a cycle the resolver can
// enter in any order is refused in every order.
//
// A "#/$defs/..." pointer is the one lookup it does not have to guess. load
// holds every such reference out of the resolver's own pass and resolves each
// to the definition the resolver's rule names for it in its own place
// (GitHub #557), so its edge here is that one definition (defs.Target), and a
// plain pointer is read from the root, where the resolver reads it now that no
// $defs lookup hands it another document.
//
// Taking every possible registry target rather than the one a given run
// takes is what makes this an over-approximation: a document the pipeline
// survives can still be refused. Measured against the JSON Schema Test Suite's
// own draft2020-12 groups (every group embedded four ways) that costs nothing,
// 0 of 1736 documents; TestReachCycle_KnownFalseRefusals pins the mechanisms
// behind the adversarial cases that remain (a node with no $id-scoped ancestor
// searches the whole tree, and declaring does not model which of two
// same-named anchors the registry keeps). That price buys the property no
// exact model of this resolver's state can have: the same verdict regardless
// of which order the document's components declare.
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
	// defs holds, for each "#/$defs/..." reference of the parsed model, the node
	// of the definition the resolver's rule names for it (defs.Target), or nil
	// when it names none. load holds exactly these references out of the
	// resolver's own pass and resolves each to that definition afterwards
	// (withDefsHeld, resolveDefs), so this is an edge the resolver takes, not an
	// over-approximation of one (GitHub #557).
	defs map[*yaml.Node]*yaml.Node
	// parent is each node's parent in the tree, for the two walks upward below.
	parent map[*yaml.Node]*yaml.Node
	// decls holds, per schema-document root, every mapping under it declaring
	// $anchor or $id; nil keys the whole tree, which every lookup from a node
	// outside the model searches.
	decls map[*yaml.Node][]*yaml.Node
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
		parent: map[*yaml.Node]*yaml.Node{}, decls: map[*yaml.Node][]*yaml.Node{}, defs: map[*yaml.Node]*yaml.Node{}}
	declared := r.index()
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
				r.recordDefs(doc, js, n)
			}
			return nil
		}})
	}
	r.group(declared)
	return r, starts
}

// recordDefs records the definition a held "#/$defs/..." reference at n
// resolves to, as load will resolve it.
func (r *reach) recordDefs(doc *soa.OpenAPI, js *schemaRef, n *yaml.Node) {
	pointer, ok := heldDefsPointer(js)
	if !ok {
		return
	}
	r.defs[n] = nil
	if t, _, found := defs.Target(doc, js, pointer); found && t.GetSchema() != nil {
		r.defs[n] = deref(t.GetSchema().GetRootNode())
	}
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

// index fills byKey and parent with one bounded walk that never follows an
// alias, so each node is entered once, and returns the mappings declaring
// $anchor or $id.
func (r *reach) index() (declared []*yaml.Node) {
	stack := []*yaml.Node{r.root}
	for visited := 0; len(stack) > 0 && visited < maxReachNodes; visited++ {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || n.Kind == yaml.AliasNode {
			continue
		}
		if n.Kind == yaml.MappingNode && (child(n, "$anchor") != nil || child(n, "$id") != nil) {
			declared = append(declared, n)
		}
		r.indexChildren(n)
		stack = append(stack, n.Content...)
	}
	return declared
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
	if t, held := r.defs[n]; held {
		if t != nil {
			out = append(out, t)
		}
		return out
	}
	if pointer := strings.TrimSpace(fragment); strings.HasPrefix(pointer, "/") && pointer != "/" {
		return append(out, r.pointerTargets(decodeFragment(pointer))...)
	}
	// An empty fragment, or a lone '/', names the document the resolver holds,
	// which is always the root now that no $defs lookup hands it another: the
	// root is no schema, so it lands nowhere.
	return out
}

// pointerTargets is every node the pointer names. The resolver reads a plain
// pointer from the root; only a "#/$defs/..." pointer outside the parsed model
// — under a raw node the resolver unmarshals as a standalone schema, whose
// references load does not hold — is read by the resolver's own lookup, and
// for that one every node holding the path is kept.
func (r *reach) pointerTargets(pointer string) []*yaml.Node {
	tokens := pointerTokens(pointer)
	if len(tokens) == 0 {
		return nil
	}
	var starts []*yaml.Node
	if defs.IsPointer(jsontext.Pointer(pointer)) {
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
