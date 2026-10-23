package document

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestSites_ListEveryObjectAroundTheMetadata pins the one list the extension
// and undeclared-key censuses share: each object, keyed by its path from the
// root, at its own pointer, holding that object and no other. A nil tag
// contributes no site, the guard operation's lowerTagDefs keeps over the same
// list, so nothing dereferences it and no site is keyed at an index holding no
// tag.
func TestSites_ListEveryObjectAroundTheMetadata(t *testing.T) {
	t.Parallel()
	kept := &soa.Tag{Name: "kept", ExternalDocs: &oas3.ExternalDocumentation{}}
	doc := &soa.OpenAPI{
		Info:         soa.Info{Contact: &soa.Contact{}, License: &soa.License{}},
		ExternalDocs: &oas3.ExternalDocumentation{},
		Components:   &soa.Components{},
		Tags:         []*soa.Tag{nil, kept},
	}

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

	// The two externalDocs objects are alike in type and content, so only their
	// identity tells a site holding the other one's apart.
	want := []extensible{doc, &doc.Info, doc.Info.Contact, doc.Info.License,
		doc.ExternalDocs, doc.Components, kept, kept.ExternalDocs}
	require.Len(t, all, len(want))
	for i, s := range all {
		assert.Same(t, want[i], s.model, "the site at %q holds the object written there", s.owner)
	}
}
