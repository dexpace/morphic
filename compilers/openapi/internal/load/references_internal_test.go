package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// sitedFailuresSpec carries one unresolvable $ref at each of the six positions
// GitHub #385 names (parameter, callback, requestBody, response, header,
// pathItem), plus a component schema, a securityScheme entry, and a $ref this
// compile refuses to leave the document for — nine positions in all, each
// naming its own target.
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

	_, got := resolveWith(t.Context(), pointerAt(0, overlay.Origin{}), doc, sourceDocument{path: "root.yaml"}, Options{}, nil)

	want := []struct {
		pointer        jsontext.Pointer
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
	byPointer := make(map[jsontext.Pointer]ir.Diagnostic, len(got))
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

// TestResolve_AChainedFailureNamesWhereItStopped pins the failure of a $ref
// whose target is a $ref that fails: the reason is about the second, so the
// report at the first quotes both, and the second's own report, when the walk
// reaches it, quotes only itself. The second is named with its document when
// that is not the source, since #/components/... would read as the source's.
func TestResolve_AChainedFailureNamesWhereItStopped(t *testing.T) {
	t.Parallel()

	t.Run("within the source, for each kind of trail", func(t *testing.T) {
		t.Parallel()
		doc, valErrs := parseSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {$ref: '#/components/parameters/Alias'}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Alias'}
components:
  parameters:
    Alias: {$ref: 'other.yaml#/components/parameters/P'}
  schemas:
    Alias: {$ref: 'other.yaml#/components/schemas/S'}
`)
		require.Empty(t, valErrs)

		_, got := resolveWith(t.Context(), pointerAt(0, overlay.Origin{}), doc, sourceDocument{path: "root.yaml"}, Options{}, nil)

		assertFailures(t, got, map[jsontext.Pointer]string{
			"/paths/~1a/get/parameters/0": `unresolved $ref "#/components/parameters/Alias", ` +
				`through "other.yaml#/components/parameters/P": external reference not allowed`,
			"/paths/~1a/get/responses/200/content/application~1json/schema": `unresolved $ref "#/components/schemas/Alias", ` +
				`through "other.yaml#/components/schemas/S": external reference not allowed`,
			"/components/parameters/Alias": `unresolved $ref "other.yaml#/components/parameters/P": ` +
				"external reference not allowed",
			"/components/schemas/Alias": `unresolved $ref "other.yaml#/components/schemas/S": ` +
				"external reference not allowed",
		})
	})

	t.Run("within another document", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "ext.yaml", "components:\n  responses:\n    A: {$ref: '#/components/responses/Missing'}\n")
		src := compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200": {$ref: 'ext.yaml#/components/responses/A'}
components:
  responses:
    Missing: {description: the source has one by this name}
`)}

		_, diags, err := Load(t.Context(), 0, src, Options{AllowExternalRefs: true})
		require.NoError(t, err)

		assertFailures(t, diags, map[jsontext.Pointer]string{
			"/paths/~1a/get/responses/200": `unresolved $ref "ext.yaml#/components/responses/A", ` +
				`through "#/components/responses/Missing" in ` + filepath.Join(dir, "ext.yaml") + ": not found",
		})
	})
}

// assertFailures requires got to hold exactly one unresolved-ref error per
// pointer in want and nothing else, each message starting as want says. The
// rest is the resolver's own wording.
func assertFailures(t *testing.T, got []ir.Diagnostic, want map[jsontext.Pointer]string) {
	t.Helper()
	seen := make(map[jsontext.Pointer]int, len(got))
	for _, d := range got {
		seen[d.Provenance.Pointer]++
		prefix, ok := want[d.Provenance.Pointer]
		if !assert.True(t, ok, "a report at %s: %+v", d.Provenance.Pointer, got) {
			continue
		}
		assert.Equal(t, diag.UnresolvedRef, d.Code)
		assert.Equal(t, ir.SeverityError, d.Severity)
		assert.True(t, strings.HasPrefix(d.Message, prefix), "want prefix %q, got %q", prefix, d.Message)
	}
	for pointer := range want {
		assert.Equal(t, 1, seen[pointer], "one report at %s: %+v", pointer, got)
	}
}

// TestResolve_AFindingNamesTheDocumentItIsIn pins the document a finding's
// message places it in, through Load. The second and third $refs name ext.yaml,
// whose target is itself a $ref on to sub/ext2.yaml, where both findings are:
// naming the document a $ref names would send a reader to a line of ext.yaml
// that sits in the other file. The third also fails, and its failure is pinned.
func TestResolve_AFindingNamesTheDocumentItIsIn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "ext.yaml", `components:
  responses:
    R: {content: {}}
    Chain: {$ref: 'sub/ext2.yaml#/components/responses/R2'}
    Broken: {$ref: 'sub/ext2.yaml#/components/responses/Scalar'}
`)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o700))
	writeFile(t, filepath.Join(dir, "sub"), "ext2.yaml", `# a line ext.yaml does not have
components:
  responses:
    R2: {content: {}}
    Scalar: 42
`)
	src := compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200": {$ref: 'ext.yaml#/components/responses/R'}
        "201": {$ref: 'ext.yaml#/components/responses/Chain'}
        "202": {$ref: 'ext.yaml#/components/responses/Broken'}
`)}

	_, diags, err := Load(t.Context(), 0, src, Options{AllowExternalRefs: true})
	require.NoError(t, err)

	ext, ext2 := filepath.Join(dir, "ext.yaml"), filepath.Join(dir, "sub", "ext2.yaml")
	for pointer, want := range map[jsontext.Pointer]string{
		"/paths/~1a/get/responses/200": ", at 3:8 of " + ext,
		"/paths/~1a/get/responses/201": ", at 4:9 of " + ext2,
		"/paths/~1a/get/responses/202": ", at 5:13 of " + ext2,
	} {
		var found []string
		for _, d := range diags {
			if d.Provenance.Pointer == pointer && strings.HasPrefix(d.Code, diag.Validation+"/") {
				found = append(found, d.Message)
			}
		}
		require.Len(t, found, 1, "%s: %+v", pointer, diags)
		assert.True(t, strings.HasSuffix(found[0], want), "%s: want suffix %q, got %q", pointer, want, found[0])
	}
	assert.Equal(t, `unresolved $ref "ext.yaml#/components/responses/Broken": `+
		"unable to resolve reference: sub/ext2.yaml#/components/responses/Scalar",
		openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, "/paths/~1a/get/responses/202"),
		"a resolution that recorded where it ended stopped at no other reference to quote")
}

// TestResolve_ReachedFindingsDoNotDependOnDeclarationOrder compiles one source
// as written and with its paths reversed, and requires the same reports.
// The library hands a later $ref the object an earlier one built, so which $ref
// draws a finding follows declaration order; where it is reported must not. The
// shapes: a target several $refs share, one an internal alias chains to, one
// built again from cached bytes (/a reads the document first), one that fails
// to build, reached twice, and a $ref /z's chain resolves before the walk
// reaches it (/m), as written but not reversed.
func TestResolve_ReachedFindingsDoNotDependOnDeclarationOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "ext.yaml", `components:
  responses:
    A: {description: fine}
    R: {content: {}}
    B: {content: {}}
    R2: {content: {}}
    Scalar: 42
`)
	paths := []string{
		"/a: 'ext.yaml#/components/responses/A'",
		"/x: 'ext.yaml#/components/responses/R'",
		"/y: 'ext.yaml#/components/responses/R'",
		"/c: '#/components/responses/Alias'",
		"/b1: 'ext.yaml#/components/responses/B'",
		"/b2: 'ext.yaml#/components/responses/B'",
		"/s1: 'ext.yaml#/components/responses/Scalar'",
		"/s2: 'ext.yaml#/components/responses/Scalar'",
		"/z: '#/paths/~1m/get/responses/200'",
		"/m: 'ext.yaml#/components/responses/R2'",
	}
	compile := func(order []string) []string {
		src := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n"
		for _, p := range order {
			path, ref, _ := strings.Cut(p, ": ")
			src += "  " + path + ": {get: {responses: {\"200\": {$ref: " + ref + "}}}}\n"
		}
		src += "components:\n  responses:\n    Alias: {$ref: 'ext.yaml#/components/responses/R'}\n"
		_, diags, err := Load(t.Context(), 0,
			compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(src)}, Options{AllowExternalRefs: true})
		require.NoError(t, err)
		out := make([]string, 0, len(diags))
		for _, d := range diags {
			out = append(out, fmt.Sprintf("%s %s %s", d.Code, d.Provenance.Pointer, d.Message))
		}
		slices.Sort(out)
		return out
	}

	reversedPaths := slices.Clone(paths)
	slices.Reverse(reversedPaths)
	asWritten := compile(paths)
	reversed := compile(reversedPaths)

	if d := cmp.Diff(asWritten, reversed); d != "" {
		t.Errorf("reports depend on declaration order (-as written +reversed):\n%s", d)
	}
	var findingSites []string
	for _, line := range asWritten {
		if code, rest, _ := strings.Cut(line, " "); strings.HasPrefix(code, diag.Validation+"/") {
			site, _, _ := strings.Cut(rest, " ")
			findingSites = append(findingSites, site)
		}
	}
	assert.Equal(t, []string{
		"/components/responses/Alias",   // R: the least of /x, /y, /c and the alias itself
		"/paths/~1b1/get/responses/200", // B: drawn twice, kept once
		"/paths/~1m/get/responses/200",  // R2: /m, which /z's chain resolves first as written
		"/paths/~1s1/get/responses/200", // Scalar: the lesser of the two that fail
	}, findingSites, "each finding at the least pointer among the $refs reaching its target")
}

// TestResolve_AnArtifactInAnotherDocumentIsDropped pins that a finding the
// source's own findings drop as a library artifact is dropped from a document a
// $ref reads too: the library cannot hold 1e400 in a float64, and Morphic reads
// the bound from the raw node instead.
func TestResolve_AnArtifactInAnotherDocumentIsDropped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "ext.yaml", `components:
  responses:
    R:
      description: ok
      content:
        application/json:
          schema: {type: number, maximum: 1e400}
`)
	src := compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200": {$ref: 'ext.yaml#/components/responses/R'}
`)}

	doc, diags, err := Load(t.Context(), 0, src, Options{AllowExternalRefs: true})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Empty(t, diags)
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

// fakeWalkItem builds a soa.WalkItem whose Match hands m over a single model
// at field, the way the real walk hands a Matcher a model at the position it
// sits.
func fakeWalkItem(field string, model any) soa.WalkItem {
	return soa.WalkItem{
		Location: soa.Locations{{ParentField: field}},
		Match: func(m soa.Matcher) error {
			return m.Any(model)
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

	site, err := eachReference(items, "reference resolver", func(_ jsontext.Pointer, r resolvable) error {
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
				assert.Equal(t, jsontext.Pointer("/components/responses/000"), d.Provenance.Pointer,
					"the reference being resolved when the panic hit")
			}
		}
	})

	t.Run("a panic between items reports no site", func(t *testing.T) {
		t.Parallel()
		visited := 0
		items := func(yield func(soa.WalkItem) bool) {
			// The first reference resolves cleanly, so eachReference resets
			// site before the panic below. Without that reset, the panic would
			// be blamed on the first reference's site.
			if !yield(fakeWalkItem("first", fakeResolvable{ref: "#/first"})) {
				return
			}
			panic("boom between items")
		}

		site, err := eachReference(items, "reference resolver", func(jsontext.Pointer, resolvable) error {
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

// TestReachedFindings covers where resolveWith reports the findings its walk draws. A
// finding is placed at the least pointer among the $refs whose trails end at
// its target, whichever $ref drew it; one along a trail with no target stays at
// its own. One drawn twice is kept at the least of its places, and one at no
// node is told apart by its place too. One the source's own would drop is not.
func TestReachedFindings(t *testing.T) {
	t.Parallel()
	node, other := &yaml.Node{Line: 3, Column: 5}, &yaml.Node{Line: 4, Column: 1}
	at := func(n *yaml.Node, rule, msg string) error {
		return &validation.Error{Severity: validation.SeverityError, Rule: rule,
			UnderlyingError: errors.New(msg), Node: n}
	}
	toT := trail{docs: []string{"e.yaml"}, target: "e.yaml#/T"}
	f := reachedFindings{sites: map[references.Reference]jsontext.Pointer{}}

	f.note("/paths/~1y", toT, []error{at(node, "r", "m"), at(nil, "r", "m"),
		at(node, validation.RuleValidationOperationIdUnique, "m")})
	f.note("/paths/~1x", toT, nil)                         // reaches T, draws nothing
	f.note("/paths/~1z", toT, []error{at(node, "r", "m")}) // T built again
	f.note("/paths/~1b", trail{stopped: "#/g"}, []error{at(other, "r", "m")})
	f.note("/paths/~1a", trail{stopped: "#/g"}, []error{at(other, "r", "m")})
	f.note("/paths/~1c", trail{stopped: "#/h"}, []error{at(nil, "r", "m"), errors.New("bare")})

	type placed struct {
		Pointer jsontext.Pointer
		Message string
	}
	diags := f.diags(func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} })
	got := make([]placed, 0, len(diags))
	for _, d := range diags {
		got = append(got, placed{d.Provenance.Pointer, d.Message})
	}
	want := []placed{
		{"/paths/~1x", "m, at 3:5 of e.yaml"},
		{"/paths/~1x", "m"},
		{"/paths/~1a", `m, at 4:1 of the document that "#/g" names`},
		{"/paths/~1c", "m"},
		{"/paths/~1c", "bare"},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("reachedFindings.diags (-want +got):\n%s", d)
	}
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

		got := reachedFinding(site, "other.yaml", verr)

		assert.Equal(t, ir.SeverityWarning, got.Severity)
		assert.Equal(t, diag.Validation+"/some-rule", got.Code)
		assert.Equal(t, site, got.Provenance)
		assert.Equal(t, "boom, at 5:9 of other.yaml", got.Message)
	})

	t.Run("a structured finding with no node", func(t *testing.T) {
		t.Parallel()
		verr := validation.Error{Severity: "error", Rule: "some-rule", UnderlyingError: errors.New("boom")}

		got := reachedFinding(site, "other.yaml", verr)

		assert.Equal(t, "boom", got.Message, "no position to append when the finding names no node")
	})

	t.Run("an unstructured error", func(t *testing.T) {
		t.Parallel()
		got := reachedFinding(site, "other.yaml", errors.New("plain failure"))

		assert.Equal(t, diag.Validation, got.Code, "no rule to suffix the bare code with")
		assert.Equal(t, ir.SeverityError, got.Severity)
		assert.Equal(t, "plain failure", got.Message)
		assert.Equal(t, site, got.Provenance)
	})
}

// TestFindingPlace covers each answer findingPlace gives a trail: its last
// document, the one named by the reference it stopped at, quoted with the
// document that reference is written in when that is not the source, and none
// for a trail that was cut or is empty.
func TestFindingPlace(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		trail trail
		want  string
	}{
		"ended on an object": {trail{docs: []string{"a.yaml", "b.yaml"}}, "b.yaml"},
		"stopped at the first reference": {trail{stopped: "a.yaml#/x"},
			`the document that "a.yaml#/x" names`},
		"stopped further on, in another document": {trail{docs: []string{"a.yaml"}, stopped: "#/x"},
			`the document that "#/x" in a.yaml names`},
		"stopped further on, in the source": {trail{docs: []string{"a.yaml"}, stopped: "#/x", endsInSource: true},
			`the document that "#/x" names`},
		"cut at the bound": {trail{docs: []string{"a.yaml"}, cut: true}, "a document the $ref leads to"},
		"empty":            {trail{}, "a document the $ref leads to"},
	} {
		assert.Equal(t, tc.want, findingPlace(tc.trail), name)
	}
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

// TestResolve_ResolvesWhatTheLibraryResolves is a drift test: resolveWith must
// resolve exactly the references ResolveAllReferences does, over every corpus
// fixture. The library names a Matcher field per kind and resolveWith matches
// Any, so a kind the library adds later must still agree here.
//
// External references are off on both sides. The library's default reads the
// real file system where resolveWith's Options{} refuses, a difference in I/O
// policy rather than in which references either walk reaches. A fixture build
// refuses before resolving, by the pre-parse refusals or the chain-cycle check,
// is skipped: the resolver has no guard of its own against those cycles.
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
		if d, found := chainCycle(t.Context(), scan.InSource(0), root, doc1); found && d.Severity == ir.SeverityError {
			continue // a chain the resolver would recurse through forever
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

		resolveWith(t.Context(), pointerAt(0, overlay.Origin{}), doc2, sourceDocument{path: path}, Options{}, nil)
		got := referencePairs(t, doc2)

		if d := cmp.Diff(want, got); d != "" {
			t.Errorf("%s: resolved references differ from the library's own walk (-want +got):\n%s", path, d)
		}
	}
	assert.GreaterOrEqual(t, checked, 10,
		"most of the corpus reaches the comparison; a filter this tight would check nothing")
}
