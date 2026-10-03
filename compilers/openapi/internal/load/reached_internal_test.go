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
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"root.yaml":  reachedFixtureRoot,
		"other.yaml": reachedFixtureOther,
		"third.yaml": reachedFixtureThird,
	})
	got, _ := loadExternal(t, filepath.Join(dir, "root.yaml"), reachedFixtureRoot, Options{})
	return got.Doc, dir
}

// TestTargetOf covers which references reach an object in another document,
// and the document each names: the one its chain ends in, not the one it
// first leaves for. An internal reference counts once its chain leaves the
// source, and one that stays in it, or ends on no object, reaches nothing.
func TestTargetOf(t *testing.T) {
	t.Parallel()
	doc, dir := reachedFixtureDoc(t)
	schema := func(name string) resolvable {
		ref, ok := doc.Components.Schemas.Get(name)
		require.True(t, ok, name)
		return ref
	}
	stops, ok := doc.Paths.Get("/stops")
	require.True(t, ok)

	for name, want := range map[string]string{
		"S":       filepath.Join(dir, "other.yaml"),
		"Chained": filepath.Join(dir, "third.yaml"),
		"ToS":     filepath.Join(dir, "other.yaml"),
	} {
		got, ok := targetOf("/site", schema(name))
		require.True(t, ok, name)
		assert.Equal(t, want, got.doc, name)
		assert.Equal(t, jsontext.Pointer("/site"), got.site, name)
		assert.NotNil(t, got.node, name)
	}
	for name, r := range map[string]resolvable{"ToLocal": schema("ToLocal"), "/stops": stops} {
		_, ok := targetOf("/site", r)
		assert.False(t, ok, "%s reaches no object in another document", name)
	}
}

// TestReachedObject covers every outcome of reachedObject: a schema reference
// resolving to an object, one resolving to a boolean (nothing to validate), the
// default arm's success path for a plain reference, a chain that ends on no
// object, and a resolvable that cannot say what it names, which no real
// document produces, so a fake drives it.
func TestReachedObject(t *testing.T) {
	t.Parallel()
	doc, _ := reachedFixtureDoc(t)

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
// the six concrete kinds it walks through a synthetic Reference wrapper (see
// reachedWalk's own doc comment for why a wrapper rather than the bare type),
// and the default arm for a kind it does not reconcile schemas in at all
// (an example, a link, a security scheme).
func TestReachedWalk(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("a schema walks through ConcreteToReferenceable", func(t *testing.T) {
		t.Parallel()
		doc, _ := reachedFixtureDoc(t)
		ref, ok := doc.Components.Schemas.Get("S")
		require.True(t, ok)
		obj, _, ok := reachedObject(ref)
		require.True(t, ok)
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

	// An internal reference is visited: whether its chain leaves the source is
	// targetOf's to decide (TestTargetOf).
	t.Run("a model that is not a resolved reference is not visited", func(t *testing.T) {
		t.Parallel()
		cases := map[string]reachedFake{
			"not a reference": {isRef: false, isResolved: true, ref: "other.yaml#/x"},
			"unresolved":      {isRef: true, isResolved: false, ref: "other.yaml#/x"},
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
		assert.Zero(t, visited, "neither reaches visit")
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

// reachedFakePanicObject is an object whose Validate panics, driving
// checkAll's barrier without depending on a particular library fault.
type reachedFakePanicObject struct{}

func (reachedFakePanicObject) Validate(context.Context, ...validation.Option) []error {
	panic("object validation panicked")
}

// reachedFakePanicHolder is a reachedFake naming a reachedFakePanicObject, so
// reachedObject succeeds and validateObject then panics on it.
type reachedFakePanicHolder struct{ reachedFake }

func (reachedFakePanicHolder) GetObjectAny() any { return reachedFakePanicObject{} }

func (reachedFakePanicHolder) GetRootNode() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode} }

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
	r := reachedFakePanicHolder{reachedFake{isRef: true, isResolved: true, ref: "other.yaml#/x"}}
	item := reachedFakeWalkItem(r, soa.Locations{{ParentField: "paths"}, {ParentKey: strPtr("x")}})

	for name, c := range map[string]struct {
		items   iter.Seq[soa.WalkItem]
		site    jsontext.Pointer
		message string
	}{
		"in an object's validation": {reachedFakeWalkItems(item), "/paths/x", "object validation panicked"},
		"reading the walk": {func(yield func(soa.WalkItem) bool) {
			if yield(item) {
				panic("walk boom")
			}
		}, "", "walk boom"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			diags := checkReached(t.Context(), pointerAt(0, overlay.Origin{}), c.items, newReached(doc, Options{}))

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

// reachWantDiags is the exact diagnostic list
// TestResolve_ValidatesWhatAnExternalReferenceReaches expects, with other.yaml
// at otherPath: resolution's own finding first, then validation's, by site.
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

	if d := cmp.Diff(reachWantDiags(filepath.Join(dir, "other.yaml")), diags); d != "" {
		t.Errorf("diagnostics did not match (-want +got):\n%s", d)
	}
	for _, d := range diags {
		assert.NotContains(t, d.Message, "17:25", "/unreached's own finding must not appear: %+v", d)
		assert.NotContains(t, d.Message, "21:40", "the numeric-literal artifact must not appear: %+v", d)
	}
}

// TestResolve_EveryKindOfObjectIsValidated reaches each kind of object a $ref
// names that the other tests do not, a header, request body, callback,
// example, link and security scheme, each with a defect only its own Validate
// finds. Each finding is reported at its $ref, under the rule and severity the
// library gives it: the callback's is a warning.
func TestResolve_EveryKindOfObjectIsValidated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
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
	writeFiles(t, dir, map[string]string{"root.yaml": root, "other.yaml": `components:
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

	_, diags := loadExternal(t, filepath.Join(dir, "root.yaml"), root, Options{})

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
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"other.yaml": other, "root.yaml": root})
		_, diags := loadExternal(t, filepath.Join(dir, "root.yaml"), root, opts)
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
	dir := t.TempDir()
	writeFile(t, dir, "other.yaml", `paths:
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
`)
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
		_, diags := loadExternal(t, filepath.Join(dir, "root.yaml"), src, Options{})
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
	dir := t.TempDir()
	root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {parameters: [{name: q, in: query, schema: {$ref: "./a.yaml#/components/schemas/X"}}], responses: {"200": {description: ok}}}}
  /b: {get: {parameters: [{$ref: "./b.yaml#/components/parameters/Y"}], responses: {"200": {description: ok}}}}
`
	writeFiles(t, dir, map[string]string{
		"root.yaml": root,
		"a.yaml":    "components:\n  schemas:\n    X: {$ref: \"./b.yaml#/components/parameters/Y/schema\"}\n",
		"b.yaml": "components:\n  parameters:\n    Y: {name: p, in: sideways, schema: " +
			"{type: object, properties: {p1: {type: string}, p2: {type: string}, p3: {type: string}, p4: {type: string}, p5: {type: string}, p6: {type: string}, p7: {type: string}, p8: {type: string}, p9: {type: string}, p10: {type: string}}}}\n",
	})
	bPath := filepath.Join(dir, "b.yaml")

	_, diags := loadExternal(t, filepath.Join(dir, "root.yaml"), root, Options{MaxSourceNodes: 60, MaxAliasSurplus: 1})

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, jsontext.Pointer("/paths/~1b/get/parameters/0"), diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "reach in "+bPath+" span more than the 61 nodes")

	_, diags = loadExternal(t, filepath.Join(dir, "root.yaml"), root, Options{MaxSourceNodes: 120, MaxAliasSurplus: 1})
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
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"other.yaml": other})
	rootPath := filepath.Join(dir, "root.yaml")
	writeFiles(t, dir, map[string]string{"root.yaml": nestedRootFixture})
	otherPath := filepath.Join(dir, "other.yaml")

	t.Run("a tight budget refuses the container after the overlap", func(t *testing.T) {
		t.Parallel()
		_, diags := loadExternal(t, rootPath, nestedRootFixture, Options{MaxSourceNodes: 150, MaxAliasSurplus: 1})

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
		_, again := loadExternal(t, rootPath, nestedRoot(reversed), Options{MaxSourceNodes: 150, MaxAliasSurplus: 1})
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
			_, diags := loadExternal(t, rootPath, nestedRootFixture, opts)

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
