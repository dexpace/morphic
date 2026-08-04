package pass // internal test package — exercises the collectors directly

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestCollectTypeIDs_DeepValueTreeIsTruncated drives what this pass does with a
// truncated walk: a value tree nested past the shared cap must not be silently
// under-checked, so the flag comes back out of the collector and Validate turns
// it into a diagnostic rather than claiming the document is referentially closed.
// The bound itself is ir.MaxWalkDepth's to hold; this is the reporting half.
func TestCollectTypeIDs_DeepValueTreeIsTruncated(t *testing.T) {
	t.Parallel()
	v := ir.Value{Kind: ir.ValueNull}
	for range ir.MaxWalkDepth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	m := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/m", Examples: []ir.Example{{Value: &v}}}}
	doc := &ir.Document{Types: ir.TypeRegistry{m.ID: m}}

	_, truncated := collectTypeIDs(doc, "doc")
	assert.True(t, truncated, "a tree nested past the cap must report truncation")

	diags := checkDanglingRefs(doc)
	require.NotEmpty(t, diags)
	assert.Equal(t, "ir/walk-truncated", diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

// TestCollectTypeIDs_UnmodeledBytesYieldNoReference states the result the byte
// payloads must produce: a preserved JSON blob is opaque data, so bytes that
// happen to spell a type ID are not a reference.
func TestCollectTypeIDs_UnmodeledBytesYieldNoReference(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Unmodeled: ir.Unmodeled{"openapi:x-thing": {
		Reason: ir.ReasonVendorExtension,
		Value:  ir.RawValue(`"t/ghost/in-bytes"`),
	}}}
	sites, truncated := collectTypeIDs(doc, "doc")
	assert.False(t, truncated)
	assert.Empty(t, sites)
}

// TestCheckDanglingRefs_NilTypeDefIsNotFollowed pins report-only behaviour on
// a malformed registry: a nil entry is skipped rather than dereferenced, so the
// pass reports what it can instead of panicking.
func TestCheckDanglingRefs_NilTypeDefIsNotFollowed(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/nil": nil}}
	assert.Empty(t, checkDanglingRefs(doc))
}
