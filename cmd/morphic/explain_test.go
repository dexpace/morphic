package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const explainSpec = `openapi: 3.1.0
info: {title: t, version: v}
components:
  schemas:
    S:
      type: object
      properties: {a: {type: string}}
      not: {type: string}
`

// TestRun_ExplainReportsTheNodeAtACoordinate is the end-to-end path: --explain
// reports what compiling produced at one source pointer instead of the document.
func TestRun_ExplainReportsTheNodeAtACoordinate(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", explainSpec)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec, "--explain", "/components/schemas/S"}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "coordinate /components/schemas/S")
	assert.Contains(t, out, "node t/openapi/components/schemas/S (model)")
	assert.Contains(t, out, "openapi/validation-only-keyword",
		"a diagnostic stamped at the coordinate is part of what happened there")
	assert.NotContains(t, out, `"irVersion"`, "--explain replaces the document, it does not add to it")
}

// TestRun_ExplainListsALoaderFindingAtItsDeclaration pins that a finding the
// loader made on a raw node is stamped at that node's pointer, so explaining
// the declaration lists it beside what the lowering reports there (GitHub
// #803). Located by line and column alone, it stamped no coordinate at all.
func TestRun_ExplainListsALoaderFindingAtItsDeclaration(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"+
		"components:\n  securitySchemes:\n    s: {type: oauth2}\n")
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec, "--explain", "/components/securitySchemes/s"}, &stdout, &stderr)

	require.Equal(t, 1, code, "an error diagnostic fails the run, explained or not: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "diagnostics (1)")
	assert.Contains(t, out, "error openapi/validation/validation-required-field: "+
		"`securityScheme.flows` is required for type=oauth2")
}

// TestRun_ExplainTakesTheWholeDocumentAsTheEmptyPointer pins that an empty
// --explain is the root coordinate rather than an absent flag: the empty JSON
// Pointer names the whole document, and it is the only query everything
// interned sits beneath.
func TestRun_ExplainTakesTheWholeDocumentAsTheEmptyPointer(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", explainSpec)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec, "--explain", ""}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, `coordinate "" (the whole document)`)
	assert.Contains(t, out, "/components/schemas/S -> t/openapi/components/schemas/S (model)",
		"the whole document holds every coordinate that interned a node")
	assert.NotContains(t, out, `"irVersion"`, "an empty --explain is still --explain, not the document")
}

// TestRun_ExplainNamesNoPrimitiveAtTheWholeDocument pins GitHub #528 end to
// end: a shared primitive's empty Pointer sits on NoSource, so the root query
// does not name it. Both assertions on the explanation hold on a compile that
// interned no primitive, so the document is compiled too, to show this one did.
func TestRun_ExplainNamesNoPrimitiveAtTheWholeDocument(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", explainSpec)
	var document, explained, stderr bytes.Buffer

	require.Equal(t, 0, run([]string{"compile", spec}, &document, &stderr), "stderr: %s", stderr.String())
	require.Equal(t, 0, run([]string{"compile", spec, "--explain", ""}, &explained, &stderr),
		"stderr: %s", stderr.String())

	require.Contains(t, document.String(), `"t/prim/string"`, `property "a" interns the shared string primitive`)
	out := explained.String()
	assert.Contains(t, out, "no type node was interned at this coordinate",
		"nothing is declared at the document root itself")
	assert.NotContains(t, out, "t/prim/", "a shared primitive is at no coordinate")
}

// TestRun_ExplainWriteFailureIsReported pins that a stdout refusing the
// explanation fails the run as a refused document does: exit 2 when the spec
// earned 0, its own code otherwise, and the reason on stderr. Left unchecked,
// the explanation would be lost while the exit code said it was written.
func TestRun_ExplainWriteFailureIsReported(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec string
		want int
	}{
		{"clean spec", explainSpec, exitFailure},
		{"spec with findings", unresolvedRefSpec, exitFindings},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := writeFile(t, "spec.yaml", tt.spec)
			var stderr bytes.Buffer

			code := run([]string{"compile", spec, "--explain", ""}, failWriter{err: errors.New("pipe closed")}, &stderr)

			assert.Equal(t, tt.want, code, "stderr: %s", stderr.String())
			assert.Contains(t, stderr.String(), "morphic: write explanation: pipe closed")
		})
	}
}

// splitCommon is a document a split spec's reference reads, declaring a Pet
// with a child object and a finding of its own.
const splitCommon = `openapi: 3.1.0
info: {title: Common, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      type: object
      properties:
        owner: {type: object, properties: {id: {type: integer}}}
      not: {required: [x]}
`

// TestRun_ExplainReadsTheSpecsOwnCoordinates pins that --explain addresses the
// spec it compiles, whatever another document a reference reads declares at
// the same pointer: neither that document's nodes nor its findings are taken
// for the spec's, whether the spec declares something there or not.
func TestRun_ExplainReadsTheSpecsOwnCoordinates(t *testing.T) {
	t.Parallel()
	const use = "      responses:\n        \"200\":\n          description: ok\n          content:\n" +
		"            application/json: {schema: {$ref: './common.yaml#/components/schemas/Pet'}}\n"
	root := "openapi: 3.1.0\ninfo: {title: Root, version: \"1\"}\npaths:\n  /pets:\n    get:\n" + use
	for name, tc := range map[string]struct {
		spec, want string
	}{
		"the spec declares one":  {root + "components:\n  schemas:\n    Pet: {type: object, properties: {name: {type: string}}}\n", "node t/openapi/components/schemas/Pet (model)"},
		"the spec declares none": {root, "no type node was interned at this coordinate"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := writeFile(t, "root.yaml", tc.spec)
			require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(spec), "common.yaml"), []byte(splitCommon), 0o644))
			var stdout, stderr bytes.Buffer

			code := run([]string{"compile", spec, "--explain", "/components/schemas/Pet"}, &stdout, &stderr)

			require.Equal(t, 0, code, "stderr: %s", stderr.String())
			require.Contains(t, stderr.String(), "common.yaml#/components/schemas/Pet",
				"sanity: common.yaml's Pet is lowered and draws its finding there")
			out := stdout.String()
			assert.Contains(t, out, tc.want)
			assert.NotContains(t, out, "common.yaml", "nothing of common.yaml's is the spec's")
			assert.NotContains(t, out, "validation-only-keyword")
		})
	}
}
