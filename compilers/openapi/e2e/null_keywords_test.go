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
	positions := []struct {
		name, format string
		contexts     []string
	}{
		{"component", "{%s}", []string{"", "type: object, ", "type: string, ", "type: array, items: {}, ", "const: null, "}},
		{"property", "{type: object, properties: {p: {%s}}}", []string{"", "const: null, ", "nullable: true, "}},
		{"allOf branch", "{allOf: [{%s}]}", []string{"", "type: object, "}},
	}
	versions := []struct {
		name  string
		minor oas.Minor
	}{{"3.0.3", oas.V30}, {"3.1.0", oas.V31}}
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
							return fmt.Sprintf("openapi: %s\ninfo: {title: t, version: \"1\"}\npaths: {}\n"+
								"components:\n  schemas:\n    S: %s\n", version.name, fmt.Sprintf(position.format, schema))
						}
						removed, removedDiags := compileSpecText(t, spec(strings.TrimSuffix(context, ", ")))
						written, writtenDiags := compileSpecText(t, spec(context+fmt.Sprintf("%q: null", keyword)))
						assert.Empty(t, cmp.Diff(removed.Types, written.Types), "a null keyword is the keyword left out")
						assert.Empty(t, cmp.Diff(removedDiags, withoutNullWarning(t, writtenDiags, keyword), cmpopts.EquateEmpty()))
					})
				}
			}
		}
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
