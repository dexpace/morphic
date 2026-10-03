package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// reachedFake is a minimal resolvable, for the branches of eachReached and
// reachedObject a real document cannot drive on its own: no real Referenced*
// type is ever unresolved-and-external at once with a chosen URI, and none
// omits GetObjectAny.
type reachedFake struct {
	ref        references.Reference
	isRef      bool
	isResolved bool
}

func (f reachedFake) IsReference() bool                  { return f.isRef }
func (f reachedFake) IsResolved() bool                   { return f.isResolved }
func (f reachedFake) GetReference() references.Reference { return f.ref }
func (f reachedFake) Resolve(context.Context, references.ResolveOptions) ([]error, error) {
	return nil, nil
}

// reachedFakeWalkItem returns a soa.WalkItem whose Match hands model to whichever of
// matcher's callbacks a caller supplies, at loc.
func reachedFakeWalkItem(model any, loc soa.Locations) soa.WalkItem {
	return soa.WalkItem{
		Location: loc,
		Match: func(m soa.Matcher) error {
			if m.Any == nil {
				return nil
			}
			return m.Any(model)
		},
	}
}

// reachedFakeWalkItems adapts a fixed slice of items to the iter.Seq soa.Walk
// produces, for a caller that wants to hand eachReached a walk it controls.
func reachedFakeWalkItems(items ...soa.WalkItem) func(func(soa.WalkItem) bool) {
	return func(yield func(soa.WalkItem) bool) {
		for _, it := range items {
			if !yield(it) {
				return
			}
		}
	}
}

// strPtr is a *string literal helper for building soa.Locations by hand.
func strPtr(s string) *string { return &s }

// writeFiles writes each of files (name -> content) under dir.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		writeFile(t, dir, name, content)
	}
}

// loadExternal loads root from rootPath (inside a t.TempDir the caller
// populated) with external references allowed, and requires it to succeed.
func loadExternal(t *testing.T, rootPath, root string, opts Options) (*Document, []ir.Diagnostic) {
	t.Helper()
	opts.AllowExternalRefs = true
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: rootPath, Data: []byte(root)}, opts)
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)
	return got, diags
}

// TestSpanned covers spanned's three properties: it counts every node under
// root, an alias counts as a copy of what it names, and a limit stops the
// count before the whole tree — proportionally to what covered records, since
// covered is the same walk's bookkeeping. It has no case for a nil node: see
// spanned's own doc comment for why yaml.v3 never gives it one to skip.
func TestSpanned(t *testing.T) {
	t.Parallel()

	t.Run("counts every node under root", func(t *testing.T) {
		t.Parallel()
		root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode}, {Kind: yaml.ScalarNode},
		}}
		covered := map[*yaml.Node]bool{}
		assert.Equal(t, 3, spanned(root, -1, covered), "the mapping plus its two scalar children")
		assert.Len(t, covered, 3)
	})

	t.Run("an alias counts its target as a copy", func(t *testing.T) {
		t.Parallel()
		target := &yaml.Node{Kind: yaml.ScalarNode}
		alias := &yaml.Node{Kind: yaml.AliasNode, Alias: target}
		root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{alias}}
		covered := map[*yaml.Node]bool{}
		assert.Equal(t, 3, spanned(root, -1, covered), "root, the alias node itself, and the target it names")
		assert.True(t, covered[target], "the alias's target is recorded as covered too")
	})

	t.Run("a limit stops the count before the whole tree", func(t *testing.T) {
		t.Parallel()
		root := &yaml.Node{Kind: yaml.MappingNode}
		for range 5 {
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode})
		}
		covered := map[*yaml.Node]bool{}
		got := spanned(root, 3, covered)
		assert.Less(t, got, 6, "the unbounded count is 6: root plus five children")
		assert.Equal(t, got, len(covered), "covered records exactly what the stopped count spanned")
	})
}

// TestReached_DocumentFallsBackToTheURIWhenResolutionFails covers document's
// error arm: a reference's document key is normally the resolver's own
// absolute spelling, but a reference the resolver could not place — here
// because v.path itself cannot be parsed as a reference at all — is charged
// under its own URI instead, so it still gets a budget of its own rather than
// being silently uncharged.
func TestReached_DocumentFallsBackToTheURIWhenResolutionFails(t *testing.T) {
	t.Parallel()
	v := &reached{path: "\x00not a valid target location"}

	got := v.document(reachedFake{isRef: true, isResolved: true, ref: "other.yaml#/a"})

	assert.Equal(t, "other.yaml", got)
}

// reachedFixtureOther and reachedFixtureRoot exercise reachedObject's outcomes
// through a real, resolved document: a schema reference to an object (ok), a
// schema reference to a boolean (nothing but its value), the default arm's
// success path for a non-schema reference (a path item), and a path item whose
// chain stops at a pointer that names nothing.
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
`

// reachedFixtureDoc loads reachedFixtureRoot/reachedFixtureOther and returns
// the resolved document, for the reachedObject/validateObject/reachedWalk unit
// tests that read specific resolved references off it.
func reachedFixtureDoc(t *testing.T) *soa.OpenAPI {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"other.yaml": reachedFixtureOther})
	rootPath := filepath.Join(dir, "root.yaml")
	writeFiles(t, dir, map[string]string{"root.yaml": reachedFixtureRoot})
	got, _ := loadExternal(t, rootPath, reachedFixtureRoot, Options{})
	return got.Doc
}

// TestReachedObject covers every outcome of reachedObject: a schema reference
// resolving to an object, one resolving to a boolean (nothing to validate), the
// default arm's success path for a plain reference, a chain that ends on no
// object, and a resolvable that cannot say what it names, which no real
// document produces, so a fake drives it.
func TestReachedObject(t *testing.T) {
	t.Parallel()
	doc := reachedFixtureDoc(t)

	t.Run("a schema reference to an object", func(t *testing.T) {
		t.Parallel()
		ref, ok := doc.Components.Schemas.Get("S")
		require.True(t, ok)
		obj, node, ok := reachedObject(ref)
		assert.True(t, ok)
		assert.NotNil(t, obj)
		require.NotNil(t, node)
		assert.Equal(t, yaml.MappingNode, node.Kind)
	})

	t.Run("a schema reference to a boolean has nothing but its value", func(t *testing.T) {
		t.Parallel()
		ref, ok := doc.Components.Schemas.Get("Bool")
		require.True(t, ok)
		obj, node, ok := reachedObject(ref)
		assert.False(t, ok)
		assert.Nil(t, obj)
		assert.Nil(t, node)
	})

	t.Run("the default arm succeeds for a plain reference", func(t *testing.T) {
		t.Parallel()
		ref, ok := doc.Paths.Get("/x")
		require.True(t, ok)
		obj, node, ok := reachedObject(ref)
		assert.True(t, ok)
		assert.Same(t, ref.GetObject(), obj)
		assert.Same(t, ref.GetObject().GetRootNode(), node)
	})

	t.Run("a chain that stops at a pointer naming nothing ends on no object", func(t *testing.T) {
		t.Parallel()
		ref, ok := doc.Paths.Get("/stops")
		require.True(t, ok)
		require.True(t, ref.IsResolved(), "its first hop resolved; the second did not")
		obj, node, ok := reachedObject(ref)
		assert.False(t, ok)
		assert.Nil(t, obj)
		assert.Nil(t, node)
	})

	t.Run("a resolvable that cannot say what it names names nothing to validate", func(t *testing.T) {
		t.Parallel()
		_, _, ok := reachedObject(reachedFake{isRef: true, isResolved: true, ref: "other.yaml#/x"})
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
// reachedWalk and hopDocuments switch on already exists, wired together.
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
// the six concrete kinds it walks through a synthetic Reference wrapper (see
// reachedWalk's own doc comment for why a wrapper rather than the bare type),
// and the default arm for a kind it does not reconcile schemas in at all
// (an example, a link, a security scheme).
func TestReachedWalk(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("a schema walks through ConcreteToReferenceable", func(t *testing.T) {
		t.Parallel()
		doc := reachedFixtureDoc(t)
		ref, ok := doc.Components.Schemas.Get("S")
		require.True(t, ok)
		obj, _, ok := reachedObject(ref)
		require.True(t, ok)
		assert.GreaterOrEqual(t, countSchemas(ctx, obj), 2, "the schema itself and its inner property")
	})

	kinds := aliasKindsResolved(t)

	// aliasKindsFixture's own path item and response nest every field behind a
	// further $ref (proving hopDocuments' dispatch is what that fixture is
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

	t.Run("the default arm holds nothing to reconcile", func(t *testing.T) {
		t.Parallel()
		for _, kind := range []string{"example", "link", "securityScheme"} {
			assert.Zero(t, countSchemas(ctx, kinds[kind]), "%s holds no schema", kind)
		}
		assert.Zero(t, countSchemas(ctx, 42), "an unrecognized type is the same default arm")
	})
}

// TestEachReached drives eachReached's own filtering and panic barrier
// directly, with synthetic walk items: a real document cannot produce a model
// that is unresolved-and-visited, or make visit itself fail or panic on
// command.
func TestEachReached(t *testing.T) {
	t.Parallel()

	t.Run("visit's error stops the walk and is returned with its site", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		item := reachedFakeWalkItem(reachedFake{isRef: true, isResolved: true, ref: "other.yaml#/x"},
			soa.Locations{{ParentField: "paths"}, {ParentKey: strPtr("x")}})
		visited := 0
		site, err := eachReached(reachedFakeWalkItems(item), func(jsontext.Pointer, resolvable) error {
			visited++
			return boom
		})
		assert.Equal(t, 1, visited)
		assert.ErrorIs(t, err, boom)
		assert.Equal(t, jsontext.Pointer("/paths/x"), site)
	})

	t.Run("a visit that panics is reported at its site", func(t *testing.T) {
		t.Parallel()
		item := reachedFakeWalkItem(reachedFake{isRef: true, isResolved: true, ref: "other.yaml#/x"},
			soa.Locations{{ParentField: "z"}})
		site, err := eachReached(reachedFakeWalkItems(item), func(jsontext.Pointer, resolvable) error {
			panic("kaboom")
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kaboom")
		assert.Equal(t, jsontext.Pointer("/z"), site)
	})

	t.Run("a walk that panics between items gives site empty", func(t *testing.T) {
		t.Parallel()
		first := reachedFakeWalkItem(reachedFake{isRef: true, isResolved: true, ref: "other.yaml#/x"},
			soa.Locations{{ParentField: "z"}})
		items := func(yield func(soa.WalkItem) bool) {
			if !yield(first) {
				return
			}
			panic("walk boom")
		}
		visited := 0
		site, err := eachReached(items, func(jsontext.Pointer, resolvable) error {
			visited++
			return nil
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "walk boom")
		assert.Equal(t, jsontext.Pointer(""), site, "site was reset after the first item's successful visit")
		assert.Equal(t, 1, visited)
	})

	t.Run("a model that is not a resolved external reference is not visited", func(t *testing.T) {
		t.Parallel()
		cases := map[string]reachedFake{
			"not a reference":      {isRef: false, isResolved: true, ref: "other.yaml#/x"},
			"unresolved":           {isRef: true, isResolved: false, ref: "other.yaml#/x"},
			"internal (empty URI)": {isRef: true, isResolved: true, ref: "#/internal"},
		}
		items := make([]soa.WalkItem, 0, len(cases))
		for _, c := range cases {
			items = append(items, reachedFakeWalkItem(c, nil))
		}
		visited := 0
		_, err := eachReached(reachedFakeWalkItems(items...), func(jsontext.Pointer, resolvable) error {
			visited++
			return nil
		})
		require.NoError(t, err)
		assert.Zero(t, visited, "none of not-a-reference, unresolved, or internal reaches visit")
	})
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

// reachedFakePanicObject implements just enough of a resolved object to reach
// validateObject's dispatch — GetRootNode for reachedObject's default arm, and
// Validate for validateObject's — and panics from Validate on command, driving
// checkReached's panic barrier without depending on any particular library
// fault to be the one available to trigger it.
type reachedFakePanicObject struct{}

func (reachedFakePanicObject) GetRootNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode}
}

func (reachedFakePanicObject) Validate(context.Context, ...validation.Option) []error {
	panic("object validation panicked")
}

// reachedFakePanicHolder is a reachedFake whose GetObjectAny returns a
// reachedFakePanicObject, so reachedObject succeeds and validateObject then
// panics on it.
type reachedFakePanicHolder struct{ reachedFake }

func (f reachedFakePanicHolder) GetObjectAny() any { return reachedFakePanicObject{} }

func (f reachedFakePanicHolder) GetRootNode() *yaml.Node {
	return reachedFakePanicObject{}.GetRootNode()
}

// TestValidateReached_APanicIsReportedAtTheRef pins how the panic barrier turns
// a recovered panic into a diagnostic: openapi/validation, "validation
// panicked", at the $ref that reached the panicking object. It drives
// checkReached with a real newReached and a walk yielding one resolved external
// reference whose object panics on Validate. No real document reaches this:
// the root document newReached passes keeps a security requirement from
// panicking (TestResolve_ASecurityRequirementInAReachedOperationDoesNotPanic).
func TestValidateReached_APanicIsReportedAtTheRef(t *testing.T) {
	t.Parallel()
	doc, valErrs := parseSpec(t, minimal31)
	require.Empty(t, valErrs)
	checks := newReached(doc, "root.yaml", Options{})
	at := pointerAt(0, overlay.Origin{})

	r := reachedFakePanicHolder{reachedFake{isRef: true, isResolved: true, ref: "other.yaml#/x"}}
	items := reachedFakeWalkItems(reachedFakeWalkItem(r, soa.Locations{{ParentField: "paths"}, {ParentKey: strPtr("x")}}))

	diags := checkReached(t.Context(), at, items, checks)

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.Validation, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "validation panicked")
	assert.Contains(t, diags[0].Message, "object validation panicked")
	assert.Equal(t, jsontext.Pointer("/paths/x"), diags[0].Provenance.Pointer, "reported at the $ref that reached the panicking object")
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
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"other.yaml": securityRequirementFixtureOther})
	rootPath := filepath.Join(dir, "root.yaml")
	writeFiles(t, dir, map[string]string{"root.yaml": securityRequirementFixtureRoot})

	_, diags := loadExternal(t, rootPath, securityRequirementFixtureRoot, Options{})

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

// m3WantDiags is the exact diagnostic list TestResolve_ValidatesWhatAnExternalReferenceReaches
// expects, in walk order, with other.yaml at otherPath.
func m3WantDiags(otherPath string) []ir.Diagnostic {
	return []ir.Diagnostic{
		{
			Severity:   ir.SeverityError,
			Code:       diag.Validation + "/validation-required-field",
			Message:    "`parameter.in` is required, at 10:11 of " + otherPath,
			Provenance: ir.Provenance{Source: 0, Pointer: "/paths/~1x"},
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
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"other.yaml": reachOtherFixture})
	rootPath := filepath.Join(dir, "root.yaml")
	writeFiles(t, dir, map[string]string{"root.yaml": reachRootFixture})

	_, diags := loadExternal(t, rootPath, reachRootFixture, Options{})

	if d := cmp.Diff(m3WantDiags(filepath.Join(dir, "other.yaml")), diags); d != "" {
		t.Errorf("diagnostics did not match (-want +got):\n%s", d)
	}
	for _, d := range diags {
		assert.NotContains(t, d.Message, "17:25", "/unreached's own finding must not appear: %+v", d)
		assert.NotContains(t, d.Message, "21:40", "the numeric-literal artifact must not appear: %+v", d)
	}
}

// TestResolve_AnObjectReachedTwiceIsValidatedOnce covers both ways one object
// can be reached more than once: two independent $refs to the same named
// component, and a container reached alongside something inside it addressed
// by pointer rather than by component — in both orders, since the mechanism
// differs (covered when the container comes first, the finding's own
// deduplication when it comes second).
func TestResolve_AnObjectReachedTwiceIsValidatedOnce(t *testing.T) {
	t.Parallel()

	t.Run("two refs to one component report once, at the first", func(t *testing.T) {
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
  /a:
    get: {operationId: getA, parameters: [{$ref: "./other.yaml#/components/parameters/P"}], responses: {"200": {description: ok}}}
  /b:
    get: {operationId: getB, parameters: [{$ref: "./other.yaml#/components/parameters/P"}], responses: {"200": {description: ok}}}
`
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"other.yaml": other})
		rootPath := filepath.Join(dir, "root.yaml")
		writeFiles(t, dir, map[string]string{"root.yaml": root})

		_, diags := loadExternal(t, rootPath, root, Options{})

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/parameters/0"), diags[0].Provenance.Pointer,
			"reported at the first reference, not the second one to the same object")
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

	t.Run("container then contained: the covered skip", func(t *testing.T) {
		t.Parallel()
		root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: "./other.yaml#/paths/~1x"}
  /b:
    get:
      operationId: getB
      parameters:
        - $ref: "./other.yaml#/paths/~1x/get/parameters/0"
      responses: {"200": {description: ok}}
`
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"other.yaml": other})
		rootPath := filepath.Join(dir, "root.yaml")
		writeFiles(t, dir, map[string]string{"root.yaml": root})

		_, diags := loadExternal(t, rootPath, root, Options{})

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1a"), diags[0].Provenance.Pointer,
			"the container's own finding; the contained parameter is covered and skipped, not re-reported at /paths/~1b")
	})

	// Reported-finding deduplication (above) hides a dropped covered skip from
	// a plain finding count: re-validating an already-covered node reproduces
	// the very same (node, rule, message), which the reported map catches
	// independently of covered. What covered alone prevents is re-charging an
	// already-covered node's budget, so that is what proves it is still
	// checked: a container large enough that charging its own contained
	// parameter a second time crosses a budget charging it once does not.
	t.Run("container then contained: the covered skip also spares its budget", func(t *testing.T) {
		t.Parallel()
		largeOther := nestedOtherFixture()
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
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"other.yaml": largeOther})
		rootPath := filepath.Join(dir, "root.yaml")
		writeFiles(t, dir, map[string]string{"root.yaml": root})

		_, diags := loadExternal(t, rootPath, root, Options{MaxSourceNodes: 125, MaxAliasSurplus: 1})

		require.Len(t, diags, 1, "the container's own finding only; charging its contained parameter's "+
			"budget again, on top of the container's own charge, is what crosses this budget: %+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1b"), diags[0].Provenance.Pointer)
		assert.NotEqual(t, diag.BudgetExceeded, diags[0].Code)
	})

	t.Run("contained then container: the finding's own deduplication", func(t *testing.T) {
		t.Parallel()
		root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters:
        - $ref: "./other.yaml#/paths/~1x/get/parameters/0"
      responses: {"200": {description: ok}}
  /b: {$ref: "./other.yaml#/paths/~1x"}
`
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"other.yaml": other})
		rootPath := filepath.Join(dir, "root.yaml")
		writeFiles(t, dir, map[string]string{"root.yaml": root})

		_, diags := loadExternal(t, rootPath, root, Options{})

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/parameters/0"), diags[0].Provenance.Pointer,
			"the parameter's own finding, reported first; the container re-validates the same node but the finding is deduplicated, not re-reported at /paths/~1b")
	})
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
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"other.yaml": v32OtherFixture})
		rootPath := filepath.Join(dir, "root.yaml")
		writeFiles(t, dir, map[string]string{"root.yaml": v32RootFixture})

		_, diags := loadExternal(t, rootPath, v32RootFixture, Options{})

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
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"other.yaml": other})
		rootPath := filepath.Join(dir, "root.yaml")
		writeFiles(t, dir, map[string]string{"root.yaml": root})

		_, diags := loadExternal(t, rootPath, root, Options{})

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
	for name, files := range map[string]map[string]string{
		"in the source": {"root.yaml": header + "  schemas:\n    S: " + body + "\n"},
		"reached directly": {
			"root.yaml":  header + "  schemas:\n    S: {$ref: './other.yaml#/components/schemas/T'}\n",
			"other.yaml": "components:\n  schemas:\n    T: " + body + "\n",
		},
		"reached inside a parameter": {
			"root.yaml":  header + "  parameters:\n    P: {$ref: './other.yaml#/components/parameters/Q'}\n",
			"other.yaml": "components:\n  parameters:\n    Q: {name: q, in: query, schema: " + body + "}\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeFiles(t, dir, files)

			_, diags := loadExternal(t, filepath.Join(dir, "root.yaml"), files["root.yaml"], Options{})

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
// separate response, is reached after that, to pin that a document already
// over its budget is refused silently rather than reported again.
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
		"components:\n  responses:\n    Small: {description: ok}\n")
	return b.String()
}

// nestedRootFixture reaches nestedOtherFixture's parameter from /a, the path
// item around it from /b, and Small from /c.
const nestedRootFixture = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters:
        - $ref: "./other.yaml#/paths/~1x/get/parameters/0"
      responses: {"200": {description: ok}}
  /b: {$ref: "./other.yaml#/paths/~1x"}
  /c:
    get:
      operationId: getC
      responses:
        "200": {$ref: "./other.yaml#/components/responses/Small"}
`

// TestResolve_ReachedValidationIsBudgeted runs nestedRootFixture under three
// budgets. At 151 nodes (150 + 1) the parameter's finding is reported once,
// from /a, and the path item reached after it, which charges those nodes
// again, is refused as budget-exceeded. Charged once each, the two total 198
// nodes, so 198 (197 + 1) admits both where MaxSourceNodes alone, 197, would
// not: that boundary is what shows the alias budget's share is counted. An
// unbounded budget admits both too.
func TestResolve_ReachedValidationIsBudgeted(t *testing.T) {
	t.Parallel()
	other := nestedOtherFixture()
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"other.yaml": other})
	rootPath := filepath.Join(dir, "root.yaml")
	writeFiles(t, dir, map[string]string{"root.yaml": nestedRootFixture})
	otherPath := filepath.Join(dir, "other.yaml")

	t.Run("a tight budget refuses the container after the overlap", func(t *testing.T) {
		t.Parallel()
		_, diags := loadExternal(t, rootPath, nestedRootFixture, Options{MaxSourceNodes: 150, MaxAliasSurplus: 1})

		require.Len(t, diags, 2, "exactly two: /c's own document is already over budget and refuses silently, "+
			"not with a second budget-exceeded: %+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/parameters/0"), diags[0].Provenance.Pointer)
		assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)

		assert.Equal(t, diag.BudgetExceeded, diags[1].Code)
		assert.Equal(t, ir.SeverityError, diags[1].Severity)
		assert.Equal(t, jsontext.Pointer("/paths/~1b"), diags[1].Provenance.Pointer)
		assert.Equal(t, fmt.Sprintf(
			"the objects references reach in %s span more than the 151 nodes a document is validated over; "+
				"this one, and any reached after it, is not validated", otherPath),
			diags[1].Message)
	})

	t.Run("a looser budget admits both", func(t *testing.T) {
		t.Parallel()
		_, diags := loadExternal(t, rootPath, nestedRootFixture, Options{MaxSourceNodes: 197, MaxAliasSurplus: 1})

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/parameters/0"), diags[0].Provenance.Pointer)
		for _, d := range diags {
			assert.NotEqual(t, diag.BudgetExceeded, d.Code, "%+v", d)
		}
	})

	t.Run("an unbounded budget admits both", func(t *testing.T) {
		t.Parallel()
		_, diags := loadExternal(t, rootPath, nestedRootFixture, Options{})

		require.Len(t, diags, 1, "%+v", diags)
		for _, d := range diags {
			assert.NotEqual(t, diag.BudgetExceeded, d.Code, "%+v", d)
		}
	})
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
