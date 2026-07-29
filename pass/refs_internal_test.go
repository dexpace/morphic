package pass // internal test package — exercises the walk's bounds directly

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestCollectTypeIDs_DeepValueTreeIsTruncated drives the depth cap: a value tree
// nested past it must not be silently under-checked, so the walk reports
// truncation and Validate turns that into a diagnostic rather than claiming the
// document is referentially closed.
func TestCollectTypeIDs_DeepValueTreeIsTruncated(t *testing.T) {
	t.Parallel()
	v := ir.Value{Kind: ir.ValueNull}
	for range maxRefWalkDepth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	m := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/m", Examples: []ir.Example{{Value: &v}}}}
	doc := &ir.Document{Types: ir.TypeRegistry{m.ID: m}}

	_, truncated := collectTypeIDs(doc, "doc")
	assert.True(t, truncated, "a tree nested past the cap must report truncation")

	diags := checkDanglingTypeRefs(doc)
	require.NotEmpty(t, diags)
	assert.Equal(t, "ir/walk-truncated", diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

// TestCollectTypeIDs_SharedPointerVisitedOnce drives the cycle guard: the same
// *TypeRef reached through two template arguments is descended into once, so its
// target is collected once rather than per reference to it.
func TestCollectTypeIDs_SharedPointerVisitedOnce(t *testing.T) {
	t.Parallel()
	shared := &ir.TypeRef{Target: "t/ghost/shared"}
	m := &ir.Model{TypeCommon: ir.TypeCommon{
		ID: "t/m",
		Instantiation: &ir.TemplateInstantiation{Args: []ir.TemplateArg{
			{Type: shared},
			{Type: shared},
		}},
	}}
	doc := &ir.Document{Types: ir.TypeRegistry{m.ID: m}}

	sites, truncated := collectTypeIDs(doc, "doc")
	assert.False(t, truncated)

	var n int
	for _, s := range sites {
		if s.target == "t/ghost/shared" {
			n++
		}
	}
	assert.Equal(t, 1, n, "the shared pointer's target is collected once")
}

// TestCollectTypeIDs_PreservedBytesAreNotWalked drives the byte-sequence skip: a
// preserved JSON blob is opaque data, so bytes that happen to spell a type ID
// are not a reference — and skipping them keeps the largest part of a document
// out of the walk.
func TestCollectTypeIDs_PreservedBytesAreNotWalked(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Preserved: ir.Preserved{"openapi:x-thing": {
		Reason: ir.ReasonVendorExtension,
		Value:  ir.RawValue(`"t/ghost/in-bytes"`),
	}}}
	sites, truncated := collectTypeIDs(doc, "doc")
	assert.False(t, truncated)
	assert.Empty(t, sites)
}

// TestCheckDanglingTypeRefs_NilTypeDefIsNotFollowed pins report-only behaviour on
// a malformed registry: a nil entry is skipped rather than dereferenced, so the
// pass reports what it can instead of panicking.
func TestCheckDanglingTypeRefs_NilTypeDefIsNotFollowed(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/nil": nil}}
	assert.Empty(t, checkDanglingTypeRefs(doc))
}
