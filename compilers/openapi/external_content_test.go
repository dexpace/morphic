// This file is a package-level suite, not a per-source-file test: it pins how
// the lowering reads what references bring in from another document, across
// every kind of object the operation walk follows.
package openapi_test // external test package — exercises only the public API

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// foreignContent is a document whose objects each hold a schema $ref to its own
// Thing, which the source declares too, with another shape.
const foreignContent = `openapi: 3.1.0
info: {title: E, version: "1"}
paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
  /back: {$ref: 'root.yaml#/components/pathItems/Own'}
components:
  schemas:
    Thing: {type: object, properties: {theirs: {type: string}}}
  responses:
    R:
      description: ok
      content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
  parameters:
    P: {name: p, in: query, schema: {$ref: '#/components/schemas/Thing'}}
  headers:
    H: {schema: {$ref: '#/components/schemas/Thing'}}
  requestBodies:
    B: {content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}}
  callbacks:
    C:
      '{$request.body#/u}':
        post:
          responses:
            "200":
              description: ok
              content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
`

// foreignRoot is a source whose get operation, at /op, is completed by the
// lines given, and which declares a Thing of its own and a path item Own.
func foreignRoot(paths string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
` + paths + `components:
  schemas:
    Thing: {type: object, properties: {ours: {type: integer}}}
  pathItems:
    Own:
      get:
        responses:
          "200":
            description: ok
            content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
`
}

// TestExternalContent_ARefInItNamesItsOwnDocument pins GitHub #762. A $ref in
// an object another document holds names a position in that document, which the
// compile cannot lower (GitHub #74): it is reported unresolved, never resolved
// to what the source declares at the same pointer. Each row reaches the object
// by a kind of entry the operation walk follows. The last comes back into the
// source, whose Thing it does name.
func TestExternalContent_ARefInItNamesItsOwnDocument(t *testing.T) {
	t.Parallel()
	op := func(lines string) string { return "  /op:\n    get:\n" + lines }
	ok200 := "      responses: {\"200\": {description: ok}}\n"
	for _, c := range []struct {
		name, paths string
		foreign     bool
	}{
		{"a path item", "  /p: {$ref: './ext.yaml#/paths/~1x'}\n", true},
		{"a response", op("      responses: {\"200\": {$ref: './ext.yaml#/components/responses/R'}}\n"), true},
		{"a default response", op("      responses: {default: {$ref: './ext.yaml#/components/responses/R'}}\n"), true},
		{"a parameter", op("      parameters: [{$ref: './ext.yaml#/components/parameters/P'}]\n" + ok200), true},
		{"a header", op("      responses:\n        \"200\":\n          description: ok\n" +
			"          headers: {X-H: {$ref: './ext.yaml#/components/headers/H'}}\n"), true},
		{"a request body", op("      requestBody: {$ref: './ext.yaml#/components/requestBodies/B'}\n" + ok200), true},
		{"a callback", op("      callbacks: {cb: {$ref: './ext.yaml#/components/callbacks/C'}}\n" + ok200), true},
		{"a callback's path item", op("      callbacks: {cb: {'{$request.body#/v}': {$ref: './ext.yaml#/paths/~1x'}}}\n" + ok200), true},
		{"a path item back in the source", "  /p: {$ref: './ext.yaml#/paths/~1back'}\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(foreignContent), 0o600))
			root := foreignRoot(c.paths)
			_, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
				compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
			require.NoError(t, err)

			var foreign []ir.Diagnostic
			for _, d := range diags {
				if strings.Contains(d.Message, "the other document holding it") {
					foreign = append(foreign, d)
				}
			}
			if !c.foreign {
				assert.Empty(t, foreign, "a $ref back in the source names the source's own")
				return
			}
			require.Len(t, foreign, 1, "%+v", diags)
			assert.Equal(t, ir.SeverityError, foreign[0].Severity)
			assert.Contains(t, foreign[0].Message, `"#/components/schemas/Thing"`)
		})
	}
}
