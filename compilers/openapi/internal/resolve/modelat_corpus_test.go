package resolve_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// TestScope_ModelAt_AnswersAsTheWholeReadDoesAcrossTheCorpus holds ModelAt,
// which reads a pointer a token at a time, to one read of the whole pointer,
// for every position the corpus's specs hold: each place their model's walk
// reaches, each path through their tree, and each of those with a token
// appended that names nothing, an index, the empty key or an escape. A step
// that answers otherwise than the library's walk shows here, as the one past
// an operation's responses, which the model holds by value, did.
func TestScope_ModelAt_AnswersAsTheWholeReadDoesAcrossTheCorpus(t *testing.T) {
	t.Parallel()
	var files []string
	for _, pattern := range []string{"../../../../testdata/*/*.yaml", "../../../../testdata/*/*/*.yaml",
		"../../../../testdata/*/*.json", "../load/testdata/*.yaml"} {
		matches, err := filepath.Glob(pattern)
		require.NoError(t, err)
		files = append(files, matches...)
	}
	specs, schemas := 0, 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		got, _, err := load.Load(t.Context(), 0, compilers.Source{Path: file, Data: data}, load.Options{})
		if err != nil || got == nil {
			continue // not a document the loader builds a model of
		}
		specs++
		sc := resolve.Scope{Doc: got.Doc}
		for _, pointer := range positionsOf(t, got.Doc, data) {
			whole := wholeModelAt(got.Doc, pointer)
			if whole != nil {
				schemas++
			}
			assert.Same(t, whole, sc.ModelAt(pointer), "%s %q", file, pointer)
		}
	}
	assert.Greater(t, specs, 100, "the corpus is read")
	assert.Greater(t, schemas, 500, "and its pointers reach schemas, not only nothing")
}

// positionsOf returns the pointers asked of a spec: where its model's walk
// goes, every path through its tree, and each with a token appended.
func positionsOf(t *testing.T, doc *soa.OpenAPI, data []byte) []jsontext.Pointer {
	t.Helper()
	var bases []jsontext.Pointer
	for item := range soa.Walk(t.Context(), doc) {
		bases = append(bases, jsontext.Pointer(item.Location.ToJSONPointer()))
	}
	var tree yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &tree))
	treePaths(&tree, "", &bases, 0)
	seen := map[jsontext.Pointer]bool{}
	var out []jsontext.Pointer
	for _, base := range bases {
		for _, extra := range []jsontext.Pointer{"", "/0", "/x", "/", "/~1", "/properties"} {
			if p := base + extra; !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// treePaths appends the pointer of each node in the tree under n, below at,
// to a depth and a count that keep the largest spec quick to read.
func treePaths(n *yaml.Node, at jsontext.Pointer, out *[]jsontext.Pointer, depth int) {
	if n == nil || depth > 12 || len(*out) > 20000 {
		return
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			treePaths(c, at, out, depth)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			p := at.AppendToken(n.Content[i].Value)
			*out = append(*out, p)
			treePaths(n.Content[i+1], p, out, depth+1)
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			p := at.AppendToken(strconv.Itoa(i))
			*out = append(*out, p)
			treePaths(c, p, out, depth+1)
		}
	default:
		// A scalar or an alias holds no path below it.
	}
}

// wholeModelAt is ModelAt as one read of the whole pointer.
func wholeModelAt(doc *soa.OpenAPI, pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	target, err := jsonpointer.GetTarget(doc, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
	if js, ok := target.(*oas3.JSONSchema[oas3.Referenceable]); ok && err == nil && js != nil {
		return js
	}
	return nil
}
