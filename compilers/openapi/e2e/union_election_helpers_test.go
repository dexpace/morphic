// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// electionVersions are the OpenAPI versions every election case is written at.
var electionVersions = []string{"3.0.3", "3.1.0"}

// swapEntryIndices names, in a document's JSON, entry 0 of an allOf as entry 1
// and the other way round, so the document compiled from an allOf's entries in
// one order reads in the coordinates of the other.
var swapEntryIndices = strings.NewReplacer("allOf/0", "allOf/1", "allOf/1", "allOf/0")

// positionKey is the members of a node that say where its source lies in the
// file, which two spellings of one document never share.
var positionKey = map[string]bool{"position": true, "sources": true}

// withoutPositions returns tree without any member naming a position or the
// sources table, whatever it nests in.
func withoutPositions(tree any) any {
	switch x := tree.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, value := range x {
			if !positionKey[key] {
				out[key] = withoutPositions(value)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = withoutPositions(value)
		}
		return out
	default:
		return tree
	}
}

// electionView is doc as the comparison of two entry orders reads it: its
// JSON, renamed by rename into the first order's coordinates, with
// no sources table and no position, and its findings as a sorted multiset of
// severity, code and pointer, which leaves out the words of a message. The
// findings sit beside the document under "findings".
func electionView(t *testing.T, doc *ir.Document, rename func(string) string) any {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	text := rename(string(raw))
	var tree map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &tree))
	findings := []string{}
	if list, ok := tree["diagnostics"].([]any); ok {
		for _, d := range list {
			findings = append(findings, fmt.Sprintf("%v %v %v",
				valueAtPath(d, "severity"), valueAtPath(d, "code"), valueAtPath(d, "provenance", "pointer")))
		}
	}
	slices.Sort(findings)
	delete(tree, "diagnostics")
	view, ok := withoutPositions(tree).(map[string]any)
	require.True(t, ok)
	view["findings"] = findings
	return view
}

// compileInEveryOrder compiles the schemas build gives, once with the entries
// of each allOf as written and once swapped, each with the components as
// declared and reversed, and holds the four documents equal in the first
// order's coordinates. It returns the compile of the entries as written and
// the components as declared. The entries are two, since the swap names them
// by index.
func compileInEveryOrder(
	t *testing.T, version string, build func(swapped bool) []string,
) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	return compileInEveryOrderRenamed(t, version, build, func(text string, swapped bool) string {
		if swapped {
			return swapEntryIndices.Replace(text)
		}
		return text
	})
}

// entryNodes names no allOf entry in a document's JSON: the node that stands
// at an entry, whichever entry it is, and the key an entry is kept under, are
// read without the index.
var entryNodes = regexp.MustCompile(`/?allOf/\d+`)

// withoutEntryIndices is compileInEveryOrder for a tie whose pick shows only
// in the entry a node stands at: the two orders compare equal once no entry
// has an index, which is all a tie among alike entries may change.
func withoutEntryIndices(
	t *testing.T, version string, build func(swapped bool) []string,
) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	return compileInEveryOrderRenamed(t, version, build, func(text string, _ bool) string {
		return entryNodes.ReplaceAllString(text, "")
	})
}

// compileInEveryOrderRenamed is compileInEveryOrder with the coordinates the
// two orders are compared in given by rename.
func compileInEveryOrderRenamed(
	t *testing.T, version string, build func(swapped bool) []string, rename func(text string, swapped bool) string,
) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	var (
		first  any
		doc    *ir.Document
		diags  []ir.Diagnostic
		anchor string
	)
	for _, swapped := range []bool{false, true} {
		for _, reversed := range []bool{false, true} {
			schemas := slices.Clone(build(swapped))
			if reversed {
				slices.Reverse(schemas)
			}
			name := fmt.Sprintf("swapped-%t-reversed-%t", swapped, reversed)
			got, found := compileAnnotationSpec(t, "election", admissionSpec(version, "", schemas...))
			view := electionView(t, got, func(text string) string { return rename(text, swapped) })
			if first == nil {
				first, doc, diags, anchor = view, got, found, name
				continue
			}
			if diff := cmp.Diff(first, view); diff != "" {
				t.Errorf("%s differs from %s (-%s +%s):\n%s", name, anchor, anchor, name, diff)
			}
		}
	}
	require.NotNil(t, doc)
	return doc, diags
}

// pairSchemas builds the schemas of a case that writes one allOf of two entries
// under the name given: the entries in the order a and b when swapped is
// false, in the order b and a otherwise. The others are written first, and beside is written
// beside the allOf.
func pairSchemas(
	swapped bool, name, a, b string, beside string, others ...string,
) []string {
	list, _, _ := composeEntries(swapped, a, b)
	if beside != "" {
		beside = ", " + beside
	}
	return append(slices.Clone(others), name+": {allOf: "+list+beside+"}")
}

// requireKeptEntries holds the Unmodeled entries of td to exactly the keys
// given, each kept for want of a home, which is how an entry is kept whole.
func requireKeptEntries(t *testing.T, td ir.TypeDef, what string, indices ...int) {
	t.Helper()
	keys := make([]string, 0, len(indices))
	for _, i := range indices {
		keys = append(keys, entryKey(i))
	}
	assert.Equal(t, degradedKept(keys...), keptOn(td), what)
}

// requireOneInfoAtEach holds the document to one report of a construct kept for
// want of a home at the entry of the component named at each index.
func requireOneInfoAtEach(t *testing.T, diags []ir.Diagnostic, name, what string, indices ...int) {
	t.Helper()
	for _, i := range indices {
		pointer := fmt.Sprintf("/components/schemas/%s/allOf/%d", name, i)
		assert.Equal(t, 1, degradedInfosUnder(diags, pointer), "%s: one report at %s", what, pointer)
	}
	assert.Equal(t, len(indices), degradedInfosAt(diags, name), "%s: the reports of %s", what, name)
}

// The entries the election cases write, each a union naming its kinds.
const (
	oneStrInt      = "{oneOf: [{type: string}, {type: integer}]}"
	oneIntBool     = "{oneOf: [{type: integer}, {type: boolean}]}"
	anyNumBool     = "{anyOf: [{type: number}, {type: boolean}]}"
	anyStrInt      = "{anyOf: [{type: string}, {type: integer}]}"
	objBody        = "type: object, properties: {x: {type: string}}"
	oneObjBranches = "oneOf: [{type: object, properties: {a: {type: string}}}, {type: object, properties: {b: {type: string}}}]"
	anyObjBranches = "anyOf: [{type: object, properties: {c: {type: string}}}, {type: object, properties: {d: {type: string}}}]"
)

// unionsUnder lists the IDs of the unions a document holds for the component
// named, at the component itself or under it.
func unionsUnder(doc *ir.Document, name string) []ir.TypeID {
	var ids []ir.TypeID
	for id, td := range doc.Types {
		if td.Kind() != ir.KindUnion {
			continue
		}
		if s := string(id); strings.Contains(s, "/schemas/"+name+"/") || strings.HasSuffix(s, "/schemas/"+name) {
			ids = append(ids, id)
		}
	}
	return ids
}
