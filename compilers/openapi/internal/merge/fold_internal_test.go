package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// redeclared is the property v of the allOf branch at, typed target.
func redeclared(target ir.TypeID, at string) ir.Property {
	return ir.Property{WireName: "v", Type: ir.TypeRef{Target: target}, Provenance: ptrAt(jsontext.Pointer("/allOf/" + at + "/properties/v"))}
}

// TestPropIndex_ARedeclarationFoldsOnceTheRegistryIsWhole pins what frees the
// fold from declaration order (GitHub #898): the walk lands a wire name's first
// declaration and records every later one, the model hands its folds over
// once, and the fold reads the registry it settles against, which holds nodes
// the walk had not built when it met the declarations.
func TestPropIndex_ARedeclarationFoldsOnceTheRegistryIsWhole(t *testing.T) {
	t.Parallel()
	reg := ir.TypeRegistry{"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString}}
	var queued []Settle
	m := &ir.Model{ID: "t/m"}
	idx := NewPropIndex(m, func(s Settle) { queued = append(queued, s) })

	w := func(at string) ir.Property {
		return ir.Property{WireName: "w", Type: ir.TypeRef{Target: "t/str"}, Provenance: ptrAt(jsontext.Pointer("/allOf/" + at + "/properties/w"))}
	}
	idx.MergeProperty(redeclared("t/str", "0"), written(`{"type":"string"}`))
	idx.MergeProperty(w("0"), unread(t))
	idx.MergeProperty(redeclared("t/uri", "1"), unread(t))
	idx.MergeProperty(w("1"), unread(t))
	idx.MergeProperty(redeclared("t/bool", "2"), written(`{"type":"boolean"}`))

	require.Len(t, queued, 1, "one settlement per model, however many fields are redeclared")
	require.Len(t, m.Properties, 2, "a redeclaration adds no property")
	assert.Equal(t, ir.TypeID("t/str"), m.Properties[0].Type.Target, "nothing is folded before the settlement runs")
	assert.Empty(t, m.Properties[0].Unmodeled)

	reg["t/uri"] = &ir.Primitive{ID: "t/uri", Prim: ir.PrimURL}
	reg["t/bool"] = &ir.Primitive{ID: "t/bool", Prim: ir.PrimBool}
	var settled []ir.Diagnostic
	queued[0](reg, func(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any) {
		settled = append(settled, diag.Newf(sev, code, ir.Provenance{Pointer: pointer}, format, args...))
	})

	assert.Equal(t, ir.TypeID("t/uri"), m.Properties[0].Type.Target,
		"the narrower type wins, judged against a node the walk had not built")
	require.Len(t, settled, 1)
	assert.Equal(t, diag.ConflictingRedecl, settled[0].Code)
	assert.Equal(t, ptrAt("/allOf/2/properties/v").Pointer, settled[0].Provenance.Pointer)
	assert.Equal(t, []string{losingDeclarationKey + "/allOf/2/properties/v"}, slices.Sorted(maps.Keys(m.Properties[0].Unmodeled)),
		"the losing declaration is kept, and only it")
	assert.Equal(t, ir.TypeID("t/str"), m.Properties[1].Type.Target, "a field declared once is left as declared")
}

// TestPropIndex_AModelDeclaringEachNameOnceQueuesNothing pins the common path:
// a model whose properties are each declared once has nothing to fold, so it
// hands no settlement over.
func TestPropIndex_AModelDeclaringEachNameOnceQueuesNothing(t *testing.T) {
	t.Parallel()
	m := &ir.Model{ID: "t/m"}
	idx := NewPropIndex(m, func(Settle) { t.Fatal("a model with no redeclaration queues no fold") })

	idx.MergeProperty(ir.Property{WireName: "a", Provenance: ptrAt("/properties/a")}, unread(t))
	idx.MergeProperty(ir.Property{WireName: "b", Provenance: ptrAt("/properties/b")}, unread(t))

	require.Len(t, m.Properties, 2)
	assert.Equal(t, "a", m.Properties[0].WireName)
	assert.Equal(t, "b", m.Properties[1].WireName)
}

// kindDecl declares the field "p" as kind at a pointer named for the kind, so
// what a fold reports and keeps reads the same whatever order the
// declarations come in.
func kindDecl(k ir.PrimKind, def *ir.Value) declaration {
	at := jsontext.Pointer("/k/" + string(k))
	return declaration{
		prop:   ir.Property{WireName: "p", Type: primRef(k), Default: def, Provenance: ptrAt(at)},
		source: written(`{"k":"` + string(k) + `"}`),
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
	reg["t/prim/uint16"] = &ir.Primitive{ID: "t/prim/uint16", Prim: ir.PrimUint16}
	orders := permutations(len(decls))
	outcomes := make([]foldOutcome, 0, len(orders))
	for _, order := range orders {
		g, recorded := stubMerger(reg)
		ordered := make([]declaration, 0, len(decls))
		for _, i := range order {
			ordered = append(ordered, decls[i])
		}
		got := g.fold(ordered)
		out := foldOutcome{typ: got.Type, def: got.Default, kept: slices.Sorted(maps.Keys(got.Unmodeled))}
		for _, d := range *recorded {
			out.diags = append(out.diags, d.Code+" "+string(d.Provenance.Pointer))
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
	two, half := num("2"), num("1.5")
	for _, tc := range []struct {
		name  string
		decls []declaration
		want  foldOutcome
	}{
		{"one type within every other", []declaration{
			kindDecl(ir.PrimInt32, nil), kindDecl(ir.PrimUint32, nil), kindDecl(ir.PrimUint16, nil),
		}, foldOutcome{typ: primRef(ir.PrimUint16)}},
		{"the type that loses the fewest", []declaration{
			kindDecl(ir.PrimNumber, nil), kindDecl(ir.PrimString, nil), kindDecl(ir.PrimInt32, nil),
		}, foldOutcome{
			typ:   primRef(ir.PrimInt32),
			kept:  []string{losingDeclarationKey + "/k/string"},
			diags: []string{diag.ConflictingRedecl + " /k/string"},
		}},
		{"a value the field takes, from any declaration", []declaration{
			kindDecl(ir.PrimNumber, &half), kindDecl(ir.PrimNumber, &two), kindDecl(ir.PrimInteger, nil),
		}, foldOutcome{
			typ:   primRef(ir.PrimInteger),
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

// TestFold_ATieGoesToTheFirstAndIsReported pins the one choice declaration
// order makes: where two types each lose as many declarations as the other,
// the first declared wins, and the other is reported and kept.
func TestFold_ATieGoesToTheFirstAndIsReported(t *testing.T) {
	t.Parallel()
	for _, order := range [][2]ir.PrimKind{{ir.PrimInt32, ir.PrimUint32}, {ir.PrimUint32, ir.PrimInt32}} {
		g, recorded := stubMerger(narrowingRegistry())
		got := g.fold([]declaration{kindDecl(order[0], nil), kindDecl(order[1], nil)})

		assert.Equal(t, primRef(order[0]), got.Type)
		require.Len(t, *recorded, 1)
		assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code, "an overlap is an info")
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/k/"+string(order[1]))
	}
}

// TestFold_ADroppedFirstDeclarationIsKept pins that the declaration whose
// identity the field keeps can lose its type like any other: it is reported
// and kept verbatim at its own pointer.
func TestFold_ADroppedFirstDeclarationIsKept(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(narrowingRegistry())
	first := kindDecl(ir.PrimString, nil)

	got := g.fold([]declaration{first, kindDecl(ir.PrimInt32, nil), kindDecl(ir.PrimNumber, nil)})

	assert.Equal(t, primRef(ir.PrimInt32), got.Type)
	assert.Equal(t, first.prop.Provenance, got.Provenance, "the field keeps the first declaration's identity")
	require.Len(t, *recorded, 1)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Equal(t, jsontext.Pointer("/k/string"), (*recorded)[0].Provenance.Pointer)
	assert.Equal(t, ir.RawValue(`{"k":"string"}`), got.Unmodeled[losingDeclarationKey+"/k/string"].Value)
}

// TestFold_AConstraintConflictNamesTheDeclarationKeptFrom pins where a
// constraint conflict points: at the declaration that wrote the value kept,
// which is not the first declaration where that one sets none.
func TestFold_AConstraintConflictNamesTheDeclarationKeptFrom(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(narrowingRegistry())
	maxLen := func(n int64) *ir.Constraints { return &ir.Constraints{MaxLength: &n} }
	d := func(at string, c *ir.Constraints) declaration {
		return declaration{
			prop:   ir.Property{WireName: "p", Type: primRef(ir.PrimString), Constraints: c, Provenance: ptrAt(jsontext.Pointer(at))},
			source: written(`{}`),
		}
	}

	got := g.fold([]declaration{d("/a", nil), d("/b", maxLen(5)), d("/c", maxLen(5)), d("/d", maxLen(6))})

	require.NotNil(t, got.Constraints)
	assert.Equal(t, int64(5), *got.Constraints.MaxLength)
	require.Len(t, *recorded, 1, "an agreeing declaration is no conflict")
	assert.Equal(t, jsontext.Pointer("/d"), (*recorded)[0].Provenance.Pointer)
	assert.Contains(t, (*recorded)[0].Message, "kept what the declaration at /b says over the one at /d",
		"the first to write the kept value is named, not one agreeing with it")
}

// TestFold_ADroppedDeclarationStillSaysWhatIsNotShape pins what a declaration
// whose type the field drops still contributes: whether the field is required
// or secret, and its docs where no declaration before it gave any. None of
// them describes the shape that lost.
func TestFold_ADroppedDeclarationStillSaysWhatIsNotShape(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(narrowingRegistry())
	first := kindDecl(ir.PrimString, nil)
	dropped := kindDecl(ir.PrimInteger, nil)
	dropped.prop.Required, dropped.prop.Secret = true, true
	dropped.prop.Docs.Description = "a count"

	got := g.fold([]declaration{first, dropped})

	assert.Equal(t, primRef(ir.PrimString), got.Type, "the tie goes to the first")
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
		prop:   ir.Property{WireName: "p", Type: primRef(ir.PrimString), Provenance: ptrAt(jsontext.Pointer(pointer))},
		source: written(raw),
	}
}

// TestFold_UnmodeledKeepsTheFirstOfAKeyAndReportsAnother pins what the field
// keeps of every declaration's Unmodeled. Each entry lands, since the keys are
// namespaced, and one two declarations give alike lands once, unreported. A
// key a later declaration gives another value is the first one's; the later
// one is reported and kept verbatim, as a detail given differently is, so the
// field does not follow branch order and nothing goes without a word.
func TestFold_UnmodeledKeepsTheFirstOfAKeyAndReportsAnother(t *testing.T) {
	t.Parallel()
	entry := func(v string) ir.UnmodeledEntry { return ir.UnmodeledEntry{Value: ir.RawValue(v)} }
	first, second := stringAt("/allOf/0/properties/p", `{"x":0}`), stringAt("/allOf/1/properties/p", `{"x":1}`)
	first.prop.Unmodeled = ir.Unmodeled{"x-a": entry(`1`), "x-alike": entry(`"s"`), "x-shared": entry(`"first"`)}
	second.prop.Unmodeled = ir.Unmodeled{"x-b": entry(`2`), "x-alike": entry(`"s"`), "x-shared": entry(`"second"`)}

	for _, order := range [][]declaration{{first, second}, {second, first}} {
		g, recorded := stubMerger(narrowingRegistry())
		kept, other := order[0], order[1]

		got := g.fold(order)

		assert.Equal(t, kept.prop.Unmodeled["x-shared"], got.Unmodeled["x-shared"], "the first declaration's value stands")
		for _, key := range []string{"x-a", "x-b", "x-alike"} {
			assert.Contains(t, got.Unmodeled, key)
		}
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(other.prop.Provenance.Pointer), "the other is kept verbatim")
		require.Len(t, *recorded, 1, "only the key given differently is reported")
		assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
		assert.Equal(t, other.prop.Provenance.Pointer, (*recorded)[0].Provenance.Pointer)
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
	field := func(name, n string) ir.Field { return ir.Field{Name: name, Value: num(n)} }
	cases := []struct {
		name string
		a, b func(p *ir.Property)
	}{
		{"a number default", func(p *ir.Property) { v := num("1"); p.Default = &v },
			func(p *ir.Property) { v := num("1.0"); p.Default = &v }},
		{"an object default in two member orders", func(p *ir.Property) { p.Default = obj(field("a", "1"), field("b", "2")) },
			func(p *ir.Property) { p.Default = obj(field("b", "2e0"), field("a", "1")) }},
		{"examples", func(p *ir.Property) { v := num("2"); p.Examples = []ir.Example{{Name: "e", Value: &v}} },
			func(p *ir.Property) { v := num("20e-1"); p.Examples = []ir.Example{{Name: "e", Value: &v}} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(narrowingRegistry())
			first := declaration{prop: ir.Property{WireName: "p", Type: primRef(ir.PrimAny), Provenance: ptrAt("/allOf/0/properties/p")}, source: unread(t)}
			second := declaration{prop: ir.Property{WireName: "p", Type: primRef(ir.PrimAny), Provenance: ptrAt("/allOf/1/properties/p")}, source: unread(t)}
			tc.a(&first.prop)
			tc.b(&second.prop)

			got := g.fold([]declaration{first, second})

			assert.Empty(t, *recorded)
			assert.Empty(t, got.Unmodeled)
		})
	}
	g, recorded := stubMerger(narrowingRegistry())
	one, two := num("1"), num("2")
	first := declaration{prop: ir.Property{WireName: "p", Type: primRef(ir.PrimAny), Default: &one, Provenance: ptrAt("/a")}, source: written(`{}`)}
	second := declaration{prop: ir.Property{WireName: "p", Type: primRef(ir.PrimAny), Default: &two, Provenance: ptrAt("/b")}, source: written(`{}`)}
	g.fold([]declaration{first, second})
	require.Len(t, *recorded, 1, "two values are still two")
}

// TestFold_ThePromotionMarkerFollowsTheDeprecationKept pins that the field
// names the heuristic that promoted its deprecation whichever declaration gave
// it: a deprecation promoted from an extension is marked on that declaration's
// provenance, and the field keeps the first declaration's provenance.
func TestFold_ThePromotionMarkerFollowsTheDeprecationKept(t *testing.T) {
	t.Parallel()
	const marker = "extension-promotion"
	plain := stringAt("/allOf/0/properties/p", `{}`)
	promoted := stringAt("/allOf/1/properties/p", `{}`)
	promoted.prop.Deprecation = &ir.Deprecation{Message: "use q"}
	promoted.prop.Provenance.Inferred = marker
	declared := stringAt("/allOf/2/properties/p", `{"deprecated":true}`)
	declared.prop.Deprecation = &ir.Deprecation{Message: "gone"}

	for _, order := range [][]declaration{{plain, promoted}, {promoted, plain}} {
		g, recorded := stubMerger(narrowingRegistry())
		got := g.fold(order)
		assert.Equal(t, &ir.Deprecation{Message: "use q"}, got.Deprecation)
		assert.Equal(t, marker, got.Provenance.Inferred, "the promoted deprecation stays marked")
		assert.Empty(t, *recorded)
	}

	g, recorded := stubMerger(narrowingRegistry())
	got := g.fold([]declaration{plain, declared, promoted})
	assert.Equal(t, &ir.Deprecation{Message: "gone"}, got.Deprecation, "the first deprecation written is kept")
	assert.Empty(t, got.Provenance.Inferred, "a declared deprecation kept carries no heuristic's name")
	require.Len(t, *recorded, 1)
	assert.Equal(t, promoted.prop.Provenance.Pointer, (*recorded)[0].Provenance.Pointer)
}

// TestFold_DeclarationsPastTheBoundAreKeptUnjudged pins the bound on one fold:
// the first maxFoldDeclarations declarations fold as a set, and each later one,
// whatever it says, is kept verbatim under one budget error at the first of
// them, so a document cannot drive the pairwise judgment past any time budget.
func TestFold_DeclarationsPastTheBoundAreKeptUnjudged(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(narrowingRegistry())
	at := func(i int) jsontext.Pointer { return jsontext.Pointer(fmt.Sprintf("/allOf/%d/properties/p", i)) }
	decls := make([]declaration, 0, maxFoldDeclarations+2)
	for i := range maxFoldDeclarations {
		decls = append(decls, declaration{prop: ir.Property{WireName: "p", Type: primRef(ir.PrimString), Provenance: ptrAt(at(i))}, source: unread(t)})
	}
	for i := maxFoldDeclarations; i < maxFoldDeclarations+2; i++ {
		decls = append(decls, declaration{prop: ir.Property{WireName: "p", Type: primRef(ir.PrimURL), Provenance: ptrAt(at(i))},
			source: written(`{"type":"string","format":"uri"}`)})
	}

	got := g.fold(decls)

	assert.Equal(t, primRef(ir.PrimString), got.Type, "a declaration past the bound narrows nothing")
	require.Len(t, *recorded, 1)
	assert.Equal(t, ir.SeverityError, (*recorded)[0].Severity)
	assert.Equal(t, diag.BudgetExceeded, (*recorded)[0].Code)
	assert.Equal(t, at(maxFoldDeclarations), (*recorded)[0].Provenance.Pointer)
	assert.Contains(t, (*recorded)[0].Message, "declared 66 times, past the 64")
	assert.Equal(t, []string{losingDeclarationKey + string(at(maxFoldDeclarations)), losingDeclarationKey + string(at(maxFoldDeclarations+1))},
		slices.Sorted(maps.Keys(got.Unmodeled)), "each is kept")
}

// TestFold_ANarrowerTypeThatLosesIsAnOverlap pins how the fold reports a type
// narrower than the winner's that still lost. The judgment's narrowing is
// transitive, so such a type holds every type the winner holds and wins; were
// it ever to lose, it shares the winner's values, and is reported as an
// overlap, never as a pair the merge does not compare.
func TestFold_ANarrowerTypeThatLosesIsAnOverlap(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(narrowingRegistry())
	f := newFolding(g, []declaration{stringAt("/allOf/0/properties/p", `{}`), stringAt("/allOf/1/properties/p", `{}`)})
	f.rel[f.winner][1] = relBNarrower

	f.drop(1)

	require.Len(t, f.notes[1], 1)
	assert.Contains(t, f.notes[1][0].msg, "whose values overlap")
	assert.True(t, f.lost[1])
}

// TestSameExamples_ComparesEachExampleWhole pins that two example lists agree
// only where each example agrees whole: its value as one JSON value, and its
// name, summary and description as written, a missing value included.
func TestSameExamples_ComparesEachExampleWhole(t *testing.T) {
	t.Parallel()
	two, twoAgain := num("2"), num("2.0")
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
	one, half := num("1"), num("1.5")
	for _, tc := range []struct {
		name     string
		examples []ir.Example
		refused  bool
	}{
		{"no examples say nothing", nil, false},
		{"an example with no value says nothing", []ir.Example{{Value: &one}, {Summary: "no value"}}, false},
		{"one refused example refuses the keyword", []ir.Example{{Value: &one}, {Value: &half}}, true},
	} {
		g, recorded := stubMerger(narrowingRegistry())
		broad := ir.Property{WireName: "p", Type: primRef(ir.PrimNumber), Examples: tc.examples, Provenance: ptrAt("/b")}
		narrow := ir.Property{WireName: "p", Type: primRef(ir.PrimInteger), Provenance: ptrAt("/a")}

		got := foldTwo(g, narrow, broad, written(`{"examples":[1]}`))

		if !tc.refused {
			assert.Equal(t, tc.examples, got.Examples, "%s: adopted whole", tc.name)
			assert.Empty(t, *recorded, tc.name)
			continue
		}
		assert.Nil(t, got.Examples, "%s: refused whole", tc.name)
		require.Len(t, *recorded, 1, tc.name)
		assert.Contains(t, (*recorded)[0].Message, "does not admit the examples", tc.name)
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/b", "%s: the writer is kept", tc.name)
	}
}
