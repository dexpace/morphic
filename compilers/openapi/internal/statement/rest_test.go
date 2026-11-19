// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// nodeKeys returns the keys the mapping node writes itself, in order.
func nodeKeys(t *testing.T, node *yaml.Node) []string {
	t.Helper()
	require.NotNil(t, node)
	var out []string
	for i := 0; i+1 < len(node.Content); i += 2 {
		out = append(out, node.Content[i].Value)
	}
	return out
}

// TestRead_ASplitEntrysRestListsNoReference pins the rest of an allOf entry
// writing a $ref beside other keys: the conjunct listed after that reference,
// writing none of its own, its node holding its keys alone, and its statement
// listing its own allOf and no reference, as the entry {…} of `{allOf:
// [{$ref: …}, {…}]}` reads. Read as a position, the same object is the two
// conjuncts, whichever reading comes first.
func TestRead_ASplitEntrysRestListsNoReference(t *testing.T) {
	t.Parallel()
	spec := document("3.1.0", "T: {type: string}",
		"S: {allOf: [{$ref: '#/components/schemas/T', description: d, allOf: [{minLength: 1}]}]}")
	for _, positionFirst := range []bool{false, true} {
		table, loweringContext := tableOver(t, spec, rulesAt(t, "3.1.0", ""))
		at := loweringContext.Position("/components/schemas/S/allOf/0")
		if positionFirst {
			table.At(at)
		}
		st := table.At(component(loweringContext, "S"))
		require.Equal(t, []string{
			"/allOf/0 wrapper=false capped=false",
			"/allOf/0/allOf/0 wrapper=false capped=false",
			"/allOf/0 wrapper=true capped=false",
		}, listing(st))
		ref, rest := st.Entries[0].Schema, st.Entries[2].Schema
		require.NotNil(t, ref.Ref)
		assert.Nil(t, rest.Ref, "the rest writes no reference")
		assert.Nil(t, rest.BesideRef, "nor holds one as a conjunct")
		assert.Same(t, ref, rest.WrittenBeside, "it names the entry before it")
		assert.Equal(t, []string{"description", "allOf"}, keyNames(rest))
		assert.Equal(t, []string{"description", "allOf"}, nodeKeys(t, rest.Node), "its node writes its keys alone")
		read := table.Read(rest)
		assert.Same(t, rest, read.Schema)
		assert.Equal(t, []string{"/allOf/0 wrapper=false capped=false"}, listing(read), "its own allOf, no reference")
		assert.Same(t, read, table.Read(rest), "built once")

		position := table.At(at)
		require.NotSame(t, rest, position.Schema, "a position reads the object otherwise")
		assert.Same(t, ref, position.BesideRef, "as the reference and the words, the one reference")
		require.NotEmpty(t, position.Entries)
		assert.Same(t, ref, position.Entries[0].Schema)
		assert.Nil(t, position.WrittenBeside)
	}
}

// TestRead_ARestOfAMergedReferenceWritesItsKeysAlone pins that the node of
// an entry's rest holds the keys the statement reads, merges expanded: a
// $ref an entry merges in is no key of its rest's node, which a reader
// rendering the node verbatim would otherwise keep.
func TestRead_ARestOfAMergedReferenceWritesItsKeysAlone(t *testing.T) {
	t.Parallel()
	spec := strings.Replace(document("3.1.0", "T: {type: string}", "S: {allOf: [{<<: *r, description: d}]}"),
		"paths: {}\n", "paths: {}\nx-r: &r {$ref: '#/components/schemas/T'}\n", 1)
	table, loweringContext := tableOver(t, spec, rulesAt(t, "3.1.0", ""))
	st := table.At(component(loweringContext, "S"))
	require.Len(t, st.Entries, 2)
	require.NotNil(t, st.Entries[0].Ref, "the merged reference conjoins")
	rest := st.Entries[1].Schema
	assert.Equal(t, []string{"description"}, nodeKeys(t, rest.Node))
	assert.Equal(t, "d", rest.Node.Content[1].Value)
}

// TestRead_AnEntrysRestIsOneWhicheverListReadsIt pins that an entry's rest is
// built once: the composition holding it and a wrapper around it, each
// flattening the entry, list one rest and one reference, so what a walk
// remembers of either is the same whichever statement it read it from.
func TestRead_AnEntrysRestIsOneWhicheverListReadsIt(t *testing.T) {
	t.Parallel()
	table, loweringContext := tableOver(t, document("3.1.0", "T: {type: string}",
		"S: {allOf: [{allOf: [{$ref: '#/components/schemas/T', description: d}]}]}"), rulesAt(t, "3.1.0", ""))
	st := table.At(component(loweringContext, "S"))
	require.Len(t, st.Entries, 3)
	wrapper := table.Read(st.Entries[2].Schema)
	require.Len(t, wrapper.Entries, 2)
	assert.Same(t, st.Entries[0].Schema, wrapper.Entries[0].Schema, "one reference")
	assert.Same(t, st.Entries[1].Schema, wrapper.Entries[1].Schema, "one rest")
}
