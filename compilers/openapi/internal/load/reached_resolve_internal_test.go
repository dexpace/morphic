package load

import (
	"encoding/json/jsontext"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

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
// TestResolve_ValidatesWhatAnExternalReferenceReaches expects: the findings,
// each placed in other.yaml's entry at the node it is about, in the order of
// the sites that reached them, then the repeat /z's list writes by naming P
// twice.
func reachWantDiags() []ir.Diagnostic {
	at := func(code, message, pointer string, line, column int) ir.Diagnostic {
		return ir.Diagnostic{
			Severity: ir.SeverityError, Code: diag.Validation + "/" + code, Message: message,
			Provenance: ir.Provenance{Source: 1, Pointer: jsontext.Pointer(pointer),
				Position: ir.Position{Line: line, Column: column}},
		}
	}
	return []ir.Diagnostic{
		at("validation-type-mismatch",
			`Schema Object field "minProperties" must be a non-negative integer in OpenAPI 3.1 (Schema Object)`,
			"/components/schemas/Bad/minProperties", 31, 22),
		at("validation-type-mismatch", `Schema Object field "deprecated" must be a boolean in OpenAPI 3.1 (Schema Object)`,
			"/components/schemas/Bad/deprecated", 32, 19),
		at("validation-required-field", `Parameter Object field "in" is REQUIRED in OpenAPI 3.1 (Parameter Object)`,
			"/paths/~1x/get/parameters/0", 10, 11),
		at("validation-allowed-values",
			`Parameter Object field "in" must be one of "query", "header", "path", "cookie" in OpenAPI 3.1 `+
				`(Parameter Object)`, "/components/parameters/P/in", 21, 22),
		at("validation-type-mismatch",
			`Schema Object field "minLength" must be a non-negative integer in OpenAPI 3.1 (Schema Object)`,
			"/components/responses/R/content/application~1json/schema/minLength", 27, 45),
		{
			Severity:   ir.SeverityError,
			Code:       diag.DuplicateParameter,
			Message:    `parameter "p" in "sideways" repeats the entry at /paths/~1z/get/parameters/0`,
			Provenance: ir.Provenance{Source: 0, Pointer: "/paths/~1z/get/parameters/1"},
		},
	}
}

// TestResolve_ValidatesWhatAnExternalReferenceReaches is GitHub #545's repro:
// compiled as the source, other.yaml raises more findings than root.yaml
// reaches, and only what is reached is checked at all. The numeric literal P's
// schema writes (minimum: .5) is a number, so it draws none, and nothing is
// reported from /unreached, which nothing in root.yaml names.
func TestResolve_ValidatesWhatAnExternalReferenceReaches(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": reachOtherFixture})

	_, diags := loadExternal(t, dir, reachRootFixture, Options{})

	if d := cmp.Diff(reachWantDiags(), diags); d != "" {
		t.Errorf("diagnostics did not match (-want +got):\n%s", d)
	}
	for _, d := range diags {
		assert.NotEqual(t, ir.Position{Line: 17, Column: 25}, d.Provenance.Position,
			"/unreached's own finding must not appear: %+v", d)
		assert.NotEqual(t, ir.Position{Line: 21, Column: 40}, d.Provenance.Position,
			"the numeric literal must not appear: %+v", d)
	}
}

// TestResolve_WhatTheSourcesValidationNeverCheckedIsValidated pins what a $ref
// naming the source's file reaches that the source's own check never read:
// raw YAML, such as an extension's value. It is checked as an object
// in another document is, whether the $ref is the source's own or comes back
// into it from another document (GitHub #759), and its finding is placed at the
// node it is about (GitHub #879). A node of the grammar a schema $ref names, or
// raw YAML that is no schema, is not read as a schema, so it is reported
// unresolved, once (GitHub #769), and nothing in it is validated as one.
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
`, []string{"/x-defs/S/minLength openapi/validation/validation-type-mismatch"}},
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
`, []string{"/x-responses/R/links/L openapi/validation/validation-mutually-exclusive-fields"},
		},
		{"a response named as a schema", nil, header + `paths: {}
components:
  schemas:
    S: {$ref: 'root.yaml#/components/responses/R'}
  responses:
    R: {description: ok, minLength: -1}
`, []string{"/components/schemas/S openapi/unresolved-ref"}},
		{"raw YAML holding no schema", nil, header + `paths: {}
components:
  schemas:
    A: {$ref: '#/x-lib/S'}
    B: {$ref: '#/x-lib/N'}
x-lib:
  S: just a string
  N: null
`, []string{"/components/schemas/A openapi/unresolved-ref", "/components/schemas/B openapi/unresolved-ref"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, diags := loadExternal(t, externalDir(t, c.files), c.root, Options{})

			assert.Empty(t, cmp.Diff(c.want, diagLines(diags)))
		})
	}
}

// TestResolve_AComponentTheYAMLWritesAsNoSchemaIsTheParsers pins that a
// component the YAML writes as no schema, here a string, holds the schema the
// parser built there, as the lowering reads it: a $ref to it names it, and the
// rule table's finding that it is no schema is the one report, where raw YAML
// holding no schema is reported unresolved (GitHub #769).
func TestResolve_AComponentTheYAMLWritesAsNoSchemaIsTheParsers(t *testing.T) {
	t.Parallel()
	_, diags := loadExternal(t, t.TempDir(), `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: '#/components/schemas/S'}
    S: just a string
`, Options{})

	assert.Empty(t, cmp.Diff([]string{"/components/schemas/S openapi/validation/validation-type-mismatch"},
		diagLines(diags)), "the table's finding, and no unresolved $ref")
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

	assert.Empty(t, cmp.Diff([]string{
		"/components/headers/H/schema/minLength openapi/validation/validation-type-mismatch",
	}, diagLines(diags)), "the source's own finding, once, as the header it declares: the $ref's reading as a parameter adds none")
}

// TestResolve_EveryKindOfObjectIsValidated reaches each kind of object a $ref
// names that the other tests do not, a header, request body, callback,
// example, link and security scheme, each with a defect only its own table
// entry finds. Each finding is placed in other.yaml's entry at the node it is
// about, under the rule and severity the table gives it.
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
		got = append(got, fmt.Sprintf("%d %s %s %s", d.Provenance.Source, d.Provenance.Pointer, d.Severity, d.Code))
	}
	want := []string{
		"1 /components/securitySchemes/S/type error openapi/validation/validation-allowed-values",
		"1 /components/callbacks/C/{$bogus error openapi/validation/validation-invalid-format",
		"1 /components/requestBodies/B/content/application~1json/schema/minLength error " +
			"openapi/validation/validation-type-mismatch",
		"1 /components/examples/E error openapi/validation/validation-mutually-exclusive-fields",
		"1 /components/headers/H/style error openapi/validation/validation-allowed-values",
		"1 /components/links/L error openapi/validation/validation-mutually-exclusive-fields",
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("findings (-want +got):\n%s", d)
	}
}

// TestResolve_AnObjectReachedTwiceIsReportedOnce covers the ways one node can
// be reached more than once: two $refs to one component, a container reached
// beside something inside it, at the lesser $ref and at the greater, and one
// node read as two kinds. Each finding is placed at its node in other.yaml,
// whichever $ref is declared first. Inside a container, an object is validated
// twice, and charged twice, and its finding reported once.
func TestResolve_AnObjectReachedTwiceIsReportedOnce(t *testing.T) {
	t.Parallel()
	compile := func(t *testing.T, other, root string, opts Options) []ir.Diagnostic {
		t.Helper()
		_, diags := loadExternal(t, externalDir(t, map[string]string{"other.yaml": other}), root, opts)
		return diags
	}

	t.Run("two refs to one component report once, at the node", func(t *testing.T) {
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
		assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/parameters/P/in",
			Position: ir.Position{Line: 6, Column: 22}}, diags[0].Provenance)
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

	t.Run("a container at the lesser $ref and what it holds report once", func(t *testing.T) {
		t.Parallel()
		diags := compile(t, other, containedFirst, Options{})

		require.Len(t, diags, 1, "the parameter the container holds draws it again, and that copy is dropped: %+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1x/get/parameters/0/in"), diags[0].Provenance.Pointer)
	})

	t.Run("what a container holds is charged again", func(t *testing.T) {
		t.Parallel()
		diags := compile(t, nestedOtherFixture(), containedFirst, Options{MaxSourceNodes: 150, MaxAliasSurplus: 1})

		require.Len(t, diags, 2, "the container fits this budget and its parameter, charged again, "+
			"does not: %+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1x/get/parameters/0/in"), diags[0].Provenance.Pointer)
		assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)
		assert.Equal(t, jsontext.Pointer("/paths/~1b/get/parameters/0"), diags[1].Provenance.Pointer)
		assert.Equal(t, diag.BudgetExceeded, diags[1].Code)
	})

	t.Run("what a container spans but does not read is validated through its own $ref", func(t *testing.T) {
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
		assert.Equal(t, jsontext.Pointer("/paths/~1x/x-shared/in"), diags[0].Provenance.Pointer)
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
		const minLength = "/components/responses/R/content/application~1json/schema/minLength"
		require.Len(t, sites[minLength], 1, "read as a response: %+v", diags)
		assert.Contains(t, sites[minLength][0], `Schema Object field "minLength"`)
		assert.NotEmpty(t, sites["/components/responses/R/type"], "read as a schema, type: 42 is a finding: %+v", diags)
	})

	t.Run("a contained object at the lesser $ref and its container report once", func(t *testing.T) {
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

		require.Len(t, diags, 1, "the container draws it again, and that copy is dropped: %+v", diags)
		assert.Equal(t, jsontext.Pointer("/paths/~1x/get/parameters/0/in"), diags[0].Provenance.Pointer)
	})
}

// TestResolve_ReachedSitesDoNotDependOnDeclarationOrder compiles one source as
// written and with its paths and parameters reversed, and requires the same
// reports, each at its node in other.yaml whichever $ref reached it first. The
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
		"openapi/validation/validation-allowed-values /components/parameters/P/in",    // P: /a and /b reach it
		"openapi/validation/validation-allowed-values /components/parameters/P2/in",   // P2: Q chains to R's target
		"openapi/validation/validation-allowed-values /paths/~1x/get/parameters/0/in", // q: held by /d, reached at /c
		"openapi/validation/validation-allowed-values /paths/~1y/get/parameters/0/in", // r: /e holds what /f reaches
		"openapi/validation/validation-required-field /components/parameters/P2",      // P2 writes no name
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

// v32OtherFixture and v32RootFixture are a 3.2 root reaching two external
// schemas, one writing a keyword only 3.2 defines (Pet's defaultMapping) and
// one with a defect (Broken: {type: 42}).
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

// TestResolve_AReachedSchemaIsHeldToTheSourcesMinor: Pet's defaultMapping is a
// keyword 3.2 defines, so S, reached from a 3.2 source, reports nothing, while
// the type: 42 of Broken, which B reaches, is reported beside it. The second case reaches the same keyword
// nested in a path item rather than as the object itself.
func TestResolve_AReachedSchemaIsHeldToTheSourcesMinor(t *testing.T) {
	t.Parallel()

	t.Run("a schema reached directly", func(t *testing.T) {
		t.Parallel()
		dir := externalDir(t, map[string]string{"other.yaml": v32OtherFixture})

		_, diags := loadExternal(t, dir, v32RootFixture, Options{})

		for _, d := range diags {
			assert.Equal(t, jsontext.Pointer("/components/schemas/Broken/type"), d.Provenance.Pointer,
				"S's defaultMapping is a 3.2 keyword and reports nothing: %+v", d)
		}
		assert.NotEmpty(t, diags, "Broken's type: 42 is a defect")
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

		assert.Empty(t, diags, "the nested defaultMapping is a 3.2 keyword too: %+v", diags)
	})
}

// TestResolve_AReached30SchemaIsCheckedAsTheSourcesAre compiles one 3.0 schema
// in the source, reached directly, and reached inside a parameter, and each
// draws the same two findings, as 3.0 words them: its type list, which 3.0
// does not admit (3.0.4 §Schema Object: type "Value MUST be a string"), and its
// negative minLength. examples is not a 3.0 keyword, and draws none.
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

			require.Len(t, diags, 2, "%+v", diags)
			for _, d := range diags {
				assert.Equal(t, diag.Validation+"/validation-type-mismatch", d.Code)
			}
			assert.Contains(t, diags[0].Message, `Schema Object field "minLength" must be a non-negative integer in OpenAPI 3.0`)
			assert.Contains(t, diags[1].Message, `Schema Object field "type" must be a string in OpenAPI 3.0`)
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
// reached from /a, the path item at /b, which charges those nodes again, is refused
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
		assert.Equal(t, jsontext.Pointer("/paths/~1x/get/parameters/0/in"), diags[0].Provenance.Pointer)
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
			assert.Equal(t, jsontext.Pointer("/paths/~1x/get/parameters/0/in"), diags[0].Provenance.Pointer)
			assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)
			assert.Equal(t, jsontext.Pointer("/components/responses/Small/content/application~1json/schema/minLength"),
				diags[1].Provenance.Pointer)
			assert.Equal(t, diag.Validation+"/validation-type-mismatch", diags[1].Code)
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
// diagnostics: each finding once, placed in other.yaml at its node.
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
		_, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})
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
	made := map[string]bool{}
	recovered := 0
	for _, d := range rebuilt {
		assert.Equal(t, 1, d.Provenance.Source, "placed in other.yaml, where its node is: %+v", d)
		key := fmt.Sprint(d.Code, d.Provenance, d.Message)
		assert.False(t, made[key], "made once: %+v", d)
		made[key] = true
		assert.False(t, strings.HasPrefix(string(d.Provenance.Pointer), "/paths/~1unreached"),
			"/unreached must not be validated: %+v", d)
		if d.Provenance.Position == (ir.Position{Line: 8, Column: 25}) {
			recovered++
			assert.Equal(t, jsontext.Pointer("/paths/~1x/get/parameters/0/in"), d.Provenance.Pointer)
		}
	}
	assert.Equal(t, 1, recovered, "the recovered get's own finding, exactly once: %+v", rebuilt)
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
		invalid  = "openapi/validation/validation-type-mismatch"
		mismatch = "openapi/validation/validation-type-mismatch"
		minimum  = `Schema Object field "minLength" must be a non-negative integer in OpenAPI 3.1 (Schema Object)`
		inValues = `Parameter Object field "in" must be one of "query", "header", "path", "cookie" in OpenAPI 3.1 (Parameter Object)`
		noIn     = `Parameter Object field "in" is REQUIRED in OpenAPI 3.1 (Parameter Object)`
		noName   = `Security Scheme Object field "name" is REQUIRED when type=apiKey in OpenAPI 3.1 (Security Scheme Object)`
		noURL    = `Security Scheme Object field "openIdConnectUrl" is REQUIRED when type=openIdConnect in OpenAPI 3.1 (Security Scheme Object)`
		noFlows  = `Security Scheme Object field "flows" is REQUIRED when type=oauth2 in OpenAPI 3.1 (Security Scheme Object)`
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
			{"/x-params/q", required, noIn, "{name: q"},
			{"/x-schemes/key", required, noName, "{type: apiKey"},
			{"/x-schemes/oidc", required, noURL, "{type: openIdConnect}"},
			{"/x-schemes/oauth", required, noFlows, "{type: oauth2}"},
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
			"a type mismatch is told once",
			mappingSpec("    S: {$ref: '#/x-lib/A'}\n", "x-lib:\n  A: {type: string, minLength: abc}\n"),
			[]inSourceFinding{{"/x-lib/A/minLength", mismatch, minimum, "abc"}},
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
			want := []string{"/x-lib/Bad/minLength openapi/validation/validation-type-mismatch"}

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
		root, _, err := decodeStream(t.Context(), []byte(spec))
		require.NoError(t, err)
		nodes := nodeCount(t.Context(), root)
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
// item's method or a callback's expression, and an additionalOperations'
// operations checked. An object
// reached both itself and inside another reached object draws each warning
// once. A 3.2 document draws none; another document's object is held to the
// source's version (GitHub #839), each warning placed at its pointer there.
func TestResolve_AReachedObjectIsHeldToTheLaterVersionTable(t *testing.T) {
	t.Parallel()
	const later = " openapi/later-version-construct"
	pet := "x-lib:\n  Pet: {type: object, discriminator: {propertyName: k, defaultMapping: '#/components/schemas/Cat'}}\n"
	cases := []struct {
		name, version, body string
		want                []string
	}{
		{"a schema keyword", "3.1.0",
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

	t.Run("another document's object is held to the source's version", func(t *testing.T) {
		t.Parallel()
		dir := externalDir(t, map[string]string{"other.yaml": pet})
		_, diags := loadExternal(t, dir, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"+
			"components:\n  schemas:\n    Ref: {$ref: 'other.yaml#/x-lib/Pet'}\n    Cat: {type: object}\n", Options{})
		assert.Empty(t, cmp.Diff([]string{"/x-lib/Pet/discriminator/defaultMapping" + later}, diagLines(diags)), "%+v", diags)
		require.Len(t, diags, 1)
		assert.Equal(t, 1, diags[0].Provenance.Source, "placed in other.yaml's entry")
	})
}
