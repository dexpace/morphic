// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package document

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
)

// TestSites_ListEveryObjectAroundTheMetadata pins the one list the extension
// and undeclared-key censuses share: each object, keyed by its path from the
// root, at its own pointer, holding that object and no other. A tag, nil or
// not, contributes no site: its keys are its TagDef's (operation's tagKeys).
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
	all := sites(service.New(0, &load.Document{Doc: doc}, service.Policy{}))
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
	}, got, "a tag keeps its own keys, on its TagDef, so the document keeps none")

	// The two externalDocs objects are alike in type and content, so only their
	// identity tells a site holding the other one's apart.
	want := []extensible{doc, &doc.Info, doc.Info.Contact, doc.Info.License,
		doc.ExternalDocs, doc.Components}
	require.Len(t, all, len(want))
	for i, s := range all {
		assert.Same(t, want[i], s.model, "the site at %q holds the object written there", s.owner)
	}
}
