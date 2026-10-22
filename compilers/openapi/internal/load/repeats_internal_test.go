package load

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
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
		assert.False(t, strings.HasPrefix(d.Code, diag.Validation), "the table leaves the rule to this check: %+v", d)
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

// serverRepeatsAt loads src and returns each openapi/duplicate-server-name it
// draws, keyed by the pointer it is reported at, with the message as the value.
// A refused load fails the test rather than returning none: the check never
// ran on it.
func serverRepeatsAt(t *testing.T, src string) map[jsontext.Pointer]string {
	t.Helper()
	loaded, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, Options{})
	require.NoError(t, err)
	require.NotNil(t, loaded, "load refused the document: %+v", diags)
	got := map[jsontext.Pointer]string{}
	for _, d := range diags {
		if d.Code == diag.DuplicateServerName {
			assert.Equal(t, ir.SeverityWarning, d.Severity)
			got[d.Provenance.Pointer] = d.Message
		}
	}
	return got
}

// nameRepeat is the message a repeat of name reports, naming the first holder
// at first.
func nameRepeat(name, first string) string {
	return fmt.Sprintf("server name %q repeats the name at %s; both servers are kept, "+
		"and a consumer keying servers by name cannot tell them apart", name, first)
}

// TestServerRepeats_ReportsEveryListTheSourceWrites covers each way a servers
// list repeats a name and each place one is written, lowered or not: the
// document's, a path item's and an operation's, a component path item nothing
// mounts, a webhook, a component callback, and the fields beside a path item
// $ref. Each repeat is reported once, at its own name, naming the first entry
// holding it, at source indices. A 3.1 document is checked as well: its parser
// reads the name, and the lowering keeps it.
func TestServerRepeats_ReportsEveryListTheSourceWrites(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\n"
	const operation = `{responses: {"200": {description: ok}}}`
	tests := []struct {
		name string
		src  string
		want map[jsontext.Pointer]string
	}{
		{
			name: "two entries of the document's list",
			src: head + `servers:
  - {url: https://a.example.com, name: prod}
  - {url: https://b.example.com, name: prod}
paths: {}
`,
			want: map[jsontext.Pointer]string{"/servers/1/name": nameRepeat("prod", "/servers/0/name")},
		},
		{
			name: "three entries: one report per repeat, each naming the first",
			src: head + `servers:
  - {url: https://a.example.com, name: prod}
  - {url: https://b.example.com, name: prod}
  - {url: https://c.example.com, name: prod}
paths: {}
`,
			want: map[jsontext.Pointer]string{
				"/servers/1/name": nameRepeat("prod", "/servers/0/name"),
				"/servers/2/name": nameRepeat("prod", "/servers/0/name"),
			},
		},
		{
			name: "an alias repeating an entry, and a merge key carrying a name into two",
			src: head + `x-base: &b {name: edge}
servers:
  - &prod {url: https://a.example.com, name: prod}
  - *prod
  - {<<: *b, url: https://b.example.com}
  - {<<: *b, url: https://c.example.com}
paths: {}
`,
			want: map[jsontext.Pointer]string{
				"/servers/1/name": nameRepeat("prod", "/servers/0/name"),
				"/servers/3/name": nameRepeat("edge", "/servers/2/name"),
			},
		},
		{
			name: "an unnamed entry ahead, which keeps the source indices",
			src: head + `servers:
  - {url: https://a.example.com, name: ""}
  - {url: https://b.example.com, name: prod}
  - {url: https://c.example.com}
  - {url: https://d.example.com, name: prod}
paths: {}
`,
			want: map[jsontext.Pointer]string{"/servers/3/name": nameRepeat("prod", "/servers/1/name")},
		},
		{
			name: "a path item's list and its operation's",
			src: head + `paths:
  /x:
    servers:
      - {url: https://a.example.com, name: eu}
      - {url: https://b.example.com, name: eu}
    get:
      servers:
        - {url: https://c.example.com, name: us}
        - {url: https://d.example.com, name: us}
      responses: {"200": {description: ok}}
`,
			want: map[jsontext.Pointer]string{
				"/paths/~1x/servers/1/name":     nameRepeat("eu", "/paths/~1x/servers/0/name"),
				"/paths/~1x/get/servers/1/name": nameRepeat("us", "/paths/~1x/get/servers/0/name"),
			},
		},
		{
			name: "a component path item nothing mounts, a webhook and a component callback",
			src: head + `paths: {}
webhooks:
  hook:
    post:
      servers:
        - {url: https://a.example.com, name: w}
        - {url: https://b.example.com, name: w}
      responses: {"200": {description: ok}}
components:
  pathItems:
    Unused:
      servers:
        - {url: https://c.example.com, name: p}
        - {url: https://d.example.com, name: p}
  callbacks:
    CB:
      "{$request.query.q}":
        servers:
          - {url: https://e.example.com, name: c}
          - {url: https://f.example.com, name: c}
`,
			want: map[jsontext.Pointer]string{
				"/webhooks/hook/post/servers/1/name":                         nameRepeat("w", "/webhooks/hook/post/servers/0/name"),
				"/components/pathItems/Unused/servers/1/name":                nameRepeat("p", "/components/pathItems/Unused/servers/0/name"),
				"/components/callbacks/CB/{$request.query.q}/servers/1/name": nameRepeat("c", "/components/callbacks/CB/{$request.query.q}/servers/0/name"),
			},
		},
		{
			name: "a path item two paths mount, reported where it is written",
			src: head + `paths:
  /a: {$ref: "#/components/pathItems/Shared"}
  /b: {$ref: "#/components/pathItems/Shared"}
components:
  pathItems:
    Shared:
      servers:
        - {url: https://a.example.com, name: s}
        - {url: https://b.example.com, name: s}
      get: ` + operation + `
`,
			want: map[jsontext.Pointer]string{
				"/components/pathItems/Shared/servers/1/name": nameRepeat("s", "/components/pathItems/Shared/servers/0/name"),
			},
		},
		{
			name: "the fields beside a path item $ref and their operation, which an alias mounts again",
			src: head + `paths:
  /a: &use
    $ref: "#/components/pathItems/P"
    servers:
      - {url: https://a.example.com, name: b}
      - {url: https://b.example.com, name: b}
    get:
      servers:
        - {url: https://c.example.com, name: o}
        - {url: https://d.example.com, name: o}
      responses: {"200": {description: ok}}
  /b: *use
components:
  pathItems:
    P:
      put: ` + operation + `
`,
			want: map[jsontext.Pointer]string{
				"/paths/~1a/servers/1/name":     nameRepeat("b", "/paths/~1a/servers/0/name"),
				"/paths/~1a/get/servers/1/name": nameRepeat("o", "/paths/~1a/get/servers/0/name"),
			},
		},
		{
			name: "a 3.1 document",
			src: `openapi: 3.1.0
info: {title: T, version: "1"}
servers:
  - {url: https://a.example.com, name: prod}
  - {url: https://b.example.com, name: prod}
paths: {}
`,
			want: map[jsontext.Pointer]string{"/servers/1/name": nameRepeat("prod", "/servers/0/name")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, serverRepeatsAt(t, tc.src))
		})
	}
}

// TestServerRepeats_ReportsOnlyANameOneListRepeatsExactly is the boundary: an
// entry with no name or an empty one holds none, and a name may recur across
// lists, since a path item's or an operation's servers replace the ones above
// them rather than join them. Names equal only once canonicalized are the
// openapi package's TestDuplicateServerName_CanonicalCollisionsAreNotReported,
// which proves each collision is real.
func TestServerRepeats_ReportsOnlyANameOneListRepeatsExactly(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\n"
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "distinct names",
			src: head + `servers:
  - {url: https://a.example.com, name: prod}
  - {url: https://b.example.com, name: staging}
paths: {}
`,
		},
		{
			name: "entries with no name or an empty one",
			src: head + `servers:
  - {url: https://api.example.com/v1}
  - {url: https://api.example.com/v-1}
  - {url: https://a.example.com, name: ""}
  - {url: https://b.example.com, name: ""}
paths: {}
`,
		},
		{
			// Each list holds two entries, or it would be passed over unread.
			name: "one name in the lists of the document, a path item and two operations",
			src: head + `servers:
  - {url: https://a.example.com, name: prod}
  - {url: https://b.example.com, name: dev}
paths:
  /x:
    servers:
      - {url: https://c.example.com, name: eu}
      - {url: https://d.example.com, name: prod}
    get:
      servers:
        - {url: https://e.example.com, name: prod}
        - {url: https://f.example.com, name: us}
      responses: {"200": {description: ok}}
    put:
      servers:
        - {url: https://g.example.com, name: prod}
        - {url: https://h.example.com, name: dev}
      responses: {"200": {description: ok}}
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, serverRepeatsAt(t, tc.src))
		})
	}
}
