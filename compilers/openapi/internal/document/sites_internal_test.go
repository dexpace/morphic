package document

import (
	"encoding/json/jsontext"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestTagExtensions_NilEntrySkipped pins the same guard lowerTagDefs keeps on
// the other side of the tag list: a nil entry contributes no extension site, so
// nothing dereferences it and no site is keyed at an index holding no tag.
func TestTagExtensions_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Tags: []*soa.Tag{nil, {Name: "kept"}}}

	got := tagExtensions(lowering.Ctx{Doc: doc})

	require.Len(t, got, 2, "one surviving tag contributes its own site and its externalDocs one")
	assert.Equal(t, "tags/1", got[0].Scope, "the site is keyed at the tag's own index, not its position")
	assert.Equal(t, jsontext.Pointer("/tags/1"), got[0].Owner)
}

// TestTagUnknownSites_NilEntrySkipped is TestTagExtensions_NilEntrySkipped's
// twin on the census side: the two walks of the tag list keep the same guard, so
// neither can be the one that dereferences a nil entry or keys a site at an
// index holding no tag.
func TestTagUnknownSites_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Tags: []*soa.Tag{nil, {Name: "kept"}}}

	got := tagUnknownSites(lowering.Ctx{Doc: doc})

	require.Len(t, got, 2,
		"the nil tag contributes nothing, and the surviving one contributes its own site and its externalDocs")
	assert.Equal(t, "tags/1", got[0].scope, "the site is keyed at the tag's own index, not its position")
	assert.Equal(t, jsontext.Pointer("/tags/1"), got[0].owner)
	assert.Equal(t, "tags/1/externalDocs", got[1].scope, "and its externalDocs is scoped under that same index")
	assert.Equal(t, jsontext.Pointer("/tags/1/externalDocs"), got[1].owner)
}
