// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// uEsc spells a \uXXXX escape from its four hex digits, so the halves of a
// surrogate pair are never adjacent where this file is written: an editor or a
// tool may fold a pair into the character it names, and a fixture that holds
// the character then compiles without the reader these tests are for.
func uEsc(hex string) string { return `\u` + hex }

// astralPair is the escape of U+1F600 as JSON writes it, a surrogate pair.
var astralPair = uEsc("d83d") + uEsc("de00")

// jsonSlots is a JSON source in which every <SLOT> is where a row writes the
// text it holds to. A name is followed by a line break before its value, so a
// finding at the value sits at one position however wide the name is written.
const jsonSlots = `{
  "openapi": "3.0.3",
  "info": {"title": "t", "version": "1", "description": "d<DESC>"},
  "paths": {
    "/p<PATH>":
    {
      "get": {"operationId": "get", "responses": {"200": {"description": "ok"}}}
    }
  },
  "components": {
    "schemas": {
      "S<NAME>":
      {"type": "string", "enum": ["e<ENUM>"]},
      "T": {
        "type": "object",
        "properties": {
          "p<PROP>":
          {"type": "string", "description": "t<TDESC>"}
        }
      }
    }
  }
}
`

// fill returns jsonSlots with the text of each slot named in with, and the
// others empty.
func fill(with map[string]string) string {
	src := jsonSlots
	for _, slot := range []string{"DESC", "PATH", "NAME", "ENUM", "PROP", "TDESC"} {
		src = strings.ReplaceAll(src, "<"+slot+">", with[slot])
	}
	return src
}

// escapeRow is one spelling of a JSON string that the YAML parser refuses or
// misreads: what a source writes at a slot, what its twin writes there, and what
// the IR must hold for it. The twin spells the same string as the parser reads.
type escapeRow struct {
	slot, escaped, twin string
	// mark is the text the escaped source must carry, to hold the fixture to
	// the spelling the row is about.
	mark string
	// want is the string the IR holds, as JSON writes it.
	want string
}

// TestCompile_AnEscapedJSONSourceCompilesAsItsLiteralTwin holds a JSON source
// that writes a string in a spelling the YAML parser refuses or misreads to the
// IR and the diagnostics of its twin, the same string spelled as the parser
// reads it (docs/compilers/openapi.md#yaml-decoding). A row is a spelling at a
// position: a surrogate pair in an enum member, description, schema name,
// property name and path, either case of its hex, a solidus escape in a value
// and a name, and a raw DEL, C1 control, U+FFFE, U+FFFF, NEL, line separator
// and paragraph separator, where the NEL is kept and the parser folds it to a
// space.
func TestCompile_AnEscapedJSONSourceCompilesAsItsLiteralTwin(t *testing.T) {
	t.Parallel()
	const astral = "\U0001F600"
	raw := func(slot, char, escape string) escapeRow {
		return escapeRow{slot, "x" + char + "y", "x" + escape + "y", char, "x" + char + "y"}
	}
	rows := map[string]escapeRow{
		"a surrogate pair in an enum member":       {"ENUM", astralPair, astral, astralPair, astral},
		"a surrogate pair in upper case":           {"ENUM", uEsc("D83D") + uEsc("DE00"), astral, uEsc("D83D"), astral},
		"a surrogate pair in mixed case":           {"ENUM", uEsc("d83D") + uEsc("De00"), astral, uEsc("d83D"), astral},
		"a surrogate pair in a description":        {"DESC", astralPair, astral, astralPair, astral},
		"a surrogate pair in a schema description": {"TDESC", "a" + astralPair + "b", "a" + astral + "b", astralPair, "a" + astral + "b"},
		"a surrogate pair in a schema name":        {"NAME", astralPair, astral, astralPair, "S" + astral},
		"a surrogate pair in a property name":      {"PROP", astralPair, astral, astralPair, "p" + astral},
		"a surrogate pair in a path":               {"PATH", astralPair, astral, astralPair, "/p" + astral},
		"a solidus escape in a description":        {"DESC", `a\/b`, "a/b", `\/`, "da/b"},
		"a solidus escape in a path":               {"PATH", `\/q`, "/q", `\/`, "/p/q"},
		"a solidus escape in a schema name":        {"NAME", `\/q`, "/q", `\/`, "S/q"},
		"a raw DEL in an enum member":              raw("ENUM", "\u007f", uEsc("007f")),
		"a raw C1 control":                         raw("ENUM", "\u0080", uEsc("0080")),
		"the last raw C1 control":                  raw("ENUM", "\u009f", uEsc("009f")),
		"a raw U+FFFE":                             raw("ENUM", "\ufffe", uEsc("fffe")),
		"a raw U+FFFF":                             raw("ENUM", "\uffff", uEsc("ffff")),
		"a raw NEL in a description":               raw("DESC", "\u0085", uEsc("0085")),
		"a raw NEL in an enum member":              raw("ENUM", "\u0085", uEsc("0085")),
		"a raw line separator":                     raw("DESC", "\u2028", uEsc("2028")),
		"a raw paragraph separator":                raw("DESC", "\u2029", uEsc("2029")),
	}
	for name, row := range rows {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			escaped, twin := fill(map[string]string{row.slot: row.escaped}), fill(map[string]string{row.slot: row.twin})
			require.Contains(t, escaped, row.mark, "the fixture carries the spelling the row is about")

			gotDoc, gotDiags := parseFull(t, escaped)
			wantDoc, wantDiags := parseFull(t, twin)

			assert.Empty(t, cmp.Diff(withoutSourceHash(wantDoc), withoutSourceHash(gotDoc)), "the IR of the twin (-twin +escaped)")
			assert.Empty(t, cmp.Diff(wantDiags, gotDiags), "the diagnostics of the twin (-twin +escaped)")
			lowered, err := json.Marshal(gotDoc)
			require.NoError(t, err)
			assert.Contains(t, string(lowered), row.want, "the string the source writes")
		})
	}
}

// TestCompile_AJSONSourceWithATabBeforeTheRootCompilesAsItsTwin holds a source
// that opens with a tab, which JSON allows before its value and the YAML
// parser refuses, to the source that opens with a space, so every position
// agrees.
func TestCompile_AJSONSourceWithATabBeforeTheRootCompilesAsItsTwin(t *testing.T) {
	t.Parallel()
	gotDoc, gotDiags := parseFull(t, "\t"+fill(nil))
	wantDoc, wantDiags := parseFull(t, " "+fill(nil))

	assert.Empty(t, cmp.Diff(withoutSourceHash(wantDoc), withoutSourceHash(gotDoc)),
		"the IR of the twin (-twin +tab)")
	assert.Empty(t, cmp.Diff(wantDiags, gotDiags), "the diagnostics of the twin (-twin +tab)")
}

// TestCompile_AJSONSourceWithAKeyPastTheYAMLLimitCompiles holds a source whose
// path and property name are longer than the YAML parser allows a key (its
// colon within 1,024 characters of the key's start), which JSON limits not at
// all, to a clean compile that keeps each key whole.
func TestCompile_AJSONSourceWithAKeyPastTheYAMLLimitCompiles(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("k", 1100)

	doc, diags := parseFull(t, fill(map[string]string{"PATH": long, "PROP": long}))

	openapitest.RequireNoErrorDiags(t, diags)
	lowered, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.Contains(t, string(lowered), `"/p`+long+`"`, "the path is kept whole")
	assert.Contains(t, string(lowered), `"p`+long+`"`, "and so is the property name")
}

// externalJSON is an external document, JSON, whose operation writes slot
// twice, as a summary and as a response description, each on a line of its own.
const externalJSON = `{
  "openapi": "3.1.0",
  "info": {"title": "O", "version": "1"},
  "paths": {
    "/x": {
      "get": {
        "operationId": "getX",
        "summary": "s<X>",
        "responses": {
          "200": {
            "description": "ok<X>"
          }
        }
      }
    }
  }
}
`

// TestCompile_AJSONExternalDocumentCompilesAsItsLiteralTwin extends the twin
// comparison to a document an external reference names, read from a file and
// over HTTP, since each document the load phase reads is parsed apart from the
// source (docs/compilers/openapi.md#yaml-decoding).
func TestCompile_AJSONExternalDocumentCompilesAsItsLiteralTwin(t *testing.T) {
	t.Parallel()
	const astral = "\U0001F600"
	for name, row := range map[string]struct{ escaped, twin string }{
		"a surrogate pair": {astralPair, astral},
		"a solidus escape": {`a\/b`, "a/b"},
		"a raw NEL":        {"\u0085", uEsc("0085")},
		"a raw DEL":        {"\u007f", uEsc("007f")},
		"a raw U+FFFF":     {"\uffff", uEsc("ffff")},
	} {
		escaped := strings.ReplaceAll(externalJSON, "<X>", row.escaped)
		twin := strings.ReplaceAll(externalJSON, "<X>", row.twin)
		require.NotEqual(t, escaped, twin)

		t.Run(name+" in a file", func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(compileBeside(t, twin), compileBeside(t, escaped)); diff != "" {
				t.Errorf("the escaped document compiled to other IR than its twin (-twin +escaped):\n%s", diff)
			}
		})
		t.Run(name+" over HTTP", func(t *testing.T) {
			t.Parallel()
			gotEscaped, gotTwin := compileServed(t, escaped, twin)
			if diff := cmp.Diff(gotTwin, gotEscaped); diff != "" {
				t.Errorf("the escaped document compiled to other IR than its twin (-twin +escaped):\n%s", diff)
			}
		})
	}
}

// TestCompile_AJSONOverlayAppliesAsItsLiteralTwin extends the twin comparison
// to a JSON overlay, whose document the library parses with the YAML parser
// unless it is read as JSON first (docs/compilers/openapi.md#overlays).
func TestCompile_AJSONOverlayAppliesAsItsLiteralTwin(t *testing.T) {
	t.Parallel()
	const patch = `{
  "overlay": "1.0.0",
  "info": {"title": "Patch", "version": "1"},
  "actions": [
    {
      "target": "$.components.schemas.Pet",
      "update": {
        "description": "<X>"
      }
    }
  ]
}
`
	const astral = "\U0001F600"
	for name, row := range map[string]struct{ escaped, twin, want string }{
		"a surrogate pair": {astralPair, astral, astral},
		"a solidus escape": {`a\/b`, "a/b", "a/b"},
		"a raw NEL":        {"\u0085", uEsc("0085"), "\u0085"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compile := func(x string) (*ir.Document, []ir.Diagnostic) {
				data := strings.ReplaceAll(patch, "<X>", x)
				doc, diags := compileWith(t, openapi.Options{Overlay: &openapi.Overlay{Path: "patch.json", Data: []byte(data)}})
				return withoutSourceHash(doc), diags
			}
			gotDoc, gotDiags := compile(row.escaped)
			wantDoc, wantDiags := compile(row.twin)

			assert.Empty(t, cmp.Diff(wantDoc, gotDoc), "the IR of the twin (-twin +escaped)")
			assert.Empty(t, cmp.Diff(wantDiags, gotDiags), "the diagnostics of the twin (-twin +escaped)")
			lowered, err := json.Marshal(gotDoc)
			require.NoError(t, err)
			assert.Contains(t, string(lowered), row.want, "the string the overlay writes")
		})
	}
}

// halfEscape is half of a surrogate pair, written with no other half.
var halfEscape = uEsc("d83d")

// TestCompile_ALoneSurrogateIsRefusedWhereItIsWritten holds a lone surrogate
// escape to a refusal that names the escape as written and where it begins, at
// each route a JSON document enters by: the source, an external document and an
// overlay.
func TestCompile_ALoneSurrogateIsRefusedWhereItIsWritten(t *testing.T) {
	t.Parallel()
	want := `the escape ` + halfEscape + ` is half of a UTF-16 surrogate pair, ` +
		`written without its other half, so it names no character`

	t.Run("in the source", func(t *testing.T) {
		t.Parallel()
		src := fill(map[string]string{"DESC": halfEscape})
		_, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(src)}, compilers.Options{})

		require.NoError(t, err)
		msg := openapitest.DiagMessageAt(t, diags, diag.UndecodableSource, ir.SeverityError, "")
		assert.Contains(t, msg, "line 3, column "+columnOf(t, src, halfEscape)+": "+want)
	})
	t.Run("in an external document", func(t *testing.T) {
		t.Parallel()
		ext := strings.ReplaceAll(externalJSON, "<X>", halfEscape)
		const root = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /x: {$ref: \"./other.yaml#/paths/~1x\"}\n"
		diags := compileBesideExpectingErrors(t, ext, root)

		require.True(t, openapitest.HasDiag(diags, diag.UnresolvedRef), "%+v", diags)
		d, _ := openapitest.FirstDiag(diags, diag.UnresolvedRef)
		assert.Contains(t, d.Message, "does not parse as YAML: line 8, column "+columnOf(t, ext, halfEscape)+": "+want)
	})
	t.Run("in an overlay", func(t *testing.T) {
		t.Parallel()
		patch := "{\n  \"overlay\": \"1.0.0\",\n  \"info\": {\"title\": \"P\", \"version\": \"1\"},\n" +
			"  \"actions\": [{\"target\": \"$.info\", \"update\": {\"description\": \"" + halfEscape + "\"}}]\n}\n"
		_, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(overlaySpec)},
			compilers.Options{FormatOptions: openapi.Options{Overlay: &openapi.Overlay{Path: "patch.json", Data: []byte(patch)}}})

		require.NoError(t, err)
		msg := openapitest.DiagMessageAt(t, diags, diag.OverlayInvalid, ir.SeverityError, "")
		assert.Contains(t, msg, "cannot parse overlay: line 4, column "+columnOf(t, patch, halfEscape)+": "+want)
	})
}

// columnOf returns, as text, the column at which needle begins on its line in
// src, counted in characters from one.
func columnOf(t *testing.T, src, needle string) string {
	t.Helper()
	at := strings.Index(src, needle)
	require.GreaterOrEqual(t, at, 0, "the fixture carries %q", needle)
	line := src[strings.LastIndex(src[:at], "\n")+1 : at]
	return strconv.Itoa(len([]rune(line)) + 1)
}

// TestCompile_APositionAfterAnEscapeCountsItAsWritten holds the position of a
// finding that follows a surrogate pair on its line to where the source writes
// it: as far along as with twelve single characters in the pair's place, and
// eleven columns past the source written with the character itself. It holds
// a raw NEL, line separator and paragraph separator to ending no line: a
// finding after one is on line one, at the column a single character leaves
// it (docs/compilers/openapi.md#yaml-decoding).
func TestCompile_APositionAfterAnEscapeCountsItAsWritten(t *testing.T) {
	t.Parallel()
	const spec = `{"openapi":"3.1.0","info":{"title":"x<T>y","version":"1"},"paths":{"/x":{"get":` +
		`{"parameters":[{"name":"h","in":"header","allowReserved":true,"schema":{"type":"string"}}],` +
		`"responses":{"200":{"description":"ok"}}}}}}` + "\n"
	columnOfFinding := func(title string) int {
		_, diags := parseFull(t, strings.ReplaceAll(spec, "<T>", title))
		d, ok := openapitest.FirstDiag(diags, diag.InapplicableLocationKeyword)
		require.True(t, ok, "the finding the fixture draws: %+v", diags)
		require.Equal(t, 1, d.Provenance.Position.Line)
		return d.Provenance.Position.Column
	}

	escaped := columnOfFinding(astralPair)

	assert.Equal(t, columnOfFinding(strings.Repeat("z", len(astralPair))), escaped, "twelve characters in its place")
	assert.Equal(t, escaped-(len(astralPair)-1), columnOfFinding("\U0001F600"), "one character in its place")
	for _, char := range []string{"\u0085", "\u2028", "\u2029"} {
		assert.Equal(t, columnOfFinding("z"), columnOfFinding(char), "%U ends no line", []rune(char)[0])
	}
}
