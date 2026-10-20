// This file is a package-level suite, not a per-source-file test: it pins how
// the lowering reads what references bring in from another document, across
// every kind of object the operation walk follows.
package openapi_test // external test package — exercises only the public API

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// passedContent is a document whose /a responds with an object naming its own
// Pet, and a Toy it does not declare, and with a schema that is only a $ref to
// its Pet.
const passedContent = `openapi: 3.1.0
info: {title: E, version: "1"}
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  pet: {$ref: '#/components/schemas/Pet'}
                  toy: {$ref: '#/components/schemas/Toy'}
        "201":
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Pet'}}}
components:
  schemas:
    Pet: {type: object, properties: {theirs: {type: string}}}
`

// passingRoot is a source whose /a is passedContent's, completed by the lines
// given. Its /0 names /a first, so the resolver has followed /a's $ref before
// any pointer passes it, in either order. It declares a Pet of its own.
func passingRoot(paths string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /0: {$ref: '#/paths/~1a'}
` + paths + `components:
  schemas:
    Pet: {type: object, properties: {ours: {type: integer}}}
`
}

// TestExternalContent_APointerPastItsRefNamesItsContent pins GitHub #762 for a
// pointer in the source that passes /a's $ref into ext.yaml, which the resolver
// reports resolved against the source. What it names is ext.yaml's content, as
// /a's own lowering reads it, so each $ref written there is unresolved, with
// the reason, in both orders: Pet is ext.yaml's, which is not lowered, and Toy
// names nothing there. Read as the source's, the entry's and the hoist's rows
// followed declaration order, and the mapping named the source's Pet. Each row
// is declared first in the order that went wrong.
func TestExternalContent_APointerPastItsRefNamesItsContent(t *testing.T) {
	t.Parallel()
	const (
		object = "/paths/~1a/get/responses/200/content/application~1json/schema"
		alias  = "/paths/~1a/get/responses/201/content/application~1json/schema"
		pet    = `unresolved $ref "#/components/schemas/Pet": it names a position in the other document holding it, ` +
			"which is not lowered"
		toy = `unresolved $ref "#/components/schemas/Toy": in <dir>/ext.yaml, it names nothing in the document it ` +
			"points into"
	)
	copies := []string{"/paths/~10", "/paths/~1a"}
	want := make([]string, 0, 3*len(copies))
	for _, at := range copies {
		want = append(want,
			"error openapi/unresolved-ref "+at+"/get/responses/200/content/application~1json/schema/properties/pet "+pet,
			"error openapi/unresolved-ref "+at+"/get/responses/200/content/application~1json/schema/properties/toy "+toy,
			"error openapi/unresolved-ref "+at+"/get/responses/201/content/application~1json/schema "+pet)
	}
	slices.Sort(want)
	objectReadsTheirs := func(t *testing.T, doc *ir.Document) {
		t.Helper()
		model, ok := doc.Types[ir.TypeID("t/anon"+object)].(*ir.Model)
		require.True(t, ok, "the object is lowered as a model")
		for _, name := range []string{"pet", "toy"} {
			p, ok := propByWire(model, name)
			require.True(t, ok, name)
			assert.Equal(t, ir.TypeID("t/prim/any"), p.Type.Target, name)
		}
	}
	for _, c := range []struct {
		name, item string
		check      func(t *testing.T, doc *ir.Document)
	}{
		{"an entry", "  /b: {get: {responses: {\"200\": {$ref: '#/paths/~1a/get/responses/200'}}}}\n", objectReadsTheirs},
		{"a schema $ref", "  /c: {get: {responses: {\"200\": {description: ok, content: " +
			"{application/json: {schema: {$ref: '#" + object + "'}}}}}}}\n", objectReadsTheirs},
		{"a mapping's read-through", "  /d: {get: {responses: {\"200\": {description: ok, content: {application/json: " +
			"{schema: {type: object, properties: {k: {type: string}}, discriminator: {propertyName: k, " +
			"mapping: {x: '#" + alias + "'}}}}}}}}}\n", func(t *testing.T, doc *ir.Document) {
			t.Helper()
			model, ok := doc.Types[ir.TypeID("t/anon/paths/~1d/get/responses/200/content/application~1json/schema")].(*ir.Model)
			require.True(t, ok, "the mapping's schema is lowered as a model")
			require.NotNil(t, model.Discriminator)
			assert.Equal(t, map[string]ir.TypeID{"x": ir.TypeID("t/anon" + alias)}, model.Discriminator.Mapping,
				"its $ref names ext.yaml's Pet, so the mapping names the position, not the source's Pet")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			const a = "  /a: {$ref: './ext.yaml#/paths/~1a'}\n"
			compiled := make([][]string, 0, 2)
			// One directory for both orders, so that the reports naming ext.yaml
			// compare alike.
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(passedContent), 0o600))
			for _, paths := range []string{c.item + a, a + c.item} {
				root := passingRoot(paths)
				doc, diags, err := openapi.New().Compile(t.Context(),
					[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
					compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
				require.NoError(t, err)
				require.NotNil(t, doc)
				got := make([]string, 0, len(diags))
				for _, d := range diags {
					got = append(got, fmt.Sprintf("%s %s %s %s", d.Severity, d.Code, d.Provenance.Pointer,
						strings.ReplaceAll(d.Message, dir, "<dir>")))
				}
				slices.Sort(got)
				assert.Equal(t, want, got, root)
				c.check(t, doc)
				compiled = append(compiled, compiledThrough(t, dir, root))
			}
			assert.Empty(t, cmp.Diff(compiled[0], compiled[1]), "declaration order decided what the position names")
		})
	}
}

// TestExternalContent_ASchemaRefPastARefNamesItsContent pins a schema $ref in
// the source whose pointer passes the $ref of the response R into ext.yaml,
// which the resolver reports resolved against the source. What it names is
// ext.yaml's schema (GitHub #762), so the $ref written there names ext.yaml's
// Pet, which ext.yaml does not declare, and is unresolved, naming that
// document, as where /x's own response lowers it.
// Read as the source's, it named the source's Pet without a word, and no
// declaration lowers the position to correct it.
func TestExternalContent_ASchemaRefPastARefNamesItsContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(`components:
  responses:
    R:
      description: ok
      content: {application/json: {schema: {type: object, properties: {pet: {$ref: '#/components/schemas/Pet'}}}}}
`), 0o600))
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x: {get: {responses: {"200": {$ref: '#/components/responses/R'}}}}
components:
  schemas:
    Pet: {type: object, properties: {ours: {type: integer}}}
    UsesR: {type: object, properties: {body: {$ref: '#/components/responses/R/content/application~1json/schema'}}}
  responses:
    R: {$ref: './ext.yaml#/components/responses/R'}
`
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)

	const named = "/components/responses/R/content/application~1json/schema"
	unresolved := func(at string) ir.Diagnostic {
		return ir.Diagnostic{Severity: ir.SeverityError, Code: "openapi/unresolved-ref",
			Message: `unresolved $ref "#/components/schemas/Pet": in ` + filepath.Join(dir, "ext.yaml") +
				", it names nothing in the document it points into",
			Provenance: ir.Provenance{Pointer: jsontext.Pointer(at + "/properties/pet")}}
	}
	assert.ElementsMatch(t, []ir.Diagnostic{
		unresolved(named),
		unresolved("/paths/~1x/get/responses/200/content/application~1json/schema"),
	}, diags)
	require.NotNil(t, doc)
	usesR, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/UsesR")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, usesR.Properties, 1)
	assert.Equal(t, ir.TypeID("t/anon"+named), usesR.Properties[0].Type.Target)
	body, ok := doc.Types[ir.TypeID("t/anon"+named)].(*ir.Model)
	require.True(t, ok, "the position is hoisted as a model")
	require.Len(t, body.Properties, 1)
	assert.Equal(t, ir.TypeID("t/prim/any"), body.Properties[0].Type.Target, "not the source's Pet")
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
// source, so it is unresolved as any other $ref to a missing file is, naming
// the document it is written in.
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
					Message: `unresolved $ref "` + c.ref + `#/components/schemas/Thing": in ` + filepath.Join(dir, c.file) +
						", the document it names could not be read: open " + filepath.Join(dir, "sub", c.ref) +
						": no such file or directory",
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

// TestExternalContent_ARefInTheSourceNamingItNamesIt pins GitHub #576: a $ref
// in the source's own content names the source by the rule a $ref in another
// document's content does, its document part joined onto the source's
// directory (see TestExternalContent_ARefNamingTheSourceNamesIt). Spelled
// through the current directory, its parent, or a directory and back, or by
// its absolute path however unclean, it names the source's own Thing, whether
// or not external references are allowed, as it names no other document. One
// in a directory of its own names a file there, and so does a file beside it.
func TestExternalContent_ARefInTheSourceNamingItNamesIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const pointer = "/paths/~1op/get/responses/200/content/application~1json/schema"
	for _, c := range []struct {
		name, ref string
		names     bool
	}{
		{"its file name", "root.yaml", true},
		{"through the current directory", "./root.yaml", true},
		{"through its parent", "../" + filepath.Base(dir) + "/root.yaml", true},
		{"through a directory and back", "sub/../root.yaml", true},
		{"its absolute path", filepath.Join(dir, "root.yaml"), true},
		{"its absolute path, not cleaned", dir + "/./root.yaml", true},
		{"in a directory of its own", "sub/root.yaml", false},
		{"another file beside it", "other.yaml", false},
	} {
		for _, allow := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/allow-external-refs=%t", c.name, allow), func(t *testing.T) {
				t.Parallel()
				root := foreignRoot("  /op:\n    get:\n      operationId: op\n      responses:\n        \"200\":\n" +
					"          description: ok\n" +
					"          content: {application/json: {schema: {$ref: '" + c.ref + "#/components/schemas/Thing'}}}\n")
				doc, diags, err := openapi.New().Compile(t.Context(),
					[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
					compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: allow}})
				require.NoError(t, err)

				if !c.names {
					assert.True(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
						return d.Code == "openapi/unresolved-ref" && d.Provenance.Pointer == pointer
					}), "the $ref names a file that is not the source: %v", diags)
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
}

// TestExternalContent_AnAliasedRefNamingTheSourceKeepsWhatItNames pins the cost
// of reading such a $ref otherwise. A path item reached by a $ref written with a
// YAML alias, for its value or for its key, through a spelling of the source that
// no other key holds, was built as a copy whose own $refs nothing resolved, and
// the lowering dropped the response it names. The same $ref written as a plain
// string kept it.
func TestExternalContent_AnAliasedRefNamingTheSourceKeepsWhatItNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	spelled := "'" + dir + "/./root.yaml#/components/pathItems/I'"
	for _, c := range []struct{ name, ref, anchor string }{
		{"a plain string", "{$ref: " + spelled + "}", ""},
		{"an alias as the value", "{$ref: *r}", "x-r: &r " + spelled + "\n"},
		{"an alias as the key", "{*k : " + spelled + "}", "x-k: &k '$ref'\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" + c.anchor + "paths:\n  /a: " + c.ref + "\n" +
				"components:\n  pathItems:\n    I:\n      get:\n        operationId: i\n" +
				"        responses: {\"200\": {$ref: '#/components/responses/R'}}\n" +
				"  responses:\n    R: {description: ok}\n"
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
				compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
			require.NoError(t, err)

			openapitest.RequireNoErrorDiags(t, diags)
			op, ok := opByName(doc, "i")
			require.True(t, ok)
			assert.Len(t, op.Responses, 1, "the response the path item's $ref names")
		})
	}
}

// TestExternalContent_ASourcePositionItNamesByFileResolvesInEitherOrder pins
// two positions in the source that another document's content names by the
// source's file name: raw YAML a discriminator mapping value names (GitHub
// #765), and a response schema under /paths a schema $ref names (GitHub #766).
// Each resolved only where the source's own path item lowered it first, and
// was reported unresolved otherwise; bound by position, each resolves in
// either declaration order, to the same document.
func TestExternalContent_ASourcePositionItNamesByFileResolvesInEitherOrder(t *testing.T) {
	t.Parallel()
	const schema = "/get/responses/200/content/application~1json/schema"
	tests := []struct {
		name, ext, tail string
		paths           []string
		check           func(t *testing.T, doc *ir.Document)
	}{
		{"a mapping value naming raw YAML", `paths:
  /b:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties: {kind: {type: string}}
                discriminator: {propertyName: kind, mapping: {cat: 'root.yaml#/x-lib/Cat'}}
`, "x-lib:\n  Cat: {type: object, properties: {meow: {type: string}}}\n", []string{
			"  /b: {$ref: './ext.yaml#/paths/~1b'}\n",
			"  /a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n" +
				"          content: {application/json: {schema: {$ref: '#/x-lib/Cat'}}}\n",
		}, func(t *testing.T, doc *ir.Document) {
			model, ok := doc.Types[ir.TypeID("t/anon/paths/~1b"+schema)].(*ir.Model)
			require.True(t, ok, "the mapping's schema is lowered as a model")
			require.NotNil(t, model.Discriminator)
			assert.Equal(t, map[string]ir.TypeID{"cat": "t/anon/x-lib/Cat"}, model.Discriminator.Mapping)
		}},
		{"a schema $ref naming a response schema", `openapi: 3.1.0
info: {title: E, version: "1"}
paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: 'root.yaml#/paths/~1b` + schema + `'}}}
`, "components:\n  schemas:\n    Thing: {type: object}\n", []string{
			"  /a: {$ref: './ext.yaml#/paths/~1x'}\n",
			"  /b:\n    get:\n      responses:\n        \"200\":\n          description: ok\n" +
				"          content: {application/json: {schema: {type: object, properties: {t: {$ref: '#/components/schemas/Thing'}}}}}\n",
		}, func(t *testing.T, doc *ir.Document) {
			got := operationAt(t, doc, "GET", "/a").Responses[0].Payload.Contents[0].Type.Target
			assert.Equal(t, ir.TypeID("t/anon/paths/~1b"+schema), got, "/a responds with /b's schema")
		}},
	}
	for _, tc := range tests {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(tc.ext), 0o600))
		compiled := make([][]string, 0, 2)
		for _, reversed := range []bool{false, true} {
			paths := slices.Clone(tc.paths)
			if reversed {
				slices.Reverse(paths)
			}
			root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" + strings.Join(paths, "") + tc.tail
			path := filepath.Join(dir, "root.yaml")
			require.NoError(t, os.WriteFile(path, []byte(root), 0o600))
			doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: []byte(root)}},
				compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
			require.NoError(t, err, tc.name)
			require.NotNil(t, doc, tc.name)
			for _, d := range diags {
				assert.NotEqual(t, "openapi/unresolved-ref", d.Code, "%s, reversed=%t: %+v", tc.name, reversed, d)
			}
			tc.check(t, doc)
			compiled = append(compiled, compiledThrough(t, dir, root))
		}
		assert.Empty(t, cmp.Diff(compiled[0], compiled[1]), "%s: declaration order decided what it names", tc.name)
	}
}

// TestExternalContent_ARefWrittenInItLendsItsUseSiteNothing pins that a schema
// $ref written in another document's content lends the use site holding it
// nothing, as the parser's resolver resolved no $ref there (GitHub #751): no
// default or description to a parameter or a header, no parts to a form body's
// encoding, no file type to a binary body, and no discriminator to a schema
// composing through it. A $ref the source writes still lends its target's
// default (GitHub #1023).
func TestExternalContent_ARefWrittenInItLendsItsUseSiteNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.yaml"), []byte(`RB:
  content:
    multipart/form-data:
      schema: {$ref: '#/Form'}
      encoding: {file: {contentType: image/png}}
RBBin:
  content: {application/x-custom: {schema: {$ref: '#/Bin'}}}
Param: {name: lent, in: query, schema: {$ref: '#/Fmt'}}
Resp:
  description: ok
  headers: {X-Lent: {schema: {$ref: '#/Fmt'}}}
Kid: {allOf: [{$ref: '#/Pet'}]}
Form: {type: object, properties: {file: {type: string, format: binary}}}
Bin: {type: string, format: binary}
Fmt: {type: string, default: json, description: fmt}
Pet: {type: object, discriminator: {propertyName: k}, properties: {k: {type: string}}}
`), 0o600))
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /form:
    post:
      parameters:
        - {$ref: './lib.yaml#/Param'}
        - {name: own, in: query, schema: {$ref: './lib.yaml#/Fmt'}}
      requestBody: {$ref: './lib.yaml#/RB'}
      responses: {"200": {$ref: './lib.yaml#/Resp'}}
  /file:
    post:
      requestBody: {$ref: './lib.yaml#/RBBin'}
      responses: {"204": {description: ok}}
components:
  schemas:
    Local: {allOf: [{$ref: './lib.yaml#/Kid'}], properties: {z: {type: string}}}
`
	doc, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	ops := map[ir.OpID]ir.Operation{}
	require.False(t, ir.ForEachOperation(doc, func(op ir.Operation) { ops[op.ID] = op }))
	form, file := ops["op/openapi/paths/~1form/post"], ops["op/openapi/paths/~1file/post"]

	params := map[string]ir.Parameter{}
	for _, p := range form.Params {
		params[p.Name.Source] = p
	}
	assert.Nil(t, params["lent"].Default, "a parameter's default")
	assert.Empty(t, params["lent"].Docs.Description, "a parameter's description")
	require.NotNil(t, params["own"].Default)
	assert.Equal(t, "json", params["own"].Default.Str, "the source's own $ref")
	require.Len(t, form.Responses, 1)
	require.Len(t, form.Responses[0].Headers, 1)
	assert.Nil(t, form.Responses[0].Headers[0].Default, "a header's default")
	assert.Empty(t, form.Responses[0].Headers[0].Docs.Description, "a header's description")
	require.NotNil(t, form.Request)
	require.Len(t, form.Request.Contents, 1)
	assert.Empty(t, form.Request.Contents[0].Encoding, "a form body's parts")
	require.NotNil(t, file.Request)
	require.Len(t, file.Request.Contents, 1)
	assert.Equal(t, ir.TypeID("t/prim/any"), file.Request.Contents[0].Type.Target, "a binary body's type")
	assert.Nil(t, file.Request.Contents[0].File)
	local, ok := doc.Types["t/openapi/components/schemas/Local"].(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, local.DiscriminatorValue, "a discriminator through a composed schema")
}
