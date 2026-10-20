package canary_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"slices"
)

// reachFuzzRefs, reachFuzzIDs and reachFuzzAnchors are the fixed vocabularies
// the grammar below draws $ref/$id/$anchor values from, the shapes GitHub #526
// and #546 report: random mixes of "#", "#/" and $defs-relative pointers,
// cross-component references, and relative/absolute $id values.
var (
	reachFuzzRefs = []string{
		"#a", "#b", "#/$defs/m", "#/$defs/n", "#/$defs/m/properties/p",
		"#/components/schemas/A", "#/components/schemas/B", "#/components/schemas/C",
		"#/components/schemas/A/$defs/m", "#/components/schemas/B/$defs/n",
		"#/components/schemas/A/properties/p", "#/components/schemas/B/properties/q",
		"https://x.test/a", "https://x.test/a#a", "https://x.test/b#/$defs/m", "a.json", "#/x-s", "#/x-s/$defs/m",
		// Spellings the resolver reads differently from a byte-for-byte reader:
		// a percent-encoded pointer, a second '#', and URIs a '.', a '..' or a
		// percent-encoding turns into another path.
		"#/%24defs/m", "#/$defs%2Fn", "#/$defs/m#x", "#a#x", "#", "#/", "./a.json", "foo/..", ".",
		"https://x.test/a/", "https://x.test/a%20b", "https://x.test/a b",
	}
	reachFuzzIDs = []string{
		"https://x.test/a", "https://x.test/b", "a.json", "https://x.test/a/", "https://x.test/a%20b", "./a.json", "a/..",
	}
	reachFuzzAnchors = []string{"a", "b"}
)

// reachFuzzSchema is one node of the grammar's tree: a subset of
// $ref/type/$anchor/$id/$defs/properties chosen at random, kept as a struct
// rather than a generic map so writeJSON below controls key order deterministically
// (a map has none).
type reachFuzzSchema struct {
	ref, typ, anchor, id              string
	hasRef, hasType, hasAnchor, hasID bool
	defs, props                       map[string]*reachFuzzSchema
	defOrder, propOrder               []string
	// yamlAnchor names a YAML anchor the rendered mapping carries; merge names
	// the anchor of an earlier mapping it merges in with a `<<` key.
	yamlAnchor, merge string
}

// genFuzzSchema generates one random schema node. depth is an explicit,
// decrementing bound on the recursion (styleguide bounded-recursion rule):
// callers below pass 2 for a component and 1 for the document-level "x-s"
// extension, so nesting stops well short of anything pathological.
func genFuzzSchema(rng *rand.Rand, depth int) *reachFuzzSchema {
	s := &reachFuzzSchema{}
	if rng.Float64() < 0.55 {
		s.ref, s.hasRef = reachFuzzRefs[rng.Intn(len(reachFuzzRefs))], true
	} else {
		s.typ, s.hasType = []string{"object", "string"}[rng.Intn(2)], true
	}
	if rng.Float64() < 0.3 {
		s.anchor, s.hasAnchor = reachFuzzAnchors[rng.Intn(len(reachFuzzAnchors))], true
	}
	if rng.Float64() < 0.15 {
		s.id, s.hasID = reachFuzzIDs[rng.Intn(len(reachFuzzIDs))], true
	}
	if depth > 0 && rng.Float64() < 0.45 {
		s.defOrder = fuzzSample(rng, []string{"m", "n"})
		s.defs = genFuzzChildren(rng, depth, s.defOrder)
	}
	if depth > 0 && rng.Float64() < 0.35 {
		s.propOrder = fuzzSample(rng, []string{"p", "q"})
		s.props = genFuzzChildren(rng, depth, s.propOrder)
	}
	return s
}

func genFuzzChildren(rng *rand.Rand, depth int, keys []string) map[string]*reachFuzzSchema {
	out := make(map[string]*reachFuzzSchema, len(keys))
	for _, k := range keys {
		out[k] = genFuzzSchema(rng, depth-1)
	}
	return out
}

// fuzzSample returns a random-sized, randomly ordered subset of pool with no
// repeats — a size from 1 to len(pool), inclusive.
func fuzzSample(rng *rand.Rand, pool []string) []string {
	n := 1 + rng.Intn(len(pool))
	picked := slices.Clone(pool)
	rng.Shuffle(len(picked), func(i, j int) { picked[i], picked[j] = picked[j], picked[i] })
	return picked[:n]
}

// writeJSON renders s as a flow-style JSON object to embed inline in the
// surrounding YAML; every value in this grammar's fixed vocabularies is plain
// ASCII with no character %q would escape unexpectedly, so this needs no
// general JSON encoder.
func (s *reachFuzzSchema) writeJSON(sb *bytes.Buffer) {
	if s.yamlAnchor != "" {
		sb.WriteString("&" + s.yamlAnchor + " ")
	}
	sb.WriteByte('{')
	first := true
	if s.merge != "" {
		sb.WriteString("<<: *" + s.merge)
		first = false
	}
	field := func(key, value string) {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		fmt.Fprintf(sb, "%q: %q", key, value)
	}
	if s.hasRef {
		field("$ref", s.ref)
	}
	if s.hasType {
		field("type", s.typ)
	}
	if s.hasAnchor {
		field("$anchor", s.anchor)
	}
	if s.hasID {
		field("$id", s.id)
	}
	s.writeChildren(sb, &first, "$defs", s.defOrder, s.defs)
	s.writeChildren(sb, &first, "properties", s.propOrder, s.props)
	sb.WriteByte('}')
}

func (s *reachFuzzSchema) writeChildren(sb *bytes.Buffer, first *bool, key string, order []string, children map[string]*reachFuzzSchema) {
	if children == nil {
		return
	}
	if !*first {
		sb.WriteString(", ")
	}
	*first = false
	fmt.Fprintf(sb, "%q: {", key)
	for i, k := range order {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(sb, "%q: ", k)
		children[k].writeJSON(sb)
	}
	sb.WriteByte('}')
}

// genFuzzDoc generates one random document: an OpenAPI 3.1 skeleton, an
// optional "x-s" extension schema, one to three components named from
// {A, B, C}, each an independently generated reachFuzzSchema, and sometimes a
// "$defs" at the document's root, which the grammar's "#/$defs/..." pointers
// read first (GitHub #910). Sometimes the first component is a YAML anchor
// that a later one merges.
func genFuzzDoc(rng *rand.Rand) string {
	var sb bytes.Buffer
	sb.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n")
	if rng.Float64() < 0.2 {
		sb.WriteString("x-s: ")
		genFuzzSchema(rng, 1).writeJSON(&sb)
		sb.WriteByte('\n')
	}
	sb.WriteString("components:\n  schemas:\n")
	keys := fuzzSample(rng, []string{"A", "B", "C"})
	schemas := make([]*reachFuzzSchema, len(keys))
	for i := range keys {
		schemas[i] = genFuzzSchema(rng, 2)
	}
	if len(keys) > 1 && rng.Float64() < 0.35 {
		schemas[0].yamlAnchor = "base"
		schemas[1+rng.Intn(len(keys)-1)].merge = "base"
	}
	for i, k := range keys {
		sb.WriteString("    " + k + ": ")
		schemas[i].writeJSON(&sb)
		sb.WriteByte('\n')
	}
	if rng.Float64() < 0.3 {
		sb.WriteString("$defs:\n")
		for _, k := range fuzzSample(rng, []string{"m", "n"}) {
			sb.WriteString("  " + k + ": ")
			genFuzzSchema(rng, 1).writeJSON(&sb)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// reachFuzzSeed and reachFuzzN fix the grammar's sample, the documents the
// oracle compiles from it (GitHub #916). math/rand's v1 source, not
// math/rand/v2's, is documented to produce the same sequence for a seed across
// Go versions, so the same documents are generated on every run without being
// stored.
const (
	reachFuzzSeed = 1
	reachFuzzN    = 200
)

// genReachFuzzDocs generates the reachFuzzN documents, in order, from
// reachFuzzSeed.
func genReachFuzzDocs() []string {
	rng := rand.New(rand.NewSource(reachFuzzSeed))
	docs := make([]string, reachFuzzN)
	for i := range docs {
		docs[i] = genFuzzDoc(rng)
	}
	return docs
}
