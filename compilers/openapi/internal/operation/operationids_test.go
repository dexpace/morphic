package operation_test

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

const duplicateOperationIDSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: '#/components/pathItems/Shared'}
components:
  pathItems:
    Shared:
      get:
        operationId: sharedGet
        responses: {"200": {description: ok}}
`

// TestOperations_DuplicateOperationIDReported covers the collision a shared path
// item creates without the document repeating anything: one operationId written
// once, mounted twice, so two operations claim it. OpenAPI requires the id to be
// unique across the API, and nothing upstream can see this — the resolver reads
// one declaration.
func TestOperations_DuplicateOperationIDReported(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, duplicateOperationIDSpec)
	opA := opByPath(t, doc, "GET", "/a")
	opB := opByPath(t, doc, "GET", "/b")
	assert.Equal(t, opA.Name.Source, opB.Name.Source, "the IR still records what the document said")

	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DuplicateOperationID, ir.SeverityWarning),
		"the second claim is reported once, not both claims")
	for _, d := range diags {
		if d.Code == diag.DuplicateOperationID {
			assert.Equal(t, jsontext.Pointer("/paths/~1b/get"), d.Provenance.Pointer, "reported at the mount that collided")
			assert.Contains(t, d.Message, "/paths/~1a/get", "and names the mount that claimed it first")
		}
	}
}

// TestOperations_DistinctOperationIDsClean is the control: ordinary operations
// with their own ids, and an operation with none at all, raise nothing.
func TestOperations_DistinctOperationIDsClean(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.PathsSpec(`  /a:
    get:
      operationId: getA
      responses: {"200": {description: ok}}
  /b:
    get:
      responses: {"200": {description: ok}}
`))
	assert.False(t, openapitest.HasDiag(diags, diag.DuplicateOperationID))
}

// renderOperationIDDiags renders each operationId diagnostic in diags as
// "severity code pointer", sorted. Pinning the three together in one string is
// what shows a table row's severity, code and location cannot drift apart, and
// rendering every validation finding beside the compiler's two codes is what
// shows the rule table, which leaves operationId uniqueness to this package,
// adds none.
func renderOperationIDDiags(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Code == diag.DuplicateOperationID || d.Code == diag.ConflictingOperationID ||
			strings.HasPrefix(d.Code, diag.Validation) {
			out = append(out, fmt.Sprintf("%s %s %s", d.Severity, d.Code, d.Provenance.Pointer))
		}
	}
	slices.Sort(out)
	return out
}

// TestOperations_OperationIDUniqueness is the end-to-end table over operationId
// reuse. One declaration mounted more than once, by $ref, YAML alias or merge
// key, warns at every mount but the one it is written at, else the first by
// pointer. A second declaration writing the same id is an error where it is
// written, against a path, callback, webhook or component alike.
//
// Several rows name their paths so the mount a declaration is written at sorts
// after the one reusing it; pointer order alone would warn at the declaration
// there and anchor a conflict at an alias that writes no id.
func TestOperations_OperationIDUniqueness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		spec string
		want []string
	}{
		{
			name: "two refs mount one component declaration twice",
			spec: duplicateOperationIDSpec,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a ref to a path mounts its declaration twice (GitHub #502)",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: "#/paths/~1b"}
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1a/get"},
		},
		{
			name: "a path-item alias mounts one declaration twice (GitHub #502)",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b: *item
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a path-item alias sorting before its anchor is the mount warned",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /z: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /a: *item
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1a/get"},
		},
		{
			name: "two path declarations genuinely repeat the id",
			spec: openapitest.PathsSpec(`  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`),
			want: []string{"error openapi/conflicting-operation-id /paths/~1b/get"},
		},
		{
			name: "an operation alias mounts one declaration twice across paths",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get: &op
      operationId: dup
      responses: {"200": {description: ok}}
  /b:
    get: *op
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "an operation alias sorting before its anchor is the mount warned",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /z:
    get: &op
      operationId: dup
      responses: {"200": {description: ok}}
  /a:
    get: *op
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1a/get"},
		},
		{
			name: "an operation alias mounts one declaration twice across methods",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get: &op
      operationId: dup
      responses: {"200": {description: ok}}
    put: *op
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1a/put"},
		},
		{
			name: "a merge key mounts one declaration twice",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b:
    <<: *item
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a merge key sorting before its anchor is the mount warned",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /z: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /a:
    <<: *item
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1a/get"},
		},
		{
			// The pair /b writes itself overrides the one the merge key names, so
			// /b declares an operation of its own that repeats the id.
			name: "an operation overriding the one a merge key names is a second declaration",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b:
    <<: *item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
`,
			want: []string{"error openapi/conflicting-operation-id /paths/~1b/get"},
		},
		{
			name: "a two-deep ref chain mounts one declaration twice",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: '#/paths/~1a'}
components:
  pathItems:
    Shared:
      get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a callback operation genuinely repeats a path operation's id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post:
      operationId: dup
      responses: {"200": {description: ok}}
  /b:
    post:
      operationId: parent
      callbacks:
        onEvent:
          '{$request.body#/url}':
            post: {operationId: dup, responses: {"200": {description: ok}}}
      responses: {"200": {description: ok}}
`,
			want: []string{
				"error openapi/conflicting-operation-id /paths/~1b/post/callbacks/onEvent/{$request.body#~1url}/post",
			},
		},
		{
			name: "a webhook genuinely repeats a path operation's id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post: {operationId: dup, responses: {"200": {description: ok}}}
webhooks:
  hook:
    post: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"error openapi/conflicting-operation-id /webhooks/hook/post"},
		},
		{
			name: "a webhook reusing a path item by ref mounts its declaration twice",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post: {operationId: dup, responses: {"200": {description: ok}}}
webhooks:
  hook: {$ref: '#/paths/~1a'}
`,
			want: []string{"warning openapi/duplicate-operation-id /webhooks/hook/post"},
		},
		{
			name: "two webhooks genuinely repeat one id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  h1:
    post: {operationId: dup, responses: {"200": {description: ok}}}
  h2:
    post: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"error openapi/conflicting-operation-id /webhooks/h2/post"},
		},
		{
			name: "an alias between two components mounts one declaration twice",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/One'}
  /b: {$ref: '#/components/pathItems/Two'}
components:
  pathItems:
    One: &shared
      get: {operationId: dup, responses: {"200": {description: ok}}}
    Two: *shared
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "two components each ref'd once genuinely repeat the id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/One'}
  /b: {$ref: '#/components/pathItems/Two'}
components:
  pathItems:
    One:
      get: {operationId: dup, responses: {"200": {description: ok}}}
    Two:
      get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"error openapi/conflicting-operation-id /components/pathItems/Two/get"},
		},
		{
			name: "an alias remount and a genuine repeat both name one id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b: *item
  /c:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{
				"error openapi/conflicting-operation-id /paths/~1c/get",
				"warning openapi/duplicate-operation-id /paths/~1b/get",
			},
		},
		{
			// /z writes the second declaration and /b only aliases it, so the
			// conflict belongs at /z even though /b sorts first of the two.
			name: "a repeat is reported where it is written, not at an alias of it",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /z: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b: *item
`,
			want: []string{
				"error openapi/conflicting-operation-id /paths/~1z/get",
				"warning openapi/duplicate-operation-id /paths/~1b/get",
			},
		},
		{
			// Both parents $ref one callback component, so its operation is one
			// declaration the two of them mount, not a declaration of each.
			name: "a callback ref'd from two parent operations mounts one declaration twice",
			spec: sharedCallbackSpec,
			want: []string{
				"warning openapi/duplicate-operation-id /paths/~1d/post/callbacks/onEvent/{$request.body#~1url}/post",
			},
		},
		{
			// /b declares get and put sharing one id, and /a mounts both again by
			// referencing /b whole. get and put are two declarations, so besides
			// the remount they conflict with each other: an error where put writes
			// the id, and a warning at each of /a's mounts, where the reuse is.
			name: "a ref'd item declaring get and put that share an id",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
    put: {operationId: dup, responses: {"200": {description: ok}}}
  /a: {$ref: '#/paths/~1b'}
`,
			want: []string{
				"error openapi/conflicting-operation-id /paths/~1b/put",
				"warning openapi/duplicate-operation-id /paths/~1a/get",
				"warning openapi/duplicate-operation-id /paths/~1a/put",
			},
		},
		{
			name: "an additionalOperations entry genuinely repeats a method's id",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
    additionalOperations:
      COPY: {operationId: dup, responses: {"200": {description: ok}}}
`,
			want: []string{"error openapi/conflicting-operation-id /paths/~1a/get"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, tc.spec)
			assert.Empty(t, cmp.Diff(tc.want, renderOperationIDDiags(diags)))
		})
	}
}

// TestOperations_SelfReferencesByFileName pins the reuse a reference naming
// this document by its file name makes. The compiler reads it as internal, so
// its claim has the plain reference's pointer.
//
// The last case is a self-reference the compiler does not read as one, since
// its document part carries a directory (GitHub #576), so its pointer is
// another. The resolver answers it from the source as held (GitHub #759), so
// it shares the declaring node, and that is what makes it a reuse.
//
// A reader is needed because the resolver counts a document part as leaving
// the document.
func TestOperations_SelfReferencesByFileName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		paths string
		want  []string
	}{
		{
			name: "two spellings of one component",
			paths: `  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: 'self.yaml#/components/pathItems/Shared'}
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a path and a self-reference to it",
			paths: `  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /b: {$ref: 'self.yaml#/paths/~1a'}
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
		{
			name: "a self-reference through a directory, read as another document's pointer (GitHub #576)",
			paths: `  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: './self.yaml#/components/pathItems/Shared'}
`,
			want: []string{"warning openapi/duplicate-operation-id /paths/~1b/get"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
` + tc.paths + `components:
  pathItems:
    Shared:
      get: {operationId: dup, responses: {"200": {description: ok}}}
`
			path := filepath.Join(t.TempDir(), "self.yaml")
			require.NoError(t, os.WriteFile(path, []byte(spec), 0o600))

			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: path, Data: []byte(spec)}},
				compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})

			require.NoError(t, err)
			require.NotNil(t, doc, "compile refused: %+v", diags)
			assert.Empty(t, cmp.Diff(tc.want, renderOperationIDDiags(diags)))
		})
	}
}

// TestOperations_OperationIDFindingsIgnoreDeclarationOrder is the two-order
// half of the table above: harness.Check stops at the first error diagnostic,
// so its order-invariance oracle never reaches conflicting-operation-id, and a
// hand-written case is the only thing that can pin it. Each pair declares the
// same document with two of its entries swapped, the first in an order the
// check this replaced got wrong: it reported whichever claim was lowered
// second, so a document and its reverse named different operations.
func TestOperations_OperationIDFindingsIgnoreDeclarationOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		first, then string
	}{
		{
			name: "two genuinely repeated path operations",
			first: openapitest.PathsSpec(`  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`),
			then: openapitest.PathsSpec(`  /a:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`),
		},
		{
			name: "two webhooks genuinely repeating one id",
			first: `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  h2:
    post: {operationId: dup, responses: {"200": {description: ok}}}
  h1:
    post: {operationId: dup, responses: {"200": {description: ok}}}
`,
			then: `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  h1:
    post: {operationId: dup, responses: {"200": {description: ok}}}
  h2:
    post: {operationId: dup, responses: {"200": {description: ok}}}
`,
		},
		{
			name: "a ref remounting one component at two paths",
			first: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b: {$ref: '#/components/pathItems/Shared'}
  /a: {$ref: '#/components/pathItems/Shared'}
components:
  pathItems:
    Shared:
      get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			then: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: '#/components/pathItems/Shared'}
components:
  pathItems:
    Shared:
      get: {operationId: dup, responses: {"200": {description: ok}}}
`,
		},
		{
			name: "a ref remounting the path it names",
			first: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/paths/~1b'}
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
`,
			then: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    get: {operationId: dup, responses: {"200": {description: ok}}}
  /a: {$ref: '#/paths/~1b'}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, firstDiags := parseFull(t, tc.first)
			_, thenDiags := parseFull(t, tc.then)
			first := renderOperationIDDiags(firstDiags)
			require.NotEmpty(t, first, "the case reports something to compare")
			assert.Empty(t, cmp.Diff(first, renderOperationIDDiags(thenDiags)))
		})
	}
}
