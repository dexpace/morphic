// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// lentDetails is each detail a $ref's referent lends a property, as the
// referent Lender writes it and as an allOf entry restating the property
// writes another, with the value the field holds.
var lentDetails = []struct {
	detail, lender, entry string
	held                  func(ir.Property) any
	lent, written         any
}{
	{"description", "description: Lent.", "description: Written.",
		func(p ir.Property) any { return p.Docs.Description }, "Lent.", "Written."},
	{"title", "title: Lent", "title: Written",
		func(p ir.Property) any { return p.Docs.Summary }, "Lent", "Written"},
	{"external docs", "externalDocs: {url: 'https://lent.example'}", "externalDocs: {url: 'https://written.example'}",
		func(p ir.Property) any { return p.Docs.ExternalDocs[0].URL }, "https://lent.example", "https://written.example"},
	{"deprecation", "deprecated: true, x-deprecated-reason: lent", "deprecated: true",
		func(p ir.Property) any { return *p.Deprecation }, ir.Deprecation{Message: "lent"}, ir.Deprecation{}},
	{"default", "default: lent", "default: written",
		func(p ir.Property) any { return p.Default.String }, "lent", "written"},
}

// lentSpec declares Lender, a string writing lender, Base, whose field f is a
// bare $ref to it, and Composed, an allOf of entries in order.
func lentSpec(lender string, entries ...string) string {
	return openapitest.ComponentSpec(fmt.Sprintf(`    Lender: {type: string, %s}
    Base: {type: object, properties: {f: {$ref: '#/components/schemas/Lender'}}}
    Composed: {allOf: [%s]}
`, lender, strings.Join(entries, ", ")))
}

// conflictNotes returns the reports among diags that two declarations give a
// detail differently (lastWritten).
func conflictNotes(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, diagnostic := range withCode(diags, diag.DegradedConstruct) {
		if strings.Contains(diagnostic.Message, "differently; kept the one") {
			out = append(out, diagnostic)
		}
	}
	return out
}

// TestAllOf_ALentDetailIsNoPartyToAConflict pins GitHub #2152: where an allOf
// entry restates a member to give it a detail its declaration's $ref target
// lends, nothing that declaration wrote is set aside, so nothing is reported
// or kept beside the field, for a base brought by its entry and for a
// declaration the model writes. The field keeps the entry's value. Written
// the other way round, the lent value is last and the field keeps it, as
// declaration order decides a tie, reporting the entry's it set aside.
func TestAllOf_ALentDetailIsNoPartyToAConflict(t *testing.T) {
	t.Parallel()
	const (
		base    = "{$ref: '#/components/schemas/Base'}"
		bareRef = "{properties: {f: {$ref: '#/components/schemas/Lender'}}}"
	)
	for _, tc := range lentDetails {
		entry := "{properties: {f: {" + tc.entry + "}}}"
		for _, lending := range []string{base, bareRef} {
			doc, diags := schema.LowerSpec(t, lentSpec(tc.lender, lending, entry))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Empty(t, conflictNotes(diags), "%s through %s", tc.detail, lending)
			field := propertyOf(t, doc, "Composed", "f")
			assert.Equal(t, tc.written, tc.held(field), tc.detail)
			assert.Empty(t, field.Unmodeled, "%s: nothing is kept beside the field", tc.detail)
		}

		doc, diags := schema.LowerSpec(t, lentSpec(tc.lender, entry, base))
		openapitest.RequireNoErrorDiags(t, diags)
		notes := conflictNotes(diags)
		require.Len(t, notes, 1, "%s written first", tc.detail)
		assert.Contains(t, notes[0].Message, "give its "+tc.detail+" differently", tc.detail)
		assert.Equal(t, "/components/schemas/Composed/allOf/0/properties/f", string(notes[0].Provenance.Pointer), tc.detail)
		assert.Equal(t, tc.lent, tc.held(propertyOf(t, doc, "Composed", "f")), tc.detail)
	}
}

// TestAllOf_ADetailWrittenBesideARefIsAPartyToAConflict pins the other side of
// GitHub #2152: a declaration writing a detail beside its $ref, a deprecation
// member a key states among them, wrote what the fold sets aside, so it is
// reported.
func TestAllOf_ADetailWrittenBesideARefIsAPartyToAConflict(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ detail, lender, beside, entry string }{
		{"description", "description: Lent.", "description: Beside.", "description: Written."},
		{"title", "title: Lent", "title: Beside", "title: Written"},
		{"external docs", "externalDocs: {url: 'https://lent.example'}", "externalDocs: {url: 'https://beside.example'}",
			"externalDocs: {url: 'https://written.example'}"},
		{"deprecation", "deprecated: true", "x-deprecated-reason: beside", "deprecated: true"},
		{"deprecation", "x-deprecated-reason: lent", "deprecated: true", "deprecated: true, x-deprecated-reason: written"},
		{"default", "default: lent", "default: beside", "default: written"},
	} {
		lending := "{properties: {f: {$ref: '#/components/schemas/Lender', " + tc.beside + "}}}"
		_, diags := schema.LowerSpec(t, lentSpec(tc.lender, lending, "{properties: {f: {"+tc.entry+"}}}"))
		openapitest.RequireNoErrorDiags(t, diags)
		notes := conflictNotes(diags)
		require.Len(t, notes, 1, "%s beside the $ref", tc.beside)
		assert.Contains(t, notes[0].Message, "give its "+tc.detail+" differently", tc.beside)
	}
}

// TestAllOf_AMemberAFoldSettledLendsWhatItKept pins GitHub #2152 through a
// part whose member a fold settled, of the part's own declarations or beside
// one its entry brings: the member kept a lent description or a written one,
// which the pointer it takes does not say, and a model restating the member
// reports only the written one, at the entry bringing the part. The part's own
// fold reports only a written description it set aside.
func TestAllOf_AMemberAFoldSettledLendsWhatItKept(t *testing.T) {
	t.Parallel()
	const (
		base      = "{$ref: '#/components/schemas/Base'}"
		bareRef   = "{properties: {f: {$ref: '#/components/schemas/Lender'}}}"
		composing = "/components/schemas/Composed/allOf/0"
	)
	for _, tc := range []struct {
		name  string
		part  []string
		notes []string
	}{
		{"brought, lent kept", []string{base, "{properties: {f: {readOnly: true}}}"}, nil},
		{"brought, written kept", []string{base, "{properties: {f: {description: Part wrote this.}}}"}, []string{composing}},
		{"own, lent kept", []string{"{properties: {f: {description: Part wrote this.}}}", bareRef},
			[]string{"/components/schemas/Part/allOf/0/properties/f"}},
		{"own, written kept", []string{bareRef, "{properties: {f: {description: Part wrote this.}}}"}, []string{composing}},
	} {
		doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Lender: {type: string, description: Lent.}
    Base: {type: object, properties: {f: {$ref: '#/components/schemas/Lender'}}}
    Part: {allOf: [`+strings.Join(tc.part, ", ")+`]}
    Composed: {allOf: [{$ref: '#/components/schemas/Part'}, {properties: {f: {description: Written.}}}]}
`))
		openapitest.RequireNoErrorDiags(t, diags)
		var at []string
		for _, note := range conflictNotes(diags) {
			at = append(at, string(note.Provenance.Pointer))
		}
		assert.Equal(t, tc.notes, at, tc.name)
		model, ok := doc.Types[componentID("Composed")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, model.Properties, 1)
		assert.Equal(t, "Written.", model.Properties[0].Docs.Description, tc.name)
	}
}
