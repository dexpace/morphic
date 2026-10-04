package schema_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
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

// TestDynamicRef_InAnotherDocumentsContentIsNotExpanded pins a $dynamicRef in
// content another document holds: its fragment names that document's
// $dynamicAnchor, which this compile does not index, so it is kept verbatim
// rather than expanded to the source's anchor of the same name (GitHub #762).
func TestDynamicRef_InAnotherDocumentsContentIsNotExpanded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(`paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$dynamicRef: '#node'}}}
components:
  schemas:
    Other: {$dynamicAnchor: node, type: object}
`), 0o600))
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p: {$ref: './ext.yaml#/paths/~1x'}
components:
  schemas:
    Node: {$dynamicAnchor: node, type: object}
`
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)

	want := ir.Diagnostic{Severity: ir.SeverityInfo, Code: "openapi/degraded-construct",
		Message: "$dynamicRef was not expanded because it is written in another document, whose " +
			"$dynamicAnchor it names and which is not lowered; it is kept verbatim under Unmodeled",
		Provenance: ir.Provenance{Pointer: "/paths/~1p/get/responses/200/content/application~1json/schema/$dynamicRef"}}
	assert.Contains(t, diags, want)
	require.NotNil(t, doc)
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	require.Len(t, doc.Services[0].Groups[0].Operations, 1)
	op := doc.Services[0].Groups[0].Operations[0]
	require.Len(t, op.Responses, 1)
	held, ok := doc.Types[openapitest.BodyTarget(t, op.Responses[0].Payload)].(*ir.Scalar)
	require.True(t, ok, "the position holds a node of its own for the reference it keeps")
	require.NotNil(t, held.Base)
	assert.Equal(t, ir.TypeID("t/prim/any"), held.Base.Target,
		"the source's anchor of the same name is not what the reference names")
	assert.Equal(t, ir.ReasonDegradedLowering, held.Unmodeled["openapi:$dynamicRef"].Reason)
}

// TestUnresolvedRef_InAnotherDocumentsContentSaysWhyAtEachPosition pins the
// reason GitHub #762 gives a $ref in content another document holds at the two
// positions that report an unresolved one in words of their own: an allOf
// branch, and a union branch beside structural keywords. Without it the first
// read as though it named nothing, and the second said nothing was declared
// there, though both documents declare Thing. A $ref naming a third document
// keeps the plain report, as it does in the source.
func TestUnresolvedRef_InAnotherDocumentsContentSaysWhyAtEachPosition(t *testing.T) {
	t.Parallel()
	const (
		at  = "/paths/~1p/get/responses/200/content/application~1json/schema"
		why = ": it names a position in the other document holding it, which is not lowered"
	)
	for _, tc := range []struct {
		name, schema, at, message string
	}{
		{
			name:    "an allOf branch",
			schema:  "{allOf: [{$ref: '#/components/schemas/Thing'}, {type: object}]}",
			at:      at + "/allOf/0",
			message: `unresolved allOf $ref "#/components/schemas/Thing"` + why,
		},
		{
			name:    "a union branch beside structural keywords",
			schema:  "{type: object, properties: {x: {type: string}}, oneOf: [{$ref: '#/components/schemas/Thing'}]}",
			at:      at + "/oneOf/0",
			message: `union branch $ref "#/components/schemas/Thing" is unresolved` + why + "; the branch is kept verbatim",
		},
		{
			name:    "an allOf branch naming a third document",
			schema:  "{allOf: [{$ref: './third.yaml#/Thing'}, {type: object}]}",
			at:      at + "/allOf/0",
			message: `unresolved allOf $ref "./third.yaml#/Thing"`,
		},
		{
			name:    "a union branch naming a third document",
			schema:  "{type: object, properties: {x: {type: string}}, oneOf: [{$ref: './third.yaml#/Thing'}]}",
			at:      at + "/oneOf/0",
			message: `union branch $ref "./third.yaml#/Thing" resolves to nothing this document declares; the branch is kept verbatim`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(`paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: `+tc.schema+`}}
components:
  schemas:
    Thing: {type: object}
`), 0o600))
			const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p: {$ref: './ext.yaml#/paths/~1x'}
components:
  schemas:
    Thing: {type: object}
`
			_, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
				compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
			require.NoError(t, err)

			var reported []ir.Diagnostic
			for _, d := range diags {
				if d.Code == "openapi/unresolved-ref" {
					reported = append(reported, d)
				}
			}
			want := []ir.Diagnostic{{Severity: ir.SeverityError, Code: "openapi/unresolved-ref",
				Message: tc.message, Provenance: ir.Provenance{Pointer: jsontext.Pointer(tc.at)}}}
			assert.Empty(t, cmp.Diff(want, reported))
		})
	}
}
