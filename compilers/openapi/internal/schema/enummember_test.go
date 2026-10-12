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

// memberKeys returns each member's ID past its enum's path: the key, and a
// repeat's occurrence number after it.
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
	assert.Equal(t, []string{"n:-1e0", "n:1e0", "n:0"}, memberKeys(enumOf(t, doc, componentID("N"))))
}

// TestEnumMemberIDs_OneNumberSpelledTwiceIsARepeat holds a number's key to its
// value: 10, 10.0 and 1e1 are one number, so the second and third are repeats,
// each keeping the spelling it was written with.
func TestEnumMemberIDs_OneNumberSpelledTwiceIsARepeat(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    N:
      type: number
      enum: [10, 10.0, 1e1, 100]
`))
	members := enumOf(t, doc, componentID("N"))
	assert.Equal(t, []string{"n:1e1", "n:1e1/2", "n:1e1/3", "n:1e2"}, memberKeys(members))
	spelled := make([]ir.BigVal, 0, len(members))
	for _, m := range members {
		spelled = append(spelled, m.Value.Num)
	}
	assert.Equal(t, []ir.BigVal{"10", "10.0", "1e1", "100"}, spelled, "the value is kept as written")
	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DuplicateEnumValue, ir.SeverityWarning))
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
	assert.Equal(t, map[string]bool{"s:1": true, "n:1e0": true, "s:true": true, "b:true": true}, keys)
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
	assert.Equal(t, []string{"s:a", "s:b", "s:a/2", "s:a/3"}, memberKeys(enumOf(t, doc, componentID("E"))))

	var at []string
	for _, d := range diags {
		if d.Code == diag.DuplicateEnumValue {
			assert.Equal(t, ir.SeverityWarning, d.Severity)
			at = append(at, string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"/components/schemas/E/enum/2", "/components/schemas/E/enum/3"}, at)
}

// TestEnumMemberIDs_RepeatIsReportedAtItsSourceIndex covers a nullable enum,
// whose null is not a member: the pointer a repeat is reported at, and the index
// it names, are positions in the enum as written, not in the member list.
func TestEnumMemberIDs_RepeatIsReportedAtItsSourceIndex(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    E:
      type: [string, "null"]
      enum: [null, a, b, a]
`))
	assert.Equal(t, []string{"s:a", "s:b", "s:a/2"}, memberKeys(enumOf(t, doc, componentID("E"))))
	msg := openapitest.DiagMessageAt(t, diags, diag.DuplicateEnumValue, ir.SeverityWarning, "/components/schemas/E/enum/3")
	assert.Contains(t, msg, "the member at index 1;")
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
