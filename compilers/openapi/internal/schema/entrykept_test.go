// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestEntryKept_ContentTwoEntriesWriteApartIsKeptWholeAsItsReason pins an entry
// kept whole for a keyword another entry writes otherwise: each is kept under
// its index with the reason its keywords share, content vocabulary's here, and
// reported at itself, in either order of the entries.
func TestEntryKept_ContentTwoEntriesWriteApartIsKeptWholeAsItsReason(t *testing.T) {
	t.Parallel()
	for _, entries := range []string{
		"{contentMediaType: text/plain}, {contentMediaType: text/html}",
		"{contentMediaType: text/html}, {contentMediaType: text/plain}",
	} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Str: {type: string}\n"+
			"    S: {allOf: [{$ref: '#/components/schemas/Str'}, "+entries+"]}\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		kept := typeByName(doc, "S").Common().Unmodeled
		assert.Equal(t, []string{"openapi:allOf/1", "openapi:allOf/2"}, unmodeledKeys(kept), entries)
		for _, key := range []string{"openapi:allOf/1", "openapi:allOf/2"} {
			assert.Equal(t, ir.ReasonNoIRHome, kept[key].Reason, entries)
		}
		for _, at := range []string{"/components/schemas/S/allOf/1", "/components/schemas/S/allOf/2"} {
			assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at),
				"another conjunct of this allOf writes a keyword this inline branch keeps otherwise", entries)
		}
	}
}

// TestEntryKept_WhatARefEntryWritesBesideItsRefIsReportedAsBesideIt pins how a
// report names what an allOf entry writes beside a $ref that resolves: as
// written beside that $ref, never as an inline branch, whether each keyword is
// kept under its own key or what it writes is kept whole (#2068). An inline
// entry writing the same keyword is still named an inline branch.
func TestEntryKept_WhatARefEntryWritesBesideItsRefIsReportedAsBesideIt(t *testing.T) {
	t.Parallel()
	const head = "    T: {type: object, properties: {a: {type: string}}}\n" +
		"    U: {type: object, properties: {u: {type: string}}}\n    Str: {type: string}\n"
	for _, c := range []struct{ entries, code, want string }{
		{"{$ref: '#/components/schemas/T', minLength: 2}, {type: object}", diag.DegradedConstruct,
			"has no home for minLength written beside this $ref; kept"},
		{"{$ref: '#/components/schemas/T', unevaluatedProperties: false}, {type: object}", diag.ValidationOnlyKeyword,
			"conjunct beside this allOf branch's $ref is merged in place, and nothing reads the rest of it; " +
				"the conjunct (unevaluatedProperties) is kept"},
		{"{$ref: '#/components/schemas/T', allOf: [{$ref: '#/components/schemas/U'}], unevaluatedProperties: false}",
			diag.ValidationOnlyKeyword, "conjunct beside this allOf branch's $ref is merged in place, its nested allOf composes"},
		{"{$ref: '#/components/schemas/T', minLength: 2}, {minLength: 3}", diag.DegradedConstruct,
			"writes a keyword the conjunct beside this $ref keeps otherwise, and one entry under that keyword holds one " +
				"value; the conjunct (minLength) is kept"},
		{"{$ref: '#/components/schemas/T', title: a}, {title: b}", diag.DegradedConstruct,
			"disagree on a keyword the conjunct beside this $ref writes, so nothing it writes folds; the conjunct (title) is kept"},
		{"{$ref: '#/components/schemas/Str', unevaluatedItems: false}", diag.ValidationOnlyKeyword,
			"which has no home for what the conjunct beside this $ref adds; the conjunct (unevaluatedItems) is kept"},
	} {
		_, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", head+"    S: {allOf: ["+c.entries+"]}\n"))
		message := openapitest.DiagMessageAt(t, diags, c.code, ir.SeverityInfo, "/components/schemas/S/allOf/0")
		assert.Contains(t, message, c.want, c.entries)
		assert.NotContains(t, message, "inline", c.entries)
	}

	_, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", head+
		"    S: {allOf: [{$ref: '#/components/schemas/T'}, {minLength: 2}]}\n"))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S/allOf/1"),
		"has no home for minLength written on this inline branch; kept")
}

// TestEntryKept_AnEntryKeptWholeHoldsWhatItFoldsOnce pins an entry kept whole
// for a closure it does not type while what else it writes folds: its vendor
// extension and its validation-only keyword are kept inside it alone, and its
// description is typed, where the allOf refines and where it composes.
func TestEntryKept_AnEntryKeptWholeHoldsWhatItFoldsOnce(t *testing.T) {
	t.Parallel()
	const entry = "{additionalProperties: false, x-a: 1, propertyNames: {pattern: '^a'}, description: d}"
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Str: {type: string}\n"+
		"    S: {allOf: [{$ref: '#/components/schemas/Str'}, "+entry+"]}\n"+
		"    M: {allOf: [{type: object, properties: {a: {type: string}}}, "+entry+"]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"S", "M"} {
		td := typeByName(doc, name)
		assert.Equal(t, []string{"openapi:allOf/1"}, unmodeledKeys(td.Common().Unmodeled), name)
		assert.Equal(t, "d", td.Common().Docs.Description, name)
	}
}

// TestEntryKept_AnEntryOfAnUndecidedAllOfIsReportedAgainstTheKeywordsBesideIt
// pins the report of what an entry keeps where no entry is elected: the
// position lowered by the keywords beside the allOf, the top type here, and
// says so, not that it is a use of a conjunct.
func TestEntryKept_AnEntryOfAnUndecidedAllOfIsReportedAgainstTheKeywordsBesideIt(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: [{type: string}, {enum: [a, 1]}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, at := range []string{"/components/schemas/S/allOf/0", "/components/schemas/S/allOf/1"} {
		message := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at)
		assert.Contains(t, message, "as no entry of it is the one it lowers from", at)
		assert.NotContains(t, message, "a use of one of its allOf conjuncts", at)
	}
}

// TestEntryKept_AFailedRefKeepsItsReferenceAndEachKeywordBesideIt pins a $ref
// entry naming nothing: its reference is kept as written under openapi:$ref and
// each keyword beside it under its own key, reported once at the entry beside
// the error the failure draws, and its required list still reads on the model.
func TestEntryKept_AFailedRefKeepsItsReferenceAndEachKeywordBesideIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Obj: {type: object, properties: {b: {type: string}}}\n"+
		"    S: {allOf: [{$ref: '#/components/schemas/Ghost', minLength: 2, required: [b], x-a: 1}, {$ref: '#/components/schemas/Obj'}]}\n"))
	td := typeByName(doc, "S")
	kept := td.Common().Unmodeled
	assert.Equal(t, []string{"openapi:$ref", "openapi:minLength", "openapi:x-a"}, unmodeledKeys(kept))
	assert.JSONEq(t, `"#/components/schemas/Ghost"`, string(kept["openapi:$ref"].Value))
	assert.Equal(t, ir.ReasonDegradedLowering, kept["openapi:$ref"].Reason)
	assert.Equal(t, ir.ReasonVendorExtension, kept["openapi:x-a"].Reason)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/allOf/0"), kept["openapi:$ref"].Provenance.Pointer)
	assert.Equal(t, []string{"b"}, td.(*ir.Model).Requires)
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, "/components/schemas/S/allOf/0"), "Ghost")
	message := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S/allOf/0")
	assert.Contains(t, message, "minLength written beside this $ref, which names nothing")
	assert.NotContains(t, message, "x-a")
}

// TestEntryKept_FailedRefsAreKeptByTheirReferenceInEitherOrder pins the keys of
// several $ref entries naming nothing, by reference text and then by the entry
// as written, two written alike being one entry at the position, and one with
// no keyword beside it drawing nothing but the error.
func TestEntryKept_FailedRefsAreKeptByTheirReferenceInEitherOrder(t *testing.T) {
	t.Parallel()
	for _, entries := range []string{
		"{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/A', minLength: 2}, {$ref: '#/components/schemas/A', minLength: 2}",
		"{$ref: '#/components/schemas/A', minLength: 2}, {$ref: '#/components/schemas/A', minLength: 2}, {$ref: '#/components/schemas/B'}",
	} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: ["+entries+"]}\n"))
		kept := typeByName(doc, "S").Common().Unmodeled
		assert.Equal(t, []string{"openapi:$ref/0", "openapi:$ref/1", "openapi:minLength"}, unmodeledKeys(kept), entries)
		assert.JSONEq(t, `"#/components/schemas/A"`, string(kept["openapi:$ref/0"].Value), entries)
		assert.JSONEq(t, `"#/components/schemas/B"`, string(kept["openapi:$ref/1"].Value), entries)
		assert.Equal(t, jsontext.Pointer("/components/schemas/S"), kept["openapi:$ref/0"].Provenance.Pointer, entries)
		assert.Equal(t, jsontext.Pointer("/components/schemas/S"), kept["openapi:minLength"].Provenance.Pointer, entries)
		var errs int
		for _, diagnostic := range diags {
			if diagnostic.Severity == ir.SeverityError {
				errs++
			}
		}
		assert.Equal(t, 3, errs, "each reference is reported where it is written: %s", entries)
	}
}

// TestEntryKept_AFailedRefIsKeptWholeWhereItsKeywordsCannotBeKeptApart pins a
// $ref entry naming nothing kept whole, its reference inside it, for a closure
// it does not type and for a keyword another entry or the position writes
// otherwise, and kept apart for a member another entry's merge reads.
func TestEntryKept_AFailedRefIsKeptWholeWhereItsKeywordsCannotBeKeptApart(t *testing.T) {
	t.Parallel()
	const ghost = "$ref: '#/components/schemas/Ghost'"
	for name, tc := range map[string]struct {
		schema string
		keys   []string
		said   string
	}{
		"closure": {"{allOf: [{" + ghost + ", additionalProperties: false}, {type: object}]}",
			[]string{"openapi:allOf/0"}, "names nothing, so nothing reads what it writes"},
		"another entry": {"{allOf: [{" + ghost + ", minLength: 2}, {minLength: 3}]}",
			[]string{"openapi:allOf/0", "openapi:allOf/1"}, "names nothing, so nothing reads what it writes"},
		"the position": {"{minLength: 3, allOf: [{" + ghost + ", minLength: 2}, {type: object}]}",
			[]string{"openapi:allOf/0", "openapi:minLength"}, "names nothing, so nothing reads what it writes"},
		"a merged member": {"{allOf: [{" + ghost + ", properties: {a: {type: string}}}, {properties: {b: {type: string}}}]}",
			[]string{"openapi:$ref", "openapi:properties"}, "properties written beside this $ref, which names nothing"},
	} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: "+tc.schema+"\n"))
		assert.Equal(t, tc.keys, unmodeledKeys(typeByName(doc, "S").Common().Unmodeled), name)
		assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S/allOf/0"),
			tc.said, name)
	}
}
