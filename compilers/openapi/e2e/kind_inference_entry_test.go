// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// entryClaims are allOf entries whose keywords claim an array alone, the kinds
// the position lowers to by the kind-inference policy, with the keyword a typed
// twin writes beside `type: array` and the node kind they build.
var entryClaims = []struct {
	name, keyword string
	versions      []string
	kind          ir.TypeKind
}{
	{"items", "items: {type: string}", []string{"3.0.3", "3.1.0"}, ir.KindList},
	{"prefixItems", "prefixItems: [{type: string}]", []string{"3.1.0"}, ir.KindTuple},
}

// withoutKindInference is comparableDocument of doc with the kind-inference
// marker left out, so a schema whose kind the policy reads compares with its
// twin declaring that kind.
func withoutKindInference(t *testing.T, doc *ir.Document) string {
	t.Helper()
	return strings.ReplaceAll(comparableDocument(t, doc), `"inferred":"kind-inference",`, "")
}

// TestKindInference_AnEntryClaimingThePositionsKindIsItsPrincipal holds that
// an allOf entry whose keywords claim every kind the position lowers to by the
// policy says what the position is, as one writing that type does: at every
// position `{allOf: [{K}]}` is a use of the node its entry builds, marked,
// keeping nothing and reporting nothing, and compiles to what
// `{allOf: [{type: array, K}]}` compiles to but for the marker, as `{K}` does
// beside `{type: array, K}`. With the policy off it is the top type keeping K.
func TestKindInference_AnEntryClaimingThePositionsKindIsItsPrincipal(t *testing.T) {
	t.Parallel()
	for _, tc := range entryClaims {
		for _, version := range tc.versions {
			t.Run(tc.name+"/"+version, func(t *testing.T) {
				t.Parallel()
				spelling := "{allOf: [{" + tc.keyword + "}]}"
				doc, diags := compileKindInference(t, "entry", conjunctionSpec(version, spelling), false)
				requireErrorsAndWarnings(t, diags, nil)
				for _, at := range conjunctionPositions {
					used := usedAt(t, doc, at)
					assert.Equal(t, ir.TypeID("t/anon"+string(at)+"/allOf/0"), used, "a use of the entry at %s", at)
					node := kindNode(t, doc, used)
					assert.Equal(t, tc.kind, node.Kind(), at)
					assert.True(t, markedKindInference(node), "the entry's node is the policy's reading at %s", at)
					assert.Empty(t, keptUnder(doc, at), at)
					assert.Empty(t, degradedUnder(diags, at), at)
				}

				typed, _ := compileKindInference(t, "entry", conjunctionSpec(version,
					"{allOf: [{type: array, "+tc.keyword+"}]}"), false)
				assert.Equal(t, comparableDocument(t, typed), withoutKindInference(t, doc),
					"the entry's claim lowers as its declared kind")
				flat, _ := compileKindInference(t, "entry", conjunctionSpec(version, "{"+tc.keyword+"}"), false)
				flatTyped, _ := compileKindInference(t, "entry", conjunctionSpec(version, "{type: array, "+tc.keyword+"}"), false)
				assert.Equal(t, comparableDocument(t, flatTyped), withoutKindInference(t, flat),
					"the flat spelling's claim lowers as its declared kind")

				off, offDiags := compileKindInference(t, "entry", conjunctionSpec(version, spelling), true)
				for _, at := range conjunctionPositions {
					assert.Equal(t, prim(ir.PrimAny), usedAt(t, off, at), "no kind read at %s", at)
					key := at + "/allOf/0/" + jsontext.Pointer(strings.SplitN(tc.keyword, ":", 2)[0])
					assert.Equal(t, map[jsontext.Pointer]ir.UnmodeledReason{key: ir.ReasonDegradedLowering},
						keptUnder(off, at), at)
					assert.Equal(t, []jsontext.Pointer{at + "/allOf/0"}, degradedUnder(offDiags, at), at)
				}
			})
		}
	}
}

// entryClaimOrders are allOf entries the election picks among, each written in
// both orders: an entry claiming an array beside one bounding it, beside a
// $ref to a schema admitting every kind, and beside a copy of itself.
var entryClaimOrders = []struct {
	name   string
	first  string
	second string
}{
	{"beside a bound", "{items: {type: string}}", "{minItems: 1}"},
	{"beside an untyped ref", "{items: {type: string}}", "{$ref: '#/components/schemas/Any'}"},
	{"beside its copy", "{items: {type: string}}", "{items: {type: string}}"},
}

// TestKindInference_AnEntryClaimingTheKindIsPickedInEitherOrder holds that the
// entry claiming the position's kind is its principal whichever order the
// entries are written in: the two orders compile to one document but for the
// entries' indices, the position a use of the list built at the claiming
// entry (the first copy of one written twice).
func TestKindInference_AnEntryClaimingTheKindIsPickedInEitherOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range entryClaimOrders {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			forward := fmt.Sprintf("{allOf: [%s, %s]}", tc.first, tc.second)
			backward := fmt.Sprintf("{allOf: [%s, %s]}", tc.second, tc.first)
			one, oneDiags := compileKindInference(t, "order", conjunctionSpec("3.1.0", forward, "Any: {description: d}"), false)
			two, _ := compileKindInference(t, "order", conjunctionSpec("3.1.0", backward, "Any: {description: d}"), false)
			requireErrorsAndWarnings(t, oneDiags, nil)
			if tc.first != tc.second {
				assert.Equal(t, comparableDocument(t, one), entriesSwapped(t, two), "the entry order decided the document")
			} else {
				assert.Equal(t, comparableDocument(t, one), comparableDocument(t, two))
			}
			for _, at := range conjunctionPositions {
				node := kindNode(t, one, usedAt(t, one, at))
				assert.Equal(t, ir.KindList, node.Kind(), at)
				assert.True(t, markedKindInference(node), at)
				assert.Equal(t, at+"/allOf/0", node.Common().Provenance.Pointer, at)
			}
		})
	}
}

// TestKindInference_AnEntryClaimNamesNoKindItDoesNotHoldAlone holds the gate
// on the claim: an entry says the position's kind only where the policy reads
// that kind and the entry's own keywords claim all of it. Beside a declared
// string the position is that string; beside an entry claiming an object, the
// two admit an array and an object, and the position is the top type keeping
// each; and keywords beside the allOf claiming the kind are the principal, as
// with no allOf.
func TestKindInference_AnEntryClaimNamesNoKindItDoesNotHoldAlone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema string
		want         func(t *testing.T, doc *ir.Document, at jsontext.Pointer)
	}{
		{"a declared string", "{allOf: [{type: string}, {items: {type: string}}]}",
			func(t *testing.T, doc *ir.Document, at jsontext.Pointer) {
				t.Helper()
				assert.Equal(t, prim(ir.PrimString), usedAt(t, doc, at), at)
			}},
		{"an object claimed beside it", "{allOf: [{items: {type: string}}, {properties: {a: {type: string}}}]}",
			func(t *testing.T, doc *ir.Document, at jsontext.Pointer) {
				t.Helper()
				assert.Equal(t, prim(ir.PrimAny), usedAt(t, doc, at), at)
				assert.Len(t, keptUnder(doc, at), 2, "each entry is kept at %s", at)
			}},
		{"the keywords beside it", "{items: {type: string}, allOf: [{items: {minLength: 1}}]}",
			func(t *testing.T, doc *ir.Document, at jsontext.Pointer) {
				t.Helper()
				used := usedAt(t, doc, at)
				require.Equal(t, ir.KindList, kindNode(t, doc, used).Kind(), at)
				assert.Equal(t, at, kindNode(t, doc, used).Common().Provenance.Pointer, "the position's own list at %s", at)
				assert.Contains(t, keptUnder(doc, at), at+"/allOf/0", "the entry is kept at %s", at)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileKindInference(t, "gate", conjunctionSpec("3.1.0", tc.schema), false)
			requireErrorsAndWarnings(t, diags, nil)
			for _, at := range conjunctionPositions {
				tc.want(t, doc, at)
			}
		})
	}
}

// TestKindInference_ABoundBesideARefToAnInferredListIsPlacedByTheList holds
// that a bound of another kind beside a $ref to an allOf whose entry claims an
// array is placed against the list that position lowers to: a `minLength` is
// kept beside it with an info, as the list holds no string bound, a `minItems`
// lands on the carrier, and with the policy off, where the target is the top
// type, the `minLength` is typed as any bound on the top type is.
func TestKindInference_ABoundBesideARefToAnInferredListIsPlacedByTheList(t *testing.T) {
	t.Parallel()
	spec := admissionSpec("3.1.0", "",
		"F: {allOf: [{items: {type: string}}, {minItems: 1}]}",
		"Holder: {type: object, properties: {s: {$ref: '#/components/schemas/F', minLength: 2},"+
			" n: {$ref: '#/components/schemas/F', minItems: 2}}}")
	on, onDiags := compileKindInference(t, "bound", spec, false)
	requireErrorsAndWarnings(t, onDiags, nil)
	alias, ok := kindNode(t, on, componentID("F")).(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, alias.Base)
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/F/allOf/0"), alias.Base.Target)
	assert.Equal(t, ir.KindList, kindNode(t, on, alias.Base.Target).Kind())
	props := holderProps(t, on)
	assert.True(t, props["s"].Constraints == nil || props["s"].Constraints.MinLength == nil,
		"no string bound is typed beside a list")
	assert.Contains(t, props["s"].Unmodeled, "openapi:minLength")
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/Holder/properties/s"},
		degradedUnder(onDiags, "/components/schemas/Holder/properties/s"))
	require.NotNil(t, props["n"].Constraints)
	require.NotNil(t, props["n"].Constraints.MinItems)
	assert.Equal(t, int64(2), *props["n"].Constraints.MinItems)

	off, _ := compileKindInference(t, "bound", spec, true)
	offProps := holderProps(t, off)
	require.NotNil(t, offProps["s"].Constraints)
	require.NotNil(t, offProps["s"].Constraints.MinLength, "the top type takes the bound")
	assert.Equal(t, int64(2), *offProps["s"].Constraints.MinLength)
}
