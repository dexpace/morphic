package document

import (
	"encoding/json/jsontext"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestSites_ListEveryObjectAroundTheMetadata pins the one list the extension
// and undeclared-key censuses share: each object, keyed by its path from the
// root, at its own pointer. A nil tag contributes no site, the guard
// lowerTagDefs keeps on the other side of the tag list, so nothing
// dereferences it and no site is keyed at an index holding no tag.
func TestSites_ListEveryObjectAroundTheMetadata(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Tags: []*soa.Tag{nil, {Name: "kept"}}}

	type keyed struct {
		scope string
		owner jsontext.Pointer
	}
	all := sites(lowering.Ctx{Doc: doc})
	got := make([]keyed, 0, len(all))
	for _, s := range all {
		got = append(got, keyed{s.scope, s.owner})
	}

	assert.Equal(t, []keyed{
		{"", ""},
		{"info", "/info"},
		{"info/contact", "/info/contact"},
		{"info/license", "/info/license"},
		{"externalDocs", "/externalDocs"},
		{"components", "/components"},
		{"tags/1", "/tags/1"},
		{"tags/1/externalDocs", "/tags/1/externalDocs"},
	}, got, "the surviving tag is keyed at its own index, not its position among the survivors")
}
