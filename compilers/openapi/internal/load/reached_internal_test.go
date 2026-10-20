package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// externalDir writes files, name to content, into a fresh directory for a root
// to reach, and returns the directory.
func externalDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		writeFile(t, dir, name, content)
	}
	return dir
}

// loadExternal loads root as root.yaml in dir, with external references
// allowed, and requires it to load.
func loadExternal(t *testing.T, dir, root string, opts Options) (*Document, []ir.Diagnostic) {
	t.Helper()
	opts.AllowExternalRefs = true
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}, opts)
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)
	return got, diags
}

// TestSpanned covers spanned's properties: it counts every node under root,
// an alias counts as a copy of what it names, a tree of exactly limit nodes
// fits, and a limit stops the count one node past it, never at the whole tree.
// It has no case for a nil node: see spanned's own doc comment for why yaml.v3
// never gives it one to skip.
func TestSpanned(t *testing.T) {
	t.Parallel()
	wide := &yaml.Node{Kind: yaml.MappingNode}
	for range 1000 {
		wide.Content = append(wide.Content, &yaml.Node{Kind: yaml.ScalarNode})
	}
	target := &yaml.Node{Kind: yaml.ScalarNode}
	aliased := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{{Kind: yaml.AliasNode, Alias: target}}}

	for name, c := range map[string]struct {
		root  *yaml.Node
		limit int
		count int
		fits  bool
	}{
		"every node under root":                {wide, -1, 1001, true},
		"an alias counts its target as a copy": {aliased, -1, 3, true},
		"a tree of exactly limit nodes fits":   {wide, 1001, 1001, true},
		"a limit stops the count one past it":  {wide, 6, 7, false},
	} {
		count, fits := spanned(c.root, c.limit)
		assert.Equal(t, c.count, count, name)
		assert.Equal(t, c.fits, fits, name)
	}
}

// TestReached_ChargeHoldsADocumentToWhatItHasLeft pins that charge measures an
// object against what its document has left, not against the whole budget: of
// a document with 6 nodes left, a 7-node object is refused and a 6-node one is
// admitted, after which a document with none left refuses even one node.
func TestReached_ChargeHoldsADocumentToWhatItHasLeft(t *testing.T) {
	t.Parallel()
	object := func(nodes int) *yaml.Node {
		root := &yaml.Node{Kind: yaml.MappingNode}
		for range nodes - 1 {
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode})
		}
		return root
	}
	charged := func() *reached {
		return &reached{limit: 10, spent: map[string]int{"doc": 4}, over: map[string]bool{}}
	}

	v := charged()
	d, over := v.charge(ir.Provenance{}, "doc", object(7))
	assert.True(t, over)
	require.Len(t, d, 1)
	assert.Equal(t, diag.BudgetExceeded, d[0].Code)

	v = charged()
	d, over = v.charge(ir.Provenance{}, "doc", object(6))
	assert.False(t, over)
	assert.Empty(t, d)
	assert.Equal(t, 10, v.spent["doc"])

	d, over = v.charge(ir.Provenance{}, "doc", object(1))
	assert.True(t, over)
	require.Len(t, d, 1)
	assert.Equal(t, diag.BudgetExceeded, d[0].Code)
}

// reachedFixtureOther, reachedFixtureThird and reachedFixtureRoot exercise
// reachedObject and targetOf through a real, resolved document: a schema
// reference to an object, one to a boolean (nothing but its value), a path
// item, a path item whose chain stops at a pointer naming nothing, a response
// whose chain loops, a chain through two documents, and internal references
// that stay in the source or leave it.
const reachedFixtureOther = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      responses: {"200": {description: ok}}
  /stops: {$ref: "#/paths/~1missing"}
components:
  schemas:
    Obj: {type: object, properties: {inner: {type: string}}}
    AnyBool: true
    Hop: {$ref: "./third.yaml#/components/schemas/Leaf"}
  responses:
    Loop: {$ref: "#/components/responses/Back"}
    Back: {$ref: "#/components/responses/Loop"}
`

const reachedFixtureThird = `components:
  schemas:
    Leaf: {type: string}
`

const reachedFixtureRoot = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x: {$ref: "./other.yaml#/paths/~1x"}
  /stops: {$ref: "./other.yaml#/paths/~1stops"}
components:
  schemas:
    S: {$ref: "./other.yaml#/components/schemas/Obj"}
    Bool: {$ref: "./other.yaml#/components/schemas/AnyBool"}
    Chained: {$ref: "./other.yaml#/components/schemas/Hop"}
    Local: {type: string}
    ToLocal: {$ref: "#/components/schemas/Local"}
    ToS: {$ref: "#/components/schemas/S"}
  responses:
    Loops: {$ref: "./other.yaml#/components/responses/Loop"}
`

// reachedFixtureDoc loads reachedFixtureRoot beside the documents it names
// and returns the resolved document and the directory they are in.
func reachedFixtureDoc(t *testing.T) (*soa.OpenAPI, string) {
	t.Helper()
	dir := externalDir(t, map[string]string{"other.yaml": reachedFixtureOther, "third.yaml": reachedFixtureThird})
	got, _ := loadExternal(t, dir, reachedFixtureRoot, Options{})
	return got.Doc, dir
}

// TestTargetOf covers which reference objects reach an object, and the
// document each names: the one its chain ends in, not the one it first leaves
// for. One whose chain ends on no object reaches nothing.
func TestTargetOf(t *testing.T) {
	t.Parallel()
	doc, dir := reachedFixtureDoc(t)
	x, ok := doc.Paths.Get("/x")
	require.True(t, ok)
	stops, ok := doc.Paths.Get("/stops")
	require.True(t, ok)

	got, ok := targetOf("/site", x, nil)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "other.yaml"), got.doc)
	assert.False(t, got.inSource)
	assert.Equal(t, jsontext.Pointer("/site"), got.site)
	assert.NotNil(t, got.node)
	_, ok = targetOf("/site", stops, nil)
	assert.False(t, ok, "/stops reaches no object")
}

// TestBindSchemas_AChainEndsInTheDocumentItEndsIn is TestTargetOf for schema
// $refs, which the bindings read: each chain ends in the document its last
// hop lands in, not the one it first leaves for, and a schema the model does
// not hold there is built for the landing, a boolean one included.
func TestBindSchemas_AChainEndsInTheDocumentItEndsIn(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": reachedFixtureOther, "third.yaml": reachedFixtureThird})
	got, _ := loadExternal(t, dir, reachedFixtureRoot, Options{})
	doc, bindings := got.Doc, got.Bindings
	table := bindings.Table()
	for name, want := range map[string]struct {
		doc     string
		pointer jsontext.Pointer
	}{
		"S":       {filepath.Join(dir, "other.yaml"), "/components/schemas/Obj"},
		"Bool":    {filepath.Join(dir, "other.yaml"), "/components/schemas/AnyBool"},
		"Chained": {filepath.Join(dir, "third.yaml"), "/components/schemas/Leaf"},
		"ToS":     {filepath.Join(dir, "other.yaml"), "/components/schemas/Obj"},
		"ToLocal": {filepath.Join(dir, "root.yaml"), "/components/schemas/Local"},
	} {
		js, ok := doc.Components.Schemas.Get(name)
		require.True(t, ok, name)
		e, ok := bindings.Entry(js)
		require.True(t, ok, name)
		require.Equal(t, bind.Resolved, e.Chain.Status, name)
		assert.Equal(t, want.doc, table.Document(e.Chain.End.Doc).Name, name)
		assert.Equal(t, want.pointer, e.Chain.End.Pointer, name)
		assert.NotNil(t, bindings.End(js), "%s ends on a schema held for it", name)
	}
}

// TestBuiltTargets covers which schemas built for the bindings' landings are
// validated, and where: one a source $ref's chain reaches, placed at the least
// such $ref and named by its document, in the source or not. One only another
// document's content reaches, one that failed to build and a boolean schema
// are none.
func TestBuiltTargets(t *testing.T) {
	t.Parallel()
	built := buildRaw(t, "{type: object}")
	found := []error{errors.New("decoding found this")}
	got := builtTargets([]typed.Built{
		{Landing: bind.Landing{At: bind.Position{Pointer: "/x-lib/A"}, Name: "spec.yaml",
			Site: bind.Position{Pointer: "/s"}, Reached: true}, Schema: built, Findings: found},
		{Landing: bind.Landing{At: bind.Position{Doc: 1, Pointer: "/B"}, Name: "other.yaml",
			Site: bind.Position{Pointer: "/t"}, Reached: true}, Schema: built},
		{Landing: bind.Landing{At: bind.Position{Doc: 1, Pointer: "/C"}, Name: "other.yaml"}, Schema: built},
		{Landing: bind.Landing{At: bind.Position{Pointer: "/x-lib/D"}, Reached: true}, Err: errors.New("no")},
		{Landing: bind.Landing{At: bind.Position{Pointer: "/x-lib/E"}, Reached: true}, Schema: buildRaw(t, "true")},
	})

	require.Len(t, got, 2)
	assert.Equal(t, reachedTarget{site: "/s", obj: built, node: built.GetSchema().GetRootNode(), doc: "spec.yaml",
		inSource: true, found: found}, got[0])
	assert.Equal(t, jsontext.Pointer("/t"), got[1].site)
	assert.Equal(t, "other.yaml", got[1].doc)
	assert.False(t, got[1].inSource)
}

// buildRaw decodes src, YAML, as the bindings build a schema for a landing.
func buildRaw(t *testing.T, src string) *schemaRef {
	t.Helper()
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &n))
	js := &schemaRef{}
	_, err := marshaller.UnmarshalNode(t.Context(), "", n.Content[0], js)
	require.NoError(t, err)
	return js
}

// TestReachedObject covers every outcome of reachedObject: a reference object
// resolving to an object, a chain that ends on no object, and a resolvable
// that cannot say what it names, which no real document produces, so a fake
// drives it.
func TestReachedObject(t *testing.T) {
	t.Parallel()
	doc, _ := reachedFixtureDoc(t)

	t.Run("a reference object resolving to an object", func(t *testing.T) {
		t.Parallel()
		ref, ok := doc.Paths.Get("/x")
		require.True(t, ok)
		obj, node, ok := reachedObject(ref)
		assert.True(t, ok)
		assert.Same(t, ref.GetObject(), obj)
		assert.Same(t, ref.GetObject().GetRootNode(), node)
	})

	stops, ok := doc.Paths.Get("/stops")
	require.True(t, ok)
	loops, ok := doc.Components.Responses.Get("Loops")
	require.True(t, ok)
	for name, ref := range map[string]resolvable{"stops": stops, "loops": loops} {
		t.Run("a chain that "+name+" ends on no object", func(t *testing.T) {
			t.Parallel()
			require.True(t, ref.IsResolved(), "the resolver marks it resolved, with nothing at its end")
			obj, node, ok := reachedObject(ref)
			assert.False(t, ok)
			assert.Nil(t, obj)
			assert.Nil(t, node)
		})
	}

	t.Run("a resolvable that cannot say what it names names nothing to validate", func(t *testing.T) {
		t.Parallel()
		_, _, ok := reachedObject(fakeResolvable{ref: "other.yaml#/x", resolved: true})
		assert.False(t, ok)
	})
}

// TestValidateObject covers validateObject's two outcomes: an object, a schema
// among them, validated through its own Validate, and a value with no
// Validate, which no resolvable a real document produces reaches, so it is
// driven directly.
func TestValidateObject(t *testing.T) {
	t.Parallel()
	doc, valErrs := parseSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      parameters: [{name: q, in: sideways, schema: {type: string}}]
      responses: {"200": {description: ok}}
components:
  schemas:
    Bad: {type: string, minLength: -1}
`)
	require.Len(t, valErrs, 2, "the source's own validation sees both defects: %v", valErrs)
	schema, ok := doc.Components.Schemas.Get("Bad")
	require.True(t, ok)
	pathItem, ok := doc.Paths.Get("/x")
	require.True(t, ok)

	for name, c := range map[string]struct {
		obj  any
		rule string
	}{
		"a schema":    {schema.GetResolvedSchema(), validation.RuleValidationInvalidSchema},
		"a path item": {pathItem.GetObject(), validation.RuleValidationAllowedValues},
	} {
		errs := validateObject(t.Context(), c.obj, nil)
		require.Len(t, errs, 1, "%s: %v", name, errs)
		verr, ok := asValidationError(errs[0])
		require.True(t, ok, name)
		assert.Equal(t, c.rule, verr.Rule, name)
	}
	assert.Nil(t, validateObject(t.Context(), 42, nil), "a value with no Validate")
}

// aliasKindsResolved resolves aliasKindsFixture (unprepared_internal_test.go)
// and returns the bare object each Referenced* wrapper resolves to, keyed as
// resolvedModels keys them: this fixture is the one place every kind
// reachedWalk and resolutionTrail switch on already exists, wired together.
func aliasKindsResolved(t *testing.T) map[string]any {
	t.Helper()
	doc, diags, err := resolveSpec(t, aliasKindsFixture, "root.yaml")
	require.NoError(t, err)
	require.Empty(t, diags)
	models := resolvedModels(t, doc)
	out := map[string]any{}
	out["pathItem"] = models["pathItem"].(*soa.ReferencedPathItem).GetObject()
	out["parameter"] = models["parameter"].(*soa.ReferencedParameter).GetObject()
	out["header"] = models["header"].(*soa.ReferencedHeader).GetObject()
	out["requestBody"] = models["requestBody"].(*soa.ReferencedRequestBody).GetObject()
	out["response"] = models["response"].(*soa.ReferencedResponse).GetObject()
	out["callback"] = models["callback"].(*soa.ReferencedCallback).GetObject()
	out["example"] = models["example"].(*soa.ReferencedExample).GetObject()
	out["link"] = models["link"].(*soa.ReferencedLink).GetObject()
	out["securityScheme"] = models["securityScheme"].(*soa.ReferencedSecurityScheme).GetObject()
	return out
}

// countSchemas counts the schema matches reachedWalk(ctx, obj) yields.
func countSchemas(ctx context.Context, obj any) int {
	n := 0
	matchSchemas(reachedWalk(ctx, obj), func(*oas3.JSONSchema[oas3.Referenceable]) error {
		n++
		return nil
	})
	return n
}

// TestReachedWalk covers every branch of reachedWalk: the schema case, each of
// the concrete kinds it walks through a synthetic Reference wrapper (see
// reachedWalk's own doc comment for why a wrapper rather than the bare type),
// the three that hold no schema but are walked for the later version's
// constructs, and the default arm for a kind it does not know.
func TestReachedWalk(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("a schema walks as one", func(t *testing.T) {
		t.Parallel()
		obj := buildRaw(t, "{type: object, properties: {inner: {type: string}}}")
		assert.GreaterOrEqual(t, countSchemas(ctx, obj), 2, "the schema itself and its inner property")
	})

	kinds := aliasKindsResolved(t)

	// aliasKindsFixture's own path item and response nest every field behind a
	// further $ref (resolutionTrail's dispatch is what that fixture is
	// for), so neither has a schema an unresolved-boundary walk would ever
	// reach; a schema nested inline, as a real document can write it, needs
	// its own small fixture instead.
	t.Run("a path item walks into its operations", func(t *testing.T) {
		t.Parallel()
		doc, valErrs := parseSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      parameters:
        - {name: q, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
`)
		require.Empty(t, valErrs)
		ref, ok := doc.Paths.Get("/x")
		require.True(t, ok)
		assert.Positive(t, countSchemas(ctx, ref.GetObject()))
	})
	t.Run("a parameter walks into its own schema", func(t *testing.T) {
		t.Parallel()
		assert.Positive(t, countSchemas(ctx, kinds["parameter"]))
	})
	t.Run("a header walks into its own schema", func(t *testing.T) {
		t.Parallel()
		assert.Positive(t, countSchemas(ctx, kinds["header"]))
	})
	t.Run("a request body walks into its content's schema", func(t *testing.T) {
		t.Parallel()
		assert.Positive(t, countSchemas(ctx, kinds["requestBody"]))
	})
	t.Run("a response walks into its header's schema", func(t *testing.T) {
		t.Parallel()
		doc, valErrs := parseSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      responses:
        "200":
          description: ok
          headers:
            X: {schema: {type: string}}
`)
		require.Empty(t, valErrs)
		ref, ok := doc.Paths.Get("/x")
		require.True(t, ok)
		resp, ok := ref.GetObject().Get().GetResponses().Get("200")
		require.True(t, ok)
		assert.Positive(t, countSchemas(ctx, resp.GetObject()))
	})
	t.Run("a callback does not panic even though it holds no schema of its own", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() { countSchemas(ctx, kinds["callback"]) })
	})

	t.Run("an example, a link and a security scheme are walked, holding no schema", func(t *testing.T) {
		t.Parallel()
		for _, kind := range []string{"example", "link", "securityScheme"} {
			assert.Zero(t, countSchemas(ctx, kinds[kind]), "%s holds no schema", kind)
			assert.Positive(t, countItems(ctx, kinds[kind]), "%s is walked all the same", kind)
		}
	})
	t.Run("the default arm walks nothing", func(t *testing.T) {
		t.Parallel()
		assert.Zero(t, countItems(ctx, 42))
	})
}

// countItems returns how many items reachedWalk yields for obj.
func countItems(ctx context.Context, obj any) int {
	n := 0
	for range reachedWalk(ctx, obj) {
		n++
	}
	return n
}

// securityRequirementFixtureOther and its root reach an operation whose
// security requirement is checked against a scheme name the components never
// declare — the SecurityRequirement.Validate path that panics without the
// root document in context (security.go).
const securityRequirementFixtureOther = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      security:
        - nosuch: []
      responses: {"200": {description: ok}}
`

const securityRequirementFixtureRoot = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x: {$ref: "./other.yaml#/paths/~1x"}
`

// panickingObject is an object whose Validate panics, driving checkAll's
// barrier without depending on a particular library fault.
type panickingObject struct{}

func (panickingObject) Validate(context.Context, ...validation.Option) []error {
	panic("object validation panicked")
}

// panickingRef is a fakeResolvable naming a panickingObject, so reachedObject
// succeeds and validateObject then panics on it.
type panickingRef struct{ fakeResolvable }

func (panickingRef) GetObjectAny() any { return panickingObject{} }

func (panickingRef) GetRootNode() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode} }

// TestCheckReached_APanicIsReported pins how both of checkReached's barriers
// turn a recovered panic into a diagnostic: openapi/validation, "validation
// panicked". One in an object's validation is reported at the $ref that
// reached it. One reading the walk is reported at the root, and nothing read
// before it is validated. No real document reaches either: the root document
// newReached passes keeps a security requirement from panicking
// (TestResolve_ASecurityRequirementInAReachedOperationDoesNotPanic).
func TestCheckReached_APanicIsReported(t *testing.T) {
	t.Parallel()
	doc, valErrs := parseSpec(t, minimal31)
	require.Empty(t, valErrs)
	item := fakeWalkItem("x", panickingRef{fakeResolvable{ref: "other.yaml#/x", resolved: true}})

	for name, c := range map[string]struct {
		items   iter.Seq[soa.WalkItem]
		site    jsontext.Pointer
		message string
	}{
		"in an object's validation": {slices.Values([]soa.WalkItem{item}), "/x", "object validation panicked"},
		"reading the walk": {func(yield func(soa.WalkItem) bool) {
			if yield(item) {
				panic("walk boom")
			}
		}, "", "walk boom"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			diags := checkReached(t.Context(), pointerAt(0, overlay.Origin{}), c.items, nil, newReached(doc, Options{}, sourceTree{}))

			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, diag.Validation, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, "validation panicked: "+c.message, diags[0].Message)
			assert.Equal(t, c.site, diags[0].Provenance.Pointer)
		})
	}
}

// TestResolve_ASecurityRequirementInAReachedOperationDoesNotPanic pins the
// production path: newReached's WithContextObject(doc) is what keeps
// SecurityRequirement.Validate from panicking (security.go, "OpenAPI is
// required") when the operation it belongs to is reached through an external
// reference rather than declared in the root. Dropping that option is the
// mutation this test must catch, and it must catch it as a validation-panicked
// diagnostic — not as a reference-resolver-panicked one, which would mean the
// wrong barrier (eachReference's, for #565's resolution phase) caught it
// instead.
func TestResolve_ASecurityRequirementInAReachedOperationDoesNotPanic(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": securityRequirementFixtureOther})

	_, diags := loadExternal(t, dir, securityRequirementFixtureRoot, Options{})

	for _, d := range diags {
		assert.NotContains(t, d.Message, "panicked", "%+v", d)
		assert.NotEqual(t, diag.UnresolvedRef, d.Code, "no reference resolver panicked diagnostic: %+v", d)
	}
}

// reachRootFixture and reachOtherFixture are GitHub #545's repro. root.yaml
// reaches every kind of object other.yaml's findings live in:
// a path item (twice, /x and /y, to prove the once behaviour incidentally), an
// operation's own inline parameter, a reused parameter component (twice, to
// prove it again), a response, and a schema. /unreached and the numeric
// literal in P's schema are never reached by anything root.yaml writes.
const reachRootFixture = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x: {$ref: "./other.yaml#/paths/~1x"}
  /y: {$ref: "./other.yaml#/paths/~1x"}
  /z:
    get:
      operationId: getZ
      parameters:
        - $ref: "./other.yaml#/components/parameters/P"
        - $ref: "./other.yaml#/components/parameters/P"
      responses:
        "200": {$ref: "./other.yaml#/components/responses/R"}
components:
  schemas:
    S: {$ref: "./other.yaml#/components/schemas/Bad"}
`

const reachOtherFixture = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      security:
        - nosuch: []
      parameters:
        - {name: q, schema: {type: string}}
        - {$ref: "#/components/parameters/P"}
      responses: {"200": {description: ok}}
  /unreached:
    get:
      operationId: getU
      parameters:
        - {name: u, in: nowhere, schema: {type: string}}
      responses: {"200": {description: ok}}
components:
  parameters:
    P: {name: p, in: sideways, schema: {type: string, minimum: .5}}
  responses:
    R:
      description: ok
      content:
        application/json:
          schema: {type: string, minLength: -1}
  schemas:
    Bad:
      type: object
      minProperties: -3
      deprecated: "yes"
`

// reachWantDiags is the exact diagnostic list
// TestResolve_ValidatesWhatAnExternalReferenceReaches expects, with other.yaml
// at otherPath: resolution's own finding first, then validation's, by site,
// then the repeat /z's list writes by naming P twice.
func reachWantDiags(otherPath string) []ir.Diagnostic {
	return []ir.Diagnostic{
		{
			Severity:   ir.SeverityError,
			Code:       diag.Validation + "/validation-required-field",
			Message:    "`parameter.in` is required, at 10:11 of " + otherPath,
			Provenance: ir.Provenance{Source: 0, Pointer: "/paths/~1x"},
		},
		{
			Severity:   ir.SeverityError,
			Code:       diag.Validation + "/validation-invalid-schema",
			Message:    "schema.minProperties minimum: got -3, want 0, at 31:22 of " + otherPath,
			Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/S"},
		},
		{
			Severity:   ir.SeverityError,
			Code:       diag.Validation + "/validation-type-mismatch",
			Message:    "schema.deprecated expected `boolean`, got `string`, at 32:19 of " + otherPath,
			Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/S"},
		},
		{
			Severity: ir.SeverityError,
			Code:     diag.Validation + "/validation-allowed-values",
			Message: "parameter.in must be one of [`query, querystring, header, path, cookie`], " +
				"at 10:11 of " + otherPath,
			Provenance: ir.Provenance{Source: 0, Pointer: "/paths/~1x"},
		},
		{
			Severity: ir.SeverityError,
			Code:     diag.Validation + "/validation-allowed-values",
			Message: "parameter.in must be one of [`query, querystring, header, path, cookie`], " +
				"at 21:22 of " + otherPath,
			Provenance: ir.Provenance{Source: 0, Pointer: "/paths/~1z/get/parameters/0"},
		},
		{
			Severity:   ir.SeverityError,
			Code:       diag.Validation + "/validation-invalid-schema",
			Message:    "schema.minLength minimum: got -1, want 0, at 27:45 of " + otherPath,
			Provenance: ir.Provenance{Source: 0, Pointer: "/paths/~1z/get/responses/200"},
		},
		{
			Severity:   ir.SeverityError,
			Code:       diag.DuplicateParameter,
			Message:    `parameter "p" in "sideways" repeats the entry at /paths/~1z/get/parameters/0`,
			Provenance: ir.Provenance{Source: 0, Pointer: "/paths/~1z/get/parameters/1"},
		},
	}
}

// TestResolve_ValidatesWhatAnExternalReferenceReaches is GitHub #545's repro:
// compiled as the source, other.yaml raises seven findings, but
// only six are reachable through root.yaml, and only what is reached is
// validated at all. The numeric-literal artifact P's schema would otherwise
// raise (minimum: .5, an artifact numericLiteralArtifact drops everywhere, not
// only for the source) is absent, and so is anything from /unreached, which
// nothing in root.yaml names.
func TestResolve_ValidatesWhatAnExternalReferenceReaches(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": reachOtherFixture})

	_, diags := loadExternal(t, dir, reachRootFixture, Options{})

	if d := cmp.Diff(reachWantDiags(filepath.Join(dir, "other.yaml")), diags); d != "" {
		t.Errorf("diagnostics did not match (-want +got):\n%s", d)
	}
	for _, d := range diags {
		assert.NotContains(t, d.Message, "17:25", "/unreached's own finding must not appear: %+v", d)
		assert.NotContains(t, d.Message, "21:40", "the numeric-literal artifact must not appear: %+v", d)
	}
}

// TestResolve_WhatTheSourcesValidationNeverCheckedIsValidated pins what a $ref
// naming the source's file reaches that the source's own validation never
// checked: raw YAML, such as an extension's value. It is validated as an object
// in another document is, whether the $ref is the source's own or comes back
// into it from another document (GitHub #759), and its finding is placed at the
// node it is about (GitHub #879). A node of the grammar a schema $ref names is
// not read as a schema, so it is reported unresolved and nothing in it is
// validated as one.
func TestResolve_WhatTheSourcesValidationNeverCheckedIsValidated(t *testing.T) {
	t.Parallel()
	const header = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	for _, c := range []struct {
		name  string
		files map[string]string
		root  string
		want  []string
	}{
		{"a schema under an extension", nil, header + `paths: {}
components:
  schemas:
    A: {$ref: 'root.yaml#/x-defs/S'}
x-defs:
  S: {type: string, minLength: -1}
`, []string{"/x-defs/S/minLength openapi/validation/validation-invalid-schema"}},
		{
			"a response under an extension, from another document",
			map[string]string{"other.yaml": "components:\n  responses:\n    Back: {$ref: 'root.yaml#/x-responses/R'}\n"},
			header + `paths:
  /a:
    get:
      responses:
        "200": {$ref: 'other.yaml#/components/responses/Back'}
x-responses:
  R:
    description: ok
    links:
      L: {operationId: a, operationRef: '#/paths/~1a/get'}
`, []string{"/x-responses/R/links/L/operationId openapi/validation/validation-mutually-exclusive-fields"},
		},
		{"a response named as a schema", nil, header + `paths: {}
components:
  schemas:
    S: {$ref: 'root.yaml#/components/responses/R'}
  responses:
    R: {description: ok, minLength: -1}
`, []string{"/components/schemas/S openapi/unresolved-ref"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, diags := loadExternal(t, externalDir(t, c.files), c.root, Options{})

			assert.Empty(t, cmp.Diff(c.want, diagLines(diags)))
		})
	}
}

// TestResolve_ANodeReadAsAnotherKindReportsNothingTheSourceDid pins what a node
// a $ref reads as another kind holds: what the source declares inside it, here
// a header's schema, whose finding the source's own validation reported. That
// finding is reported once, by the source, and not again for the $ref (GitHub
// #759).
func TestResolve_ANodeReadAsAnotherKindReportsNothingTheSourceDid(t *testing.T) {
	t.Parallel()
	_, diags := loadExternal(t, t.TempDir(), `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters: [{$ref: 'root.yaml#/components/headers/H'}]
      responses: {"200": {description: ok}}
components:
  headers:
    H: {name: h, in: query, schema: {type: string, minLength: -1}}
`, Options{})

	assert.Empty(t, cmp.Diff([]string{"/components/headers/H/schema/minLength openapi/validation/validation-invalid-schema"},
		diagLines(diags)), "the source's own finding, once")
}

// TestCheckReached_APanicDroppingWhatTheSourceDeclaresIsReported pins the
// barrier over the second read of the walk, which drops what the source
// declares from what a $ref naming its file reached: a panic there is reported
// at the root, and nothing is validated.
func TestCheckReached_APanicDroppingWhatTheSourceDeclaresIsReported(t *testing.T) {
	t.Parallel()
	got, _ := loadExternal(t, t.TempDir(), `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  responses:
    A: {$ref: 'root.yaml#/x-defs/R'}
x-defs:
  R: {description: ok, links: {L: {operationId: a, operationRef: '#/paths/~1a/get'}}}
`, Options{})
	reads := 0
	items := func(yield func(soa.WalkItem) bool) {
		reads++
		if reads > 1 {
			panic("walk boom")
		}
		for item := range soa.Walk(t.Context(), got.Doc) {
			if !yield(item) {
				return
			}
		}
	}

	diags := checkReached(t.Context(), pointerAt(0, overlay.Origin{}), items, nil, newReached(got.Doc, Options{}, sourceTree{}))

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, "validation panicked: walk boom", diags[0].Message)
	assert.Equal(t, jsontext.Pointer(""), diags[0].Provenance.Pointer)
}

// TestResolve_EveryKindOfObjectIsValidated reaches each kind of object a $ref
// names that the other tests do not, a header, request body, callback,
// example, link and security scheme, each with a defect only its own Validate
// finds. Each finding is reported at its $ref, under the rule and severity the
// library gives it: the callback's is a warning.
func TestResolve_EveryKindOfObjectIsValidated(t *testing.T) {
	t.Parallel()
	root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      requestBody: {$ref: './other.yaml#/components/requestBodies/B'}
      callbacks: {cb: {$ref: './other.yaml#/components/callbacks/C'}}
      responses:
        "200":
          description: ok
          headers: {X: {$ref: './other.yaml#/components/headers/H'}}
          links: {l: {$ref: './other.yaml#/components/links/L'}}
          content: {application/json: {examples: {e: {$ref: './other.yaml#/components/examples/E'}}}}
components:
  securitySchemes:
    s: {$ref: './other.yaml#/components/securitySchemes/S'}
`
	dir := externalDir(t, map[string]string{"other.yaml": `components:
  headers:
    H: {style: form, schema: {type: string}}
  requestBodies:
    B: {content: {application/json: {schema: {minLength: -1}}}}
  callbacks:
    C: {'{$bogus': {get: {responses: {"200": {description: ok}}}}}
  examples:
    E: {value: 1, externalValue: 'https://x.test/e'}
  links:
    L: {operationId: a, operationRef: '#/paths/~1a/get'}
  securitySchemes:
    S: {type: bogus}
`})

	_, diags := loadExternal(t, dir, root, Options{})

	got := make([]string, 0, len(diags))
	for _, d := range diags {
		got = append(got, fmt.Sprintf("%s %s %s", d.Provenance.Pointer, d.Severity, d.Code))
	}
	want := []string{
		"/components/securitySchemes/s error openapi/validation/validation-allowed-values",
		"/paths/~1a/get/callbacks/cb warning openapi/validation/validation-invalid-format",
		"/paths/~1a/get/requestBody error openapi/validation/validation-invalid-schema",
		"/paths/~1a/get/responses/200/content/application~1json/examples/e error " +
			"openapi/validation/validation-mutually-exclusive-fields",
		"/paths/~1a/get/responses/200/headers/X error openapi/validation/validation-allowed-values",
		"/paths/~1a/get/responses/200/links/l error openapi/validation/validation-mutually-exclusive-fields",
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("findings (-want +got):\n%s", d)
	}
}

// TestResolve_AnObjectReachedTwiceIsReportedOnce covers the ways one node can
// be reached more than once: two $refs to one component, a container reached
// beside something inside it, at the lesser $ref and at the greater, and one
// node read as two kinds. Each root declares first the $ref that does not sort
// first, so a report placed in walk order lands at the wrong one. Inside a
// container, an object is validated twice and its finding reported once.
func TestResolve_AnObjectReachedTwiceIsReportedOnce(t *testing.T) {
	t.Parallel()
	compile := func(t *testing.T, other, root string, opts Options) []ir.Diagnostic {
		t.Helper()
		_, diags := loadExternal(t, externalDir(t, map[string]string{"other.yaml": other}), root, opts)
		return diags
	}

	t.Run("two refs to one component report once, at the lesser", func(t *testing.T) {
		t.Parallel()
		other := `openapi: 3.1.0
info: {title: O, version: "1"}
paths: {}
components:
  parameters:
    P: {name: p, in: sideways, schema: {type: string}}
`
		root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    get: {operationId: getB, parameters: [{$ref: "./other.yaml#/components/parameters/P"}], responses: {"200": {description: ok}}}
  /a:
    get: {operationId: getA, parameters: [{$ref: "./other.yaml#/components/parameters/P"}], responses: {"200": {description: ok}}}
`
		diags := compile(t, other, root, Options{})

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/parameters/0"), diags[0].Provenance.Pointer,
			"reported at the lesser $ref, not the one declared first")
	})

	other := `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      parameters:
        - name: q
          in: sideways
          schema: {type: string}
      responses: {"200": {description: ok}}
`
	containedFirst := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    get:
      operationId: getB
      parameters:
        - $ref: "./other.yaml#/paths/~1x/get/parameters/0"
      responses: {"200": {description: ok}}
  /a: {$ref: "./other.yaml#/paths/~1x"}
`

	t.Run("a container at the lesser $ref keeps the finding of what it holds", func(t *testing.T) {
		t.Parallel()
		diags := compile(t, other, containedFirst, Options{})

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1a"), diags[0].Provenance.Pointer,
			"the container's own finding; the parameter it holds draws it again, and that copy is dropped")
	})

	t.Run("what a container holds is charged again", func(t *testing.T) {
		t.Parallel()
		diags := compile(t, nestedOtherFixture(), containedFirst, Options{MaxSourceNodes: 150, MaxAliasSurplus: 1})

		require.Len(t, diags, 2, "the container fits this budget and its parameter, charged again, "+
			"does not: %+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1a"), diags[0].Provenance.Pointer)
		assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)
		assert.Equal(t, jsontext.Pointer("/paths/~1b/get/parameters/0"), diags[1].Provenance.Pointer)
		assert.Equal(t, diag.BudgetExceeded, diags[1].Code)
	})

	t.Run("what a container spans but does not read is validated at its own $ref", func(t *testing.T) {
		t.Parallel()
		other := `paths:
  /x:
    x-shared: {name: q, in: sideways, schema: {type: string}}
    get: {responses: {"200": {description: ok}}}
`
		root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b: {get: {parameters: [{$ref: "./other.yaml#/paths/~1x/x-shared"}], responses: {"200": {description: ok}}}}
  /a: {$ref: "./other.yaml#/paths/~1x"}
`
		diags := compile(t, other, root, Options{})

		require.Len(t, diags, 1, "the path item at /a holds the parameter but never reads it: %+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1b/get/parameters/0"), diags[0].Provenance.Pointer)
		assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)
	})

	t.Run("one node read as two kinds is validated as each", func(t *testing.T) {
		t.Parallel()
		other := "components:\n  responses:\n    R: {description: ok, type: 42, " +
			"content: {application/json: {schema: {minLength: -2}}}}\n"
		root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {responses: {"200": {$ref: "./other.yaml#/components/responses/R"}}}}
components:
  schemas:
    S: {$ref: "./other.yaml#/components/responses/R"}
`
		diags := compile(t, other, root, Options{})

		sites := map[jsontext.Pointer][]string{}
		for _, d := range diags {
			sites[d.Provenance.Pointer] = append(sites[d.Provenance.Pointer], d.Message)
		}
		require.Len(t, sites["/paths/~1a/get/responses/200"], 1, "read as a response: %+v", diags)
		assert.Contains(t, sites["/paths/~1a/get/responses/200"][0], "schema.minLength minimum: got -2")
		assert.NotEmpty(t, sites["/components/schemas/S"], "read as a schema, type: 42 is a finding: %+v", diags)
	})

	t.Run("a contained object at the lesser $ref keeps its finding there", func(t *testing.T) {
		t.Parallel()
		root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b: {$ref: "./other.yaml#/paths/~1x"}
  /a:
    get:
      operationId: getA
      parameters:
        - $ref: "./other.yaml#/paths/~1x/get/parameters/0"
      responses: {"200": {description: ok}}
`
		diags := compile(t, other, root, Options{})

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/parameters/0"), diags[0].Provenance.Pointer,
			"the parameter's own finding; the container draws it again, and that copy is dropped")
	})
}

// TestResolve_ReachedSitesDoNotDependOnDeclarationOrder compiles one source as
// written and with its paths and parameters reversed, and requires the same
// reports. Placed in walk order, a finding moves with the declarations. The
// shapes: a component two paths reach, a container at the lesser $ref and one
// at the greater, and an internal $ref whose chain leaves the source, which
// sorts before the external $ref it chains to.
func TestResolve_ReachedSitesDoNotDependOnDeclarationOrder(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": `paths:
  /x:
    get:
      parameters: [{name: q, in: under, schema: {type: string}}]
      responses: {"200": {description: ok}}
  /y:
    get:
      parameters: [{name: r, in: over, schema: {type: string}}]
      responses: {"200": {description: ok}}
components:
  parameters:
    P: {name: p, in: sideways, schema: {type: string}}
    P2: {in: aslant, schema: {type: string}}
`})
	paths := []string{
		"/b: {get: {parameters: [{$ref: './other.yaml#/components/parameters/P'}], responses: {'200': {description: ok}}}}",
		"/a: {get: {parameters: [{$ref: './other.yaml#/components/parameters/P'}], responses: {'200': {description: ok}}}}",
		"/d: {$ref: './other.yaml#/paths/~1x'}",
		"/c: {get: {parameters: [{$ref: './other.yaml#/paths/~1x/get/parameters/0'}], responses: {'200': {description: ok}}}}",
		"/f: {get: {parameters: [{$ref: './other.yaml#/paths/~1y/get/parameters/0'}], responses: {'200': {description: ok}}}}",
		"/e: {$ref: './other.yaml#/paths/~1y'}",
	}
	parameters := []string{
		"R: {$ref: './other.yaml#/components/parameters/P2'}",
		"Q: {$ref: '#/components/parameters/R'}",
	}
	compile := func(paths, parameters []string) []string {
		src := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  " + strings.Join(paths, "\n  ") +
			"\ncomponents:\n  parameters:\n    " + strings.Join(parameters, "\n    ") + "\n"
		_, diags := loadExternal(t, dir, src, Options{})
		out := make([]string, 0, len(diags))
		for _, d := range diags {
			out = append(out, fmt.Sprintf("%s %s %s", d.Code, d.Provenance.Pointer, d.Message))
		}
		slices.Sort(out)
		return out
	}

	asWritten := compile(paths, parameters)
	slices.Reverse(paths)
	slices.Reverse(parameters)
	reversed := compile(paths, parameters)

	if d := cmp.Diff(asWritten, reversed); d != "" {
		t.Errorf("reports depend on declaration order (-as written +reversed):\n%s", d)
	}
	sites := make([]string, 0, len(asWritten))
	for _, line := range asWritten {
		code, rest, _ := strings.Cut(line, " ")
		site, _, _ := strings.Cut(rest, " ")
		sites = append(sites, code+" "+site)
	}
	assert.Equal(t, []string{
		"openapi/validation/validation-allowed-values /components/parameters/Q",    // P2: Q chains to R's target
		"openapi/validation/validation-allowed-values /paths/~1a/get/parameters/0", // P: the lesser of /a and /b
		"openapi/validation/validation-allowed-values /paths/~1c/get/parameters/0", // q: held by /d, reached at /c
		"openapi/validation/validation-allowed-values /paths/~1e",                  // r: /e holds what /f reaches
		"openapi/validation/validation-required-field /components/parameters/Q",    // P2, as resolution places it
	}, sites)
}

// TestResolve_AChainIsChargedToTheDocumentItEndsIn reaches one object of
// b.yaml directly and a schema inside it through a.yaml. Both are b.yaml's
// nodes, so both are charged to b.yaml, and together they cross a budget
// neither crosses alone. Charged to the first document each $ref names, the
// schema would go to a.yaml and nothing would cross.
func TestResolve_AChainIsChargedToTheDocumentItEndsIn(t *testing.T) {
	t.Parallel()
	root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {parameters: [{name: q, in: query, schema: {$ref: "./a.yaml#/components/schemas/X"}}], responses: {"200": {description: ok}}}}
  /b: {get: {parameters: [{$ref: "./b.yaml#/components/parameters/Y"}], responses: {"200": {description: ok}}}}
`
	dir := externalDir(t, map[string]string{
		"a.yaml": "components:\n  schemas:\n    X: {$ref: \"./b.yaml#/components/parameters/Y/schema\"}\n",
		"b.yaml": "components:\n  parameters:\n    Y: {name: p, in: sideways, schema: " +
			"{type: object, properties: {p1: {type: string}, p2: {type: string}, p3: {type: string}, p4: {type: string}, p5: {type: string}, p6: {type: string}, p7: {type: string}, p8: {type: string}, p9: {type: string}, p10: {type: string}}}}\n",
	})
	bPath := filepath.Join(dir, "b.yaml")

	_, diags := loadExternal(t, dir, root, Options{MaxSourceNodes: 60, MaxAliasSurplus: 1})

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, jsontext.Pointer("/paths/~1b/get/parameters/0"), diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "reach in "+bPath+" span more than the 61 nodes")

	_, diags = loadExternal(t, dir, root, Options{MaxSourceNodes: 120, MaxAliasSurplus: 1})
	require.Len(t, diags, 1, "a budget both fit admits both: %+v", diags)
	assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)
}

// v32OtherFixture and v32RootFixture are a 3.2 root reaching
// two external schemas, one that only trips the wrong-meta-schema artifact
// (Pet's defaultMapping, a 3.2-only discriminator keyword) and one with a
// genuine defect beside it (Broken: {type: 42}) that must survive
// reconciliation.
const v32OtherFixture = `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      type: object
      properties: {petType: {type: string}}
      required: [petType]
      discriminator:
        propertyName: petType
        defaultMapping: '#/components/schemas/Dog'
        mapping: {cat: '#/components/schemas/Cat'}
    Dog: {allOf: [{$ref: '#/components/schemas/Pet'}]}
    Cat: {allOf: [{$ref: '#/components/schemas/Pet'}]}
    Broken: {type: 42}
`

const v32RootFixture = `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S: {$ref: "./other.yaml#/components/schemas/Pet"}
    B: {$ref: "./other.yaml#/components/schemas/Broken"}
`

// TestResolve_AReached32SchemaIsReconciled: Pet's defaultMapping is a 3.2-only
// discriminator keyword the library checks against the 3.1 meta-schema, so S
// must report nothing, while B's genuine type: 42 survives beside it. The
// second case reaches the same keyword nested in a path item, which the
// reconciliation finds through a walk of the object (reachedWalk) rather than
// as the object itself.
func TestResolve_AReached32SchemaIsReconciled(t *testing.T) {
	t.Parallel()

	t.Run("a schema reached directly", func(t *testing.T) {
		t.Parallel()
		dir := externalDir(t, map[string]string{"other.yaml": v32OtherFixture})

		_, diags := loadExternal(t, dir, v32RootFixture, Options{})

		for _, d := range diags {
			assert.Equal(t, jsontext.Pointer("/components/schemas/B"), d.Provenance.Pointer,
				"S's defaultMapping is an artifact and must report nothing: %+v", d)
		}
		assert.NotEmpty(t, diags, "B's type: 42 is a real defect and must survive reconciliation")
	})

	t.Run("the same keyword nested inside a reached path item", func(t *testing.T) {
		t.Parallel()
		other := `openapi: 3.2.0
info: {title: O, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      parameters:
        - name: q
          in: query
          schema:
            type: object
            properties: {petType: {type: string}}
            required: [petType]
            discriminator:
              propertyName: petType
              defaultMapping: '#/components/schemas/Dog'
      responses: {"200": {description: ok}}
components:
  schemas:
    Dog: {type: object}
`
		root := `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /x: {$ref: "./other.yaml#/paths/~1x"}
`
		dir := externalDir(t, map[string]string{"other.yaml": other})

		_, diags := loadExternal(t, dir, root, Options{})

		assert.Empty(t, diags, "the nested defaultMapping is an artifact reachedWalk must reconcile: %+v", diags)
	})
}

// TestResolve_AReached30SchemaIsCheckedAsTheSourcesAre compiles one 3.0 schema
// in the source, reached directly, and reached inside a parameter, and each
// draws only its minLength finding. Validated at 3.0, the one reached directly
// would also draw findings for its type list and examples, which 3.0's
// meta-schema forbids: the source's schemas are checked against 3.1's, and
// reconciling that for 3.0 is a change of its own (metaSchemaReconciledMinor).
func TestResolve_AReached30SchemaIsCheckedAsTheSourcesAre(t *testing.T) {
	t.Parallel()
	const header = "openapi: 3.0.3\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n"
	const body = "{type: [string, 'null'], examples: [a], minLength: -1}"
	for name, c := range map[string]struct{ root, other string }{
		"in the source": {root: header + "  schemas:\n    S: " + body + "\n"},
		"reached directly": {
			root:  header + "  schemas:\n    S: {$ref: './other.yaml#/components/schemas/T'}\n",
			other: "components:\n  schemas:\n    T: " + body + "\n",
		},
		"reached inside a parameter": {
			root:  header + "  parameters:\n    P: {$ref: './other.yaml#/components/parameters/Q'}\n",
			other: "components:\n  parameters:\n    Q: {name: q, in: query, schema: " + body + "}\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := externalDir(t, map[string]string{"other.yaml": c.other})

			_, diags := loadExternal(t, dir, c.root, Options{})

			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, diag.Validation+"/validation-invalid-schema", diags[0].Code)
			assert.Contains(t, diags[0].Message, "schema.minLength minimum: got -1, want 0")
		})
	}
}

// nestedOtherFixture is an external document whose path item holds a parameter
// with an invalid in: sideways and a schema of twenty properties. Reached once
// through the parameter and once through the path item around it, those nodes
// are charged twice, which crosses a budget a single charge does not. Small, a
// separate response with a defect of its own, is reached after that, to pin
// that a document over its budget is not validated further.
func nestedOtherFixture() string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: O, version: \"1\"}\npaths:\n" +
		"  /x:\n    get:\n      operationId: getX\n      parameters:\n" +
		"        - name: q\n          in: sideways\n          schema:\n" +
		"            type: object\n            properties:\n")
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "              p%d: {type: string}\n", i)
	}
	b.WriteString("      responses:\n        \"200\": {description: ok}\n" +
		"components:\n  responses:\n    Small: {description: ok, content: {application/json: {schema: {minLength: -1}}}}\n")
	return b.String()
}

// nestedRootPaths reach nestedOtherFixture's parameter from /a, the path item
// around it from /b, and Small from /c. They are declared last first: in that
// order a walk reaches the path item before the parameter it holds.
var nestedRootPaths = []string{
	`  /c:
    get:
      operationId: getC
      responses:
        "200": {$ref: "./other.yaml#/components/responses/Small"}
`,
	"  /b: {$ref: \"./other.yaml#/paths/~1x\"}\n",
	`  /a:
    get:
      operationId: getA
      parameters:
        - $ref: "./other.yaml#/paths/~1x/get/parameters/0"
      responses: {"200": {description: ok}}
`,
}

// nestedRoot returns a source declaring paths in the order given.
func nestedRoot(paths []string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" + strings.Join(paths, "")
}

// TestResolve_ReachedValidationIsBudgeted runs nestedRootPaths under three
// budgets. At 151 nodes (150 + 1) the parameter's finding is reported once,
// from /a, the path item at /b, which charges those nodes again, is refused
// as budget-exceeded, and Small at /c is not validated, in either declaration
// order. The three total 206 nodes, so 206 (205 + 1) admits all of them where
// MaxSourceNodes alone, 205, would not: that boundary is what shows the alias
// budget's share is counted. An unbounded budget admits them all too.
func TestResolve_ReachedValidationIsBudgeted(t *testing.T) {
	t.Parallel()
	nestedRootFixture := nestedRoot(nestedRootPaths)
	other := nestedOtherFixture()
	dir := externalDir(t, map[string]string{"other.yaml": other})
	otherPath := filepath.Join(dir, "other.yaml")

	t.Run("a tight budget refuses the container after the overlap", func(t *testing.T) {
		t.Parallel()
		_, diags := loadExternal(t, dir, nestedRootFixture, Options{MaxSourceNodes: 150, MaxAliasSurplus: 1})

		require.Len(t, diags, 2, "exactly two: Small's document is already over budget, so neither "+
			"its finding nor a second budget-exceeded is reported: %+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/parameters/0"), diags[0].Provenance.Pointer)
		assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)

		assert.Equal(t, diag.BudgetExceeded, diags[1].Code)
		assert.Equal(t, ir.SeverityError, diags[1].Severity)
		assert.Equal(t, jsontext.Pointer("/paths/~1b"), diags[1].Provenance.Pointer)
		assert.Equal(t, fmt.Sprintf(
			"the objects references reach in %s span more than the 151 nodes a document is validated over; "+
				"its validation stops at this one", otherPath),
			diags[1].Message)

		reversed := slices.Clone(nestedRootPaths)
		slices.Reverse(reversed)
		_, again := loadExternal(t, dir, nestedRoot(reversed), Options{MaxSourceNodes: 150, MaxAliasSurplus: 1})
		if d := cmp.Diff(diags, again); d != "" {
			t.Errorf("the budget is crossed at another object when the paths are reversed (-as written +reversed):\n%s", d)
		}
	})

	for name, opts := range map[string]Options{
		"a budget the three fit admits them all": {MaxSourceNodes: 205, MaxAliasSurplus: 1},
		"an unbounded budget admits them all":    {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags := loadExternal(t, dir, nestedRootFixture, opts)

			require.Len(t, diags, 2, "%+v", diags)
			assert.Equal(t, jsontext.Pointer("/paths/~1a/get/parameters/0"), diags[0].Provenance.Pointer)
			assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)
			assert.Equal(t, jsontext.Pointer("/paths/~1c/get/responses/200"), diags[1].Provenance.Pointer)
			assert.Equal(t, diag.Validation+"/validation-invalid-schema", diags[1].Code)
		})
	}
}

// respelledOther is an external document with an anchored get whose parameter
// is itself invalid, a put with an invalid schema, and an /unreached path.
const respelledOther = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    get: &g
      operationId: EXTGET
      parameters:
        - {name: q, in: sideways, schema: {type: string}}
      responses: {"200": {description: ok}}
    put:
      operationId: EXTPUT
      parameters:
        - {name: p, schema: {type: string, minLength: -1}}
      responses:
        "200": {description: ok}
        "404": &r {description: EXTANCHOREDRESP}
  /unreached:
    get:
      operationId: getU
      parameters:
        - {name: u, in: nowhere, schema: {type: string}}
      responses: {"200": {description: ok}}
`

// respelledRoot is a source whose two paths reference other.yaml's anchored
// path item over HTTP, spelling its URL with scheme.
func respelledRoot(scheme, host string) string {
	url := scheme + "://" + host + "/other.yaml#/paths/~1x"
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /x: {$ref: \"" + url + "\"}\n  /y: {$ref: \"" + url + "\"}\n"
}

// TestResolve_ValidatesOnceAfterARebuild: an upper-cased scheme forces GitHub
// #538's rebuild, since the resolver's own parse of a mis-keyed document skips
// the anchored get. /x's 8:25 finding, inside that get, exists only in the
// recovered document, and validateReached, run once on the document resolve
// returns, must find it exactly once. other.yaml is served over HTTP so the
// spelling is the one difference between the runs, which must report the same
// diagnostics, the document's name aside: each finding once, at /x.
func TestResolve_ValidatesOnceAfterARebuild(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(respelledOther))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	run := func(scheme string) []ir.Diagnostic {
		src := compilers.Source{Path: "root.yaml", Data: []byte(respelledRoot(scheme, host))}
		_, diags, err := Load(t.Context(), 0, src, Options{AllowExternalRefs: true})
		require.NoError(t, err)
		return diags
	}

	notRebuilt := run("http")
	rebuilt := run("HTTP")

	respelled := cmp.Transformer("respelled", func(msg string) string {
		return strings.ReplaceAll(msg, "HTTP://", "http://")
	})
	if d := cmp.Diff(notRebuilt, rebuilt, respelled); d != "" {
		t.Errorf("the diagnostics must be identical whether or not a rebuild ran (-not-rebuilt +rebuilt):\n%s", d)
	}
	for _, d := range rebuilt {
		assert.Equal(t, jsontext.Pointer("/paths/~1x"), d.Provenance.Pointer,
			"every finding is reported once, at the first $ref that reaches it: %+v", d)
	}

	count8_25 := 0
	for _, d := range rebuilt {
		if strings.Contains(d.Message, "8:25") {
			count8_25++
			assert.Equal(t, jsontext.Pointer("/paths/~1x"), d.Provenance.Pointer)
		}
	}
	assert.Equal(t, 1, count8_25, "the recovered get's own finding, exactly once: %+v", rebuilt)

	for _, d := range rebuilt {
		assert.NotContains(t, d.Message, "in: nowhere", "/unreached must not be validated: %+v", d)
	}
}

// markerPosition returns the line and column, both counted from 1, at which
// marker starts in spec. The marker must occur once, so a fixture edited to
// repeat it fails here rather than naming the wrong node.
func markerPosition(t *testing.T, spec, marker string) (line, col int) {
	t.Helper()
	require.Equal(t, 1, strings.Count(spec, marker), "the marker %q must occur once in the fixture", marker)
	at := strings.Index(spec, marker)
	return 1 + strings.Count(spec[:at], "\n"), at - strings.LastIndex(spec[:at], "\n")
}

// inSourceFinding is one finding on a node of the source: the pointer it is
// placed at, its code and what it says, and at, the snippet of the fixture the
// node starts at, whose line and column it carries beside the pointer.
// "{line}" in text stands for that line.
type inSourceFinding struct {
	pointer, code, text, at string
}

// render spells the finding as renderDiags spells a diagnostic.
func (f inSourceFinding) render(t *testing.T, spec string) string {
	t.Helper()
	line, col := markerPosition(t, spec, f.at)
	text := strings.ReplaceAll(f.text, "{line}", fmt.Sprint(line))
	return fmt.Sprintf("%s %d:%d %s %s", f.pointer, line, col, f.code, text)
}

// renderDiags spells each diagnostic as its pointer, position, code and
// message, sorted.
func renderDiags(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		at := d.Provenance.Position
		out = append(out, fmt.Sprintf("%s %d:%d %s %s", d.Provenance.Pointer, at.Line, at.Column, d.Code, d.Message))
	}
	slices.Sort(out)
	return out
}

// reachedInSourceFixture is GitHub #872's reproducer: three security schemes
// and a parameter, each written under an x-* key and reached by a $ref, each
// missing what its own kind requires.
const reachedInSourceFixture = `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - key: []
  - oidc: []
  - oauth: []
paths:
  /x:
    get:
      parameters:
        - $ref: '#/x-params/q'
      responses: {"200": {description: ok}}
x-schemes:
  key: {type: apiKey, in: header}
  oidc: {type: openIdConnect}
  oauth: {type: oauth2}
x-params:
  q: {name: q, schema: {type: string}}
components:
  securitySchemes:
    key: {$ref: '#/x-schemes/key'}
    oidc: {$ref: '#/x-schemes/oidc'}
    oauth: {$ref: '#/x-schemes/oauth'}
`

// TestResolve_WhatAReferenceReachesInTheSourceIsValidated pins that an object a
// $ref reaches in the source is validated, whether or not external references
// are read (GitHub #872): one under an x-* key, which the source's model never
// declares, whether reached by a $ref, a discriminator mapping entry, or a $ref
// inside raw YAML. Each finding is placed as the source's own are, at the
// pointer and position of the node it is about, not at the $ref (GitHub
// #879), resolution's and validation's alike. An object the model declares is
// reported once, by the source; a valid one reports nothing.
func TestResolve_WhatAReferenceReachesInTheSourceIsValidated(t *testing.T) {
	t.Parallel()
	const (
		required = "openapi/validation/validation-required-field"
		allowed  = "openapi/validation/validation-allowed-values"
		invalid  = "openapi/validation/validation-invalid-schema"
		mismatch = "openapi/validation/validation-type-mismatch"
		minimum  = "schema.minLength minimum: got -1, want 0"
		inValues = "parameter.in must be one of [`query, querystring, header, path, cookie`]"
	)
	const header = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	byOperation := header + `paths:
  /x:
    get:
      parameters:
        - $ref: '%s'
      responses: {"200": {description: ok}}
`
	for _, c := range []struct {
		name string
		spec string
		want []inSourceFinding
	}{
		{"GitHub #872's reproducer", reachedInSourceFixture, []inSourceFinding{
			{"/x-params/q", required, "`parameter.in` is required", "{name: q"},
			{"/x-params/q", allowed, inValues, "{name: q"},
			{"/x-schemes/key", required, "`securityScheme.name` is required for type=apiKey", "{type: apiKey"},
			{"/x-schemes/oidc", required,
				"`securityScheme.openIdConnectUrl` is required for type=openIdConnect", "{type: openIdConnect}"},
			{"/x-schemes/oauth", required, "`securityScheme.flows` is required for type=oauth2", "{type: oauth2}"},
		}},
		{
			"a schema only a discriminator mapping entry reaches",
			mappingSpec(petMapping("x: '#/x-lib/Bad'"), "x-lib:\n  Bad: {type: string, minLength: -1}\n"),
			[]inSourceFinding{{"/x-lib/Bad/minLength", invalid, minimum, "-1"}},
		},
		{
			"a schema a $ref inside raw YAML reaches",
			mappingSpec("    S: {$ref: '#/x-lib/A'}\n",
				"x-lib:\n  A: {type: object, properties: {b: {$ref: '#/x-lib/B'}}}\n  B: {type: string, minLength: -1}\n"),
			[]inSourceFinding{{"/x-lib/B/minLength", invalid, minimum, "-1"}},
		},
		{
			"a type mismatch is told by resolution and by validation",
			mappingSpec("    S: {$ref: '#/x-lib/A'}\n", "x-lib:\n  A: {type: string, minLength: abc}\n"),
			[]inSourceFinding{
				{"/x-lib/A/minLength", mismatch, "schema.minLength line {line}: cannot unmarshal !!str `abc` into int64", "abc"},
				{"/x-lib/A", mismatch, "schema.minLength expected `integer`, got `string`", "{type: string, minLength: abc}"},
			},
		},
		{
			"a declared component is reported by the source alone",
			fmt.Sprintf(byOperation, "#/components/parameters/P") +
				"components:\n  parameters:\n    P: {name: p, in: sideways, schema: {type: string}}\n",
			[]inSourceFinding{{"/components/parameters/P/in", allowed, inValues, "sideways"}},
		},
		{
			"a valid object under an extension reports nothing",
			fmt.Sprintf(byOperation, "#/x-params/q") +
				"x-params:\n  q: {name: q, in: query, schema: {type: string}}\n",
			nil,
		},
	} {
		for _, external := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, external references %t", c.name, external), func(t *testing.T) {
				t.Parallel()
				want := make([]string, 0, len(c.want))
				for _, f := range c.want {
					want = append(want, f.render(t, c.spec))
				}
				slices.Sort(want)

				_, diags := loadTargets(t, "spec.yaml", c.spec, external)

				assert.Empty(t, cmp.Diff(want, renderDiags(diags)))
			})
		}
	}
}

// TestResolve_InSourceReachedSitesDoNotDependOnDeclarationOrder pins that an
// object in the source reached from several places is reported once, at the
// node the finding is about rather than at any of them, whatever order they
// and the object are declared in. The places are $refs, discriminator mapping
// entries, and a $ref inside raw YAML; each case is compiled as written and
// then reversed, with the object moved from after the components to before
// them.
func TestResolve_InSourceReachedSitesDoNotDependOnDeclarationOrder(t *testing.T) {
	t.Parallel()
	const bad = "  Bad: {type: string, minLength: -1}\n"
	const lib = "x-lib:\n" + bad
	ref := func(name string) string { return "    " + name + ": {$ref: '#/x-lib/Bad'}\n" }
	holder := func(name, mapping string) string {
		return "    " + name + ": {type: object, discriminator: {propertyName: k, mapping: {" + mapping + "}}}\n"
	}
	for _, c := range []struct {
		name    string
		lib     string
		entries []string
	}{
		{"two $refs and an entry, the least a $ref", lib,
			[]string{ref("A"), ref("B"), holder("Pet", "x: '#/x-lib/Bad'")}},
		{"two $refs and an entry, the least an entry", lib,
			[]string{holder("Alpha", "x: '#/x-lib/Bad'"), ref("Beta"), ref("Gamma")}},
		{"two entries of one mapping and a $ref", lib,
			[]string{holder("Alpha", "y: '#/x-lib/Bad', x: '#/x-lib/Bad'"), ref("Beta")}},
		{"a $ref and a $ref inside raw YAML", "x-lib:\n  A: {type: object, properties: {b: {$ref: '#/x-lib/Bad'}}}\n" + bad,
			[]string{"    S: {$ref: '#/x-lib/A'}\n", ref("T")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			compile := func(entries []string, libFirst bool) []string {
				const header = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
				components := "components:\n  schemas:\n" + strings.Join(entries, "")
				spec := header + components + c.lib
				if libFirst {
					spec = header + c.lib + components
				}
				_, diags := loadTargets(t, "spec.yaml", spec, false)
				return diagLines(diags)
			}
			want := []string{"/x-lib/Bad/minLength openapi/validation/validation-invalid-schema"}

			backwards := slices.Clone(c.entries)
			slices.Reverse(backwards)

			asWritten := compile(c.entries, false)
			reversed := compile(backwards, true)

			assert.Empty(t, cmp.Diff(want, asWritten), "as written")
			assert.Empty(t, cmp.Diff(want, reversed), "reversed")
		})
	}
}

// TestResolve_AnObjectTheSourceDeclaresIsNotChargedToTheBudget pins that what a
// $ref reaches in the source is charged to the validation budget only when the
// source's own validation did not cover it. A parameter and the schema inside
// it, both reached, fit a budget of the source's own size when the model
// declares them, as it has validated them already; written under an x-* key,
// the same two overlap, are charged twice, and cross it.
func TestResolve_AnObjectTheSourceDeclaresIsNotChargedToTheBudget(t *testing.T) {
	t.Parallel()
	var props []string
	for i := 1; i <= 20; i++ {
		props = append(props, fmt.Sprintf("p%d: {type: string}", i))
	}
	param := "{name: q, in: query, schema: {type: object, properties: {" + strings.Join(props, ", ") + "}}}"
	const header = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	const paths = "paths:\n  /a:\n    get:\n      parameters:\n        - $ref: '%s'\n      responses: {\"200\": {description: ok}}\n"
	declared := header + fmt.Sprintf(paths, "#/components/parameters/P") +
		"components:\n  schemas:\n    S: {$ref: '#/components/parameters/P/schema'}\n  parameters:\n    P: " + param + "\n"
	undeclared := header + fmt.Sprintf(paths, "#/x-lib/P") +
		"components:\n  schemas:\n    S: {$ref: '#/x-lib/P/schema'}\nx-lib:\n  P: " + param + "\n"
	// load compiles spec under a budget of its own size, and returns the budget
	// a document's validation is held to: that size and the alias surplus.
	load := func(t *testing.T, spec string) (diags []ir.Diagnostic, limit int) {
		t.Helper()
		root, _, err := decodeStream([]byte(spec))
		require.NoError(t, err)
		nodes := nodeCount(root)
		opts := Options{MaxSourceNodes: nodes, MaxAliasSurplus: 1}
		got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, opts)
		require.NoError(t, err)
		require.NotNil(t, got, "%+v", diags)
		return diags, nodes + 1
	}

	t.Run("a declared parameter and its schema", func(t *testing.T) {
		t.Parallel()
		diags, _ := load(t, declared)

		assert.Empty(t, diags)
	})
	t.Run("the same two under an extension", func(t *testing.T) {
		t.Parallel()
		diags, limit := load(t, undeclared)

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
		assert.Equal(t, ir.SeverityError, diags[0].Severity)
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/parameters/0"), diags[0].Provenance.Pointer)
		assert.Contains(t, diags[0].Message, fmt.Sprintf("span more than the %d nodes", limit))
	})
}

// TestResolve_AReachedObjectIsHeldToTheLaterVersionTable pins that an object
// a $ref reaches in the source outside its model draws what the same object
// written inline draws in an older document: a warning for each construct a
// later minor added, at the pointer it is written at, even beneath a path
// item's method or a callback's expression, with the library's refusal of it
// taken over, and an additionalOperations' operations checked. An object
// reached both itself and inside another reached object draws each warning
// once. A 3.2 document draws none; another document's object keeps the
// library's own reading (GitHub #839).
func TestResolve_AReachedObjectIsHeldToTheLaterVersionTable(t *testing.T) {
	t.Parallel()
	const later = " openapi/later-version-construct"
	pet := "x-lib:\n  Pet: {type: object, discriminator: {propertyName: k, defaultMapping: '#/components/schemas/Cat'}}\n"
	cases := []struct {
		name, version, body string
		want                []string
	}{
		{"a schema keyword, its refusal taken over", "3.1.0",
			"paths: {}\ncomponents:\n  schemas:\n    Ref: {$ref: '#/x-lib/Pet'}\n    Cat: {type: object}\n" + pet,
			[]string{"/x-lib/Pet/discriminator/defaultMapping" + later}},
		{"a response field", "3.1.0",
			"paths: {}\ncomponents:\n  responses:\n    R: {$ref: '#/x-responses/R'}\nx-responses:\n  R: {description: ok, summary: S}\n",
			[]string{"/x-responses/R/summary" + later}},
		{"a security scheme field", "3.1.0",
			"paths: {}\ncomponents:\n  securitySchemes:\n    s: {$ref: '#/x-schemes/s'}\nx-schemes:\n" +
				"  s: {type: http, scheme: basic, deprecated: true}\n",
			[]string{"/x-schemes/s/deprecated" + later}},
		{"an example field", "3.1.0",
			"paths:\n  /a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n" +
				"          content: {application/json: {examples: {e: {$ref: '#/x-examples/E'}}}}\n" +
				"x-examples:\n  E: {dataValue: 1}\n",
			[]string{"/x-examples/E/dataValue" + later}},
		{"a path item's additionalOperations, its operations checked", "3.1.0",
			"paths:\n  /a: {$ref: '#/x-paths/A'}\nx-paths:\n  A:\n    additionalOperations:\n" +
				"      PURGE: {parameters: [{name: q, in: sideways, schema: {type: string}}], " +
				"responses: {\"200\": {description: ok}}}\n",
			[]string{"/x-paths/A/additionalOperations" + later,
				"/x-paths/A/additionalOperations/PURGE/parameters/0/in openapi/validation/validation-allowed-values"}},
		{"an operation of a path item, under its method", "3.1.0",
			"paths:\n  /a: {$ref: '#/x-paths/A'}\nx-paths:\n  A:\n" +
				"    get: {responses: {\"200\": {description: ok, summary: S}}}\n",
			[]string{"/x-paths/A/get/responses/200/summary" + later}},
		{"an operation of a callback, under its expression", "3.1.0",
			"paths:\n  /a:\n    get:\n      callbacks: {cb: {$ref: '#/x-cb/C'}}\n" +
				"      responses: {\"200\": {description: ok}}\nx-cb:\n  C:\n" +
				"    '{$request.body#/u}': {post: {responses: {\"200\": {description: ok, summary: S}}}}\n",
			[]string{"/x-cb/C/{$request.body#~1u}/post/responses/200/summary" + later}},
		{"an object reached inside another reached one, warned once", "3.1.0",
			"paths: {}\ncomponents:\n  schemas:\n    Ref: {$ref: '#/x-lib/Pet'}\n" +
				"    Inner: {$ref: '#/x-lib/Pet/properties/kind'}\n    Cat: {type: object}\n" +
				"x-lib:\n  Pet: {type: object, properties: {kind: {type: object, discriminator: " +
				"{propertyName: k, defaultMapping: '#/components/schemas/Cat'}}}}\n",
			[]string{"/x-lib/Pet/properties/kind/discriminator/defaultMapping" + later}},
		{"a 3.2 document, which added them", "3.2.0",
			"paths: {}\ncomponents:\n  schemas:\n    Ref: {$ref: '#/x-lib/Pet'}\n    Cat: {type: object}\n" +
				"  responses:\n    R: {$ref: '#/x-responses/R'}\n" + pet + "x-responses:\n  R: {description: ok, summary: S}\n",
			[]string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: " + c.version + "\ninfo: {title: T, version: \"1\"}\n" + c.body
			_, diags := loadTargets(t, "spec.yaml", spec, false)
			assert.Empty(t, cmp.Diff(c.want, diagLines(diags)), "%+v", diags)
		})
	}

	t.Run("another document's object keeps the library's reading", func(t *testing.T) {
		t.Parallel()
		dir := externalDir(t, map[string]string{"other.yaml": pet})
		_, diags := loadExternal(t, dir, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"+
			"components:\n  schemas:\n    Ref: {$ref: 'other.yaml#/x-lib/Pet'}\n    Cat: {type: object}\n", Options{})
		assert.Empty(t, cmp.Diff([]string{"/components/schemas/Ref openapi/validation/validation-invalid-schema"},
			diagLines(diags)), "%+v", diags)
	})
}
