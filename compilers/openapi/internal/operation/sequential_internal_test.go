package operation

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestFillSequential_EmptyItemEncoding(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	content := &ir.Content{}
	media := &soa.MediaType{ItemEncoding: &soa.Encoding{}}
	diags := fillSequential(l.ctx, l.types, content, media, "/mp")
	assert.Equal(t, &ir.PartEncoding{Multi: true}, content.ItemEncoding,
		"a config-free itemEncoding still records that the tail repeats")
	assert.Nil(t, content.Unmodeled, "nothing is preserved raw")
	assert.Empty(t, diags)
}

// TestPositionalEncoding_WithoutRootNode covers the media type whose source node
// cannot be read. prefixEncoding is declared — nothing else routes a content
// entry here — so nothing being written is a construct lost rather than one that
// was never there, and the position reports that instead of announcing a
// preservation it did not make (GitHub #144).
func TestPositionalEncoding_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	content := &ir.Content{}
	media := &soa.MediaType{PrefixEncoding: []*soa.Encoding{{}}, ItemEncoding: &soa.Encoding{}}
	diags := fillSequential(l.ctx, l.types, content, media, "/mp")
	assert.Nil(t, content.ItemEncoding, "prefixes still block the every-item lowering")
	assert.Nil(t, content.Unmodeled, "a media type with no source node has nothing verbatim to keep")
	openapitest.AssertHasCode(t, diags, diag.UnpreservableConstruct, ir.SeverityError)
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"nothing was kept, so nothing announces that it was")
}
