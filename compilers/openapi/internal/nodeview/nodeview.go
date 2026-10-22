// Package nodeview reads a YAML mapping the way the resolver will: through
// aliases, through `<<` merge keys, and with duplicate keys resolved the way the
// parser's model resolves them (docs/parser-library.md#duplicate-key).
//
// It is separate from the scans that first needed it because the schema lowering
// needs the same view — a `$dynamicAnchor` lookup and an anchor walk both read
// mappings the raw yaml.Node tree does not present directly. A view over source
// text is neither a scan nor a lowering, so it sits below both.
package nodeview

import (
	"encoding/json/jsontext"
	"strconv"

	"github.com/speakeasy-api/openapi/yml"
	yaml "gopkg.in/yaml.v3"
)

// maxAliasChain bounds how many alias hops Deref follows. yaml.v3 resolves an
// alias to its anchor, so a chain this long cannot come from a parsed document —
// the bound is what keeps the walk terminating without relying on that, per the
// bounded-recursion rule.
//
// It is this package's own: a bound borrowed from a package above this one
// would invert the dependency.
const maxAliasChain = 10000

// maxPointerSegments bounds how many tokens one JSON pointer walk follows. A
// pointer names a position in the document, so a real one is a handful of
// segments deep; the bound is what keeps the walk terminating on a pointer built
// to be long rather than to name anything, per the bounded-recursion rule.
const maxPointerSegments = 1024

// MergeDepthLimit bounds how deep a chain of `<<` merge keys the mapping view
// expands. It is far tighter than maxAliasChain: each merge level
// re-materializes every pair beneath it, so expanding a chain of depth d costs
// O(d²), and real specs nest merge keys one or two levels deep. A chain that
// hits the bound is not silently truncated: View.Exhausted says so, and each
// reader the bound stops reports it.
const MergeDepthLimit = 64

// maxCachedPairs bounds the total expanded pairs one View retains, about 50 MB
// at 2²¹ for the pairs themselves. MergeDepthLimit caps one mapping's expansion
// depth; this caps a document with many merged mappings. Past it the view still
// answers correctly, only without memoizing.
//
// It also bounds the key indexes without charging them: an index exists only
// for a mapping whose pairs were retained and holds one entry per pair. The
// bound is a count and a map entry costs more than a Pair, so the memory
// ceiling with indexes is a multiple of the figure above. See keyIndex.
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

// View reads a raw yaml.Node tree as speakeasy's unmarshaller does,
// dereferencing aliases and expanding `<<` merge keys (yml.ResolveAlias,
// yml.ResolveMergeKeys). A cycle the scan misses because it reads a different
// tree than the resolver faults the process (GitHub #26), so every mapping read
// in this file goes through a View.
//
// It memoizes each mapping's expansion, since otherwise a merge chain goes
// cubic. The memo is a pure cache, answering as a fresh view would at the same
// depth, so one view can be shared across walks (see expansion and
// View.serves). MergeDepthLimit and maxCachedPairs bound it. keyIndex adds key
// maps for pointer walks.
type View struct {
	pairs       map[*yaml.Node]expansion
	keys        map[*yaml.Node]map[string]*yaml.Node
	cachedPairs int
	inFlight    map[*yaml.Node]bool
	exhausted   bool
}

// Exhausted reports whether the view stopped short of a full expansion at its
// merge-depth bound. A caller that refuses a document on incomplete information
// needs to say so rather than report a clean scan, which is the one thing the
// bound must not be allowed to hide.
func (v *View) Exhausted() bool { return v.exhausted }

// Expanded reports whether n's pairs are all its `<<` merges supply, read as a
// pointer walk reads them: false for a mapping whose merges run past
// MergeDepthLimit, where a key it lacks may lie past the bound. A node that is
// no mapping has nothing to expand.
func (v *View) Expanded(n *yaml.Node) bool {
	return v.expand(Deref(n), 0).complete
}

// New returns an empty view; a view must not outlive the node tree whose
// expansions it caches.
func New() *View {
	// keys is left nil: most views never index anything, and keyIndex allocates
	// it on the first mapping wide enough to earn one.
	return &View{
		pairs:    map[*yaml.Node]expansion{},
		inFlight: map[*yaml.Node]bool{},
	}
}

// expansion is one mapping's effective pairs with what the memo needs to serve
// them again: how deep the expansion reached, and whether it reached
// everything.
//
// height is the longest chain of `<<` merges beneath the node, 0 for a mapping
// that merges nothing. The depth bound truncates from the entry point down, so
// an entry computed from one read answers another only when the whole chain
// still fits (GitHub #404).
type expansion struct {
	pairs    []Pair
	height   int
	complete bool
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
	return v.expand(Deref(n), 0).pairs
}

// expand returns n's expansion, incomplete if a merge cycle was broken or
// MergeDepthLimit was reached. Only a complete expansion is memoized: caching
// an incomplete one could make one traversal order lose a $ref another would
// find. serves decides which reads a memoized entry may answer.
//
// Truncation is not contagious: only the node that hit the bound is refused,
// since truncation only drops pairs and never invents an edge. Letting one
// over-deep chain disable the view would let a document disable every reader
// by opening with one; the exhausted flag records it instead (Exhausted).
func (v *View) expand(n *yaml.Node, depth int) expansion {
	if n == nil || n.Kind != yaml.MappingNode {
		return expansion{complete: true}
	}
	if cached, ok := v.pairs[n]; ok && v.serves(cached, depth) {
		return cached
	}
	// A merge cycle needs no bound of its own: it requires an alias to an
	// ancestor, which the alias pre-scan refuses before a compile's views read
	// the tree. Detection reads the root before that scan, and inFlight closes
	// a cycle there having read every mapping on it.
	if v.inFlight[n] {
		return expansion{}
	}
	if depth > MergeDepthLimit {
		v.exhausted = true
		return expansion{}
	}

	v.inFlight[n] = true
	e := v.expandContent(n, depth)
	delete(v.inFlight, n)

	// An incomplete result is memoized at an entry point too: nothing is in
	// flight, so it depends on n alone, and a truncated chain is not
	// re-expanded once per node that references it.
	if e.complete || v.isEntryPoint(depth) {
		v.memoize(n, e)
	}
	return e
}

// serves reports whether a memoized expansion is the answer a fresh view would
// give a read at this depth, which is the only condition under which the memo
// may answer instead of expanding.
//
// A complete entry expanded its whole chain, so it is the answer wherever that
// chain still fits under the bound; from deeper than that a fresh read would
// truncate, and the memo must not hide the truncation. An incomplete entry was
// kept only because it was an entry point, and an entry point is the one read
// it can stand in for.
func (v *View) serves(e expansion, depth int) bool {
	if !e.complete {
		return v.isEntryPoint(depth)
	}
	return depth+e.height <= MergeDepthLimit
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
func (v *View) memoize(n *yaml.Node, e expansion) {
	if v.cachedPairs+len(e.pairs) > maxCachedPairs {
		return
	}
	v.pairs[n] = e
	v.cachedPairs += len(e.pairs)
}

// expandContent splits a mapping's raw content into the pairs it declares itself
// and the pairs its `<<` keys merge in, then applies the two precedence rules
// that govern them. They point in opposite directions, so they cannot share one
// pass: a repeated explicit key resolves to its last value, while a merged key
// yields to an explicit one and to any earlier merge source.
//
// The height it records is one more than the tallest merge source's, so a
// mapping that merges nothing has height 0.
func (v *View) expandContent(n *yaml.Node, depth int) expansion {
	var explicit, merged []Pair
	e := expansion{complete: true}

	for i := 0; i+1 < len(n.Content); i += 2 {
		raw, val := n.Content[i], Deref(n.Content[i+1])
		// The library's own predicate, so the view and the unmarshaller cannot
		// disagree about which key merges. It is handed the key unresolved, as the
		// library hands it: an alias standing in for `<<` is an ordinary key. Every
		// `<<` merges, not only the last as in yaml.v3's decoder.
		if yml.IsMergeKey(raw) {
			got := v.mergeSource(val, depth+1)
			merged = append(merged, got.pairs...)
			e.complete = e.complete && got.complete
			e.height = max(e.height, got.height+1)
			continue
		}
		key, own := KeyOf(raw)
		if !own {
			continue // a non-scalar key cannot name a schema keyword
		}
		explicit = append(explicit, Pair{Key: key, Val: val})
	}

	e.pairs = appendUnseen(dedupeLastWins(explicit), merged)
	return e
}

// KeyOf returns the key the parser gives the mapping pair whose raw key node is
// raw, and whether the pair is one of the mapping's own: a `<<` merge key
// names none, nor does a key that is no scalar once its alias is followed. The
// view and every reader of a mapping's raw pairs ask it, so none can disagree
// with the parser about a pair's key.
func KeyOf(raw *yaml.Node) (string, bool) {
	if yml.IsMergeKey(raw) {
		return "", false
	}
	key := Deref(raw)
	if key == nil || key.Kind != yaml.ScalarNode {
		return "", false
	}
	return key.Value, true
}

// RepeatedKeys returns the keys a mapping writes more than once, each once, in
// the order their second copies appear. A key an explicit pair shares with a
// `<<` merge is the merge's precedence rule at work, not a repeat, so merged
// pairs are not counted. n is dereferenced; a node that is no mapping repeats
// nothing.
func RepeatedKeys(n *yaml.Node) []string {
	n = Deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	copies := make(map[string]int, len(n.Content)/2)
	var out []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, own := KeyOf(n.Content[i])
		if !own {
			continue
		}
		if copies[key]++; copies[key] == 2 {
			out = append(out, key)
		}
	}
	return out
}

// dedupeLastWins keeps the last pair for each key, at that last occurrence's
// position: a mapping that repeats a key is ill-formed, and the parser's model
// decodes its last occurrence, with only a warning
// (docs/parser-library.md#duplicate-key).
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
//
// The height of a sequence is its tallest element's: the elements are
// alternatives at one level, not links in a chain.
func (v *View) mergeSource(val *yaml.Node, depth int) expansion {
	if val == nil || val.Kind != yaml.SequenceNode {
		return v.expand(val, depth)
	}
	var out []Pair
	e := expansion{complete: true}
	for _, item := range val.Content {
		got := v.expand(Deref(item), depth)
		out = append(out, got.pairs...)
		e.complete = e.complete && got.complete
		e.height = max(e.height, got.height)
	}
	e.pairs = dedupeFirstWins(out)
	return e
}

// dedupeFirstWins keeps only the first pair for each key, preserving order — the
// rule across the elements of a merge sequence, where an earlier source outranks
// a later one.
func dedupeFirstWins(pairs []Pair) []Pair {
	return appendUnseen(nil, pairs)
}

// DocumentPath walks pointer, a position in this document, from root as RFC
// 6901 reads it, returning the root and each node reached, aliases
// dereferenced. complete reports whether every token was followed; if not, the
// path ends at the last node reached. A lone '/' names the root's member keyed
// "", as ids.Ptr("") spells it, so a caller reading $id down a path sees one
// written there.
func (v *View) DocumentPath(root *yaml.Node, pointer jsontext.Pointer) (path []*yaml.Node, complete bool) {
	cur := Deref(root)
	if cur == nil {
		return nil, false
	}
	path = append(path, cur)
	segments := 0
	for token := range pointer.Tokens() {
		segments++
		if segments > maxPointerSegments {
			return path, false
		}
		cur = Deref(v.ChildByToken(cur, token))
		if cur == nil {
			return path, false
		}
		path = append(path, cur)
	}
	return path, true
}

// ChildByToken returns the child of a mapping (by key) or sequence (by index)
// node named by one JSON pointer token, or nil when absent. The mapping arm
// reads through the view, so pointer navigation resolves an alias key and an
// aliased or merged value as MappingPairs reads them.
//
// n itself is not dereferenced, unlike MappingPairs: an alias handed here
// matches neither arm and answers nil. Every caller walks with a dereference at
// each hop, as DocumentPath does, so the difference is unreachable; it is
// written down because that sibling promises the opposite.
func (v *View) ChildByToken(n *yaml.Node, token string) *yaml.Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		return v.mappingChild(n, token)
	case yaml.SequenceNode:
		idx, err := strconv.Atoi(token)
		if err != nil || idx < 0 || idx >= len(n.Content) {
			return nil
		}
		return n.Content[idx]
	}
	return nil
}

// mappingChild answers one key of a mapping through the key index, falling back
// to a scan of its pairs for a mapping the index declines to cover.
//
// The built index is read before the pairs are, because on the path this exists
// to speed up they are the same answer: re-deriving the pairs first would spend
// a Deref and a memo lookup to reach a map read that never needed them.
//
// n is known to be a mapping node here, so it is its own Deref and keys the
// index under the same node MappingPairs memoizes the pairs under.
func (v *View) mappingChild(n *yaml.Node, token string) *yaml.Node {
	// Read without asking serves: every read of an index follows a pointer walk,
	// which enters at depth 0, and there the memo entry it projects is always
	// the answer. The index holds no state of its own to differ from it.
	if index := v.keys[n]; index != nil {
		return index[token]
	}
	pairs := v.MappingPairs(n)
	if index := v.keyIndex(n, pairs); index != nil {
		return index[token]
	}
	for _, p := range pairs {
		if p.Key == token {
			return p.Val
		}
	}
	return nil
}

// minIndexedPairs is the width below which a mapping is scanned rather than
// indexed.
//
// An index costs a map allocation and an insert per pair to save a comparison
// per pair per later read, so a narrow or rarely read mapping never repays it.
// The wide ones a walk revisits are the few a components block holds. 16 is
// where the costs roughly meet; BenchmarkDocumentPath_IntoAWideMapping carries
// the widths that show it.
//
// Width says nothing about reuse: declaresResourceIDAbove builds a view per
// call and reads each node once, so indexing there cost time and allocations
// for nothing. keyIndex adds a reuse condition.
const minIndexedPairs = 16

// keyIndex returns n's expansion as a key map, built on the second read of a
// mapping, or nil for a mapping it does not index.
//
// Without it, resolving R references into a components mapping of M entries
// scans R×M pairs, quadratic in the document's own size. A key map answers as
// the scan does because expandContent yields each key once.
//
// The first read of a mapping only leaves a nil marker in v.keys, so an index
// exists only where a walk came back. Indexes hold at most maxCachedPairs
// entries between them, and markers at most maxCachedPairs/minIndexedPairs.
func (v *View) keyIndex(n *yaml.Node, pairs []Pair) map[string]*yaml.Node {
	if len(pairs) < minIndexedPairs {
		return nil
	}
	// Gate on the memo itself, not a copy of memoize's budget test, so this keeps
	// tracking memoize; at depth 0 the two select the same mappings. The memo
	// bounds the indexes rather than being charged for them, which would halve
	// a memo that keeps merge chains from going cubic.
	if _, memoized := v.pairs[n]; !memoized {
		return nil
	}
	if _, seen := v.keys[n]; !seen {
		if v.keys == nil {
			v.keys = map[*yaml.Node]map[string]*yaml.Node{}
		}
		// A nil entry is a marker, never an empty index: only a mapping of at
		// least minIndexedPairs is stored.
		v.keys[n] = nil // read once; the next read is what earns an index
		return nil
	}

	// Only a marker reaches here: callers read a built index from v.keys first.
	index := make(map[string]*yaml.Node, len(pairs))
	for _, p := range pairs {
		index[p.Key] = p.Val
	}
	v.keys[n] = index
	return index
}

// Deref follows AliasNode links to the anchored node, bounded against an alias
// chain that loops (the anchor-cycle detector reports those separately).
func Deref(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && n.Kind == yaml.AliasNode && i <= maxAliasChain; i++ {
		n = n.Alias
	}
	return n
}
