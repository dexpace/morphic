package operation

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/ir"
)

func TestFillParamSchema_EmptyEitherNoOp(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	param := &ir.Parameter{}
	diags := fillParamSchema(l.ctx, l.types, param, emptyEitherSchema(), "/p")
	assert.Nil(t, param.Constraints)
	assert.Nil(t, param.Default)
	assert.Empty(t, diags)
}

// TestPreserveParamXML_ModelSetWithoutRawSourceRecordsNothing pins the same guard
// on the parameter carrier. The hint is kept verbatim, so it is read off the
// source node, and a schema whose model reports xml with no bytes behind it must
// record nothing rather than announce a preservation it did not make.
func TestPreserveParamXML_ModelSetWithoutRawSourceRecordsNothing(t *testing.T) {
	t.Parallel()
	l, _ := loweredFor(t, componentSpec("    A: {type: string}\n"))
	s := &oas3.Schema{XML: &oas3.XML{}}
	require.NotNil(t, s.GetXML(), "the model reports the hint as set")
	require.Nil(t, annotation.RawPropertyNode(s, "xml"), "and no raw node backs it")

	var param ir.Parameter
	diags := preserveParamXML(l.ctx, &param, s, "/paths/~1x/get/parameters/0/schema")

	assert.Nil(t, param.Unmodeled, "no entry is recorded when there are no bytes to record")
	assert.Empty(t, diags, "and nothing is announced, so the two channels agree")
}
