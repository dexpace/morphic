package load

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// paramRepeatComponents are the components every case below draws on: two
// parameters holding one slot, and a third holding another.
const paramRepeatComponents = `components:
  parameters:
    Limit: {name: limit, in: query, schema: {type: integer}}
    Limit2: {name: limit, in: query, schema: {type: string}}
    Other: {name: other, in: query, schema: {type: string}}
`

// repeatsAt loads src and returns each openapi/duplicate-parameter it draws,
// keyed by the pointer it is reported at, with the message as the value.
func repeatsAt(t *testing.T, src string) map[jsontext.Pointer]string {
	t.Helper()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, Options{})
	require.NoError(t, err)
	got := map[jsontext.Pointer]string{}
	for _, d := range diags {
		assert.NotEqual(t, diag.Validation+"/validation-operation-parameters", d.Code, "the library's report is dropped")
		if d.Code == diag.DuplicateParameter {
			assert.Equal(t, ir.SeverityError, d.Severity)
			got[d.Provenance.Pointer] = d.Message
		}
	}
	return got
}

// TestParameterRepeats_ReportsEveryListTheSourceWrites covers each place a
// parameter list is written, lowered or not: a path item's entries an operation
// shadows, a path item with no operation, a component path item nothing
// mounts, a component callback, and a webhook. Each repeat is reported once, at
// itself, naming the first entry holding its slot, whether its entries are
// inline or $refs.
func TestParameterRepeats_ReportsEveryListTheSourceWrites(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	tests := []struct {
		name string
		src  string
		want map[jsontext.Pointer]string
	}{
		{
			name: "path-item entries an operation shadows",
			src: head + `paths:
  /x:
    parameters:
      - $ref: "#/components/parameters/Limit"
      - $ref: "#/components/parameters/Limit2"
    get:
      parameters:
        - {name: limit, in: query, schema: {type: boolean}}
      responses: {"200": {description: ok}}
` + paramRepeatComponents,
			want: map[jsontext.Pointer]string{
				"/paths/~1x/parameters/1": `parameter "limit" in "query" repeats the entry at /paths/~1x/parameters/0`,
			},
		},
		{
			name: "a path item with no operation",
			src: head + `paths:
  /x:
    parameters:
      - $ref: "#/components/parameters/Limit"
      - {name: limit, in: query, schema: {type: string}}
` + paramRepeatComponents,
			want: map[jsontext.Pointer]string{
				"/paths/~1x/parameters/1": `parameter "limit" in "query" repeats the entry at /paths/~1x/parameters/0`,
			},
		},
		{
			name: "a component path item nothing mounts, and its operation",
			src: head + "paths: {}\n" + paramRepeatComponents + `  pathItems:
    Unused:
      parameters:
        - {name: a, in: header, schema: {type: string}}
        - {name: a, in: header, schema: {type: string}}
      get:
        parameters:
          - $ref: "#/components/parameters/Other"
          - $ref: "#/components/parameters/Limit"
          - $ref: "#/components/parameters/Limit2"
          - $ref: "#/components/parameters/Limit"
        responses: {"200": {description: ok}}
`,
			want: map[jsontext.Pointer]string{
				"/components/pathItems/Unused/parameters/1":     `parameter "a" in "header" repeats the entry at /components/pathItems/Unused/parameters/0`,
				"/components/pathItems/Unused/get/parameters/2": `parameter "limit" in "query" repeats the entry at /components/pathItems/Unused/get/parameters/1`,
				"/components/pathItems/Unused/get/parameters/3": `parameter "limit" in "query" repeats the entry at /components/pathItems/Unused/get/parameters/1`,
			},
		},
		{
			name: "a component callback and a webhook",
			src: head + `paths: {}
webhooks:
  hook:
    post:
      parameters:
        - $ref: "#/components/parameters/Limit"
        - $ref: "#/components/parameters/Limit"
      responses: {"200": {description: ok}}
` + paramRepeatComponents + `  callbacks:
    CB:
      "{$request.query.q}":
        parameters:
          - $ref: "#/components/parameters/Limit2"
          - $ref: "#/components/parameters/Limit"
`,
			want: map[jsontext.Pointer]string{
				"/webhooks/hook/post/parameters/1":                         `parameter "limit" in "query" repeats the entry at /webhooks/hook/post/parameters/0`,
				"/components/callbacks/CB/{$request.query.q}/parameters/1": `parameter "limit" in "query" repeats the entry at /components/callbacks/CB/{$request.query.q}/parameters/0`,
			},
		},
		{
			name: "a path item two paths mount, reported where it is written",
			src: head + `paths:
  /a: {$ref: "#/components/pathItems/Shared"}
  /b: {$ref: "#/components/pathItems/Shared"}
` + paramRepeatComponents + `  pathItems:
    Shared:
      parameters:
        - $ref: "#/components/parameters/Limit"
        - $ref: "#/components/parameters/Limit"
      get:
        responses: {"200": {description: ok}}
`,
			want: map[jsontext.Pointer]string{
				"/components/pathItems/Shared/parameters/1": `parameter "limit" in "query" repeats the entry at /components/pathItems/Shared/parameters/0`,
			},
		},
		{
			name: "one name in two locations, and a $ref that resolves to nothing",
			src: head + `paths:
  /x/{limit}:
    get:
      parameters:
        - {name: limit, in: path, required: true, schema: {type: string}}
        - $ref: "#/components/parameters/Limit"
        - $ref: "#/components/parameters/Missing"
        - $ref: "#/components/parameters/Missing"
      responses: {"200": {description: ok}}
` + paramRepeatComponents,
			want: map[jsontext.Pointer]string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, repeatsAt(t, tc.src))
		})
	}
}

// TestParameterRepeats_AWalkThatPanicsIsReported pins the barrier around the
// walk: the library's walk faults on shapes its parser accepts, and the panic
// is reported as an error rather than crashing the compile.
func TestParameterRepeats_AWalkThatPanicsIsReported(t *testing.T) {
	t.Parallel()
	panics := func(func(soa.WalkItem) bool) { panic("walk fault") }
	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }
	diags := parameterRepeats(at, Siblings{}, panics)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.Validation, diags[0].Code)
	assert.Contains(t, diags[0].Message, "parameter check panicked: walk fault")
}

// reservedAt loads src and returns each openapi/inapplicable-location-keyword
// it draws, keyed by the pointer it is reported at, with the message as the
// value. Each is a warning, and no pointer draws two.
func reservedAt(t *testing.T, src string) map[jsontext.Pointer]string {
	t.Helper()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, Options{})
	require.NoError(t, err)
	return reservedIn(t, diags)
}

// reservedIn is reservedAt over diagnostics already drawn.
func reservedIn(t *testing.T, diags []ir.Diagnostic) map[jsontext.Pointer]string {
	t.Helper()
	got := map[jsontext.Pointer]string{}
	for _, d := range diags {
		if d.Code != diag.InapplicableLocationKeyword {
			continue
		}
		assert.Equal(t, ir.SeverityWarning, d.Severity)
		assert.NotContains(t, got, d.Provenance.Pointer, "one declaration, one report")
		got[d.Provenance.Pointer] = d.Message
	}
	return got
}

// TestReservedApplies_FollowsEachMinorsText holds the predicate to the text of
// each minor at every location and the styles that differ there: before 3.2,
// in: query alone; in 3.2, every location and style that percent-encodes,
// which leaves out a header and a cookie in style cookie. A querystring is the
// lowering's to report, and an in OpenAPI does not define the parser's.
func TestReservedApplies_FollowsEachMinorsText(t *testing.T) {
	t.Parallel()
	const cookie = serializationStyleCookie
	tests := []struct {
		in    soa.ParameterIn
		style soa.SerializationStyle
		want  map[string]bool // by minor
	}{
		{soa.ParameterInQuery, soa.SerializationStyleForm, map[string]bool{"3.0": true, "3.1": true, "3.2": true}},
		{soa.ParameterInQuery, soa.SerializationStyleDeepObject, map[string]bool{"3.0": true, "3.1": true, "3.2": true}},
		{soa.ParameterInQueryString, "", map[string]bool{"3.0": true, "3.1": true, "3.2": true}},
		{soa.ParameterInPath, soa.SerializationStyleSimple, map[string]bool{"3.0": false, "3.1": false, "3.2": true}},
		{soa.ParameterInPath, soa.SerializationStyleMatrix, map[string]bool{"3.0": false, "3.1": false, "3.2": true}},
		{soa.ParameterInHeader, soa.SerializationStyleSimple, map[string]bool{"3.0": false, "3.1": false, "3.2": false}},
		{soa.ParameterInCookie, soa.SerializationStyleForm, map[string]bool{"3.0": false, "3.1": false, "3.2": true}},
		{soa.ParameterInCookie, cookie, map[string]bool{"3.0": false, "3.1": false, "3.2": false}},
		{"body", "", map[string]bool{"3.0": true, "3.1": true, "3.2": true}},
	}
	for _, tc := range tests {
		for _, minor := range supportedMinors {
			assert.Equal(t, tc.want[minor], reservedApplies(minor, tc.in, tc.style), "%s in=%s style=%s", minor, tc.in, tc.style)
		}
	}
}

// reservedVersionSpec writes one parameter, line, in an operation of a
// document declaring version.
func reservedVersionSpec(version, line string) string {
	return "openapi: " + version + `
info: {title: T, version: "1"}
paths:
  /p/{n}:
    get:
      parameters:
        - ` + line + `
      responses: {"200": {description: ok}}
`
}

// TestReservedFinding_ReportsWhereTheVersionGivesNoEffect pins what is reported
// and what is said, one row per version and location: presence rather than
// truth, so a declared false is reported, and the message names the location,
// the minor and that minor's own rule. The parser's refusal of style cookie
// (GitHub #389) stands beside the report and is not this rule's.
func TestReservedFinding_ReportsWhereTheVersionGivesNoEffect(t *testing.T) {
	t.Parallel()
	const (
		at      = "/paths/~1p~1{n}/get/parameters/0/allowReserved"
		query   = "which applies it to in=query alone; lowered as declared"
		encoded = "which applies it only where the value is percent-encoded; lowered as declared"
		schema  = ", schema: {type: string}}"
	)
	tests := []struct {
		version, line, want string
	}{
		{"3.0.3", "{name: n, in: path, required: true, allowReserved: true" + schema,
			"parameter field allowReserved has no effect for in=path in OpenAPI 3.0, " + query},
		{"3.0.3", "{name: h, in: header, allowReserved: true" + schema,
			"parameter field allowReserved has no effect for in=header in OpenAPI 3.0, " + query},
		{"3.1.0", "{name: c, in: cookie, allowReserved: false" + schema,
			"parameter field allowReserved has no effect for in=cookie in OpenAPI 3.1, " + query},
		{"3.1.1", "{name: c, in: cookie, style: cookie, allowReserved: true" + schema,
			"parameter field allowReserved has no effect for in=cookie in OpenAPI 3.1, " + query},
		{"3.0.3", "{name: q, in: query, allowReserved: true" + schema, ""},
		{"3.1.0", "{name: n, in: path, required: true" + schema, ""},
		{"3.2.0", "{name: h, in: header, allowReserved: false" + schema,
			"parameter field allowReserved has no effect for in=header in OpenAPI 3.2, " + encoded},
		{"3.2.0", "{name: c, in: cookie, style: cookie, allowReserved: true" + schema,
			"parameter field allowReserved has no effect for in=cookie with style=cookie in OpenAPI 3.2, " + encoded},
		{"3.2.0", "{name: n, in: path, required: true, style: label, allowReserved: true" + schema, ""},
		{"3.2.0", "{name: c, in: cookie, allowReserved: true" + schema, ""},
		{"3.2.0", "{name: q, in: query, style: deepObject, allowReserved: true, schema: {type: object}}", ""},
		{"3.2.0", "{name: w, in: querystring, allowReserved: true, content: {text/plain: {schema: {type: string}}}}", ""},
	}
	for _, tc := range tests {
		t.Run(tc.version+" "+tc.line, func(t *testing.T) {
			t.Parallel()
			want := map[jsontext.Pointer]string{}
			if tc.want != "" {
				want[at] = tc.want
			}
			assert.Equal(t, want, reservedAt(t, reservedVersionSpec(tc.version, tc.line)))
		})
	}
}

// reservedSitesSpec declares allowReserved outside query at every place a
// Parameter Object can be written: a component two operations reach and one
// nothing reaches, a path item's list and the list beside a path item $ref, a
// path item component, a callback, a webhook, and an object under an x- key a
// $ref reaches, which a second $ref reaches inside another such object.
const reservedSitesSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters:
      - {name: h, in: header, allowReserved: true, schema: {type: string}}
    get:
      parameters:
        - $ref: '#/components/parameters/Shared'
        - $ref: '#/x-params/xp'
      responses: {"200": {description: ok}}
    post:
      parameters:
        - $ref: '#/components/parameters/Shared'
        - $ref: '#/x-paths/X/get/parameters/0'
      callbacks:
        cb:
          '{$request.body#/url}':
            post:
              parameters:
                - {name: p, in: path, required: true, allowReserved: true, schema: {type: string}}
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
  /b:
    $ref: '#/components/pathItems/PI'
    parameters:
      - {name: s, in: header, allowReserved: true, schema: {type: string}}
  /x:
    $ref: '#/x-paths/X'
webhooks:
  evt:
    post:
      parameters:
        - {name: w, in: header, allowReserved: false, schema: {type: string}}
      responses: {"200": {description: ok}}
components:
  parameters:
    Shared: {name: c, in: cookie, allowReserved: true, schema: {type: string}}
    Unused: {name: u, in: header, allowReserved: true, schema: {type: string}}
  pathItems:
    PI:
      get:
        parameters:
          - {name: k, in: cookie, allowReserved: true, schema: {type: string}}
        responses: {"200": {description: ok}}
x-params:
  xp: {name: xh, in: header, allowReserved: true, schema: {type: string}}
x-paths:
  X:
    get:
      parameters:
        - {name: xc, in: cookie, allowReserved: true, schema: {type: string}}
      responses: {"200": {description: ok}}
`

// TestReservedFinding_ReachesEveryPlaceAParameterIsWritten pins the reach of
// the rule: each Parameter Object is judged once, where it is written, whether
// anything lowers it or not and however many $refs reach it, never at a $ref.
func TestReservedFinding_ReachesEveryPlaceAParameterIsWritten(t *testing.T) {
	t.Parallel()
	got := reservedAt(t, reservedSitesSpec)
	assert.Equal(t, []jsontext.Pointer{
		"/components/parameters/Shared/allowReserved",
		"/components/parameters/Unused/allowReserved",
		"/components/pathItems/PI/get/parameters/0/allowReserved",
		"/paths/~1a/parameters/0/allowReserved",
		"/paths/~1a/post/callbacks/cb/{$request.body#~1url}/post/parameters/0/allowReserved",
		"/paths/~1b/parameters/0/allowReserved",
		"/webhooks/evt/post/parameters/0/allowReserved",
		"/x-params/xp/allowReserved",
		"/x-paths/X/get/parameters/0/allowReserved",
	}, slices.Sorted(maps.Keys(got)))
}

// TestReservedFinding_ARefIsNotJudgedOnceResolved pins the $ref guard where it
// has work to do. Load asks before references resolve, when a $ref holds no
// object; once resolved, it holds its target's, and judging it would report the
// target's keyword again at every $ref reaching it.
func TestReservedFinding_ARefIsNotJudgedOnceResolved(t *testing.T) {
	t.Parallel()
	loaded, _, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - $ref: '#/components/parameters/H'
      responses: {"200": {description: ok}}
components:
  parameters:
    H: {name: h, in: header, allowReserved: true, schema: {type: string}}
`)}, Options{})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }
	got := versionCheck(at, soa.Walk(t.Context(), loaded.Doc), loaded.Doc.OpenAPI)
	require.Len(t, got.diags, 1)
	assert.Equal(t, jsontext.Pointer("/components/parameters/H/allowReserved"), got.diags[0].Provenance.Pointer)
}

// TestReservedFinding_AnotherDocumentIsNotJudgedByTheRoots pins that a
// parameter another document holds is not held to the root's version: a cookie
// in form style is one 3.2 applies the keyword to, and a 3.1 root does not.
// That document's own version is GitHub #839's.
func TestReservedFinding_AnotherDocumentIsNotJudgedByTheRoots(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": `openapi: 3.2.0
info: {title: O, version: "1"}
paths: {}
components:
  parameters:
    C: {name: c, in: cookie, allowReserved: true, schema: {type: string}}
`})
	_, diags := loadExternal(t, dir, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - $ref: 'other.yaml#/components/parameters/C'
      responses: {"200": {description: ok}}
`, Options{})
	assert.Empty(t, reservedIn(t, diags))
}
