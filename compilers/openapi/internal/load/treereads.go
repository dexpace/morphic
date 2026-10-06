package load

import (
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonpointer"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// maxScanMerges bounds how many merged mappings a read counted by treeReads is
// inside at once, along the whole pointer, which bounds its recursion. The
// library has no such bound. A read past it is priced past any limit, so the
// resumption reading it is refused and the budget reported crossed; no
// document reaches it without a thousand `<<` keys along one pointer.
const maxScanMerges = 1 << 10

// treeReads counts what jsonpointer.GetTarget's read of a pointer in a YAML
// tree costs the library, which compares a mapping's keys in order for the
// first matching a token, then tries each `<<` key's mapping in turn. Each
// mapping is answered from an index built once (see mappingKeys), so counting a
// read costs its depth, not the widths it scans.
//
// It only prices reads: GetTarget makes them, so a count that strayed from the
// library would misprice a read, never misread one. It models
// speakeasy-api/openapi v1.25.2: TestTreeReads_CountsWhatTheLibraryReadTakes
// holds where it ends to GetTarget's; re-check its counting on a bump.
type treeReads struct {
	keys map[*yaml.Node]*mappingKeys
	// built counts the pairs indexed, which is this reader's own work.
	built int
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
	parts, ok := partsOf(pointer)
	if !ok {
		return 0, nil
	}
	s := tally{reads: t, limit: limit}
	target, _ := s.walk(root, parts, 0)
	return s.steps, target
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
	t.built += k.pairs
	return k
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

// tally is one counted read: the steps taken, and the limit past which it stops.
type tally struct {
	reads        *treeReads
	steps, limit int
}

// walk follows parts from n as getCurrentStackTarget does, a part at a time,
// and returns the node the read ends on. A mapping holding no key for a part is
// read through its merge keys, which take the rest of the read (see merged).
func (s *tally) walk(n *yaml.Node, parts []part, merges int) (*yaml.Node, bool) {
	p, rest := part{}, parts
	if len(parts) > 0 {
		p, rest = parts[0], parts[1:]
	}
	for {
		if n = s.content(n); n == nil {
			return nil, false
		}
		// The library reads an empty last token as the node it is read on.
		if len(rest) == 0 && p.value == "" {
			return n, true
		}
		var next *yaml.Node
		switch n.Kind {
		case yaml.MappingNode:
			k := s.reads.index(n)
			pos, ok := k.first[unescaped(p.value)]
			if !ok {
				s.steps += k.pairs
				return s.merged(n, k, p, rest, merges)
			}
			s.steps += pos + 1
			next = n.Content[2*pos+1]
		case yaml.SequenceNode:
			if next = element(n, p); next == nil {
				return nil, false
			}
		default:
			return nil, false
		}
		if len(rest) == 0 {
			return next, true
		}
		n, p, rest = next, rest[0], rest[1:]
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

// element returns the element of sequence n that p names, or nil for none.
func element(n *yaml.Node, p part) *yaml.Node {
	i, err := strconv.Atoi(p.value)
	if !p.index || err != nil || i < 0 || i >= len(n.Content) {
		return nil
	}
	return n.Content[i]
}

// merged reads p and rest in mapping n, indexed as k, which holds no key for
// p, as the library does: through the mapping each `<<` key names, in turn,
// until one holds the rest of the read. merges counts the merged mappings the
// read is inside; past maxScanMerges it is past the limit.
func (s *tally) merged(n *yaml.Node, k *mappingKeys, p part, rest []part, merges int) (*yaml.Node, bool) {
	for _, pos := range k.merges {
		if s.steps > s.limit {
			return nil, false
		}
		m := nodeview.Deref(n.Content[2*pos+1])
		if m == nil || m.Kind != yaml.MappingNode {
			continue
		}
		if merges >= maxScanMerges {
			s.steps = s.limit + 1
			return nil, false
		}
		if target, ok := s.inMapping(m, p, rest, merges+1); ok {
			s.steps += pos + 1
			return target, true
		}
	}
	s.steps += k.pairs
	return nil, false
}

// inMapping reads p and rest in mapping m as the library reads a merged one:
// its own keys, then its merge keys, with no visit to m and no reading of an
// empty last token as m itself.
func (s *tally) inMapping(m *yaml.Node, p part, rest []part, merges int) (*yaml.Node, bool) {
	k := s.reads.index(m)
	pos, ok := k.first[unescaped(p.value)]
	if !ok {
		s.steps += k.pairs
		return s.merged(m, k, p, rest, merges)
	}
	s.steps += pos + 1
	if len(rest) == 0 {
		return m.Content[2*pos+1], true
	}
	return s.walk(m.Content[2*pos+1], rest, merges)
}
