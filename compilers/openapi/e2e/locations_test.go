// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/ir"
)

// mapped is the prefix the location tests map.
const mapped = "https://example.com/x/"

// locationSpec writes a source whose schema p names ref, and q the document
// beside it by a relative path.
func locationSpec(ref string) string {
	return head31 + "paths: {}\ncomponents:\n  schemas:\n    p: {$ref: '" + ref + "'}\n    q: {$ref: 'b.yaml#/B'}\n"
}

// TestCompile_AMappedURLIsTheDocumentItsLocationHolds holds a URL under a mapped
// prefix to the file the mapping puts it at, named, labelled, listed and
// confined as that file: one type for the mapped URL and the relative path, no
// URL among the sources, with external references refused.
func TestCompile_AMappedURLIsTheDocumentItsLocationHolds(t *testing.T) {
	t.Parallel()
	for name, prefix := range map[string]string{"as written": mapped, "spelled another way": "HTTPS://Example.COM:443/x/"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := realDir(t)
			layout(t, dir, map[string]string{"b.yaml": "B: {type: string}\n"})
			rec := &recordingReader{inner: loopbackDisk{}}

			doc, diags := compileWithReader(t, filepath.Join(dir, "spec.yaml"), locationSpec(mapped+"b.yaml#/B"),
				openapi.Options{Locations: []openapi.Location{{Prefix: prefix, Location: "."}}}, rec)

			assertNoErrorDiags(t, diags)
			want := ids.In("b.yaml").ForPointer("/B")
			assert.Equal(t, want, useTarget(t, doc, "p"), "the type b.yaml declares at /B, labelled as the file is")
			assert.Equal(t, want, useTarget(t, doc, "q"))
			require.Len(t, doc.Sources, 2, "%v", sourcePaths(doc))
			assert.Equal(t, "b.yaml", filepath.Base(doc.Sources[1].Path))
			assert.NotContains(t, strings.Join(sourcePaths(doc), " "), "example.com")
			for _, loc := range rec.locations() {
				assert.True(t, strings.HasPrefix(loc, dir), "%s lies in the source's directory", loc)
			}
		})
	}
}

// TestCompile_AMappedURLThatNormalizesOutOfItsPrefixIsRefused holds a URL whose
// dot segments leave the prefix, once spelled, to the network's refusal, and
// to no read of a path above the directory; the URL itself is asked of the
// reader, which refuses it as on the network.
func TestCompile_AMappedURLThatNormalizesOutOfItsPrefixIsRefused(t *testing.T) {
	t.Parallel()
	root := realDir(t)
	dir := filepath.Join(root, "d")
	layout(t, root, map[string]string{"secret.yaml": "B: {type: string}\n", "d/b.yaml": "B: {type: string}\n"})
	rec := &recordingReader{inner: loopbackDisk{}}

	_, diags := compileWithReader(t, filepath.Join(dir, "spec.yaml"), locationSpec(mapped+"%2e%2e/secret.yaml#/B"),
		openapi.Options{Locations: []openapi.Location{{Prefix: mapped, Location: "."}}}, rec)

	bad := unresolvedRefs(diags)
	require.Len(t, bad, 1, "%+v", diags)
	assert.Equal(t, "/components/schemas/p", string(bad[0].Provenance.Pointer))
	assert.Contains(t, bad[0].Message, onNetwork)
	for _, loc := range rec.locations() {
		if strings.HasPrefix(loc, "https://") {
			continue
		}
		assert.True(t, strings.HasPrefix(loc, dir+string(filepath.Separator)), "%s is not above the directory", loc)
	}
}

// TestCompile_AMappedSegmentThatDecodesToASeparatorNamesNothing holds a
// remainder joined segment by segment: a segment whose decoding holds a
// separator, or climbs, refuses the reference unread in either mode, and the
// location reads the same with or without a final separator.
func TestCompile_AMappedSegmentThatDecodesToASeparatorNamesNothing(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, ref, segment string }{
		{"an escaped slash", mapped + "a%2F..%2F..%2Fsecret.yaml#/B", "a/../../secret.yaml"},
		{"an escaped backslash", mapped + "a%5C..%5Csecret.yaml#/B", `a\..\secret.yaml`},
	}
	for _, tc := range tests {
		for _, location := range []string{".", "../x"} {
			for _, allow := range []bool{true, false} {
				t.Run(tc.name+" under "+location, func(t *testing.T) {
					t.Parallel()
					root := realDir(t)
					layout(t, root, map[string]string{"secret.yaml": "B: {type: string}\n", "x/b.yaml": "B: {type: string}\n"})
					rec := &recordingReader{inner: loopbackDisk{}}

					_, diags := compileWithReader(t, filepath.Join(root, "x", "spec.yaml"), locationSpec(tc.ref),
						openapi.Options{AllowExternalRefs: allow, Locations: []openapi.Location{{Prefix: mapped, Location: location}}}, rec)

					var mine []ir.Diagnostic
					for _, diagnostic := range unresolvedRefs(diags) {
						if diagnostic.Provenance.Pointer == "/components/schemas/p" {
							mine = append(mine, diagnostic)
						}
					}
					require.Len(t, mine, 1, "%+v", diags)
					assert.Contains(t, mine[0].Message, "its URL maps to no location: the segment \""+tc.segment+
						"\" holds an escaped separator")
					for _, loc := range rec.locations() {
						assert.NotContains(t, loc, "secret.yaml", "nothing above the location is read")
					}
				})
			}
		}
	}
}

// TestCompile_ALocationWithNoFinalSeparatorIsTheDirectoryItNames holds a
// directory location written without its final separator to the directory,
// not to its parent: b.yaml under "../x" is x/b.yaml, never the sibling of x.
func TestCompile_ALocationWithNoFinalSeparatorIsTheDirectoryItNames(t *testing.T) {
	t.Parallel()
	for _, location := range []string{".", "../x", "../x/"} {
		t.Run(location, func(t *testing.T) {
			t.Parallel()
			root := realDir(t)
			layout(t, root, map[string]string{"b.yaml": "B: {type: integer}\n", "x/b.yaml": "B: {type: string}\n"})
			rec := &recordingReader{inner: loopbackDisk{}}

			doc, diags := compileWithReader(t, filepath.Join(root, "x", "spec.yaml"), locationSpec(mapped+"b.yaml#/B"),
				openapi.Options{AllowExternalRefs: true, Locations: []openapi.Location{{Prefix: mapped, Location: location}}}, rec)

			assertNoErrorDiags(t, diags)
			for _, loc := range rec.locations() {
				assert.Equal(t, filepath.Join(root, "x", "b.yaml"), filepath.Clean(loc), "never the parent's b.yaml")
			}
			assert.NotEmpty(t, rec.locations())
			assert.Equal(t, ir.TypeID("type/primitive/string"), typeTarget(t, doc, useTarget(t, doc, "p")))
		})
	}
}

// typeTarget is the type the alias type id names.
func typeTarget(t *testing.T, doc *ir.Document, id ir.TypeID) ir.TypeID {
	t.Helper()
	alias, ok := doc.Types[id].(*ir.Scalar)
	require.True(t, ok, "%s is an alias", id)
	return alias.Base.Target
}

// TestCompile_AFetchedDocumentsReferenceToAMappedURLIsNotAFile holds the
// mapping to what the caller's source may use: a document read over the
// network that names a mapped URL is refused as a network document naming a
// file, and the mapped file is never read.
func TestCompile_AFetchedDocumentsReferenceToAMappedURLIsNotAFile(t *testing.T) {
	t.Parallel()
	dir := realDir(t)
	layout(t, dir, map[string]string{"b.yaml": "B: {type: string}\n"})
	srv := serveDocs(t, map[string]string{"/net.yaml": "X: {$ref: '" + mapped + "b.yaml#/B'}\n"})
	rec := &recordingReader{inner: loopbackDisk{}}
	src := head31 + "paths: {}\ncomponents:\n  schemas:\n    p: {$ref: '" + srv.URL + "/net.yaml#/X'}\n"

	_, diags := compileWithReader(t, filepath.Join(dir, "spec.yaml"), src,
		openapi.Options{AllowExternalRefs: true, Locations: []openapi.Location{{Prefix: mapped, Location: "."}}}, rec)

	bad := unresolvedRefs(diags)
	require.Len(t, bad, 1, "%+v", diags)
	assert.Equal(t, 1, bad[0].Provenance.Source, "in net.yaml's entry")
	assert.Contains(t, bad[0].Message, reftable.OffNetwork)
	assert.Equal(t, []string{srv.URL + "/net.yaml"}, rec.locations(), "b.yaml is not read")
}

// TestCompile_ALocateOptionMapsTheURLsASelfPutsReadsAt holds the remedy for a
// 3.2 document whose $self is an http(s) URL: its relative references name
// URLs beside the $self, and a mapping of that URL's prefix to the source's
// directory reads them from the files there, labelled as the files are,
// external references refused; without it they are refused as on the network.
func TestCompile_ALocateOptionMapsTheURLsASelfPutsReadsAt(t *testing.T) {
	t.Parallel()
	dir := realDir(t)
	layout(t, dir, map[string]string{"b.yaml": "B: {type: string}\n"})
	src := "openapi: 3.2.0\n$self: " + mapped + "api.yaml\ninfo: {title: t, version: \"1\"}\npaths: {}\n" +
		"components:\n  schemas:\n    p: {$ref: 'b.yaml#/B'}\n"
	rec := &recordingReader{inner: loopbackDisk{}}

	doc, diags := compileWithReader(t, filepath.Join(dir, "spec.yaml"), src,
		openapi.Options{Locations: []openapi.Location{{Prefix: mapped, Location: "."}}}, rec)

	assertNoErrorDiags(t, diags)
	assert.Equal(t, ids.In("b.yaml").ForPointer("/B"), useTarget(t, doc, "p"))
	assert.Equal(t, []string{filepath.Join(dir, "b.yaml")}, rec.locations())

	_, diags = compileWithReader(t, filepath.Join(dir, "spec.yaml"), src, openapi.Options{}, &recordingReader{inner: loopbackDisk{}})
	bad := unresolvedRefs(diags)
	require.Len(t, bad, 1, "%+v", diags)
	assert.Contains(t, bad[0].Message, onNetwork)
}

// TestCompile_AMappedIdentifiersRelativeReferenceReadsTheFileBesideTheSpec
// holds GitHub #1051's spec: a schema whose $id names the URL it is published
// under, a relative $ref under it, and that URL's directory mapped to the
// spec's. The $ref reads the b.yaml beside the spec in the confined mode, and
// one climbing out of the prefix names the URL above it, never the file above
// the mapped directory: a mapping places a document, never an identifier.
func TestCompile_AMappedIdentifiersRelativeReferenceReadsTheFileBesideTheSpec(t *testing.T) {
	t.Parallel()
	const spec = head31 + "paths: {}\ncomponents:\n  schemas:\n    S:\n      $id: 'https://example.com/x/s.json'\n" +
		"      type: object\n      properties:\n        p: {$ref: 'b.yaml#/B'}\n        u: {$ref: '../up.yaml#/U'}\n"
	for name, allow := range map[string]bool{"confined": false, "external references allowed": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := realDir(t)
			dir := filepath.Join(root, "d")
			layout(t, root, map[string]string{"up.yaml": "U: {type: boolean}\n", "d/b.yaml": "B: {type: string}\n"})
			rec := &recordingReader{inner: loopbackDisk{}}

			doc, diags := compileWithReader(t, filepath.Join(dir, "spec.yaml"), spec,
				openapi.Options{AllowExternalRefs: allow, Locations: []openapi.Location{{Prefix: mapped, Location: "."}}}, rec)

			require.NotNil(t, doc, "%+v", diags)
			bad := unresolvedRefs(diags)
			require.Len(t, bad, 1, "%+v", diags)
			assert.Equal(t, "/components/schemas/S/properties/u", string(bad[0].Provenance.Pointer))
			assert.Contains(t, bad[0].Message, "https://example.com/up.yaml")
			assert.Contains(t, sourcePaths(doc), filepath.Join(dir, "b.yaml"), "b.yaml beside the spec is read")
			for _, loc := range rec.locations() {
				assert.NotEqual(t, filepath.Join(root, "up.yaml"), loc, "no file above the mapped directory is read")
			}
		})
	}
}
