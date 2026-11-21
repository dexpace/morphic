// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules

import (
	"context"
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/ir"
)

// TestCheckRepeatedKeys_ReportsEachKeyWrittenAgainAtTheRepeat holds the check
// of repeated keys over every mapping of a tree, values and list items
// included: each key a mapping writes again is one warning at its second copy,
// placed by Locate, or nowhere where none is given, and a key that is no
// scalar is none; a version the table does not cover, and a cancelled context,
// report nothing.
func TestCheckRepeatedKeys_ReportsEachKeyWrittenAgainAtTheRepeat(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\nl:\n  - {k: 1, k: 2, k: 3}\n? [x]\n: 1\n? [x]\n: 2\na: 2\n"), &doc))

	diags := CheckRepeatedKeys(t.Context(), &doc, "3.1.0", Options{})

	require.Len(t, diags, 2, "%+v", diags)
	assert.ElementsMatch(t, []ir.Position{{Line: 3, Column: 12}, {Line: 8, Column: 1}},
		[]ir.Position{diags[0].Provenance.Position, diags[1].Provenance.Position}, "the second k once, and a")

	placed := CheckRepeatedKeys(t.Context(), &doc, "3.1.0", Options{Locate: func(pointer jsontext.Pointer) ir.Provenance {
		return ir.Provenance{Pointer: pointer}
	}})
	require.Len(t, placed, 2)
	assert.ElementsMatch(t, []jsontext.Pointer{"/l/0/k", "/a"},
		[]jsontext.Pointer{placed[0].Provenance.Pointer, placed[1].Provenance.Pointer})

	assert.Nil(t, CheckRepeatedKeys(t.Context(), &doc, "2.0", Options{}), "no table covers the version")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	assert.Nil(t, CheckRepeatedKeys(cancelled, &doc, "3.1.0", Options{}), "a cut walk says nothing")
}

// TestSharedNode_NothingIsSharedWhereNothingIsWritten holds the predicate a
// frame's sharing is read off to a node that is absent.
func TestSharedNode_NothingIsSharedWhereNothingIsWritten(t *testing.T) {
	t.Parallel()
	assert.False(t, sharedNode(nil))
	assert.True(t, sharedNode(&yaml.Node{Kind: yaml.ScalarNode, Anchor: "a"}))
}

// TestTaggedOutside_FindsTheItemOfAListTaggedOutsideTheRuleset holds the
// finder of the tag a type mismatch names: a list's first item tagged outside
// YAML 1.2's JSON schema ruleset, and nothing for a list whose items all lie
// within it.
func TestTaggedOutside_FindsTheItemOfAListTaggedOutsideTheRuleset(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("[a, !local x, !!binary aGk=]\n"), &doc))
	list := doc.Content[0]

	got := taggedOutside(list)

	require.NotNil(t, got)
	assert.Equal(t, "!local", got.Tag)
	require.NoError(t, yaml.Unmarshal([]byte("[a, !!str b]\n"), &doc))
	assert.Nil(t, taggedOutside(doc.Content[0]))
	assert.Nil(t, taggedOutside(nil), "nothing is written")
}

// TestCheck_ASharedRootTellsEveryAcceptance holds Options.Shared: with it, the
// check tells Judged of every value it accepts, though nothing aliases,
// anchors or merges it, as a node other places may read as other kinds needs;
// without it, only of the refusals.
func TestCheck_ASharedRootTellsEveryAcceptance(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{name: p, in: query, required: on}\n"), &doc))
	for shared, want := range map[bool][]string{
		true:  {"accepted p", "accepted query", "refused on", "accepted {name}"},
		false: {"refused on"},
	} {
		var got []string
		NewChecker().Check(t.Context(), &doc, "3.1.0", Options{Start: Parameter, Shared: shared,
			Judged: func(node *yaml.Node, refused bool) {
				verdict, text := "accepted", node.Value
				if refused {
					verdict = "refused"
				}
				if node.Kind == yaml.MappingNode {
					text = "{" + node.Content[0].Value + "}"
				}
				got = append(got, verdict+" "+text)
			}})

		assert.ElementsMatch(t, want, got, "shared %v", shared)
	}
}
