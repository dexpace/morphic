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
	"encoding/json/jsontext"
	"net/url"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
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

// maxPointerSegments bounds how many tokens one JSON pointer walk follows. A
// pointer names a position in the document, so a real one is a handful of
// segments deep; the bound is what keeps the walk terminating on a pointer built
// to be long rather than to name anything, per the bounded-recursion rule.
const maxPointerSegments = 1024

// MergeDepthLimit bounds how deep a chain of `<<` merge keys the mapping view
// expands. It is far tighter than maxAliasChain: each merge level
// re-materializes every pair beneath it, so expanding a chain of depth d costs
// O(d²), and real specs nest merge keys one or two levels deep. A chain that
// hits the bound is reported via a diag.CycleScanFailed warning (see
// View.expand and refCycles), not silently truncated.
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
// over-deep chain disable the view would let a document disable the scan by
// opening with one; refCycles records it through the exhausted flag instead.
func (v *View) expand(n *yaml.Node, depth int) expansion {
	if n == nil || n.Kind != yaml.MappingNode {
		return expansion{complete: true}
	}
	if cached, ok := v.pairs[n]; ok && v.serves(cached, depth) {
		return cached
	}
	// A merge cycle needs no bound of its own: it requires an alias to an
	// ancestor, which anchorCycle refuses before refCycles runs.
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
		if IsMergeKey(raw) {
			got := v.mergeSource(val, depth+1)
			merged = append(merged, got.pairs...)
			e.complete = e.complete && got.complete
			e.height = max(e.height, got.height+1)
			continue
		}
		key := Deref(raw)
		if key == nil || key.Kind != yaml.ScalarNode {
			continue // a non-scalar key (after Deref) cannot name a schema keyword
		}
		explicit = append(explicit, Pair{Key: key.Value, Val: val})
	}

	e.pairs = appendUnseen(dedupeLastWins(explicit), merged)
	return e
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

// IsMergeKey reports whether a raw mapping key node is a `<<` merge key, as
// speakeasy's yml.IsMergeKey does. The key is checked undereferenced (an alias
// standing in for it is not a scalar) and by resolved tag (a quoted '<<'
// resolves to !!str); speakeasy treats both as ordinary keys, and expanding
// them would invent pairs it never sees.
//
// yaml.v3's isMerge (decode.go) is the wrong model: it accepts an empty or
// non-specific tag and honors only the last `<<`, where speakeasy merges every
// one (hence expandContent accumulates them). Neither difference is reachable
// from a parsed document today; re-check on a dependency bump.
func IsMergeKey(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.Value == "<<" && n.Tag == ynode.MergeTag
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
func (v *View) PureRefTarget(n *yaml.Node) (jsontext.Pointer, bool) {
	// Through the index where the walk already built one. This runs on every node
	// a pointer descended, immediately after the walk that descended it, so a
	// scan here re-reads exactly the mappings ChildByToken just stopped scanning
	// — leaving the quadratic the index removes standing in its sibling.
	if index := v.keys[n]; index != nil {
		return pureRefFrom(index["$ref"])
	}
	return PureRefTargetOf(v.MappingPairs(n))
}

// PureRefTargetOf is PureRefTarget over an already-expanded pair list, so a
// caller that needs both the pairs and the target expands the mapping once. The
// target is normalized by InternalPointer, so it is a bare pointer ('/a/b'),
// not the '#/a/b' the source spells.
func PureRefTargetOf(pairs []Pair) (jsontext.Pointer, bool) {
	for _, p := range pairs {
		if p.Key != "$ref" {
			continue
		}
		return pureRefFrom(p.Val)
	}
	return "", false
}

// pureRefFrom is the decision both readings share, once the value written at
// `$ref` is in hand: a key that is absent and one whose value is not a scalar
// are the same answer, so reading the index cannot part company with the scan.
func pureRefFrom(val *yaml.Node) (jsontext.Pointer, bool) {
	if val == nil || val.Kind != yaml.ScalarNode {
		return "", false
	}
	return InternalPointer(val.Value)
}

// InternalPointer reports the JSON pointer a $ref value names inside this
// document, and whether the resolver walks it as a pointer into this
// document.
//
// It mirrors the resolver (references/reference.go GetURI and GetJSONPointer,
// v1.24.0): split on '#', trim both halves, percent-decode the pointer; an
// empty URI half names this document. A raw read would hide a resolvable
// pointer from the cycle scan, such as '#/paths/~1a ' with a trailing space.
// Re-check on a dependency bump.
//
// A fragment not starting with '/' is refused: `#name` is a $anchor (GitHub
// #523, #526). A bare '#' names the root. One decoding to non-UTF-8 bytes is
// kept, unlike resolve.FragmentPointer's refusal (GitHub #520).
func InternalPointer(ref string) (jsontext.Pointer, bool) {
	parts := strings.Split(ref, "#")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) != "" {
		return "", false // no fragment, or a fragment in another document
	}
	pointer := strings.TrimSpace(parts[1])
	if decoded, err := url.QueryUnescape(pointer); err == nil {
		pointer = decoded
	}
	// A `#name` fragment is a $anchor the resolver looks up by name, not a
	// pointer it walks. Walking the name from the root as a key refused a
	// document as a cycle the resolver never enters (GitHub #523), and a chain
	// the resolver does follow through the lookup is one the scan cannot see
	// (GitHub #526). A bare '#' stays: it names the root.
	if pointer != "" && !strings.HasPrefix(pointer, "/") {
		return "", false // a $anchor name or other non-pointer fragment
	}
	// Kept for non-UTF-8 bytes too: no document key spells them, but the resolver
	// walks every token before the one it cannot find, and a walk through a
	// reference already on the chain is the re-entrant hop the scan refuses
	// whether or not the pointer then resolves.
	return jsontext.Pointer(pointer), true
}

// PointerPath walks a normalized JSON pointer (as InternalPointer returns it)
// against root, returning the root and each node reached, aliases dereferenced.
// complete reports whether every token was followed; if not, the path ends at
// the last node reached.
//
// It returns the whole path because speakeasy resolves a reference holding its
// own lock and read-locks each reference the walk passes through, so a pointer
// through one already being resolved deadlocks before arriving (v1.24.0).
//
// '/a/' has tokens "a" and "" (RFC 6901 §3). Dropping the empty token would
// make a node the walk passes through its destination, which the caller's
// re-entrancy check exempts (GitHub #238).
func (v *View) PointerPath(root *yaml.Node, pointer jsontext.Pointer) (path []*yaml.Node, complete bool) {
	return v.walkPointer(root, pointer, tokenless(pointer))
}

// DocumentPath walks a pointer that names a position in this document rather
// than a reference some source wrote, and is otherwise PointerPath.
//
// They differ on '/'. PointerPath lands it on the root, as the resolver does, a
// departure from RFC 6901 that tokenless records. ids.Ptr("") spells the root
// member whose key is the empty string as '/', so here only the empty pointer
// names the root. Taking '/' for the root would hide an $id written on that
// member from a caller reading $id down a path.
func (v *View) DocumentPath(root *yaml.Node, pointer jsontext.Pointer) (path []*yaml.Node, complete bool) {
	return v.walkPointer(root, pointer, pointer == "")
}

// walkPointer is the shared walk; atRoot says whether pointer carries no tokens
// at all, which is the one question the two readings answer differently.
func (v *View) walkPointer(root *yaml.Node, pointer jsontext.Pointer, atRoot bool) (path []*yaml.Node, complete bool) {
	cur := Deref(root)
	if cur == nil {
		return nil, false
	}
	path = append(path, cur)
	if atRoot {
		return path, true
	}

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

// tokenless reports whether a pointer carries no reference tokens at all, so it
// names the root. Two spellings do, and the resolver lands on the root for both
// (v1.24.0): the empty pointer, which is how a bare '#' reaches here and which
// references/resolution.go resolveAgainstDocument short-circuits to the root
// document, and a lone '/'. The second is a deliberate departure from RFC 6901,
// which reads '/' as one empty token — getNavigationStack special-cases it to an
// empty navigation stack, and this walk models what the resolver walks rather
// than what the grammar admits.
func tokenless(pointer jsontext.Pointer) bool {
	return pointer == "" || pointer == "/"
}

// ChildByToken returns the child of a mapping (by key) or sequence (by index)
// node named by one JSON pointer token, or nil when absent. The mapping arm
// reads through the view, so pointer navigation resolves an alias key and an
// aliased or merged value exactly as PureRefTarget does.
//
// n itself is not dereferenced, unlike MappingPairs and PureRefTarget: an alias
// handed here matches neither arm and answers nil. Every caller walks with a
// dereference at each hop, as PointerPath does, so the difference is
// unreachable; it is written down because those siblings promise the opposite.
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
// where the costs roughly meet; BenchmarkPointerPath_IntoAWideMapping carries
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
