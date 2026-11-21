// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// useAt is the pointer of the use every spelling test writes, the schema C.
const useAt = "/components/schemas/C"

// spellingRule is the rule the spelling tests cite.
const spellingRule = "docs/compilers/openapi.md#ref-use-site"

// allOfToken is an allOf index in a pointer, or after an unmodeled name's
// prefix.
var allOfToken = regexp.MustCompile(`(^|/)allOf/[0-9]+`)

// eraseAllOf relocates as the OpenAPI cases do: every allOf index after the
// use's pointer, and after an unmodeled name's "openapi:" prefix, erased.
func eraseAllOf(s string) string {
	if rest, ok := strings.CutPrefix(s, "openapi:"); ok {
		return "openapi:" + strings.TrimPrefix(allOfToken.ReplaceAllString(rest, ""), "/")
	}
	before, after, ok := strings.Cut(s, useAt)
	if !ok {
		return s
	}
	return before + useAt + allOfToken.ReplaceAllString(after, "")
}

// spellingSpec is a document at version writing T, a string, Obj, an object,
// and C, the use, as the YAML flow mapping schema.
func spellingSpec(version, schema string) []byte {
	return fmt.Appendf(nil, `openapi: %s
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    T: {type: string}
    Obj: {type: object, properties: {a: {type: string}}}
    C: %s
`, version, schema)
}

// spellingPair is a product of two cases: the partner, writing partner, and
// the spelling, writing spelling and equal to the partner under eq, whose To
// and Relocate it fills.
func spellingPair(version, partner, spelling string, eq Equal) fake {
	eq.To = []int{0}
	if eq.Relocate == nil {
		eq.Relocate = eraseAllOf
	}
	if eq.Rule == "" {
		eq.Rule = spellingRule
	}
	return fake{axes: []Axis{coreAxis("spelling", "partner", "spelling")}, write: func(at []int) (CaseDocument, bool) {
		if at[0] == 0 {
			return CaseDocument{Sources: []compilers.Source{{Path: "spec.yaml", Data: spellingSpec(version, partner)}}}, true
		}
		return CaseDocument{Sources: []compilers.Source{{Path: "spec.yaml", Data: spellingSpec(version, spelling)}},
			Equal: &eq}, true
	}}
}

// spellingVerdict is the spelling oracle's verdict on the second case of
// fakeValue.
func spellingVerdict(t *testing.T, fakeValue fake) Verdict {
	t.Helper()
	withAsked(t, spelled)
	result, ok := CheckCase(t.Context(), fakeValue, []int{1})
	require.True(t, ok)
	require.NotEqual(t, OutcomePanic, result.Outcome, result.Detail)
	return result.Verdicts[0]
}

func TestSpelled_ScalarAndModelDecoratorsAreOneUse(t *testing.T) {
	verdict := spellingVerdict(t, spellingPair("3.1.0",
		`{type: array, items: {allOf: [{$ref: '#/components/schemas/Obj'}], description: d}}`,
		`{type: array, items: {$ref: '#/components/schemas/Obj', description: d}}`, Equal{}))
	assert.Equal(t, passed(OracleSpelling), verdict, verdict.Detail)
}

func TestSpelled_ACarrierHoldsWhatItsDecoratorHeld(t *testing.T) {
	verdict := spellingVerdict(t, spellingPair("3.1.0",
		`{type: object, properties: {p: {allOf: [{$ref: '#/components/schemas/Obj'}], description: d, examples: [{a: x}]}}}`,
		`{type: object, properties: {p: {$ref: '#/components/schemas/Obj', description: d, examples: [{a: x}]}}}`,
		Equal{}))
	assert.Equal(t, passed(OracleSpelling), verdict, verdict.Detail)
}

// TestSpelled_ADifferenceIsFoundWithWhereItLies holds each finding's first
// line to where its two documents differ.
func TestSpelled_ADifferenceIsFoundWithWhereItLies(t *testing.T) {
	for _, tc := range []struct {
		name, partner, spelling, what string
	}{
		{"a use's description", `{type: array, items: {allOf: [{$ref: '#/components/schemas/Obj'}], description: d}}`,
			`{type: array, items: {$ref: '#/components/schemas/Obj', description: e}}`, "types.elementType.use.docs.description"},
		{"a bound", `{allOf: [{$ref: '#/components/schemas/T'}], pattern: '^a4'}`,
			`{allOf: [{$ref: '#/components/schemas/T'}, {pattern: '^a3'}]}`, "types.constraints.pattern"},
		{"a decorator's description and kept keyword",
			`{type: object, properties: {p: {allOf: [{$ref: '#/components/schemas/Obj'}], description: d, not: {required: [a]}}}}`,
			`{type: object, properties: {p: {$ref: '#/components/schemas/Obj', description: e, not: {required: [b]}}}}`,
			"types.properties.decorator.unmodeled.openapi:not.value.required,types.properties.docs.description"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict := spellingVerdict(t, spellingPair("3.1.0", tc.partner, tc.spelling, Equal{}))
			require.True(t, verdict.Found(), "%+v", verdict)
			what, rest, _ := strings.Cut(verdict.Detail, "\n")
			assert.Equal(t, tc.what, what)
			assert.True(t, strings.HasPrefix(rest, spellingRule+"\n"), rest)
			assert.Contains(t, rest, "--- spec.yaml\n", "both sources follow")
		})
	}
}

// TestSpelled_AKeptKeywordsNameRelocates compares two orders of one entry
// keeping a keyword: the record's name holds the entry's index, which
// relocates like any pointer.
func TestSpelled_AKeptKeywordsNameRelocates(t *testing.T) {
	verdict := spellingVerdict(t, spellingPair("3.1.0",
		`{allOf: [{$ref: '#/components/schemas/T'}, {contentMediaType: text/html}]}`,
		`{allOf: [{contentMediaType: text/html}, {$ref: '#/components/schemas/T'}]}`, Equal{}))
	assert.Equal(t, passed(OracleSpelling), verdict, verdict.Detail)
}

func TestSpelled_TwoNamesRelocatedToOneDecline(t *testing.T) {
	inline := `{properties: {q: {type: object, properties: {x: {type: string}}}}}`
	verdict := spellingVerdict(t, spellingPair("3.1.0", `{allOf: [{$ref: '#/components/schemas/Obj'}]}`,
		`{allOf: [{$ref: '#/components/schemas/Obj'}, `+inline+`, `+inline+`]}`, Equal{}))
	assert.Equal(t, declined(OracleSpelling, DeclineRelocationCollides), verdict)
}

// TestSpelled_AStatedReportIsRequired holds a 3.0 keyword beside a $ref to the
// report the spelling states, and to the one its partner draws in its place.
func TestSpelled_AStatedReportIsRequired(t *testing.T) {
	stated := func(at string) []Stated {
		return []Stated{{Severity: ir.SeverityWarning, Code: "openapi/later-version-construct", Pointer: jsontext.Pointer(at)}}
	}
	minLength := spellingPair("3.0.3", `{allOf: [{$ref: '#/components/schemas/T'}], minLength: 1}`,
		`{$ref: '#/components/schemas/T', minLength: 1}`, Equal{Stated: stated(useAt + "/minLength")})
	verdict := spellingVerdict(t, minLength)
	assert.Equal(t, passed(OracleSpelling), verdict, verdict.Detail)

	nullable := Equal{Stated: stated(useAt + "/nullable"), Replaced: []Stated{{Severity: ir.SeverityWarning,
		Code: "openapi/validation/validation-no-effect", Pointer: useAt + "/nullable"}}}
	verdict = spellingVerdict(t, spellingPair("3.0.3", `{allOf: [{$ref: '#/components/schemas/T'}], nullable: true}`,
		`{$ref: '#/components/schemas/T', nullable: true}`, nullable))
	assert.Equal(t, passed(OracleSpelling), verdict, verdict.Detail)

	for _, amiss := range []Equal{
		{Stated: stated(useAt + "/maxLength")},
		{Stated: []Stated{{Severity: ir.SeverityInfo, Code: "openapi/later-version-construct", Pointer: useAt + "/minLength"}}},
	} {
		verdict = spellingVerdict(t, spellingPair("3.0.3", `{allOf: [{$ref: '#/components/schemas/T'}], minLength: 1}`,
			`{$ref: '#/components/schemas/T', minLength: 1}`, amiss))
		assert.Equal(t, "diagnostics:openapi/later-version-construct,stated:openapi/later-version-construct",
			strings.SplitN(verdict.Detail, "\n", 2)[0], "a report stated at another pointer or severity is no report drawn")
	}

	var unswapped compileFunc
	unswapped = swapCompile(t, func(ctx context.Context, spec string, data []byte, ps []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		doc, diags, err := unswapped(ctx, spec, data, ps)
		return doc, slicesDeleteCode(diags, "openapi/later-version-construct"), err
	})
	verdict = spellingVerdict(t, minLength)
	require.True(t, verdict.Found(), "%+v", verdict)
	assert.Equal(t, "stated:openapi/later-version-construct", strings.SplitN(verdict.Detail, "\n", 2)[0])
}

// TestRelocatedSet_RelocatesEachPointerAndNode pins what a spelling's
// reports compare by: each pointer and each node read in the partner's
// coordinates.
func TestRelocatedSet_RelocatesEachPointerAndNode(t *testing.T) {
	reportAt := func(at string) ir.Diagnostic {
		return ir.Diagnostic{Severity: ir.SeverityWarning, Code: "x/y",
			Provenance: ir.Provenance{Pointer: jsontext.Pointer(at), Node: "type/openapi" + at}}
	}

	got := relocatedSet([]ir.Diagnostic{reportAt(useAt + "/allOf/1")}, eraseAllOf)

	assert.Equal(t, diagnosticSet([]ir.Diagnostic{reportAt(useAt)}), got)
}

// slicesDeleteCode returns diags less every report of code.
func slicesDeleteCode(diags []ir.Diagnostic, code string) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, diagnostic := range diags {
		if diagnostic.Code != code {
			out = append(out, diagnostic)
		}
	}
	return out
}

// TestSpelled_ARefusalIsFoundSayingWhichSide refuses one side, then the
// other: the side that alone compiles to no document is the finding.
func TestSpelled_ARefusalIsFoundSayingWhichSide(t *testing.T) {
	partner, spelling := `{allOf: [{$ref: '#/components/schemas/T'}]}`, `{$ref: '#/components/schemas/T'}`
	fake := spellingPair("3.1.0", partner, spelling, Equal{})
	uncompiled := swapUncompiled(t)

	uncompiled[string(spellingSpec("3.1.0", partner))] = "refused"
	assert.Contains(t, spellingVerdict(t, fake).Detail, "refused: the partner compiles to no document")

	delete(uncompiled, string(spellingSpec("3.1.0", partner)))
	uncompiled[string(spellingSpec("3.1.0", spelling))] = "refused"
	assert.Contains(t, spellingVerdict(t, fake).Detail, "refused: this spelling compiles to no document")
}

// TestSpelled_TwoSidesThatDoNotCompileDecline holds a pair neither side of
// which compiled to a decline a record lists, however each failed: two
// refusals would compare alike whatever the case says. One side drawing an
// error over a document is a difference in the reports, found.
func TestSpelled_TwoSidesThatDoNotCompileDecline(t *testing.T) {
	partner, spelling := `{allOf: [{$ref: '#/components/schemas/T'}]}`, `{$ref: '#/components/schemas/T'}`
	fake := spellingPair("3.1.0", partner, spelling, Equal{})
	uncompiled := swapUncompiled(t)
	for _, how := range []string{"refused", "error", "no document"} {
		uncompiled[string(spellingSpec("3.1.0", partner))] = how
		uncompiled[string(spellingSpec("3.1.0", spelling))] = how

		verdict := spellingVerdict(t, fake)

		assert.Equal(t, declined(OracleSpelling, DeclineErrorDiagnostic), verdict, how)
		assert.True(t, listed(verdict), how)
	}

	delete(uncompiled, string(spellingSpec("3.1.0", partner)))
	uncompiled[string(spellingSpec("3.1.0", spelling))] = "error"
	verdictValue := spellingVerdict(t, fake)
	require.True(t, verdictValue.Found(), "%+v", verdictValue)
	assert.Equal(t, "diagnostics:x/refused", strings.SplitN(verdictValue.Detail, "\n", 2)[0])
}

// swapUncompiled makes the compile of each source the returned map names fail
// as the map says: "refused" to no document and an error diagnostic, as the
// engine refuses, "error" to its document and an error diagnostic, and "no
// document" to neither.
func swapUncompiled(t *testing.T) map[string]string {
	t.Helper()
	how := map[string]string{}
	boom := ir.Diagnostic{Severity: ir.SeverityError, Code: "x/refused", Message: "refused"}
	var unswapped compileFunc
	unswapped = swapCompile(t, func(ctx context.Context, spec string, data []byte, ps []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		doc, diags, err := unswapped(ctx, spec, data, ps)
		switch how[string(data)] {
		case "refused":
			return nil, append(diags, boom), err
		case "error":
			return doc, append(diags, boom), err
		case "no document":
			return nil, diags, err
		default:
			return doc, diags, err
		}
	})
	return how
}

// TestSpelled_WhatStopsAComparison pins what stops a comparison: no partner
// declines, and a compile's error, a document that does not encode and a
// partner writing nothing are faults.
func TestSpelled_WhatStopsAComparison(t *testing.T) {
	fakeValue := spellingPair("3.1.0", `{$ref: '#/components/schemas/T'}`, `{$ref: '#/components/schemas/T'}`, Equal{})
	unequal := fake{axes: fakeValue.axes, write: func(at []int) (CaseDocument, bool) {
		doc, _ := fakeValue.write(at)
		doc.Equal = nil
		return doc, true
	}}
	assert.Equal(t, declined(OracleSpelling, DeclineNoPartner), spellingVerdict(t, unequal))

	broken := errors.New("engine broke")
	swapCompile(t, func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, broken
	})
	withAsked(t, spelled)
	result, ok := CheckCase(t.Context(), fakeValue, []int{1})
	require.True(t, ok)
	assert.True(t, result.Faulted(), "a compile's error is a fault")
	assert.Contains(t, result.Detail, "harness: compile case [1] or its partner: engine broke")

	swapCompile(t, func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return &ir.Document{Name: "\xff"}, nil, nil
	})
	result, ok = CheckCase(t.Context(), fakeValue, []int{1})
	require.True(t, ok)
	assert.True(t, result.Faulted(), "a document that does not encode is a fault")
	assert.Contains(t, result.Detail, "harness: encode case [1] or its partner: ")

	alone := fake{axes: fakeValue.axes, write: func(at []int) (CaseDocument, bool) {
		doc, _ := fakeValue.write(at)
		return doc, at[0] == 1
	}}
	result, ok = CheckCase(t.Context(), alone, []int{1})
	require.True(t, ok)
	assert.True(t, result.Faulted(), "a partner the product writes nothing for is a fault")
	assert.Contains(t, result.Detail, "harness: case [1] names the partner [0], which writes nothing")
}

// TestSpelled_ACaseNamingAPartnerAmissPanics pins the asserts on what a
// product writes: an Equal with no relocation or no rule, and an oracle asked
// of a case that writes nothing.
func TestSpelled_ACaseNamingAPartnerAmissPanics(t *testing.T) {
	withAsked(t, spelled)
	for _, eq := range []Equal{{Rule: spellingRule}, {Relocate: eraseAllOf}} {
		fake := spellingPair("3.1.0", `{$ref: '#/components/schemas/T'}`, `{$ref: '#/components/schemas/T'}`, Equal{})
		write := fake.write
		fake.write = func(at []int) (CaseDocument, bool) {
			doc, ok := write(at)
			if doc.Equal != nil {
				eq.To = doc.Equal.To
				doc.Equal = &eq
			}
			return doc, ok
		}
		result, ok := CheckCase(t.Context(), fake, []int{1})
		require.True(t, ok)
		assert.Equal(t, OutcomePanic, result.Outcome)
		assert.Contains(t, result.Detail, "names a partner with no relocation or no rule")
	}

	fakeValue := spellingPair("3.1.0", `{$ref: '#/components/schemas/T'}`, `{$ref: '#/components/schemas/T'}`, Equal{})
	doc, _ := fakeValue.write([]int{1})
	shard := newShard(fake{axes: fakeValue.axes, write: func([]int) (CaseDocument, bool) { return CaseDocument{}, false }}, fakeValue.axes, []int{1})
	assert.PanicsWithValue(t, "harness: case [1] writes nothing to compile", func() { spelled(t.Context(), shard, []int{1}, doc) })
}

func TestReadTree_RefusesWhatItCannotRelocate(t *testing.T) {
	_, err := relocatedTree([]byte(`{"`+useAt+`/allOf/1": 1, "`+useAt+`": 2}`), eraseAllOf)
	require.ErrorIs(t, err, errCollides)
	assert.Contains(t, err.Error(), `"`+useAt+`"`)

	deep := strings.Repeat("[", maxSpellingDepth+2) + strings.Repeat("]", maxSpellingDepth+2)
	_, err = relocatedTree([]byte(deep), eraseAllOf)
	require.ErrorIs(t, err, errTreeTooDeep)
	_, err = relocatedTree([]byte(deep[1:len(deep)-1]), eraseAllOf)
	require.NoError(t, err, "a document at the bound is read")

	for _, broken := range []string{`{"a": [1, `, `{"a": {"b": `, `{"a"`, `{"a": 1, `, ``} {
		_, err := relocatedTree([]byte(broken), eraseAllOf)
		require.Error(t, err, broken)
	}

	tree, err := relocatedTree([]byte(`{"x":"`+useAt+`/allOf/0/y","n":1.50,"t":true,"f":false,"z":null}`), eraseAllOf)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"x": useAt + "/y", "n": number("1.50"), "t": true, "f": false, "z": nil}, tree)
}

func TestDiffersIn_NamesEachPlaceOnce(t *testing.T) {
	a := map[string]any{"types": map[string]any{
		"type/x": map[string]any{"id": "type/x", "kind": "model", "list": []any{"a", "b"}},
		"type/y": map[string]any{"id": "type/y"},
	}}
	b := map[string]any{"types": map[string]any{
		"type/x": map[string]any{"id": "type/x", "kind": "scalar", "list": []any{"a"}},
	}}
	diags := diagnosed{mine: []string{"warning\x00a/b\x00x"}, theirs: []string{"warning\x00a/b\x00y", "info\x00c\x00z"}}

	assert.Equal(t, "diagnostics:a/b,diagnostics:c,stated:x,types.id,types.kind,types.list", differsIn(a, b, diags, []string{"stated:x"}))
	lacks := map[string]any{"u": map[string]any{"k": map[string]any{"v": 1}, "l": []any{"x", map[string]any{"w": 2}}}}
	assert.Equal(t, "u.k.v,u.l,u.l.w", differsIn(lacks, map[string]any{}, diagnosed{}, nil), "a member one side lacks is named by what it holds")
	assert.Equal(t, "document", differsIn("a", nil, diagnosed{}, nil))
	assert.Empty(t, differsIn(a, a, diagnosed{mine: diags.mine, theirs: diags.mine}, nil))

	var deep any = "x"
	for range maxSpellingDepth + 1 {
		deep = []any{deep}
	}
	assert.Equal(t, "…", differsIn(deep, deep, diagnosed{}, nil), "the walk stops at its bound")
}

func TestComparableSide_ADocumentNestedPastTheBoundIsDeclined(t *testing.T) {
	deep := strings.Repeat("[", maxSpellingDepth) + strings.Repeat("]", maxSpellingDepth)
	doc := &ir.Document{Types: ir.TypeRegistry{"type/x": &ir.Scalar{ID: "type/x",
		Unmodeled: ir.Unmodeled{"x": {Value: ir.RawValue(deep)}}}}}
	_, why, err := comparableSide(doc, eraseAllOf)
	require.NoError(t, err)
	assert.Equal(t, DeclineTooDeep, why)
}

func TestIndented_SaysWhyATreeDoesNotEncode(t *testing.T) {
	assert.Equal(t, []string{"{", `  "n": 1.50`, "}"}, indented(map[string]any{"n": number("1.50")}))
	got := indented(map[string]any{"s": "\xff"})
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "does not encode: ")
}
