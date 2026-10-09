package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// groupDoc wraps groups in one service of an otherwise clean document.
func groupDoc(tags []ir.TagDef, groups ...ir.OperationGroup) *ir.Document {
	doc := &ir.Document{IRVersion: ir.IRVersion, TagDefs: tags}
	doc.Services = []ir.Service{{ID: "s/x/S", Name: ir.Naming{Source: "s", Canonical: "s"}, Groups: groups}}
	return doc
}

func grp(id ir.GroupID) ir.OperationGroup {
	return ir.OperationGroup{ID: id, Name: ir.Naming{Source: "g", Canonical: "g"}}
}

func TestVerify_CleanGroupIDsHaveNoViolations(t *testing.T) {
	t.Parallel()
	declared := grp("g/openapi/tags/pets")
	declared.Provenance.Pointer = "/tags/0"
	nested := grp("g/synth/openapi/path-prefix/~1x")
	declared.Groups = []ir.OperationGroup{nested}
	doc := groupDoc([]ir.TagDef{{Name: "pets"}}, declared,
		grp("g/synth/openapi/default"), grp("g/synth/openapi/webhooks"), grp("g/other/Interface/Name"),
		grp("g/openapi/tags/never-declared"))
	assert.Empty(t, irverify.Verify(doc))
}

func TestVerify_GroupIDViolations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   ir.GroupID
		want string
	}{
		{"no kind prefix", "x/openapi/tags/a", "ir/id-malformed"},
		{"space alone", "g/openapi", "ir/id-malformed"},
		{"empty segment in the path", "g/openapi/tags//a", "ir/id-malformed"},
		{"trailing separator", "g/openapi/tags/", "ir/id-malformed"},
		{"unescaped tag name", "g/openapi/tags/a~b", "ir/id-malformed"},
		{"a tag name with a chain", "g/openapi/tags/a/b", "ir/id-malformed"},
		{"unknown synth rule", "g/synth/openapi/sometimes", "ir/group-id-synth-rule"},
		{"synth without a format", "g/synth", "ir/id-malformed"},
		{"default carrying a key", "g/synth/openapi/default/x", "ir/group-id-synth-rule"},
		{"path-prefix without a key", "g/synth/openapi/path-prefix", "ir/group-id-synth-rule"},
		{"path-prefix with a bad key", "g/synth/openapi/path-prefix/a~", "ir/group-id-synth-rule"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := irverify.Verify(groupDoc(nil, grp(tc.id)))
			assert.Contains(t, violationCodes(got), tc.want, "%q", tc.id)
		})
	}
}

// TestVerify_DeclaredTagGroupNeedsItsPointer plants a tag-declared group that
// forgot where its declaration is. An undeclared tag legitimately has none.
func TestVerify_DeclaredTagGroupNeedsItsPointer(t *testing.T) {
	t.Parallel()
	got := irverify.Verify(groupDoc([]ir.TagDef{{Name: "pets"}}, grp("g/openapi/tags/pets")))
	require.Equal(t, []string{"ir/group-id-tag-pointer"}, violationCodes(got))
	assert.Equal(t, "doc.Services[0].Groups[0]", got[0].Path)

	assert.Empty(t, irverify.Verify(groupDoc(nil, grp("g/openapi/tags/pets"))),
		"a used but undeclared tag has no declaration to point at")
}

// TestVerify_GroupIDClassIsCoveredWithoutAnIrverifyEdit plants the two defects
// the reflection-derived checks catch for a class they were never told about.
func TestVerify_GroupIDClassIsCoveredWithoutAnIrverifyEdit(t *testing.T) {
	t.Parallel()
	assert.Contains(t, violationCodes(irverify.Verify(groupDoc(nil, grp("")))), "ir/empty-group-id")

	nested := grp("g/synth/openapi/default")
	outer := grp("g/synth/openapi/default")
	outer.Groups = []ir.OperationGroup{nested}
	got := irverify.Verify(groupDoc(nil, outer))
	assert.Contains(t, violationCodes(got), "ir/duplicate-group-id")
}

func TestVerify_TooDeepGroupsAreReportedAsTruncation(t *testing.T) {
	t.Parallel()
	g := grp("g/synth/openapi/default")
	for range 200 {
		g = ir.OperationGroup{ID: "g/other/x", Name: g.Name, Groups: []ir.OperationGroup{g}}
	}
	assert.Contains(t, violationCodes(irverify.Verify(groupDoc(nil, g))), "ir/walk-truncated")
}
