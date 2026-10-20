// This file pins how the binder reads what a reference object resolved to:
// at the node the parser's chain reached, in the source or another document.
package openapi_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// TestReferenceEnds_AnEndingSlashIsReadWhereTheParserResolvedIt pins a
// parameter reference whose pointer ends in '/', into raw YAML of the source
// or of another document. The parser resolves it to the node before the '/',
// and the binder reads its content there: the parameter's schema $ref
// resolves, and so does a schema $ref through the parameter.
func TestReferenceEnds_AnEndingSlashIsReadWhereTheParserResolvedIt(t *testing.T) {
	t.Parallel()
	const param = "{name: q, in: query, schema: {$ref: '#/components/schemas/Int'}}"
	for _, ref := range []string{"#/x-params/q/", "./ext.yaml#/x-params/q/"} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"),
				[]byte("x-params:\n  q: {name: q, in: query, schema: {type: integer}}\n"), 0o600))
			root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {parameters: [{$ref: '` + ref + `'}], responses: {"200": {description: ok}}}}
components:
  schemas:
    Int: {type: integer}
    S: {$ref: '#/paths/~1a/get/parameters/0/schema'}
x-params:
  q: ` + param + "\n"
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
				compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
			require.NoError(t, err)
			assert.Empty(t, diags)
			require.NotNil(t, doc)
			op := doc.Services[0].Groups[0].Operations[0]
			require.Len(t, op.Params, 1)
			s, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Scalar)
			require.True(t, ok)
			if ref == "#/x-params/q/" {
				assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Int"), op.Params[0].Type.Target)
				return
			}
			assert.Equal(t, ir.TypeID("t/prim/integer"), op.Params[0].Type.Target)
			assert.Equal(t, ir.TypeID("t/anon/paths/~1a/get/parameters/0/schema"), s.Base.Target)
		})
	}
}

// TestReferenceEnds_AnEndInTheSourceIsReadThereUnderSelf pins a response
// reference in a document declaring $self, which the parser resolves against
// the $self URL and names its end by that (GitHub #863): it ends in the
// source, so a schema $ref through it resolves there.
func TestReferenceEnds_AnEndInTheSourceIsReadThereUnderSelf(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.2.0
$self: https://example.com/api/openapi.yaml
info: {title: T, version: "1"}
paths:
  /a: {get: {responses: {"404": {$ref: '#/components/responses/NotFound'}}}}
components:
  responses:
    NotFound: {description: nf, content: {application/json: {schema: {$ref: '#/components/schemas/X'}}}}
  schemas:
    X: {type: integer}
    S: {$ref: '#/paths/~1a/get/responses/404/content/application~1json/schema'}
`
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
		compilers.Options{})
	require.NoError(t, err)
	assert.Empty(t, diags)
	require.NotNil(t, doc)
	s, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/anon/paths/~1a/get/responses/404/content/application~1json/schema"), s.Base.Target)
}
