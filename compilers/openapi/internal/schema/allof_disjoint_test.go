// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// disjointComponents are the schemas the cases below compose, written for 3.1.
// Props writes no type, so it admits a string though it constrains objects, and
// Yes is the boolean schema a $ref can land on.
const disjointComponents = `    List: {type: array, items: {type: string}}
    Str: {type: string}
    Obj: {type: object}
    Props: {properties: {a: {type: string}}}
    NullStr: {type: [string, "null"]}
    NullObj: {type: [object, "null"]}
    Alias: {$ref: '#/components/schemas/List'}
    Composed: {allOf: [{$ref: '#/components/schemas/List'}]}
    U: {oneOf: [{type: string}, {type: integer}]}
    Yes: true
`

// Short spellings of the $ref entries the cases compose.
const (
	refList = `{$ref: '#/components/schemas/List'}`
	refStr  = `{$ref: '#/components/schemas/Str'}`
	refObj  = `{$ref: '#/components/schemas/Obj'}`
)

// disjointSpec is a document holding disjointComponents, extra, and S written
// as s.
func disjointSpec(s, extra string) string {
	return openapitest.ComponentSpec(disjointComponents + extra + "    S: " + s + "\n")
}

// disjointReports returns the warnings in diags that say conjuncts share no
// value.
func disjointReports(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityWarning &&
			strings.Contains(d.Message, "share no value") {
			out = append(out, d)
		}
	}
	return out
}

// TestAllOf_DisjointConjunctsReportOnceAtTheSchema holds every spelling of an
// allOf whose conjuncts share no value to one warning at the schema, naming
// each conjunct that narrows the instance types, and to no other warning of
// the code in the document: an inline branch's own report is an info there.
func TestAllOf_DisjointConjunctsReportOnceAtTheSchema(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, s, want string
	}{
		{"a type beside the allOf against a $ref",
			`{type: object, allOf: [` + refList + `]}`, "type: object, allOf/0: array"},
		{"two $refs",
			`{allOf: [` + refStr + `, ` + refObj + `]}`, "allOf/0: string, allOf/1: object"},
		{"an inline branch against a $ref",
			`{allOf: [` + refObj + `, {type: string}]}`, "allOf/0: object, allOf/1: string"},
		{"an integer against a string",
			`{allOf: [{type: string}, {type: integer}]}`, "allOf/0: string, allOf/1: number"},
		{"a type beside the allOf against an inline branch",
			`{type: object, allOf: [{type: string}]}`, "type: object, allOf/0: string"},
		{"a type set against a $ref",
			`{type: [string, integer], allOf: [` + refObj + `]}`, "type: string or number, allOf/0: object"},
		{"three entries overlapping pairwise and empty jointly",
			`{allOf: [{type: [string, integer]}, {type: [integer, boolean]}, {type: [boolean, string]}]}`,
			"allOf/0: string or number, allOf/1: number or boolean, allOf/2: string or boolean"},
		{"a $ref through an alias",
			`{allOf: [{$ref: '#/components/schemas/Alias'}, ` + refObj + `]}`, "allOf/0: array, allOf/1: object"},
		{"a $ref to a composition",
			`{allOf: [{$ref: '#/components/schemas/Composed'}, ` + refObj + `]}`, "allOf/0: array, allOf/1: object"},
		{"a flattened wrapper",
			`{allOf: [{allOf: [` + refStr + `, ` + refObj + `]}]}`,
			"allOf/0/allOf/0: string, allOf/0/allOf/1: object"},
		{"a flattened wrapper's own type",
			`{allOf: [{type: string, allOf: [` + refObj + `]}]}`, "allOf/0/allOf/0: object, allOf/0: string"},
		{"a null against an object",
			`{allOf: [{type: "null"}, ` + refObj + `]}`, "allOf/0: null, allOf/1: object"},
		{"a nullable string against an object",
			`{allOf: [{$ref: '#/components/schemas/NullStr'}, ` + refObj + `]}`, "allOf/0: string, allOf/1: object"},
		{"an entry stating nothing goes unnamed",
			`{allOf: [` + refStr + `, {description: d}, ` + refObj + `]}`, "allOf/0: string, allOf/2: object"},
		{"a distributed union with an inline branch",
			`{allOf: [` + refObj + `, {type: string}], oneOf: [` + refObj + `, ` + refList + `]}`,
			"allOf/0: object, allOf/1: string"},
		{"a distributed union",
			`{allOf: [` + refStr + `, ` + refObj + `], oneOf: [` + refObj + `, ` + refList + `]}`,
			"allOf/0: string, allOf/1: object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := parseFull(t, disjointSpec(tc.s, ""))

			reports := disjointReports(doc.Diagnostics)
			require.Len(t, reports, 1, "one report, wherever the conjuncts are written: %+v", doc.Diagnostics)
			assert.Equal(t, "/components/schemas/S", string(reports[0].Provenance.Pointer))
			assert.Contains(t, reports[0].Message, "share no value ("+tc.want+")")
			assert.Equal(t, 1, openapitest.CountDiagsAt(doc.Diagnostics, diag.DegradedConstruct, ir.SeverityWarning),
				"no other degraded-construct warning in the document: %+v", doc.Diagnostics)
		})
	}
}

// TestAllOf_ConjunctsSharingAValueStaySilent holds the readings that must not
// report: a conjunct writing no type states nothing, and neither does a
// boolean, an unresolved $ref or a keyword beside a $ref, and a null both
// sides admit is a value they share.
func TestAllOf_ConjunctsSharingAValueStaySilent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, s, extra string
		// also is a code the case must still draw, which is what a case that
		// stays silent about the conjuncts says about something else.
		also string
	}{
		{"an integer with a number", `{allOf: [{type: integer}, {type: number}]}`, "", ""},
		{"an object with an object", `{allOf: [` + refObj + `, {type: object}]}`, "", ""},
		{"a string with a schema writing no type",
			`{allOf: [` + refStr + `, {$ref: '#/components/schemas/Props'}]}`, "", ""},
		{"a string with a constraint of strings", `{allOf: [` + refStr + `, {maxLength: 3}]}`, "", ""},
		{"two nullable types",
			`{allOf: [{$ref: '#/components/schemas/NullStr'}, {$ref: '#/components/schemas/NullObj'}]}`, "", ""},
		{"a null with a nullable object",
			`{allOf: [{type: "null"}, {$ref: '#/components/schemas/NullObj'}]}`, "", ""},
		{"a type set holding the other's type", `{allOf: [{type: [string, object]}, ` + refObj + `]}`, "", ""},
		{"a union stating no type", `{allOf: [{$ref: '#/components/schemas/U'}, {type: object}]}`, "", ""},
		{"a true and a false entry", `{allOf: [true, false, ` + refObj + `]}`, "", diag.FalseSchema},
		{"a schema composing itself", `{allOf: [{$ref: '#/components/schemas/Cy'}, ` + refObj + `]}`,
			"    Cy: {allOf: [{$ref: '#/components/schemas/Cy'}, {$ref: '#/components/schemas/Cy'}]}\n", ""},
		{"an unresolved $ref", `{allOf: [{$ref: '#/components/schemas/Ghost'}, ` + refObj + `]}`, "", ""},
		{"a $ref landing on a boolean", `{allOf: [{$ref: '#/components/schemas/Yes'}, ` + refObj + `]}`, "", ""},
		{"a keyword beside a $ref", `{allOf: [{$ref: '#/components/schemas/Str', type: object}]}`, "", ""},
		{"a type no dialect defines", `{type: widget, allOf: [` + refObj + `]}`, "", ""},
		{"an enum passing the allOf over", `{enum: [a], allOf: [` + refStr + `, ` + refObj + `]}`, "", ""},
		{"an allOf beside a $ref", `{$ref: '#/components/schemas/Str', allOf: [` + refObj + `]}`, "", ""},
		{"a null read past its budget",
			`{allOf: [` + strings.Repeat(`{$ref: '#/components/schemas/Props'}, `, 130) +
				`{$ref: '#/components/schemas/NullStr'}, {$ref: '#/components/schemas/NullObj'}]}`, "", ""},
		{"a distributed union composing nothing",
			`{properties: {a: {type: string}}, oneOf: [` + refObj + `, ` + refList + `]}`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := parseFull(t, disjointSpec(tc.s, tc.extra))

			assert.Empty(t, disjointReports(doc.Diagnostics))
			if tc.also != "" {
				assert.True(t, openapitest.HasDiag(doc.Diagnostics, tc.also),
					"the case still draws %s: %+v", tc.also, doc.Diagnostics)
			}
		})
	}
}

// TestAllOf_ComposingADisjointSchemaDrawsNoSecondReport holds a schema composing
// one whose conjuncts share no value to the one report, at the schema that
// writes them.
func TestAllOf_ComposingADisjointSchemaDrawsNoSecondReport(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, disjointSpec(`{allOf: [`+refList+`]}`, `    Mid: {allOf: [`+refStr+`, `+refObj+`]}
    Outer: {allOf: [{$ref: '#/components/schemas/Mid'}]}
    Outer2: {allOf: [{$ref: '#/components/schemas/Mid'}, `+refObj+`]}
`))

	reports := disjointReports(doc.Diagnostics)
	require.Len(t, reports, 1, "%+v", doc.Diagnostics)
	assert.Equal(t, "/components/schemas/Mid", string(reports[0].Provenance.Pointer))
}

// TestAllOf_DisjointInlineBranchReportsItsResidueAsInfo holds an inline branch
// the schema's report covers to its residue alone: an info that names what is
// kept, with the branch still in Unmodeled, since the warning is the schema's.
func TestAllOf_DisjointInlineBranchReportsItsResidueAsInfo(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, disjointSpec(`{allOf: [`+refObj+`, {type: string, maxLength: 3}]}`, ""))

	var atBranch []ir.Diagnostic
	for _, d := range doc.Diagnostics {
		if d.Provenance.Pointer == "/components/schemas/S/allOf/1" {
			atBranch = append(atBranch, d)
		}
	}
	require.Len(t, atBranch, 1, "%+v", doc.Diagnostics)
	assert.Equal(t, ir.SeverityInfo, atBranch[0].Severity)
	assert.Contains(t, atBranch[0].Message, "the branch (type, maxLength) is kept verbatim under Unmodeled")

	s, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Contains(t, s.Unmodeled, "openapi:allOf/1", "and the branch is still kept")
	assert.Len(t, disjointReports(doc.Diagnostics), 1)
}

// TestAllOf_DisjointConjunctsFollowTheNullableReading holds the report to the
// reading of null the nullable-widening policy selects: a schema written
// `nullable: true` beside conjuncts that share no other value admits null while
// the policy is on, and does not once it is off.
func TestAllOf_DisjointConjunctsFollowTheNullableReading(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.0.3", `    Str: {type: string}
    Obj: {type: object}
    S:
      allOf: [`+refStr+`, `+refObj+`]
      nullable: true
`)

	on, onDiags := compileNullable(t, spec, false)
	require.NotNil(t, on)
	assert.Empty(t, disjointReports(onDiags), "the policy reads null in, which both conjuncts then share")
	assert.True(t, openapitest.HasDiag(onDiags, diag.InferredNullable), "and says it did: %+v", onDiags)

	off, offDiags := compileNullable(t, spec, true)
	require.NotNil(t, off)
	assert.Len(t, disjointReports(offDiags), 1, "as 3.0 defines nullable, the string and the object share nothing")
	assert.False(t, openapitest.HasDiag(offDiags, diag.InferredNullable))
}

// TestAllOf_DisjointConjunctsIgnoreDeclarationOrder holds the report to what
// the source writes, not to which component lowered first: the composing schema
// is declared before the schemas it names and then after them.
func TestAllOf_DisjointConjunctsIgnoreDeclarationOrder(t *testing.T) {
	t.Parallel()
	const (
		composing = "    S: {allOf: [{$ref: '#/components/schemas/Alias'}, {$ref: '#/components/schemas/Composed'}, " + refStr + "]}\n"
		targets   = `    Str: {type: string}
    List: {type: array, items: {type: string}}
    Alias: {$ref: '#/components/schemas/List'}
    Composed: {allOf: [{type: object}]}
`
	)
	before, _ := parseFull(t, openapitest.ComponentSpec(composing+targets))
	after, _ := parseFull(t, openapitest.ComponentSpec(targets+composing))

	got, want := reported(disjointReports(before.Diagnostics)), reported(disjointReports(after.Diagnostics))
	require.Len(t, got, 1, "%+v", before.Diagnostics)
	assert.Contains(t, got[0], "share no value (allOf/0: array, allOf/1: object, allOf/2: string)")
	assert.Empty(t, cmp.Diff(want, got), "the report does not depend on the declaration order")
}

// TestAllOf_DisjointConjunctsReportAtEachPositionKind holds the report to the two
// shapes a schema position is written in: a property of an object, and the
// schema of a response's media type.
func TestAllOf_DisjointConjunctsReportAtEachPositionKind(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /z:
    delete:
      operationId: removeZone
      parameters:
        - name: q
          in: query
          schema:
            allOf:
              - $ref: '#/components/schemas/Str'
              - $ref: '#/components/schemas/Obj'
      requestBody:
        content:
          application/json:
            schema:
              allOf:
                - $ref: '#/components/schemas/Str'
                - $ref: '#/components/schemas/Obj'
      responses:
        "400":
          description: bad request
          content:
            application/json:
              schema:
                allOf:
                  - $ref: '#/components/schemas/Str'
                  - $ref: '#/components/schemas/Obj'
components:
  schemas:
    List: {type: array, items: {type: string}}
    Str: {type: string}
    Obj: {type: object}
    Holder:
      type: object
      properties:
        errors:
          type: object
          allOf: [{$ref: '#/components/schemas/List'}]
        list:
          type: array
          items:
            allOf:
              - $ref: '#/components/schemas/Str'
              - $ref: '#/components/schemas/Obj'
`)

	reports := disjointReports(doc.Diagnostics)
	got := make([]string, 0, len(reports))
	for _, d := range reports {
		got = append(got, string(d.Provenance.Pointer))
	}
	assert.ElementsMatch(t, []string{
		"/components/schemas/Holder/properties/errors",
		"/components/schemas/Holder/properties/list/items",
		"/paths/~1z/delete/parameters/0/schema",
		"/paths/~1z/delete/requestBody/content/application~1json/schema",
		"/paths/~1z/delete/responses/400/content/application~1json/schema",
	}, got, "one report at each position: %+v", doc.Diagnostics)
}
