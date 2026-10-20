// This file holds promotionCarriers (promotion_test.go) to the IR. That map is
// the sweep over every node the compiler promotes a deprecation into, and it is
// hand-written: a carrier the IR gains, and the compiler starts building without
// a PromoteDeprecation call, would be covered by no row and reddens nothing —
// the exact shape of the defect Parameter had before GitHub #423.
package openapi_test // external test package — exercises only the public API

import (
	"go/types"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/internal/jsonshape"
	"github.com/dexpace/morphic/ir"
)

// deprecationType is what a carrier's *ir.Deprecation field points to, named by
// the compiler rather than spelled as a string.
var deprecationType = reflect.TypeFor[ir.Deprecation]()

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
// with, which is why it is spelled here rather than skipped when the IR is
// read; each reason is held to the IR below, so an entry cannot outlive it.
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
	require.NotEmpty(t, found, "the shape holds no *ir.Deprecation field at all, so the read reached "+
		"nothing and proves nothing about the carriers the lists name")

	listed := slices.Sorted(slices.Values(slices.Concat(
		slices.Collect(maps.Keys(promotedCarrierRows)), slices.Collect(maps.Keys(unpromotedCarriers)))))
	names := make([]string, 0, len(found))
	for _, n := range found {
		names = append(names, n.Obj().Name())
	}
	assert.Empty(t, cmp.Diff(names, listed),
		"every ir struct carrying a *Deprecation must be mapped to its promotion rows or "+
			"exempted with a reason, once each (-declared +listed)")

	for _, n := range found {
		name := n.Obj().Name()
		field, _, _ := types.LookupFieldOrMethod(n, false, n.Obj().Pkg(), "Provenance")
		_, hasProvenance := field.(*types.Var)
		if _, promoted := promotedCarrierRows[name]; promoted {
			assert.True(t, hasProvenance, "%s is promoted into, so rule 4 needs it to carry a Provenance", name)
			continue
		}
		reason := unpromotedCarriers[name]
		if strings.HasPrefix(reason, "carries no Provenance") {
			assert.False(t, hasProvenance, "%s now carries a Provenance, so its exemption no longer holds: "+
				"promote into it and move it to promotedCarrierRows", name)
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
// of type *ir.Deprecation, sorted by name. It reads them off the JSON shape
// ir.Document reaches, which lists a sealed sum's variants from the ir sources,
// so a carrier is covered the day it is declared.
func deprecationCarrierTypes(t *testing.T) []*types.Named {
	t.Helper()
	p, err := decls.Load(deprecationType.PkgPath())
	require.NoError(t, err)
	shape, err := jsonshape.Read(p, "Document")
	require.NoError(t, err)

	var found []*types.Named
	for _, typ := range shape.Types {
		if slices.ContainsFunc(typ.Members, pointsToDeprecation) {
			found = append(found, typ.Named)
		}
	}
	return found
}

// pointsToDeprecation reports whether m is a field of type *ir.Deprecation,
// however many aliases spell it.
func pointsToDeprecation(m jsonshape.Member) bool {
	ptr, isPointer := types.Unalias(m.Field.Type()).(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(ptr.Elem()).(*types.Named)
	return isNamed && named.Obj().Name() == deprecationType.Name() &&
		named.Obj().Pkg().Path() == deprecationType.PkgPath()
}
