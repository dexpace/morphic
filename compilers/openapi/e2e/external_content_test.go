// This file is a package-level suite, not a per-source-file test: it pins how
// the lowering reads what references bring in from another document, across
// every kind of object the operation walk follows.
package e2e_test // external test package — exercises only the public API

import (
	"fmt"
	"maps"
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
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
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
// and lowers to the type declared there, in that document's namespace, never
// to what the source declares at the same pointer. Each row reaches the object
// by a kind of entry the operation walk follows. The last comes back into the
// source, whose Thing it names.
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
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
				compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
			require.NoError(t, err)
			require.NotNil(t, doc)
			openapitest.RequireNoErrorDiags(t, diags)

			// ext.yaml's Thing is lowered only where a $ref reaches it, so its
			// presence says the row's $ref named it rather than the source's.
			theirs, lowered := doc.Types[ids.In("ext.yaml").NamedType("/components/schemas/Thing")].(*ir.Model)
			assert.Equal(t, c.foreign, lowered, "%+v", slices.Collect(maps.Keys(doc.Types)))
			if lowered {
				require.Len(t, theirs.Properties, 1)
				assert.Equal(t, "theirs", theirs.Properties[0].WireName)
				assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/schemas/Thing"}, theirs.Provenance)
			}
		})
	}
}

// TestExternalContent_AMappingNamesAsTheSpecificationSays pins the line
// GitHub #762 draws inside another document's content. A mapping value that is
// a pointer names a position in that document, as such a $ref does, and so does
// a defaultMapping. One that is a component's name is an implicit connection,
// which the specification recommends resolving against the entry document, so
// it names the source's component.
func TestExternalContent_AMappingNamesAsTheSpecificationSays(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(foreignContent), 0o600))
	root := foreignRoot("  /op:\n    get:\n      responses: {\"200\": {$ref: './ext.yaml#/components/responses/D'}}\n")
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	assert.False(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool { return d.Code == "openapi/unresolved-ref" }),
		"every mapping value resolves: %+v", diags)
	ext := ids.In("ext.yaml")
	model, ok := doc.Types[ext.AnonType("/components/responses/D/content/application~1json/schema")].(*ir.Model)
	require.True(t, ok, "the response's schema is lowered as a model, where ext.yaml declares it")
	require.NotNil(t, model.Discriminator)
	theirs := ext.NamedType("/components/schemas/Thing")
	assert.Equal(t, map[string]ir.TypeID{"byName": "t/openapi/components/schemas/Thing", "byRef": theirs},
		model.Discriminator.Mapping)
	assert.Equal(t, theirs, model.Discriminator.Default)
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
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)
		openapitest.RequireNoErrorDiags(t, diags)

		const thing = ir.TypeID("t/openapi/components/schemas/Thing")
		mapping := ids.In("ext.yaml").AnonType("/paths/~1x/get/responses/200/content/application~1json/schema")
		base, ok := doc.Types[mapping].(*ir.Model)
		require.True(t, ok, "%v: the mapping's schema is lowered as a model, where ext.yaml declares it", order)
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
// reports resolved against the source. What it names is ext.yaml's content,
// lowered once where ext.yaml declares it, as /a's own lowering reads it, in
// both orders: Pet is ext.yaml's, and Toy names nothing there, reported once.
// Read as the source's, the entry's and the hoist's rows followed declaration
// order, and the mapping named the source's Pet. Each row is declared first in
// the order that went wrong.
func TestExternalContent_APointerPastItsRefNamesItsContent(t *testing.T) {
	t.Parallel()
	const (
		object = "/paths/~1a/get/responses/200/content/application~1json/schema"
		alias  = "/paths/~1a/get/responses/201/content/application~1json/schema"
	)
	want := []string{"error openapi/unresolved-ref 1" + object + `/properties/toy unresolved $ref ` +
		`"#/components/schemas/Toy": it names nothing in the document it points into`}
	ext := ids.In("ext.yaml")
	objectReadsTheirs := func(t *testing.T, doc *ir.Document) {
		t.Helper()
		model, ok := doc.Types[ext.AnonType(object)].(*ir.Model)
		require.True(t, ok, "the object is lowered as a model, where ext.yaml declares it")
		for name, target := range map[string]ir.TypeID{"pet": ext.NamedType("/components/schemas/Pet"), "toy": "t/prim/any"} {
			p, ok := propByWire(model, name)
			require.True(t, ok, name)
			assert.Equal(t, target, p.Type.Target, name)
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
			assert.Equal(t, map[string]ir.TypeID{"x": ext.NamedType("/components/schemas/Pet")}, model.Discriminator.Mapping,
				"its $ref names ext.yaml's Pet, which the mapping reads through to, not the source's Pet")
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
					compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
				require.NoError(t, err)
				require.NotNil(t, doc)
				got := make([]string, 0, len(diags))
				for _, d := range diags {
					got = append(got, fmt.Sprintf("%s %s %d%s %s", d.Severity, d.Code, d.Provenance.Source,
						d.Provenance.Pointer, strings.ReplaceAll(d.Message, dir, "<dir>")))
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
// ext.yaml's schema (GitHub #762), lowered once where ext.yaml declares it, the
// node /x's own response lowers to as well, so the $ref written there names
// ext.yaml's Pet, which ext.yaml does not declare: it is unresolved, reported
// once, there. Read as the source's, it named the source's Pet without a word.
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
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)

	const named = "/components/responses/R/content/application~1json/schema"
	assert.Empty(t, cmp.Diff([]ir.Diagnostic{{Severity: ir.SeverityError, Code: "openapi/unresolved-ref",
		Message:    `unresolved $ref "#/components/schemas/Pet": it names nothing in the document it points into`,
		Provenance: ir.Provenance{Source: 1, Pointer: named + "/properties/pet"}}}, diags))
	require.NotNil(t, doc)
	theirs := ids.In("ext.yaml").AnonType(named)
	usesR, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/UsesR")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, usesR.Properties, 1)
	assert.Equal(t, theirs, usesR.Properties[0].Type.Target)
	ops := allOperations(doc)
	require.Len(t, ops, 1)
	require.Len(t, ops[0].Responses, 1)
	assert.Equal(t, theirs, openapitest.BodyTarget(t, ops[0].Responses[0].Payload), "/x's response lowers to that node")
	body, ok := doc.Types[theirs].(*ir.Model)
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
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
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
// source, so it is unresolved as any other $ref to a missing file is, reported
// where the document in the subdirectory writes it.
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
				compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
			require.NoError(t, err)

			if !c.names {
				want := ir.Diagnostic{Severity: ir.SeverityError, Code: "openapi/unresolved-ref",
					Message: `unresolved $ref "` + c.ref + `#/components/schemas/Thing": ` +
						"the document it names could not be read: open " + filepath.Join(dir, "sub", c.ref) +
						": no such file or directory",
					Provenance: ir.Provenance{Source: 1, Pointer: "/paths/~1x/get/responses/200/content/application~1json/schema"}}
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
					compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: allow}})
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
				compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
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
			model, ok := doc.Types[ids.In("ext.yaml").AnonType("/paths/~1b"+schema)].(*ir.Model)
			require.True(t, ok, "the mapping's schema is lowered as a model, where ext.yaml declares it")
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
				compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
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

// TestExternalContent_ARefLendsItsUseSiteWhatItsTypeReaches pins that a use
// site borrows what its schema $ref ends on, a default, a description, a form
// body's parts, a file body or a discriminator, whichever documents the chain
// passes, as its type reaches that schema there. A form body's parts are keyed
// to the properties of the model in the document declaring it, so none
// addresses a property the IR lacks (GitHub #1023), and a model composing a
// discriminated base from another document takes its discriminator value.
func TestExternalContent_ARefLendsItsUseSiteWhatItsTypeReaches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.yaml"), []byte(`RB:
  content:
    multipart/form-data:
      schema: {$ref: '#/Form'}
      encoding: {file: {contentType: image/png}}
RBHome:
  content:
    multipart/form-data:
      schema: {$ref: './root.yaml#/components/schemas/Form'}
      encoding: {file: {contentType: image/png}}
RBBin:
  content: {application/x-custom: {schema: {$ref: '#/Bin'}}}
RBBinHome:
  content: {application/x-custom: {schema: {$ref: './root.yaml#/components/schemas/Bin'}}}
Param: {name: lent, in: query, schema: {$ref: '#/Fmt'}}
ParamHome: {name: home, in: query, schema: {$ref: './root.yaml#/components/schemas/Fmt'}}
Resp:
  description: ok
  headers:
    X-Lent: {schema: {$ref: '#/Fmt'}}
    X-Home: {schema: {$ref: './root.yaml#/components/schemas/Fmt'}}
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
        - {$ref: './lib.yaml#/ParamHome'}
        - {name: own, in: query, schema: {$ref: './lib.yaml#/Fmt'}}
      requestBody: {$ref: './lib.yaml#/RB'}
      responses: {"200": {$ref: './lib.yaml#/Resp'}}
  /home:
    post:
      requestBody: {$ref: './lib.yaml#/RBHome'}
      responses: {"204": {description: ok}}
  /own:
    post:
      requestBody:
        content:
          multipart/form-data:
            schema: {$ref: './lib.yaml#/Form'}
            encoding: {file: {contentType: image/png}}
      responses: {"204": {description: ok}}
  /file:
    post:
      requestBody: {$ref: './lib.yaml#/RBBin'}
      responses: {"204": {description: ok}}
  /file-home:
    post:
      requestBody: {$ref: './lib.yaml#/RBBinHome'}
      responses: {"204": {description: ok}}
components:
  schemas:
    Local: {allOf: [{$ref: './lib.yaml#/Kid'}], properties: {z: {type: string}}}
    Mine: {allOf: [{$ref: './lib.yaml#/Pet'}], properties: {z: {type: string}}}
    Form: {type: object, properties: {file: {type: string, format: binary}}}
    Bin: {type: string, format: binary}
    Fmt: {type: string, default: json, description: fmt}
`
	doc, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	ops := map[ir.OpID]ir.Operation{}
	require.False(t, ir.ForEachOperation(doc, func(op ir.Operation) { ops[op.ID] = op }))
	op := func(path string) ir.Operation { return ops[ir.OpID("op/openapi/paths/~1"+path+"/post")] }

	params := map[string]ir.Parameter{}
	for _, p := range op("form").Params {
		params[p.Name.Source] = p
	}
	for _, name := range []string{"lent", "own", "home"} {
		require.NotNil(t, params[name].Default, "%s: a parameter's default", name)
		assert.Equal(t, "json", params[name].Default.Str, name)
		assert.Equal(t, "fmt", params[name].Docs.Description, "%s: a parameter's description", name)
	}
	require.Len(t, op("form").Responses, 1)
	headers := map[string]ir.Header{}
	for _, h := range op("form").Responses[0].Headers {
		headers[h.Name.Source] = h
	}
	for _, name := range []string{"X-Lent", "X-Home"} {
		require.NotNil(t, headers[name].Default, "%s: a header's default", name)
		assert.Equal(t, "json", headers[name].Default.Str, name)
		assert.Equal(t, "fmt", headers[name].Docs.Description, "%s: a header's description", name)
	}

	lib := ids.In("lib.yaml")
	for path, keys := range map[string][]ir.PropID{
		"form": {lib.Prop("/Form/properties/file")},
		"own":  {lib.Prop("/Form/properties/file")},
		"home": {"p/openapi/components/schemas/Form/properties/file"},
	} {
		require.NotNil(t, op(path).Request, path)
		require.Len(t, op(path).Request.Contents, 1, path)
		assert.Equal(t, keys, slices.Sorted(maps.Keys(op(path).Request.Contents[0].Encoding)), "%s: a form body's parts", path)
	}
	for _, path := range []string{"file", "file-home"} {
		require.NotNil(t, op(path).Request, path)
		require.Len(t, op(path).Request.Contents, 1, path)
		body := op(path).Request.Contents[0]
		assert.Equal(t, ir.TypeID("t/prim/bytes"), body.Type.Target, "%s: a binary body's type", path)
		assert.NotNil(t, body.File, path)
	}
	for name, base := range map[string]ir.TypeID{"Local": lib.AnonType("/Kid"), "Mine": lib.AnonType("/Pet")} {
		model, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/"+name)].(*ir.Model)
		require.True(t, ok, name)
		require.NotNil(t, model.Base, name)
		assert.Equal(t, base, model.Base.Target, name)
		assert.Equal(t, name, model.DiscriminatorValue, "%s: a discriminator through a composed schema", name)
	}
}

// TestExternalContent_APartFlagReadsTheFoldedType pins the order Finish
// settles in. A form body another document declares is built only once every
// lowering has returned, after its part keys were queued, and its part `file`
// is redeclared over a node built later still, so the fold waits too. The
// part's Filename is read off the type that fold settles on, bytes; read
// before it, the first declaration's string would leave the part unflagged.
func TestExternalContent_APartFlagReadsTheFoldedType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.yaml"), []byte(`Form:
  allOf:
    - {type: object, properties: {file: {type: string}}}
    - {type: object, properties: {file: {$ref: '#/Z/properties/bin'}}}
Z: {type: object, properties: {bin: {type: string, format: binary}}}
`), 0o600))
	root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /up:\n    post:\n" +
		"      operationId: up\n      requestBody:\n        content:\n          multipart/form-data:\n" +
		"            schema: {$ref: './lib.yaml#/Form'}\n      responses: {'204': {description: ok}}\n"
	path := filepath.Join(dir, "root.yaml")
	require.NoError(t, os.WriteFile(path, []byte(root), 0o600))

	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})

	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.False(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return d.Code == "openapi/conflicting-redeclaration"
	}), "the binary narrows the string: %+v", diags)
	op := operationAt(t, doc, "POST", "/up")
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	enc := op.Request.Contents[0].Encoding
	part, ok := enc[ids.In("lib.yaml").Prop("/Form/allOf/0/properties/file")]
	require.True(t, ok, "the part is keyed on the property the fold kept: %v", enc)
	assert.True(t, part.Filename, "a file part, as the settled type says")
	assert.False(t, part.Multi)
}

// TestExternalContent_ANodeAnotherDocumentHoistsIsNoCopyOfAnInlineOne pins
// that the copy rule reads a hoisted node's document as well as its pointer. A
// field redeclared as a $ref to an enum another document writes inline, at the
// very pointer the root writes its own copy at, and as that copy inline: the
// referenced enum was declared apart, so it wins in either branch order, with
// nothing reported or kept.
func TestExternalContent_ANodeAnotherDocumentHoistsIsNoCopyOfAnInlineOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(`openapi: 3.1.0
info: {title: O, version: "1"}
paths: {}
components:
  schemas:
    T:
      allOf:
        - {}
        - {type: object, properties: {p: {type: string, enum: [a, b]}}}
`), 0o600))
	ref := "{type: object, properties: {p: {$ref: './other.yaml#/components/schemas/T/allOf/1/properties/p'}}}"
	inline := "{type: object, properties: {p: {type: string, enum: [a, b]}}}"
	want := ids.In("other.yaml").AnonType("/components/schemas/T/allOf/1/properties/p")
	for _, branches := range [][2]string{{ref, inline}, {inline, ref}} {
		root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
			"    T:\n      allOf:\n        - " + branches[0] + "\n        - " + branches[1] + "\n"
		path := filepath.Join(dir, "root.yaml")
		require.NoError(t, os.WriteFile(path, []byte(root), 0o600))

		doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: []byte(root)}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})

		require.NoError(t, err)
		assert.Empty(t, diags, "first branch %s", branches[0])
		m, ok := doc.Types["t/openapi/components/schemas/T"].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		assert.Equal(t, want, m.Properties[0].Type.Target, "the referenced enum wins: first branch %s", branches[0])
		assert.Empty(t, m.Properties[0].Unmodeled, "first branch %s", branches[0])
	}
}

// TestExternalContent_ACopyInAnotherDocumentIsJudgedThere pins that the copy
// rule reads a declaration's document as well as its pointer when the
// composing model is itself another document's. F2 writes an enum inline and
// then a $ref to an equal component; F3 narrows a string to that inline enum
// first, which moves the declaration the rule reads to the enum's own. Each
// inline enum is a copy where it is written, so the component wins, with
// nothing reported or kept.
func TestExternalContent_ACopyInAnotherDocumentIsJudgedThere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(`openapi: 3.1.0
info: {title: O, version: "1"}
paths: {}
components:
  schemas:
    E: {type: string, enum: [a, b]}
    F2:
      allOf:
        - {type: object, properties: {p: {type: string, enum: [a, b]}}}
        - {type: object, properties: {p: {$ref: '#/components/schemas/E'}}}
    F3:
      allOf:
        - {type: object, properties: {p: {type: string}}}
        - {type: object, properties: {p: {type: string, enum: [a, b]}}}
        - {type: object, properties: {p: {$ref: '#/components/schemas/E'}}}
`), 0o600))
	const root = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    R2: {$ref: './other.yaml#/components/schemas/F2'}\n    R3: {$ref: './other.yaml#/components/schemas/F3'}\n"
	path := filepath.Join(dir, "root.yaml")
	require.NoError(t, os.WriteFile(path, []byte(root), 0o600))

	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})

	require.NoError(t, err)
	assert.Empty(t, diags)
	for _, name := range []string{"F2", "F3"} {
		m, ok := doc.Types[ir.TypeID("t/openapi@other.yaml/components/schemas/"+name)].(*ir.Model)
		require.True(t, ok, name)
		require.Len(t, m.Properties, 1, name)
		assert.Equal(t, ir.TypeID("t/openapi@other.yaml/components/schemas/E"), m.Properties[0].Type.Target, name)
		assert.Empty(t, m.Properties[0].Unmodeled, name)
	}
}
