// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// sequences maps the pointer of each reversed sequence, from its document's
// root and in one source's coordinates, to its length.
type sequences map[string]int

// permutation is what reverseSequences reversed: in the source's coordinates
// and in the permuted source's.
type permutation struct{ written, moved sequences }

// reverseSequences returns src with every allOf and required sequence of two
// or more reversed, in every document of its stream, and where each reversed
// sequence sits in either source. JSON Schema defines both without position.
// Data is not entered, and a sequence at or beneath an anchor, or one a scalar
// of the stream points into, is left as written, since reversing it changes
// what an alias or a reference names. An alias carried above its anchor is
// moved back (hoistAnchors). It declines as reverseMappings does, bar a
// repeated key, and where it reverses nothing.
func reverseSequences(src []byte) ([]byte, permutation, Decline) {
	docs, why := parseStream(src)
	if why != "" {
		return nil, permutation{}, why
	}
	w := sequenceWalk{pointed: map[string]bool{}}
	for _, doc := range docs {
		if why := w.walk(doc, asKeywords, 0); why != "" {
			return nil, permutation{}, why
		}
	}
	perm := w.reverse()
	if len(perm.written) == 0 {
		return nil, permutation{}, DeclineNothingToPermute
	}
	for _, doc := range docs {
		if why := hoistAnchors(doc); why != "" {
			return nil, permutation{}, why
		}
	}
	out, err := encodeYAML(docs)
	if err != nil {
		return nil, permutation{}, DeclineUnencodable
	}
	if _, why := parseStream(out); why != "" {
		return nil, permutation{}, DeclinePermutationUnparsable
	}
	return out, perm, ""
}

// reading is how the walk reads a mapping: its keys as keywords or as names,
// or the whole node as data, whose sequences are no candidates.
type reading uint8

const (
	asKeywords reading = iota
	asNames
	asData
)

// candidate is an allOf or required sequence the walk met, and its pointer.
type candidate struct {
	node    *yaml.Node
	pointer string
}

// sequenceWalk is the state of one reverseSequences walk: the pointer prefixes
// the stream's scalars name an element through, the candidates met, and the
// pointer of the node being visited.
type sequenceWalk struct {
	pointed    map[string]bool
	candidates []candidate
	pointer    []byte
}

// reverse reverses each candidate no scalar points into and returns where the
// reversed sequences sit. Element i of a reversed sequence of length n is
// element n-1-i of the permuted source, which moves a nested one. A pointer two
// candidates share, a key written twice or a path two documents of the stream
// both write, is held: what the IR says at it names one of them, not which.
func (w *sequenceWalk) reverse() permutation {
	met := make(map[string]int, len(w.candidates))
	for _, c := range w.candidates {
		met[c.pointer]++
	}
	perm := permutation{written: sequences{}, moved: sequences{}}
	for _, c := range w.candidates {
		if met[c.pointer] == 1 && !w.held(c.pointer) {
			perm.written[c.pointer] = len(c.node.Content)
		}
	}
	for _, c := range w.candidates {
		n, reversed := perm.written[c.pointer]
		if !reversed {
			continue
		}
		moved, _ := rewriteFrom(c.pointer, 0, perm.written, func(n, i int) string { return strconv.Itoa(n - 1 - i) })
		perm.moved[moved] = n
		slices.Reverse(c.node.Content)
	}
	return perm
}

// walk visits n, read as mode says, recording each scalar's pointer prefixes
// (point) and each candidate. An anchored node is read as data, since an
// alias names it in either order, and an alias is not entered: the node it
// names is visited where it is written.
func (w *sequenceWalk) walk(n *yaml.Node, mode reading, depth int) Decline {
	if depth > maxReverseDepth {
		return DeclineTooDeep
	}
	if n.Anchor != "" {
		mode = asData
	}
	switch n.Kind {
	case yaml.ScalarNode:
		w.point(n.Value)
		return ""
	case yaml.MappingNode:
		return w.mapping(n, mode, depth)
	case yaml.SequenceNode, yaml.DocumentNode:
		return w.sequence(n, mode, depth)
	default:
		return ""
	}
}

// mapping visits a mapping's entries. Of a keyword mapping, data is not read
// as keywords, a name map's value is read as names, and so is each
// requirement of a security list, since a scheme may be called required; an
// allOf or required sequence of two or more is a candidate. A names mapping's
// values are read as keywords, whatever their keys.
func (w *sequenceWalk) mapping(n *yaml.Node, mode reading, depth int) Decline {
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, v := n.Content[i], n.Content[i+1]
		if key.Kind == yaml.ScalarNode {
			w.point(key.Value)
		}
		k := keyText(key)
		mark := len(w.pointer)
		w.pointer = appendToken(w.pointer, k)
		if mode == asKeywords && orderFreeKeyword(k) &&
			v.Kind == yaml.SequenceNode && len(v.Content) >= 2 && v.Anchor == "" {
			w.candidates = append(w.candidates, candidate{node: v, pointer: string(w.pointer)})
		}
		why := w.walk(v, valueReading(mode, k, v), depth+1)
		w.pointer = w.pointer[:mark]
		if why != "" {
			return why
		}
	}
	return ""
}

// valueReading is how the value v of key k is read in a mapping read as mode.
func valueReading(mode reading, k string, v *yaml.Node) reading {
	switch {
	case mode == asData:
		return asData
	case mode == asNames:
		return asKeywords
	case data(k):
		return asData
	case nameMap(k) && v.Kind == yaml.MappingNode, k == "security" && v.Kind == yaml.SequenceNode:
		return asNames
	default:
		return asKeywords
	}
}

// sequence visits each entry of a sequence, read as the sequence is, or a
// document's root, which adds no token to the pointer.
func (w *sequenceWalk) sequence(n *yaml.Node, mode reading, depth int) Decline {
	for i, entry := range n.Content {
		mark := len(w.pointer)
		if n.Kind == yaml.SequenceNode {
			w.pointer = appendToken(w.pointer, strconv.Itoa(i))
		}
		why := w.walk(entry, mode, depth+1)
		w.pointer = w.pointer[:mark]
		if why != "" {
			return why
		}
	}
	return ""
}

// point records the prefixes text names an element through: the text after
// its first '#', percent-decoded and split on '/', gives the tokens before
// each decimal one. A prefix naming the root is no sequence's, so it is left
// out.
func (w *sequenceWalk) point(text string) {
	_, fragment, ok := strings.Cut(text, "#")
	if !ok {
		return
	}
	if decoded, err := url.PathUnescape(fragment); err == nil {
		fragment = decoded
	}
	tokens := strings.Split(fragment, "/")
	for k := 1; k < len(tokens); k++ {
		if decimal(tokens[k]) {
			w.pointed[strings.Join(tokens[:k], "/")] = true
		}
	}
}

// decimal reports whether token is a run of ASCII digits.
func decimal(token string) bool {
	return token != "" && strings.Trim(token, "0123456789") == ""
}

// held reports whether a scalar points into the sequence at pointer: a
// recorded prefix equals it, or is a suffix of it at a token boundary, as a
// pointer relative to an embedded resource is.
func (w *sequenceWalk) held(pointer string) bool {
	for i := range len(pointer) {
		if pointer[i] == '/' && w.pointed[pointer[i:]] {
			return true
		}
	}
	return false
}

// keyText is a mapping key as the parser reads it: an alias stands for the
// node it names.
func keyText(key *yaml.Node) string {
	if key.Kind == yaml.AliasNode {
		return key.Alias.Value
	}
	return key.Value
}

// appendToken appends '/' and token to pointer, '~' written "~0" and '/'
// written "~1".
func appendToken(pointer []byte, token string) []byte {
	pointer = append(pointer, '/')
	for i := range len(token) {
		switch token[i] {
		case '~':
			pointer = append(pointer, '~', '0')
		case '/':
			pointer = append(pointer, '~', '1')
		default:
			pointer = append(pointer, token[i])
		}
	}
	return pointer
}

// orderFreeKeyword reports whether keyword's sequence is one JSON Schema
// defines without position, the ones the walk reverses: allOf, which
// "validates successfully against all schemas", and required, an array of
// unique names.
func orderFreeKeyword(keyword string) bool { return keyword == "allOf" || keyword == "required" }

// data reports whether a keyword's value is data rather than keywords: an
// example, a default, a constant, an enumeration or an extension's value.
func data(keyword string) bool {
	switch keyword {
	case "example", "examples", "default", "const", "enum":
		return true
	default:
		return strings.HasPrefix(keyword, "x-")
	}
}

// nameMap reports whether a keyword's mapping value is keyed by names rather
// than keywords: a property called default is a schema like any other. A Link
// Object's parameters are among them, so a literal parameter value is read as
// keywords, not as data.
func nameMap(keyword string) bool {
	switch keyword {
	case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas",
		"dependentRequired", "schemas", "responses", "parameters", "requestBodies", "headers",
		"securitySchemes", "links", "callbacks", "pathItems", "paths", "webhooks", "content",
		"encoding", "variables", "mapping", "scopes":
		return true
	default:
		return false
	}
}

// erase writes every index of a reversed sequence that s names as "-".
func erase(s string, seqs sequences) string {
	return rewriteIndices(s, seqs, func(int, int) string { return "-" })
}

// rebase writes every index i of a reversed sequence of length n that s names
// as n-1-i, which moves a string from the permuted source's coordinates to
// those of the source as written.
func rebase(s string, seqs sequences) string {
	return rewriteIndices(s, seqs, func(n, i int) string { return strconv.Itoa(n - 1 - i) })
}

// rewriteIndices rewrites each index s names in a sequence of seqs as write
// does. From the first '/' at which one applies, it walks the '/'-separated
// tokens, keeping the pointer walked so far; where that pointer is one of
// seqs and the next token is a decimal index below its length, the index is
// rewritten.
func rewriteIndices(s string, seqs sequences, write func(n, i int) string) string {
	if len(seqs) == 0 || !mayNameAnIndex(s) {
		return s
	}
	for start := range len(s) {
		if s[start] != '/' {
			continue
		}
		if out, ok := rewriteFrom(s, start, seqs, write); ok {
			return out
		}
	}
	return s
}

// mayNameAnIndex reports whether s holds an order-free keyword as a token with
// another after it: every sequence's pointer ends in one, so a string naming
// an index of a reversed sequence does.
func mayNameAnIndex(s string) bool {
	for start := 0; ; {
		end := strings.IndexByte(s[start:], '/')
		if end < 0 {
			return false
		}
		end += start
		if start > 0 && orderFreeKeyword(s[start:end]) {
			return true
		}
		start = end + 1
	}
}

// rewriteFrom is rewriteIndices' walk from the '/' at start, and whether it
// rewrote an index.
func rewriteFrom(s string, start int, seqs sequences, write func(n, i int) string) (string, bool) {
	var b strings.Builder
	b.WriteString(s[:start])
	applied := false
	for at := start; at < len(s); {
		end := len(s)
		if next := strings.IndexByte(s[at+1:], '/'); next >= 0 {
			end = at + 1 + next
		}
		token := s[at+1 : end]
		b.WriteByte('/')
		if i, ok := indexIn(token, seqs[s[start:at]]); ok {
			token, applied = write(seqs[s[start:at]], i), true
		}
		b.WriteString(token)
		at = end
	}
	return b.String(), applied
}

// indexIn reads token as an index below n.
func indexIn(token string, n int) (int, bool) {
	if !decimal(token) {
		return 0, false
	}
	i, err := strconv.Atoi(token)
	return i, err == nil && i < n
}
