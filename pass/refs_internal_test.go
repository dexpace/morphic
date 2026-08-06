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

// TestCheckDanglingRefs_TruncatedWalkClaimsNoDeclarations holds the one direction
// a declaration-derived registry can be wrong in. The operation the shallow
// reference names is declared, but past the walk cap, so a registry built from
// what the walk reached answers "not declared" for a node that is — and a false
// dangling error is worse than the silence it replaced. The registries Document
// keys maps by are read off the field rather than off the walk and are unaffected.
func TestCheckDanglingRefs_TruncatedWalkClaimsNoDeclarations(t *testing.T) {
	t.Parallel()
	deep, target := opDocNested(ir.MaxWalkDepth)
	diags := checkDanglingRefs(deep)
	require.NotEmpty(t, diags)
	assert.Equal(t, "ir/walk-truncated", diags[0].Code)
	for _, d := range diags {
		assert.NotEqual(t, "ir/dangling-op-ref", d.Code,
			"%s is declared past the cap, not undeclared", target)
	}

	// The other half: the same shape within the cap resolves, so the case above
	// is not passing on a walk that collects no reference at all.
	shallow, _ := opDocNested(0)
	assert.Empty(t, checkDanglingRefs(shallow))
}

// opDocNested returns a document whose first group holds an operation overloading
// one declared inside depth levels of nested groups, plus the ID they agree on.
// At a depth past ir.MaxWalkDepth the walk reaches the reference and not the
// declaration; below it, one walk reaches both.
func opDocNested(depth int) (*ir.Document, ir.OpID) {
	target := ir.OpID("op/x/Nested")
	nested := ir.OperationGroup{Operations: []ir.Operation{{ID: target}}}
	for range depth {
		nested = ir.OperationGroup{Groups: []ir.OperationGroup{nested}}
	}
	return &ir.Document{Services: []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{
			{Operations: []ir.Operation{{ID: "op/x/Overload", OverloadOf: &target}}},
			nested,
		},
	}}}, target
}

// TestCheckDanglingRefs_NilTypeDefIsNotFollowed pins report-only behaviour on
// a malformed registry: a nil entry is skipped rather than dereferenced, so the
// pass reports what it can instead of panicking.
func TestCheckDanglingRefs_NilTypeDefIsNotFollowed(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/nil": nil}}
	assert.Empty(t, checkDanglingRefs(doc))
}
