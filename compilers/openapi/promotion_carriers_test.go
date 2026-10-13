// This file holds promotionCarriers (promotion_test.go) to the IR. That map is
// the sweep over every node the compiler promotes a deprecation into, and it is
// hand-written: a carrier the IR gains, and the compiler starts building without
// a PromoteDeprecation call, would be covered by no row and reddens nothing —
// the exact shape of the defect Parameter had before GitHub #423.
package openapi_test // external test package — exercises only the public API

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// deprecationPtrType is the field type that makes an IR struct a carrier.
var deprecationPtrType = reflect.TypeFor[*ir.Deprecation]()

// maxIRTypeGraphDepth bounds the walk over the IR's static type graph (the
// bounded-recursion rule). Each distinct reflect.Type is visited once, so the
// seen set already terminates it; reaching the cap means the walk stopped being
// a walk over the IR, so it fails rather than truncating.
const maxIRTypeGraphDepth = 512

// promotedCarrierRows maps each IR struct the compiler promotes into to the
// promotionCarriers rows that witness it. Property appears twice because the
// compiler builds one at two positions — a response header and a model
// property — and the sweep has to reach both construction sites.
var promotedCarrierRows = map[string][]string{
	"AuthScheme": {"auth scheme"},
	"Operation":  {"operation"},
	"Parameter":  {"parameter"},
	"Property":   {"header", "property"},
	"TypeCommon": {"type"},
}

// unpromotedCarriers names every IR struct carrying a Deprecation that no row
// witnesses, against the reason. An entry is a claim a reviewer has to agree
// with, which is why it is spelled here rather than dropped from the walk; each
// reason is held to the IR below, so an entry cannot outlive it.
var unpromotedCarriers = map[string]string{
	"EnumMember": "carries no Provenance, so rule 4 of ir-design §12 keeps promotion out until it gains one",
	"Message":    "the OpenAPI compiler builds none: a Message is an AsyncAPI node",
	"Variant":    "carries no Provenance, so rule 4 of ir-design §12 keeps promotion out until it gains one",
}

// TestPromotionCarriers_NameEveryDeprecationCarrierInTheIR fails when the IR
// declares a struct carrying a *Deprecation that promotedCarrierRows neither maps
// to a promotionCarriers row nor unpromotedCarriers exempts with a reason that
// still holds — and when either list names a struct the IR no longer declares.
func TestPromotionCarriers_NameEveryDeprecationCarrierInTheIR(t *testing.T) {
	t.Parallel()
	found := deprecationCarrierTypes(t)
	require.NotEmpty(t, found, "the walk found no *ir.Deprecation field at all, so it reached "+
		"nothing and proves nothing about the carriers the lists name")

	listed := slices.Sorted(slices.Values(slices.Concat(
		slices.Collect(maps.Keys(promotedCarrierRows)), slices.Collect(maps.Keys(unpromotedCarriers)))))
	names := make([]string, 0, len(found))
	for _, rt := range found {
		names = append(names, rt.Name())
	}
	assert.Empty(t, cmp.Diff(names, listed),
		"every ir struct carrying a *Deprecation must be mapped to its promotion rows or "+
			"exempted with a reason, once each (-declared +listed)")

	for _, rt := range found {
		_, hasProvenance := rt.FieldByName("Provenance")
		if _, promoted := promotedCarrierRows[rt.Name()]; promoted {
			assert.True(t, hasProvenance, "%s is promoted into, so rule 4 needs it to carry a Provenance", rt.Name())
			continue
		}
		reason := unpromotedCarriers[rt.Name()]
		if strings.HasPrefix(reason, "carries no Provenance") {
			assert.False(t, hasProvenance, "%s now carries a Provenance, so its exemption no longer holds: "+
				"promote into it and move it to promotedCarrierRows", rt.Name())
		}
	}
}

// TestPromotionCarriers_RowsAreTheOnesTheCorpusBuilds holds promotedCarrierRows
// and promotionCarriers to each other: every row a struct claims is one the
// corpus builds, and every row the corpus builds is claimed by exactly one
// struct. Together with the test above, the chain runs IR → mapping → corpus row
// → construction site, and no link can be dropped alone.
func TestPromotionCarriers_RowsAreTheOnesTheCorpusBuilds(t *testing.T) {
	t.Parallel()
	doc, diags := parseCorpus(t, "extension-promotion")
	assertNoErrorDiags(t, diags)
	assert.Empty(t, doc.Messages, "the reason unpromotedCarriers gives for Message is that the "+
		"OpenAPI compiler builds none")

	var claimed []string
	for _, rows := range promotedCarrierRows {
		claimed = append(claimed, rows...)
	}
	slices.Sort(claimed)
	built := slices.Sorted(maps.Keys(promotionCarriers(t, doc)))
	assert.Empty(t, cmp.Diff(built, claimed),
		"promotedCarrierRows must claim every promotionCarriers row, once each (-built +claimed)")
}

// deprecationCarrierTypes returns every struct type the IR declares with a field
// of type *ir.Deprecation, sorted by name.
//
// The walk starts at ir.Document and visits each distinct reflect.Type once, so
// recursive shapes terminate. The sealed TypeDef sum is reached only through an
// interface, which a walk over the static type graph cannot descend into, so each
// concrete kind is walked from its own root as well — seeded from the kinds the
// ir sources declare rather than a list here, so a kind is covered the day it is
// added (pass/validate_carriers_test.go walks the same two halves for the same
// reason).
func deprecationCarrierTypes(t *testing.T) []reflect.Type {
	t.Helper()
	var found []reflect.Type
	seen := map[reflect.Type]bool{}

	var walk func(rt reflect.Type, depth int)
	walk = func(rt reflect.Type, depth int) {
		require.Less(t, depth, maxIRTypeGraphDepth, "the IR type graph nests past %d", maxIRTypeGraphDepth)
		if seen[rt] {
			return
		}
		seen[rt] = true
		switch rt.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(rt.Elem(), depth+1)
		case reflect.Map:
			walk(rt.Key(), depth+1)
			walk(rt.Elem(), depth+1)
		case reflect.Struct:
			for f := range rt.Fields() {
				if f.Type == deprecationPtrType {
					found = append(found, rt)
				}
				walk(f.Type, depth+1)
			}
		default:
			// A leaf: no other kind has a component type to descend into.
		}
	}

	walk(reflect.TypeFor[ir.Document](), 0)
	for _, kind := range sealedTypeKinds(t) {
		td, ok := ir.NewTypeDef(kind)
		require.True(t, ok, "no concrete type is registered for kind %q", kind)
		rt := reflect.TypeOf(td)
		require.Equal(t, reflect.Pointer, rt.Kind(), "NewTypeDef must return a pointer for %q", kind)
		walk(rt.Elem(), 0)
	}
	slices.SortFunc(found, func(a, b reflect.Type) int { return strings.Compare(a.Name(), b.Name()) })
	return found
}
