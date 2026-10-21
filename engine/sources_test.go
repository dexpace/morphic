package engine_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

// splitRoot is a spec whose operation takes a parameter and a response from
// common.yaml beside it, the shape a spec split across files takes.
const splitRoot = `openapi: 3.1.0
info: {title: Split, version: "1"}
paths:
  /pets:
    get:
      parameters:
        - $ref: './common.yaml#/components/parameters/Limit'
      responses:
        "404": {$ref: './common.yaml#/components/responses/NotFound'}
`

// splitCommon is the document splitRoot's references name.
const splitCommon = `openapi: 3.1.0
info: {title: Common, version: "1"}
paths: {}
components:
  parameters:
    Limit: {name: limit, in: query, schema: {type: integer}}
  responses:
    NotFound: {description: not found}
`

// unresolved returns the openapi/unresolved-ref diagnostics among diags.
func unresolved(diags []ir.Diagnostic) []ir.Diagnostic {
	return slices.DeleteFunc(slices.Clone(diags), func(d ir.Diagnostic) bool {
		return d.Code != "openapi/unresolved-ref"
	})
}

// TestEngine_RunSourcesCompilesASpecSplitAcrossSources pins that a spec split
// across files compiles from memory alone. The paths name a directory that is
// not there, so a reference that resolves can only have been served from the
// sources, and one that reached the disk would fail.
func TestEngine_RunSourcesCompilesASpecSplitAcrossSources(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	dir := filepath.Join(t.TempDir(), "absent")

	res, err := eng.RunSources(t.Context(), []compilers.Source{
		{Path: filepath.Join(dir, "root.yaml"), Data: []byte(splitRoot)},
		{Path: filepath.Join(dir, "common.yaml"), Data: []byte(splitCommon)},
	}, engine.RunOptions{})

	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Empty(t, unresolved(res.Diagnostics), "both references are served from the sources")
	assert.Equal(t, filepath.Join(dir, "root.yaml"), res.Sources[0].Path, "the first source is the spec")
}

// TestEngine_RunSourcesReadsNothingElse pins that RunSources reads only the
// sources it was handed: the document a reference names sits on disk beside
// the spec's path, and is still not read when no source holds it.
func TestEngine_RunSourcesReadsNothingElse(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	root := writeNamed(t, "root.yaml", splitRoot)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(root), "common.yaml"), []byte(splitCommon), 0o644))

	res, err := eng.RunSources(t.Context(), []compilers.Source{{Path: root, Data: []byte(splitRoot)}},
		engine.RunOptions{})

	require.NoError(t, err)
	assert.Len(t, unresolved(res.Diagnostics), 2, "neither reference reaches the file on disk: %+v", res.Diagnostics)
}

// TestEngine_RunSourcesAgreesWithRun pins that a spec compiled from memory is
// the spec compiled from disk: the same files, handed once as paths and once
// as sources, give one Result.
func TestEngine_RunSourcesAgreesWithRun(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	root := writeNamed(t, "root.yaml", splitRoot)
	common := filepath.Join(filepath.Dir(root), "common.yaml")
	require.NoError(t, os.WriteFile(common, []byte(splitCommon), 0o644))

	fromDisk, err := eng.Run(t.Context(), root, engine.RunOptions{})
	require.NoError(t, err)
	fromMemory, err := eng.RunSources(t.Context(), []compilers.Source{
		{Path: root, Data: []byte(splitRoot)}, {Path: common, Data: []byte(splitCommon)},
	}, engine.RunOptions{})
	require.NoError(t, err)

	require.Empty(t, unresolved(fromDisk.Diagnostics), "the disk run must resolve both, or this compares nothing")
	assert.Empty(t, cmp.Diff(fromDisk, fromMemory))
}

// TestEngine_RunSourcesServesAFileASettingNames pins that a file a setting
// names is served from the sources too: an overlay held in memory applies, and
// no file is read for it.
func TestEngine_RunSourcesServesAFileASettingNames(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	const overlay = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\n" +
		"actions:\n  - target: $.info\n    update: {title: Patched}\n"
	dir := filepath.Join(t.TempDir(), "absent")

	res, err := eng.RunSources(t.Context(), []compilers.Source{
		{Path: filepath.Join(dir, "spec.yaml"), Data: []byte(testspec.Tiny)},
		{Path: filepath.Join(dir, "patch.yaml"), Data: []byte(overlay)},
	}, engine.RunOptions{CompilerOptions: map[string]string{"overlay": filepath.Join(dir, "patch.yaml")}})

	require.NoError(t, err)
	require.NotNil(t, res.Document, "diagnostics: %+v", res.Diagnostics)
	assert.Equal(t, "Patched", res.Document.Name)
}

// TestEngine_RunSourcesIgnoresParsed pins that a source's Parsed is not read:
// it is one compile's live state, so a caller's value, here one no compiler
// knows, is dropped rather than handed to the compile.
func TestEngine_RunSourcesIgnoresParsed(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)

	res, err := eng.RunSources(t.Context(), []compilers.Source{
		{Path: "spec.yaml", Data: []byte(testspec.Tiny), Parsed: struct{}{}},
	}, engine.RunOptions{})

	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Equal(t, "Tiny", res.Document.Name)
}

// TestEngine_RunSourcesRefusals pins that a misconfigured run is a Go error
// naming its fault, before anything is compiled: no sources, two naming one
// document, a threshold that names none, and an engine no constructor built.
func TestEngine_RunSourcesRefusals(t *testing.T) {
	t.Parallel()
	built, err := engine.New()
	require.NoError(t, err)
	tiny := []compilers.Source{{Path: "s.yaml", Data: []byte(testspec.Tiny)}}
	tests := []struct {
		name    string
		eng     *engine.Engine
		sources []compilers.Source
		opts    engine.RunOptions
		want    string
	}{
		{"no sources", built, nil, engine.RunOptions{}, "no sources"},
		{"two sources naming one document", built,
			[]compilers.Source{{Path: "a/b.yaml", Data: tiny[0].Data}, {Path: "a/./b.yaml", Data: tiny[0].Data}},
			engine.RunOptions{}, "names a document"},
		{"a threshold naming none", built, tiny, engine.RunOptions{FailOn: "fatal"}, "unknown fail-on threshold"},
		{"an unbuilt engine", &engine.Engine{}, tiny, engine.RunOptions{}, "uninitialized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := tt.eng.RunSources(t.Context(), tt.sources, tt.opts)

			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestEngine_RunConfinesReferencesToTheSpecsDirectory pins the default every
// run reads under: a document beside the spec is read, one outside its
// directory is refused with the reason, and allow-external-refs lifts the
// confinement.
func TestEngine_RunConfinesReferencesToTheSpecsDirectory(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	top := t.TempDir()
	specs := filepath.Join(top, "specs")
	require.NoError(t, os.Mkdir(specs, 0o755))
	root := filepath.Join(specs, "root.yaml")
	src := strings.Replace(splitRoot, "./common.yaml#/components/parameters/Limit",
		"../common.yaml#/components/parameters/Limit", 1)
	require.NoError(t, os.WriteFile(root, []byte(src), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(specs, "common.yaml"), []byte(splitCommon), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(top, "common.yaml"), []byte(splitCommon), 0o644))

	confined, err := eng.Run(t.Context(), root, engine.RunOptions{})
	require.NoError(t, err)
	allowed, err := eng.Run(t.Context(), root,
		engine.RunOptions{CompilerOptions: map[string]string{"allow-external-refs": "true"}})
	require.NoError(t, err)

	refused := unresolved(confined.Diagnostics)
	require.Len(t, refused, 1, "only the reference leaving the directory is refused: %+v", confined.Diagnostics)
	assert.Equal(t, jsontext.Pointer("/paths/~1pets/get/parameters/0"), refused[0].Provenance.Pointer)
	assert.Contains(t, refused[0].Message, "lies outside")
	assert.Empty(t, unresolved(allowed.Diagnostics), "allow-external-refs reads it")
}
