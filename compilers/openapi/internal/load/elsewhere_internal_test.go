// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
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

// TestPlaces_SitePlacesASiteInItsDocument pins places.site: a source site
// where the source places it, another document's in its Document.Sources
// entry, and one in a document no entry holds at the source's root rather than
// nowhere.
func TestPlaces_SitePlacesASiteInItsDocument(t *testing.T) {
	t.Parallel()
	others := Others{list: []Other{{Index: 2, Label: "a.yaml"}}, byName: map[string]int{"dir/a.yaml": 0}}
	sites := places{source: provenance.Doc{Source: 7}, others: others}

	assert.Equal(t, ir.Provenance{Source: 7, Pointer: "/x"}, sites.site(refSite{pointer: "/x"}))
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/y"}, sites.site(refSite{doc: "dir/a.yaml", pointer: "/y"}))
	assert.Equal(t, ir.Provenance{Source: 7}, sites.site(refSite{doc: "gone.yaml", pointer: "/z"}))
}
