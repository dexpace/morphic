// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestCompareSites pins the order a finding's placement is chosen by: the
// source's sites first, then another document's by its name, then by pointer.
func TestCompareSites(t *testing.T) {
	t.Parallel()
	sites := []refSite{
		{doc: "b.yaml", pointer: "/a"},
		{doc: "a.yaml", pointer: "/z"},
		{pointer: "/z"},
		{doc: "a.yaml", pointer: "/a"},
		{pointer: "/a"},
	}

	slices.SortFunc(sites, compareSites)

	assert.Equal(t, []refSite{
		{pointer: "/a"}, {pointer: "/z"}, {doc: "a.yaml", pointer: "/a"}, {doc: "a.yaml", pointer: "/z"},
		{doc: "b.yaml", pointer: "/a"},
	}, sites)
}

// TestSitePlacer_PlacesASiteInItsDocument pins sitePlacer: a source site where
// at places it, another document's in its Document.Sources entry, and one in a
// document no entry holds at the source's root rather than nowhere.
func TestSitePlacer_PlacesASiteInItsDocument(t *testing.T) {
	t.Parallel()
	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Source: 7, Pointer: p} }
	others := Others{list: []Other{{Index: 2, Label: "a.yaml"}}, byName: map[string]int{"dir/a.yaml": 0}}
	place := sitePlacer(at, others)

	assert.Equal(t, ir.Provenance{Source: 7, Pointer: "/x"}, place(refSite{pointer: "/x"}))
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/y"}, place(refSite{doc: "dir/a.yaml", pointer: "/y"}))
	assert.Equal(t, ir.Provenance{Source: 7}, place(refSite{doc: "gone.yaml", pointer: "/z"}))
}
