// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// judgment is what a check told its Judged and Referenced hooks.
type judgment struct {
	refused, accepted []*yaml.Node
	referenced        map[specrules.Kind][]*yaml.Node
	diags             []ir.Diagnostic
}

// judge checks doc as an object of kind in a 3.1 document, collecting what the
// hooks were told.
func judge(t *testing.T, kind specrules.Kind, doc string) judgment {
	t.Helper()
	var nodeValue yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &nodeValue))
	judgmentValue := judgment{referenced: map[specrules.Kind][]*yaml.Node{}}
	judgmentValue.diags = specrules.NewChecker().Check(t.Context(), &nodeValue, "3.1.0", specrules.Options{
		Start:  kind,
		Locate: func(pointer jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: pointer} },
		Judged: func(node *yaml.Node, refused bool) {
			if refused {
				judgmentValue.refused = append(judgmentValue.refused, node)
				return
			}
			judgmentValue.accepted = append(judgmentValue.accepted, node)
		},
		Referenced: func(kind specrules.Kind, node *yaml.Node) {
			judgmentValue.referenced[kind] = append(judgmentValue.referenced[kind], node)
		},
	})
	return judgmentValue
}

// values returns the Value of each node, for a comparison by what was written.
func values(nodes []*yaml.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.Value)
	}
	return out
}

// TestCheck_JudgedTellsEachValueTheCheckRefusedWhereItIsWritten holds the hook
// to the values the check refuses as the wrong type, outside a shared frame
// only, and never to a null or to a value it accepts.
func TestCheck_JudgedTellsEachValueTheCheckRefusedWhereItIsWritten(t *testing.T) {
	t.Parallel()

	judgment := judge(t, specrules.Parameter, "{name: n, in: query, required: \"yes\", explode: on, deprecated: ~, schema: {type: string}}")

	assert.ElementsMatch(t, []string{"yes", "on"}, values(judgment.refused))
	assert.Empty(t, judgment.accepted, "outside a shared frame, an accepted value is not told")
	assert.Len(t, judgment.diags, 3, "the two errors and the null's warning, which the hook is not told of")
}

// TestCheck_JudgedTellsAnObjectThatIsNoMappingAndATaggedValue holds the hook to
// the places the check reports a notMapping and a value tagged outside the
// ruleset, each told as refused.
func TestCheck_JudgedTellsAnObjectThatIsNoMappingAndATaggedValue(t *testing.T) {
	t.Parallel()

	judgment := judge(t, specrules.Components, "{parameters: {a: hello}, schemas: {S: {title: !local t, description: !!str d, maxLength: !!int 3, example: !!binary aGk=}}}")

	assert.ElementsMatch(t, []string{"hello", "t"}, values(judgment.refused), "the entry that is no object, and the tagged title")
}

// TestCheck_ATaggedItemOfAListIsNamedByItsTag holds a list refused for an item
// it holds under a tag outside the ruleset to the sentence naming that item's
// tag, which the list itself, written plain, does not carry.
func TestCheck_ATaggedItemOfAListIsNamedByItsTag(t *testing.T) {
	t.Parallel()

	judgment := judge(t, specrules.Schema, "{type: [string, !acme integer]}")

	require.Len(t, judgment.diags, 1)
	assert.Equal(t, mismatch, judgment.diags[0].Code)
	assert.Contains(t, judgment.diags[0].Message, `; it is tagged "!acme", outside YAML 1.2's JSON schema ruleset`)
}

// TestCheck_ATagOutsideTheRulesetIsRefusedWithTheTagSaid holds the table's error
// for a scalar or a sequence under a tag the JSON schema ruleset lacks, saying
// the tag and the minor, and none for the tags it has.
func TestCheck_ATagOutsideTheRulesetIsRefusedWithTheTagSaid(t *testing.T) {
	t.Parallel()

	judgment := judge(t, specrules.Schema, "{title: !local t, type: !local [string], description: !!str d, maxLength: !!int 3, example: !!binary aGk=}")

	got := map[string]string{}
	for _, diagnostic := range judgment.diags {
		assert.Equal(t, mismatch, diagnostic.Code)
		assert.Equal(t, ir.SeverityError, diagnostic.Severity)
		got[string(diagnostic.Provenance.Pointer)] = diagnostic.Message
	}
	assert.Contains(t, got["/title"], `; it is tagged "!local", outside YAML 1.2's JSON schema ruleset, to which OpenAPI 3.1 limits tags (Format)`)
	assert.Contains(t, got["/type"], `it is tagged "!local", outside YAML 1.2's JSON schema ruleset`)
	assert.NotContains(t, got, "/description", "!!str is in the ruleset")
	assert.NotContains(t, got, "/maxLength", "!!int is in the ruleset")
	assert.NotContains(t, got, "/example", "a field of any shape is the values' to read")
}

// TestCheck_JudgedTellsEachValueAFrameSharesAcceptedOrRefused holds the hook to
// a node two places read: an anchored mapping mounted as a schema and as a
// properties map, where the check accepts it in one and refuses it in the other,
// is told both ways, so a caller can leave it as written.
func TestCheck_JudgedTellsEachValueAFrameSharesAcceptedOrRefused(t *testing.T) {
	t.Parallel()

	judgment := judge(t, specrules.Components, "{schemas: {A: &m {type: string, maxLength: 3}, B: {type: object, properties: *m}}}")

	assert.Contains(t, values(judgment.accepted), "3", "A reads maxLength as a keyword")
	assert.Contains(t, values(judgment.refused), "3", "B reads it as a schema, and refuses it")
	assert.Contains(t, values(judgment.refused), "string")
	acceptedNodes := map[*yaml.Node]bool{}
	for _, node := range judgment.accepted {
		acceptedNodes[node] = true
	}
	var both int
	for _, node := range judgment.refused {
		if acceptedNodes[node] {
			both++
		}
	}
	assert.Positive(t, both, "the same node, told both ways")
}

// TestCheck_JudgedIsToldOfAValueRefusedInEveryPlaceOfASharedFrame holds a
// parameter an alias mounts twice, which each place refuses, to being refused
// and never accepted.
func TestCheck_JudgedIsToldOfAValueRefusedInEveryPlaceOfASharedFrame(t *testing.T) {
	t.Parallel()

	judgment := judge(t, specrules.Components, "{parameters: {a: &p {name: n, in: query, required: \"yes\"}, b: *p}}")

	assert.Contains(t, values(judgment.refused), "yes")
	assert.NotContains(t, values(judgment.accepted), "yes")
	assert.Contains(t, values(judgment.accepted), "query", "a value accepted in a shared frame is told so")
}

// TestCheck_AFieldOfAnyShapeAcceptsNothing holds a shared scalar a parameter's
// required refuses to being refused and never accepted when the other place
// reads it as a value: a default, an example, a const or an enum member, which
// read no type and so have no say.
func TestCheck_AFieldOfAnyShapeAcceptsNothing(t *testing.T) {
	t.Parallel()
	for name, use := range map[string]string{
		"schema default": "schema: {type: string, default: *r}",
		"schema example": "schema: {type: string, example: *r}",
		"schema const":   "schema: {type: string, const: *r}",
		"enum member":    "schema: {type: string, enum: [*r]}",
		"example":        "schema: {type: string}, example: *r",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			judgment := judge(t, specrules.Components, "{x-r: &r \"yes\", parameters: {a: {name: n, in: query, required: *r, "+use+"}}}")

			assert.Contains(t, values(judgment.refused), "yes")
			assert.NotContains(t, values(judgment.accepted), "yes")
		})
	}
}

// TestCheck_ReferencedTellsEachMappingReadAsAReferenceObject holds the hook to
// the mappings read as a Reference Object where an object of kind belongs, its
// aliases followed, and to no inline object.
func TestCheck_ReferencedTellsEachMappingReadAsAReferenceObject(t *testing.T) {
	t.Parallel()

	judgment := judge(t, specrules.Components,
		"{parameters: {a: {$ref: '#/x'}, b: &r {$ref: ~}, c: *r, d: {name: n, in: query, schema: {type: string}}}}")

	got := judgment.referenced[specrules.Parameter]
	require.Len(t, got, 3, "a, b and c")
	for _, node := range got {
		assert.Equal(t, yaml.MappingNode, node.Kind, "an alias is followed")
		assert.Equal(t, "$ref", node.Content[0].Value)
	}
	assert.Len(t, judgment.referenced, 1, "no other kind")
}

// TestCheck_AFindingNamesTheLeastPositionThatReadIt holds the position a
// finding on a node two places mount names, where it is placed elsewhere than
// either, to the least of them in both declaration orders.
func TestCheck_AFindingNamesTheLeastPositionThatReadIt(t *testing.T) {
	t.Parallel()
	messages := make([]string, 0, 4)
	for _, doc := range []string{"{schemas: {B: &m {type: 7}, A: *m}}", "{schemas: {A: &m {type: 7}, B: *m}}"} {
		var node yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(doc), &node))
		diags := specrules.NewChecker().Check(t.Context(), &node, "3.1.0", specrules.Options{
			Start:  specrules.Components,
			Locate: func(jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: "/placed"} },
			ReadAt: true,
		})
		for _, diagnostic := range diags {
			messages = append(messages, diagnostic.Message)
		}
	}

	require.Len(t, messages, 4, "two findings in each order")
	assert.Equal(t, messages[:2], messages[2:])
	assert.Contains(t, messages[0], "; read at /schemas/A/type")
}
