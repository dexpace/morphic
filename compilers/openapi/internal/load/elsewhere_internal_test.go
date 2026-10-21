package load

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
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

// TestPositionOf_PlacesASiteInTheDocumentTheTableRead pins positionOf and
// siteIn: a source site is the source's position, another document's is found
// by its name or by the key a request reads a URL by (readKey), and one in a
// document the table never read is placed nowhere.
func TestPositionOf_PlacesASiteInTheDocumentTheTableRead(t *testing.T) {
	t.Parallel()
	_, root := buildDoc(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n"+
		"    A: {$ref: 'other.yaml#/T'}\n    B: {$ref: 'http://h/a%20b.yaml#/T'}\n")
	table := bind.Bind(root, bind.Config{Self: "spec.yaml", Open: func(string) (*yaml.Node, error) {
		var n yaml.Node
		return &n, yaml.Unmarshal([]byte("T: {type: string}\n"), &n)
	}})
	other, ok := table.Find("other.yaml")
	require.True(t, ok)
	remote, ok := table.Find("http://h/a%20b.yaml")
	require.True(t, ok)
	self := sourceDocument{path: "spec.yaml"}

	for site, want := range map[refSite]bind.Position{
		{pointer: "/x"}:                           {Doc: bind.Source, Pointer: "/x"},
		{doc: "other.yaml", pointer: "/y"}:        {Doc: other, Pointer: "/y"},
		{doc: "http://h/a b.yaml", pointer: "/z"}: {Doc: remote, Pointer: "/z"},
		self.siteIn("spec.yaml", "/w"):            {Doc: bind.Source, Pointer: "/w"},
		self.siteIn("http://h/a b.yaml", "/v"):    {Doc: remote, Pointer: "/v"},
	} {
		got, ok := self.positionOf(table, site)
		require.True(t, ok, "%+v", site)
		assert.Equal(t, want, got, "%+v", site)
	}
	_, ok = self.positionOf(table, refSite{doc: "gone.yaml", pointer: "/x"})
	assert.False(t, ok, "a document the table never read")
}

// TestUnplaced_NamesTheDocumentOfASiteOutsideTheSource pins how unplaced
// spells the reference each schema of an end the table could not place was
// read under: by its pointer in the source, and by its pointer in its document
// elsewhere.
func TestUnplaced_NamesTheDocumentOfASiteOutsideTheSource(t *testing.T) {
	t.Parallel()
	inSource := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	inA := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	bd := typed.NewBuilder(nil)

	unplaced(t.Context(), bd, referenceEnd{walk: walkOf(&soa.ReferencedParameter{Object: &soa.Parameter{Schema: inSource}})},
		refSite{pointer: "/paths/~1x/get/parameters/0"})
	unplaced(t.Context(), bd, referenceEnd{walk: walkOf(&soa.ReferencedParameter{Object: &soa.Parameter{Schema: inA}})},
		refSite{doc: "a.yaml", pointer: "/components/parameters/P"})

	site, ok := bd.Bindings().Unplaced(inSource)
	require.True(t, ok)
	assert.Equal(t, "/paths/~1x/get/parameters/0", site)
	site, ok = bd.Bindings().Unplaced(inA)
	require.True(t, ok)
	assert.Equal(t, "/components/parameters/P in a.yaml", site)
}
