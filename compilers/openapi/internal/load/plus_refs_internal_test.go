package load

import (
	"encoding/json/jsontext"
	"iter"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// loadedPlus is a Load of a document, with the tree the load was handed, so a
// test can see what it left behind (GitHub #911).
type loadedPlus struct {
	doc    *Document
	diags  []ir.Diagnostic
	root   *yaml.Node
	source string
}

// loadPlus loads spec, and requires that Load put everything back as written:
// the tree it was handed, and every model the lowering reads.
func loadPlus(t *testing.T, spec string, opts Options) loadedPlus {
	t.Helper()
	parsed, err := Decode([]byte(spec))
	require.NoError(t, err)
	src := compilers.Source{Path: "spec.yaml", Data: []byte(spec), Parsed: parsed}
	doc, diags, err := Load(t.Context(), 0, src, opts)
	require.NoError(t, err)
	require.NotNil(t, doc, "%v", diags)
	l := loadedPlus{doc: doc, diags: diags, root: parsed.Root(), source: spec}
	if opts.Overlay == nil {
		assertSameTree(t, decodeRoot(t, spec), l.root)
	}
	l.assertModelsAsWritten(t)
	return l
}

// assertModelsAsWritten requires every reference model the lowering can read to
// hold the $ref its scalar spells, in the model and in its core, whatever the
// resolver was handed. It finds them by a walk of its own: the source's, what
// the mapping targets hold, and everything each chain reached.
func (l loadedPlus) assertModelsAsWritten(t *testing.T) {
	t.Helper()
	ctx := t.Context()
	seen := map[any]bool{}
	var queue []any
	add := func(model any) {
		if _, _, ok := refCopies(model); ok && !seen[model] {
			seen[model] = true
			queue = append(queue, model)
		}
	}
	walk := func(items iter.Seq[soa.WalkItem]) {
		for item := range items {
			require.NoError(t, item.Match(soa.Matcher{Any: func(model any) error { add(model); return nil }}))
		}
	}
	walk(walkSource(ctx, l.doc.Doc, l.doc.Siblings))
	for _, s := range l.doc.Targets.resolved {
		add(s.ref)
	}
	for _, js := range l.doc.Targets.byPointer {
		walk(walkOf(js)(ctx))
	}
	for _, js := range l.doc.Targets.built {
		walk(walkOf(js)(ctx))
	}
	checked := 0
	for len(queue) > 0 {
		model := queue[0]
		queue = queue[1:]
		node, _, _ := refCopies(model)
		own, _ := model.(interface{ GetReference() references.Reference })
		written := nodeview.Deref(node).Value
		require.Equal(t, references.Reference(written), own.GetReference(), "%T's own copy", model)
		require.Equal(t, written, *coreRefOf(t, model), "%T's core copy of %q", model, written)
		checked++
		for _, rec := range resolutionChain(model).records {
			if !rec.reached {
				continue
			}
			walk(rec.walk(ctx))
		}
	}
	require.Positive(t, checked, "the document holds a reference")
}

// errorsOf returns the messages of the error diagnostics.
func errorsOf(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			out = append(out, d.Message)
		}
	}
	return out
}

// assertNoRewrittenSpelling requires that no diagnostic spells '+' as "%2B",
// which the source did not write.
func assertNoRewrittenSpelling(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		assert.NotContains(t, d.Message, "%2B", "%s at %s", d.Code, d.Provenance.Pointer)
	}
}

// refAt returns the reference the source writes at site.
func (l loadedPlus) refAt(t *testing.T, site jsontext.Pointer) resolvable {
	t.Helper()
	var found resolvable
	for item := range walkSource(t.Context(), l.doc.Doc, l.doc.Siblings) {
		require.NoError(t, item.Match(soa.Matcher{Any: func(model any) error {
			if r, ok := model.(resolvable); ok && r.IsReference() && jsontext.Pointer(item.Location.ToJSONPointer()) == site {
				found = r
			}
			return nil
		}}))
	}
	require.NotNil(t, found, "no reference is written at %s", site)
	return found
}

// nodeAt returns the node the source's tree holds at pointer.
func (l loadedPlus) nodeAt(t *testing.T, pointer jsontext.Pointer) *yaml.Node {
	t.Helper()
	path, complete := nodeview.New().PointerPath(nodeview.DocumentRoot(l.root), pointer)
	require.True(t, complete, "the tree holds %s", pointer)
	return path[len(path)-1]
}

// assertReaches requires the reference written at site to resolve to the node
// the tree holds at target.
func (l loadedPlus) assertReaches(t *testing.T, site, target jsontext.Pointer) {
	t.Helper()
	_, node, ok := reachedObject(l.refAt(t, site))
	require.True(t, ok, "the $ref at %s resolves to an object", site)
	assert.Same(t, l.nodeAt(t, target), node, "the $ref at %s names %s", site, target)
}

const plusIssueSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a+b:
    get:
      operationId: g
      responses:
        "200": {description: ok, content: {application/json: {schema: {type: string}}}}
  /c:
    get:
      operationId: h
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: '#/paths/~1a+b/get/responses/200/content/application~1json/schema'}
`

func TestLoad_APlusInAPointerNamesAPlus(t *testing.T) {
	t.Parallel()

	l := loadPlus(t, plusIssueSpec, Options{})

	assert.Empty(t, errorsOf(l.diags))
	l.assertReaches(t,
		"/paths/~1c/get/responses/200/content/application~1json/schema",
		"/paths/~1a+b/get/responses/200/content/application~1json/schema")
}

// TestLoad_APlusAndAnEscapedPlusAndASpaceNameTheirOwnKeys pins the collision the
// resolver's own reading made: it caches an object by the pointer it decodes, so
// a document holding both "/a b" and "/a+b" answered "#/paths/~1a+b/..." with
// whichever its first reference reached. Each key is reached by its own
// spelling, in whichever order the paths and the references are written.
func TestLoad_APlusAndAnEscapedPlusAndASpaceNameTheirOwnKeys(t *testing.T) {
	t.Parallel()
	const suffix = "/get/responses/200/content/application~1json/schema"
	path := map[string]string{
		"space": `  "/a b": {get: {responses: {"200": {description: ok, content: {application/json: {schema: {type: string}}}}}}}` + "\n",
		"plus":  `  "/a+b": {get: {responses: {"200": {description: ok, content: {application/json: {schema: {type: integer}}}}}}}` + "\n",
	}
	use := map[string]string{
		"plus":    "    Plus: {$ref: '#/paths/~1a+b" + suffix + "'}\n",
		"escaped": "    Escaped: {$ref: '#/paths/~1a%2Bb" + suffix + "'}\n",
		"space":   "    Space: {$ref: '#/paths/~1a%20b" + suffix + "'}\n",
	}
	orders := []struct {
		name  string
		paths []string
		uses  []string
	}{
		{"paths and uses as written", []string{"space", "plus"}, []string{"plus", "escaped", "space"}},
		{"paths reversed", []string{"plus", "space"}, []string{"plus", "escaped", "space"}},
		{"uses reversed", []string{"space", "plus"}, []string{"space", "escaped", "plus"}},
		{"both reversed", []string{"plus", "space"}, []string{"space", "escaped", "plus"}},
	}
	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n")
			for _, p := range order.paths {
				b.WriteString(path[p])
			}
			b.WriteString("components:\n  schemas:\n")
			for _, u := range order.uses {
				b.WriteString(use[u])
			}

			l := loadPlus(t, b.String(), Options{})

			assert.Empty(t, errorsOf(l.diags))
			l.assertReaches(t, "/components/schemas/Plus", "/paths/~1a+b"+suffix)
			l.assertReaches(t, "/components/schemas/Escaped", "/paths/~1a+b"+suffix)
			l.assertReaches(t, "/components/schemas/Space", "/paths/~1a b"+suffix)
		})
	}
}

func TestLoad_EveryKindOfReferenceResolvesThroughAPlusKey(t *testing.T) {
	t.Parallel()
	l := loadPlus(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a+b:
    get:
      parameters:
        - {name: q, in: query, schema: {type: string}}
      responses:
        "200":
          description: ok
          headers:
            H: {schema: {type: string}}
          content:
            application/problem+json:
              examples:
                E: {value: 1}
    post:
      requestBody:
        content: {application/json: {schema: {type: string}}}
      responses: {"204": {description: none}}
  /c:
    $ref: '#/paths/~1a+b'
  /d:
    post:
      parameters:
        - $ref: '#/paths/~1a+b/get/parameters/0'
      requestBody:
        $ref: '#/paths/~1a+b/post/requestBody'
      responses:
        "200": {$ref: '#/paths/~1a+b/get/responses/200'}
        default:
          description: d
          headers:
            H: {$ref: '#/paths/~1a+b/get/responses/200/headers/H'}
          content:
            application/problem+json:
              examples:
                E: {$ref: '#/paths/~1a+b/get/responses/200/content/application~1problem+json/examples/E'}
`, Options{})

	assert.Empty(t, errorsOf(l.diags))
	tests := []struct {
		kind, site, target jsontext.Pointer
	}{
		{"path item", "/paths/~1c", "/paths/~1a+b"},
		{"parameter", "/paths/~1d/post/parameters/0", "/paths/~1a+b/get/parameters/0"},
		{"request body", "/paths/~1d/post/requestBody", "/paths/~1a+b/post/requestBody"},
		{"response", "/paths/~1d/post/responses/200", "/paths/~1a+b/get/responses/200"},
		{"header", "/paths/~1d/post/responses/default/headers/H", "/paths/~1a+b/get/responses/200/headers/H"},
		{"example under a +json media type", "/paths/~1d/post/responses/default/content/application~1problem+json/examples/E",
			"/paths/~1a+b/get/responses/200/content/application~1problem+json/examples/E"},
	}
	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			l.assertReaches(t, tc.site, tc.target)
		})
	}
}

func TestLoad_APlusKeyOfRawYAMLIsReached(t *testing.T) {
	t.Parallel()
	l := loadPlus(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {$ref: '#/x-defs/A'}
    T: {$ref: '#/x-a'}
x-defs:
  A: {type: object, properties: {p: {$ref: '#/x-defs/B+C'}, q: {$ref: 'other.yaml#/x+y'}}}
  B+C: {type: integer}
x-a: {$ref: '#/x-b+c'}
x-b+c: {type: integer}
`, Options{})

	assert.Empty(t, errorsOf(l.diags))
	l.assertReaches(t, "/components/schemas/T", "/x-b+c")
	obj, _, ok := reachedObject(l.refAt(t, "/components/schemas/S"))
	require.True(t, ok)
	props := obj.(*oas3.JSONSchema[oas3.Concrete]).GetSchema().GetProperties()
	p, _ := props.Get("p")
	_, node, ok := reachedObject(p)
	require.True(t, ok, "a $ref inside an object built from raw YAML resolves too")
	assert.Same(t, l.nodeAt(t, "/x-defs/B+C"), node)
	q, _ := props.Get("q")
	assert.Equal(t, references.Reference("other.yaml#/x+y"), q.GetRef(),
		"and one the load phase never resolves is put back as written as well")
}

func TestLoad_AReferenceThroughAnAliasIsReached(t *testing.T) {
	t.Parallel()
	l := loadPlus(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a+b:
    get:
      responses:
        "200": {description: ok, content: {application/json: {schema: {type: string}}}}
x-target: &target '#/paths/~1a+b/get/responses/200/content/application~1json/schema'
components:
  schemas:
    Aliased: {$ref: *target}
`, Options{})

	assert.Empty(t, errorsOf(l.diags))
	l.assertReaches(t, "/components/schemas/Aliased", "/paths/~1a+b/get/responses/200/content/application~1json/schema")
}

// TestLoad_AHeldReferenceBesideAPathItemRefIsQuotedAsWritten pins the words of
// the refusal of a "#/$defs/..." reference written in the fields beside a path
// item's $ref: it quotes the $ref as the source writes it.
func TestLoad_AHeldReferenceBesideAPathItemRefIsQuotedAsWritten(t *testing.T) {
	t.Parallel()
	l := loadPlus(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: '#/$defs/a+b'}}}
  /b:
    get:
      responses:
        "200": {description: ok}
`, Options{})

	errs := errorsOf(l.diags)
	require.Len(t, errs, 1)
	assert.True(t, strings.HasPrefix(errs[0], `unresolved $ref "#/$defs/a+b": it is written in the fields beside a path item $ref`), errs[0])
	assertNoRewrittenSpelling(t, l.diags)
}

func TestLoad_AHeldReferenceThroughAPlusResolvesToItsOwnDefinition(t *testing.T) {
	t.Parallel()
	l := loadPlus(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p: {$ref: '#/$defs/a+b'}
        q: {$ref: '#/$defs/nope+x'}
      $defs:
        a+b: {type: string}
`, Options{})

	l.assertReaches(t, "/components/schemas/A/properties/p", "/components/schemas/A/$defs/a+b")
	errs := errorsOf(l.diags)
	require.Len(t, errs, 1, "the definition that is not there")
	assert.Equal(t, `unresolved $ref "#/$defs/nope+x": definition not found: #/$defs/nope+x`, errs[0])
	assertNoRewrittenSpelling(t, l.diags)
}

func TestLoad_ADiscriminatorMappingToAPlusKeyIsResolved(t *testing.T) {
	t.Parallel()
	l := loadPlus(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      type: object
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping:
          bc: '#/x-defs/B+C'
x-defs:
  B+C:
    type: object
    properties: {other: {$ref: 'other.yaml#/x+y'}}
`, Options{})

	assert.Empty(t, errorsOf(l.diags))
	target := l.doc.Targets.At("/x-defs/B+C")
	require.NotNil(t, target, "the mapping target is resolved")
	assert.Same(t, l.nodeAt(t, "/x-defs/B+C"), target.GetSchema().GetRootNode())
	other, _ := target.GetSchema().GetProperties().Get("other")
	assert.Equal(t, references.Reference("other.yaml#/x+y"), other.GetRef())
}

func TestLoad_AnUnresolvedReferenceIsQuotedAsTheSourceWritesIt(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n"
	tests := []struct {
		name string
		body string
	}{
		{"a parameter", "  /a:\n    get:\n      parameters: [{$ref: '#/paths/~1nope+x/get'}]\n      responses: {\"200\": {description: ok}}\n"},
		{"a response", "  /a:\n    get:\n      responses: {\"200\": {$ref: '#/paths/~1nope+x/get'}}\n"},
		{"a schema", "  /a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n" +
			"          content: {application/json: {schema: {$ref: '#/paths/~1nope+x/get'}}}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := loadPlus(t, head+tc.body, Options{})

			errs := errorsOf(l.diags)
			require.Len(t, errs, 1)
			assert.True(t, strings.HasPrefix(errs[0], `unresolved $ref "#/paths/~1nope+x/get": `), errs[0])
			assert.Contains(t, errs[0], "~1nope+x", "the resolver's reason names the key as written too")
			assertNoRewrittenSpelling(t, l.diags)
		})
	}
}

func TestLoad_AChainThatStopsAtAPlusReferenceQuotesItAsWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "other.yaml", `components:
  schemas:
    X: {$ref: '#/components/schemas/Y+Z'}
`)
	spec := `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: 'other.yaml#/components/schemas/X'}
`
	parsed, err := Decode([]byte(spec))
	require.NoError(t, err)

	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(spec), Parsed: parsed},
		Options{AllowExternalRefs: true})

	require.NoError(t, err)
	errs := errorsOf(diags)
	require.Len(t, errs, 1)
	assert.True(t, strings.HasPrefix(errs[0], `unresolved $ref "other.yaml#/components/schemas/X", through "#/components/schemas/Y+Z" in `+
		filepath.Join(dir, "other.yaml")+": "), errs[0])
	assertNoRewrittenSpelling(t, diags)
}

func TestLoad_APlusInAnotherDocumentIsReached(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "other.yaml", `x+y: {type: string}
x-start: {$ref: '#/x-p+q'}
x-p+q: {type: integer}
`)
	spec := `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: 'other.yaml#/x+y'}
    B: {$ref: 'other.yaml#/x-start'}
`
	parsed, err := Decode([]byte(spec))
	require.NoError(t, err)

	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(spec), Parsed: parsed},
		Options{AllowExternalRefs: true})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, diags)
	assertSameTree(t, decodeRoot(t, spec), parsed.Root())
}

func TestExternal_PrepareRewritesTheTreeItStoresAndRestoreTreesPutsItBack(t *testing.T) {
	t.Parallel()
	const body = "a: &x '#/p+q'\nb: {$ref: *x}\nc: {$ref: 'o.yaml#/r+s'}\n"
	esc := newEscapes()
	doc := &soa.OpenAPI{}
	reader := newExternal(doc, Options{}, newExternalReads(sourceDocument{escapes: esc}))

	_, err := reader.prepare("k", strings.NewReader(body))

	require.NoError(t, err)
	cached, ok := doc.GetCachedExternalDocument("k")
	require.True(t, ok)
	root, ok := cached.(*yaml.Node)
	require.True(t, ok)
	assert.Equal(t, []string{"#/p%2Bq", "o.yaml#/r%2Bs"}, refValues(root), "the resolver is handed the tree rewritten")
	assert.Equal(t, "#/p+q", root.Content[0].Content[1].Value, "but for the scalar an alias names")

	esc.restoreTrees()

	assertSameTree(t, decodeRoot(t, body), root)
}

func TestLoad_APlusReferenceAnOverlayWritesIsReached(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a+b:
    get:
      responses: {"200": {description: ok, content: {application/json: {schema: {type: string}}}}}
components:
  schemas:
    S: {type: object}
`
	opts := Options{Overlay: &overlay.Options{Path: "patch.yaml", Data: []byte(overlayHeader +
		"  - target: $.components.schemas\n    update:\n      Over:\n" +
		"        $ref: '#/paths/~1a+b/get/responses/200/content/application~1json/schema'\n")}, OverlaySrcIndex: 1}

	l := loadPlus(t, spec, opts)

	assert.Empty(t, errorsOf(l.diags))
	assert.True(t, l.doc.Overlay.Applied())
	l.assertReaches(t, "/components/schemas/Over", "/paths/~1a+b/get/responses/200/content/application~1json/schema")
	for _, v := range refValues(l.root) {
		assert.NotContains(t, v, "%2B", "the overlay's spelling is the tree's")
	}
}

func TestResolve_APlusReferenceInAnUnresolvableSpellingIsLeftToTheResolver(t *testing.T) {
	t.Parallel()
	// '+' in a fragment the resolver cannot decode already reads as a plus.
	l := loadPlus(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: '#/components/schemas/B+%zz'}
`, Options{})

	assert.True(t, slices.ContainsFunc(errorsOf(l.diags), func(msg string) bool {
		return strings.HasPrefix(msg, `unresolved $ref "#/components/schemas/B+%zz": `)
	}), "%v", errorsOf(l.diags))
}

// TestLoad_ACheckAfterResolveQuotesTheReferenceAsWritten pins that the models are
// put back when resolve returns, so what the checks that follow it quote is
// the source's spelling: the refusal of a chain into the fields beside a path
// item's $ref quotes each reference it refuses.
func TestLoad_ACheckAfterResolveQuotesTheReferenceAsWritten(t *testing.T) {
	t.Parallel()
	spec := strings.Replace(strings.ReplaceAll(besideChainSpec, "~1a", "~1a+b"), "  /a:\n", "  /a+b:\n", 1)

	l := loadPlus(t, spec, Options{})

	refused := map[jsontext.Pointer]string{}
	for _, d := range l.diags {
		if d.Code == diag.UnresolvedRef && d.Severity == ir.SeverityError {
			refused[d.Provenance.Pointer] = d.Message
		}
	}
	require.Len(t, refused, 3)
	assert.True(t, strings.HasPrefix(refused["/components/requestBodies/X"],
		`unresolved $ref "#/paths/~1a+b/get/requestBody": it reaches /paths/~1a+b/get/requestBody; `), refused["/components/requestBodies/X"])
	assertNoRewrittenSpelling(t, l.diags)
}

// TestHold_AnObjectUnderAPlusKeyIsHeldAheadUnderTheSpellingAPlusRefUses pins
// that a $ref in a document naming the source's object at a key with a plus, by
// a spelling of the source no key holds, finds the source's own object held
// under that spelling and pointer: the pointer the resolver is handed (see
// fragment.ForResolver) is the one the object is held under.
func TestHold_AnObjectUnderAPlusKeyIsHeldAheadUnderTheSpellingAPlusRefUses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc, pass := heldResolution(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a+b:
    get:
      responses:
        "200": {description: ok}
`, filepath.Join(dir, "root.yaml"))
	unclean := dir + "/./root.yaml"
	tree, ok := parseTree([]byte("a: {$ref: '" + unclean + "#/paths/~1a+b/get/responses/200'}\n"))
	require.True(t, ok)

	pass.reader.holdSpellings(tree, filepath.Join(dir, "sub", "doc.yaml"))

	held, ok := doc.GetCachedReferencedObject(unclean + "#/paths/~1a+b/get/responses/200")
	require.True(t, ok, "held under the pointer that spells the plus")
	assert.Same(t, response200(t, doc, "/a+b"), held, "the source's own object")
}

// TestLoad_ARespelledURLHoldingAPlusReferenceIsReadAsTheTreeIs pins that a
// document the resolver parses itself, because net/url respells its URL, is
// resolved a second time over the prepared tree when it holds a $ref whose '+'
// the resolver would read as a space: its own parse keeps the written spelling.
// The document holds no anchor, which is the other thing that sends it round
// again, and is fetched once whichever way its URL is spelled.
func TestLoad_ARespelledURLHoldingAPlusReferenceIsReadAsTheTreeIs(t *testing.T) {
	t.Parallel()
	spellings := append([]struct {
		name  string
		spell func(canonical string) string
	}{{"as written", func(c string) string { return c }}}, respellings...)
	for _, tc := range spellings {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, requests := countingServer(t, "x-start: {$ref: '#/x-p+q'}\nx-p+q: {type: integer}\n")
			spec := "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
				"    A: {$ref: '" + tc.spell(srv.URL+"/ext.yaml") + "#/x-start'}\n"

			got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "root.yaml", Data: []byte(spec)},
				Options{AllowExternalRefs: true})

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Empty(t, diags)
			assert.Equal(t, int32(1), requests.Load(), "the document is fetched once")
		})
	}
}

// TestResolveExternal_ARespellingHoldingOnlyPlainReferencesIsNotRebuilt pins the
// anyRewritable gate: a respelled URL whose document carries $refs, none of
// which the resolver reads otherwise than the prepared tree is spelled, has
// nothing a second pass would change, so the rebuild it would cost never runs.
func TestResolveExternal_ARespellingHoldingOnlyPlainReferencesIsNotRebuilt(t *testing.T) {
	t.Parallel()
	const plain = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    get: {operationId: getX, responses: {"200": {$ref: '#/components/responses/R'}}}
components:
  responses:
    R: {description: ok, x-p+q: {$ref: 'o.yaml#/a%2Bb'}}
`
	srv, requests := countingServer(t, plain)
	url := "HTTP://" + strings.TrimPrefix(srv.URL, "http://") + "/ext.yaml"
	rebuild := func() (*soa.OpenAPI, error) {
		t.Fatal("rebuild must not be called: nothing the respelled document holds is read otherwise")
		return nil, nil // unreachable: t.Fatal stops this goroutine first
	}

	doc, diags, err := resolveSpecWith(t, rootReferencing(url), rebuild)

	require.NoError(t, err)
	assert.Empty(t, diags)
	ref, ok := doc.Paths.Get("/x")
	require.True(t, ok)
	assert.Equal(t, "getX", ref.GetObject().Get().GetOperationID())
	assert.Equal(t, int32(1), requests.Load())
}
