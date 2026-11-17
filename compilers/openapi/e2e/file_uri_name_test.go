// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
)

// sibDoc is a document of one schema that declares an identifier, which is
// what turns one file read under two names into a collision.
const sibDoc = "X: {$id: 'urn:x:sib', type: object, properties: {n: {type: string}}}\n"

// sibSchemas writes the components section naming sibDoc's X and the source's
// own A by the spellings given, in the order given.
func sibSchemas(entries ...[2]string) string {
	out := head31 + "paths: {}\ncomponents:\n  schemas:\n"
	for _, e := range entries {
		out += "    " + e[0] + ": {$ref: '" + e[1] + "'}\n"
	}
	return out
}

// realDir is a temporary directory with its symbolic links resolved, so a path
// built from it is the one the file system reports.
func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

// TestCompile_AFileIsOneDocumentWhateverItsReferenceSpellsIt holds a file named
// by a relative path, a dotted one and a file: URI to one document: the three
// schemas target one type, the file is one entry of Document.Sources, and no
// identifier is declared twice, in either order.
func TestCompile_AFileIsOneDocumentWhateverItsReferenceSpellsIt(t *testing.T) {
	t.Parallel()
	for name, order := range map[string][]string{"A first": {"A", "B", "C"}, "B first": {"B", "A", "C"}} {
		dir := realDir(t)
		layout(t, dir, map[string]string{"sib.yaml": sibDoc})
		spelling := map[string]string{"A": "sib.yaml#/X", "B": "file://" + dir + "/sib.yaml#/X", "C": "urn:x:sib"}
		var entries [][2]string
		for _, k := range order {
			entries = append(entries, [2]string{k, spelling[k]})
		}

		doc, diags := compileOnDisk(t, filepath.Join(dir, "root.yaml"), sibSchemas(entries...), true)

		assertNoErrorDiags(t, diags)
		want := ids.In("sib.yaml").ForPointer("/X")
		for _, k := range order {
			assert.Equal(t, want, useTarget(t, doc, k), "%s: %s", name, k)
		}
		paths := sourcePaths(doc)
		assert.Len(t, paths, 2, "%s: the source and sib.yaml once: %v", name, paths)
		assert.Equal(t, 1, countSuffix(paths, "sib.yaml"), name)
	}
}

// countSuffix counts the entries of paths ending in suffix.
func countSuffix(paths []string, suffix string) int {
	return len(slices.DeleteFunc(slices.Clone(paths), func(p string) bool { return filepath.Base(p) != suffix }))
}

// TestCompile_AFileURIIsRefusedByItsSpellingWhereReadsAreConfined holds the
// confined mode to the reference's spelling, not to the order the documents
// are read in: a file: URI naming another document is refused unread, a
// relative path to it reads, and either spelling of the source itself
// resolves; an absolute path is refused as it was.
func TestCompile_AFileURIIsRefusedByItsSpellingWhereReadsAreConfined(t *testing.T) {
	t.Parallel()
	const uriMessage = "it is named by a file URI, which a read follows only when external references are allowed"
	const pathMessage = "the $ref names it by an absolute path, which a read follows only when external references are allowed"
	tests := []struct {
		name    string
		b       string // B's reference, with DIR for the directory
		refused string // the message B draws, or "" where B resolves
	}{
		{"a file URI to another document", "file://DIR/sib.yaml#/X", uriMessage},
		{"a file URI to the source", "file://DIR/root.yaml#/components/schemas/A", ""},
		{"an absolute path to another document", "DIR/sib.yaml#/X", pathMessage},
		{"an absolute path to the source", "DIR/root.yaml#/components/schemas/A", ""},
	}
	for _, tc := range tests {
		for order, first := range map[string]string{"B after": "A", "B before": "B"} {
			t.Run(tc.name+", "+order, func(t *testing.T) {
				t.Parallel()
				dir := realDir(t)
				layout(t, dir, map[string]string{"sib.yaml": sibDoc})
				b := [2]string{"B", strings.ReplaceAll(tc.b, "DIR", dir)}
				a := [2]string{"A", "sib.yaml#/X"}
				c := [2]string{"C", "urn:x:sib"}
				entries := [][2]string{a, b, c}
				if first == "B" {
					entries = [][2]string{b, a, c}
				}

				doc, diags := compileOnDisk(t, filepath.Join(dir, "root.yaml"), sibSchemas(entries...), false)

				want := ids.In("sib.yaml").ForPointer("/X")
				assert.Equal(t, want, useTarget(t, doc, "A"), "A resolves")
				assert.Equal(t, want, useTarget(t, doc, "C"), "C resolves")
				bad := unresolvedRefs(diags)
				if tc.refused == "" {
					assert.Empty(t, bad, "%+v", diags)
					assert.Equal(t, componentID("A"), useTarget(t, doc, "B"), "B names the source's own A")
					return
				}
				require.Len(t, bad, 1, "%+v", diags)
				assert.Equal(t, "/components/schemas/B", string(bad[0].Provenance.Pointer))
				assert.Contains(t, bad[0].Message, tc.refused)
			})
		}
	}
}
