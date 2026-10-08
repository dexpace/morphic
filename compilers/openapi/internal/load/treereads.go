package load

import (
	"encoding/json/jsontext"
	"math"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonpointer"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// treeReads reads a pointer in a YAML tree as jsonpointer.GetTarget does, and
// counts what that read costs: the library compares a mapping's keys in order
// for the first matching a token, then tries each `<<` key's mapping in turn.
// Each mapping is answered from an index built once (see mappingKeys), so a
// read costs treeReads its depth, not the widths it passes. It prices the
// library's reads (cost), reads for loops, which charges its own (read), and
// answers resolve.Scope.Holds (holds).
//
// It models speakeasy-api/openapi v1.25.2: TestTreeReads_CountsWhatTheLibraryReadTakes
// holds where it ends to GetTarget's; re-check it on a bump.
type treeReads struct {
	keys map[*yaml.Node]*mappingKeys
	// indexed counts the pairs indexed since drain last answered: this
	// counter's own work, which its caller charges as its own.
	indexed int
}

// mappingKeys is what a mapping's keys say to the library's read: the pair
// holding each key first, the key read through an alias as the library reads
// one, the pairs whose key is written `<<`, in order, and how many times the
// library's loop over the pairs runs when nothing stops it.
type mappingKeys struct {
	first  map[string]int
	merges []int
	pairs  int
}

// part is one token of a pointer as the library reads it: still escaped, and
// whether it reads as an index.
type part struct {
	value string
	index bool
}

// newTreeReads returns a counter with nothing indexed.
func newTreeReads() *treeReads {
	return &treeReads{keys: map[*yaml.Node]*mappingKeys{}}
}

// cost returns the steps GetTarget's read of pointer in the tree under root
// takes, one for each node it visits and each pair its loops over a mapping
// pass, and the node it returns, or nil where it fails. It stops counting once
// past limit.
func (t *treeReads) cost(root *yaml.Node, pointer string, limit int) (int, *yaml.Node) {
	return t.tally(root, pointer, limit, false)
}

// read is cost counting this reader's own steps rather than the library's: a
// step for each node visited, each key looked up in a mapping's index and each
// `<<` key tried, so a wide mapping costs a read no more than a narrow one.
func (t *treeReads) read(root *yaml.Node, pointer string, limit int) (int, *yaml.Node) {
	return t.tally(root, pointer, limit, true)
}

// tally is cost, or read where own. The limit is held below math.MaxInt, so a
// read priced past it still counts in an int (see push).
func (t *treeReads) tally(root *yaml.Node, pointer string, limit int, own bool) (int, *yaml.Node) {
	parts, ok := partsOf(pointer)
	if !ok {
		return 0, nil
	}
	s := tally{reads: t, limit: min(limit, math.MaxInt-1), own: own}
	target := s.walk(root, parts)
	return s.steps, target
}

// modelCost returns the steps GetTarget's read of pointer in doc, the source's
// model, takes: none for a pointer the library refuses before reading, one for
// each token the model answers or fails, and where the read leaves the model
// (navigation.Walk), one more and the library's read of the tokens left in the
// raw YAML it goes on in, which it reads together there (see cost). Counting
// stops once past limit.
func (t *treeReads) modelCost(doc any, pointer jsontext.Pointer, limit int) int {
	tokens, ok := navigation.Tokens(pointer)
	if !ok {
		return 0
	}
	at, rest, err := navigation.Walk(doc, tokens)
	steps := len(tokens) - len(rest)
	if err != nil {
		return steps + 1
	}
	raw, leaves := at.(*yaml.Node)
	if !leaves || len(rest) == 0 {
		return steps
	}
	scan, _ := t.cost(raw, joinTokens(rest), limit-steps-1)
	return steps + 1 + scan
}

// holds reports whether the library's read of token in raw finds a node, read
// through the index (see resolve.Scope.Holds), so asking it of one wide mapping
// once per reference costs its width once.
func (t *treeReads) holds(raw *yaml.Node, token string) bool {
	_, target := t.read(raw, "/"+jsonpointer.EscapeString(token), math.MaxInt)
	return target != nil
}

// drain returns the pairs indexed since it last answered.
func (t *treeReads) drain() int {
	n := t.indexed
	t.indexed = 0
	return n
}

// index returns the index of mapping n, building it the first time.
func (t *treeReads) index(n *yaml.Node) *mappingKeys {
	if k, ok := t.keys[n]; ok {
		return k
	}
	k := &mappingKeys{first: map[string]int{}, pairs: (len(n.Content) + 1) / 2}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		if key.Kind == yaml.ScalarNode && key.Value == "<<" {
			k.merges = append(k.merges, i/2)
		}
		if read := nodeview.Deref(key); read != nil && read.Kind == yaml.ScalarNode {
			if _, seen := k.first[read.Value]; !seen {
				k.first[read.Value] = i / 2
			}
		}
	}
	t.keys[n] = k
	t.indexed += k.pairs
	return k
}

// joinTokens returns the pointer naming tokens, or "" for none, which reads
// nothing. It keeps each token's bytes, as the library reads them: jsontext
// would spell a byte that is no UTF-8 as U+FFFD, a key the read never compares.
func joinTokens(tokens []string) string {
	return string(jsonpointer.PartsToJSONPointer(tokens))
}

// partsOf returns the tokens of pointer as the library's navigation stack
// holds them, or false for a pointer it refuses before reading anything.
func partsOf(pointer string) ([]part, bool) {
	if jsonpointer.JSONPointer(pointer).Validate() != nil {
		return nil, false
	}
	if pointer == "/" {
		return nil, true
	}
	tokens := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	parts := make([]part, len(tokens))
	for i, token := range tokens {
		parts[i] = part{value: token, index: isIndex(token)}
	}
	return parts, true
}

// isIndex reports whether the library reads token as an index: digits, with
// no leading zero unless it is one.
func isIndex(token string) bool {
	if token == "" || len(token) > 1 && token[0] == '0' {
		return false
	}
	return strings.Trim(token, "0123456789") == ""
}

// unescaped returns the key token names, decoded as the library decodes it.
func unescaped(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
}

// tally is one counted read: the steps taken, and the limit past which it
// stops. own counts the reader's own work rather than the library's. trying
// holds the reads through `<<` keys the read is inside, innermost last, and
// open the mapping and part of each, which none re-enters.
type tally struct {
	reads        *treeReads
	steps, limit int
	own          bool
	trying       []merging
	open         map[mergeKey]bool
}

// merging is a read of parts[at] on through the `<<` keys of mapping n, which
// holds no key for it: k is n's index, and next counts the merge keys tried.
type merging struct {
	n        *yaml.Node
	k        *mappingKeys
	at, next int
}

// mergeKey is the mapping a read through `<<` keys is in, and its part.
type mergeKey struct {
	n  *yaml.Node
	at int
}

// compared returns the steps a loop over a mapping's pairs takes, stopping at
// the n-th: n for the library's, which compares them in order, and one for this
// reader's own, which looks the key up in the mapping's index.
func (s *tally) compared(n int) int {
	if s.own {
		return 1
	}
	return n
}

// mergesLooped returns the steps the loop over a mapping's `<<` keys takes,
// having passed pairs of its pairs and tried merges of the keys: the pairs for
// the library, which compares every key, and the merges for this reader's own,
// which knows where they are.
func (s *tally) mergesLooped(pairs, merges int) int {
	if s.own {
		return merges
	}
	return pairs
}

// walk reads parts from root as getCurrentStackTarget does, a part at a time,
// and returns the node the read ends on, or nil where it fails. A mapping
// holding no key for a part is read through the mapping each of its `<<` keys
// names, in turn, until one holds the rest of the read. The library recurses
// as deep as a merge chain goes, which only the caller's budgets bound; these
// reads are a stack instead, each pushed at a step, so the limit bounds them.
func (s *tally) walk(root *yaml.Node, parts []part) *yaml.Node {
	if len(parts) == 0 {
		parts = []part{{}}
	}
	end, ok := s.follow(root, parts, 0)
	for !ok {
		m, at, more := s.nextMerge()
		if !more {
			return nil
		}
		end, ok = s.inMerged(m, parts, at)
	}
	s.unwind()
	return end
}

// follow reads parts from n, from parts[at] on, and returns the node the read
// ends on, and whether it ends on one.
func (s *tally) follow(n *yaml.Node, parts []part, at int) (*yaml.Node, bool) {
	for ; ; at++ {
		if n = s.content(n); n == nil {
			return nil, false
		}
		// The library reads an empty last token as the node it is read on.
		last := at == len(parts)-1
		if last && parts[at].value == "" {
			return n, true
		}
		next, ok := s.child(n, parts, at)
		if !ok || last {
			return next, ok
		}
		n = next
	}
}

// content returns the node n stands for, past any alias or document node, or
// nil where the library fails: no node, an alias of none, or an empty document.
// It counts the visit, and fails past the limit.
func (s *tally) content(n *yaml.Node) *yaml.Node {
	s.steps++
	for n != nil && s.steps <= s.limit {
		switch n.Kind {
		case yaml.AliasNode:
			n = nodeview.Deref(n)
		case yaml.DocumentNode:
			if len(n.Content) == 0 {
				return nil
			}
			n = n.Content[0]
		default:
			return n
		}
	}
	return nil
}

// child returns the node parts[at] names in n, and whether n holds one.
func (s *tally) child(n *yaml.Node, parts []part, at int) (*yaml.Node, bool) {
	switch n.Kind {
	case yaml.MappingNode:
		return s.lookup(n, parts, at)
	case yaml.SequenceNode:
		next := element(n, parts[at])
		return next, next != nil
	default:
		return nil, false
	}
}

// element returns the element of sequence n that p names, or nil for none.
func element(n *yaml.Node, p part) *yaml.Node {
	i, err := strconv.Atoi(p.value)
	if !p.index || err != nil || i < 0 || i >= len(n.Content) {
		return nil
	}
	return n.Content[i]
}

// lookup returns the value mapping n holds under parts[at]'s key, and whether
// it holds one, as the library finds it: in the first pair holding the key,
// having compared those before it. One holding none has every pair compared,
// and is pushed, for the read to go on through its `<<` keys.
func (s *tally) lookup(n *yaml.Node, parts []part, at int) (*yaml.Node, bool) {
	k := s.reads.index(n)
	pos, ok := k.first[unescaped(parts[at].value)]
	if !ok {
		s.steps += s.compared(k.pairs)
		s.push(n, k, at)
		return nil, false
	}
	s.steps += s.compared(pos + 1)
	return n.Content[2*pos+1], true
}

// inMerged reads parts from parts[at] on in mapping m as the library reads a
// merged one: with no visit to m and no reading of an empty last token as m.
func (s *tally) inMerged(m *yaml.Node, parts []part, at int) (*yaml.Node, bool) {
	next, ok := s.lookup(m, parts, at)
	if !ok || at == len(parts)-1 {
		return next, ok
	}
	return s.follow(next, parts, at+1)
}

// push starts the read of parts[at] on through the `<<` keys of mapping n,
// indexed as k. With none, it fails there, having looped over n's pairs for
// them. A read already open on n for the same part would recur without end,
// as the library's does; no parsed tree holds such a cycle (see refusals),
// and it is priced past the limit, which stops it.
func (s *tally) push(n *yaml.Node, k *mappingKeys, at int) {
	if len(k.merges) == 0 {
		s.steps += s.mergesLooped(k.pairs, 0)
		return
	}
	key := mergeKey{n: n, at: at}
	if s.open[key] {
		s.steps = s.limit + 1
		return
	}
	if s.open == nil {
		s.open = map[mergeKey]bool{}
	}
	s.open[key] = true
	s.trying = append(s.trying, merging{n: n, k: k, at: at})
}

// nextMerge returns the mapping the innermost read through `<<` keys tries
// next, and the part it reads there, or false where none is left. A read whose
// keys are all tried fails, having looped over them, and the one it is inside
// goes on to its next; past the limit, every one fails.
func (s *tally) nextMerge() (*yaml.Node, int, bool) {
	for len(s.trying) > 0 && s.steps <= s.limit {
		r := &s.trying[len(s.trying)-1]
		for r.next < len(r.k.merges) {
			pos := r.k.merges[r.next]
			r.next++
			if m := nodeview.Deref(r.n.Content[2*pos+1]); m != nil && m.Kind == yaml.MappingNode {
				return m, r.at, true
			}
		}
		s.steps += s.mergesLooped(r.k.pairs, len(r.k.merges))
		delete(s.open, mergeKey{n: r.n, at: r.at})
		s.trying = s.trying[:len(s.trying)-1]
	}
	return nil, 0, false
}

// unwind counts, for each read through `<<` keys the read ends inside, the
// loop over its pairs that reached the key whose mapping held the rest.
func (s *tally) unwind() {
	for _, r := range s.trying {
		s.steps += s.mergesLooped(r.k.merges[r.next-1]+1, r.next)
	}
}
