// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package census_test

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/census"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// universe is the IR's, derived once: it is read-only, and deriving it asks
// the go command.
var universe = sync.OnceValues(census.NewUniverse)

// fixtureUniverseReport is what an empty census of the fixture's universe
// prints: each fact Root reaches, in order, with a count of zero. Crimson
// repeats Red's value, so it adds none; the constants of time.Duration and
// reflect.Kind belong to another package; an embedding adds no member of its
// own.
const fixtureUniverseReport = `documents 0
no-document 0
truncated 0
member 0 0 Circle.radius
member 0 0 Common.id
member 0 0 Leaf.label
member 0 0 Root.Untagged
member 0 0 Root.color
member 0 0 Root.flag
member 0 0 Root.grants
empty 0 0 Root.grants
member 0 0 Root.index
empty 0 0 Root.index
member 0 0 Root.inline
member 0 0 Root.labels
member 0 0 Root.leaf
member 0 0 Root.leaves
member 0 0 Root.level
member 0 0 Root.name
member 0 0 Root.reflect
member 0 0 Root.shapes
member 0 0 Root.sum
member 0 0 Root.timeout
member 0 0 Square.side
value 0 0 Color "blue"
value 0 0 Color "red"
value 0 0 Kind "circle"
value 0 0 Kind "hexagon"
value 0 0 Kind "square"
value 0 0 Level -20
value 0 0 Level -1
value 0 0 Level 0
value 0 0 Level 10
value 0 0 Level 100
`

// fixturePackage loads one of the packages under testdata.
func fixturePackage(t *testing.T, name string) *decls.Package {
	t.Helper()
	p, err := decls.Load("./testdata/" + name)
	require.NoError(t, err)
	return p
}

// fixtureUniverse is the universe of the fixture's Root.
func fixtureUniverse(t *testing.T) census.Universe {
	t.Helper()
	u, err := census.UniverseOf(fixturePackage(t, "fixture"), "Root")
	require.NoError(t, err)
	return u
}

func TestUniverseOf_ListsEveryFactTheRootReaches(t *testing.T) {
	t.Parallel()
	got := census.New(fixtureUniverse(t)).Report()
	if diff := cmp.Diff(fixtureUniverseReport, got); diff != "" {
		t.Errorf("report (-want +got):\n%s", diff)
	}
}

func TestUniverseOf_Refusals(t *testing.T) {
	t.Parallel()
	p := fixturePackage(t, "fixture")
	for _, tc := range []struct {
		root, want string
	}{
		{"Missing", "census: jsonshape: "},
		{"Hidden", "the encoder does not write Hidden.hidden"},
		{"Skipped", "the encoder does not write Skipped.Dropped"},
		{"Scaled", "constant Half has kind Float, want String or Int"},
		{"Loose", "Plain is tagged by string, which is no type of its package"},
		{"Monthly", "Dated is tagged by time.Month, which is no type of its package"},
		{"Bare", "Unlisted is tagged by Tagless, which declares no constants"},
	} {
		t.Run(tc.root, func(t *testing.T) {
			t.Parallel()
			u, err := census.UniverseOf(p, tc.root)
			require.Error(t, err)
			assert.Zero(t, u)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestCheckVersion_AcceptsOnlyTheBuildsOwnVersion(t *testing.T) {
	t.Parallel()
	require.NoError(t, census.CheckVersion(fixturePackage(t, "fixture"), "1.2.3"))

	for _, tc := range []struct {
		name, pkg, want string
	}{
		{"another version", "fixture", "this build reads IRVersion 0.0.1, but the declarations it read are of 1.2.3"},
		{"no IRVersion", "absent", "declares no IRVersion string constant"},
		{"an IRVersion that is not a string", "numeric", "declares no IRVersion string constant"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := census.CheckVersion(fixturePackage(t, tc.pkg), "0.0.1")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
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
		"member Operation.auth",
		"empty Operation.auth",
		`value TypeKind "tuple"`,
		`value StreamingMode "none"`,
	} {
		assert.Equal(t, "0 0", got[want], want)
	}
	assert.NotContains(t, got, "empty Operation.params", "an omitempty slice's emptiness is its absence")
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

func TestNewUniverse_NamesOnlyTypesOfPackageIR(t *testing.T) {
	t.Parallel()
	u, err := universe()
	require.NoError(t, err)
	p, err := decls.Load(reflect.TypeFor[ir.Document]().PkgPath())
	require.NoError(t, err)

	var named int
	for fact := range facts(t, census.New(u).Report()) {
		kind, rest, _ := strings.Cut(fact, " ")
		owner, _, _ := strings.Cut(rest, ".")
		if kind == "value" {
			owner, _, _ = strings.Cut(rest, " ")
		}
		if kind == "documents" || kind == "no-document" || kind == "truncated" {
			continue
		}
		named++
		_, err := p.Named(owner)
		assert.NoError(t, err, "%s is not a type package ir declares", fact)
	}
	assert.Positive(t, named)
}

func TestNewUniverse_RefusesDeclarationsOfAnotherVersion(t *testing.T) {
	t.Parallel()
	u, err := census.NewUniverseOfVersion("0.0.1")
	require.Error(t, err)
	assert.Zero(t, u)
	assert.Contains(t, err.Error(), "this build reads IRVersion 0.0.1, but the declarations it read are of "+ir.IRVersion)
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
