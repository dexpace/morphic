// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestVerify_ATagsKeptKeysAreHeldToTheEntryChecks holds a TagDef's Unmodeled to
// the checks every carrier gets: an entry with no reason and one under an empty
// key are findings at the tag.
func TestVerify_ATagsKeptKeysAreHeldToTheEntryChecks(t *testing.T) {
	t.Parallel()
	doc := stamped(closedDoc())
	doc.TagDefs = []ir.TagDef{{Name: "T", Unmodeled: ir.Unmodeled{
		"openapi:x-bad": {Value: ir.RawValue(`1`)},
		"":              {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`1`)},
	}}}
	got := verifyDeclared(doc)
	assert.Equal(t, []string{"ir/empty-unmodeled-key", "ir/empty-unmodeled-reason"}, codesOf(got))
	for _, v := range got {
		assert.Contains(t, v.Path, "document.TagDefs[0].Unmodeled")
	}
}

// TestVerify_AGroupsInferredMarkerIsHeldToTheJoinedForm holds the list a group
// records a promotion in to the form ir.JoinInferred writes.
func TestVerify_AGroupsInferredMarkerIsHeldToTheJoinedForm(t *testing.T) {
	t.Parallel()
	for list, want := range map[ir.Inferred][]string{
		"extension-promotion":                     nil,
		"extension-promotion,a":                   nil,
		"extension-promotion,,a":                  {"ir/inferred-not-canonical"},
		"extension-promotion,extension-promotion": {"ir/inferred-not-canonical"},
	} {
		doc := stamped(closedDoc())
		doc.Services = []ir.Service{{ID: "service/x/S", Name: named("s"),
			Groups: []ir.OperationGroup{{ID: "operation-group/x/S/g", Name: named("g"), Inferred: list}}}}
		got := verifyDeclared(doc)
		if want == nil {
			assert.Empty(t, got, string(list))
			continue
		}
		assert.Equal(t, want, codesOf(got), string(list))
		assert.Equal(t, "document.Services[0].Groups[0].Inferred", got[0].Path)
	}
}
