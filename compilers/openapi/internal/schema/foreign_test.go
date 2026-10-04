package schema_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestRef_InAnotherDocumentsContentNamesThatDocument pins how a schema $ref in
// content another document holds is reported: unresolved, with the reason,
// although the source declares a schema at the pointer it spells (GitHub
// #762). Lowered as any, it names nothing of the source's.
func TestRef_InAnotherDocumentsContentNamesThatDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(`paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
`), 0o600))
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p: {$ref: './ext.yaml#/paths/~1x'}
components:
  schemas:
    Thing: {type: object}
`
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)

	want := ir.Diagnostic{Severity: ir.SeverityError, Code: "openapi/unresolved-ref",
		Message: `unresolved $ref "#/components/schemas/Thing": it names a position in the other document ` +
			"holding it, which is not lowered",
		Provenance: ir.Provenance{Pointer: "/paths/~1p/get/responses/200/content/application~1json/schema"}}
	assert.Contains(t, diags, want)
	require.NotNil(t, doc)
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	require.Len(t, doc.Services[0].Groups[0].Operations, 1)
	op := doc.Services[0].Groups[0].Operations[0]
	require.Len(t, op.Responses, 1)
	assert.Equal(t, ir.TypeID("t/prim/any"), openapitest.BodyTarget(t, op.Responses[0].Payload))
}
