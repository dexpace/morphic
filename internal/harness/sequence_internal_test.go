// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// header opens every fixture of the sequence oracle's tests: an empty API whose
// schemas are the lines that follow it.
const header = `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
`

// schemas is header followed by the schema entries, one per argument, each
// written under components.schemas.
func schemas(entries ...string) string {
	var b strings.Builder
	b.WriteString(header)
	for _, e := range entries {
		b.WriteString("    " + e + "\n")
	}
	return b.String()
}

// ordered reads src as a YAML stream and gives each document with its mappings
// as ordered pairs and every alias expanded, so two streams compare equal
// exactly when they say the same things in the same order. assertSameYAML does
// not: it reads a mapping regardless of its order.
func ordered(t *testing.T, src string) []any {
	t.Helper()
	var docs []any
	dec := yaml.NewDecoder(strings.NewReader(src))
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			return docs
		}
		require.NoError(t, err, "%q must decode", src)
		docs = append(docs, orderedNode(&n))
	}
}

// orderedNode is ordered's reading of one node.
func orderedNode(n *yaml.Node) any {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil
		}
		return orderedNode(n.Content[0])
	case yaml.AliasNode:
		return orderedNode(n.Alias)
	case yaml.MappingNode:
		pairs := make([]any, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			pairs = append(pairs, [2]any{orderedNode(n.Content[i]), orderedNode(n.Content[i+1])})
		}
		return pairs
	case yaml.SequenceNode:
		items := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			items = append(items, orderedNode(c))
		}
		return items
	default:
		return n.ShortTag() + " " + n.Value
	}
}

// TestReverseSequences_Permutes covers what the rewrite reverses and what it
// leaves alone. Each wanted source is written out by hand with its sequences
// reversed, and is compared with every mapping in the order the source wrote
// it, since only a sequence's order is the rewrite's to change.
func TestReverseSequences_Permutes(t *testing.T) {
	t.Parallel()
	const s = "/components/schemas"
	tests := []struct {
		name     string
		src      string
		want     string    // the permuted source, when the rewrite permutes it
		written  sequences // the reversed sequences, in the source's coordinates
		moved    sequences // the same in the permuted source's, when they differ
		declines Decline   // why the rewrite declines it, when it does
	}{
		{
			name:    "allOf entries reverse",
			src:     schemas("AB: {allOf: [{title: a}, {title: b}, {title: c}]}"),
			want:    schemas("AB: {allOf: [{title: c}, {title: b}, {title: a}]}"),
			written: sequences{s + "/AB/allOf": 3},
		},
		{
			name: "required names reverse and properties keep their order",
			src:  schemas("Obj: {type: object, required: [x, y], properties: {x: {}, y: {}}}"),
			want: schemas("Obj: {type: object, required: [y, x], properties: {x: {}, y: {}}}"),
			written: sequences{
				s + "/Obj/required": 2,
			},
		},
		{
			// The inner sequence is reversed too, and sits at the other index
			// once the outer one has moved.
			name:    "a nested allOf reverses and its pointer follows its parent",
			src:     schemas("AB: {allOf: [{allOf: [{title: a}, {title: b}]}, {title: c}]}"),
			want:    schemas("AB: {allOf: [{title: c}, {allOf: [{title: b}, {title: a}]}]}"),
			written: sequences{s + "/AB/allOf": 2, s + "/AB/allOf/0/allOf": 2},
			moved:   sequences{s + "/AB/allOf": 2, s + "/AB/allOf/1/allOf": 2},
		},
		{
			// Their order is the IR's to keep: a union's variants are named by
			// position, and the rest is listed in source order as presentation.
			name: "sequences the IR reads in order are not permuted",
			src: schemas("U: {anyOf: [{title: a}, {title: b}], oneOf: [{title: a}, {title: b}], " +
				"type: [string, 'null'], enum: [1, 2], prefixItems: [{title: a}, {title: b}]}"),
			declines: DeclineNothingToPermute,
		},
		{
			name: "data is not entered",
			src: schemas("D: {type: object, default: {allOf: [1, 2], required: [b, a]}, " +
				"example: {required: [b, a]}, examples: [{allOf: [1, 2]}], const: {required: [b, a]}, " +
				"enum: [{required: [b, a]}], x-ext: {allOf: [1, 2]}}"),
			declines: DeclineNothingToPermute,
		},
		{
			// In a name map a key is a name, not a keyword, so a property called
			// default is a schema like any other.
			name: "a name map's keys are names, so keyword spellings among them are entered",
			src: schemas("N: {type: object, properties: {default: {allOf: [{title: a}, {title: b}]}, " +
				"example: {allOf: [{title: a}, {title: b}]}, enum: {allOf: [{title: a}, {title: b}]}, " +
				"x-y: {allOf: [{title: a}, {title: b}]}}}"),
			want: schemas("N: {type: object, properties: {default: {allOf: [{title: b}, {title: a}]}, " +
				"example: {allOf: [{title: b}, {title: a}]}, enum: {allOf: [{title: b}, {title: a}]}, " +
				"x-y: {allOf: [{title: b}, {title: a}]}}}"),
			written: sequences{
				s + "/N/properties/default/allOf": 2,
				s + "/N/properties/example/allOf": 2,
				s + "/N/properties/enum/allOf":    2,
				s + "/N/properties/x-y/allOf":     2,
			},
		},
		{
			// A token of a pointer is escaped, so a path and a media type are one
			// token each.
			name: "a pointer escapes the slash of a path and a media type",
			src: `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a:
    get:
      responses:
        default:
          description: d
          content:
            application/json:
              schema: {allOf: [{title: a}, {title: b}]}
`,
			want: `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a:
    get:
      responses:
        default:
          description: d
          content:
            application/json:
              schema: {allOf: [{title: b}, {title: a}]}
`,
			written: sequences{"/paths/~1a/get/responses/default/content/application~1json/schema/allOf": 2},
		},
		{
			// A reference names the first entry of N's allOf, which reversing
			// would change.
			name: "a sequence a pointer points into is left as written",
			src: schemas("N: {allOf: [{title: a}, {title: b}]}",
				"Free: {allOf: [{title: c}, {title: d}]}",
				"User: {type: object, properties: {branch: {$ref: '#/components/schemas/N/allOf/0'}}}"),
			want: schemas("N: {allOf: [{title: a}, {title: b}]}",
				"Free: {allOf: [{title: d}, {title: c}]}",
				"User: {type: object, properties: {branch: {$ref: '#/components/schemas/N/allOf/0'}}}"),
			written: sequences{s + "/Free/allOf": 2},
		},
		{
			name: "a pointer is percent-decoded before it is matched",
			src: schemas("'My N': {allOf: [{title: a}, {title: b}]}",
				"User: {type: object, properties: {branch: {$ref: '#/components/schemas/My%20N/allOf/0'}}}"),
			declines: DeclineNothingToPermute,
		},
		{
			// The pointer is relative to an embedded resource, so it names the
			// sequence by a suffix of the sequence's own pointer.
			name: "a pointer relative to an embedded resource holds the sequence",
			src: schemas("R: {$id: 'https://example.test/r', allOf: [{title: a}, {title: b}]}",
				"User: {type: object, properties: {branch: {$ref: 'https://example.test/r#/allOf/0'}}}"),
			declines: DeclineNothingToPermute,
		},
		{
			// An alias is not entered, and what an anchor holds is left alone: an
			// alias of it would otherwise name the sequence by another order.
			name: "a sequence beneath an anchor is left as written",
			src: schemas("Base: &b {allOf: [{title: a}, {title: b}]}", "Use: *b",
				"Other: {allOf: [{title: c}, {title: d}]}"),
			want: schemas("Base: &b {allOf: [{title: a}, {title: b}]}", "Use: *b",
				"Other: {allOf: [{title: d}, {title: c}]}"),
			written: sequences{s + "/Other/allOf": 2},
		},
		{
			name: "every document of a stream is walked",
			src:  schemas("AB: {allOf: [{title: a}, {title: b}]}") + "---\nother: {allOf: [{a: 1}, {b: 2}]}\n",
			want: schemas("AB: {allOf: [{title: b}, {title: a}]}") + "---\nother: {allOf: [{b: 2}, {a: 1}]}\n",
			written: sequences{
				s + "/AB/allOf": 2,
				"/other/allOf":  2,
			},
		},
		{
			// Each document writes AB's allOf, so a pointer into it names one of
			// two sequences, and only the compiler knows which.
			name: "a path two documents of the stream write is held",
			src: schemas("AB: {allOf: [{title: a}, {title: b}]}") + "---\n" +
				schemas("AB: {allOf: [{title: c}, {title: d}, {title: e}]}", "CD: {allOf: [{title: f}, {title: g}]}"),
			want: schemas("AB: {allOf: [{title: a}, {title: b}]}") + "---\n" +
				schemas("AB: {allOf: [{title: c}, {title: d}, {title: e}]}", "CD: {allOf: [{title: g}, {title: f}]}"),
			written: sequences{s + "/CD/allOf": 2},
		},
		{
			// The first document writes AB's allOf as one entry, which a report
			// of the compile can name at index 0: reversing the second's would
			// read that report as moved.
			name: "a path another document writes with one entry is held",
			src: schemas("AB: {allOf: [{title: a}]}") + "---\n" +
				schemas("AB: {allOf: [{title: c}, {title: d}]}", "CD: {allOf: [{title: f}, {title: g}]}"),
			want: schemas("AB: {allOf: [{title: a}]}") + "---\n" +
				schemas("AB: {allOf: [{title: c}, {title: d}]}", "CD: {allOf: [{title: g}, {title: f}]}"),
			written: sequences{s + "/CD/allOf": 2},
		},
		{
			// The parser keeps one copy of a key written twice, which the walk
			// cannot tell.
			name:     "a key written twice is held",
			src:      schemas("AB: {allOf: [{title: a}, {title: b}], allOf: [{title: c}, {title: d}, {title: e}]}"),
			declines: DeclineNothingToPermute,
		},
		{
			// A security requirement's keys are scheme names, so a scheme called
			// required is a name and its scopes are no list of names.
			name:     "a security requirement's keys are names",
			src:      schemas("S: {type: string}") + "security: [{required: [a, b]}]\n",
			declines: DeclineNothingToPermute,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, perm, why := reverseSequences([]byte(tc.src))
			require.Equal(t, tc.declines, why)
			if why != "" {
				return
			}
			assert.Equal(t, ordered(t, tc.want), ordered(t, string(got)), "the permutation reverses these sequences and nothing else")
			assert.Equal(t, tc.written, perm.written)
			wantMoved := tc.moved
			if wantMoved == nil {
				wantMoved = tc.written
			}
			assert.Equal(t, wantMoved, perm.moved)
		})
	}
}

// TestReverseSequences_AnAnchorMovesToTheFirstUse pins that reversing a
// sequence holding an anchor and its alias leaves a stream that parses: the
// anchor moves to the alias that now comes first, and the alias takes its place.
func TestReverseSequences_AnAnchorMovesToTheFirstUse(t *testing.T) {
	t.Parallel()
	got, _, why := reverseSequences([]byte(schemas("AB: {allOf: [{properties: {p: &x {type: string}}}, {properties: {q: *x}}]}")))
	require.Empty(t, why, "a sequence holding an anchor and its alias is still permuted")

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal(got, &doc), "the permuted source must parse: an alias before its anchor does not")
	allOf := lookup(t, &doc, "components", "schemas", "AB", "allOf")
	require.Len(t, allOf.Content, 2)

	q := lookup(t, allOf.Content[0], "properties", "q")
	assert.Equal(t, "x", q.Anchor, "the entry that now comes first declares the anchor")
	assert.NotEqual(t, yaml.AliasNode, q.Kind)
	p := lookup(t, allOf.Content[1], "properties", "p")
	assert.Equal(t, yaml.AliasNode, p.Kind, "the entry that now comes second is the alias")
	assert.Equal(t, "x", p.Value)
}

// lookup descends from n through mapping keys, failing the test where one is
// missing.
func lookup(t *testing.T, n *yaml.Node, keys ...string) *yaml.Node {
	t.Helper()
	if n.Kind == yaml.DocumentNode {
		n = n.Content[0]
	}
	for _, k := range keys {
		require.Equal(t, yaml.MappingNode, n.Kind, "descending to %q", k)
		var next *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == k {
				next = n.Content[i+1]
			}
		}
		require.NotNil(t, next, "no key %q", k)
		n = next
	}
	return n
}

// TestReverseSequences_DeclinesWhatItCannotPermute pins each way the rewrite
// declines a source, none of which is a finding about the compiler.
func TestReverseSequences_DeclinesWhatItCannotPermute(t *testing.T) {
	t.Parallel()
	deep := "allOf: " + strings.Repeat("[", maxReverseDepth+2) + strings.Repeat("]", maxReverseDepth+2) + "\n"
	for _, tc := range []struct {
		name, src string
		why       Decline
	}{
		{"a source that is no YAML", "\t- a: [\n", DeclineNotYAML},
		{"a stream longer than the rewrite reads",
			strings.Repeat("allOf: [a, b]\n---\n", maxPermutedDocuments) + "allOf: [a, b]\n", DeclineTooManyDocuments},
		{"a tree nested past the bound under an allOf", deep, DeclineTooDeep},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _, why := reverseSequences([]byte(tc.src))
			assert.Equal(t, tc.why, why)
			assert.Nil(t, got)
		})
	}
}

// TestReverseSequences_EncodeFailureIsRefused drives the defensive re-encode
// path through the seam, as the mapping rewrite's is driven.
func TestReverseSequences_EncodeFailureIsRefused(t *testing.T) {
	orig := encodeYAML
	t.Cleanup(func() { encodeYAML = orig })
	encodeYAML = func([]*yaml.Node) ([]byte, error) { return nil, errors.New("boom") }

	got, _, why := reverseSequences([]byte("allOf: [a, b]\n"))

	assert.Equal(t, DeclineUnencodable, why)
	assert.Nil(t, got)
}

// TestReverseSequences_APermutationThatDoesNotParseIsDeclined drives the check
// that holds the rewrite to writing YAML.
func TestReverseSequences_APermutationThatDoesNotParseIsDeclined(t *testing.T) {
	orig := encodeYAML
	t.Cleanup(func() { encodeYAML = orig })
	encodeYAML = func([]*yaml.Node) ([]byte, error) { return []byte("a: [unclosed\n"), nil }

	got, _, why := reverseSequences([]byte("allOf: [a, b]\n"))

	assert.Equal(t, DeclinePermutationUnparsable, why)
	assert.Nil(t, got)
}

// TestReverseSequences_AnAnchorGivenTwiceIsDeclined pins that a source whose
// anchors could be rebound by the move undoing an alias written above its
// anchor is declined once something is reversed, as the mapping rewrite
// declines it.
func TestReverseSequences_AnAnchorGivenTwiceIsDeclined(t *testing.T) {
	t.Parallel()
	src := schemas("A: &a {type: string}", "B: &a {type: integer}", "AB: {allOf: [{title: a}, {title: b}]}")

	got, _, why := reverseSequences([]byte(src))

	assert.Equal(t, DeclineAnchorRedefined, why)
	assert.Nil(t, got)
}

// TestRewriteIndices_RewritesTheIndicesOfReversedSequences pins the walk that
// erases and rebases an index: it starts at the first '/' from which one
// applies, keeps the pointer walked so far as written, and leaves a string
// naming no index of a reversed sequence as it is.
func TestRewriteIndices_RewritesTheIndicesOfReversedSequences(t *testing.T) {
	t.Parallel()
	written := sequences{"/c/AB/allOf": 2, "/c/AB/allOf/0/allOf": 2}
	moved := sequences{"/c/AB/allOf": 2, "/c/AB/allOf/1/allOf": 2}
	for _, tc := range []struct{ name, in, erased, rebased string }{
		{"an ID naming an element of nested sequences", "t/anon/c/AB/allOf/0/allOf/1/properties/p",
			"t/anon/c/AB/allOf/-/allOf/-/properties/p", "t/anon/c/AB/allOf/1/allOf/1/properties/p"},
		{"a pointer from the root", "/c/AB/allOf/1/allOf/0", "/c/AB/allOf/-/allOf/0", "/c/AB/allOf/0/allOf/1"},
		{"an index past the sequence's length", "/c/AB/allOf/2", "/c/AB/allOf/2", "/c/AB/allOf/2"},
		{"a sequence nobody reversed", "/c/CD/allOf/0", "/c/CD/allOf/0", "/c/CD/allOf/0"},
		{"a relative name", "openapi:allOf/1", "openapi:allOf/1", "openapi:allOf/1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.erased, erase(tc.in, written))
			assert.Equal(t, tc.rebased, rebase(tc.in, moved))
		})
	}
}
