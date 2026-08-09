package irverify

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// declaredIDViolations runs the empty-identity check and drops the truncation
// flag, which the cases below assert nothing about;
// TestWalkChecks_EachReportsTruncation holds that half.
func declaredIDViolations(doc *ir.Document) []Violation {
	vs, _ := checkDeclaredIDs(doc, declarations{})
	return vs
}

// idBearingDoc holds one node of every type in the IR that declares an ID of its
// own. present says whether the classes Document keys no map by carry theirs;
// the map-keyed ones always do, because an empty key is checkRegistryKeys'
// business and a fixture that left one empty would be testing that rule instead.
func idBearingDoc(present bool) *ir.Document {
	model := &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/x/M"},
		Properties: []ir.Property{{
			ID:   pick(present, ir.PropID("p/x/M/f")),
			Type: ir.TypeRef{Target: "t/x/M"},
		}},
	}
	return &ir.Document{
		Types:    ir.TypeRegistry{model.ID: model},
		Channels: map[ir.ChannelID]ir.Channel{"c/x/C": {ID: "c/x/C"}},
		Messages: map[ir.MessageID]ir.Message{"m/x/M": {ID: "m/x/M"}},
		Auth:     map[ir.AuthID]ir.AuthScheme{"auth/x/A": {ID: "auth/x/A"}},
		Services: []ir.Service{{
			ID: pick(present, ir.ServiceID("s/x/S")),
			Groups: []ir.OperationGroup{{
				Operations: []ir.Operation{{ID: pick(present, ir.OpID("op/x/S/op"))}},
			}},
		}},
	}
}

// pick returns want when present and the zero identity otherwise, so one fixture
// spells both halves of the comparison below.
func pick[T ~string](present bool, want T) T {
	if present {
		return want
	}
	return ""
}

// TestCheckDeclaredIDs_ReachesEveryIDDeclaringNode is the guard on this check's
// copy of ir.declaredID's predicate. ir.DeclaredIDs answers only for the
// non-empty identities, so the check reads the value graph itself, and a copy
// that drifted would go silent on whatever class it stopped recognizing rather
// than fail.
//
// The two are compared on the same fixture read two ways: every declaration
// ir.DeclaredIDs finds for a class with no registry must be reported when that
// same node carries nothing, at the same path. Nothing here is hand-listed, so a
// new ID-bearing node type joins both sides at once.
func TestCheckDeclaredIDs_ReachesEveryIDDeclaringNode(t *testing.T) {
	t.Parallel()
	filled := idBearingDoc(true)
	decls, truncated := ir.DeclaredIDs(filled)
	require.False(t, truncated)
	require.NotEmpty(t, decls)

	keyed := ir.DocumentRegistries(filled)
	var want []string
	for _, d := range decls {
		if _, hasRegistry := keyed[d.Class]; hasRegistry {
			continue
		}
		want = append(want, "ir/empty-"+ir.RefNoun(d.Class)+"-id "+d.Path)
	}
	require.NotEmpty(t, want, "the fixture must declare a class Document keys no map by")

	reported := declaredIDViolations(idBearingDoc(false))
	got := make([]string, 0, len(reported))
	for _, v := range reported {
		got = append(got, v.Code+" "+v.Path)
	}
	assert.Empty(t, cmp.Diff(want, got),
		"this check and ir.DeclaredIDs must find the same declarations (-ir +irverify)")
}

// TestCheckDeclaredIDs_PopulatedIDsAreClean is the silent half. The fixture
// differs from the one above only in whether the identities are there, so
// nothing but the identity can be what the check reads.
func TestCheckDeclaredIDs_PopulatedIDsAreClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, declaredIDViolations(idBearingDoc(true)))
}

// TestCheckDeclaredIDs_EmptyOperationAndServiceIDs is the reported shape:
// neither class has a registry key for checkRegistryKeys to read, and
// ir.DeclaredIDs drops an empty ID before checkDuplicateIDs could see it, so
// before this check nothing said anything about either.
func TestCheckDeclaredIDs_EmptyOperationAndServiceIDs(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{{
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{{}, {}}}},
	}}}

	got := declaredIDViolations(doc)
	require.Len(t, got, 3, "one service and both operations declare nothing")
	assert.Equal(t, "ir/empty-service-id", got[0].Code)
	assert.Equal(t, "doc.Services[0]", got[0].Path)
	assert.Equal(t, "ir/empty-op-id", got[1].Code)
	assert.Equal(t, "doc.Services[0].Groups[0].Operations[0]", got[1].Path)
	assert.Equal(t, "doc.Services[0].Groups[0].Operations[1]", got[2].Path)
}

// TestCheckDeclaredIDs_EmptyPropertyID covers the third class with no registry
// key. A property is a position inside its model rather than a document-level
// entry, but its ID is minted from a source pointer like any other and every
// PropID reference in the document — a PropPath segment, an encoding key, a
// discriminator's tag — resolves against it.
func TestCheckDeclaredIDs_EmptyPropertyID(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/x/M": &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/x/M"},
		Properties: []ir.Property{{Type: ir.TypeRef{Target: "t/x/M"}}},
	}}}

	got := declaredIDViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/empty-prop-id", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Properties[0]", got[0].Path)
}

// TestCheckDeclaredIDs_MapKeyedClassesAreLeftToTheRegistryRule pins the
// exclusion. checkRegistryKeys already reports an empty identity in these four
// classes from the registry key, so reporting it again here would give one
// defect two reports under one code.
func TestCheckDeclaredIDs_MapKeyedClassesAreLeftToTheRegistryRule(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{
		Types:    ir.TypeRegistry{"": &ir.Any{}},
		Channels: map[ir.ChannelID]ir.Channel{"": {}},
		Messages: map[ir.MessageID]ir.Message{"": {}},
		Auth:     map[ir.AuthID]ir.AuthScheme{"": {}},
	}
	assert.Empty(t, declaredIDViolations(doc))

	codes := map[string]bool{}
	for _, v := range Verify(doc) {
		codes[v.Code] = true
	}
	for _, noun := range []string{"type", "channel", "message", "auth"} {
		assert.True(t, codes["ir/empty-"+noun+"-id"],
			"checkRegistryKeys must still be the one reporting an empty %s identity", noun)
	}
}

// TestVerify_ReportsEmptyDeclaredIDs pins that Verify runs the check, not just
// the test: a node with no identity has to reach a caller that only ever calls
// Verify.
func TestVerify_ReportsEmptyDeclaredIDs(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{{
		Name:   ir.Naming{Source: "svc", Canonical: "svc"},
		Groups: []ir.OperationGroup{{Name: ir.Naming{Source: "g", Canonical: "g"}}},
	}}}

	found := Verify(doc)
	codes := make([]string, 0, len(found))
	for _, v := range found {
		codes = append(codes, v.Code)
	}
	assert.Contains(t, codes, "ir/empty-service-id")
}
