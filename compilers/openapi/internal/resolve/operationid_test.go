package resolve_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// TestOperationIDAt_ReadsTheOperationWrittenAtAPointer pins the read a mounted
// request body is named by: the operationId of the operation written at a
// pointer, as a reference to its body resolves there. Raw YAML answers where a
// mapping is written, a path item's $ref siblings and aliases included, and
// the model only where nothing is, through a reference. Any scalar but null
// reads as its text; nothing else names one.
func TestOperationIDAt_ReadsTheOperationWrittenAtAPointer(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\nx-op: &op {operationId: aliased}\n" +
		"x-item: &item {post: {operationId: viaItem}}\nx-name: &name named\npaths:\n" +
		"  /a: {post: {operationId: postA}, get: {summary: s}, put: {operationId: 7}, patch: {operationId: ~}}\n" +
		"  /b: {$ref: '#/components/pathItems/P', put: {operationId: besidePut}, post: {operationId: besidePost}}\n" +
		"  /c: {$ref: '#/components/pathItems/P'}\n" +
		"  /d: {get: *op, post: {<<: *op}, put: {operationId: *name}}\n  /f: *item\n" +
		"components:\n  pathItems:\n    P: {put: {operationId: putP}}\n"
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	var file yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(spec), &file))
	raw := file.Content[0]
	for pointer, want := range map[jsontext.Pointer]string{
		"/paths/~1a/post":             "postA",
		"/components/pathItems/P/put": "putP",
		"/paths/~1a/put":              "7",
		"/paths/~1b/post":             "besidePost",
		"/paths/~1b/put":              "besidePut",
		"/paths/~1c/put":              "putP",
		"/paths/~1d/get":              "aliased",
		"/paths/~1d/post":             "aliased",
		"/paths/~1f/post":             "viaItem",
		"/paths/~1d/put":              "named",
		"/x-name":                     "",
		"/paths/~1a":                  "",
		"/paths/~1a/get":              "",
		"/paths/~1a/patch":            "",
		"/paths/~1a/post/operationId": "",
		"/paths/~1e/post":             "",
		"/paths/~1a/post/~2":          "",
	} {
		assert.Equal(t, want, resolve.OperationIDAt(raw, doc.Doc, pointer), pointer)
	}
	assert.Equal(t, "putP", resolve.OperationIDAt(nil, doc.Doc, "/paths/~1c/put"), "the model alone")
	assert.Equal(t, "besidePut", resolve.OperationIDAt(raw, nil, "/paths/~1b/put"), "the YAML alone")
	assert.Empty(t, resolve.OperationIDAt(raw, nil, "/paths/~1c/put"), "a reference only the model follows")
	assert.Empty(t, resolve.OperationIDAt(nil, nil, "/paths/~1a/post"))
}
