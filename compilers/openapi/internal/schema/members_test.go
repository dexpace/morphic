// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// composedDoc compiles schemas, components of a 3.1 document, and requires the
// result to verify.
func composedDoc(t *testing.T, schemas string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", schemas))
	openapitest.RequireNoErrorDiags(t, diags)
	require.Empty(t, irverify.Verify(doc))
	return doc, diags
}

// diagsUnder returns the diagnostics of diags at or beneath prefix.
func diagsUnder(diags []ir.Diagnostic, prefix string) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, diagnostic := range diags {
		if strings.HasPrefix(string(diagnostic.Provenance.Pointer), prefix) {
			out = append(out, diagnostic)
		}
	}
	return out
}

// TestMembers_TwoEntriesDeclaringAMemberApartDeclareItOnTheComposition pins a
// member two allOf entries declare apart: the composing model declares it, in
// the composed namespace under one identity in either entry order, and a pair
// no value satisfies keeps the entry written later, the specialization, with a
// report where the declaration that lost is written, naming the property it
// decided (GitHub #1920).
func TestMembers_TwoEntriesDeclaringAMemberApartDeclareItOnTheComposition(t *testing.T) {
	t.Parallel()
	const parts = "    A: {type: object, properties: {p: {type: string}}}\n" +
		"    B: {type: object, properties: {p: {type: integer}}}\n"
	const id = "property/composed/components/schemas/AB/allOf/properties/p"
	for entries, tc := range map[string]struct {
		later ir.TypeID
		lost  string
	}{
		"[{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]": {"type/primitive/integer", "/components/schemas/A/properties/p"},
		"[{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/A'}]": {"type/primitive/string", "/components/schemas/B/properties/p"},
	} {
		doc, diags := composedDoc(t, parts+"    AB: {allOf: "+entries+"}\n")

		property := propertyOf(t, doc, "AB", "p")
		assert.Equal(t, ir.PropertyID(id), property.ID, entries)
		assert.Equal(t, tc.later, property.Type.Target, "the entry written later: %s", entries)
		assert.Empty(t, property.Unmodeled, "the declaration that lost stays on its model: %s", entries)
		flat := ir.FlattenedProperties(doc.Types, componentID("AB"))
		require.Len(t, flat.Properties, 1)
		assert.Equal(t, property.ID, flat.Properties[0].Property.ID, "the flattening takes the composition's own")
		require.Len(t, diags, 1, "%+v", diags)
		found := diagsAtPointer(diags, diag.ConflictingRedecl, tc.lost)
		require.Len(t, found, 1, entries)
		assert.Equal(t, id, found[0].Provenance.Node, entries)
	}
}

// TestMembers_ARefAndTheSchemaItNamesWrittenInlineFoldAlike pins a member a
// model declares itself beside one a $ref entry brings (GitHub #1951): in
// either entry order, the field takes the type, docs and bounds it takes where
// the named schema's declaration is written inline in the entry's place, the
// narrower type wherever it is written and, of a pair no value satisfies, the
// later. A report about the entry's declaration names the member, and the
// property keeps the identity of the declaration the model writes.
func TestMembers_ARefAndTheSchemaItNamesWrittenInlineFoldAlike(t *testing.T) {
	t.Parallel()
	const base = "{type: integer, description: from base}"
	for _, own := range []string{"{type: number}", "{type: string}", "{type: integer, minimum: 1}", "{description: own}"} {
		for _, refFirst := range []bool{true, false} {
			ref, inline := "{$ref: '#/components/schemas/Base'}", "{type: object, properties: {id: "+base+"}}"
			mine := "{type: object, properties: {id: " + own + "}}"
			entries := func(brought string) string {
				if refFirst {
					return "[" + brought + ", " + mine + "]"
				}
				return "[" + mine + ", " + brought + "]"
			}
			name := fmt.Sprintf("%s, base first %v", own, refFirst)
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Base: {type: object, properties: {id: "+base+"}}\n"+
				"    Ref: {allOf: "+entries(ref)+"}\n    Inline: {allOf: "+entries(inline)+"}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			require.Empty(t, irverify.Verify(doc))

			byRef, written := propertyOf(t, doc, "Ref", "id"), propertyOf(t, doc, "Inline", "id")
			mineAt := map[bool]string{true: "1", false: "0"}[refFirst]
			assert.Equal(t, ir.PropertyID("property/openapi/components/schemas/Ref/allOf/"+mineAt+"/properties/id"), byRef.ID, name)
			assert.Equal(t, ids.Ptr("components", "schemas", "Ref", "allOf", mineAt, "properties", "id"),
				byRef.Provenance.Pointer, name)
			assert.Equal(t, written.Type, byRef.Type, name)
			assert.Equal(t, written.Docs, byRef.Docs, name)
			assert.Equal(t, written.Constraints, byRef.Constraints, name)
			atRef := 0
			for _, diagnostic := range diags {
				if diagnostic.Provenance.Node == string(byRef.ID) {
					atRef++
				}
			}
			assert.Equal(t, len(diagsUnder(diags, "/components/schemas/Inline/")), atRef, "%s: as many reports: %+v", name, diags)
		}
	}
}

// TestMembers_ATieAnEntrySettledIsNotCountedAgain pins where a $ref entry and
// the schema it names written inline fold apart: the entry's schema declares
// the member as a string and then an integer, a tie its settlement gives the
// later. Beside one more string, the entry brings the integer alone, which
// ties again and stands as the later, while the inline spelling counts two
// strings against one integer. Each spelling reports its tie.
func TestMembers_ATieAnEntrySettledIsNotCountedAgain(t *testing.T) {
	t.Parallel()
	holding := func(kind string) string { return "{type: object, properties: {p: {type: " + kind + "}}}" }
	part := "{allOf: [" + holding("string") + ", " + holding("integer") + "]}"
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Part: "+part+"\n"+
		"    Ref: {allOf: ["+holding("string")+", {$ref: '#/components/schemas/Part'}]}\n"+
		"    Inline: {allOf: ["+holding("string")+", "+part+"]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Equal(t, ir.TypeID("type/primitive/integer"), propertyOf(t, doc, "Ref", "p").Type.Target)
	assert.Equal(t, ir.TypeID("type/primitive/string"), propertyOf(t, doc, "Inline", "p").Type.Target)
	for _, model := range []string{"Ref", "Inline"} {
		at := diagsUnder(diags, "/components/schemas/"+model+"/")
		require.Len(t, at, 1, model)
		assert.Equal(t, diag.ConflictingRedecl, at[0].Code, model)
	}
}

// TestMembers_AMemberAPartSettledIsJudgedWhereItsTypeIsWritten pins a member
// two entries each bring settled. Each part declares it as a string holding a
// bound and as an enum, so its member keeps the first declaration's identity
// and the enum the other hoisted. The two enums are copies of one declaration,
// each written at a declaration of its part, so the member takes one and both
// bounds, as the four declarations written inline do, whichever each part
// writes first.
func TestMembers_AMemberAPartSettledIsJudgedWhereItsTypeIsWritten(t *testing.T) {
	t.Parallel()
	const enum = "{type: object, properties: {b: {type: string, enum: [x, y, z]}}}"
	bound := func(keyword string) string { return "{type: object, properties: {b: {type: string, " + keyword + "}}}" }
	for _, boundFirst := range []bool{true, false} {
		part := func(keyword string) string {
			if boundFirst {
				return "{allOf: [" + bound(keyword) + ", " + enum + "]}"
			}
			return "{allOf: [" + enum + ", " + bound(keyword) + "]}"
		}
		name := fmt.Sprintf("bound first %v", boundFirst)
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
			"    S1: "+part("minLength: 1")+"\n    S2: "+part("maxLength: 9")+"\n"+
				"    Ref: {allOf: [{$ref: '#/components/schemas/S1'}, {$ref: '#/components/schemas/S2'}]}\n"+
				"    Inline: {allOf: ["+part("minLength: 1")+", "+part("maxLength: 9")+"]}\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		require.Empty(t, irverify.Verify(doc))

		byRef, written := propertyOf(t, doc, "Ref", "b"), propertyOf(t, doc, "Inline", "b")
		one, nine := int64(1), int64(9)
		assert.Equal(t, &ir.Constraints{MinLength: &one, MaxLength: &nine}, byRef.Constraints, name)
		assert.Equal(t, written.Constraints, byRef.Constraints, name)
		assert.IsType(t, &ir.Enum{}, doc.Types[byRef.Type.Target], name)
		assert.Empty(t, diagsUnder(diags, "/components/schemas/Ref/"), name)
		assert.Empty(t, diagsUnder(diags, "/components/schemas/Inline/"), name)
	}
}

// TestMembers_AMemberAPartMeetsIsJudgedAtEachDeclarationItHolds pins a member
// an entry brings met, in either entry order. The part declares it as a list
// of strings, then of an enum, each holding a bound, so its member is a list
// the fold built, whose element the second declaration hoisted. Folded with
// another entry's list of that enum, the element reads as written at the
// part's declaration: the lists meet in the tightest bounds, and the other
// entry's default, listing a value the enum lacks, is its own type's to answer
// for, as written inline.
func TestMembers_AMemberAPartMeetsIsJudgedAtEachDeclarationItHolds(t *testing.T) {
	t.Parallel()
	list := func(rest string) string { return "{type: object, properties: {b: {type: array, " + rest + "}}}" }
	const enum = "items: {type: string, enum: [x, y]}"
	part := "{allOf: [" + list("minItems: 3, items: {type: string}") + ", " + list("maxItems: 9, "+enum) + "]}"
	leaf := list("maxItems: 5, default: [q, q, q], " + enum)
	for _, leafFirst := range []bool{true, false} {
		entries := func(a, b string) string {
			if leafFirst {
				return "[" + a + ", " + b + "]"
			}
			return "[" + b + ", " + a + "]"
		}
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S1: "+part+"\n    S2: "+leaf+"\n"+
			"    Ref: {allOf: "+entries("{$ref: '#/components/schemas/S2'}", "{$ref: '#/components/schemas/S1'}")+"}\n"+
			"    Inline: {allOf: "+entries(leaf, part)+"}\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		require.Empty(t, irverify.Verify(doc))

		three, five := int64(3), int64(5)
		for _, model := range []string{"Ref", "Inline"} {
			name := fmt.Sprintf("%s, leaf first %v", model, leafFirst)
			b := propertyOf(t, doc, model, "b")
			met, isList := doc.Types[b.Type.Target].(*ir.List)
			require.True(t, isList, name)
			assert.Equal(t, &ir.Constraints{MinItems: &three, MaxItems: &five}, met.Constraints, name)
			require.NotNil(t, b.Default, name)
			assert.Len(t, b.Default.List, 3, name)
			assert.Empty(t, diagsUnder(diags, "/components/schemas/"+model+"/"), name)
		}
	}
}

// TestMembers_AMemberOnlyEntriesDeclareIsJudgedWhereItsTypeIsWritten pins a
// member two entries each bring settled from their own entries, so each names
// a place no schema is written at. Where each keeps the enum one of its
// entries declares, and where each holds the enum its fold built of two, the
// two are copies of one declaration, so the member takes one and what both
// say beside it, with nothing reported.
func TestMembers_AMemberOnlyEntriesDeclareIsJudgedWhereItsTypeIsWritten(t *testing.T) {
	t.Parallel()
	holding := func(schema string) string { return "{type: object, properties: {b: " + schema + "}}" }
	composing := func(a, b string) string {
		return "{allOf: [{$ref: '#/components/schemas/" + a + "'}, {$ref: '#/components/schemas/" + b + "'}]}"
	}
	one, nine := int64(1), int64(9)
	for name, tc := range map[string]struct {
		first, second [2]string
		members       int
		want          *ir.Constraints
	}{
		"an enum an entry declares": {
			first:   [2]string{"{type: string, enum: [x, y]}", "{type: string, minLength: 1}"},
			second:  [2]string{"{type: string, enum: [x, y]}", "{type: string, maxLength: 9}"},
			members: 2, want: &ir.Constraints{MinLength: &one, MaxLength: &nine},
		},
		"an enum the fold built": {
			first:   [2]string{"{type: string, enum: [x, y]}", "{type: string, enum: [y, z]}"},
			second:  [2]string{"{type: string, enum: [y, z]}", "{type: string, enum: [x, y]}"},
			members: 1,
		},
	} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
			"    A1: "+holding(tc.first[0])+"\n    B1: "+holding(tc.first[1])+"\n    One: "+composing("A1", "B1")+"\n"+
				"    A2: "+holding(tc.second[0])+"\n    B2: "+holding(tc.second[1])+"\n    Two: "+composing("A2", "B2")+"\n"+
				"    Ref: "+composing("One", "Two")+"\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		require.Empty(t, irverify.Verify(doc), name)

		b := propertyOf(t, doc, "Ref", "b")
		enum, isEnum := doc.Types[b.Type.Target].(*ir.Enum)
		require.True(t, isEnum, name)
		assert.Len(t, enum.Members, tc.members, name)
		assert.Equal(t, tc.want, b.Constraints, name)
		assert.Empty(t, diagsUnder(diags, "/components/schemas/Ref/"), name)
	}
}

// TestMembers_ABroughtMembersDefaultIsItsOwnTypesWhereThatIsWritten pins the
// default a settled member an entry brings holds, written against its own
// type: the part declares a list of strings and then one of an enum, giving
// the default, beside another entry's list of that enum, whose copy the built
// list takes. The default lists a value the enum lacks, which its own
// declaration answers for, so the member keeps it in either entry order, as
// the part's own member does.
func TestMembers_ABroughtMembersDefaultIsItsOwnTypesWhereThatIsWritten(t *testing.T) {
	t.Parallel()
	list := func(rest, items string) string {
		return "{type: object, properties: {b: {type: array, " + rest + ", items: {type: string" + items + "}}}}"
	}
	const enum = ", enum: [x, y]"
	for _, order := range [][2]string{{"A", "S"}, {"S", "A"}} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
			"    A: "+list("maxItems: 5", enum)+"\n"+
				"    S: {allOf: ["+list("minItems: 3", "")+", "+list("maxItems: 9, default: [q, q, q]", enum)+"]}\n"+
				"    Ref: {allOf: [{$ref: '#/components/schemas/"+order[0]+"'}, {$ref: '#/components/schemas/"+order[1]+"'}]}\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		require.Empty(t, irverify.Verify(doc))

		b := propertyOf(t, doc, "Ref", "b")
		require.NotNil(t, b.Default, "%v", order)
		assert.Equal(t, propertyOf(t, doc, "S", "b").Default, b.Default, "%v", order)
		assert.Empty(t, diagsUnder(diags, "/components/schemas/Ref/"), "%v", order)
	}
}

// TestMembers_AReportNamesABroughtMemberWhereItSaysWhatItDoes pins what a
// report says of a member an entry brings settled. Where one declaration gave
// its type, the report names that declaration, not the first the member is
// named for, which says another type. Where its model's composition built the
// type of what only its entries declare, no schema is written where the model
// declares it, so the report names the entry bringing it. Either way the
// report sits where it names.
func TestMembers_AReportNamesABroughtMemberWhereItSaysWhatItDoes(t *testing.T) {
	t.Parallel()
	holding := func(schema string) string { return "{type: object, properties: {p: " + schema + "}}" }
	const composing = "{allOf: [{$ref: '#/components/schemas/M'}, {$ref: '#/components/schemas/C'}]}"
	for name, tc := range map[string]struct{ part, over string }{
		"the declaration giving its type": {
			part: "{allOf: [" + holding("{type: string}") + ", " + holding("{type: integer}") + "]}",
			over: "/components/schemas/M/allOf/1/properties/p",
		},
		"the entry bringing what is written nowhere": {
			part: "{allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}",
			over: "/components/schemas/N/allOf/0",
		},
	} {
		_, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
			"    A: "+holding("{type: string, enum: [x, y]}")+"\n    B: "+holding("{type: string, enum: [y, z]}")+"\n"+
				"    M: "+tc.part+"\n    C: "+holding("{type: boolean}")+"\n    N: "+composing+"\n"))

		at := diagsAtPointer(diags, diag.ConflictingRedecl, tc.over)
		require.Len(t, at, 1, name)
		assert.Contains(t, at[0].Message, "kept what the declaration at /components/schemas/C/properties/p says over the one at "+
			tc.over+", which stays declared on its model", name)
	}
}

// TestMembers_ASettledMembersInlineTypeIsNoBaseOfWhatComposesIt pins the model
// a fold composes of a member one entry brings settled: the part declares the
// member with no type and then as an inline object, so the member keeps the
// first declaration's identity and the model the second hoisted. No $ref names
// that model, so the composition lists it among its Mixins, as it lists the
// other entry's, and elects no Base (SelectBase).
func TestMembers_ASettledMembersInlineTypeIsNoBaseOfWhatComposesIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    S1: {allOf: [{type: object, properties: {a: {description: about a}}}, "+
			"{type: object, properties: {a: {type: object, properties: {x: {type: string}}}}}]}\n"+
			"    S2: {type: object, properties: {a: {type: object, properties: {y: {type: string}}}}}\n"+
			"    Ref: {allOf: [{$ref: '#/components/schemas/S1'}, {$ref: '#/components/schemas/S2'}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	require.Empty(t, irverify.Verify(doc))

	composed, isModel := doc.Types[propertyOf(t, doc, "Ref", "a").Type.Target].(*ir.Model)
	require.True(t, isModel, "two inline objects compose")
	assert.Nil(t, composed.Base, "no $ref names either object")
	assert.Equal(t, []ir.TypeRef{
		{Target: "type/anonymous/components/schemas/S1/allOf/1/properties/a"},
		{Target: "type/anonymous/components/schemas/S2/properties/a"},
	}, composed.Mixins)
}

// TestMembers_RequiresAndTheDiscriminatorFollowTheDeclaredMember pins what
// names a member a composition settles: a composition requiring one it does
// not declare requires the member it declares, its name leaving Requires; a
// model declaring the member itself keeps that declaration's identity; and one
// whose declaration another model holds too, through a $ref to the allOf
// branch writing it, takes one in the composed namespace, which a
// discriminator naming that declaration follows. A discriminator naming a
// member only entries declare names it by its ID once the model declares it,
// as for a member the model writes (docs/compilers/openapi.md#discriminator).
func TestMembers_RequiresAndTheDiscriminatorFollowTheDeclaredMember(t *testing.T) {
	t.Parallel()
	doc, _ := composedDoc(t, `    A: {type: object, properties: {kind: {type: string}}}
    B: {type: object, properties: {kind: {type: string, maxLength: 9}}}
    Required:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
      required: [kind]
    Tagged:
      allOf: [{$ref: '#/components/schemas/B'}]
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind}
    Shared:
      allOf: [{$ref: '#/components/schemas/B'}, {type: object, properties: {kind: {type: string}}}]
      discriminator: {propertyName: kind}
    Branch: {$ref: '#/components/schemas/Shared/allOf/1'}
    Brought:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
      discriminator: {propertyName: kind}
`)
	required, ok := typeByName(doc, "Required").(*ir.Model)
	require.True(t, ok)
	assert.True(t, propertyOf(t, doc, "Required", "kind").Required)
	assert.Empty(t, required.Requires)

	for name, want := range map[string]ir.PropertyID{
		"Tagged":  "property/openapi/components/schemas/Tagged/properties/kind",
		"Shared":  "property/composed/components/schemas/Shared/allOf/properties/kind",
		"Brought": "property/composed/components/schemas/Brought/allOf/properties/kind",
	} {
		tagged, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, name)
		kind := propertyOf(t, doc, name, "kind")
		assert.Equal(t, want, kind.ID, name)
		assert.Equal(t, ir.TypeID("type/primitive/string"), kind.Type.Target, name)
		assert.Equal(t, new(int64(9)), kind.Constraints.MaxLength, "%s: B's bound", name)
		require.NotNil(t, tagged.Discriminator, name)
		assert.Equal(t, kind.ID, tagged.Discriminator.Property, name)
		assert.Empty(t, tagged.Discriminator.PropertyName, "%s: one locator", name)
	}
}

// TestMembers_AModelIsSettledAfterWhatItComposes pins a model composing one
// whose member a fold settles: it reads that member settled, through an
// alias entry too, whichever is declared first, so Mid's integer, not its own
// number, is what Top's string meets, each conflict sitting where that integer
// is written.
func TestMembers_AModelIsSettledAfterWhatItComposes(t *testing.T) {
	t.Parallel()
	const base = "    Base: {type: object, properties: {id: {type: integer}}}\n"
	const mid = "    Mid:\n      allOf: [{$ref: '#/components/schemas/Base'}, {type: object, properties: {id: {type: number}}}]\n"
	const tops = "    Top:\n      allOf: [{$ref: '#/components/schemas/Mid'}, {type: object, properties: {id: {type: string}}}]\n" +
		"    Aliased:\n      allOf: [{$ref: '#/components/schemas/Mid', description: d}, {type: object, properties: {id: {type: string}}}]\n"
	for _, order := range []string{base + mid + tops, tops + mid + base} {
		_, diags := composedDoc(t, order)
		found := diagsAtPointer(diags, diag.ConflictingRedecl, "/components/schemas/Base/properties/id")
		require.Len(t, found, 2, "%+v", diags)
		for i, model := range []string{"Aliased", "Top"} {
			assert.Contains(t, found[i].Message, "type/primitive/integer", model)
			assert.Contains(t, found[i].Message, "kept what the declaration at /components/schemas/"+model+"/allOf/1/properties/id", model)
		}
	}
}

// TestMembers_ACycleSettlesTheSameInEitherOrder pins two models composing
// each other, each declaring a member the other does: one IR whichever is
// declared first.
func TestMembers_ACycleSettlesTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	const a = "    A:\n      allOf: [{$ref: '#/components/schemas/B'}, {type: object, properties: {x: {type: string}}}]\n"
	const b = "    B:\n      allOf: [{$ref: '#/components/schemas/A'}, {type: object, properties: {x: {type: integer}}}]\n"
	first, firstDiags := composedDoc(t, a+b)
	later, laterDiags := composedDoc(t, b+a)
	assert.Empty(t, cmp.Diff(first.Types, later.Types, orderInvariantIR()...))
	assert.Equal(t, reportedAt(firstDiags), reportedAt(laterDiags))
}

// TestMembers_AModelAFoldComposesIsSettledInTheNextRound pins a fold
// whose member two unrelated models declare: the fold composes them, as the
// entries are written, into a model of its own, whose member they declare apart
// is settled in turn, the later entry's again, with the report where the one
// that lost is written.
func TestMembers_AModelAFoldComposesIsSettledInTheNextRound(t *testing.T) {
	t.Parallel()
	const parts = `    A1: {type: object, properties: {r: {type: string}}}
    B1: {type: object, properties: {r: {type: integer}}}
    A: {type: object, properties: {q: {$ref: '#/components/schemas/A1'}}}
    B: {type: object, properties: {q: {$ref: '#/components/schemas/B1'}}}
`
	for entries, tc := range map[string]struct {
		later ir.TypeID
		lost  string
	}{
		"[{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]": {"type/primitive/integer", "/components/schemas/A1/properties/r"},
		"[{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/A'}]": {"type/primitive/string", "/components/schemas/B1/properties/r"},
	} {
		doc, diags := composedDoc(t, parts+"    M: {allOf: "+entries+"}\n")

		property := propertyOf(t, doc, "M", "q")
		composed, ok := doc.Types[property.Type.Target].(*ir.Model)
		require.True(t, ok, "got %T", doc.Types[property.Type.Target])
		require.Len(t, composed.Properties, 1, "its own r")
		assert.Equal(t, tc.later, composed.Properties[0].Type.Target, entries)
		assert.Len(t, diagsAtPointer(diags, diag.ConflictingRedecl, tc.lost), 1, "%s: %+v", entries, diags)
	}
}

// TestMembers_CompositionsNestedPastTheRoundsAreReported pins the rounds the
// settlement reads: a chain of folds each composing a model whose member is
// folded again settles a round a level, and one more for the member the
// deepest two declare apart. Seven levels take the eight rounds, so that
// member's tie is reported; eight leave the model composing it unsettled,
// reported once, and its tie unreported.
func TestMembers_CompositionsNestedPastTheRoundsAreReported(t *testing.T) {
	t.Parallel()
	for levels, past := range map[int]int{7: 0, 8: 1} {
		var spec strings.Builder
		for i := 1; i <= levels; i++ {
			fmt.Fprintf(&spec, "    A%d: {type: object, properties: {q: {$ref: '#/components/schemas/A%d'}}}\n", i, i+1)
			fmt.Fprintf(&spec, "    B%d: {type: object, properties: {q: {$ref: '#/components/schemas/B%d'}}}\n", i, i+1)
		}
		fmt.Fprintf(&spec, "    A%d: {type: object, properties: {r: {type: string}}}\n", levels+1)
		fmt.Fprintf(&spec, "    B%d: {type: object, properties: {r: {type: integer}}}\n", levels+1)
		spec.WriteString("    M:\n      allOf: [{$ref: '#/components/schemas/A1'}, {$ref: '#/components/schemas/B1'}]\n")
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", spec.String()))
		require.Empty(t, irverify.Verify(doc))
		assert.Equal(t, past, openapitest.CountDiagsAt(diags, diag.BudgetExceeded, ir.SeverityError), "%d levels: %+v", levels, diags)
		assert.Equal(t, 1-past, openapitest.CountDiagsAt(diags, diag.ConflictingRedecl, ir.SeverityWarning), "%d levels: %+v", levels, diags)
	}
}

// TestMembers_AModelSettlesOnceHoweverManyRoundsReachIt pins a model settled
// in one round and composed by a model a fold builds, which the next round
// settles: the first declares a member only its two entries declare, which a
// second settlement of it would declare again, so it holds that member once.
func TestMembers_AModelSettlesOnceHoweverManyRoundsReachIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    X: {type: object, properties: {r: {type: string}}}\n    Y: {type: object, properties: {r: {type: string, maxLength: 9}}}\n"+
			"    A1: {allOf: [{$ref: '#/components/schemas/X'}, {$ref: '#/components/schemas/Y'}]}\n"+
			"    B1: {type: object, properties: {r: {type: integer}}}\n"+
			"    A: {type: object, properties: {q: {$ref: '#/components/schemas/A1'}}}\n"+
			"    B: {type: object, properties: {q: {$ref: '#/components/schemas/B1'}}}\n"+
			"    M: {allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	require.Empty(t, irverify.Verify(doc))

	composed, isModel := doc.Types[propertyOf(t, doc, "M", "q").Type.Target].(*ir.Model)
	require.True(t, isModel, "the fold composes A1 and B1")
	require.Len(t, composed.Properties, 1, "settled in the round after")
	first, isModel := doc.Types["type/openapi/components/schemas/A1"].(*ir.Model)
	require.True(t, isModel)
	assert.Len(t, first.Properties, 1, "its member only entries declare, once")
}

// TestMembers_AHierarchyOfAnyDepthSettlesWhateverItsModelsAreNamed pins a
// hierarchy deeper than any bound on a walk's depth, each level redeclaring as
// a number the integer its base declares: every level settles after its base,
// so each member is the base's integer, whether the most derived model's name
// sorts first or last, since a name is presentation.
func TestMembers_AHierarchyOfAnyDepthSettlesWhateverItsModelsAreNamed(t *testing.T) {
	t.Parallel()
	const levels = 100
	for _, derivedFirst := range []bool{true, false} {
		name := func(level int) string {
			if derivedFirst {
				return fmt.Sprintf("L%03d", levels-level)
			}
			return fmt.Sprintf("L%03d", level)
		}
		var spec strings.Builder
		fmt.Fprintf(&spec, "    %s: {type: object, properties: {x: {type: integer}}}\n", name(0))
		for level := 1; level < levels; level++ {
			fmt.Fprintf(&spec, "    %s: {allOf: [{$ref: '#/components/schemas/%s'}, "+
				"{type: object, properties: {x: {type: number}}}]}\n", name(level), name(level-1))
		}
		doc, _ := composedDoc(t, spec.String())
		for level := range levels {
			property := propertyOf(t, doc, name(level), "x")
			assert.Equal(t, ir.TypeID("type/primitive/integer"), property.Type.Target, "level %d, derived first %v", level, derivedFirst)
		}
	}
}

// TestMembers_AUnionBoundedBesideItIsNotTakenForAVariant pins a member declared
// as a union holding a bound of its own and as a type within one of its
// variants: the bound bounds that type's values too, which the variants do not
// say, so the type is not taken as narrower, which would drop the bound
// unreported; the pair is reported as not compared, written inline or brought
// by an entry.
func TestMembers_AUnionBoundedBesideItIsNotTakenForAVariant(t *testing.T) {
	t.Parallel()
	_, diags := composedDoc(t, `    U: {anyOf: [{type: string}, {type: integer}], maxLength: 2}
    Base: {type: object, properties: {p: {$ref: '#/components/schemas/U'}}}
    Inline:
      allOf:
        - {type: object, properties: {p: {$ref: '#/components/schemas/U'}}}
        - {type: object, properties: {p: {type: string}}}
    Brought:
      allOf:
        - $ref: '#/components/schemas/Base'
        - {type: object, properties: {p: {type: string}}}
`)
	for _, at := range []string{"/components/schemas/Inline/allOf/0/properties/p", "/components/schemas/Brought/allOf/0"} {
		found := diagsAtPointer(diags, diag.DegradedConstruct, at)
		require.Len(t, found, 1, "%s: %+v", at, diags)
		assert.Contains(t, found[0].Message, "that the merge does not compare", at)
	}
}

// TestMembers_AModelTheMergeComposesNamesItsMembersApart pins the members a
// model the merge composes settles, a field's composition and a distributed
// variant, each at the pointer of the schema written there, which settles a
// member of that name too: the two are different properties, so each takes an
// identity of its own, and the document verifies.
func TestMembers_AModelTheMergeComposesNamesItsMembersApart(t *testing.T) {
	t.Parallel()
	const parts = `    X1: {type: object, properties: {y: {type: integer, enum: [1, 2]}}}
    X2: {type: object, properties: {y: {type: number}}}
    X3: {type: object, properties: {y: {type: integer}}}
`
	for name, tc := range map[string]struct{ schemas, at string }{
		"a field's composition": {schemas: parts + `    A: {type: object, properties: {x: {$ref: '#/components/schemas/X1'}}}
    M:
      allOf: [{$ref: '#/components/schemas/A'}]
      properties:
        x: {allOf: [{$ref: '#/components/schemas/X2'}, {$ref: '#/components/schemas/X3'}]}
`, at: "components/schemas/M/properties/x"},
		"a distributed variant": {schemas: parts + `    C: {type: object, properties: {w: {type: string}}}
    U:
      allOf: [{$ref: '#/components/schemas/X1'}]
      oneOf:
        - allOf: [{$ref: '#/components/schemas/X2'}, {$ref: '#/components/schemas/X3'}]
        - $ref: '#/components/schemas/C'
`, at: "components/schemas/U/oneOf/0"},
	} {
		doc, _ := composedDoc(t, tc.schemas)

		for space, id := range map[string]ir.PropertyID{
			"anonymous": ir.PropertyID("property/composed/" + tc.at + "/allOf/properties/y"),
			"composed":  ir.PropertyID("property/composed/" + tc.at + "/allOf/allOf/properties/y"),
		} {
			model, ok := doc.Types[ir.TypeID("type/"+space+"/"+tc.at)].(*ir.Model)
			require.True(t, ok, "%s: the %s model", name, space)
			property, ok := openapitest.PropsByWire(model.Properties)["y"]
			require.True(t, ok, "%s: the %s model settles y", name, space)
			assert.Equal(t, id, property.ID, "%s: the %s model", name, space)
		}
	}
}
