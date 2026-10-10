package schema_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// enumOf returns the members of the enum at id.
func enumOf(t *testing.T, doc *ir.Document, id ir.TypeID) []ir.EnumMember {
	t.Helper()
	enum, ok := doc.Types[id].(*ir.Enum)
	require.True(t, ok, "%s is an enum", id)
	return enum.Members
}

// memberKeys returns the last segment of each member's ID, which is the key.
func memberKeys(members []ir.EnumMember) []string {
	keys := make([]string, 0, len(members))
	for _, m := range members {
		_, key, _ := strings.Cut(strings.TrimPrefix(string(m.ID), "e/openapi/components/schemas/"), "/")
		keys = append(keys, key)
	}
	return keys
}

func TestEnumMemberIDs_SharedCanonicalNameStillDistinct(t *testing.T) {
	t.Parallel()
	doc, _ := lowerSpec(t, openapitest.ComponentSpec(`    E:
      type: string
      enum: [a-b, a_b, "A B"]
`))
	members := enumOf(t, doc, componentID("E"))
	require.Len(t, members, 3)
	assert.Equal(t, members[0].Name.Canonical, members[1].Name.Canonical, "the names render from the same words")
	assert.Equal(t, members[0].Name.Canonical, members[2].Name.Canonical)
	assert.Equal(t, []string{"s:a-b", "s:a_b", "s:A B"}, memberKeys(members))
}

func TestEnumMemberIDs_SignedNumbersAreThreeIDs(t *testing.T) {
	t.Parallel()
	doc, _ := lowerSpec(t, openapitest.ComponentSpec(`    N:
      type: integer
      enum: [-1, 1, 0]
`))
	assert.Equal(t, []string{"n:-1", "n:1", "n:0"}, memberKeys(enumOf(t, doc, componentID("N"))))
}

func TestEnumMemberIDs_NamesThatCanonicalizeToNothingStillGetIDs(t *testing.T) {
	t.Parallel()
	doc, _ := lowerSpec(t, openapitest.ComponentSpec(`    E:
      type: string
      enum: ["", "-", "!"]
`))
	members := enumOf(t, doc, componentID("E"))
	require.Len(t, members, 3)
	for _, m := range members {
		assert.Empty(t, m.Name.Canonical, "%q has no words", m.Name.Source)
	}
	assert.Equal(t, []string{"s:", "s:-", "s:!"}, memberKeys(members))
}

func TestEnumMemberIDs_StringAndNumberOfEqualTextDiffer(t *testing.T) {
	t.Parallel()
	doc, _ := lowerSpec(t, openapitest.ComponentSpec(`    S:
      type: string
      enum: ["1"]
    N:
      type: integer
      enum: [1]
    B:
      type: string
      enum: ["true"]
    T:
      type: boolean
      enum: [true]
`))
	keys := map[string]bool{}
	for _, name := range []string{"S", "N", "B", "T"} {
		keys[memberKeys(enumOf(t, doc, componentID(name)))[0]] = true
	}
	assert.Equal(t, map[string]bool{"s:1": true, "n:1": true, "s:true": true, "b:true": true}, keys)
}

func TestEnumMemberIDs_ReorderKeepsTheIDSet(t *testing.T) {
	t.Parallel()
	byValue := func(order string) map[string]ir.EnumMemberID {
		doc, _ := lowerSpec(t, openapitest.ComponentSpec(`    E:
      type: string
      enum: `+order+"\n"))
		out := map[string]ir.EnumMemberID{}
		for _, m := range enumOf(t, doc, componentID("E")) {
			out[m.Value.Str] = m.ID
		}
		return out
	}
	assert.Equal(t, byValue("[x, y, z]"), byValue("[z, x, y]"), "reordering renames no member")
}

func TestEnumMemberIDs_RepeatedValueIsKeptSuffixedAndWarned(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    E:
      type: string
      enum: [a, b, a, a]
`))
	assert.Equal(t, []string{"s:a", "s:b", "s:a#2", "s:a#3"}, memberKeys(enumOf(t, doc, componentID("E"))))

	var at []string
	for _, d := range diags {
		if d.Code == diag.DuplicateEnumValue {
			assert.Equal(t, ir.SeverityWarning, d.Severity)
			at = append(at, string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"/components/schemas/E/enum/2", "/components/schemas/E/enum/3"}, at)
}

func TestEnumMemberIDs_AreScopedToTheirEnum(t *testing.T) {
	t.Parallel()
	doc, _ := lowerSpec(t, openapitest.ComponentSpec(`    A:
      type: string
      enum: [x]
    B:
      type: string
      enum: [x]
    Holder:
      type: object
      properties:
        p: {type: string, enum: [x]}
`))
	a := enumOf(t, doc, componentID("A"))[0].ID
	b := enumOf(t, doc, componentID("B"))[0].ID
	inline := enumOf(t, doc, "t/anon/components/schemas/Holder/properties/p")[0].ID
	assert.Equal(t, ir.EnumMemberID("e/openapi/components/schemas/A/s:x"), a)
	assert.Equal(t, ir.EnumMemberID("e/openapi/components/schemas/B/s:x"), b)
	assert.Equal(t, ir.EnumMemberID("e/anon/components/schemas/Holder/properties/p/s:x"), inline)
}
