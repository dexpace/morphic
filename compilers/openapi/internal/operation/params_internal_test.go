package operation

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

func TestFillParamSchema_EmptyEitherNoOp(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	param := &ir.Parameter{}
	diags := fillParamSchema(l.ctx, l.types, param, openapitest.EmptyEitherSchema(), "/p")
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
	l, _ := loweredFor(t, openapitest.ComponentSpec("    A: {type: string}\n"))
	s := &oas3.Schema{XML: &oas3.XML{}}
	require.NotNil(t, s.GetXML(), "the model reports the hint as set")
	require.Nil(t, annotation.RawPropertyNode(s, "xml"), "and no raw node backs it")

	var param ir.Parameter
	diags := preserveParamXML(l.ctx, &param, s, "/paths/~1x/get/parameters/0/schema")

	assert.Nil(t, param.Unmodeled, "no entry is recorded when there are no bytes to record")
	assert.Empty(t, diags, "and nothing is announced, so the two channels agree")
}

func TestParameters_PathItemMergeOverride(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /users/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}, description: path-level}
      - {name: trace, in: header, schema: {type: string}}
    get:
      operationId: g
      parameters:
        - {name: id, in: path, required: true, schema: {type: integer}, description: op-level}
      responses: {"200": {description: ok}}
`)
	loadedDoc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	var pi *soa.PathItem
	for _, rp := range loadedDoc.Doc.GetPaths().All() {
		pi = resolve.Object[soa.PathItem](rp)
	}
	require.NotNil(t, pi)
	op := pi.Get()
	require.NotNil(t, op)
	pathPtr := ids.Ptr("paths", "/users/{id}")
	opPtr := pathPtr + ids.Ptr("get")
	merged := mergeParameters(appendSourced(nil, pi.GetParameters(), pathPtr, nil, nil), op.GetParameters(), opPtr)
	require.Len(t, merged, 2, "shared (name,in) collapses to one; op wins")
	assert.Same(t, op.GetParameters()[0], merged[0].ref, "operation parameter overrides the path-item one")
	assert.Equal(t, jsontext.Pointer("/paths/~1users~1{id}/get/parameters/0"), merged[0].pointer,
		"the op-level parameter keeps its own declaration pointer")

	byName := map[string]sourcedParam{}
	for _, sp := range merged {
		byName[resolve.Object[soa.Parameter](sp.ref).GetName()] = sp
	}
	_, hasID := byName["id"]
	_, hasTrace := byName["trace"]
	assert.True(t, hasID)
	assert.True(t, hasTrace)
	assert.Equal(t, jsontext.Pointer("/paths/~1users~1{id}/parameters/1"), byName["trace"].pointer,
		"the unshadowed path-level parameter keeps its own declaration pointer, at its own path-item index")
}

// TestParamKey_NilParameterIsNotAKey pins the guard on the merge key. A nil
// entry in a parameter list has no name and no location, so it cannot key
// anything — and answering with the zero key would silently merge every such
// entry onto one another.
func TestParamKey_NilParameterIsNotAKey(t *testing.T) {
	t.Parallel()
	_, ok := paramKey(nil)
	assert.False(t, ok)
}
