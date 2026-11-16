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
	for _, d := range diags {
		if strings.HasPrefix(string(d.Provenance.Pointer), prefix) {
			out = append(out, d)
		}
	}
	return out
}

// TestMembers_TwoEntriesDeclaringAMemberApartDeclareItOnTheComposition pins a
// member two allOf entries declare apart: the composing model declares it, in
// the composed namespace under one identity in either entry order, and a pair
// no value satisfies keeps the entry written later, the specialization, with a
// report at the entry that lost naming the property it decided (GitHub #1920).
func TestMembers_TwoEntriesDeclaringAMemberApartDeclareItOnTheComposition(t *testing.T) {
	t.Parallel()
	const parts = "    A: {type: object, properties: {p: {type: string}}}\n" +
		"    B: {type: object, properties: {p: {type: integer}}}\n"
	const id = "p/composed/components/schemas/AB/allOf/properties/p"
	for entries, later := range map[string]ir.TypeID{
		"[{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]": "t/prim/integer",
		"[{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/A'}]": "t/prim/string",
	} {
		doc, diags := composedDoc(t, parts+"    AB: {allOf: "+entries+"}\n")

		p := propertyOf(t, doc, "AB", "p")
		assert.Equal(t, ir.PropID(id), p.ID, entries)
		assert.Equal(t, later, p.Type.Target, "the entry written later: %s", entries)
		assert.Empty(t, p.Unmodeled, "the declaration that lost stays on its model: %s", entries)
		flat := ir.FlattenedProperties(doc.Types, componentID("AB"))
		require.Len(t, flat.Properties, 1)
		assert.Equal(t, p.ID, flat.Properties[0].Property.ID, "the flattening takes the composition's own")
		require.Len(t, diags, 1, "%+v", diags)
		found := diagsAtPointer(diags, diag.ConflictingRedecl, "/components/schemas/AB/allOf/0")
		require.Len(t, found, 1, entries)
		assert.Equal(t, id, found[0].Provenance.Node, entries)
	}
}

// TestMembers_ARefAndTheSchemaItNamesWrittenInlineFoldAlike pins a member a
// model declares itself beside one a $ref entry brings (GitHub #1951): in
// either entry order, the field takes the type, docs and bounds it takes where
// the named schema's declaration is written inline in the entry's place, the
// narrower type wherever it is written and, of a pair no value satisfies, the
// later. The report sits at the entry, and the property keeps the identity of
// the declaration the model writes, so a report about it sits there too.
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
			assert.Equal(t, ir.PropID("p/openapi/components/schemas/Ref/allOf/"+mineAt+"/properties/id"), byRef.ID, name)
			assert.Equal(t, ids.Ptr("components", "schemas", "Ref", "allOf", mineAt, "properties", "id"),
				byRef.Provenance.Pointer, name)
			assert.Equal(t, written.Type, byRef.Type, name)
			assert.Equal(t, written.Docs, byRef.Docs, name)
			assert.Equal(t, written.Constraints, byRef.Constraints, name)
			atRef, atInline := len(diagsUnder(diags, "/components/schemas/Ref/")), len(diagsUnder(diags, "/components/schemas/Inline/"))
			assert.Equal(t, atInline, atRef, "%s: as many reports: %+v", name, diags)
		}
	}
}

// TestMembers_RequiresAndTheDiscriminatorFollowTheDeclaredMember pins what
// names a member a composition settles: a composition requiring one it does
// not declare requires the member it declares, its name leaving Requires; a
// model declaring the member itself keeps that declaration's identity; and one
// whose declaration another model holds too, through a $ref to the allOf
// branch writing it, takes one in the composed namespace, which a
// discriminator naming that declaration follows.
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
`)
	required, ok := typeByName(doc, "Required").(*ir.Model)
	require.True(t, ok)
	assert.True(t, propertyOf(t, doc, "Required", "kind").Required)
	assert.Empty(t, required.Requires)

	for name, want := range map[string]ir.PropID{
		"Tagged": "p/openapi/components/schemas/Tagged/properties/kind",
		"Shared": "p/composed/components/schemas/Shared/allOf/properties/kind",
	} {
		tagged, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, name)
		kind := propertyOf(t, doc, name, "kind")
		assert.Equal(t, want, kind.ID, name)
		assert.Equal(t, ir.TypeID("t/prim/string"), kind.Type.Target, name)
		assert.Equal(t, new(int64(9)), kind.Constraints.MaxLength, "%s: B's bound", name)
		require.NotNil(t, tagged.Discriminator, name)
		assert.Equal(t, kind.ID, tagged.Discriminator.Property, name)
	}
}

// TestMembers_AModelIsSettledAfterWhatItComposes pins a model composing one
// whose member a fold settles: it reads that member settled, through an
// alias entry too, whichever is declared first, so Mid's integer, not its own
// number, is what Top's string meets.
func TestMembers_AModelIsSettledAfterWhatItComposes(t *testing.T) {
	t.Parallel()
	const base = "    Base: {type: object, properties: {id: {type: integer}}}\n"
	const mid = "    Mid:\n      allOf: [{$ref: '#/components/schemas/Base'}, {type: object, properties: {id: {type: number}}}]\n"
	const tops = "    Top:\n      allOf: [{$ref: '#/components/schemas/Mid'}, {type: object, properties: {id: {type: string}}}]\n" +
		"    Aliased:\n      allOf: [{$ref: '#/components/schemas/Mid', description: d}, {type: object, properties: {id: {type: string}}}]\n"
	for _, order := range []string{base + mid + tops, tops + mid + base} {
		_, diags := composedDoc(t, order)
		for _, at := range []string{"/components/schemas/Top/allOf/0", "/components/schemas/Aliased/allOf/0"} {
			found := diagsAtPointer(diags, diag.ConflictingRedecl, at)
			require.Len(t, found, 1, "%s: %+v", at, diags)
			assert.Contains(t, found[0].Message, "t/prim/integer", at)
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
// is settled in turn, the later entry's again, with the report at the entry
// bringing the one that lost.
func TestMembers_AModelAFoldComposesIsSettledInTheNextRound(t *testing.T) {
	t.Parallel()
	const parts = `    A1: {type: object, properties: {r: {type: string}}}
    B1: {type: object, properties: {r: {type: integer}}}
    A: {type: object, properties: {q: {$ref: '#/components/schemas/A1'}}}
    B: {type: object, properties: {q: {$ref: '#/components/schemas/B1'}}}
`
	for entries, later := range map[string]ir.TypeID{
		"[{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]": "t/prim/integer",
		"[{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/A'}]": "t/prim/string",
	} {
		doc, diags := composedDoc(t, parts+"    M: {allOf: "+entries+"}\n")

		q := propertyOf(t, doc, "M", "q")
		composed, ok := doc.Types[q.Type.Target].(*ir.Model)
		require.True(t, ok, "got %T", doc.Types[q.Type.Target])
		require.Len(t, composed.Properties, 1, "its own r")
		assert.Equal(t, later, composed.Properties[0].Type.Target, entries)
		assert.Len(t, diagsAtPointer(diags, diag.ConflictingRedecl, "/components/schemas/M/allOf/0"), 1, "%s: %+v", entries, diags)
	}
}

// TestMembers_CompositionsNestedPastTheRoundsAreReported pins a chain of
// folds each composing a model whose member is folded again, deeper
// than the rounds settled: what the rounds reach is settled, and the rest is
// reported once.
func TestMembers_CompositionsNestedPastTheRoundsAreReported(t *testing.T) {
	t.Parallel()
	var spec strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&spec, "    A%d: {type: object, properties: {q: {$ref: '#/components/schemas/A%d'}}}\n", i, i+1)
		fmt.Fprintf(&spec, "    B%d: {type: object, properties: {q: {$ref: '#/components/schemas/B%d'}}}\n", i, i+1)
	}
	spec.WriteString("    A11: {type: object, properties: {r: {type: string}}}\n")
	spec.WriteString("    B11: {type: object, properties: {r: {type: integer}}}\n")
	spec.WriteString("    M:\n      allOf: [{$ref: '#/components/schemas/A1'}, {$ref: '#/components/schemas/B1'}]\n")
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", spec.String()))
	require.Empty(t, irverify.Verify(doc))
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.BudgetExceeded, ir.SeverityError), "%+v", diags)
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
			x := propertyOf(t, doc, name(level), "x")
			assert.Equal(t, ir.TypeID("t/prim/integer"), x.Type.Target, "level %d, derived first %v", level, derivedFirst)
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

		for space, id := range map[string]ir.PropID{
			"anon":     ir.PropID("p/composed/" + tc.at + "/allOf/properties/y"),
			"composed": ir.PropID("p/composed/" + tc.at + "/allOf/allOf/properties/y"),
		} {
			m, ok := doc.Types[ir.TypeID("t/"+space+"/"+tc.at)].(*ir.Model)
			require.True(t, ok, "%s: the %s model", name, space)
			y, ok := openapitest.PropsByWire(m.Properties)["y"]
			require.True(t, ok, "%s: the %s model settles y", name, space)
			assert.Equal(t, id, y.ID, "%s: the %s model", name, space)
		}
	}
}
