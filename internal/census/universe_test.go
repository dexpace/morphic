package census_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/census"
	"github.com/dexpace/morphic/internal/census/testdata/fixture"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// universe is the IR's, derived once: it is read-only, and deriving it asks
// the go command.
var universe = sync.OnceValues(census.NewUniverse)

// fixtureUniverseReport is what an empty census of the fixture's universe
// prints: each fact the fixture declares, in order, with a count of zero.
const fixtureUniverseReport = `documents 0 truncated 0
no-document 0
member 0 0 Circle.radius
member 0 0 Common.id
member 0 0 InArray.a
member 0 0 InMapKey.k
member 0 0 InMapValue.v
member 0 0 InPointer.p
member 0 0 InSlice.s
member 0 0 Leaf.label
member 0 0 Pointed.p
member 0 0 Renamed.r
member 0 0 Root.Options
member 0 0 Root.Untagged
member 0 0 Root.bits
member 0 0 Root.color
member 0 0 Root.inline
member 0 0 Root.keyed
member 0 0 Root.level
member 0 0 Root.name
member 0 0 Root.nested
member 0 0 Root.pair
member 0 0 Root.ptr
member 0 0 Root.renamed
member 0 0 Root.sliced
member 0 0 Root.sum
member 0 0 Root.values
member 0 0 Root.when
member 0 0 Square.side
value 0 0 Bits 1
value 0 0 Bits 2
value 0 0 Bits 4
value 0 0 Color "blue"
value 0 0 Color "red"
value 0 0 Level -20
value 0 0 Level -1
value 0 0 Level 0
value 0 0 Level 10
value 0 0 Level 100
`

// fixturePackage loads the package whose declarations the derivation cases read.
func fixturePackage(t *testing.T, name string) *decls.Package {
	t.Helper()
	p, err := decls.Load("./testdata/" + name)
	require.NoError(t, err)
	return p
}

// fixtureShapes says how to cross the fixture's one sealed sum.
func fixtureShapes() map[reflect.Type][]reflect.Type {
	return map[reflect.Type][]reflect.Type{
		reflect.TypeFor[fixture.Shape](): {reflect.TypeFor[*fixture.Circle](), reflect.TypeFor[fixture.Square]()},
	}
}

func TestDerive_ListsTheFixtureUniverseSorted(t *testing.T) {
	t.Parallel()
	u, err := census.Derive([]reflect.Type{reflect.TypeFor[fixture.Root]()}, fixturePackage(t, "fixture"),
		func(map[string][]string) (map[reflect.Type][]reflect.Type, error) { return fixtureShapes(), nil })
	require.NoError(t, err)

	if diff := cmp.Diff(fixtureUniverseReport, census.New(u).Report()); diff != "" {
		t.Errorf("report (-want +got):\n%s", diff)
	}
}

func TestDerive_Refusals(t *testing.T) {
	t.Parallel()
	opaque := []reflect.Type{reflect.TypeFor[fixture.Opaque]()}
	root := []reflect.Type{reflect.TypeFor[fixture.Root]()}
	errBoom := errors.New("boom")
	none := func(map[string][]string) (map[reflect.Type][]reflect.Type, error) { return nil, nil }

	for _, tc := range []struct {
		name   string
		roots  []reflect.Type
		pkg    string
		expand census.Expand
		want   string
	}{
		{"no root", nil, "fixture", none, "no root type"},
		{"a constant of a kind no one counts", root, "badenum", none, "constant Half has kind Float"},
		{"a hook that fails", root, "fixture",
			func(map[string][]string) (map[reflect.Type][]reflect.Type, error) { return nil, errBoom }, "boom"},
		{"an interface nothing says how to cross", opaque, "fixture", none, "no known implementations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u, err := census.Derive(tc.roots, fixturePackage(t, tc.pkg), tc.expand)
			require.Error(t, err)
			assert.Zero(t, u)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestReach_BoundsTheTypesItVisits(t *testing.T) {
	t.Parallel()
	// One struct with a field of a distinct array type for each of more types
	// than the walk will visit.
	fields := make([]reflect.StructField, census.MaxTypes+1)
	for i := range fields {
		fields[i] = reflect.StructField{Name: fmt.Sprintf("F%d", i), Type: reflect.ArrayOf(i+1, reflect.TypeFor[byte]())}
	}

	names, err := census.Reach([]reflect.Type{reflect.StructOf(fields)}, nil)
	require.Error(t, err)
	assert.Nil(t, names)
	assert.Contains(t, err.Error(), fmt.Sprintf("more than %d types", census.MaxTypes))
}

func TestReach_CrossesEveryContainerOnceAndSkipsForeignStructs(t *testing.T) {
	t.Parallel()
	names, err := census.Reach([]reflect.Type{reflect.TypeFor[fixture.Root]()}, fixtureShapes())
	require.NoError(t, err)

	// Each struct is reached through a container of its own kind alone.
	for _, only := range []string{"InArray.a", "InSlice.s", "InPointer.p", "InMapValue.v", "InMapKey.k", "Leaf.label"} {
		assert.Equal(t, 1, countOf(names, only), only)
	}
	// A struct of another package, and an unnamed one, bring no member.
	for _, name := range names {
		assert.False(t, strings.HasPrefix(name, "Time."), name)
		assert.False(t, strings.HasPrefix(name, "struct"), name)
		assert.NotEqual(t, "Root.TypeCommon", name)
	}
}

// countOf counts how many times want appears in names.
func countOf(names []string, want string) int {
	n := 0
	for _, name := range names {
		if name == want {
			n++
		}
	}
	return n
}

func TestEnumerations_ListsTheTypesOfThePackageThatDeclareConstants(t *testing.T) {
	t.Parallel()
	got, err := census.Enumerations(fixturePackage(t, "fixture"))
	require.NoError(t, err)

	// Crimson repeats Red's value, so it adds none. Neither the constant of
	// time.Duration nor the untyped one nor the one of type int is a value of
	// a type this package declares.
	want := []string{
		`Bits 1`, `Bits 2`, `Bits 4`,
		`Color "blue"`, `Color "red"`,
		`Level -20`, `Level -1`, `Level 0`, `Level 10`, `Level 100`,
		`Unreached "never"`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("enumerations (-want +got):\n%s", diff)
	}
}

func TestExpandTypeDef_Refusals(t *testing.T) {
	t.Parallel()
	require.NoError(t, census.ExpandTypeDef("TypeKind", "model", "union"))

	require.ErrorContains(t, census.ExpandTypeDef("Severity", "error"), "declares no TypeKind")
	require.ErrorContains(t, census.ExpandTypeDef("TypeKind", "model", "hologram"),
		`TypeKind "hologram" has no registered TypeDef`)
}

func TestHeld_SplitsABitSetIntoItsBits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		spelling string
		values   []string
		want     []string
	}{
		{"one bit", "2", []string{"1", "2", "4"}, []string{"2"}},
		{"two bits", "5", []string{"1", "2", "4"}, []string{"1", "4"}},
		{"no bit", "0", []string{"1", "2", "4"}, nil},
		{"a bit no constant names", "8", []string{"1", "2", "4"}, []string{"8"}},
		{"named bits and one that is not", "11", []string{"1", "2", "4"}, []string{"1", "2", "8"}},
		{"a negative number", "-1", []string{"1", "2", "4"}, []string{"-1"}},
		{"a value of a type with a zero constant", "3", []string{"0", "1", "2"}, []string{"3"}},
		{"a value of a type with a constant of two bits", "3", []string{"1", "3"}, []string{"3"}},
		{"a value of a type with a negative constant", "3", []string{"-1", "1"}, []string{"3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, census.Held(tc.spelling, tc.values...))
		})
	}
}

func TestNewUniverse_ContainsTheIRsFacts(t *testing.T) {
	t.Parallel()
	u, err := universe()
	require.NoError(t, err)
	got := facts(t, census.New(u).Report())

	for _, want := range []string{
		"member Model.base",
		"member Union.discriminator",
		"member TypeCommon.docs",
		"member Operation.params",
		`value TypeKind "tuple"`,
		`value UsageFlags 8`,
	} {
		assert.Equal(t, "0 0", got[want], want)
	}
}

func TestNewUniverse_ListsEveryEmbeddedStructOnceUnderItsOwnName(t *testing.T) {
	t.Parallel()
	u, err := universe()
	require.NoError(t, err)
	got := facts(t, census.New(u).Report())

	assert.Contains(t, got, "member TypeCommon.id")
	assert.Contains(t, got, "member Primitive.prim")
	for _, repeated := range []string{"Primitive.id", "Primitive.docs", "Model.id", "Model.provenance", "Model.TypeCommon"} {
		assert.NotContains(t, got, "member "+repeated, "TypeCommon's members are not repeated under its embedders")
	}
}

func TestNewUniverse_NamesOnlyStructsOfPackageIR(t *testing.T) {
	t.Parallel()
	u, err := universe()
	require.NoError(t, err)
	p, err := decls.Load(reflect.TypeFor[ir.Document]().PkgPath())
	require.NoError(t, err)

	var members int
	for fact := range facts(t, census.New(u).Report()) {
		kind, name, _ := strings.Cut(fact, " ")
		if kind != "member" {
			continue
		}
		members++
		owner, _, _ := strings.Cut(name, ".")
		_, err := p.Named(owner)
		assert.NoError(t, err, "%s is not a type package ir declares", name)
	}
	assert.Positive(t, members)
}

func TestNewUniverse_FailsOutsideTheModule(t *testing.T) {
	// Not parallel: t.Chdir moves the whole process, and the universe is read
	// from the working directory's module.
	t.Chdir(t.TempDir())

	u, err := census.NewUniverse()
	require.Error(t, err)
	assert.Zero(t, u)
	assert.Contains(t, err.Error(), "census:")
}
