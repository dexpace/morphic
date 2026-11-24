// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// redeclared is the property v of the allOf entry at, typed target.
func redeclared(target ir.TypeID, at string) ir.Property {
	return ir.Property{WireName: "v", Type: ir.TypeRef{Target: target}, Provenance: ptrAt(jsontext.Pointer("/allOf/" + at + "/properties/v"))}
}

// kindDecl declares the field "p" as kind at a pointer named for the kind, so
// what a fold reports and keeps reads the same whatever order the
// declarations come in.
func kindDecl(primitiveKind ir.PrimitiveKind, def *ir.Value) declaration {
	at := jsontext.Pointer("/k/" + string(primitiveKind))
	return declaration{
		property: ir.Property{WireName: "p", Type: primitiveRef(primitiveKind), Default: def, Provenance: ptrAt(at)},
		source:   written(`{"k":"` + string(primitiveKind) + `"}`),
	}
}

// foldOutcome is what a fold leaves that declaration order must not change:
// the field's type and default, the declarations it keeps, and each
// diagnostic's code at its pointer.
type foldOutcome struct {
	typ   ir.TypeRef
	def   *ir.Value
	kept  []string
	diags []string
}

// foldInEveryOrder folds decls in every order and returns each outcome.
func foldInEveryOrder(t *testing.T, decls ...declaration) []foldOutcome {
	t.Helper()
	reg := narrowingRegistry()
	reg["type/primitive/uint16"] = &ir.Primitive{ID: "type/primitive/uint16", PrimitiveKind: ir.PrimitiveUint16}
	orders := permutations(len(decls))
	outcomes := make([]foldOutcome, 0, len(orders))
	for _, order := range orders {
		stubbed, recorded := stubMerger(reg)
		ordered := make([]declaration, 0, len(decls))
		for _, i := range order {
			ordered = append(ordered, decls[i])
		}
		got := stubbed.fold(ordered)
		out := foldOutcome{typ: got.Type, def: got.Default, kept: slices.Sorted(maps.Keys(got.Unmodeled))}
		for _, diagnostic := range *recorded {
			out.diags = append(out.diags, diagnostic.Code+" "+string(diagnostic.Provenance.Pointer))
		}
		slices.Sort(out.diags)
		outcomes = append(outcomes, out)
	}
	return outcomes
}

// TestFold_TheSetDecidesWhateverTheOrder pins the fold over the whole set: a
// type that holds every other declaration's is the field's in every order, and
// where none does, the type that loses the fewest declarations is, each it
// loses being reported. A chain folded pair by pair kept int32 over uint32,
// reporting the overlap and keeping uint32, then narrowed to uint16 anyway,
// while the same declarations in another order drew nothing.
func TestFold_TheSetDecidesWhateverTheOrder(t *testing.T) {
	t.Parallel()
	two, half := number("2"), number("1.5")
	for _, tc := range []struct {
		name  string
		decls []declaration
		want  foldOutcome
	}{
		{"one type within every other", []declaration{
			kindDecl(ir.PrimitiveInt32, nil), kindDecl(ir.PrimitiveUint32, nil), kindDecl(ir.PrimitiveUint16, nil),
		}, foldOutcome{typ: primitiveRef(ir.PrimitiveUint16)}},
		{"the type that loses the fewest", []declaration{
			kindDecl(ir.PrimitiveNumber, nil), kindDecl(ir.PrimitiveString, nil), kindDecl(ir.PrimitiveInt32, nil),
		}, foldOutcome{
			typ:   primitiveRef(ir.PrimitiveInt32),
			kept:  []string{losingDeclarationKey + "/k/string"},
			diags: []string{diag.ConflictingRedecl + " /k/string"},
		}},
		{"a value the field takes, from any declaration", []declaration{
			kindDecl(ir.PrimitiveNumber, &half), kindDecl(ir.PrimitiveNumber, &two), kindDecl(ir.PrimitiveInteger, nil),
		}, foldOutcome{
			typ:   primitiveRef(ir.PrimitiveInteger),
			def:   &two,
			kept:  []string{losingDeclarationKey + "/k/number"},
			diags: []string{diag.DegradedConstruct + " /k/number"},
		}},
	} {
		for i, got := range foldInEveryOrder(t, tc.decls...) {
			assert.Equal(t, tc.want, got, "%s, order %d", tc.name, i)
		}
	}
}

// TestFold_ATieGoesToTheLaterAndIsReported pins the one choice declaration
// order makes: where two types each lose as many declarations as the other,
// the later declared wins, where a spec writes the specialization, and the
// other is reported and kept.
func TestFold_ATieGoesToTheLaterAndIsReported(t *testing.T) {
	t.Parallel()
	for _, order := range [][2]ir.PrimitiveKind{{ir.PrimitiveInt32, ir.PrimitiveUint32}, {ir.PrimitiveUint32, ir.PrimitiveInt32}} {
		stubbed, recorded := stubMerger(narrowingRegistry())
		got := stubbed.fold([]declaration{kindDecl(order[0], nil), kindDecl(order[1], nil)})

		assert.Equal(t, primitiveRef(order[1]), got.Type)
		require.Len(t, *recorded, 1)
		assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code, "an overlap is an info")
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/k/"+string(order[0]))
	}
}

// TestFold_ADroppedFirstDeclarationIsKept pins that the declaration whose
// identity the field keeps can lose its type like any other: it is reported
// and kept verbatim at its own pointer.
func TestFold_ADroppedFirstDeclarationIsKept(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(narrowingRegistry())
	first := kindDecl(ir.PrimitiveString, nil)

	got := stubbed.fold([]declaration{first, kindDecl(ir.PrimitiveInt32, nil), kindDecl(ir.PrimitiveNumber, nil)})

	assert.Equal(t, primitiveRef(ir.PrimitiveInt32), got.Type)
	assert.Equal(t, first.property.Provenance, got.Provenance, "the field keeps the first declaration's identity")
	require.Len(t, *recorded, 1)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Equal(t, jsontext.Pointer("/k/string"), (*recorded)[0].Provenance.Pointer)
	assert.Equal(t, ir.RawValue(`{"k":"string"}`), got.Unmodeled[losingDeclarationKey+"/k/string"].Value)
}

// TestFold_AConstraintConflictNamesTheDeclarationKeptFrom pins where a
// constraint conflict points: at the declaration that wrote the value kept,
// which is not the last declaration where that one sets none.
func TestFold_AConstraintConflictNamesTheDeclarationKeptFrom(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(narrowingRegistry())
	pattern := func(p string) *ir.Constraints { return &ir.Constraints{Pattern: p} }
	d := func(at string, constraints *ir.Constraints) declaration {
		return declaration{
			property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveString), Constraints: constraints, Provenance: ptrAt(jsontext.Pointer(at))},
			source:   written(`{}`),
		}
	}

	got := stubbed.fold([]declaration{d("/a", pattern("^y$")), d("/b", pattern("^x$")), d("/c", pattern("^x$")), d("/d", nil)})

	require.NotNil(t, got.Constraints)
	assert.Equal(t, "^x$", got.Constraints.Pattern)
	require.Len(t, *recorded, 1, "an agreeing declaration is no conflict")
	assert.Equal(t, jsontext.Pointer("/a"), (*recorded)[0].Provenance.Pointer)
	assert.Contains(t, (*recorded)[0].Message, "kept what the declaration at /c says over the one at /a",
		"the last to write the kept value is named, not one agreeing with it")
}

// TestFold_ADroppedDeclarationStillSaysWhatIsNotShape pins what a declaration
// whose type the field drops still contributes: whether the field is required
// or secret, and its docs where no declaration after it gives any. None of
// them describes the shape that lost.
func TestFold_ADroppedDeclarationStillSaysWhatIsNotShape(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(narrowingRegistry())
	dropped := kindDecl(ir.PrimitiveInteger, nil)
	dropped.property.Required, dropped.property.Secret = true, true
	dropped.property.Docs.Description = "a count"
	later := kindDecl(ir.PrimitiveString, nil)

	got := stubbed.fold([]declaration{dropped, later})

	assert.Equal(t, primitiveRef(ir.PrimitiveString), got.Type, "the tie goes to the later")
	assert.True(t, got.Required)
	assert.True(t, got.Secret)
	assert.Equal(t, "a count", got.Docs.Description)
	require.Len(t, *recorded, 1)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/k/integer")
}

// stringAt returns a declaration of field p typed string, written at pointer,
// whose source renders as raw.
func stringAt(pointer, raw string) declaration {
	return declaration{
		property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveString), Provenance: ptrAt(jsontext.Pointer(pointer))},
		source:   written(raw),
	}
}

// TestFold_UnmodeledKeepsTheLastOfAKeyAndReportsAnother pins what the field
// keeps of every declaration's Unmodeled. Each entry lands, since the keys are
// namespaced, and one two declarations give alike lands once, unreported. A
// key a later declaration gives another value is the later one's; the earlier
// one is reported and kept verbatim, as a detail given differently is, so
// nothing goes without a word. Of two alike, the one written at the pointer
// sorting first is kept, so no order decides where it was written.
func TestFold_UnmodeledKeepsTheLastOfAKeyAndReportsAnother(t *testing.T) {
	t.Parallel()
	entry := func(v string) ir.UnmodeledEntry { return ir.UnmodeledEntry{Value: ir.RawValue(v)} }
	first, second := stringAt("/allOf/0/properties/p", `{"x":0}`), stringAt("/allOf/1/properties/p", `{"x":1}`)
	alike := func(at jsontext.Pointer) ir.UnmodeledEntry {
		return ir.UnmodeledEntry{Value: ir.RawValue(`"s"`), Provenance: ptrAt(at)}
	}
	first.property.Unmodeled = ir.Unmodeled{"x-a": entry(`1`), "x-alike": alike("/allOf/0/x-alike"), "x-shared": entry(`"first"`)}
	second.property.Unmodeled = ir.Unmodeled{"x-b": entry(`2`), "x-alike": alike("/allOf/1/x-alike"), "x-shared": entry(`"second"`)}

	for _, order := range [][]declaration{{first, second}, {second, first}} {
		stubbed, recorded := stubMerger(narrowingRegistry())
		kept, other := order[1], order[0]

		got := stubbed.fold(order)

		assert.Equal(t, kept.property.Unmodeled["x-shared"], got.Unmodeled["x-shared"], "the later declaration's value stands")
		for _, key := range []string{"x-a", "x-b", "x-alike"} {
			assert.Contains(t, got.Unmodeled, key)
		}
		assert.Equal(t, jsontext.Pointer("/allOf/0/x-alike"), got.Unmodeled["x-alike"].Provenance.Pointer,
			"of two alike, the one written at the pointer sorting first, in either order")
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(other.property.Provenance.Pointer), "the other is kept verbatim")
		require.Len(t, *recorded, 1, "only the key given differently is reported")
		assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
		assert.Equal(t, other.property.Provenance.Pointer, (*recorded)[0].Provenance.Pointer)
		assert.Contains(t, (*recorded)[0].Message, "give its x-shared differently")
	}
}

// TestFold_AValueSpelledTwoWaysIsOneValue pins that a default and examples
// compare as JSON values: a number by magnitude and an object by its members
// whatever their order, so two declarations writing one value two ways agree,
// with nothing reported or kept.
func TestFold_AValueSpelledTwoWaysIsOneValue(t *testing.T) {
	t.Parallel()
	obj := func(fs ...ir.Field) *ir.Value { return &ir.Value{Kind: ir.ValueObject, Object: fs} }
	field := func(name, n string) ir.Field { return ir.Field{Name: name, Value: number(n)} }
	cases := []struct {
		name string
		a, b func(property *ir.Property)
	}{
		{"a number default", func(property *ir.Property) { value := number("1"); property.Default = &value },
			func(property *ir.Property) { value := number("1.0"); property.Default = &value }},
		{"an object default in two member orders", func(property *ir.Property) { property.Default = obj(field("a", "1"), field("b", "2")) },
			func(property *ir.Property) { property.Default = obj(field("b", "2e0"), field("a", "1")) }},
		{"examples", func(property *ir.Property) {
			value := number("2")
			property.Examples = []ir.Example{{Name: "e", Value: &value}}
		},
			func(property *ir.Property) {
				value := number("20e-1")
				property.Examples = []ir.Example{{Name: "e", Value: &value}}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stubbed, recorded := stubMerger(narrowingRegistry())
			first := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveAny), Provenance: ptrAt("/allOf/0/properties/p")}, source: unread(t)}
			second := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveAny), Provenance: ptrAt("/allOf/1/properties/p")}, source: unread(t)}
			tc.a(&first.property)
			tc.b(&second.property)

			got := stubbed.fold([]declaration{first, second})

			assert.Empty(t, *recorded)
			assert.Empty(t, got.Unmodeled)
		})
	}
	stubbedValue, recorded := stubMerger(narrowingRegistry())
	one, two := number("1"), number("2")
	first := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveAny), Default: &one, Provenance: ptrAt("/a")}, source: written(`{}`)}
	second := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveAny), Default: &two, Provenance: ptrAt("/b")}, source: written(`{}`)}
	stubbedValue.fold([]declaration{first, second})
	require.Len(t, *recorded, 1, "two values are still two")
}

// TestFold_ThePromotionMarkerFollowsTheSecretTaken is the marker pin for the
// other annotation a promotion writes on a declaration. Secret folds by OR, so
// the field takes it from a later declaration as readily as from the first, and
// the field keeps the first's provenance: without the join, a redaction a
// heuristic read would carry no trace of it. A declaration lends no marker for a
// redaction it declared or does not ask for, whatever else was inferred of it,
// and the answer is the same in either order.
func TestFold_ThePromotionMarkerFollowsTheSecretTaken(t *testing.T) {
	t.Parallel()
	const marker ir.Inferred = "extension-promotion"
	plain := stringAt("/allOf/0/properties/p", `{}`)
	promoted := stringAt("/allOf/1/properties/p", `{}`)
	promoted.property.Secret = true
	promoted.property.Provenance.Inferred = marker
	promoted.secretBy = marker
	declared := stringAt("/allOf/2/properties/p", `{"format":"password"}`)
	declared.property.Secret = true
	unrelated := stringAt("/allOf/3/properties/p", `{}`)
	unrelated.property.Provenance.Inferred = marker
	// A declared redaction beside a marker a promoted deprecation drew: the field
	// keeps the first declaration's deprecation, so nothing it keeps was inferred.
	declaredMarked := stringAt("/allOf/4/properties/p", `{"format":"password"}`)
	declaredMarked.property.Secret = true
	declaredMarked.property.Provenance.Inferred = marker

	for _, tc := range []struct {
		name   string
		decls  []declaration
		secret bool
		want   ir.Inferred
	}{
		{"a promoted redaction second", []declaration{plain, promoted}, true, marker},
		{"a promoted redaction first", []declaration{promoted, plain}, true, marker},
		{"beside a declared one", []declaration{declared, promoted}, true, marker},
		{"beside a declared one, swapped", []declaration{promoted, declared}, true, marker},
		{"a declared redaction alone", []declaration{plain, declared}, true, ""},
		{"a marker that is no redaction's", []declaration{plain, unrelated}, false, ""},
		{"a declared redaction's declaration marked for another fact", []declaration{plain, declaredMarked}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stubbed, _ := stubMerger(narrowingRegistry())
			got := stubbed.fold(tc.decls)
			assert.Equal(t, tc.secret, got.Secret)
			assert.Equal(t, tc.want, got.Provenance.Inferred)
		})
	}
}

// TestFold_ThePromotionMarkerFollowsTheDeprecationKept pins that the field
// names the heuristic that promoted its deprecation whichever declaration gave
// it: a deprecation promoted from an extension is marked on that declaration's
// provenance, and the field keeps the first declaration's provenance, while the
// last deprecation written is the one kept.
func TestFold_ThePromotionMarkerFollowsTheDeprecationKept(t *testing.T) {
	t.Parallel()
	const marker ir.Inferred = "extension-promotion"
	plain := stringAt("/allOf/0/properties/p", `{}`)
	promoted := stringAt("/allOf/1/properties/p", `{}`)
	promoted.property.Deprecation = &ir.Deprecation{Message: "use q"}
	promoted.property.Provenance.Inferred = marker
	declared := stringAt("/allOf/2/properties/p", `{"deprecated":true}`)
	declared.property.Deprecation = &ir.Deprecation{Message: "gone"}

	for _, order := range [][]declaration{{plain, promoted}, {promoted, plain}} {
		stubbed, recorded := stubMerger(narrowingRegistry())
		got := stubbed.fold(order)
		assert.Equal(t, &ir.Deprecation{Message: "use q"}, got.Deprecation)
		assert.Equal(t, marker, got.Provenance.Inferred, "the promoted deprecation stays marked")
		assert.Empty(t, *recorded)
	}

	stubbedValue, recorded := stubMerger(narrowingRegistry())
	got := stubbedValue.fold([]declaration{plain, promoted, declared})
	assert.Equal(t, &ir.Deprecation{Message: "gone"}, got.Deprecation, "the last deprecation written is kept")
	assert.Empty(t, got.Provenance.Inferred, "a declared deprecation kept carries no heuristic's name")
	require.Len(t, *recorded, 1)
	assert.Equal(t, promoted.property.Provenance.Pointer, (*recorded)[0].Provenance.Pointer)
}

// TestFold_DeclarationsPastTheBoundAreKeptUnjudged pins the bound on one fold:
// the first maxFoldDeclarations declarations fold as a set, and each later one,
// whatever it says, is kept verbatim under one budget error at the first of
// them, so a document cannot drive the pairwise judgment past any time budget.
func TestFold_DeclarationsPastTheBoundAreKeptUnjudged(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(narrowingRegistry())
	at := func(i int) jsontext.Pointer { return jsontext.Pointer(fmt.Sprintf("/allOf/%d/properties/p", i)) }
	decls := make([]declaration, 0, maxFoldDeclarations+2)
	for i := range maxFoldDeclarations {
		decls = append(decls, declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveString), Provenance: ptrAt(at(i))}, source: unread(t)})
	}
	for i := maxFoldDeclarations; i < maxFoldDeclarations+2; i++ {
		decls = append(decls, declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveURL), Provenance: ptrAt(at(i))},
			source: written(`{"type":"string","format":"uri"}`)})
	}

	got := stubbed.fold(decls)

	assert.Equal(t, primitiveRef(ir.PrimitiveString), got.Type, "a declaration past the bound narrows nothing")
	require.Len(t, *recorded, 1)
	assert.Equal(t, ir.SeverityError, (*recorded)[0].Severity)
	assert.Equal(t, diag.BudgetExceeded, (*recorded)[0].Code)
	assert.Equal(t, at(maxFoldDeclarations), (*recorded)[0].Provenance.Pointer)
	assert.Contains(t, (*recorded)[0].Message, "declared 66 times, past the 64")
	assert.Equal(t, []string{losingDeclarationKey + string(at(maxFoldDeclarations)), losingDeclarationKey + string(at(maxFoldDeclarations+1))},
		slices.Sorted(maps.Keys(got.Unmodeled)), "each is kept")
}

// TestFold_AnUnjudgedDeclarationThatWillNotRenderIsReported pins that a
// declaration past the bound is kept as one the fold judged and lost is: one
// with no source to render is reported unkeepable beside the budget error, not
// dropped in silence.
func TestFold_AnUnjudgedDeclarationThatWillNotRenderIsReported(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(narrowingRegistry())
	at := func(i int) jsontext.Pointer { return jsontext.Pointer(fmt.Sprintf("/allOf/%d/properties/p", i)) }
	decls := make([]declaration, 0, maxFoldDeclarations+1)
	for i := range maxFoldDeclarations {
		decls = append(decls, declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveString), Provenance: ptrAt(at(i))}, source: unread(t)})
	}
	decls = append(decls, declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveURL), Provenance: ptrAt(at(maxFoldDeclarations))}})

	got := stubbed.fold(decls)

	require.Len(t, *recorded, 2, "%+v", *recorded)
	assert.Equal(t, diag.BudgetExceeded, (*recorded)[0].Code)
	assert.Equal(t, diag.UnpreservableConstruct, (*recorded)[1].Code)
	assert.Equal(t, at(maxFoldDeclarations), (*recorded)[1].Provenance.Pointer)
	assert.Contains(t, (*recorded)[1].Message, errNoSource.Error())
	assert.Empty(t, got.Unmodeled, "nothing of it could be kept")
}

// TestFold_ANarrowerTypeThatLosesIsAnOverlap pins how the fold reports a type
// narrower than the winner's that still lost. The judgment's narrowing is
// transitive, so such a type holds every type the winner holds and wins; were
// it ever to lose, it shares the winner's values, and is reported as an
// overlap, never as a pair the merge does not compare.
func TestFold_ANarrowerTypeThatLosesIsAnOverlap(t *testing.T) {
	t.Parallel()
	stubbed, _ := stubMerger(narrowingRegistry())
	decls := []declaration{stringAt("/allOf/0/properties/p", `{}`), stringAt("/allOf/1/properties/p", `{}`)}
	_, field := fieldSpan(decls)
	folding := newFolding(&stubbed.Merger, decls, field)
	folding.rel[folding.winner][1] = secondTypeIsNarrower

	folding.drop(1)

	require.Len(t, folding.notes[1], 1)
	assert.Contains(t, folding.notes[1][0].message, "whose values overlap")
	assert.True(t, folding.lost[1])
}

// TestSameExamples_ComparesEachExampleWhole pins that two example lists agree
// only where each example agrees whole: its value as one JSON value, and its
// name, summary and description as written, a missing value included.
func TestSameExamples_ComparesEachExampleWhole(t *testing.T) {
	t.Parallel()
	two, twoAgain := number("2"), number("2.0")
	base := []ir.Example{{Name: "e", Value: &two}}
	assert.True(t, sameExamples(base, []ir.Example{{Name: "e", Value: &twoAgain}}), "one value spelled two ways")
	assert.False(t, sameExamples(base, []ir.Example{{Name: "f", Value: &two}}), "a name apart")
	assert.False(t, sameExamples(base, []ir.Example{{Name: "e"}}), "a value against none")
	assert.True(t, sameExamples([]ir.Example{{Name: "e"}}, []ir.Example{{Name: "e"}}), "no value on either")
	assert.False(t, sameExamples(base, nil), "a list against none")
}

// TestFold_ExamplesAreJudgedAsOneKeyword pins that a broader declaration's
// examples are adopted all or nothing: one example the narrower type refuses
// refuses the keyword, and an example with no value says nothing a type could
// refuse.
func TestFold_ExamplesAreJudgedAsOneKeyword(t *testing.T) {
	t.Parallel()
	one, half := number("1"), number("1.5")
	for _, tc := range []struct {
		name     string
		examples []ir.Example
		refused  bool
	}{
		{"no examples say nothing", nil, false},
		{"an example with no value says nothing", []ir.Example{{Value: &one}, {Summary: "no value"}}, false},
		{"one refused example refuses the keyword", []ir.Example{{Value: &one}, {Value: &half}}, true},
	} {
		stubbed, recorded := stubMerger(narrowingRegistry())
		broad := ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveNumber), Examples: tc.examples, Provenance: ptrAt("/b")}
		narrow := ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveInteger), Provenance: ptrAt("/a")}

		got := foldTwo(stubbed, narrow, broad, written(`{"examples":[1]}`))

		if !tc.refused {
			assert.Equal(t, tc.examples, got.Examples, "%s: adopted whole", tc.name)
			assert.Empty(t, *recorded, tc.name)
			continue
		}
		assert.Nil(t, got.Examples, "%s: refused whole", tc.name)
		require.Len(t, *recorded, 1, tc.name)
		assert.Contains(t, (*recorded)[0].Message, "admits the examples the declaration at /b gives", tc.name)
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/b", "%s: the writer is kept", tc.name)
	}
}

// TestMerger_ReconcileReportsDisagreementAndKeepsAWinner pins the documented
// policy: an unsatisfiable redeclaration is surfaced rather than silently
// resolved, and the merge still yields one property.
func TestMerger_ReconcileReportsDisagreementAndKeepsAWinner(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/str": &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
		"type/int": &ir.Primitive{ID: "type/int", PrimitiveKind: ir.PrimitiveInt32},
	})
	dst := ir.Property{Name: ir.Naming{Source: "id"}, WireName: "id", Type: ir.TypeRef{Target: "type/str"}}
	src := ir.Property{Name: ir.Naming{Source: "id"}, WireName: "id", Type: ir.TypeRef{Target: "type/int"},
		Provenance: ptrAt("/components/schemas/S/properties/id")}

	dst = foldTwo(stubbed, dst, src, written(`{"type":"integer"}`))

	require.Len(t, *recorded, 1, "one disagreement, one diagnostic")
	diagnostic := (*recorded)[0]
	assert.Equal(t, diag.ConflictingRedecl, diagnostic.Code)
	assert.Contains(t, diagnostic.Message, `"id"`, "the diagnostic names the field that disagrees")
	assert.Equal(t, ir.TypeID("type/int"), dst.Type.Target, "the later declaration keeps its type")
}

func TestKindOf_ReportsUnresolvableTargets(t *testing.T) {
	t.Parallel()
	stubbed, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/str": &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
	})

	kind, ok := judge(stubbed).kindOf(ir.TypeRef{Target: "type/str"})
	require.True(t, ok)
	assert.Equal(t, ir.PrimitiveString, kind.primitive)
	assert.Empty(t, kind.format)

	_, ok = judge(stubbed).kindOf(ir.TypeRef{Target: "type/absent"})
	assert.False(t, ok, "a target the registry does not hold resolves to no kind")
}

// TestFold_UnmodeledAlikeUnderAnotherBaseIsKept pins that an entry two
// declarations write alike, writing a reference, is one only where
// FieldDeclarationContext.Alike says they read it against one base: elsewhere
// the same text may name another schema, so the earlier one is reported and
// kept as one given differently is. An entry writing no reference is one under
// any base.
func TestFold_UnmodeledAlikeUnderAnotherBaseIsKept(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value    string
		wantKeys []string
		reports  int
	}{
		{`{"$ref":"n.json"}`, []string{"openapi:not", losingDeclarationKey + "/allOf/0/properties/p"}, 1},
		{`{"const":"n.json"}`, []string{"openapi:not"}, 0},
		{`{"$ref":"https://ex.test/n.json"}`, []string{"openapi:not"}, 0},
	} {
		entry := ir.UnmodeledEntry{Value: ir.RawValue(tc.value)}
		first, second := stringAt("/allOf/0/properties/p", `{"x":0}`), stringAt("/allOf/1/properties/p", `{"x":1}`)
		first.property.Unmodeled = ir.Unmodeled{"openapi:not": entry}
		second.property.Unmodeled = ir.Unmodeled{"openapi:not": entry}
		stubbed, recorded := stubMerger(narrowingRegistry())
		stubbed.walk.apart = true

		got := stubbed.fold([]declaration{first, second})

		assert.ElementsMatch(t, tc.wantKeys, slices.Collect(maps.Keys(got.Unmodeled)), tc.value)
		assert.Len(t, *recorded, tc.reports, tc.value)
		for _, diagnostic := range *recorded {
			assert.Contains(t, diagnostic.Message, "give its openapi:not differently", tc.value)
		}
	}
}

// TestSameDeprecation_ReadsEveryField holds sameDeprecation to every field
// ir.Deprecation declares: two deprecations differing in any one field alone
// are two, so a field the IR gains fails here until the comparison reads it.
func TestSameDeprecation_ReadsEveryField(t *testing.T) {
	t.Parallel()
	assert.True(t, sameDeprecation(&ir.Deprecation{}, &ir.Deprecation{}), "two empty deprecations are one")
	for field := range reflect.TypeFor[ir.Deprecation]().Fields() {
		var other ir.Deprecation
		v := reflect.ValueOf(&other).Elem().FieldByIndex(field.Index)
		switch v.Kind() {
		case reflect.String:
			v.SetString("x")
		case reflect.Slice:
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		default:
			require.Failf(t, "unset field", "Deprecation.%s is a %s, which this test cannot set", field.Name, v.Kind())
		}
		assert.False(t, sameDeprecation(&ir.Deprecation{}, &other), "a deprecation differing only in %s", field.Name)
	}
}
