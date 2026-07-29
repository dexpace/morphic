package pass // internal test package — exercises the walk's bounds directly

import (
	"reflect"
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

	diags := checkDanglingRefs(doc)
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
		if s.id == "t/ghost/shared" {
			n++
		}
	}
	assert.Equal(t, 1, n, "the shared pointer's target is collected once")
}

// TestCollectTypeIDs_PreservedBytesYieldNoReference states the result half: a
// preserved JSON blob is opaque data, so bytes that happen to spell a type ID
// are not a reference. This holds with or without the byte-sequence skip — a
// uint8 is not a string and could never be collected — which is why the skip
// itself is driven separately below.
func TestCollectTypeIDs_PreservedBytesYieldNoReference(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Unmodeled: ir.Unmodeled{"openapi:x-thing": {
		Reason: ir.ReasonVendorExtension,
		Value:  ir.RawValue(`"t/ghost/in-bytes"`),
	}}}
	sites, truncated := collectTypeIDs(doc, "doc")
	assert.False(t, truncated)
	assert.Empty(t, sites)
}

// TestWalkSequence_ByteSequenceIsNotDescendedInto drives the skip itself, which
// exists for cost: Unmodeled and RawConfig payloads are the largest values a
// document holds, and descending one spends a reflect.Value and a formatted path
// per byte to collect nothing.
//
// Descent is observed through the depth budget, the one effect it has that a
// visitor-less walk exposes. Starting at the cap, every element the walk
// descended into would sit one level past it and mark the walk truncated; with
// the skip nothing below the slice is reached, so it does not.
func TestWalkSequence_ByteSequenceIsNotDescendedInto(t *testing.T) {
	t.Parallel()
	w := refWalk{isRef: func(reflect.Type) bool { return true }, seen: map[uintptr]bool{}}
	w.walkSequence(reflect.ValueOf(ir.RawValue(`"t/ghost/in-bytes"`)), "doc", maxRefWalkDepth)
	assert.False(t, w.truncated, "no element of a byte sequence may be descended into")
	assert.Empty(t, w.sites)

	// The same walk one level shallower, over a sequence that is not bytes, does
	// descend — so the assertion above is the skip and not the depth arithmetic.
	deep := refWalk{isRef: func(reflect.Type) bool { return true }, seen: map[uintptr]bool{}}
	deep.walkSequence(reflect.ValueOf([]ir.TypeID{"t/x"}), "doc", maxRefWalkDepth)
	assert.True(t, deep.truncated, "a non-byte element is descended into and hits the cap")
}

// TestCheckDanglingRefs_NilTypeDefIsNotFollowed pins report-only behaviour on
// a malformed registry: a nil entry is skipped rather than dereferenced, so the
// pass reports what it can instead of panicking.
func TestCheckDanglingRefs_NilTypeDefIsNotFollowed(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/nil": nil}}
	assert.Empty(t, checkDanglingRefs(doc))
}

// TestRegistries_ResolvesUnknownClassIsReportOnly drives the unknown-class guard:
// a site whose ID type this document declares no registry for cannot resolve, and
// saying so beats indexing a zero reflect.Value and panicking. Every site
// collectRefs builds does have a registry, so nothing reaches this in practice —
// which is exactly why it is asserted here rather than left to chance.
func TestRegistries_ResolvesUnknownClassIsReportOnly(t *testing.T) {
	t.Parallel()
	regs := documentRegistries(&ir.Document{})
	site := refSite{idType: reflect.TypeOf(ir.OpID("")), id: "op/x", where: "doc"}
	assert.False(t, regs.resolves(site), "an ID class with no registry resolves to nothing")
}

// TestDocumentRegistries_DerivedFromDocumentShape pins the derivation rule that
// replaces a hand-written registry table: every ID-keyed map on Document is a
// registry, and a map keyed by plain string — Unmodeled keys on a source
// construct's name, not an identity — is not.
func TestDocumentRegistries_DerivedFromDocumentShape(t *testing.T) {
	t.Parallel()
	regs := documentRegistries(&ir.Document{})

	for _, id := range []any{ir.TypeID(""), ir.ChannelID(""), ir.MessageID(""), ir.AuthID("")} {
		assert.True(t, regs.isRef(reflect.TypeOf(id)), "%T names a Document registry", id)
	}
	assert.False(t, regs.isRef(reflect.TypeOf("")), "a plain string key is a name, not an identity")
	assert.Len(t, regs, 4, "Document declares exactly the four ID-keyed registries")
}
