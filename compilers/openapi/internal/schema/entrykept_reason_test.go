// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// keptReading is one kept keyword as a consumer reads it: its key, reason and
// value, and the reports at the keyword and at the entry writing it, by code.
type keptReading struct {
	Key, Value         string
	Reason             ir.UnmodeledReason
	AtKeyword, AtEntry []string
}

// readKept reads the one entry kept holds, reported in diags, written at
// written, where entry writes it.
func readKept(t *testing.T, kept ir.Unmodeled, diags []ir.Diagnostic, written, entry string) keptReading {
	t.Helper()
	require.Len(t, kept, 1, "%v", unmodeledKeys(kept))
	var out keptReading
	for key, e := range kept {
		out = keptReading{Key: key, Value: string(e.Value), Reason: e.Reason}
		require.Equal(t, jsontext.Pointer(written), e.Provenance.Pointer, key)
	}
	for _, d := range diags {
		switch d.Provenance.Pointer {
		case jsontext.Pointer(written):
			out.AtKeyword = append(out.AtKeyword, d.Code)
		case jsontext.Pointer(entry):
			out.AtEntry = append(out.AtEntry, d.Code)
		default:
		}
	}
	return out
}

// TestEntryKeptReason_ABoundOnANumberAsTextIsKeptAsBesideIt pins a value bound
// an inline entry writes where the position's own node carries a number as
// text, whose Constraints speak of the number: one key, the no_ir_home reason
// and the report at the keyword, as the bound written beside the type, at a
// component and at a property.
func TestEntryKeptReason_ABoundOnANumberAsTextIsKeptAsBesideIt(t *testing.T) {
	t.Parallel()
	for _, bound := range []string{"minimum: 1", "maxLength: 5"} {
		keyword, _, _ := strings.Cut(bound, ":")
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
			"    Beside: {type: string, format: int64, "+bound+"}\n"+
				"    Entry: {type: string, format: int64, allOf: [{"+bound+"}]}\n"+
				"    H: {type: object, properties: {p: {type: string, format: int64, allOf: [{"+bound+"}]}}}\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		at := "/components/schemas/"
		beside := readKept(t, typeByName(doc, "Beside").Common().Unmodeled, diags, at+"Beside/"+keyword, at+"Beside")
		entry := readKept(t, typeByName(doc, "Entry").Common().Unmodeled, diags, at+"Entry/allOf/0/"+keyword, at+"Entry/allOf/0")
		assert.Equal(t, ir.ReasonNoIRHome, beside.Reason, bound)
		assert.Equal(t, beside, entry, bound)
		p := doc.Types[ir.TypeID("t/anon/components/schemas/H/properties/p")]
		require.NotNil(t, p, bound)
		held := readKept(t, p.Common().Unmodeled, diags, at+"H/properties/p/allOf/0/"+keyword, at+"H/properties/p/allOf/0")
		assert.Equal(t, beside, held, bound)
	}
}

// TestEntryKeptReason_AnUnevaluatedKeywordKeepsItsEntryValidationOnly pins an
// entry kept whole for an unevaluated keyword: it takes the reason the same
// keyword has beside the allOf, validation_only, and its report the code that
// reason draws, over a string and over an object.
func TestEntryKeptReason_AnUnevaluatedKeywordKeepsItsEntryValidationOnly(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    Str: {type: string}\n    Obj: {type: object, properties: {a: {type: string}}}\n"+
			"    BesideStr: {allOf: [{$ref: '#/components/schemas/Str'}], unevaluatedProperties: false}\n"+
			"    EntryStr: {allOf: [{$ref: '#/components/schemas/Str'}, {unevaluatedProperties: false}]}\n"+
			"    EntryItems: {allOf: [{$ref: '#/components/schemas/Str'}, {unevaluatedItems: false}]}\n"+
			"    EntryObj: {allOf: [{$ref: '#/components/schemas/Obj'}, {unevaluatedProperties: {type: string}}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, ir.ReasonValidationOnly, typeByName(doc, "BesideStr").Common().Unmodeled["openapi:unevaluated"].Reason)
	for _, name := range []string{"EntryStr", "EntryItems", "EntryObj"} {
		kept := typeByName(doc, name).Common().Unmodeled
		assert.Equal(t, []string{"openapi:allOf/1"}, unmodeledKeys(kept), name)
		assert.Equal(t, ir.ReasonValidationOnly, kept["openapi:allOf/1"].Reason, name)
		at := "/components/schemas/" + name + "/allOf/1"
		assert.Len(t, diagsAtPointer(diags, diag.ValidationOnlyKeyword, at), 1, name)
		assert.Empty(t, diagsAtPointer(diags, diag.DegradedConstruct, at), name)
	}
}
