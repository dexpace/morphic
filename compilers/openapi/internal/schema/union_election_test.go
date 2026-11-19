// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestUnion_AnAnyOfAloneDeclaringShapeIsElected pins a oneOf narrowing only
// what the body declares beside an anyOf of objects of their own, beside an
// allOf and on one entry of it: the anyOf is distributed, and each variant
// keeps the oneOf as validation logic, the union passed over being a conjunct
// of the rest.
func TestUnion_AnAnyOfAloneDeclaringShapeIsElected(t *testing.T) {
	t.Parallel()
	const keywords = "oneOf: [{required: [a]}, {maxProperties: 3}], " +
		"anyOf: [{type: object, properties: {d: {type: string}}}, {type: object, properties: {e: {type: string}}}]"
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    T: {type: object, properties: {a: {type: string}}}
    Beside: {allOf: [{$ref: '#/components/schemas/T'}], `+keywords+`}
    Entry: {allOf: [{$ref: '#/components/schemas/T'}, {`+keywords+`}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"Beside", "Entry"} {
		union, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s distributes", name)
		assert.False(t, union.Exclusive, "%s elects its anyOf", name)
		require.Len(t, union.Variants, 2)
		for _, v := range union.Variants {
			kept := doc.Types[v.Type.Target].Common().Unmodeled
			require.Contains(t, kept, "openapi:oneOf", name)
			assert.Equal(t, ir.ReasonValidationOnly, kept["openapi:oneOf"].Reason, name)
		}
	}
}

// TestUnion_AnEntryHoldingBothIsReadWithoutThemWhereverListed pins an entry
// writing a oneOf of strings and integers beside an anyOf of objects, on the
// allOf, nested one or two allOfs deeper, under a wrapper writing a type of
// its own, and beside a $ref to the body: each keeps both unions, as the
// oneOf declares shape against the body read without them, where read with
// them the body admits nothing and the anyOf was elected.
func TestUnion_AnEntryHoldingBothIsReadWithoutThemWhereverListed(t *testing.T) {
	t.Parallel()
	const both = "{oneOf: [{type: string}, {type: integer}], " +
		"anyOf: [{type: object, properties: {d: {type: string}}}, {type: object, properties: {e: {type: string}}}]}"
	const ref = "{$ref: '#/components/schemas/T'}"
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    T: {type: object, properties: {a: {type: string}}}
    Entry: {allOf: [`+ref+`, `+both+`]}
    Nested: {allOf: [`+ref+`, {allOf: [`+both+`]}]}
    Deeper: {allOf: [`+ref+`, {allOf: [{allOf: [`+both+`]}]}]}
    Typed: {allOf: [`+ref+`, {type: object, allOf: [`+both+`]}]}
    BesideRef: {allOf: [{$ref: '#/components/schemas/T', `+both[1:]+`]}
`))
	for _, name := range []string{"Entry", "Nested", "Deeper", "Typed", "BesideRef"} {
		model, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s keeps both unions on a Model", name)
		assert.Contains(t, model.Unmodeled, "openapi:oneOf", name)
		assert.Contains(t, model.Unmodeled, "openapi:anyOf", name)
	}
}

// TestUnion_EntriesOneAllOfDeeperJudgeEachOtherByNeither pins two unions on
// two entries nested one allOf deeper beside an object body, a oneOf of
// strings and an anyOf of booleans: each declares shape against the body read
// without the other, so both are kept as degraded_lowering, as on the allOf's
// own entries.
func TestUnion_EntriesOneAllOfDeeperJudgeEachOtherByNeither(t *testing.T) {
	t.Parallel()
	const unions = "{oneOf: [{type: string}]}, {anyOf: [{type: boolean}]}"
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    T: {type: object, properties: {a: {type: string}}}
    Entries: {allOf: [{$ref: '#/components/schemas/T'}, `+unions+`]}
    Deeper: {allOf: [{$ref: '#/components/schemas/T'}, {allOf: [`+unions+`]}]}
`))
	for _, name := range []string{"Entries", "Deeper"} {
		model, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s keeps both unions on a Model", name)
		for _, key := range []string{"openapi:oneOf", "openapi:anyOf"} {
			require.Contains(t, model.Unmodeled, key, name)
			assert.Equal(t, ir.ReasonDegradedLowering, model.Unmodeled[key].Reason, "%s %s", name, key)
		}
	}
}

// TestUnion_TwoUnionsAloneLowerAsOneSchemaWritingBoth pins a oneOf of objects
// and an anyOf every one of them satisfies, with nothing beside them, on one
// schema, on one allOf entry and on two: each is the Union of the oneOf
// keeping the anyOf as degraded_lowering, the union passed over kept though
// the elected one says it, and an anyOf alone declaring shape is elected over
// a oneOf narrowing nothing it declares. Two oneOfs elect neither, and a keyword beside
// the two that no branch says is a body they distribute over.
func TestUnion_TwoUnionsAloneLowerAsOneSchemaWritingBoth(t *testing.T) {
	t.Parallel()
	const oneOf = "oneOf: [{type: object, properties: {p: {type: string}}}, {type: object, properties: {q: {type: string}}}]"
	const anyOf = "anyOf: [{type: object}]"
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    Flat: {`+oneOf+`, `+anyOf+`}
    OneEntry: {allOf: [{`+oneOf+`, `+anyOf+`}]}
    TwoEntries: {allOf: [{`+anyOf+`}, {`+oneOf+`}]}
    TwoOneOfs: {allOf: [{`+oneOf+`}, {oneOf: [{type: object, properties: {r: {type: string}}}, {required: [r]}]}]}
    Required: {allOf: [{`+oneOf+`}, {`+anyOf+`}, {required: [p]}]}
    Shapeless: {allOf: [{oneOf: [{required: [p]}, {}]}, {anyOf: [{type: object, properties: {x: {type: string}}}]}]}
`))
	for _, name := range []string{"Flat", "OneEntry", "TwoEntries"} {
		union, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s is the Union of its oneOf", name)
		assert.True(t, union.Exclusive, name)
		require.Contains(t, union.Unmodeled, "openapi:anyOf", name)
		assert.Equal(t, ir.ReasonDegradedLowering, union.Unmodeled["openapi:anyOf"].Reason, name)
		for _, v := range union.Variants {
			assert.Contains(t, string(v.Type.Target), "type/anonymous/", "%s's variant is its branch", name)
		}
	}
	shapeless, ok := typeByName(doc, "Shapeless").(*ir.Union)
	require.True(t, ok, "the anyOf declaring shape is the Union")
	assert.False(t, shapeless.Exclusive)
	require.Contains(t, shapeless.Unmodeled, "openapi:oneOf")
	assert.Equal(t, ir.ReasonValidationOnly, shapeless.Unmodeled["openapi:oneOf"].Reason)
	_, isModel := typeByName(doc, "TwoOneOfs").(*ir.Model)
	assert.True(t, isModel, "two oneOfs elect neither")
	required, ok := typeByName(doc, "Required").(*ir.Union)
	require.True(t, ok, "the oneOf distributes over the body")
	for _, v := range required.Variants {
		assert.Contains(t, string(v.Type.Target), "type/composed/", "the variant composes the body")
	}
}

// TestUnion_TwoUnionsAloneSharingNoValueWarnOnceWhereverWritten pins a oneOf of
// objects and an anyOf of strings with nothing beside them, on one schema and
// on two allOf entries: each is the Union of the oneOf keeping the anyOf, and
// draws one warning at the schema that the two share no value, naming the
// union it lowered as.
func TestUnion_TwoUnionsAloneSharingNoValueWarnOnceWhereverWritten(t *testing.T) {
	t.Parallel()
	const oneOf = "oneOf: [{type: object, properties: {p: {type: string}}}, {type: object, properties: {q: {type: string}}}]"
	const anyOf = "anyOf: [{type: string}]"
	_, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Flat: {`+oneOf+`, `+anyOf+`}
    TwoEntries: {allOf: [{`+anyOf+`}, {`+oneOf+`}]}
`))
	var warned []string
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning {
			assert.Contains(t, d.Message, "share no value")
			assert.Contains(t, d.Message, "which the union it lowered as cannot state")
			warned = append(warned, string(d.Provenance.Pointer))
		}
	}
	assert.ElementsMatch(t, []string{"/components/schemas/Flat", "/components/schemas/TwoEntries"}, warned)
}

// TestUnion_ANullBranchIsReadAgainstTheUnionPassedOver pins the anyOf a schema
// elects over a body admitting an object or null, beside a oneOf of objects
// alone: its branch naming null is dead, the passed-over oneOf keeping null
// out of the rest, beside the allOf as on two entries; beside a body admitting
// no null its string branch is dead too.
func TestUnion_ANullBranchIsReadAgainstTheUnionPassedOver(t *testing.T) {
	t.Parallel()
	const unions = "oneOf: [{type: object}], anyOf: [{type: object, properties: {x: {type: string}}}, {$ref: '#/components/schemas/Null'}]"
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    Null: {type: 'null'}
    TN: {type: [object, 'null'], properties: {a: {type: string}}}
    T: {type: object, properties: {a: {type: string}}}
    Beside: {allOf: [{$ref: '#/components/schemas/TN'}], `+unions+`}
    Two: {allOf: [{$ref: '#/components/schemas/TN'}, {oneOf: [{type: object}]}, {anyOf: [{type: object, properties: {x: {type: string}}}, {$ref: '#/components/schemas/Null'}]}]}
    NoNull: {allOf: [{$ref: '#/components/schemas/T'}], anyOf: [{type: object, properties: {x: {type: string}}}, {type: string}]}
`))
	for _, name := range []string{"Beside", "Two", "NoNull"} {
		union, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s distributes", name)
		assert.Len(t, union.Variants, 1, "%s's second branch is dead", name)
		assert.Contains(t, union.Unmodeled, "openapi:anyOf/1", name)
	}
}

// TestUnion_TwoUnionsSharingNoValueBesideAScalarKeepBoth pins a oneOf of
// strings and an anyOf of booleans beside a string body, beside its keywords,
// beside an allOf of it, on one entry and on two: each is the string the body
// gives, keeping both unions, with one warning that they share no value.
func TestUnion_TwoUnionsSharingNoValueBesideAScalarKeepBoth(t *testing.T) {
	t.Parallel()
	const one, anyOf = "oneOf: [{type: string}]", "anyOf: [{type: boolean}]"
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Flat: {type: string, `+one+`, `+anyOf+`}
    Beside: {allOf: [{type: string}], `+one+`, `+anyOf+`}
    OneEntry: {allOf: [{type: string}, {`+one+`, `+anyOf+`}]}
    TwoEntries: {allOf: [{type: string}, {`+one+`}, {`+anyOf+`}]}
    S: {type: string}
    BesideRef: {$ref: '#/components/schemas/S', `+one+`, `+anyOf+`}
`))
	for _, name := range []string{"Flat", "Beside", "OneEntry", "TwoEntries", "BesideRef"} {
		_, ok := typeByName(doc, name).(*ir.Scalar)
		assert.True(t, ok, "%s is the string its body gives", name)
		var warned []string
		for _, d := range diags {
			if d.Severity == ir.SeverityWarning && strings.HasSuffix(string(d.Provenance.Pointer), "/"+name) {
				warned = append(warned, d.Message)
			}
		}
		require.Len(t, warned, 1, name)
		assert.Contains(t, warned[0], "share no value", name)
	}
}

// TestUnion_TwoUnionEntriesBesideABodyElectByCompetingKeywords pins a oneOf
// of an object and a $ref to a string and an anyOf of that $ref alone, beside
// a string, beside the allOf and on two entries in either order: the oneOf is
// elected, both declaring shape, though the anyOf's kinds are the body's, and
// the anyOf is kept as degraded_lowering.
func TestUnion_TwoUnionEntriesBesideABodyElectByCompetingKeywords(t *testing.T) {
	t.Parallel()
	const oneOf = "oneOf: [{type: object, properties: {x: {type: string}}}, {$ref: '#/components/schemas/Kstr'}]"
	const anyOf = "anyOf: [{$ref: '#/components/schemas/Kstr'}]"
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    Kstr: {type: string}
    Beside: {allOf: [{type: string}], `+oneOf+`, `+anyOf+`}
    TwoEntries: {allOf: [{type: string}, {`+oneOf+`}, {`+anyOf+`}]}
    AnyOfFirst: {allOf: [{type: string}, {`+anyOf+`}, {`+oneOf+`}]}
`))
	for _, name := range []string{"Beside", "TwoEntries", "AnyOfFirst"} {
		union, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s is a Union", name)
		assert.True(t, union.Exclusive, "%s elects the oneOf", name)
		assert.Len(t, union.Variants, 2, name)
		require.Contains(t, union.Unmodeled, "openapi:anyOf", name)
		assert.Equal(t, ir.ReasonDegradedLowering, union.Unmodeled["openapi:anyOf"].Reason, name)
	}
}

// TestUnion_TwoShapelessUnionsSharingNoValueBesideTheirBodyWarn holds that a
// oneOf and an anyOf, neither declaring shape against the body written beside
// them, draw the one warning that the conjuncts share no value there, as
// beside an allOf of the body: two sharing no value with each other, and two
// sharing null alone beside a body keeping null out, a null being shared only
// where every conjunct admits it.
func TestUnion_TwoShapelessUnionsSharingNoValueBesideTheirBodyWarn(t *testing.T) {
	t.Parallel()
	_, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    Flat: {type: string, oneOf: [{type: 'null'}], anyOf: [{type: string}]}
    Beside: {allOf: [{type: string}], oneOf: [{type: 'null'}], anyOf: [{type: string}]}
    Shared: {type: string, oneOf: [{type: 'null'}], anyOf: [{type: 'null'}]}
    SharedBeside: {allOf: [{type: string}], oneOf: [{type: 'null'}], anyOf: [{type: 'null'}]}
    Lists: {type: string, oneOf: [{type: [string, 'null']}, {type: [string, 'null']}], anyOf: [{type: 'null'}]}
`))
	warned := map[string][]string{}
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning {
			name := string(d.Provenance.Pointer)[strings.LastIndexByte(string(d.Provenance.Pointer), '/')+1:]
			warned[name] = append(warned[name], d.Message)
		}
	}
	for _, name := range []string{"Flat", "Beside", "Shared", "SharedBeside", "Lists"} {
		require.Len(t, warned[name], 1, name)
		assert.Contains(t, warned[name][0], "share no value", name)
	}
}

// TestUnion_AnEntryElectingAUnionOfNullAloneIsNamedByItsNull holds that the
// report of conjuncts sharing no value names an entry writing a oneOf and an
// anyOf, read as its own keywords and the union it elects, by the null that
// union admits where it admits null alone, whatever the union it passes over
// admits.
func TestUnion_AnEntryElectingAUnionOfNullAloneIsNamedByItsNull(t *testing.T) {
	t.Parallel()
	_, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    Str: {allOf: [{type: string}, {oneOf: [{type: 'null'}], anyOf: [{type: string}]}]}
    Nest: {allOf: [{type: string}, {allOf: [{oneOf: [{type: 'null'}], anyOf: [{type: string}]}]}]}
    Obj: {allOf: [{type: object}, {oneOf: [{type: 'null'}], anyOf: [{type: 'null'}, {type: object, required: [x]}]}]}
`))
	want := map[string]string{
		"Str":  "(allOf/0: string, allOf/1: null, allOf/1/anyOf: string)",
		"Nest": "(allOf/0: string, allOf/1/allOf/0: null, allOf/1/allOf/0/anyOf: string)",
		"Obj":  "(allOf/0: object, allOf/1: null, allOf/1/anyOf: object)",
	}
	for name, names := range want {
		var warned []string
		for _, d := range diags {
			if d.Severity == ir.SeverityWarning && strings.HasSuffix(string(d.Provenance.Pointer), "/"+name) {
				warned = append(warned, d.Message)
			}
		}
		require.Len(t, warned, 1, name)
		assert.Contains(t, warned[0], names, name)
	}
}

// TestUnion_AVariantAdmitsNullOnlyWhereTheRestDoes pins a union whose branch
// admits a string or null beside a string body, beside the allOf, on its entry
// and as the oneOf of a pair beside and on two entries: the variant is a
// string, not nullable, the body keeping null out, and nothing warns, the
// branch's string living. With no body the variant keeps its null, but where
// the anyOf passed over keeps null out, alone or on two entries.
func TestUnion_AVariantAdmitsNullOnlyWhereTheRestDoes(t *testing.T) {
	t.Parallel()
	const branches = "[{type: string, enum: [b]}, {type: [string, 'null']}]"
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Beside: {allOf: [{type: string}], anyOf: `+branches+`}
    Entry: {allOf: [{type: string}, {anyOf: `+branches+`}]}
    PairBeside: {allOf: [{type: string}], oneOf: `+branches+`, anyOf: [{}]}
    PairTwo: {allOf: [{type: string}, {oneOf: `+branches+`}, {anyOf: [{}]}]}
    NoBody: {anyOf: `+branches+`}
    PairAlone: {oneOf: `+branches+`, anyOf: [{type: string}]}
    PairAloneTwo: {allOf: [{oneOf: `+branches+`}, {anyOf: [{type: string}]}]}
`))
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityWarning, d.Severity, d.Message)
	}
	for name, nullable := range map[string]bool{
		"Beside": false, "Entry": false, "PairBeside": false, "PairTwo": false, "NoBody": true,
		"PairAlone": false, "PairAloneTwo": false,
	} {
		union, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s is a Union", name)
		require.Len(t, union.Variants, 2, name)
		assert.False(t, union.Variants[0].Type.Nullable, name)
		assert.Equal(t, nullable, union.Variants[1].Type.Nullable, name)
	}
}

// TestUnion_TwoUnionsSharingNoMemberWarnOnceWhereverWritten pins a oneOf and
// an anyOf whose enums share no member, on one schema, on one allOf entry, on
// two and beside an object body they compose with: each draws one warning
// that the conjuncts share no value, naming the two by their value sets.
func TestUnion_TwoUnionsSharingNoMemberWarnOnceWhereverWritten(t *testing.T) {
	t.Parallel()
	const one, anyOf = "oneOf: [{type: string, enum: [a]}]", "anyOf: [{type: string, enum: [b]}]"
	const objOne, objAny = "oneOf: [{type: object, enum: [{a: 1}]}]", "anyOf: [{type: object, enum: [{b: 2}]}]"
	_, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Flat: {`+one+`, `+anyOf+`}
    OneEntry: {allOf: [{`+one+`, `+anyOf+`}]}
    TwoEntries: {allOf: [{`+one+`}, {`+anyOf+`}]}
    Composed: {allOf: [{type: object}, {`+objOne+`}, {`+objAny+`}]}
`))
	for _, name := range []string{"Flat", "OneEntry", "TwoEntries", "Composed"} {
		var warned []string
		for _, d := range diags {
			if d.Severity == ir.SeverityWarning && strings.HasSuffix(string(d.Provenance.Pointer), "/"+name) {
				warned = append(warned, d.Message)
			}
		}
		require.Len(t, warned, 1, name)
		assert.Contains(t, warned[0], "share no value", name)
		assert.Contains(t, warned[0], "anyOf: 1 value", name)
		assert.Contains(t, warned[0], "oneOf: 1 value", name)
	}
}

// TestUnion_APairAdmitsNullOnlyWhereBothUnionsMay pins the reference to a
// schema writing a oneOf and an anyOf: nullable only where each union may
// admit null, beside a $ref to an object and beside an allOf of it, drawing no
// inferred-nullable info where one union keeps null out; and nullable where the
// other's branch admits anything.
func TestUnion_APairAdmitsNullOnlyWhereBothUnionsMay(t *testing.T) {
	t.Parallel()
	const unions = "oneOf: [{type: object, properties: {x: {type: string}}}], anyOf: [{type: object}, {type: 'null'}]"
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    T: {type: object, properties: {a: {type: string}}}
    Flat: {$ref: '#/components/schemas/T', `+unions+`}
    Beside: {allOf: [{$ref: '#/components/schemas/T'}], `+unions+`}
    StringOrNull: {oneOf: [{type: string}, {type: 'null'}], anyOf: [{type: string}]}
    Open: {oneOf: [{type: string}, {type: 'null'}], anyOf: [{}]}
    C: {type: object, properties: {f: {$ref: '#/components/schemas/Flat'}, b: {$ref: '#/components/schemas/Beside'}, s: {$ref: '#/components/schemas/StringOrNull'}, o: {$ref: '#/components/schemas/Open'}}}
`))
	for _, d := range diags {
		assert.NotEqual(t, "openapi/inferred-nullable", d.Code, d.Message)
	}
	model, ok := typeByName(doc, "C").(*ir.Model)
	require.True(t, ok)
	want := map[string]bool{"f": false, "b": false, "s": false, "o": true}
	for _, p := range model.Properties {
		assert.Equal(t, want[p.Name.Source], p.Type.Nullable, p.Name.Source)
	}
}

// TestUnion_TwoUnionsSharingNoMemberBesideAScalarLowerAsTheBody pins a oneOf
// and an anyOf whose enums share no member beside a string body, beside its
// keywords, beside an allOf of it, on one entry and on two: each is the
// string keeping both unions, with one warning that they share no value.
func TestUnion_TwoUnionsSharingNoMemberBesideAScalarLowerAsTheBody(t *testing.T) {
	t.Parallel()
	const one, anyOf = "oneOf: [{type: string, enum: [a]}]", "anyOf: [{type: string, enum: [b]}]"
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Flat: {type: string, `+one+`, `+anyOf+`}
    Beside: {allOf: [{type: string}], `+one+`, `+anyOf+`}
    OneEntry: {allOf: [{type: string}, {`+one+`, `+anyOf+`}]}
    TwoEntries: {allOf: [{type: string}, {`+one+`}, {`+anyOf+`}]}
`))
	for _, name := range []string{"Flat", "Beside", "OneEntry", "TwoEntries"} {
		scalar, ok := typeByName(doc, name).(*ir.Scalar)
		require.True(t, ok, "%s is the string its body gives", name)
		assert.Contains(t, scalar.Unmodeled, "openapi:oneOf", name)
		assert.Contains(t, scalar.Unmodeled, "openapi:anyOf", name)
		var warned []string
		for _, d := range diags {
			if d.Severity == ir.SeverityWarning && strings.HasSuffix(string(d.Provenance.Pointer), "/"+name) {
				warned = append(warned, d.Message)
			}
		}
		require.Len(t, warned, 1, name)
		assert.Contains(t, warned[0], "share no value", name)
	}
}

// TestUnion_AnElectedUnionOfAPairIsReportedAsTheUnionItLowersTo pins the report
// of an anyOf kept on the second entry of an allOf whose first elects a oneOf
// of a string and null: the position is the Union of the oneOf, not collapsed,
// as one schema writing both is, and the report names that Union, never a
// nullable reference to the oneOf's one non-null branch.
func TestUnion_AnElectedUnionOfAPairIsReportedAsTheUnionItLowersTo(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Two: {allOf: [{oneOf: [{type: string}, {type: 'null'}]}, {anyOf: [{type: string}]}]}
`))
	_, ok := typeByName(doc, "Two").(*ir.Union)
	require.True(t, ok, "the position is the Union of its oneOf")
	var reported []string
	for _, d := range diags {
		if d.Provenance.Pointer == "/components/schemas/Two/allOf/1" {
			reported = append(reported, d.Message)
		}
	}
	require.Len(t, reported, 1)
	assert.Contains(t, reported[0], "lowered to the Union of its oneOf")
	assert.NotContains(t, reported[0], "nullable reference")
}

// TestUnion_ANullBranchOfAPairIsDeadWhereTheOtherKeepsNullOut pins a branch of
// the anyOf an object or null elects that is a $ref to null alone: no variant
// where the oneOf
// passed over keeps null out, kept with the warning, an inline null branch
// beside it lifted as ever; a variant where the oneOf admits null.
func TestUnion_ANullBranchOfAPairIsDeadWhereTheOtherKeepsNullOut(t *testing.T) {
	t.Parallel()
	const objp = "{type: object, properties: {x: {type: string}}}"
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Null: {type: 'null'}
    Dead: {type: [object, 'null'], oneOf: [{type: object}], anyOf: [`+objp+`, {$ref: '#/components/schemas/Null'}]}
    Both: {type: [object, 'null'], oneOf: [{type: object}], anyOf: [`+objp+`, {type: 'null'}, {$ref: '#/components/schemas/Null'}]}
    Open: {type: [object, 'null'], oneOf: [{type: [object, 'null']}], anyOf: [`+objp+`, {$ref: '#/components/schemas/Null'}]}
`))
	for name, variants := range map[string]int{"Dead": 1, "Both": 1, "Open": 2} {
		union, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, name)
		assert.Len(t, union.Variants, variants, name)
		_, kept := union.Unmodeled["openapi:anyOf/"+map[string]string{"Dead": "1", "Both": "2", "Open": "1"}[name]]
		assert.Equal(t, variants == 1, kept, name)
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/Dead"))
}

// TestUnion_APrincipalEntryWhoseUnionSharesNoKindWithItsTypeWarnsOnce holds
// that an allOf entry writing a type and a oneOf sharing no kind, standing for
// the position alone or beside an annotation, draws the one warning that the
// conjuncts share no value at the position, not a second at the entry.
func TestUnion_APrincipalEntryWhoseUnionSharesNoKindWithItsTypeWarnsOnce(t *testing.T) {
	t.Parallel()
	_, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    Alone: {allOf: [{type: integer, oneOf: [{type: string}, {type: boolean}]}]}
    Annotated: {allOf: [{description: d}, {type: integer, oneOf: [{type: string}, {type: boolean}]}]}
`))
	warned := map[string]int{}
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning {
			warned[strings.Split(string(d.Provenance.Pointer), "/")[3]]++
		}
	}
	assert.Equal(t, map[string]int{"Alone": 1, "Annotated": 1}, warned)
}

// TestUnion_APairOnTwoEntriesReadsAUnionBranchsNull pins the reference to a
// oneOf and an anyOf on two allOf entries whose oneOf's one branch is a union,
// inline or behind a $ref, admitting no null: not nullable, as on one schema,
// each union's null being its branches'.
func TestUnion_APairOnTwoEntriesReadsAUnionBranchsNull(t *testing.T) {
	t.Parallel()
	const anyOf = "{anyOf: [{type: 'null'}, {type: integer}]}"
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    U: {oneOf: [{type: string}, {type: integer}]}
    Flat: {oneOf: [{$ref: '#/components/schemas/U'}], anyOf: [{type: 'null'}, {type: integer}]}
    Ref: {allOf: [{oneOf: [{$ref: '#/components/schemas/U'}]}, `+anyOf+`]}
    Inline: {allOf: [`+anyOf+`, {oneOf: [{oneOf: [{type: string}, {type: integer}]}]}]}
    C: {type: object, properties: {f: {$ref: '#/components/schemas/Flat'}, r: {$ref: '#/components/schemas/Ref'}, i: {$ref: '#/components/schemas/Inline'}}}
`))
	model, ok := typeByName(doc, "C").(*ir.Model)
	require.True(t, ok)
	require.Len(t, model.Properties, 3)
	for _, p := range model.Properties {
		assert.False(t, p.Type.Nullable, p.Name.Source)
	}
}

// TestUnion_APairElectingAValueSetOfNullAloneWarnsWhereItSharesNoValue holds
// that a oneOf of null alone written as a value set, elected over an anyOf
// sharing no value with it, draws the one warning that the conjuncts share no
// value on one schema, on entries of an allOf and beside an allOf of an object
// body, and that two sharing the null draw none.
func TestUnion_APairElectingAValueSetOfNullAloneWarnsWhereItSharesNoValue(t *testing.T) {
	t.Parallel()
	_, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    Flat: {oneOf: [{enum: [null]}], anyOf: [{type: string}]}
    Const: {oneOf: [{const: null}], anyOf: [{type: integer}]}
    Entries: {allOf: [{oneOf: [{enum: [null]}]}, {anyOf: [{type: string}]}]}
    BesideObject: {allOf: [{type: object}], oneOf: [{const: null}], anyOf: [{type: integer}]}
    Shared: {oneOf: [{enum: [null]}], anyOf: [{type: 'null'}]}
`))
	warned := map[string][]string{}
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning {
			name := string(d.Provenance.Pointer)[strings.LastIndexByte(string(d.Provenance.Pointer), '/')+1:]
			warned[name] = append(warned[name], d.Message)
		}
	}
	for _, name := range []string{"Flat", "Const", "Entries", "BesideObject"} {
		require.Len(t, warned[name], 1, name)
		assert.Contains(t, warned[name][0], "share no value", name)
	}
	assert.Empty(t, warned["Shared"])
}

// TestUnion_TwoUnionsSharingNullAloneBesideAnInlineBodyAdmittingNullAreKept
// pins a oneOf of an object or null and an anyOf of a string or null, which
// share null alone, beside an inline string or null: beside the allOf, on one
// entry and on two, a use of the body keeping both unions, as beside a $ref
// to such a body, distributing neither; with no body, the oneOf's Union.
func TestUnion_TwoUnionsSharingNullAloneBesideAnInlineBodyAdmittingNullAreKept(t *testing.T) {
	t.Parallel()
	const one, anyOf = "oneOf: [{type: object, properties: {x: {type: string}}}, {type: 'null'}]", "anyOf: [{type: string}, {type: 'null'}]"
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    Beside: {allOf: [{type: [string, 'null']}], `+one+`, `+anyOf+`}
    OneEntry: {allOf: [{type: [string, 'null']}, {`+one+`, `+anyOf+`}]}
    TwoEntries: {allOf: [{type: [string, 'null']}, {`+one+`}, {`+anyOf+`}]}
    NoBody: {allOf: [{`+one+`}, {`+anyOf+`}]}
`))
	for _, name := range []string{"Beside", "OneEntry", "TwoEntries"} {
		scalar, ok := typeByName(doc, name).(*ir.Scalar)
		require.True(t, ok, "%s is a use of the body", name)
		assert.Contains(t, scalar.Unmodeled, "openapi:oneOf", name)
		assert.Contains(t, scalar.Unmodeled, "openapi:anyOf", name)
	}
	_, ok := typeByName(doc, "NoBody").(*ir.Union)
	assert.True(t, ok, "with no body to use the oneOf is elected")
}

// TestUnion_AShapelessUnionSharingNoKindWithAScalarWarnsBesideItsKeywords pins
// a oneOf of null alone beside a string's own keywords: the string keeping the
// oneOf, with the one warning that the conjuncts share no value, as beside an
// allOf of the string; beside an object's keywords it draws none, as pinned
// where a branch restates the object's properties.
func TestUnion_AShapelessUnionSharingNoKindWithAScalarWarnsBesideItsKeywords(t *testing.T) {
	t.Parallel()
	_, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Flat: {type: string, oneOf: [{type: 'null'}]}
    Beside: {allOf: [{type: string}], oneOf: [{type: 'null'}]}
`))
	warned := map[string]int{}
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning {
			warned[strings.Split(string(d.Provenance.Pointer), "/")[3]]++
		}
	}
	assert.Equal(t, map[string]int{"Flat": 1, "Beside": 1}, warned)
}

// TestUnion_TwoUnionsApartAreNamedTheOneOfFirst pins the order the warning
// that a oneOf and an anyOf share no value names them in: the keywords beside
// the allOf before its entries, the oneOf before the anyOf, a union admitting
// nothing alone named so.
func TestUnion_TwoUnionsApartAreNamedTheOneOfFirst(t *testing.T) {
	t.Parallel()
	const a, b = "[{type: string, enum: [a]}]", "[{type: string, enum: [b]}]"
	_, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Flat: {anyOf: `+b+`, oneOf: `+a+`}
    Mixed: {allOf: [{anyOf: `+b+`}], oneOf: `+a+`}
    Entries: {allOf: [{anyOf: `+b+`}, {oneOf: `+a+`}]}
    False: {anyOf: [{type: integer}], oneOf: [false]}
`))
	want := map[string]string{
		"Flat":    "(oneOf: 1 value, anyOf: 1 value)",
		"Mixed":   "(oneOf: 1 value, allOf/0/anyOf: 1 value)",
		"Entries": "(allOf/0/anyOf: 1 value, allOf/1/oneOf: 1 value)",
		"False":   "(oneOf: no value, anyOf: integer)",
	}
	for name, names := range want {
		var warned []string
		for _, d := range diags {
			if d.Severity == ir.SeverityWarning && strings.HasSuffix(string(d.Provenance.Pointer), "/"+name) {
				warned = append(warned, d.Message)
			}
		}
		require.Len(t, warned, 1, name)
		assert.Contains(t, warned[0], names, name)
	}
}
