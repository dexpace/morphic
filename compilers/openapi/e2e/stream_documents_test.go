// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

const (
	srOverlayFirst = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $.info\n" +
		"    update: {description: first}\n"
	srOverlaySecond = "overlay: 1.0.0\ninfo: {title: o2, version: \"1\"}\nactions:\n  - target: $.info\n" +
		"    update: {x-second: dropped}\n"
)

// compileOverlaid compiles srMin with overlay as its overlay document.
func compileOverlaid(t *testing.T, overlay string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	return compileText(t, "spec.yaml", srMin, openapi.Options{Overlay: &openapi.Overlay{Path: "ov.yaml", Data: []byte(overlay)}})
}

// TestCompile_AnOverlayStreamWithASecondDocumentIsRefused holds an overlay to
// the rule a source is held to: a second document that holds content is an
// error in the overlay's own entry, and the overlay is not applied.
func TestCompile_AnOverlayStreamWithASecondDocumentIsRefused(t *testing.T) {
	t.Parallel()

	doc, diags := compileOverlaid(t, srOverlayFirst+"---\n"+srOverlaySecond)

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.StreamDocumentsDropped, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, 1, diags[0].Provenance.Source, "the overlay's entry of Document.Sources")
	assert.Equal(t, ir.Position{Line: 7, Column: 1}, diags[0].Provenance.Position)
	assert.Equal(t, "only one document of the overlay's YAML stream was applied; "+
		"the one after it holds content that was not", diags[0].Message)
	assert.Nil(t, doc, "an overlay that is refused applies nothing and lowers nothing")
}

// TestCompile_AnOverlayAfterEmptyDocumentsIsRead holds the other half: leading
// documents that hold nothing are skipped, as they are for a source.
func TestCompile_AnOverlayAfterEmptyDocumentsIsRead(t *testing.T) {
	t.Parallel()

	doc, diags := compileOverlaid(t, "---\n---\n"+srOverlayFirst)

	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.Equal(t, "first", doc.Docs.Description)
}

// TestCompile_AnExternalDocumentStreamIsReportedInItsOwnEntry holds a document a
// reference reads to the same rule: a second document is an error at the
// external document's index, and what the reference names in the first is read.
func TestCompile_AnExternalDocumentStreamIsReportedInItsOwnEntry(t *testing.T) {
	t.Parallel()
	const stream = "A: {type: string}\n---\nB: {type: integer}\n"
	root := func(target string) string {
		return srMin + "components: {schemas: {S: {$ref: \"other.yaml#/" + target + "\"}}}\n"
	}
	const dropped = "only one document of the YAML stream was read; the one after it holds content that was not"

	t.Run("a reference into the second document", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		layout(t, dir, map[string]string{"main.yaml": root("B"), "other.yaml": stream})

		_, diags := compileDir(t, dir, openapi.Options{})

		require.Len(t, diags, 2, "%+v", diags)
		refs := byCode(diags, diag.UnresolvedRef)
		require.Len(t, refs, 1)
		assert.Equal(t, "/components/schemas/S", string(refs[0].Provenance.Pointer))
		assert.Equal(t, 0, refs[0].Provenance.Source)
		assert.Contains(t, refs[0].Message, "it names nothing in the document it points into")
		streams := byCode(diags, diag.StreamDocumentsDropped)
		require.Len(t, streams, 1)
		assert.Equal(t, ir.SeverityError, streams[0].Severity)
		assert.Equal(t, 1, streams[0].Provenance.Source, "other.yaml's entry")
		assert.Equal(t, ir.Position{Line: 3, Column: 1}, streams[0].Provenance.Position)
		assert.Equal(t, dropped, streams[0].Message)
	})

	t.Run("a reference into the first document", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		layout(t, dir, map[string]string{"main.yaml": root("A"), "other.yaml": stream})

		doc, diags := compileDir(t, dir, openapi.Options{})

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, diag.StreamDocumentsDropped, diags[0].Code)
		assert.Equal(t, 1, diags[0].Provenance.Source)
		assert.Equal(t, dropped, diags[0].Message)
		require.NotNil(t, doc)
		tree := irTree(t, doc)
		s := findByID(tree, "t/openapi/components/schemas/S")
		require.NotNil(t, s)
		base := s["base"].(map[string]any)["target"].(string)
		a := findByID(tree, base)
		require.NotNil(t, a)
		assert.Equal(t, "t/prim/string", a["base"].(map[string]any)["target"], "S is a string")
	})
}

// TestCompile_AStreamPastTheBoundIsAlwaysReported pins what the report says as
// the count of documents after the first runs through the bound of 1,024 and
// past it, for documents that hold nothing, that hold content and that do not
// parse.
func TestCompile_AStreamPastTheBoundIsAlwaysReported(t *testing.T) {
	t.Parallel()
	content := func(n int) string { return strings.Repeat("---\nfoo: 1\n", n) }
	tests := []struct {
		name, text, want string
	}{
		{"1,025 empty documents then content", srMin + strings.Repeat("---\n", 1025) + "---\nfoo: bar\n",
			"the 1024 after it hold no content, and what follows them was not read"},
		{"1,024 documents", srMin + content(1024), "the 1024 after it hold content the IR does not"},
		{"1,023 documents", srMin + content(1023), "the 1023 after it hold content the IR does not"},
		{"1,024 documents then one that does not parse", srMin + content(1024) + "---\n: : :\n",
			"the 1024 after it hold content the IR does not, and what follows could not be parsed as YAML"},
		{"1,025 documents", srMin + content(1025),
			"at least 1024 after it hold content the IR does not, and what follows the 1024 read was not read"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, diags := compileText(t, "spec.yaml", tc.text, openapi.Options{})

			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, diag.StreamDocumentsDropped, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, "only one document of the YAML stream was lowered; "+tc.want, diags[0].Message)
		})
	}
}

// TestCompile_ALaterDocumentOpeningWithADirectiveIsCountedNotUnparsed holds a
// %YAML 1.2 directive to what YAML 1.2 says: it is read, so a document after
// `...` that opens with one is a document, and one at the head of the source
// is read as the source.
func TestCompile_ALaterDocumentOpeningWithADirectiveIsCountedNotUnparsed(t *testing.T) {
	t.Parallel()

	for name, text := range map[string]string{
		"a directive before the document":     "%YAML 1.2\n---\n" + srMin,
		"a directive after a byte order mark": srBOM + "%YAML 1.2\n---\n" + srMin,
	} {
		doc, diags := compileText(t, "spec.yaml", text, openapi.Options{})

		require.NotNil(t, doc, name)
		assert.Empty(t, diags, name)
	}

	_, diags := compileText(t, "spec.yaml", srMin+"...\n%YAML 1.2\n---\nfoo: 1\n", openapi.Options{})
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.StreamDocumentsDropped, diags[0].Code)
	assert.Equal(t, "only one document of the YAML stream was lowered; the one after it holds content the IR does not",
		diags[0].Message)
}
