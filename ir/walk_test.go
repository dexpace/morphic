package ir_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// walkPaths returns every path ir.WalkValues reaches from root, in walk order,
// plus whether the depth cap cut the walk short.
func walkPaths(root any) ([]string, bool) {
	var paths []string
	truncated := ir.WalkValues(root, "doc", func(_ reflect.Value, path string) bool {
		paths = append(paths, path)
		return true
	})
	return paths, truncated
}

// nestedListValue returns a Value that is depth levels of single-element lists
// wrapping a number — the shape a deeply-nested array default lowers to.
func nestedListValue(depth int) ir.Value {
	v := ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("0")}
	for range depth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	return v
}

// TestWalkValues_PathsSpellFieldsIndicesAndKeys pins the three path rules two
// checkers report against: a field is joined by ".", a slice index and a map key
// sit in brackets (a key carrying ir.MapKeySuffix so a caller can tell it from a
// value), and an embedded field contributes no segment of its own — Examples is
// declared on TypeCommon, and is reached below the model rather than below a
// ".TypeCommon" step that neither JSON nor Go has.
func TestWalkValues_PathsSpellFieldsIndicesAndKeys(t *testing.T) {
	t.Parallel()
	value := ir.Value{Kind: ir.ValueNull}
	doc := &ir.Document{
		Name: "api",
		Types: ir.TypeRegistry{"t/x/M": &ir.Model{TypeCommon: ir.TypeCommon{
			ID:       "t/x/M",
			Examples: []ir.Example{{Value: &value}},
		}}},
	}

	paths, truncated := walkPaths(doc)
	require.False(t, truncated)
	assert.Contains(t, paths, "doc.Name")
	assert.Contains(t, paths, "doc.Types[t/x/M]"+ir.MapKeySuffix)
	assert.Contains(t, paths, "doc.Types[t/x/M].Examples[0].Value.Kind")
	assert.NotContains(t, paths, "doc.Types[t/x/M].TypeCommon.ID",
		"an embedded field contributes no path segment")
}

// TestWalkValues_MapEntriesAreVisitedInRenderedKeyOrder holds the ordering rule
// invariant 7 rests on. Go randomizes map iteration, and a pointer reachable
// from two entries is descended into at whichever entry the walk reaches first —
// so an unordered walk records a different path for it run to run, which is a
// different result set rather than a different order, and no later sort repairs
// that.
func TestWalkValues_MapEntriesAreVisitedInRenderedKeyOrder(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	for _, id := range []ir.TypeID{"t/c", "t/a", "t/d", "t/b"} {
		doc.Types[id] = &ir.Any{TypeCommon: ir.TypeCommon{ID: id}}
	}

	paths, _ := walkPaths(doc)
	var entries []string
	for _, p := range paths {
		if strings.HasPrefix(p, "doc.Types[") && strings.HasSuffix(p, ir.MapKeySuffix) {
			entries = append(entries, p)
		}
	}
	require.Len(t, entries, 4)
	assert.True(t, slices.IsSorted(entries), "entries are walked in rendered-key order, got %v", entries)
}

// TestWalkValues_DeepValueTreeIsTruncated drives the depth cap from both sides:
// a document nested past it must report truncation rather than be under-checked
// in silence, and one nested deep but within the bound must not.
func TestWalkValues_DeepValueTreeIsTruncated(t *testing.T) {
	t.Parallel()
	docWith := func(v ir.Value) *ir.Document {
		return &ir.Document{Types: ir.TypeRegistry{"t/m": &ir.Model{TypeCommon: ir.TypeCommon{
			ID:       "t/m",
			Examples: []ir.Example{{Value: &v}},
		}}}}
	}

	_, truncated := walkPaths(docWith(nestedListValue(ir.MaxWalkDepth)))
	assert.True(t, truncated, "a tree nested past the cap must report truncation")

	_, truncated = walkPaths(docWith(nestedListValue(200)))
	assert.False(t, truncated, "a tree a compiler can produce must be walked whole")
}

// TestWalkValues_SharedPointerIsDescendedIntoOnce holds the cycle guard. Nothing
// in a compiled document reaches it — a Document is a flat registry of values
// referenced by ID, so no two fields point at one struct — which is exactly why
// it needs planting: the guard is what keeps the walk terminating on a document
// that does share a pointer, and without one, removing it changes nothing.
func TestWalkValues_SharedPointerIsDescendedIntoOnce(t *testing.T) {
	t.Parallel()
	shared := &ir.TypeRef{Target: "t/x/Shared"}
	doc := &ir.Document{Types: ir.TypeRegistry{"t/m": &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/m", Instantiation: &ir.TemplateInstantiation{
			Args: []ir.TemplateArg{{Type: shared}, {Type: shared}},
		}},
	}}}

	paths, truncated := walkPaths(doc)
	require.False(t, truncated, "two template arguments are not deep")
	var reached int
	for _, p := range paths {
		if strings.HasSuffix(p, ".Target") {
			reached++
		}
	}
	assert.Equal(t, 1, reached, "the second argument finds the pointer already seen and stops there")
}

// TestWalkValues_CyclicPointerGraphTerminates states the same guard as a
// liveness claim: a value graph that points back at itself is normal input for a
// schema language, and the walk has to end on one rather than spin until the
// depth cap.
func TestWalkValues_CyclicPointerGraphTerminates(t *testing.T) {
	t.Parallel()
	type node struct{ Next *node }
	loop := &node{}
	loop.Next = loop

	paths, truncated := walkPaths(loop)
	assert.False(t, truncated, "the cycle guard stops the walk well before the depth cap")
	assert.Equal(t, []string{"doc", "doc", "doc.Next"}, paths,
		"the pointer, the struct behind it, then the field that leads back to it")
}

// payloadBytes sizes the preserved payload the byte-skip test walks. It is large
// enough that descending it would dominate any plausible visit count for a
// two-node document, and small enough to build inline.
const payloadBytes = 4096

// TestWalkValues_ByteSequencesAreNotDescendedInto drives the byte-sequence skip,
// which exists for cost rather than for correctness: a uint8 element is none of
// the things a visitor looks for, so collecting nothing from it is the same
// result either way and only the price differs.
//
// The visit count is what makes that assertable: since the result is the same
// with or without the skip, a test reading the result cannot notice the skip
// going missing. Without it the count grows past the payload's own length.
func TestWalkValues_ByteSequencesAreNotDescendedInto(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Unmodeled: ir.Unmodeled{"openapi:x-thing": {
		Reason: ir.ReasonVendorExtension,
		Value:  ir.RawValue(`"` + strings.Repeat("t", payloadBytes) + `"`),
	}}}

	paths, truncated := walkPaths(doc)
	require.False(t, truncated)
	assert.Less(t, len(paths), payloadBytes,
		"walking %d bytes of payload one value at a time is what the skip exists to avoid", payloadBytes)
}

// TestWalkValues_VisitorPrunesChildren holds the one lever a visitor has over
// the walk: returning false stops the descent at that value, which is how a
// check that has learned all it needs from a node avoids paying for the subtree
// below it.
func TestWalkValues_VisitorPrunesChildren(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Name: "api", Types: ir.TypeRegistry{
		"t/m": &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/m"}},
	}}

	var pruned []string
	ir.WalkValues(doc, "doc", func(v reflect.Value, path string) bool {
		pruned = append(pruned, path)
		return v.Type() != reflect.TypeFor[ir.TypeRegistry]()
	})

	assert.Contains(t, pruned, "doc.Types")
	for _, p := range pruned {
		assert.False(t, strings.HasPrefix(p, "doc.Types["), "%s sits below the pruned registry", p)
	}
}

// TestWalkValues_NothingUnreachableIsVisited covers the three ways the walk
// reaches nothing: an invalid root, a nil pointer, and a nil interface. None may
// be handed to a visitor, which would then have to guard every Type() call
// against the zero reflect.Value.
func TestWalkValues_NothingUnreachableIsVisited(t *testing.T) {
	t.Parallel()
	paths, truncated := walkPaths(nil)
	assert.Empty(t, paths, "an invalid root is not visited")
	assert.False(t, truncated)

	// Contact is a nil *Contact and the registry entry is a nil TypeDef, so the
	// walk reaches the pointer and the interface but nothing behind either.
	doc := &ir.Document{Contact: nil, Types: ir.TypeRegistry{"t/nil": nil}}
	paths, _ = walkPaths(doc)
	assert.Contains(t, paths, "doc.Contact", "the nil pointer itself is visited")
	assert.Contains(t, paths, "doc.Types[t/nil]", "the nil interface itself is visited")
	assert.NotContains(t, paths, "doc.Contact.Name", "nothing behind a nil pointer is reachable")
}

// TestWalkValues_ArrayElementsAreReached pins that a fixed-size array is walked
// like a slice. The IR declares none today, so this states the rule rather than
// guarding a live shape: a kind the walk silently skipped would take references
// with it.
func TestWalkValues_ArrayElementsAreReached(t *testing.T) {
	t.Parallel()
	holder := &struct{ IDs [2]ir.TypeID }{IDs: [2]ir.TypeID{"t/a", "t/b"}}

	paths, _ := walkPaths(holder)
	assert.Contains(t, paths, "doc.IDs[0]")
	assert.Contains(t, paths, "doc.IDs[1]")
}
