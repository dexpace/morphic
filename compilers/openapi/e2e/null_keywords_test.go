// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test // external test package — exercises only the public API

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_ANullKeywordLowersAsTheKeywordLeftOut holds every keyword the
// table gives a Schema Object to one reading of a null: the document lowers
// exactly as it does with the key removed, whatever node the keyword sits on and
// wherever the schema is written, and the load phase's warning for it is the
// only report. It reads the keywords off the table, so one the table gains is
// held too. An allOf branch is asked only where nothing else makes it residue,
// since residue keeps the whole branch as written, the null included.
func TestCompile_ANullKeywordLowersAsTheKeywordLeftOut(t *testing.T) {
	t.Parallel()
	const noPaths = "paths: {}\n"
	const parameter = `paths:
  /a:
    get:
      parameters: [{name: q, in: query, schema: {%s}}]
      responses: {'200': {description: ok}}
`
	positions := []struct {
		name, paths, format string
		contexts            []string
	}{
		{"component", noPaths, "{%s}", []string{"", "type: object, ", "type: string, ", "type: array, items: {}, ", "const: null, "}},
		{"property", noPaths, "{type: object, properties: {p: {%s}}}", []string{"", "const: null, ", "nullable: true, "}},
		{"allOf branch", noPaths, "{allOf: [{%s}]}", []string{"", "type: object, "}},
		{"parameter", parameter, "{}", []string{"", "type: string, "}},
	}
	versions := []struct {
		name  string
		minor oas.Minor
	}{{"3.0.3", oas.V30}, {"3.1.0", oas.V31}, {"3.2.0", oas.V32}}
	for _, version := range versions {
		for _, keyword := range nullableKeywords(t, version.minor) {
			for _, position := range positions {
				for _, context := range position.contexts {
					if strings.Contains(context, keyword+":") {
						continue // a key written twice is another rule's
					}
					t.Run(version.name+"/"+position.name+"/"+context+keyword, func(t *testing.T) {
						t.Parallel()
						spec := func(schema string) string {
							paths, component := position.paths, position.format
							if position.paths == parameter {
								paths = fmt.Sprintf(parameter, schema)
							} else {
								component = fmt.Sprintf(position.format, schema)
							}
							return fmt.Sprintf(`openapi: %s
info: {title: t, version: "1"}
%scomponents:
  schemas:
    S: %s
`, version.name, paths, component)
						}
						removed, removedDiags := compileSpecText(t, spec(strings.TrimSuffix(context, ", ")))
						written, writtenDiags := compileSpecText(t, spec(context+fmt.Sprintf("%q: null", keyword)))
						assert.Empty(t, cmp.Diff(removed.Types, written.Types), "a null keyword is the keyword left out")
						assert.Empty(t, cmp.Diff(removed.Services, written.Services), "and so at a carrier")
						assert.Empty(t, cmp.Diff(removedDiags, withoutNullWarning(t, writtenDiags, keyword), cmpopts.EquateEmpty()))
					})
				}
			}
		}
	}
}

// TestCompile_ANullIdentifierLowersAsTheKeywordLeftOut holds the two readers of
// the raw tree that decide whether a $dynamicRef expands, which no keyword of
// the sweep above reaches, as it writes no $dynamicRef: a null $id at or above
// either end starts no resource, and a null $dynamicAnchor is no anchor named
// "null".
func TestCompile_ANullIdentifierLowersAsTheKeywordLeftOut(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, keyword, schemas string
	}{
		{"beside the reference", "$id", `
    Anchored: {$dynamicAnchor: dyn, type: object}
    S: {$dynamicRef: '#dyn'%s}
`},
		{"above the reference", "$id", `
    Anchored: {$dynamicAnchor: dyn, type: object}
    S: {type: object%s, properties: {p: {$dynamicRef: '#dyn'}}}
`},
		{"beside the anchor", "$id", `
    Anchored: {$dynamicAnchor: dyn, type: object%s}
    S: {$dynamicRef: '#dyn'}
`},
		{"as the anchor a reference spells", "$dynamicAnchor", `
    Anchored: {type: object%s}
    S: {$dynamicRef: '#null'}
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := func(written string) string {
				return `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:` + fmt.Sprintf(tc.schemas, written)
			}
			removed, removedDiags := compileSpecText(t, spec(""))
			written, writtenDiags := compileSpecText(t, spec(fmt.Sprintf(", %q: null", tc.keyword)))
			assert.Empty(t, cmp.Diff(removed.Types, written.Types))
			assert.Empty(t, cmp.Diff(removedDiags, withoutNullWarning(t, writtenDiags, tc.keyword), cmpopts.EquateEmpty()))
		})
	}
}

// nullableKeywords returns the keywords the table gives a Schema Object at
// minor, but those whose value is an instance, of which a null is one.
func nullableKeywords(t *testing.T, minor oas.Minor) []string {
	t.Helper()
	schema, ok := oas.Table().Object(oas.Schema)
	require.True(t, ok)
	var out []string
	for _, f := range schema.Fields {
		if f.Defined.Holds(minor) && f.Shape != oas.Any {
			out = append(out, f.Name)
		}
	}
	require.NotEmpty(t, out)
	slices.Sort(out)
	return slices.Compact(out)
}

// withoutNullWarning returns diags without what the load phase reports at
// keyword, written null: a warning, at least one, as a later minor's keyword
// draws a second in a 3.0 document. It requires one.
func withoutNullWarning(t *testing.T, diags []ir.Diagnostic, keyword string) []ir.Diagnostic {
	t.Helper()
	found := false
	out := slices.DeleteFunc(slices.Clone(diags), func(d ir.Diagnostic) bool {
		if !strings.HasSuffix(string(d.Provenance.Pointer), "/"+strings.ReplaceAll(keyword, "/", "~1")) {
			return false
		}
		require.Equal(t, ir.SeverityWarning, d.Severity, d.Message)
		found = true
		return true
	})
	assert.True(t, found, "the load phase reports the null at the keyword")
	return out
}

// compileSpecText compiles src as spec.yaml and refuses an error from either
// the compile or its diagnostics.
func compileSpecText(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}}, compilers.Options{})
	require.NoError(t, err)
	assertNoErrorDiags(t, diags)
	return doc, diags
}
