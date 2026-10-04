// This file is a package-level suite, not a per-source-file test: it pins how
// the lowering reads what references bring in from another document, across
// every kind of object the operation walk follows.
package openapi_test // external test package — exercises only the public API

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// foreignContent is a document whose objects each name its own Thing, by a
// schema $ref or, in D, by a mapping. The source declares a Thing too, with
// another shape.
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
    D:
      description: ok
      content:
        application/json:
          schema:
            type: object
            properties: {k: {type: string}}
            discriminator:
              propertyName: k
              mapping: {byName: Thing, byRef: '#/components/schemas/Thing'}
              defaultMapping: '#/components/schemas/Thing'
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

// TestExternalContent_ARefInItNamesItsOwnDocument pins GitHub #762. A pointer
// $ref in an object another document holds names a position in that document,
// which the compile cannot lower (GitHub #74): it is reported unresolved, never
// resolved to what the source declares at the same pointer. Each row reaches
// the object by a kind of entry the operation walk follows. The last comes back
// into the source, whose Thing it does name.
func TestExternalContent_ARefInItNamesItsOwnDocument(t *testing.T) {
	t.Parallel()
	op := func(lines string) string { return "  /op:\n    get:\n" + lines }
	ok200 := "      responses: {\"200\": {description: ok}}\n"
	for _, c := range []struct {
		name, paths string
		foreign     bool
	}{
		{"a path item", "  /p: {$ref: './ext.yaml#/paths/~1x'}\n", true},
		{"a webhook", "  {}\nwebhooks:\n  h: {$ref: './ext.yaml#/paths/~1x'}\n", true},
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

// TestExternalContent_AMappingNamesAsTheSpecificationSays pins the line
// GitHub #762 draws inside another document's content. A mapping value that is
// a pointer names a position in that document, so it is unresolved, and says
// why, as such a $ref does. One that is a component's name is an implicit
// connection, which the specification recommends resolving against the entry
// document, so it names the source's component.
func TestExternalContent_AMappingNamesAsTheSpecificationSays(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(foreignContent), 0o600))
	root := foreignRoot("  /op:\n    get:\n      responses: {\"200\": {$ref: './ext.yaml#/components/responses/D'}}\n")
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	const schema = "/paths/~1op/get/responses/200/content/application~1json/schema"
	model, ok := doc.Types[ir.TypeID("t/anon"+schema)].(*ir.Model)
	require.True(t, ok, "the response's schema is lowered as a model")
	require.NotNil(t, model.Discriminator)
	assert.Equal(t, map[string]ir.TypeID{"byName": "t/openapi/components/schemas/Thing"}, model.Discriminator.Mapping)
	const why = `"#/components/schemas/Thing": it names a position in the other document holding it, which is not lowered`
	for at, message := range map[string]string{
		"/mapping/byRef":  `discriminator mapping "byRef" references unresolved schema ` + why,
		"/defaultMapping": "discriminator defaultMapping references unresolved schema " + why,
	} {
		assert.Contains(t, diags, ir.Diagnostic{Severity: ir.SeverityError, Code: "openapi/unresolved-ref",
			Message: message, Provenance: ir.Provenance{Pointer: jsontext.Pointer(schema + "/discriminator" + at)}})
	}
}

// sourceNamer is a document whose path item holds a mapping to two positions
// in root.yaml: an object, and a schema that is only a $ref.
const sourceNamer = `paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties: {k: {type: string}}
                discriminator:
                  propertyName: k
                  mapping:
                    obj: 'root.yaml#/paths/~1b/get/responses/200/content/application~1json/schema'
                    ref: 'root.yaml#/paths/~1c/get/responses/200/content/application~1json/schema'
`

// TestExternalContent_ASourcePositionItNamesIsTheSources pins where the scope
// GitHub #762 gives another document's content ends. A position in the source
// that such content names is the source's own, so the $refs written there name
// the source's Thing: the object's property, and the $ref the mapping reads
// through (GitHub #758). Read as the other document's, each named a position
// there, and the object's was reported unresolved when the mapping reached it
// before its own path item did.
func TestExternalContent_ASourcePositionItNamesIsTheSources(t *testing.T) {
	t.Parallel()
	const (
		obj = "/paths/~1b/get/responses/200/content/application~1json/schema"
		ref = "/paths/~1c/get/responses/200/content/application~1json/schema"
	)
	items := map[string]string{
		"a": "  /a: {$ref: './ext.yaml#/paths/~1x'}\n",
		"b": "  /b:\n    get:\n      responses:\n        \"200\":\n          description: ok\n" +
			"          content: {application/json: {schema: {type: object, properties: {t: {$ref: '#/components/schemas/Thing'}}}}}\n",
		"c": "  /c:\n    get:\n      responses:\n        \"200\":\n          description: ok\n" +
			"          content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}\n",
	}
	orders := [][]string{{"a", "b", "c"}, {"b", "c", "a"}}
	compiled := make([][]string, 0, len(orders))
	for _, order := range orders {
		var paths strings.Builder
		for _, key := range order {
			paths.WriteString(items[key])
		}
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(sourceNamer), 0o600))
		root := foreignRoot(paths.String())
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
			compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)
		openapitest.RequireNoErrorDiags(t, diags)

		const thing = ir.TypeID("t/openapi/components/schemas/Thing")
		base, ok := doc.Types[ir.TypeID("t/anon/paths/~1a/get/responses/200/content/application~1json/schema")].(*ir.Model)
		require.True(t, ok, "%v: the mapping's schema is lowered as a model", order)
		require.NotNil(t, base.Discriminator)
		assert.Equal(t, map[string]ir.TypeID{"obj": ir.TypeID("t/anon" + obj), "ref": thing},
			base.Discriminator.Mapping, order)
		object, ok := doc.Types[ir.TypeID("t/anon"+obj)].(*ir.Model)
		require.True(t, ok, "%v: the object is lowered as a model", order)
		require.Len(t, object.Properties, 1)
		assert.Equal(t, thing, object.Properties[0].Type.Target, order)
		compiled = append(compiled, compiledThrough(t, dir, root))
	}
	assert.Empty(t, cmp.Diff(compiled[0], compiled[1]), "declaration order decided what the source's positions name")
}

// TestExternalContent_ASourcePathThroughASchemeLikeDirectoryIsAFile pins a
// source compiled through a path holding "://" past a directory named "x:".
// The resolver reads that path as a file, not a URL, so the lowering must too:
// a $ref back into the source from a document beside it names the source's own
// Thing, and the source's path item, reached back, is read as the source's.
// Read as a URL, the source was named only as spelled, so the $ref was
// unresolved, and the path item was read again from disk as another
// document's.
func TestExternalContent_ASourcePathThroughASchemeLikeDirectoryIsAFile(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "x:")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(`paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: 'root.yaml#/components/schemas/Thing'}}}
  /back: {$ref: 'root.yaml#/components/pathItems/Own'}
`), 0o600))
	root := foreignRoot("  /p: {$ref: './ext.yaml#/paths/~1x'}\n  /q: {$ref: './ext.yaml#/paths/~1back'}\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "root.yaml"), []byte(root), 0o600))

	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: dir + "//root.yaml", Data: []byte(root)}},
		compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	require.NotNil(t, doc)
	ops := allOperations(doc)
	require.Len(t, ops, 2)
	for _, op := range ops {
		require.Len(t, op.Responses, 1)
		assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Thing"), openapitest.BodyTarget(t, op.Responses[0].Payload),
			"%s names the source's own Thing", op.ID)
	}
}

// TestExternalContent_ARefNamingTheSourceNamesIt pins the other half of
// GitHub #762's line: a $ref in another document's content is read where that
// document sits, as the resolver reads it. One whose document part names the
// source, however spelled from there, names the source's own Thing. One that
// names a file beside a document in a subdirectory names that file, not the
// source, so it is unresolved as any other cross-document $ref is.
func TestExternalContent_ARefNamingTheSourceNamesIt(t *testing.T) {
	t.Parallel()
	item := func(ref string) string {
		return "      get:\n        operationId: op\n        responses:\n          \"200\":\n" +
			"            description: ok\n" +
			"            content: {application/json: {schema: {$ref: '" + ref + "#/components/schemas/Thing'}}}\n"
	}
	for _, c := range []struct {
		name, file, ref string
		names           bool
	}{
		{"its file name, beside it", "ext.yaml", "root.yaml", true},
		{"through the current directory", "ext.yaml", "./root.yaml", true},
		{"from a subdirectory, through its parent", "sub/ext.yaml", "../root.yaml", true},
		{"its file name, from a subdirectory", "sub/ext.yaml", "root.yaml", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, c.file), []byte("paths:\n  /x:\n"+item(c.ref)), 0o600))
			root := foreignRoot("  /p: {$ref: './" + c.file + "#/paths/~1x'}\n")
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
				compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
			require.NoError(t, err)

			if !c.names {
				want := ir.Diagnostic{Severity: ir.SeverityError, Code: "openapi/unresolved-ref",
					Message:    `unresolved $ref "` + c.ref + `#/components/schemas/Thing"`,
					Provenance: ir.Provenance{Pointer: "/paths/~1p/get/responses/200/content/application~1json/schema"}}
				assert.Contains(t, diags, want, "the $ref names another file, not a position in ext.yaml")
				return
			}
			openapitest.RequireNoErrorDiags(t, diags)
			op, ok := opByName(doc, "op")
			require.True(t, ok)
			require.Len(t, op.Responses, 1)
			assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Thing"), openapitest.BodyTarget(t, op.Responses[0].Payload))
		})
	}
}
