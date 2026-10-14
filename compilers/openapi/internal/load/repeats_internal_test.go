package load

import (
	"encoding/json/jsontext"
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

// TestListRepeats_AWalkThatPanicsIsReported pins the barrier around the walk:
// the library's walk faults on shapes its parser accepts, and the panic is
// reported as an error rather than crashing the compile.
func TestListRepeats_AWalkThatPanicsIsReported(t *testing.T) {
	t.Parallel()
	panics := func(func(soa.WalkItem) bool) { panic("walk fault") }
	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }
	diags := listRepeats(at, Siblings{}, panics)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.Validation, diags[0].Code)
	assert.Contains(t, diags[0].Message, "repeat check panicked: walk fault")
}
