package openapi

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// assertOnlyLoadReportsNull requires a finding of the load phase's validation
// at pointer, where the document breaks its own grammar by writing the null, of
// severity want, and nothing else there: reading it as no schema loses nothing,
// so the lowering adds no report of its own.
func assertOnlyLoadReportsNull(t *testing.T, diags []ir.Diagnostic, pointer jsontext.Pointer, want ir.Severity) {
	t.Helper()
	var at []ir.Diagnostic
	for _, d := range diags {
		if d.Provenance.Pointer == pointer {
			at = append(at, d)
		}
	}
	require.NotEmpty(t, at, "no validation finding at %q: %+v", pointer, diags)
	for _, d := range at {
		assert.Equal(t, want, d.Severity, "%+v", d)
		assert.True(t, strings.HasPrefix(d.Code, diag.Validation+"/"), "only the load phase reports here: %+v", d)
	}
}

// TestCompile_ANullWhereASchemaBelongsIsNoSchema pins GitHub #1045 at the
// compiler's public surface. The parser decodes a null written where a schema
// belongs as the `false` schema, which would close a model, give a component a
// body that matches nothing, and report a false-schema the document never
// wrote. The compile reads it as no schema written and leaves the report to the
// load phase: an error at an entry, which lowers as the empty schema does, in
// components and in operations alike, and a warning at a keyword left out.
func TestCompile_ANullWhereASchemaBelongsIsNoSchema(t *testing.T) {
	t.Parallel()

	t.Run("in component schemas", func(t *testing.T) {
		t.Parallel()
		doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A: {type: object, properties: {p: null}}
    B: {allOf: [null, {type: object}]}
    C: null
    L: {type: array, items: null}
`)
		entries := []jsontext.Pointer{
			"/components/schemas/A/properties/p", "/components/schemas/B/allOf/0", "/components/schemas/C",
		}
		assert.False(t, openapitest.HasDiag(diags, diag.FalseSchema), "no false schema was written: %+v", diags)
		for _, pointer := range entries {
			assertOnlyLoadReportsNull(t, diags, pointer, ir.SeverityError)
		}
		assertOnlyLoadReportsNull(t, diags, "/components/schemas/L/items", ir.SeverityWarning)

		top := ir.TypeRef{Target: "t/prim/any"}
		a, ok := typeByName(doc, "A").(*ir.Model)
		require.True(t, ok)
		require.Len(t, a.Properties, 1)
		assert.Equal(t, top, a.Properties[0].Type, "a property written null is the top type")

		b, ok := typeByName(doc, "B").(*ir.Model)
		require.True(t, ok)
		assert.Empty(t, b.Additional, "a null branch closes nothing")
		assert.NotContains(t, b.Unmodeled, "openapi:allOf/0", "and keeps nothing")

		c, ok := typeByName(doc, "C").(*ir.Scalar)
		require.True(t, ok)
		assert.Equal(t, &top, c.Base, "a component written null is a scalar alias over the top type")

		l, ok := typeByName(doc, "L").(*ir.List)
		require.True(t, ok)
		assert.Equal(t, top, l.Elem, "items written null is an absent items")
	})

	t.Run("in a parameter, a header and a media type", func(t *testing.T) {
		t.Parallel()
		const operation = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      parameters:
        - {name: q, in: query, schema: %s}
      responses:
        "200":
          description: ok
          headers:
            H: {schema: %s}
          content:
            application/json:
              schema: %s
`
		doc, diags := parseFull(t, strings.ReplaceAll(operation, "%s", "null"))
		twin, twinDiags := parseFull(t, strings.ReplaceAll(operation, "%s", "{}"))
		pointers := []jsontext.Pointer{
			"/paths/~1x/get/parameters/0/schema",
			"/paths/~1x/get/responses/200/headers/H/schema",
			"/paths/~1x/get/responses/200/content/application~1json/schema",
		}
		assert.False(t, openapitest.HasDiag(diags, diag.FalseSchema), "%+v", diags)
		for _, pointer := range pointers {
			assertOnlyLoadReportsNull(t, diags, pointer, ir.SeverityError)
		}
		assert.Empty(t, twinDiags, "the twin writes no null, and draws nothing")

		top := ir.TypeRef{Target: "t/prim/any"}
		op := openapitest.FirstOp(t, doc.Services[0])
		require.Len(t, op.Params, 1)
		assert.Equal(t, top, op.Params[0].Type, "a parameter's schema")
		require.Len(t, op.Responses, 1)
		require.Len(t, op.Responses[0].Headers, 1)
		assert.Equal(t, top, op.Responses[0].Headers[0].Type, "a header's schema")
		require.Len(t, op.Responses[0].Payload.Contents, 1)
		assert.Equal(t, top, op.Responses[0].Payload.Contents[0].Type, "a media type's schema")

		assert.Empty(t, cmp.Diff(twin.Services, doc.Services, cmpopts.EquateEmpty()), "as the empty schema lowers")
	})
}
