package openapi_test // external test package — exercises only the public API

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// intConst reads the integer constant name from the package at pkgPath, where
// it is declared, rather than from a copy this test would have to keep.
func intConst(t *testing.T, pkgPath, name string) int {
	t.Helper()
	pkg, err := decls.Load(pkgPath)
	require.NoError(t, err)
	v, err := pkg.IntConst(name)
	require.NoError(t, err)
	return int(v)
}

// deepestLowered is a document holding a schema frames levels deep through
// properties, two levels of YAML a frame, under a callback's response header,
// its innermost schema carrying an example nested values levels deep.
func deepestLowered(frames, values int) []byte {
	example := strings.Repeat("{a: ", values) + "1" + strings.Repeat("}", values)
	schema := strings.Repeat("{type: object, properties: {p: ", frames-1) +
		"{type: object, example: " + example + "}" + strings.Repeat("}}", frames-1)
	return fmt.Appendf(nil, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /p:
    post:
      responses:
        "200": {description: ok}
      callbacks:
        cb:
          "{$request.body#/u}":
            post:
              responses:
                "200":
                  description: ok
                  headers:
                    X:
                      content:
                        application/json:
                          schema: %s
`, schema)
}

// codes compiles data and returns the code of every diagnostic it draws.
func codes(t *testing.T, data []byte) []string {
	t.Helper()
	_, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "deep.yaml", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

// TestMaxNesting_AdmitsWhatTheLoweringLowersInFull holds the pre-parse nesting
// bound above every document the lowering lowers without losing anything to
// its own depth bounds: a schema nested to the schema walk's bound, two levels
// a frame, under a callback's response header, its example nested to the
// value bound. Both bounds are read where they are declared, so raising either
// past what the nesting bound admits reddens this, and one level more of
// either degrades, which shows the document sits at both.
func TestMaxNesting_AdmitsWhatTheLoweringLowersInFull(t *testing.T) {
	t.Parallel()
	const tree = "github.com/dexpace/morphic/compilers/openapi/internal/"
	frames := intConst(t, tree+"schema", "maxSchemaDepth")
	values := intConst(t, tree+"value", "maxValueDepth")

	full := codes(t, deepestLowered(frames, values))
	assert.NotContains(t, full, "openapi/source-too-deep", "the deepest document lowered in full is admitted")
	assert.NotContains(t, full, "openapi/degraded-construct", "and nothing in it is lost to a depth bound")

	assert.Contains(t, codes(t, deepestLowered(frames+1, values)), "openapi/degraded-construct",
		"a frame more passes the schema walk's bound")
	assert.Contains(t, codes(t, deepestLowered(frames, values+1)), "openapi/degraded-construct",
		"a level more passes the value bound")
}

// TestCompile_RefusesADocumentNestedPastTheBound is GitHub #905's report at
// the compiler: the schema nested thousands of levels deep lowers nothing and
// draws one source-too-deep error, where it was built, resolved and then
// degraded for its depth.
func TestCompile_RefusesADocumentNestedPastTheBound(t *testing.T) {
	t.Parallel()
	const n = 9000
	body := strings.Repeat(`{"type":"array","items":`, n) + `{"type":"string"}` + strings.Repeat("}", n)
	data := []byte("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    A: " + body + "\n")

	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "deep.yaml", Data: data}}, compilers.Options{})

	require.NoError(t, err)
	assert.Nil(t, doc)
	require.Len(t, diags, 1)
	assert.Equal(t, "openapi/source-too-deep", diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, fmt.Sprintf("document nests %d levels deep, past the %d-level bound", n+4, scan.MaxNesting),
		diags[0].Message, "the root, components, schemas and A, and the levels below")
}
