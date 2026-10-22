package merge

import (
	"encoding/json/jsontext"
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

// TestFold_UnmodeledUnionsLaterWinning pins what the field keeps of every
// declaration's Unmodeled: each entry, since the keys are namespaced, and a
// later declaration's on a key two share.
func TestFold_UnmodeledUnionsLaterWinning(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(narrowingRegistry())
	entry := func(v string) ir.UnmodeledEntry { return ir.UnmodeledEntry{Value: ir.RawValue(v)} }
	first, second := kindDecl(ir.PrimString, nil), kindDecl(ir.PrimString, nil)
	first.prop.Unmodeled = ir.Unmodeled{"x-a": entry(`1`), "x-shared": entry(`"first"`)}
	second.prop.Unmodeled = ir.Unmodeled{"x-b": entry(`2`), "x-shared": entry(`"second"`)}

	got := g.fold([]declaration{first, second})

	assert.Equal(t, ir.Unmodeled{"x-a": entry(`1`), "x-b": entry(`2`), "x-shared": entry(`"second"`)}, got.Unmodeled)
	assert.Empty(t, *recorded)
}
