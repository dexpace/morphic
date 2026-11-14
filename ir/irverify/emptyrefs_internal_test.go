// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestKindPrefixes_MatchTheIdentityClasses holds kindPrefixes to naming exactly
// the types identityClasses (duplicates_internal_test.go) calls an identity, so
// every class of ID is held to the grammar and to the namespace declaration,
// and none the walk-based checks were audited against is left without a
// prefix.
func TestKindPrefixes_MatchTheIdentityClasses(t *testing.T) {
	t.Parallel()
	var want []string
	for name, desc := range identityClasses {
		if strings.HasPrefix(desc, "identity") {
			want = append(want, name)
		}
	}
	require.NotEmpty(t, want, "identityClasses must classify at least one type as an identity")

	got := map[string]bool{}
	for class := range kindPrefixes {
		got[class.Name()] = true
	}
	for _, name := range want {
		assert.Contains(t, got, name, "kindPrefixes does not list %s, which identityClasses calls an identity", name)
	}
	assert.Len(t, got, len(want), "kindPrefixes lists a type identityClasses does not call an identity")
}

// TestCheckEmptyRefs_VisitingCostsNoAllocation holds the check's cost for a
// map of references none of which is empty at nothing once it has met the
// map's type: it reads values into what it keeps, where reflect.MapIter.Value
// allocates a copy of each. It is not run in parallel: AllocsPerRun measures
// the process.
func TestCheckEmptyRefs_VisitingCostsNoAllocation(t *testing.T) {
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/pet": &ir.Model{ID: "t/pet", Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]ir.TypeID{"cat": "t/cat", "dog": "t/dog", "fox": "t/fox"},
		}},
		"t/zoo": &ir.Model{ID: "t/zoo", Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]ir.TypeID{"pet": "t/pet"},
		}},
	}}
	check := checkEmptyRefs(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "no reference is empty")
}
