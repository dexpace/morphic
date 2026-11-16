// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// admissionSpec is a spec of the OpenAPI version whose components hold the
// schemas, each one line of YAML "Name: {...}", in the order given, beside the
// paths section.
func admissionSpec(version, paths string, schemas ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "openapi: %s\ninfo: {title: t, version: \"1\"}\n", version)
	if paths == "" {
		paths = "paths: {}\n"
	}
	b.WriteString(paths)
	b.WriteString("components:\n  schemas:\n")
	for _, s := range schemas {
		b.WriteString("    " + s + "\n")
	}
	return b.String()
}

// inBothOrders runs check over the schemas as declared and over the same
// schemas reversed: the second order is the same document with its components
// swapped, so a check that holds for both holds whichever is visited first.
func inBothOrders(t *testing.T, schemas []string, check func(t *testing.T, schemas []string)) {
	t.Helper()
	t.Run("declared", func(t *testing.T) { check(t, schemas) })
	reversed := slices.Clone(schemas)
	slices.Reverse(reversed)
	t.Run("reversed", func(t *testing.T) { check(t, reversed) })
}

// wantFinding is one error or warning a document must draw: the code, the
// pointer it is drawn at, and words its message holds.
type wantFinding struct {
	code, pointer string
	contains      []string
}

// requireErrorsAndWarnings holds the whole list of error and warning findings
// of diags to want, as a set keyed by pointer and code: nothing more is drawn,
// and none is drawn twice. An info is the caller's to ask for.
func requireErrorsAndWarnings(t *testing.T, diags []ir.Diagnostic, want []wantFinding) {
	t.Helper()
	var got []ir.Diagnostic
	for _, d := range diags {
		if d.Severity == ir.SeverityError || d.Severity == ir.SeverityWarning {
			got = append(got, d)
		}
	}
	key := func(code, pointer string) string { return pointer + " " + code }
	sort.Slice(got, func(i, j int) bool {
		return key(got[i].Code, string(got[i].Provenance.Pointer)) < key(got[j].Code, string(got[j].Provenance.Pointer))
	})
	sort.Slice(want, func(i, j int) bool { return key(want[i].code, want[i].pointer) < key(want[j].code, want[j].pointer) })
	gotKeys, wantKeys := make([]string, 0, len(got)), make([]string, 0, len(want))
	for _, d := range got {
		gotKeys = append(gotKeys, key(d.Code, string(d.Provenance.Pointer)))
	}
	for _, w := range want {
		wantKeys = append(wantKeys, key(w.code, w.pointer))
	}
	require.Equal(t, wantKeys, gotKeys, "the errors and warnings of the document")
	for i, w := range want {
		for _, word := range w.contains {
			assert.Contains(t, got[i].Message, word, "the message at %s", w.pointer)
		}
	}
}

// documentTree is doc as its JSON encodes it, read back as maps and slices, so
// a check can ask every node, property and parameter alike where a fact sits.
func documentTree(t *testing.T, doc *ir.Document) any {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	var tree any
	require.NoError(t, json.Unmarshal(raw, &tree))
	return tree
}

// comparableDocument is doc without what a swap of declarations moves on its
// own, the hash of the source and the order of the findings, so two orders of
// one document compare equal when they hold the same facts.
func comparableDocument(t *testing.T, doc *ir.Document) string {
	t.Helper()
	tree, ok := documentTree(t, doc).(map[string]any)
	require.True(t, ok)
	delete(tree, "sources")
	findings := []string{}
	if list, ok := tree["diagnostics"].([]any); ok {
		for _, d := range list {
			findings = append(findings, fmt.Sprint(d))
		}
	}
	sort.Strings(findings)
	delete(tree, "diagnostics")
	raw, err := json.Marshal(tree, json.Deterministic(true))
	require.NoError(t, err)
	return string(raw) + "\n" + strings.Join(findings, "\n")
}

// TestCompile_AnEmptyConjunctIsItsOwnSchemasToReport holds that an allOf whose
// conjunct admits nothing by itself draws no report of its own: the conjunct
// that contradicts itself is reported where it is written and the allOf
// naming it draws nothing more, in both orders of the components and of the
// conjuncts.
func TestCompile_AnEmptyConjunctIsItsOwnSchemasToReport(t *testing.T) {
	t.Parallel()
	for _, swapped := range []bool{false, true} {
		entries := "{$ref: '#/components/schemas/A'}, " + intRef
		if swapped {
			entries = intRef + ", {$ref: '#/components/schemas/A'}"
		}
		schemas := []string{
			"Str: {type: string}",
			"Obj: {type: object, properties: {k: {type: string}}}",
			"Int: {type: integer}",
			"B: {allOf: [" + entries + "]}",
			"A: {allOf: [" + strRef + ", " + objRef + "]}",
		}
		t.Run(fmt.Sprintf("entries-swapped-%t", swapped), func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				_, diags := compileAnnotationSpec(t, "empty", admissionSpec("3.1.0", "", schemas...))
				requireErrorsAndWarnings(t, diags, []wantFinding{{
					code: diag.DegradedConstruct, pointer: "/components/schemas/A", contains: []string{disjoint, "string", "object"},
				}})
			})
		})
	}
}

// TestCompile_AnAllOfCycleBesideABoundIsReadAlikeFromEveryEntry holds that two
// schemas each reaching the other through an allOf, one writing a bound, give
// the same nodes for both whichever is declared first and whichever a
// property enters the cycle through, and the same findings: a read met inside
// an open cycle is no verdict to remember for the schema it started from.
func TestCompile_AnAllOfCycleBesideABoundIsReadAlikeFromEveryEntry(t *testing.T) {
	t.Parallel()
	cycle := []string{
		"A: {allOf: [{$ref: '#/components/schemas/B'}], minLength: 1}",
		"B: {allOf: [{$ref: '#/components/schemas/A'}]}",
	}
	cycleNodes := make([]string, 0, 8)
	for _, entry := range []string{"A", "B"} {
		holder := "Holder: {type: object, properties: {p: {$ref: '#/components/schemas/" + entry + "'}}}"
		whole := make([]string, 0, 2)
		for _, order := range [][]string{cycle, {cycle[1], cycle[0]}} {
			schemas := append([]string{holder}, order...)
			doc, _ := compileAnnotationSpec(t, "cycle", admissionSpec("3.1.0", "", schemas...))
			whole = append(whole, comparableDocument(t, doc))
			for _, id := range []string{"A", "B"} {
				raw, err := json.Marshal(doc.Types[ir.TypeID("t/openapi/components/schemas/"+id)])
				require.NoError(t, err)
				cycleNodes = append(cycleNodes, id+" "+string(raw))
			}
		}
		assert.Equal(t, whole[0], whole[1], "the document does not follow the declaration order, entered at %s", entry)
	}
	for _, id := range []string{"A", "B"} {
		var of []string
		for _, n := range cycleNodes {
			if strings.HasPrefix(n, id+" ") {
				of = append(of, n)
			}
		}
		assert.Len(t, of, 4)
		assert.Len(t, slices.Compact(of), 1, "%s is one node whichever way the cycle is entered", id)
	}
}

const (
	strRef   = "{$ref: '#/components/schemas/Str'}"
	objRef   = "{$ref: '#/components/schemas/Obj'}"
	intRef   = "{$ref: '#/components/schemas/Int'}"
	disjoint = "share no value"
)
