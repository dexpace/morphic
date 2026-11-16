// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// firstWrittenAt reads the pointer a repeated entry's message names.
var firstWrittenAt = regexp.MustCompile(` first written at (\S+) in `)

// repeatingKeyword writes a schema holding list, which repeats value, at
// suffix under the schema, in a document whose dialect line reads it.
type repeatingKeyword struct {
	schema  func(list string) string
	list    string
	suffix  string
	value   string
	dialect string
}

// TestCompile_ARepeatedEntryNamesItsFirstCopyWhereItsDocumentHoldsIt holds a
// repeated entry's message to name the copy it repeats where a finding on that
// copy is placed, a pointer the finding's document holds: in a schema written
// in place, in one a $ref lands on under an x- key or in another document, and
// in a list two aliases mount, in either order, which draws one finding.
func TestCompile_ARepeatedEntryNamesItsFirstCopyWhereItsDocumentHoldsIt(t *testing.T) {
	t.Parallel()
	keywords := map[string]repeatingKeyword{
		"required": {schema: func(l string) string { return "{type: object, required: " + l + "}" },
			list: "[a, a]", suffix: "/required", value: "a"},
		"dependentRequired": {schema: func(l string) string { return "{dependentRequired: {a: " + l + "}}" },
			list: "[b, b]", suffix: "/dependentRequired/a", value: "b"},
		"dependencies": {schema: func(l string) string { return "{dependencies: {a: " + l + "}}" },
			list: "[b, b]", suffix: "/dependencies/a", value: "b",
			dialect: "jsonSchemaDialect: http://json-schema.org/draft-07/schema#\n"},
	}
	for name, k := range keywords {
		head := head31 + k.dialect + "paths: {}\n"
		raw := head + "x-raw:\n  T: " + k.schema(k.list) + "\n"
		schemas := head + "components:\n  schemas:\n"
		for form, c := range map[string]struct {
			files      map[string]string
			file, base string
		}{
			"in place": {map[string]string{"main.yaml": schemas + "    S: " + k.schema(k.list) + "\n"},
				"main.yaml", "/components/schemas/S"},
			"under an x- key": {map[string]string{"main.yaml": raw + "components:\n  schemas:\n    S: {$ref: '#/x-raw/T'}\n"},
				"main.yaml", "/x-raw/T"},
			"in another document": {map[string]string{
				"main.yaml": schemas + "    S: {$ref: 'other.yaml#/x-raw/T'}\n", "other.yaml": raw,
			}, "other.yaml", "/x-raw/T"},
			"anchored at the later mount": {map[string]string{"main.yaml": schemas +
				"    B: " + k.schema("&r "+k.list) + "\n    A: " + k.schema("*r") + "\n"}, "main.yaml", "/components/schemas/A"},
			"anchored at the least mount": {map[string]string{"main.yaml": schemas +
				"    A: " + k.schema("&r "+k.list) + "\n    B: " + k.schema("*r") + "\n"}, "main.yaml", "/components/schemas/A"},
		} {
			t.Run(name+" "+form, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				layout(t, dir, c.files)

				doc, diags := compileDir(t, dir, openapi.Options{})

				require.NotNil(t, doc, "%+v", diags)
				var repeats []ir.Diagnostic
				for _, d := range diags {
					if d.Code == diag.Validation+"/validation-duplicate-key" {
						repeats = append(repeats, d)
					}
				}
				require.Len(t, repeats, 1, "%+v", diags)
				d := repeats[0]
				assert.Equal(t, c.file, filepath.Base(doc.Sources[d.Provenance.Source].Path))
				assert.Equal(t, c.base+k.suffix+"/1", string(d.Provenance.Pointer))
				named := firstWrittenAt.FindStringSubmatch(d.Message)
				require.Len(t, named, 2, d.Message)
				assert.Equal(t, c.base+k.suffix+"/0", named[1])
				assert.Equal(t, k.value, valueAt(t, c.files[c.file], jsontext.Pointer(named[1])), d.Message)
			})
		}
	}
}

// valueAt returns what the YAML text holds at pointer, read as JSON reads it,
// its aliases and merge keys followed, or nil where it holds nothing there.
func valueAt(t *testing.T, text string, pointer jsontext.Pointer) any {
	t.Helper()
	var at any
	require.NoError(t, yaml.Unmarshal([]byte(text), &at))
	for token := range pointer.Tokens() {
		switch v := at.(type) {
		case map[string]any:
			at = v[token]
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(v) {
				return nil
			}
			at = v[i]
		default:
			return nil
		}
	}
	return at
}
