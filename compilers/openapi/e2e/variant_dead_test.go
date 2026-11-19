// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// deadBranchComponents are the schemas the cases' branches name.
var deadBranchComponents = []string{
	"A: {type: object, properties: {a: {type: string}}}",
	"B: {type: object, properties: {b: {type: integer}}}",
	"Str: {type: string}",
	"Choice: {enum: [x, y]}",
}

// deadBranch is one branch a case writes, whether no instance of the object
// body beside it satisfies it, and whether it admits null alone, which lifts
// onto the reference to the position and is no variant either way.
type deadBranch struct {
	schema     string
	dead, null bool
}

// deadCase is a union written beside an object body, or on an allOf entry of
// one, and which of its branches lives.
type deadCase struct {
	name, body string
	versions   []string
	branches   []deadBranch
	// entry writes the union on the allOf entry the body lists second.
	entry bool
	// extra are components the case names beyond deadBranchComponents.
	extra []string
}

// deadCases are the shapes a union beside an object body takes: some branches
// dead, all of them, none, one alive, `false`, a string or null beside a body
// keeping null out and admitting it, a type list with or without an object,
// untyped branches and a $dynamicRef, which decide nothing, and a union on an
// allOf entry.
var deadCases = []deadCase{
	{name: "the golden shape", body: "type: object, description: d", versions: bothVersions, branches: []deadBranch{
		{schema: "{type: object}"}, {schema: "{type: array, items: {}}", dead: true}, {schema: "{type: string}", dead: true}}},
	{name: "all dead", body: "type: object, properties: {p: {type: string}}", versions: bothVersions, branches: []deadBranch{
		{schema: "{$ref: '#/components/schemas/Str'}", dead: true}, {schema: "{$ref: '#/components/schemas/Choice'}", dead: true}}},
	{name: "all live", body: "type: object, properties: {p: {type: string}}", versions: bothVersions, branches: []deadBranch{
		{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "{$ref: '#/components/schemas/B'}"}}},
	{name: "one live", body: "type: object, properties: {p: {type: string}}", versions: bothVersions, branches: []deadBranch{
		{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "{$ref: '#/components/schemas/Str'}", dead: true}}},
	{name: "a false branch", body: "type: object, properties: {p: {type: string}}", versions: []string{"3.1.0"},
		branches: []deadBranch{{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "false", dead: true}}},
	{name: "a null branch beside a body admitting null", body: "type: [object, 'null'], properties: {p: {type: string}}",
		versions: []string{"3.1.0"}, branches: []deadBranch{
			{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "{type: 'null'}", null: true}}},
	{name: "a null branch beside a nullable body", body: "type: object, nullable: true, properties: {p: {type: string}}",
		versions: []string{"3.0.3"}, branches: []deadBranch{
			{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "{enum: [null]}", null: true}}},
	{name: "a null branch beside a body keeping null out", body: "type: object, properties: {p: {type: string}}",
		versions: []string{"3.1.0"}, branches: []deadBranch{
			{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "{type: 'null'}", null: true}}},
	{name: "a type list", body: "type: object, properties: {p: {type: string}}", versions: []string{"3.1.0"},
		extra:    []string{"TL: {type: [object, string], properties: {t: {type: string}}}", "NoObj: {type: [string, integer]}"},
		branches: []deadBranch{{schema: "{$ref: '#/components/schemas/TL'}"}, {schema: "{$ref: '#/components/schemas/NoObj'}", dead: true}}},
	{name: "a string or null beside a body keeping null out", body: "type: object, properties: {p: {type: string}}",
		versions: []string{"3.1.0"}, branches: []deadBranch{
			{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "{type: [string, 'null']}", dead: true}}},
	{name: "a string or null beside a body admitting null", body: "type: [object, 'null'], properties: {p: {type: string}}",
		versions: []string{"3.1.0"}, branches: []deadBranch{
			{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "{type: [string, 'null']}"}}},
	{name: "untyped branches", body: "type: object, properties: {p: {type: string}}", versions: bothVersions,
		branches: []deadBranch{{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "{minLength: 1}"}, {schema: "{required: [z]}"},
			{schema: "{items: {type: string}}"}}},
	{name: "a dynamic reference", body: "type: object, properties: {p: {type: string}}", versions: []string{"3.1.0"},
		extra:    []string{"N1: {$dynamicAnchor: node}", "N2: {$dynamicAnchor: node}"},
		branches: []deadBranch{{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "{$dynamicRef: '#node'}"}}},
	{name: "on an allOf entry", body: "allOf: [{$ref: '#/components/schemas/A'}]", versions: bothVersions, entry: true,
		branches: []deadBranch{{schema: "{$ref: '#/components/schemas/B'}"}, {schema: "{type: string}", dead: true}}},
}

// deadSchema writes deadCaseValue's body beside, or on its second allOf entry,
// the union keyword listing branches.
func deadSchema(deadCaseValue deadCase, keyword string, branches []deadBranch) string {
	written := make([]string, 0, len(branches))
	for _, b := range branches {
		written = append(written, b.schema)
	}
	union := keyword + ": [" + strings.Join(written, ", ") + "]"
	if deadCaseValue.entry {
		return "P: {" + strings.TrimSuffix(deadCaseValue.body, "]") + ", {" + union + "}]}"
	}
	return "P: {" + deadCaseValue.body + ", " + union + "}"
}

// TestCompile_ABranchNoObjectSatisfiesIsNoVariant holds that a branch of a
// union beside an object body, beside it or on an allOf entry, that admits no
// object, nor a null the body admits, is no variant: the Union keeps it under
// its path and the schema draws one warning naming every such branch, at 3.0.3
// and 3.1.0, under oneOf and anyOf, in both orders of the branches and of the
// components, with every harness oracle passing. Where none lives, the
// position lowers as its body, keeping the union whole.
func TestCompile_ABranchNoObjectSatisfiesIsNoVariant(t *testing.T) {
	t.Parallel()
	for _, deadCase := range deadCases {
		for _, version := range deadCase.versions {
			for _, keyword := range []string{"oneOf", "anyOf"} {
				for _, reverse := range []bool{false, true} {
					branches := slices.Clone(deadCase.branches)
					if reverse {
						slices.Reverse(branches)
					}
					spec := admissionSpec(version, "", append(append([]string{deadSchema(deadCase, keyword, branches)},
						deadBranchComponents...), deadCase.extra...)...)
					t.Run(fmt.Sprintf("%s/%s/%s/reversed-%t", deadCase.name, version, keyword, reverse), func(t *testing.T) {
						t.Parallel()
						res := harness.Check(t.Context(), "spec.yaml", []byte(spec))
						require.Equal(t, harness.OutcomeOK, res.Outcome, res.Detail)
						inBothMappingOrders(t, spec, func(t *testing.T, spec string) {
							doc, diags := compileAnnotationSpec(t, "dead", spec)
							requireDeadBranches(t, doc, diags, deadCase.entry, keyword, branches)
						})
					})
				}
			}
		}
	}
}

// requireDeadBranches holds the component P, whose union keyword lists
// branches, to the shape a union with those branches dead takes.
func requireDeadBranches(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, entry bool, keyword string,
	branches []deadBranch,
) {
	t.Helper()
	path := keyword
	if entry {
		path = "allOf/1/" + keyword
	}
	var named []string
	live := 0
	for i, b := range branches {
		switch {
		case b.dead:
			named = append(named, fmt.Sprintf("%s/%d: ", path, i))
		case !b.null:
			live++
		}
	}
	var want []wantFinding
	if len(named) > 0 {
		want = []wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/P", contains: named}}
	}
	requireErrorsAndWarnings(t, diags, want)
	node := kindNode(t, doc, componentID("P"))
	if live == 0 {
		assert.NotEqual(t, ir.KindUnion, node.Kind(), "no branch lives, so the position is its body")
		assert.Contains(t, node.Common().Unmodeled, "openapi:"+keyword, "and keeps the union whole")
		return
	}
	union, ok := node.(*ir.Union)
	require.True(t, ok, "the position is the union of its live branches")
	require.Len(t, union.Variants, live)
	v := 0
	for i, b := range branches {
		at := fmt.Sprintf("/components/schemas/P/%s/%d", path, i)
		entry, kept := union.Unmodeled[fmt.Sprintf("openapi:%s/%d", keyword, i)]
		assert.Equal(t, b.dead, kept, "the union keeps %s exactly where it is dead", at)
		if kept {
			assert.Equal(t, at, string(entry.Provenance.Pointer), "the key names its keyword, provenance where it is")
		}
		if b.dead {
			requireNothingAt(t, doc, diags, at)
		}
		if b.dead || b.null {
			continue
		}
		assert.Equal(t, at, string(kindNode(t, doc, union.Variants[v].Type.Target).Common().Provenance.Pointer),
			"variant %d stands for %s", v, at)
		v++
	}
}

// requireNothingAt holds that no node and no finding stands at or under at.
func requireNothingAt(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, at string) {
	t.Helper()
	for id := range doc.Types {
		own := strings.TrimPrefix(string(id), "type/anonymous")
		assert.False(t, own == at || strings.HasPrefix(own, at+"/"), "%s lowers %s", id, at)
	}
	for _, diagnostic := range diags {
		p := string(diagnostic.Provenance.Pointer)
		assert.False(t, p == at || strings.HasPrefix(p, at+"/"), "%s reports at %s", diagnostic.Code, p)
	}
}

// TestCompile_AnUntypedBranchLivesWithKindInferenceOff holds that a branch
// naming no kind is no dead branch with the kind-inference policy off either:
// it admits every kind, so its variant keeps it.
func TestCompile_AnUntypedBranchLivesWithKindInferenceOff(t *testing.T) {
	t.Parallel()
	spec := admissionSpec("3.1.0", "", append([]string{
		"P: {type: object, properties: {p: {type: string}}, oneOf: [{$ref: '#/components/schemas/A'}, {minLength: 1}]}",
	}, deadBranchComponents...)...)
	doc, diags := compileKindInference(t, "off", spec, true)
	requireDeadBranches(t, doc, diags, false, "oneOf",
		[]deadBranch{{schema: "{$ref: '#/components/schemas/A'}"}, {schema: "{minLength: 1}"}})
}

// TestEntryClosure_ADeadBranchDeclaringMembersLeavesTheClosureKept holds that
// a union whose one member-declaring inline branch is dead still leaves an
// entry's additionalProperties kept on each variant rather than closing it:
// which object closes the model reads the branches as written, not which of
// them are variants (docs/compilers/openapi.md#additional-properties).
func TestEntryClosure_ADeadBranchDeclaringMembersLeavesTheClosureKept(t *testing.T) {
	t.Parallel()
	schema := "{allOf: [" + closureRef + ", {additionalProperties: false}], " +
		"oneOf: [{$ref: '#/components/schemas/A'}, {type: string, properties: {z: {type: string}}}]}"
	for _, version := range bothVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileAnnotationSpec(t, "closure", conjunctionSpec(version, schema, closureUser, deadBranchComponents[0]))
			requireErrorsAndWarnings(t, diags, deadWarnings("oneOf", []variantBranch{{name: "A", object: true}, {name: "Z"}}))
			for _, at := range conjunctionPositions {
				union := variantUnionAt(t, doc, string(at), 1)
				assert.Equal(t, ir.ReasonDegradedLowering, keptOn(union)["openapi:oneOf/1"], "the dead branch kept at %s", at)
				variant, ok := kindNode(t, doc, union.Variants[0].Type.Target).(*ir.Model)
				require.True(t, ok, at)
				assert.Equal(t, ir.AdditionalUnspecified, variant.Additional, "open at %s", at)
				assert.Equal(t, ir.ReasonDegradedLowering, keptOn(variant)["openapi:allOf/1"], "the closing entry kept at %s", at)
			}
		})
	}
}

// TestCompile_TwoUnionsOnEntriesKeepTheirBranchesApart holds that the
// branches a distributed union on an allOf entry keeps, on a variant, on the
// Union or as its null, are keyed by their keyword and index, the provenance
// naming the entry, and that no key collides with another entry's union, which
// is kept whole under its own keyword: a position distributes one union.
func TestCompile_TwoUnionsOnEntriesKeepTheirBranchesApart(t *testing.T) {
	t.Parallel()
	spec := admissionSpec("3.1.0", "", append([]string{"P: {allOf: [{$ref: '#/components/schemas/A'}, " +
		"{oneOf: [{$ref: '#/components/schemas/B'}, {anyOf: [{type: object}, {type: string}]}, {type: string}, " +
		"{type: 'null', description: n}]}, {anyOf: [{$ref: '#/components/schemas/B'}, {type: [object, string]}]}]}",
	}, deadBranchComponents...)...)
	res := harness.Check(t.Context(), "spec.yaml", []byte(spec))
	require.Equal(t, harness.OutcomeOK, res.Outcome, res.Detail)
	inBothMappingOrders(t, spec, func(t *testing.T, spec string) {
		doc, _ := compileAnnotationSpec(t, "entries", spec)
		union, ok := kindNode(t, doc, componentID("P")).(*ir.Union)
		require.True(t, ok, "the oneOf on the entry distributes")
		require.Len(t, union.Variants, 2)
		const entry = "/components/schemas/P/allOf/"
		want := map[ir.TypeID]map[string]string{
			union.ID:                      {"openapi:oneOf/2": entry + "1/oneOf/2", "openapi:oneOf/3": entry + "1/oneOf/3"},
			union.Variants[0].Type.Target: {"openapi:anyOf": entry + "2/anyOf"},
			union.Variants[1].Type.Target: {"openapi:anyOf": entry + "2/anyOf", "openapi:oneOf/1": entry + "1/oneOf/1"},
		}
		for id, keys := range want {
			got := map[string]string{}
			for key, kept := range kindNode(t, doc, id).Common().Unmodeled {
				got[key] = string(kept.Provenance.Pointer)
			}
			assert.Equal(t, keys, got, "what %s keeps, and where each is written", id)
		}
	})
}
