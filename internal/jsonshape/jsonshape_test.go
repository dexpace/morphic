package jsonshape_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/internal/jsonshape"
)

// load type-checks one of the fixture packages under testdata.
func load(t *testing.T, name string) *decls.Package {
	t.Helper()
	p, err := decls.Load("./testdata/" + name)
	require.NoError(t, err)
	return p
}

// goodShape is package good's description: every line is an input the encoder
// reads, so changing any of them changes the description.
const goodShape = `jsonshape 1: good.Root

type Base struct
	"kept" string
	method (Base) IsZero() bool

type ByPointer struct
	"p" string

type ByValue struct
	"v" string
	method (ByValue) MarshalJSON() ([]byte, error)

type Color string
	const "blue"
	const "red"

type Extra struct
	"more" string

type Level int
	const Floor = 2
	const Low = 2
	const High = 10

type Registry map[Color]Sum
	method (*Registry) UnmarshalJSONFrom(*encoding/json/jsontext.Decoder) error

type Root struct
	embed *Extra
	embed Base
	embed Spread
	embed inner
	"Defaulted" int omitempty
	"Untagged" bool
	"aliased" Spread
	"grid" [2][]uint8
	"index" map[Color]*Spread
	"levels" map[Level][]float64
	"named" string
	"options" *int omitzero string
	"pointer" encoding/json/jsontext.Pointer
	"reg" Registry
	"scale" Scale
	"self" []Root
	"sum" Sum
	"tagged" Tagged
	"toggle" Toggle
	method (Root) IsZero() bool

type Scale float64
	const Half = 1/2

type Spread struct
	"item" uint8

type Sum interface
	tag Color
	variant "blue" *ByPointer
	variant "red" ByValue

type Tagged struct
	"under" string

type Toggle bool
	const false
	const true

type encoding/json/jsontext.Pointer string

type inner struct
	"deep" string
`

// TestDescribe_SpellsEveryInput pins the description of a root reaching every
// form: tagged, untagged and ignored members, each kind of embedding, aliases,
// a foreign type, codec methods by receiver and by promotion, constants in
// value order (a string or boolean once by value, a number under each of its
// names), and a sealed sum with each variant once. A type nothing reaches is
// left out.
func TestDescribe_SpellsEveryInput(t *testing.T) {
	t.Parallel()
	got, err := jsonshape.Describe(load(t, "good"), "Root")
	require.NoError(t, err)
	assert.Empty(t, cmp.Diff(goodShape, got))
}

// TestDescribe_IgnoresWhatTheJSONDoesNotCarry holds a description to the
// shape alone. Package reordered redeclares package good in another order,
// with every tagged field, string and boolean constant, parameter and receiver
// renamed, one alias spelled out and another dropped, and describes alike but
// for the package it names.
func TestDescribe_IgnoresWhatTheJSONDoesNotCarry(t *testing.T) {
	t.Parallel()
	good, err := jsonshape.Describe(load(t, "good"), "Root")
	require.NoError(t, err)
	reordered, err := jsonshape.Describe(load(t, "reordered"), "Root")
	require.NoError(t, err)

	body := func(desc string) string {
		_, rest, found := strings.Cut(desc, "\n")
		require.True(t, found, "a description opens with a header line")
		return rest
	}
	assert.Empty(t, cmp.Diff(body(good), body(reordered)))
	assert.NotEqual(t, good, reordered, "the header names the package described")
}

// TestDescribe_IsDeterministic describes one package from two loads, which
// parse its files concurrently and so in no fixed order.
func TestDescribe_IsDeterministic(t *testing.T) {
	t.Parallel()
	first, err := jsonshape.Describe(load(t, "good"), "Root")
	require.NoError(t, err)
	second, err := jsonshape.Describe(load(t, "good"), "Root")
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

// TestDescribe_Refusals reaches each type whose shape a description cannot
// read from its declarations, and requires a refusal naming it rather than a
// description that says less than the encoder reads.
func TestDescribe_Refusals(t *testing.T) {
	t.Parallel()
	p := load(t, "bad")
	for _, tc := range []struct {
		root, want string
	}{
		{"Generic", "Box[int]: a generic type has a shape per instantiation"},
		{"Box", "Box[T any]: a generic type has a shape per instantiation"},
		{"Anonymous", "Anonymous: field A: struct{X int \"json:\\\"x\\\"\"} has no shape"},
		{"Dynamic", "Dynamic: field D: interface{} has no shape"},
		{"Callback", "Callback: field F: func() has no shape"},
		{"Complex", "Complex: field C: complex128 has no JSON encoding"},
		{"ComplexKey", "ComplexKey: field M: complex128 has no JSON encoding"},
		{"Quoted", `Quoted: field Q: json tag "'q,r'" quotes or escapes`},
		{"EmptyOption", `EmptyOption: field E: json tag "e,,omitempty" has an empty option`},
		{"Predeclared", "jsonshape: error: a predeclared type has no declarations"},
		{"Open", "Unsealed: an interface with no unexported method is open"},
		{"NoKind", "KindLess: a sealed interface with no Kind() method"},
		{"ArgKind", "KindWithArg: a sealed interface with no Kind() method"},
		{"VoidKind", "KindVoid: a sealed interface with no Kind() method"},
		{"PairKind", "KindPair: a sealed interface with no Kind() method"},
		{"ShapelessTag", "FuncTagged: func() has no shape"},
		{"Lonely", "Alone: no type implements the sealed interface"},
		{"Computed", "ComputedSum: variant FromField: its Kind method is not a single return of a constant"},
		{"TwoSteps", "TwoStepSum: variant Stepped: its Kind method is not a single return of a constant"},
		{"Trailed", "TrailSum: variant Trailing: its Kind method is not a single return of a constant"},
		{"Panicky", "PanicSum: variant Panics: its Kind method is not a single return of a constant"},
		{"Foreign", "ForeignSum: variant Token: its Kind method is not declared in the package read"},
	} {
		t.Run(tc.root, func(t *testing.T) {
			t.Parallel()
			got, err := jsonshape.Describe(p, tc.root)
			require.Error(t, err)
			assert.Empty(t, got)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestDescribe_RefusesARootItCannotRead covers the preconditions: a package
// to read, and a root that names a type with a name.
func TestDescribe_RefusesARootItCannotRead(t *testing.T) {
	t.Parallel()
	p := load(t, "bad")
	for _, tc := range []struct {
		name string
		pkg  *decls.Package
		root string
		want string
	}{
		{"no package", nil, "Root", "jsonshape: no package"},
		{"an undeclared root", p, "Missing", "declares no type Missing"},
		{"a function", p, "helper", "declares no type helper"},
		{"an alias of an unnamed type", p, "Unnamed", "Unnamed is not a named type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := jsonshape.Describe(tc.pkg, tc.root)
			require.Error(t, err)
			assert.Empty(t, got)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
