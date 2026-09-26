package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// sitedFailuresSpec carries one unresolvable $ref at each of the six positions
// GitHub #385 names (parameter, callback, requestBody, response, header,
// pathItem), plus a component schema, a securityScheme entry, and a $ref this
// compile refuses to leave the document for — nine positions in all, each with
// its own, distinct reason.
const sitedFailuresSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters:
      - {$ref: '#/components/parameters/GhostParam'}
    get:
      operationId: getA
      callbacks:
        bad: {$ref: '#/components/callbacks/GhostCb'}
      requestBody: {$ref: '#/components/requestBodies/GhostBody'}
      responses:
        "200": {$ref: '#/components/responses/GhostResp'}
        "201":
          description: ok
          headers:
            X-H: {$ref: '#/components/headers/GhostHeader'}
  /ref: {$ref: '#/components/pathItems/GhostItem'}
  /ext: {$ref: 'other.yaml#/paths/~1x'}
components:
  schemas:
    S: {$ref: '#/components/schemas/GhostSchema'}
  securitySchemes:
    ghost: {$ref: '#/components/securitySchemes/GhostScheme'}
`

// TestResolve_SitesEachFailureAtItsReference pins the shape of every failure
// resolveWith reports: the $ref's own pointer, the exact code and severity, and
// a message that names the reference as written. The resolver's own reason is
// matched by Contains rather than pinned exactly, so a library rewording of it
// does not redden this test on its own.
func TestResolve_SitesEachFailureAtItsReference(t *testing.T) {
	t.Parallel()
	doc, valErrs := parseSpec(t, sitedFailuresSpec)
	require.Empty(t, valErrs, "the fixture is otherwise well-formed")

	got := resolveWith(t.Context(), pointerAt(0, overlay.Origin{}), doc, "root.yaml", Options{}, nil)

	want := []struct {
		pointer        string
		ref            string
		reasonContains string
	}{
		{"/paths/~1a/parameters/0", "#/components/parameters/GhostParam", "not found"},
		{"/paths/~1a/get/callbacks/bad", "#/components/callbacks/GhostCb", "not found"},
		{"/paths/~1a/get/requestBody", "#/components/requestBodies/GhostBody", "not found"},
		{"/paths/~1a/get/responses/200", "#/components/responses/GhostResp", "not found"},
		{"/paths/~1a/get/responses/201/headers/X-H", "#/components/headers/GhostHeader", "not found"},
		{"/paths/~1ref", "#/components/pathItems/GhostItem", "not found"},
		{"/paths/~1ext", "other.yaml#/paths/~1x", "external reference not allowed"},
		{"/components/schemas/S", "#/components/schemas/GhostSchema", "not found"},
		{"/components/securitySchemes/ghost", "#/components/securitySchemes/GhostScheme", "not found"},
	}
	byPointer := make(map[string]ir.Diagnostic, len(got))
	for _, d := range got {
		byPointer[d.Provenance.Pointer] = d
	}
	require.Len(t, got, len(want), "%+v", got)
	for _, w := range want {
		d, ok := byPointer[w.pointer]
		if !assert.True(t, ok, "a failure at %s: %+v", w.pointer, got) {
			continue
		}
		assert.Equal(t, diag.UnresolvedRef, d.Code, "%s", w.pointer)
		assert.Equal(t, ir.SeverityError, d.Severity, "%s", w.pointer)
		assert.Equal(t, ir.Provenance{Source: 0, Pointer: w.pointer}, d.Provenance)
		assert.True(t, strings.HasPrefix(d.Message, fmt.Sprintf("unresolved $ref %q: ", w.ref)),
			"%s: want prefix %q, got %q", w.pointer, fmt.Sprintf("unresolved $ref %q: ", w.ref), d.Message)
		assert.Contains(t, d.Message, w.reasonContains, "%s", w.pointer)
	}
}

// TestResolve_OverlayIntroducedReferenceNamesTheOverlay pins the other half of
// pointerAt: a $ref an overlay adds is attributed to the overlay's own source
// index, not the base document's, because the overlay is what wrote it.
func TestResolve_OverlayIntroducedReferenceNamesTheOverlay(t *testing.T) {
	t.Parallel()
	_, diags, err := Load(t.Context(), 0, openapitest.SourceOf(graftBase),
		overlayOptions("  - target: $.components.schemas\n    update: {Ghost: {$ref: '#/components/schemas/Missing'}}\n"))
	require.NoError(t, err)

	found := 0
	for _, d := range diags {
		if d.Code != diag.UnresolvedRef {
			continue
		}
		found++
		assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/schemas/Ghost"}, d.Provenance,
			"the overlay introduced this $ref, so the overlay's own index names it, not the base document's")
		assert.Contains(t, d.Message, `unresolved $ref "#/components/schemas/Missing"`)
	}
	assert.Equal(t, 1, found, "diagnostics: %+v", diags)
}

// fakeResolvable is a resolvable fabricated without the speakeasy library, for
// a test that wants to drive eachReference's own control flow rather than a
// real reference's resolution.
type fakeResolvable struct {
	ref      string
	resolved bool
	resolve  func(context.Context, references.ResolveOptions) ([]error, error)
}

func (f fakeResolvable) IsReference() bool { return true }
func (f fakeResolvable) IsResolved() bool  { return f.resolved }
func (f fakeResolvable) GetReference() references.Reference {
	return references.Reference(f.ref)
}

func (f fakeResolvable) Resolve(ctx context.Context, opts references.ResolveOptions) ([]error, error) {
	if f.resolve == nil {
		return nil, nil
	}
	return f.resolve(ctx, opts)
}

// fakeWalkItem builds a soa.WalkItem whose Match hands m over a single
// fakeResolvable at field, the way the real walk hands a Matcher a model at the
// position it sits.
func fakeWalkItem(field string, r fakeResolvable) soa.WalkItem {
	return soa.WalkItem{
		Location: soa.Locations{{ParentField: field}},
		Match: func(m soa.Matcher) error {
			return m.Any(r)
		},
	}
}

// TestEachReference_StopsAtTheFirstVisitError pins the walk's own contract: it
// stops at the first reference whose visit fails, reporting that reference's
// site, and never reaches the one after it. This covers references.go's
// site-then-visit-then-reset sequence under a failing visit.
func TestEachReference_StopsAtTheFirstVisitError(t *testing.T) {
	t.Parallel()
	items := func(yield func(soa.WalkItem) bool) {
		for _, field := range []string{"first", "second", "third"} {
			if !yield(fakeWalkItem(field, fakeResolvable{ref: "#/" + field})) {
				return
			}
		}
	}
	boom := errors.New("boom")
	var visited []string

	site, err := eachReference(items, func(_ jsontext.Pointer, r resolvable) error {
		ref := string(r.GetReference())
		visited = append(visited, ref)
		if ref == "#/second" {
			return boom
		}
		return nil
	})

	require.ErrorIs(t, err, boom)
	assert.Equal(t, jsontext.Pointer("/second"), site, "the failing reference's own site")
	assert.Equal(t, []string{"#/first", "#/second"}, visited, "the third reference is never visited")
}

// TestEachReference_PanicIsReportedAtTheReference covers both places a panic in
// the third-party walk or resolver can land: while resolving one reference, and
// between two of them.
func TestEachReference_PanicIsReportedAtTheReference(t *testing.T) {
	t.Parallel()

	t.Run("through Load, at the reference being resolved", func(t *testing.T) {
		t.Parallel()
		doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(resolverPanicSpec), Options{})
		require.NoError(t, err, "a resolver fault is a spec problem, not a Go error")
		require.NotNil(t, doc)
		require.Equal(t, 1, countErrorsAt(diags, diag.UnresolvedRef), "%+v", diags)
		for _, d := range diags {
			if d.Code == diag.UnresolvedRef {
				assert.Equal(t, "/components/responses/000", d.Provenance.Pointer,
					"the reference being resolved when the panic hit")
			}
		}
	})

	t.Run("a panic between items reports no site", func(t *testing.T) {
		t.Parallel()
		visited := 0
		items := func(yield func(soa.WalkItem) bool) {
			// The first reference resolves cleanly, so eachReference's own
			// site = "" reset runs before the panic below — the reset this
			// case exists to hold: the mutation that removes it leaves the
			// first reference's site behind for the panic to be blamed on.
			if !yield(fakeWalkItem("first", fakeResolvable{ref: "#/first"})) {
				return
			}
			panic("boom between items")
		}

		site, err := eachReference(items, func(jsontext.Pointer, resolvable) error {
			visited++
			return nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "boom between items")
		assert.Equal(t, jsontext.Pointer(""), site,
			"no reference was being resolved when the panic hit")
		assert.Equal(t, 1, visited)
	})
}

// TestReachedFinding covers what reachedFinding does with each shape a
// resolution can hand it: a structured finding with a node, one without, and an
// error that is not a structured finding at all.
func TestReachedFinding(t *testing.T) {
	t.Parallel()
	site := ir.Provenance{Source: 2, Pointer: "/x"}

	t.Run("a structured finding with a node", func(t *testing.T) {
		t.Parallel()
		verr := validation.Error{Severity: "warning", Rule: "some-rule",
			UnderlyingError: errors.New("boom"), Node: &yaml.Node{Line: 5, Column: 9}}

		got := reachedFinding(site, verr)

		assert.Equal(t, ir.SeverityWarning, got.Severity)
		assert.Equal(t, diag.Validation+"/some-rule", got.Code)
		assert.Equal(t, site, got.Provenance)
		assert.Equal(t, "boom, at 5:9 of the document the $ref resolves to", got.Message)
	})

	t.Run("a structured finding with no node", func(t *testing.T) {
		t.Parallel()
		verr := validation.Error{Severity: "error", Rule: "some-rule", UnderlyingError: errors.New("boom")}

		got := reachedFinding(site, verr)

		assert.Equal(t, "boom", got.Message, "no position to append when the finding names no node")
	})

	t.Run("an unstructured error", func(t *testing.T) {
		t.Parallel()
		got := reachedFinding(site, errors.New("plain failure"))

		assert.Equal(t, diag.Validation, got.Code, "no rule to suffix the bare code with")
		assert.Equal(t, ir.SeverityError, got.Severity)
		assert.Equal(t, "plain failure", got.Message)
		assert.Equal(t, site, got.Provenance)
	})
}

// referencePairs walks doc and returns, for every reference the resolvable
// interface recognizes, its JSON pointer and whether it resolved. It excludes
// the inline sighting Walk synthesizes around an already-resolved reference's
// content (IsReference false there), the same filter resolvedModels applies.
func referencePairs(t *testing.T, doc *soa.OpenAPI) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for item := range soa.Walk(t.Context(), doc) {
		err := item.Match(soa.Matcher{Any: func(model any) error {
			r, ok := model.(resolvable)
			if !ok || !r.IsReference() {
				return nil
			}
			out[string(item.Location.ToJSONPointer())] = r.IsResolved()
			return nil
		}})
		require.NoError(t, err)
	}
	return out
}

// TestResolve_ResolvesWhatTheLibraryResolves is a drift test: resolveWith's
// walk — item.Match(soa.Matcher{Any: ...}) plus the resolvable interface — must
// reach exactly the references ResolveAllReferences reaches, kind for kind,
// over every fixture the corpus has. ResolveAllReferences' own implementation
// hand-enumerates a Matcher field per kind; Any sees every kind including one
// added after this was written, so a match here says our set was never
// narrower than the library's own.
//
// External refs are disabled on both sides. Left at ResolveAllOptions' own
// default — enabled, with no VirtualFS/HTTPClient override — the library reads
// the real filesystem for the corpus's own external-ref fixtures and resolves
// them, while resolveWith's Options{} leaves them off: a difference in I/O
// policy, not in which references the two walks reach. Probed directly: with
// the library's default left alone, resolve_main_external_valid.yaml's two
// external parameters and responses come back resolved from the library and
// refused from ours, which would fail this test for a reason that has nothing
// to do with what it checks. Matching the policy on both sides removes that.
//
// "Parses" is read the way Load reads it: past the same pre-parse refusals
// build runs (refusals), not a raw decode. Skipping that gate would hand a
// cycle or alias-bomb fixture straight to a resolver with no cycle guard of its
// own, which is exactly the deadlock and stack overflow corpus_test.go
// documents those refusals as existing to prevent.
func TestResolve_ResolvesWhatTheLibraryResolves(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../../../../testdata/openapi/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, files, "the corpus is not empty")

	checked := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		require.NoError(t, err, path)

		root, _, err := decodeStream(data)
		if err != nil {
			continue // not YAML at all: out of scope for a resolution drift test
		}
		if diag.HasError(refusals(scan.InSource(0), root, Options{})) {
			continue // a pre-parse refusal fixture: never reaches a resolver
		}
		releaseAnchors(root)
		doc1, _, err := unmarshal(t.Context(), data, root)
		if err != nil {
			continue
		}

		root2, _, err := decodeStream(data)
		require.NoError(t, err, path)
		releaseAnchors(root2)
		doc2, _, err := unmarshal(t.Context(), data, root2)
		require.NoError(t, err, path)

		checked++
		//nolint:errcheck // the resolution failures themselves are not this test's
		// concern; IsResolved, read back by referencePairs, is.
		doc1.ResolveAllReferences(t.Context(), soa.ResolveAllOptions{
			OpenAPILocation: path, DisableExternalRefs: true,
		})
		want := referencePairs(t, doc1)

		resolveWith(t.Context(), pointerAt(0, overlay.Origin{}), doc2, path, Options{}, nil)
		got := referencePairs(t, doc2)

		if d := cmp.Diff(want, got); d != "" {
			t.Errorf("%s: resolved references differ from the library's own walk (-want +got):\n%s", path, d)
		}
	}
	assert.GreaterOrEqual(t, checked, 10,
		"most of the corpus reaches the comparison; a filter this tight would check nothing")
}
