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
	"net/url"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
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

// maxCachedPairs bounds total expanded pairs one View retains, on the order of
// 50 MB at 2²¹ for the pairs themselves — more with the key indexes below, whose
// entries cost more apiece than a Pair does. It complements MergeDepthLimit: that bound caps one mapping's
// expansion depth, this one caps a document with many merged mappings. Past the
// budget the view still answers correctly — it just stops memoizing, trading a
// cache hit for a recomputation.
//
// It bounds the key index beside the pairs, without being charged for it twice:
// an index exists only for a mapping whose pairs this retained and holds one
// entry per pair, so what every index holds together is bounded by what this
// already caps. See keyIndex.
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
//
// It memoizes one thing more, for the walk rather than the expansion: keyIndex
// projects a memoized mapping into a key map, so descending a JSON pointer costs
// a map read per token instead of a scan of every pair at each one.
type View struct {
	pairs       map[*yaml.Node][]Pair
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
func (v *View) PureRefTarget(n *yaml.Node) (string, bool) {
	// Through the index where the walk already built one. This runs on every node
	// a pointer descended, immediately after the walk that descended it, so a
	// scan here re-reads exactly the mappings ChildByToken just stopped scanning
	// — leaving the quadratic the index removes standing in its sibling.
	if index, built := v.keys[n]; built {
		return pureRefFrom(index["$ref"])
	}
	return PureRefTargetOf(v.MappingPairs(n))
}

// PureRefTargetOf is PureRefTarget over an already-expanded pair list, so a
// caller that needs both the pairs and the target expands the mapping once. The
// target is normalized by InternalPointer, so it is a bare pointer ('/a/b'),
// not the '#/a/b' the source spells.
func PureRefTargetOf(pairs []Pair) (string, bool) {
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
func pureRefFrom(val *yaml.Node) (string, bool) {
	if val == nil || val.Kind != yaml.ScalarNode {
		return "", false
	}
	return InternalPointer(val.Value)
}

// InternalPointer reports the JSON pointer a $ref value names inside this
// document, and whether it names this document at all.
//
// It mirrors the resolver exactly: speakeasy splits a $ref on '#', treats what
// precedes it as a URI and what follows as the pointer, trims whitespace from
// both, and percent-decodes the pointer (references/reference.go GetURI and
// GetJSONPointer, v1.24.0). A ref whose URI half is empty names this document.
//
// Reading the raw value instead is not a near-enough approximation, it is a hole
// in the cycle scan: a pointer this package calls dangling but the resolver
// resolves is a reference the scan cannot see, and '#/paths/~1a ' — one trailing
// space — is enough to be one. A dependency bump should re-check those two
// methods, as MergeDepthLimit's comment does for the behavior it tracks.
func InternalPointer(ref string) (string, bool) {
	parts := strings.Split(ref, "#")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) != "" {
		return "", false // no fragment, or a fragment in another document
	}
	pointer := strings.TrimSpace(parts[1])
	if decoded, err := url.QueryUnescape(pointer); err == nil {
		pointer = decoded
	}
	return pointer, true
}

// PointerPath walks a normalized internal JSON pointer ('/a/b', as
// InternalPointer returns it) against the root node, keeping every node it
// passes through: element 0 is the root and each later element is the node
// reached by one more token. complete reports whether every token resolved;
// when it is false the walk stopped at the last element returned, and there is
// no destination. Alias nodes along the path are dereferenced so navigation
// follows structure.
//
// It yields the whole path rather than just the target because a pointer's
// danger is not always at its destination. speakeasy resolves a reference while
// holding that reference's own lock and read-locks every reference the pointer
// walk passes through, so a pointer that traverses a reference already being
// resolved deadlocks before it ever arrives (v1.24.0, openapi/reference.go
// resolve/GetObject). A target alone cannot express that.
//
// The leading '/' introduces the first token rather than being one, and every
// later '/' separates two — so '/a/' carries the tokens "a" and "", the second
// naming a member whose key is the empty string (RFC 6901 §3, and
// jsonpointer/navigation.go getNavigationStack, v1.24.0, which reads it the
// same way).
// Dropping the empty token instead is what a pointer's danger being upstream of
// its destination makes unsafe: it turns a node the walk descends *through* into
// the node it stops at, and the caller's re-entrancy check exempts exactly that
// node (GitHub #238).
func (v *View) PointerPath(root *yaml.Node, pointer string) (path []*yaml.Node, complete bool) {
	return v.walkPointer(root, pointer, tokenless(pointer))
}

// DocumentPath walks a pointer that names a position in this document rather
// than a reference some source wrote, and is otherwise PointerPath.
//
// The two part company on '/'. PointerPath lands it on the root because that is
// where the resolver lands it, a departure from RFC 6901 that tokenless records.
// A position built by ids.Ptr carries no such departure: ids.Ptr("") spells the
// root member whose key is the empty string exactly '/', so reading that as the
// root walks past the member the pointer names. Only the empty pointer names the
// root here.
//
// The distinction is load-bearing for a caller reading $id down a path: taking
// '/' for the root hides an $id written on that member, which is the same
// dropped-empty-token loss the rest of this walk exists to avoid.
func (v *View) DocumentPath(root *yaml.Node, pointer string) (path []*yaml.Node, complete bool) {
	return v.walkPointer(root, pointer, pointer == "")
}

// walkPointer is the shared walk; atRoot says whether pointer carries no tokens
// at all, which is the one question the two readings answer differently.
func (v *View) walkPointer(root *yaml.Node, pointer string, atRoot bool) (path []*yaml.Node, complete bool) {
	cur := Deref(root)
	if cur == nil {
		return nil, false
	}
	path = append(path, cur)
	if atRoot {
		return path, true
	}

	segments := 0
	for raw := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
		segments++
		if segments > maxPointerSegments {
			return path, false
		}
		cur = Deref(v.ChildByToken(cur, ids.UnescapeSegment(raw)))
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
func tokenless(pointer string) bool {
	return pointer == "" || pointer == "/"
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
	if index, built := v.keys[n]; built {
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
// An index costs a map allocation and one insert per pair to save a comparison
// per pair per later read, so a mapping narrow enough, or read few enough times,
// never repays it — and nearly every mapping a pointer descends is both. A
// document is mostly narrow mappings: a schema body, a media-type entry, a
// response. The wide ones a walk returns to over and over are the few a
// components block holds, and those are what this admits.
//
// 16 is where the two costs meet closely enough that either side is cheap; the
// benchmark beside this file carries the widths that show it, narrow ones
// included, so a run that regresses at n=2 or n=8 is this gate having stopped
// paying for itself.
//
// The bound is a width rather than a read count because width is known at the
// first read: counting reads would need its own per-node state, and would still
// index a mapping the walk never returns to.
const minIndexedPairs = 16

// keyIndex returns n's expansion as a key map, building it on first use, or nil
// for a mapping this view does not index.
//
// It is what stops a pointer walk rescanning the mappings it descends through.
// Resolving R references into a components mapping of M entries scans R×M pairs
// without it — quadratic in a document's own size, since both grow together —
// where an index makes each hop a map read. A key map cannot answer differently
// from the scan it replaces: expandContent yields each key once, so the pairs it
// is built from hold no duplicate for a first-match scan to prefer.
//
// It is gated on the pairs memo rather than on a second reading of memoize's
// budget test, which is a choice about coupling rather than about behaviour: at
// the depth this runs at the two select the same mappings. Every read here enters
// expand at depth 0, where isEntryPoint holds, so a truncated expansion is
// memoized deliberately and the only expansion the budget turns away is the one
// memoize turned away for the same reason a moment earlier. A merge cycle is
// refused before memoize is reached, but it yields no pairs at all and is already
// below minIndexedPairs.
//
// The memo is still the better gate, because it is the condition itself rather
// than a restatement of it. An index is a projection of a memo entry, so "is
// there an entry" is what it has to ask; a copy of memoize's arithmetic answers
// the same today and silently stops tracking it the day memoize's own test
// changes.
//
// It is bounded by that memo rather than charged against it. An index holds one
// entry per pair of a mapping the memo kept, so the entries across every index
// are bounded by cachedPairs, which maxCachedPairs already caps. Charging them
// too would halve the memo — and that memo is not a speed budget but the bound
// that keeps a merge chain from going cubic, where the bug being fixed was a
// hang. Halving it would also bring GitHub #404 within reach at half the
// document size, since which mappings keep a memo is what decides the answer
// there.
//
// On #404 itself, which records that the pairs memo is depth-sensitive and asks
// for that to be settled before this lookup work proceeds: this index is a
// projection of that memo and holds no state of its own, so it can be neither
// more nor less correct than the entry it is built from, and it adds no second
// way for a view to answer two things. It inherits #404 rather than widening it,
// and the fix landing there fixes this with it.
//
// It builds unconditionally rather than checking v.keys first: every caller
// reads the built index itself before reaching here, so a hit never arrives.
func (v *View) keyIndex(n *yaml.Node, pairs []Pair) map[string]*yaml.Node {
	if len(pairs) < minIndexedPairs {
		return nil
	}
	if _, memoized := v.pairs[n]; !memoized {
		return nil
	}

	index := make(map[string]*yaml.Node, len(pairs))
	for _, p := range pairs {
		index[p.Key] = p.Val
	}
	if v.keys == nil {
		v.keys = map[*yaml.Node]map[string]*yaml.Node{}
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
