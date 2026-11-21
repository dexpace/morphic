// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// overlaySuite is the Overlay Specification's test sets as vendored, and the
// examples of Overlay 1.2 beside them.
const overlaySuite = "testdata/overlay-specification"

// versionCounts is how many documents each version's directories hold at the
// revision the vendored README names: pass, then fail. A refresh moves them.
var versionCounts = map[string][2]int{"v1.0": {12, 20}, "v1.1": {13, 22}, "v1.2-dev": {22, 43}}

// suiteSkip is a document of the schema tests the compiler reads otherwise
// than the suite's JSON Schema does, with how it reads it instead. The suite
// says refused is whether the document is expected to draw an invalid-overlay
// finding; wantRefused is what the compiler does with it.
type suiteSkip struct {
	reason      string
	wantRefused bool
	says        string
}

// suiteSkips holds each skipped document by its path under tests/, and one
// that no longer differs from the suite fails, so a skip cannot outlive its
// reason.
var suiteSkips = map[string]suiteSkip{
	"v1.0/pass/actions-traits-example.yaml": {
		reason: "its target is not RFC 9535, whose member-name shorthand admits no `-` (section 2.5.1.1); " +
			"the suite's schema takes any string starting with $", wantRefused: true, says: "['x-oai-traits']",
	},
	"v1.1/pass/actions-traits-example.yaml": {
		reason: "its target is not RFC 9535, whose member-name shorthand admits no `-` (section 2.5.1.1); " +
			"the suite's schema takes any string starting with $", wantRefused: true, says: "['x-oai-traits']",
	},
	"v1.2-dev/pass/actions-traits-example.yaml": {
		reason: "its target is not RFC 9535, whose member-name shorthand admits no `-` (section 2.5.1.1); " +
			"the suite's schema takes any string starting with $", wantRefused: true, says: "['x-oai-traits']",
	},
	"v1.0/fail/actions-not-unique.yaml": {
		reason: "the suite's schema states uniqueItems; the specification's text states no uniqueness of actions, " +
			"and two equal actions apply in order", wantRefused: false,
	},
	"v1.1/fail/actions-not-unique.yaml": {
		reason: "the suite's schema states uniqueItems; the specification's text states no uniqueness of actions, " +
			"and two equal actions apply in order", wantRefused: false,
	},
	"v1.2-dev/fail/actions-not-unique.yaml": {
		reason: "the suite's schema states uniqueItems; the specification's text states no uniqueness of actions, " +
			"and two equal actions apply in order", wantRefused: false,
	},
}

// diffIgnoringOrigin returns how two documents differ but for where each says
// it came from: its sources, its diagnostics and every Provenance, which a
// comparison skips by type and so reaches wherever the document holds one,
// map values included.
func diffIgnoringOrigin(want, got *ir.Document) string {
	document, irDocument := *want, *got
	document.Sources, document.Diagnostics, irDocument.Sources, irDocument.Diagnostics = nil, nil, nil, nil
	return cmp.Diff(&document, &irDocument, cmpopts.IgnoreTypes(ir.Provenance{}))
}

// readFile returns the text of the file at path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// compileSet reads the three documents of the set in dir: the source with the
// overlay applied, strictly, and the output alone. A set is read when its
// documents compile with no error finding.
func compileSet(t *testing.T, dir string) (patched, output *ir.Document) {
	t.Helper()
	source, overlay, result := readFile(t, filepath.Join(dir, "openapi.yaml")), readFile(t, filepath.Join(dir, "overlay.yaml")),
		readFile(t, filepath.Join(dir, "output.yaml"))

	patched, diags := compileText(t, "openapi.yaml", source,
		openapi.Options{Overlay: &openapi.Overlay{Path: "overlay.yaml", Data: []byte(overlay)}})
	require.NotNil(t, patched, "the overlay applies strictly: %+v", diags)
	require.False(t, ir.HasError(diags), "the overlay applies strictly: %+v", diags)

	output, diags = compileText(t, "openapi.yaml", result, openapi.Options{})
	require.NotNil(t, output, "the output compiles: %+v", diags)
	require.False(t, ir.HasError(diags), "the output compiles: %+v", diags)
	return patched, output
}

// TestOverlaySuite_CompliantSets holds the eight compliant sets of the
// Overlay Specification to their output: the source patched by the overlay,
// compiled, is the IR of the output document alone, but for where each input
// says it came from.
func TestOverlaySuite_CompliantSets(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(filepath.Join(overlaySuite, "compliant-sets"))
	require.NoError(t, err)

	sets := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sets++
		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			patched, output := compileSet(t, filepath.Join(overlaySuite, "compliant-sets", e.Name()))

			assert.Empty(t, diffIgnoringOrigin(output, patched), "the patched source differs from the output (-output +patched)")

			base, diags := compileText(t, "openapi.yaml", readFile(t, filepath.Join(overlaySuite, "compliant-sets", e.Name(), "openapi.yaml")), openapi.Options{})
			require.NotNil(t, base, "%+v", diags)
			assert.NotEmpty(t, diffIgnoringOrigin(base, patched), "the overlay changed the IR, so the set tests something")
		})
	}
	assert.Equal(t, 8, sets, "the compliant sets the vendored README names")
}

// TestOverlaySuite_TheReusableActionExampleOfOverlay12 holds the specification's
// reusable action reference example, transcribed as the 1.2 text prints it, to
// its result, as the compliant sets are held.
func TestOverlaySuite_TheReusableActionExampleOfOverlay12(t *testing.T) {
	t.Parallel()
	patched, output := compileSet(t, filepath.Join(overlaySuite, "examples-1.2", "reusable"))

	assert.Empty(t, diffIgnoringOrigin(output, patched), "the patched source differs from the result description (-result +patched)")
}

// TestOverlaySuite_ReadsEachDocumentAsItsVersionDoes compiles a minimal source
// with each document of the schema tests as its overlay, leniently, so that
// only the document's own reading can refuse it. A pass document draws no
// invalid-overlay error and a fail document draws one, but for the documents
// suiteSkips excuses, each held to what is asserted of it instead.
func TestOverlaySuite_ReadsEachDocumentAsItsVersionDoes(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for version, counts := range versionCounts {
		for i, kind := range []string{"pass", "fail"} {
			dir := filepath.Join(overlaySuite, "tests", version, kind)
			files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
			require.NoError(t, err)
			require.Len(t, files, counts[i], "the documents of %s/%s", version, kind)

			for _, file := range files {
				key := version + "/" + kind + "/" + filepath.Base(file)
				seen[key] = true
				t.Run(key, func(t *testing.T) {
					t.Parallel()
					data, err := os.ReadFile(file)
					require.NoError(t, err)

					doc, diags := compileText(t, "spec.yaml", srMin,
						openapi.Options{Overlay: &openapi.Overlay{Path: filepath.Base(file), Data: data, Lax: true}})
					invalid := byCode(diags, diag.OverlayInvalid)

					if skip, skipped := suiteSkips[key]; skipped {
						assertSkippedDocument(t, key, skip, kind == "fail", invalid)
						return
					}
					if kind == "pass" {
						assert.Empty(t, invalid, "a document the suite passes is read: %+v", diags)
						assert.NotNil(t, doc, "and applies: %+v", diags)
						return
					}
					assert.NotEmpty(t, invalid, "a document the suite fails is refused as an invalid overlay: %+v", diags)
					assert.Nil(t, doc, "and nothing of it applies")
				})
			}
		}
	}
	for key := range suiteSkips {
		assert.True(t, seen[key], "the skip table names %q, which is no document", key)
	}
}

// assertSkippedDocument holds a skipped document to what the skip table says
// of it, and fails when it matches the suite after all.
func assertSkippedDocument(t *testing.T, key string, skip suiteSkip, suiteRefuses bool, invalid []ir.Diagnostic) {
	t.Helper()
	refused := len(invalid) > 0
	assert.NotEqual(t, suiteRefuses, refused, "%s is skipped (%s) and agrees with the suite: remove the skip", key, skip.reason)
	assert.Equal(t, skip.wantRefused, refused, "%s: %s", key, skip.reason)
	if skip.says != "" && assert.NotEmpty(t, invalid) {
		assert.Contains(t, invalid[0].Message, skip.says, key)
	}
}
