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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// sitedFailuresSpec carries one unresolvable $ref at each of the six positions
// GitHub #385 names (parameter, callback, requestBody, response, header,
// pathItem), plus a securityScheme entry, a $ref this compile refuses to leave
// the document for, and a component schema, whose $ref the schema bindings
// report rather than the resolver — nine positions in all, each naming its own
// target.
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
	doc := parseSpec(t, sitedFailuresSpec)

	got := resolvedDiags(t, doc, "root.yaml")

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
		doc := parseSpec(t, `openapi: 3.1.0
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

		got := resolvedDiags(t, doc, "root.yaml")

		assertFailures(t, got, map[jsontext.Pointer]string{
			"/paths/~1a/get/parameters/0": `unresolved $ref "#/components/parameters/Alias", ` +
				`through "other.yaml#/components/parameters/P": external reference not allowed`,
			"/components/parameters/Alias": `unresolved $ref "other.yaml#/components/parameters/P": ` +
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

		_, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})
		require.NoError(t, err)

		assertFailures(t, diags, map[jsontext.Pointer]string{
			"/paths/~1a/get/responses/200": `unresolved $ref "ext.yaml#/components/responses/A", ` +
				`through "#/components/responses/Missing" in ` + filepath.Join(dir, "ext.yaml") + ": not found",
		})
	})
}

// TestResolve_AMappingChainReadsADefinitionWhereItIsWritten pins GitHub #996:
// a mapping value's chain meets a "#/$defs/..." $ref as a $ref's own chain
// does, read by the $defs rule from where that $ref is written, never from the
// schema the chain came through. Where the rule finds the definition, the
// chain ends there; where it finds none, the chain fails at that $ref, which
// is reported once, there, and the definition the chain came past is not read:
// S's X, whether it is a schema or names another document. The chain's end is
// what tells, since a chain that read S's X would end on it.
func TestResolve_AMappingChainReadsADefinitionWhereItIsWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "other.yaml", "Y: {type: object}\n")
	const pet = "    Pet:\n      type: object\n      properties: {k: {type: string}}\n" +
		"      discriminator: {propertyName: k, mapping: {c: '#/x-lib/S'}}\n"
	for name, tc := range map[string]struct {
		holder string
		x      string
		want   []string
	}{
		"no definition, one the chain came past": {
			"{type: object, properties: {p: {$ref: '#/$defs/X'}}}", "{type: object}",
			[]string{"/components/schemas/Holder/properties/p error " + diag.UnresolvedRef}},
		"no definition, one naming another document the chain came past": {
			"{type: object, properties: {p: {$ref: '#/$defs/X'}}}", "{$ref: 'other.yaml#/Y'}",
			[]string{"/components/schemas/Holder/properties/p error " + diag.UnresolvedRef}},
		"the holder's own definition": {
			"{type: object, $defs: {X: {type: string}}, properties: {p: {$ref: '#/$defs/X'}}}",
			"{type: object}", []string{}},
	} {
		src := openapitest.ComponentSpec(pet+"    Holder: "+tc.holder+"\n") +
			"x-lib:\n  S: {$defs: {X: " + tc.x + "}, $ref: '#/components/schemas/Holder/properties/p'}\n"
		doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(src)},
			Options{Reader: reader.Disk{}})
		require.NoError(t, err, name)
		require.NotNil(t, doc, name)

		assert.Equal(t, tc.want, severityLines(diags), name)
		m, ok := doc.Bindings.Table().Mapping(bind.Position{Pointer: "/components/schemas/Pet/discriminator"}, "#/x-lib/S")
		require.True(t, ok, name)
		if len(tc.want) == 0 {
			assert.Equal(t, bind.Position{Pointer: "/components/schemas/Holder/$defs/X"}, m.Chain.End, name)
		} else {
			assert.Equal(t, bind.Unresolved, m.Chain.Status, name)
		}
	}
}

// severityLines renders diags as "pointer severity code", sorted.
func severityLines(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", d.Provenance.Pointer, d.Severity, d.Code))
	}
	slices.Sort(out)
	return out
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

// TestResolve_AFindingNamesTheDocumentItIsIn pins the document a finding is
// placed in, through Load. The second and third $refs name ext.yaml, whose
// target is itself a $ref on to sub/ext2.yaml, where the second's finding is:
// placing it in the document a $ref names would send a reader to a line of
// ext.yaml that sits in the other file. The third fails to resolve, so it
// reaches no object to check, and its failure is pinned.
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

	doc, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})
	require.NoError(t, err)

	var placed []string
	for _, d := range diags {
		if strings.HasPrefix(d.Code, diag.Validation+"/") {
			other, ok := doc.Others.At(d.Provenance.Source)
			require.True(t, ok, "placed in a document read beside the source: %+v", d)
			placed = append(placed, fmt.Sprintf("%s#%s %d:%d", other.Label, d.Provenance.Pointer,
				d.Provenance.Position.Line, d.Provenance.Position.Column))
		}
	}
	assert.Equal(t, []string{
		"ext.yaml#/components/responses/R 3:8",
		"sub/ext2.yaml#/components/responses/R2 4:9",
	}, placed)
	for _, d := range diags {
		if d.Provenance.Pointer == "/paths/~1a/get/responses/202" {
			assert.False(t, strings.HasPrefix(d.Code, diag.Validation+"/"), "a reference that fails reaches no object to check: %+v", d)
		}
	}
	ext := filepath.Join(dir, "ext.yaml")
	assert.Equal(t, `unresolved $ref "ext.yaml#/components/responses/Broken", `+
		`through "sub/ext2.yaml#/components/responses/Scalar" in `+ext+": it names a position that holds no response",
		openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, "/paths/~1a/get/responses/202"),
		"a chain ending on a position that holds no object names the reference that reached it, where it is written")
}

// TestResolve_AFileSpelledTwoWaysPlacesAFindingOnce pins a node of a file the
// source spells two ways, which read prepares once under each spelling: Bad's
// parameter is an alias of Base's, so each check finds Base's fields, and the
// findings are placed where ext.yaml writes them, once, in its one entry,
// whichever spelling reaches the alias.
func TestResolve_AFileSpelledTwoWaysPlacesAFindingOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "ext.yaml", `components:
  parameters:
    Base: &base {name: b, in: nowhere}
    Bad: *base
`)
	plain, dotted := filepath.Join(dir, "ext.yaml"), dir+"/./ext.yaml"
	for name, refs := range map[string][2]string{"the alias dotted": {plain, dotted}, "the alias plain": {dotted, plain}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - $ref: '` + refs[0] + `#/components/parameters/Base'
        - $ref: '` + refs[1] + `#/components/parameters/Bad'
      responses: {"200": {description: ok}}
`)}

			doc, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})
			require.NoError(t, err)

			var placed []string
			for _, d := range diags {
				if strings.HasPrefix(d.Code, diag.Validation+"/") {
					placed = append(placed, fmt.Sprintf("%d %s %d:%d", d.Provenance.Source, d.Provenance.Pointer,
						d.Provenance.Position.Line, d.Provenance.Position.Column))
				}
			}
			slices.Sort(placed)
			assert.Equal(t, []string{
				"1 /components/parameters/Base 3:11",
				"1 /components/parameters/Base/in 3:31",
			}, placed, "%+v", diags)
			require.Len(t, doc.Others.List(), 1, "one file, one entry")
			assert.Equal(t, "ext.yaml", doc.Others.List()[0].Label)
		})
	}
}

// TestResolve_AReachedFindingNamesThePositionThatReadIt pins the read
// position a finding about another document's node names, as the source's own
// do: a parameter and a schema property ext.yaml mounts through aliases are each
// reported once, at the anchor, naming the position the least $ref reaching
// them reads them at in ext.yaml, whichever path the source declares first. A
// parameter a chain of two $refs reaches is read where the last one lands, so
// it names none.
func TestResolve_AReachedFindingNamesThePositionThatReadIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "ext.yaml", `x-param: &p {name: n, in: nowhere, schema: {type: string}}
x-schema: &s {type: bogus}
paths:
  /x: {get: {parameters: [*p], responses: {"200": {description: ok}}}}
  /y: {get: {parameters: [*p], responses: {"200": {description: ok}}}}
components:
  schemas:
    S: {properties: {s: *s}}
  parameters:
    Hop: {$ref: '#/components/parameters/Bad'}
    Bad: {name: b, in: inside, schema: {type: string}}
`)
	const a, b = "  /a: {$ref: 'ext.yaml#/paths/~1x'}\n", "  /b: {$ref: 'ext.yaml#/paths/~1y'}\n"
	for name, paths := range map[string]string{"a declared first": a + b, "b declared first": b + a} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte("openapi: 3.1.0\n" +
				"info: {title: T, version: \"1\"}\npaths:\n" + paths +
				"  /c: {get: {parameters: [{$ref: 'ext.yaml#/components/parameters/Hop'}], responses: {\"200\": {description: ok}}}}\n" +
				"components: {schemas: {R: {$ref: 'ext.yaml#/components/schemas/S'}}}\n")}

			_, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})
			require.NoError(t, err)

			read := map[jsontext.Pointer]string{}
			found := 0
			for _, d := range diags {
				if strings.HasPrefix(d.Code, diag.Validation+"/") {
					require.Equal(t, 1, d.Provenance.Source, "%+v", d)
					_, at, _ := strings.Cut(d.Message, "; read at ")
					read[d.Provenance.Pointer] = at
					found++
				}
			}
			assert.Equal(t, map[jsontext.Pointer]string{
				"/x-param/in":                   "/paths/~1x/get/parameters/0/in",
				"/x-schema/type":                "/components/schemas/S/properties/s/type",
				"/components/parameters/Bad/in": "",
			}, read, "%+v", diags)
			assert.Equal(t, 3, found, "each node once: %+v", diags)
		})
	}
}

// TestResolve_ReachedFindingsDoNotDependOnDeclarationOrder compiles one source
// as written and with its paths reversed, and requires the same reports.
// The library hands a later $ref the object an earlier one built, so which $ref
// draws a finding follows declaration order; where it is placed must not. The
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
			compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(src)}, Options{Reader: reader.Disk{}})
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
		"/components/responses/B",  // drawn by /b1 and /b2, kept once
		"/components/responses/R",  // reached from /x, /y, /c and the alias itself
		"/components/responses/R2", // reached at /m, which /z's chain resolves first as written
	}, findingSites, "each finding at its node in ext.yaml, once")
	for _, site := range []string{"/paths/~1s1/get/responses/200", "/paths/~1s2/get/responses/200"} {
		assert.Contains(t, asWritten, diag.UnresolvedRef+" "+site+` unresolved $ref "ext.yaml#/components/responses/Scalar": `+
			"it names a position that holds no response", "Scalar fails each $ref alike, whichever reaches it first")
	}
}

// TestResolve_ABoundInAnotherDocumentIsTheSchemaLowerings pins that a numeric
// bound past float64's range, in a document a $ref reads, draws no finding: the
// table leaves a bound's shape to the schema lowering, which reads 1e400 from
// the raw node exactly.
func TestResolve_ABoundInAnotherDocumentIsTheSchemaLowerings(t *testing.T) {
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

	doc, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})
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
func (f fakeResolvable) GetObjectAny() any { return nil }
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

// libraryOverflows holds the corpus fixtures whose schema loop the library's
// walk recurses round though the compiler's binder reads none: it reads a
// pointer ending in an empty token, through a schema's own members, in raw YAML
// to a $ref that leads back, where the binder reads the pointer as naming
// nothing (docs/parser-library.md#schema-cycle-overflow).
var libraryOverflows = map[string]bool{
	"cycle_schema_empty_token_stays.yaml": true,
}

// TestResolve_ResolvesWhatTheLibraryResolves is a drift test: resolveWith must
// resolve exactly the reference objects ResolveAllReferences does, over every
// corpus fixture. The library names a Matcher field per kind and resolveWith
// matches Any, so a kind the library adds later must still agree here.
//
// External references are off on both sides: the library's default reads the
// real file system where resolveWith's Options{} refuses. A fixture the build
// refuses before resolving is skipped, and so is one holding a loop of schema
// $refs, or listed in libraryOverflows, which the library's own walk overflows
// the stack on (docs/parser-library.md#schema-cycle-overflow).
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
		doc1, err := unmarshal(t.Context(), data, root)
		if err != nil {
			continue
		}
		if len(bind.Bind(root, bind.Config{Self: path}).Loops()) > 0 || libraryOverflows[filepath.Base(path)] {
			continue // a loop of schema $refs, which the library's walk recurses round forever
		}

		root2, _, err := decodeStream(data)
		require.NoError(t, err, path)
		releaseAnchors(root2)
		doc2, err := unmarshal(t.Context(), data, root2)
		require.NoError(t, err, path)

		checked++
		// The resolution failures are not this test's concern; IsResolved, read
		// back by referencePairs, is. They are reported only if the comparison fails.
		failures, resolveErr := doc1.ResolveAllReferences(t.Context(), soa.ResolveAllOptions{
			OpenAPILocation: path, DisableExternalRefs: true,
		})
		want := referencePairs(t, doc1)

		resolveWith(t.Context(), doc2, Siblings{}, sourceDocument{path: path}, Options{}, nil)
		got := referencePairs(t, doc2)

		if d := cmp.Diff(want, got); d != "" {
			t.Errorf("%s: resolved references differ from the library's own walk (-want +got):\n%s\n"+
				"the library's resolution returned %d failure(s), error: %v", path, d, len(failures), resolveErr)
		}
	}
	assert.GreaterOrEqual(t, checked, 10,
		"most of the corpus reaches the comparison; a filter this tight would check nothing")
}

// TestFailureDiag_WordsAFailedReadAsTheBinderDoes pins the phrasing a reference
// object's failure takes from the binder's: a read Open or Do answered with a
// refusal, a parse failure with the parser's line, and any other reason left
// as the resolver gave it.
func TestFailureDiag_WordsAFailedReadAsTheBinderDoes(t *testing.T) {
	t.Parallel()
	parser := errors.New("yaml: line 1: did not find expected node content")
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"a read refused", unread{errors.New("external document b.yaml refused: why")},
			`unresolved $ref "b.yaml#/P": the document it names could not be read: external document b.yaml refused: why`},
		{"a document that does not parse", unread{&bind.UnparsableError{Err: parser}},
			`unresolved $ref "b.yaml#/P": the document it names does not parse as YAML: yaml: line 1: did not find expected node content`},
		{"a document holding nothing", unread{bind.ErrEmptyDocument}, `unresolved $ref "b.yaml#/P": the document it names is empty`},
		{"the resolver's own reason", errors.New("not found"), `unresolved $ref "b.yaml#/P": not found`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := failureDiag(ir.Provenance{}, "b.yaml#/P", trail{}, tc.err)
			assert.Equal(t, tc.want, d.Message)
		})
	}
}

// TestObjectKind_NamesEachReferenceObjectKind pins the word a reference
// object's failure to reach an object uses for the kind of object it asked
// for, one for each kind of reference object the library has, and the word for
// a resolvable that is none of them.
func TestObjectKind_NamesEachReferenceObjectKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		r    resolvable
		want string
	}{
		{"a path item", &soa.ReferencedPathItem{}, "path item"},
		{"a parameter", &soa.ReferencedParameter{}, "parameter"},
		{"a header", &soa.ReferencedHeader{}, "header"},
		{"a request body", &soa.ReferencedRequestBody{}, "request body"},
		{"a response", &soa.ReferencedResponse{}, "response"},
		{"an example", &soa.ReferencedExample{}, "example"},
		{"a link", &soa.ReferencedLink{}, "link"},
		{"a callback", &soa.ReferencedCallback{}, "callback"},
		{"a security scheme", &soa.ReferencedSecurityScheme{}, "security scheme"},
		{"a kind this package does not name", fakeResolvable{}, "object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, objectKind(tc.r))
		})
	}
}

// TestNotObjectError_UnwrapsToTheParsersFailure pins the error a failed
// resolution's reason travels in: it reads as the parser's failure and unwraps
// to it, so a caller asking errors.Is of that failure still finds it.
func TestNotObjectError_UnwrapsToTheParsersFailure(t *testing.T) {
	t.Parallel()
	base := errors.New("unable to resolve reference: #/x-lib/S")
	err := notObjectError{kind: "parameter", err: base}
	assert.Equal(t, base.Error(), err.Error())
	assert.ErrorIs(t, err, base)
}

// TestResolve_AChainInTheSourceQuotesTheRefItWentThrough pins the reason a
// reference object's chain through another one takes where both are written
// in the source: it quotes the $ref written at the hop before the position
// holding no object, naming no document, since the source is the one read.
func TestResolve_AChainInTheSourceQuotesTheRefItWentThrough(t *testing.T) {
	t.Parallel()
	l := loadPlus(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      parameters: [{$ref: '#/components/parameters/P'}]
      responses: {'204': {description: ok}}
components:
  parameters:
    P: {$ref: '#/x-lib/S'}
x-lib: {S: just a string}
`, Options{})

	assert.Equal(t, []string{`unresolved $ref "#/components/parameters/P", through "#/x-lib/S": ` +
		"it names a position that holds no parameter"}, errorsOf(l.diags))
}

// resolvedDiags is what a resolver pass over doc, at path and with no reader,
// reports.
func resolvedDiags(t *testing.T, doc *soa.OpenAPI, path string) []ir.Diagnostic {
	t.Helper()
	return resolveWith(t.Context(), doc, Siblings{}, sourceDocument{path: path}, Options{}, nil).
		diags(sitePlacer(pointerAt(0, overlay.Origin{}), Others{}))
}
