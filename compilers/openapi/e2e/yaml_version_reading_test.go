// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_ALaterYAMLMinorIsReadAsYAML12WithAWarning holds a %YAML directive
// declaring a later minor to what the specification of YAML asks: the document
// is read as 1.2 and a warning says so, at the directive. Another major is
// refused.
func TestCompile_ALaterYAMLMinorIsReadAsYAML12WithAWarning(t *testing.T) {
	t.Parallel()
	for _, minor := range []string{"1.3", "1.10"} {
		t.Run(minor, func(t *testing.T) {
			t.Parallel()

			doc, diags := compileText(t, "spec.yaml", "%YAML "+minor+"\n---\n"+srMin, openapi.Options{})

			require.NotNil(t, doc)
			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, srVersionReading, diags[0].Code)
			assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
			assert.Equal(t, ir.Provenance{Source: 0, Position: ir.Position{Line: 1, Column: 1}}, diags[0].Provenance,
				"a directive has no pointer, so the position alone places it")
			assert.Equal(t, "the document declares YAML "+minor+", a later version than the compiler reads; "+
				"it is read as YAML 1.2", diags[0].Message)
		})
	}

	doc, diags := compileText(t, "spec.yaml", "%YAML 2.0\n---\n"+srMin, openapi.Options{})
	assert.Nil(t, doc)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.UndecodableSource, diags[0].Code)
	assert.Contains(t, diags[0].Message, "found incompatible YAML document")
}

// TestCompile_NoWarningIsDrawnForADocumentDeclaringYAML12OrNothing holds the
// warning to the two cases the specification of YAML asks for one: a document
// that declares 1.2, or declares nothing, draws none, whatever it writes.
func TestCompile_NoWarningIsDrawnForADocumentDeclaringYAML12OrNothing(t *testing.T) {
	t.Parallel()
	body := srMin + "components:\n  schemas:\n    S:\n      type: object\n      properties:\n" +
		"        y: {type: string, enum: [yes, no, on]}\n        m: {type: integer, default: 0644}\n"
	for name, text := range map[string]string{
		"nothing":     body,
		"YAML 1.2":    "%YAML 1.2\n---\n" + body,
		"a break":     srMin + "x-b: \"a" + srNEL + "b\"\n",
		"a 1.2 break": "%YAML 1.2\n---\n" + srMin + "x-b: \"a" + srNEL + "b\"\n",
	} {
		doc, diags := compileText(t, "spec.yaml", text, openapi.Options{})

		require.NotNil(t, doc, name)
		assert.Empty(t, byCode(diags, srVersionReading), name)
	}
}

// yaml11Body is a document whose plain scalars read otherwise under YAML 1.1:
// one of each kind the warning words, and four it does not warn at.
const yaml11Body = "openapi: 3.1.0\ninfo: {title: t, version: \"1\", description: \"a" + srNEL + "b\"}\npaths: {}\n" +
	"components:\n  schemas:\n    S:\n      type: object\n      properties:\n" +
	"        m: {type: integer, default: 0644}\n" +
	"        e: {type: string, enum: [yes, n, abc]}\n" +
	"        t: {type: string, default: 1:30}\n" +
	"        o: {type: integer, default: 0o17}\n" +
	"        f: {type: number, default: 1e3}\n" +
	"        h: {type: string, default: 0X1F}\n" +
	"        y: {type: string}\n"

// TestCompile_ADocumentDeclaringYAML11IsWarnedAtWhatItReadsOtherwise holds a
// document declaring 1.1 to one warning for each thing YAML 1.1 reads as another
// JSON value, at the node holding it, and to the YAML 1.2 reading in the IR.
func TestCompile_ADocumentDeclaringYAML11IsWarnedAtWhatItReadsOtherwise(t *testing.T) {
	t.Parallel()
	const s = "/components/schemas/S/properties/"
	const tail = ", which the compiler reads; YAML 1.1, which the document declares, "
	want := map[string]string{
		"/info/description": "the scalar holds U+0085, a character in YAML 1.2, which the compiler reads; " +
			"YAML 1.1, which the document declares, ends a line there",
		s + "m/default": `plain scalar "0644" is the integer 644 in YAML 1.2` + tail + "reads the integer 420",
		s + "e/enum/0":  `plain scalar "yes" is a string in YAML 1.2` + tail + "reads the boolean true",
		s + "e/enum/1":  `plain scalar "n" is a string in YAML 1.2` + tail + "reads the boolean false",
		s + "t/default": `plain scalar "1:30" is a string in YAML 1.2` + tail + "reads the integer 90",
		s + "o/default": `plain scalar "0o17" is the integer 15 in YAML 1.2` + tail + "reads a string",
		s + "f/default": `plain scalar "1e3" is the number 1000 in YAML 1.2` + tail + "reads a string",
	}

	doc, diags := compileText(t, "spec.yaml", "%YAML 1.1\n---\n"+yaml11Body, openapi.Options{})

	require.NotNil(t, doc)
	got := map[string]string{}
	for _, d := range byCode(diags, srVersionReading) {
		assert.Equal(t, ir.SeverityWarning, d.Severity)
		assert.Equal(t, 0, d.Provenance.Source)
		got[string(d.Provenance.Pointer)] = d.Message
	}
	assert.Equal(t, want, got, "none for 0X1F, abc, the quoted version, the key y or openapi: 3.1.0")

	tree := irTree(t, doc)
	defaults := map[string]map[string]any{
		"m": {"kind": "number", "num": "644"},
		"t": {"kind": "string", "str": "1:30"},
		"o": {"kind": "number", "num": "15"},
		"f": {"kind": "number", "num": "1e3"},
		"h": {"kind": "string", "str": "0X1F"},
	}
	for name, d := range defaults {
		p := findByID(tree, "p/openapi"+s+name)
		require.NotNil(t, p, name)
		assert.Equal(t, d, p["default"], name)
	}
	assert.Equal(t, "a"+srNEL+"b", doc.Docs.Description)

	twin, twinDiags := compileText(t, "spec.yaml", yaml11Body, openapi.Options{})
	assert.Equal(t, jsonOf(t, twin), jsonOf(t, doc), "the document without its directive reads the same")
	assert.Empty(t, byCode(twinDiags, srVersionReading))
}

// TestCompile_ADirectiveOfALaterDocumentGovernsNothingTheTreeHolds holds the
// directive to the document taken: a later document's, which the compile drops,
// declares nothing for the first.
func TestCompile_ADirectiveOfALaterDocumentGovernsNothingTheTreeHolds(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: t, version: \"1\", description: \"a" + srNEL + "b\"}\npaths: {}\n" +
		"...\n%YAML 1.1\n---\nfoo: 1\n"

	_, diags := compileText(t, "spec.yaml", src, openapi.Options{})

	assert.Empty(t, byCode(diags, srVersionReading))
	require.Len(t, byCode(diags, diag.StreamDocumentsDropped), 1, "%+v", diags)
	assert.Equal(t, ir.SeverityError, byCode(diags, diag.StreamDocumentsDropped)[0].Severity)
}

// TestCompile_AnOverlayAndAnExternalDocumentDeclaringYAML11AreWarnedInTheirOwnEntry
// holds the warning to the entry of the input that declares the version.
func TestCompile_AnOverlayAndAnExternalDocumentDeclaringYAML11AreWarnedInTheirOwnEntry(t *testing.T) {
	t.Parallel()
	overlay := "%YAML 1.1\n---\n" + srOverlayFirst + "x-n: yes\n"

	_, diags := compileOverlaid(t, overlay)

	got := byCode(diags, srVersionReading)
	require.Len(t, got, 1, "%+v", diags)
	assert.Equal(t, 1, got[0].Provenance.Source, "the overlay's entry")
	assert.Equal(t, `/x-n`, string(got[0].Provenance.Pointer))

	dir := t.TempDir()
	layout(t, dir, map[string]string{
		"main.yaml":  srMin + "components: {schemas: {S: {$ref: \"other.yaml#/A\"}}}\n",
		"other.yaml": "%YAML 1.1\n---\nA: {type: string, default: yes}\n",
	})
	_, diags = compileDir(t, dir, openapi.Options{})
	got = byCode(diags, srVersionReading)
	require.Len(t, got, 1, "%+v", diags)
	assert.Equal(t, 1, got[0].Provenance.Source, "the external document's entry")
	assert.Equal(t, "/A/default", string(got[0].Provenance.Pointer))
}
