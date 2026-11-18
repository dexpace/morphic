// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// emptyMapPosition is a position a keyword is written at: a schema template
// taking the keyword's text, and where the report of the kept keyword sits,
// under its schema's pointer.
type emptyMapPosition struct {
	name, schema, at string
}

// emptyMapPositions are the positions of the matrix: a component, a property,
// an inline allOf entry beside a $ref, a union branch, an array's items, and a
// property, an array's items and a union branch each beside a $ref.
var emptyMapPositions = []emptyMapPosition{
	{"component", "C%d: {type: object, %s}", ""},
	{"property", "P%d: {type: object, properties: {p: {type: object, %s}}}", "/properties/p"},
	{"entry", "E%d: {allOf: [{$ref: '#/components/schemas/T'}, {%s}]}", "/allOf/1"},
	{"branch", "B%d: {oneOf: [{type: object, %s}, {type: integer}]}", "/oneOf/0"},
	{"items", "I%d: {type: array, items: {type: object, %s}}", "/items"},
	{"beside a $ref", "R%d: {type: object, properties: {r: {$ref: '#/components/schemas/T', %s}}}", "/properties/r"},
	{"items beside a $ref", "J%d: {type: array, items: {$ref: '#/components/schemas/T', %s}}", "/items"},
	{"branch beside a $ref", "K%d: {oneOf: [{$ref: '#/components/schemas/T', %s}, {type: integer}]}", "/oneOf/0"},
}

// emptyMapRow is a keyword of the matrix with how it lowers in a dialect, the
// document's default where none is named: written empty and written with an
// entry, the Unmodeled entry and reason it is kept under, whether an empty one
// is left out, the code reporting a kept one and whether that report sits at
// the keyword or at its schema, and the positions it is written at, all where
// none are named.
type emptyMapRow struct {
	name      string
	dialect   string
	keyword   string
	empty     string
	full      string
	entry     string
	reason    ir.UnmodeledReason
	leftOut   bool
	code      string
	atKeyword bool
	positions []string
}

// emptyMapRows are the keywords of the matrix.
var emptyMapRows = []emptyMapRow{
	{"dependentSchemas", "", "dependentSchemas", "{}", "{a: {required: [b]}}", "openapi:dependentSchemas",
		ir.ReasonValidationOnly, true, diag.ValidationOnlyKeyword, false, nil},
	{"dependentRequired", "", "dependentRequired", "{}", "{a: [b]}", "openapi:dependentRequired",
		ir.ReasonValidationOnly, true, diag.ValidationOnlyKeyword, false, nil},
	{"dependencies in a draft", "'http://json-schema.org/draft-07/schema#'", "dependencies", "{}", "{a: [b]}",
		"openapi:dependencies", ir.ReasonValidationOnly, true, diag.ValidationOnlyKeyword, false,
		[]string{"component", "property", "entry", "branch", "items"}},
	{"dependencies in 2020-12", "", "dependencies", "{}", "{a: [b]}", "openapi:dependencies",
		ir.ReasonOutOfScope, false, diag.UnknownSchemaKeyword, true, []string{"component", "property"}},
	{"definitions in 2020-12", "", "definitions", "{}", "{a: {type: string}}", "openapi:definitions",
		ir.ReasonOutOfScope, false, diag.UnknownSchemaKeyword, true, []string{"component", "property"}},
	{"not", "", "not", "{}", "{type: string}", "openapi:not",
		ir.ReasonValidationOnly, false, diag.ValidationOnlyKeyword, false, []string{"component", "property"}},
	{"if", "", "if", "{}", "{type: string}", "openapi:if-then-else",
		ir.ReasonValidationOnly, false, diag.ValidationOnlyKeyword, false, []string{"component", "property"}},
	{"propertyNames", "", "propertyNames", "{}", "{type: string}", "openapi:propertyNames",
		ir.ReasonValidationOnly, false, diag.ValidationOnlyKeyword, false, []string{"component", "property"}},
	{"contains", "", "contains", "{}", "{type: string}", "openapi:contains",
		ir.ReasonValidationOnly, false, diag.ValidationOnlyKeyword, false, []string{"component", "property"}},
}

// emptyMapSpec writes the positions of a row, each with the keyword as written
// (none where written is empty), in a document read in the
// dialect, the components declared in reverse order where reversed.
func emptyMapSpec(dialect string, positions []emptyMapPosition, keyword, written string, reversed bool) string {
	schemas := make([]string, 0, 1+len(positions))
	schemas = append(schemas, "T: {type: string}")
	for i, p := range positions {
		text := keyword + ": " + written
		if written == "" {
			text = ""
		}
		schemas = append(schemas, fmt.Sprintf(p.schema, i, text))
	}
	if reversed {
		slices.Reverse(schemas)
	}
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\n")
	if dialect != "" {
		b.WriteString("jsonSchemaDialect: " + dialect + "\n")
	}
	b.WriteString("paths: {}\ncomponents:\n  schemas:\n")
	for _, schema := range schemas {
		b.WriteString("    " + schema + "\n")
	}
	return b.String()
}

// emptyMapPositionsOf returns the positions a row is written at, all of them
// where it names none.
func emptyMapPositionsOf(names []string) []emptyMapPosition {
	if names == nil {
		return emptyMapPositions
	}
	var out []emptyMapPosition
	for _, p := range emptyMapPositions {
		if slices.Contains(names, p.name) {
			out = append(out, p)
		}
	}
	return out
}

// keptUnderKey returns the Unmodeled entries under key held by the document's
// nodes and the properties of its models, by reason.
func keptUnderKey(doc *ir.Document, key string) []ir.UnmodeledReason {
	var out []ir.UnmodeledReason
	for _, td := range doc.Types {
		if e, ok := td.Common().Unmodeled[key]; ok {
			out = append(out, e.Reason)
		}
		if m, ok := td.(*ir.Model); ok {
			for _, p := range m.Properties {
				if e, ok := p.Unmodeled[key]; ok {
					out = append(out, e.Reason)
				}
			}
		}
	}
	return out
}

// findingKeys returns each finding of diags of any severity as its severity,
// code and pointer, sorted, so two documents' reports compare whole.
func findingKeys(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", d.Severity, d.Code, d.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}

// TestEmptyDependencyMap_IsTheKeywordLeftOut holds that a `dependentSchemas`,
// `dependentRequired`, or a draft's `dependencies` written with no entry
// compiles to what the schema writing none of them does, types and findings
// alike, at every position of the matrix, while the same keyword with an entry
// is kept verbatim and reported once at its schema. The keywords that hold a
// schema, `not`, `if`, `propertyNames` and `contains`, state something when
// empty and are kept, as are `definitions` and the `dependencies` of a dialect
// that defines none, which are unknown keywords whatever they hold.
func TestEmptyDependencyMap_IsTheKeywordLeftOut(t *testing.T) {
	t.Parallel()
	for _, row := range emptyMapRows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			for _, reversed := range []bool{false, true} {
				t.Run(fmt.Sprintf("reversed %v", reversed), func(t *testing.T) {
					t.Parallel()
					checkEmptyDependencyMap(t, row, reversed)
				})
			}
		})
	}
}

// checkEmptyDependencyMap compiles the row's positions with the keyword left
// out, written empty and written with an entry, and holds each to its reading.
func checkEmptyDependencyMap(t *testing.T, row emptyMapRow, reversed bool) {
	t.Helper()
	positions := emptyMapPositionsOf(row.positions)
	compile := func(written string) (*ir.Document, []ir.Diagnostic) {
		doc, diags := compileAnnotationSpec(t, "matrix",
			emptyMapSpec(row.dialect, positions, row.keyword, written, reversed))
		requireErrorsAndWarnings(t, diags, nil)
		return doc, diags
	}
	absent, absentDiags := compile("")
	empty, emptyDiags := compile(row.empty)
	full, fullDiags := compile(row.full)

	keptFull := make([]ir.UnmodeledReason, len(positions))
	reported := findingKeys(absentDiags)
	for i, p := range positions {
		keptFull[i] = row.reason
		at := fmt.Sprintf("/components/schemas/%c%d%s", p.schema[0], i, p.at)
		if row.atKeyword {
			at += "/" + row.keyword
		}
		reported = append(reported, fmt.Sprintf("info %s %s", row.code, at))
	}
	assert.Equal(t, keptFull, keptUnderKey(full, row.entry), "written with an entry it is kept at each position")
	assert.Equal(t, sortedCopy(reported), findingKeys(fullDiags), "and reported once, at the schema writing it")

	if row.leftOut {
		assert.Empty(t, keptUnderKey(empty, row.entry), "written with no entry it is kept nowhere")
		assert.Empty(t, cmp.Diff(absent.Types, empty.Types), "its types are those of the keyword left out")
		assert.Equal(t, findingKeys(absentDiags), findingKeys(emptyDiags), "and it reports nothing")
		return
	}
	assert.Equal(t, keptFull, keptUnderKey(empty, row.entry), "written with no entry it is kept all the same")
	assert.Equal(t, sortedCopy(reported), findingKeys(emptyDiags), "and reported as it is with one")
}

// sortedCopy returns keys sorted.
func sortedCopy(keys []string) []string {
	out := slices.Clone(keys)
	slices.Sort(out)
	return out
}

// TestEmptyDependencyMap_AnEntryWritingOnlyOneSaysNothing holds that an allOf
// entry writing only an empty dependency map says nothing for the position, as
// an entry writing none does: it is not kept whole, it is no conjunct another
// entry writing the keyword disagrees with, and it folds nothing in. The
// position lowers as it does without that entry.
func TestEmptyDependencyMap_AnEntryWritingOnlyOneSaysNothing(t *testing.T) {
	t.Parallel()
	for _, keyword := range []string{"dependentSchemas", "dependentRequired"} {
		full := map[string]string{"dependentSchemas": "{a: {required: [b]}}", "dependentRequired": "{a: [b]}"}[keyword]
		t.Run(keyword, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"T: {type: string}",
				"Alone: {allOf: [{$ref: '#/components/schemas/T'}, {" + keyword + ": {}}], description: d}",
				"Bare: {allOf: [{$ref: '#/components/schemas/T'}], description: d}",
				"Two: {allOf: [{$ref: '#/components/schemas/T'}, {" + keyword + ": {}}, {" + keyword + ": " + full + "}]}",
				"One: {allOf: [{$ref: '#/components/schemas/T'}, {" + keyword + ": " + full + "}]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "entry", admissionSpec("3.1.0", "", schemas...))
				requireErrorsAndWarnings(t, diags, nil)
				assert.Equal(t, []string{
					"info openapi/validation-only-keyword /components/schemas/One/allOf/1",
					"info openapi/validation-only-keyword /components/schemas/Two/allOf/2",
				}, findingKeys(diags), "only the entries with something to keep are reported")
				assert.Equal(t, keptOn(kindNode(t, doc, componentID("Bare"))), keptOn(kindNode(t, doc, componentID("Alone"))))
				assert.Equal(t, keptOn(kindNode(t, doc, componentID("One"))), keptOn(kindNode(t, doc, componentID("Two"))))
				assert.Len(t, keptOn(kindNode(t, doc, componentID("Two"))), 1)
			})
		})
	}
}
