// Package nodeview reads a YAML mapping the way the resolver will: through
// aliases, through `<<` merge keys, and with duplicate keys resolved the way the
// parser resolves them.
//
// It is separate from the scans that first needed it because the schema lowering
// needs the same view — a `$dynamicAnchor` lookup and an anchor walk both read
// mappings the raw yaml.Node tree does not present directly. A view over source
// text is neither a scan nor a lowering, so it sits below both.
package nodeview

import (
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
)

// maxAliasChain bounds how many alias hops Deref follows. yaml.v3 resolves an
// alias to its anchor, so a chain this long cannot come from a parsed document —
// the bound is what keeps the walk terminating without relying on that, per the
// bounded-recursion rule.
//
// It is this package's own rather than the cycle scan's, which happens to use
// the same number for its descents: the scan sits above this package and a bound
// borrowed upward would invert the dependency.
const maxAliasChain = 10000

// MergeDepthLimit bounds how deep a chain of `<<` merge keys the mapping view
// expands. It is far tighter than maxCycleDepth: each merge level
// re-materializes every pair beneath it, so expanding a chain of depth d costs
// O(d²), and real specs nest merge keys one or two levels deep. A chain that
// hits the bound is reported via a diag.CycleScanFailed warning (see
// View.expand and refCycles), not silently truncated.
const MergeDepthLimit = 64

// maxCachedPairs bounds total expanded pairs one View retains, roughly
// 50 MB at 2²¹. It complements MergeDepthLimit: that bound caps one mapping's
// expansion depth, this one caps a document with many merged mappings. Past the
// budget the view still answers correctly — it just stops memoizing, trading a
// cache hit for a recomputation.
const maxCachedPairs = 1 << 21

// DocumentRoot returns the effective root node to scan: the content of a
// document node, or the node itself otherwise. It returns nil for an empty
// document.
func DocumentRoot(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		return n.Content[0]
	}
	return n
}

// Pair is one effective key/value pair of a mapping node, after alias and
// merge-key resolution.
type Pair struct {
	Key string
	Val *yaml.Node
}

// View reads a raw yaml.Node tree the way speakeasy's unmarshaller reads
// it: alias keys and values dereferenced, `<<` merge keys expanded
// (yml.ResolveAlias and yml.ResolveMergeKeys, applied per mapping in
// marshaller/unmarshaller.go). Every gap between the raw tree this scan reads
// and the resolved one speakeasy's resolver reads is a cycle that reaches the
// resolver and faults the process (GitHub #26) — so every mapping read in this
// file goes through a View.
//
// It memoizes each mapping's expansion for the scan's lifetime: without that, a
// merge chain costs O(n) per expansion and O(n) expansions per walk, going
// cubic in chain length — a hang where the bug being fixed was a crash. A
// cached expansion is always the depth-0 expansion, independent of the path
// that first reached it. MergeDepthLimit and maxCachedPairs bound the chain
// depth and cache size respectively, so unlimited memoization can't trade the
// crash for exhausted memory instead.
type View struct {
	pairs       map[*yaml.Node][]Pair
	cachedPairs int
	inFlight    map[*yaml.Node]bool
	exhausted   bool
}

// Exhausted reports whether the view stopped short of a full expansion at its
// merge-depth bound. A caller that refuses a document on incomplete information
// needs to say so rather than report a clean scan, which is the one thing the
// bound must not be allowed to hide.
func (v *View) Exhausted() bool { return v.exhausted }

// New returns an empty view; a view must not outlive the node tree whose
// expansions it caches.
func New() *View {
	return &View{
		pairs:    map[*yaml.Node][]Pair{},
		inFlight: map[*yaml.Node]bool{},
	}
}

// MappingPairs returns the effective pairs of a mapping node, following
// speakeasy's precedence: an explicit key beats one from a merge regardless of
// where the `<<` appears, an earlier merge source beats a later one on a
// shared key (yml.resolveMergeKeys), and a key repeated explicitly resolves to
// its last value.
//
// n is dereferenced, so an alias standing in for a whole mapping can be passed
// directly; a non-mapping node (including nil) yields no pairs. The returned
// slice is the view's own memo — callers must treat it as read-only.
func (v *View) MappingPairs(n *yaml.Node) []Pair {
	pairs, _ := v.expand(Deref(n), 0)
	return pairs
}

// expand returns n's effective pairs and whether the expansion is complete —
// false if a merge cycle was broken or MergeDepthLimit was reached. Only a
// complete expansion is memoized: caching an incomplete one could make one
// traversal order silently lose a $ref another would find.
//
// Truncation is not contagious — only the node that hit the bound is refused,
// every other mapping still expands in full — because truncation only ever
// drops pairs, never invents an edge. Letting one over-deep chain disable the
// whole view would let an attacker disable the scan by prefixing a document
// with one; refCycles records the fact via the exhausted flag instead.
//
// An incomplete expansion entered at depth 0 is memoized anyway, because at
// depth 0 nothing is in flight and the result is a deterministic function of n
// alone (unlike inside a chain, where how much survived depends on the entry
// depth). Every walk-level read enters at depth 0, so this is what stops a
// truncated chain from re-expanding once per node that references it.
//
// The in-flight (merge-cycle) case needs no bound of its own: it requires an
// alias to an ancestor, which anchorCycle already refuses before refCycles runs.
func (v *View) expand(n *yaml.Node, depth int) ([]Pair, bool) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, true
	}
	if cached, ok := v.pairs[n]; ok {
		return cached, true
	}
	if v.inFlight[n] {
		return nil, false
	}
	if depth > MergeDepthLimit {
		v.exhausted = true
		return nil, false
	}

	v.inFlight[n] = true
	pairs, complete := v.expandContent(n, depth)
	delete(v.inFlight, n)

	if complete || v.isEntryPoint(depth) {
		v.memoize(n, pairs)
	}
	return pairs, complete
}

// isEntryPoint reports whether an expansion that just finished at this depth was
// the outermost one, with no other expansion of the same view in flight around
// it — the condition under which even a truncated result is reproducible.
func (v *View) isEntryPoint(depth int) bool {
	return depth == 0 && len(v.inFlight) == 0
}

// memoize retains a complete expansion while the view's pair budget allows.
// Declining to cache costs a recomputation and nothing else — the cache is pure
// memoization, so a miss recomputes exactly the same pairs — which makes the
// budget a memory bound the scan can enforce without touching what it reports.
func (v *View) memoize(n *yaml.Node, pairs []Pair) {
	if v.cachedPairs+len(pairs) > maxCachedPairs {
		return
	}
	v.pairs[n] = pairs
	v.cachedPairs += len(pairs)
}

// expandContent splits a mapping's raw content into the pairs it declares itself
// and the pairs its `<<` keys merge in, then applies the two precedence rules
// that govern them. They point in opposite directions, so they cannot share one
// pass: a repeated explicit key resolves to its last value, while a merged key
// yields to an explicit one and to any earlier merge source.
func (v *View) expandContent(n *yaml.Node, depth int) ([]Pair, bool) {
	var explicit, merged []Pair
	complete := true

	for i := 0; i+1 < len(n.Content); i += 2 {
		raw, val := n.Content[i], Deref(n.Content[i+1])
		if IsMergeKey(raw) {
			got, ok := v.mergeSource(val, depth+1)
			merged = append(merged, got...)
			complete = complete && ok
			continue
		}
		key := Deref(raw)
		if key == nil || key.Kind != yaml.ScalarNode {
			continue // a non-scalar key (after Deref) cannot name a schema keyword
		}
		explicit = append(explicit, Pair{Key: key.Value, Val: val})
	}

	return appendUnseen(dedupeLastWins(explicit), merged), complete
}

// dedupeLastWins keeps the last pair for each key, at that last occurrence's
// position, matching speakeasy: a mapping that repeats a key is ill-formed, but
// speakeasy neither refuses it nor keeps the first one — it unmarshals every
// occurrence in turn, so the final value is what the resolver then works from.
func dedupeLastWins(pairs []Pair) []Pair {
	last := make(map[string]int, len(pairs))
	for i, p := range pairs {
		last[p.Key] = i
	}
	out := make([]Pair, 0, len(last))
	for i, p := range pairs {
		if last[p.Key] == i {
			out = append(out, p)
		}
	}
	return out
}

// appendUnseen appends the pairs of add whose key is not already present,
// keeping the first contributor of each — the rule for merged keys, which yield
// both to an explicit key and to an earlier merge source.
func appendUnseen(base, add []Pair) []Pair {
	if len(add) == 0 {
		return base
	}
	seenKey := make(map[string]bool, len(base)+len(add))
	for _, p := range base {
		seenKey[p.Key] = true
	}
	out := base
	for _, p := range add {
		if seenKey[p.Key] {
			continue
		}
		seenKey[p.Key] = true
		out = append(out, p)
	}
	return out
}

// mergeSource expands one `<<` value into the pairs it contributes: a mapping is
// a single merge source, a sequence is several with an earlier element taking
// precedence over a later one on a shared key.
func (v *View) mergeSource(val *yaml.Node, depth int) ([]Pair, bool) {
	if val == nil || val.Kind != yaml.SequenceNode {
		return v.expand(val, depth)
	}
	var out []Pair
	complete := true
	for _, item := range val.Content {
		got, ok := v.expand(Deref(item), depth)
		out = append(out, got...)
		complete = complete && ok
	}
	return dedupeFirstWins(out), complete
}

// MergeTag is the tag yaml.v3 resolves every `<<` merge key to, and the exact
// tag speakeasy's yml.IsMergeKey requires before treating one as a merge.
const MergeTag = "!!merge"

// IsMergeKey reports whether a raw mapping key node is a `<<` merge key,
// applying the same test speakeasy does: yml.IsMergeKey (yml/yml.go), run over
// every mapping via yml.ResolveMergeKeys. The key is checked undereferenced (an
// alias standing in for the key is not a scalar) and by resolved tag (a quoted
// '<<' resolves to !!str) — speakeasy treats both as ordinary keys, and
// expanding them would invent pairs it never sees.
//
// yaml.v3's own decoder (isMerge in decode.go) is the wrong model to copy: it's
// laxer about the tag (also accepts an empty or non-specific one) and stricter
// about repetition (honors only the last `<<`, where speakeasy merges every
// one — why expandContent accumulates them all). Neither difference is
// reachable from a parsed document today, but re-check this against
// yml.IsMergeKey on any dependency bump.
func IsMergeKey(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.Value == "<<" && n.Tag == MergeTag
}

// dedupeFirstWins keeps only the first pair for each key, preserving order — the
// rule across the elements of a merge sequence, where an earlier source outranks
// a later one.
func dedupeFirstWins(pairs []Pair) []Pair {
	return appendUnseen(nil, pairs)
}

// PureRefTarget reports the internal $ref target of a node that carries a
// top-level internal ('#/...') $ref. Sibling keys do not disqualify it:
// speakeasy follows a node's top-level $ref before any concrete sibling, so a
// $ref node with a type or properties sibling still drives the crash. The chain
// terminates only at a node with no top-level $ref at all.
func (v *View) PureRefTarget(n *yaml.Node) (string, bool) {
	return PureRefTargetOf(v.MappingPairs(n))
}

// PureRefTargetOf is PureRefTarget over an already-expanded pair list, so a
// caller that needs both the pairs and the target expands the mapping once.
func PureRefTargetOf(pairs []Pair) (string, bool) {
	for _, p := range pairs {
		if p.Key != "$ref" {
			continue
		}
		if p.Val == nil || p.Val.Kind != yaml.ScalarNode || !strings.HasPrefix(p.Val.Value, "#/") {
			return "", false
		}
		return p.Val.Value, true
	}
	return "", false
}

// ResolvePointer resolves an internal JSON pointer ('#/a/b') against the root
// node, returning the targeted node or nil when the path does not exist. Alias
// nodes along the path are dereferenced so navigation follows structure.
func (v *View) ResolvePointer(root *yaml.Node, ref string) *yaml.Node {
	cur := Deref(root)
	for raw := range strings.SplitSeq(strings.TrimPrefix(ref, "#"), "/") {
		if raw == "" {
			continue
		}
		cur = v.ChildByToken(Deref(cur), ids.UnescapeSegment(raw))
		if cur == nil {
			return nil
		}
	}
	return Deref(cur)
}

// ChildByToken returns the child of a mapping (by key) or sequence (by index)
// node named by one JSON pointer token, or nil when absent. The mapping arm
// reads through the view, so pointer navigation resolves an alias key and an
// aliased or merged value exactly as PureRefTarget does.
func (v *View) ChildByToken(n *yaml.Node, token string) *yaml.Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		for _, p := range v.MappingPairs(n) {
			if p.Key == token {
				return p.Val
			}
		}
	case yaml.SequenceNode:
		idx, err := strconv.Atoi(token)
		if err != nil || idx < 0 || idx >= len(n.Content) {
			return nil
		}
		return n.Content[idx]
	}
	return nil
}

// Deref follows AliasNode links to the anchored node, bounded against an alias
// chain that loops (the anchor-cycle detector reports those separately).
func Deref(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && n.Kind == yaml.AliasNode && i <= maxAliasChain; i++ {
		n = n.Alias
	}
	return n
}
