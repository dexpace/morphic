// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
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

// TestEntryKept_AnEntryKeptWholeHoldsWhatItFoldsOnce pins an entry kept whole
// for a closure it does not type while what else it writes folds: its vendor
// extension and its validation-only keyword are kept inside it alone, and its
// description is typed, at a component and at a property.
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
		msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at)
		assert.Contains(t, msg, "as no entry of it is the one it lowers from", at)
		assert.NotContains(t, msg, "a use of one of its allOf conjuncts", at)
	}
}
