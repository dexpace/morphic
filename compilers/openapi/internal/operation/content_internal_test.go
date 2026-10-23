package operation

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
)

func TestLowerPayload_NilMediaEntriesYieldNil(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	content := sequencedmap.New(
		sequencedmap.NewElem("application/json", (*soa.MediaType)(nil)),
	)
	payload, diags := lowerPayload(l.ctx, l.types, &l.anchors, content, "/p", "hint")
	assert.Nil(t, payload, "all-nil media map yields no payload")
	assert.Empty(t, diags)
}
