package decls_test

import (
	"go/types"
	"iter"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// goodDir holds the packages every case reads. They are siblings so that one
// pattern, goodDir+"/...", matches all of them and nothing that fails to
// type-check.
const goodDir = "./testdata/good"

// noPackages is a wildcard under the module that matches nothing, which the go
// command reports as a warning rather than an error.
const noPackages = "github.com/dexpace/morphic/internal/decls/testdata/nowhere/..."

// fixture loads the package whose declarations most cases read.
func fixture(t *testing.T) *decls.Package {
	t.Helper()
	p, err := decls.Load(goodDir + "/fixture")
	require.NoError(t, err)
	return p
}

func TestLoad_Refusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, pattern, want string
	}{
		{"a package that does not type-check", "./testdata/broken", "broken.go:4"},
		{"a package with no production file", "./testdata/testonly", "testonly has no production Go file"},
		{"a package the build thins", "./testdata/constrained", "the current build leaves out left_out.go"},
		{"a pattern matching several", goodDir + "/...", "matched 3 packages, want 1"},
		{"a pattern matching none", noPackages, "matched no packages"},
		{"a pattern the go command refuses", "-notaflag", "load -notaflag: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := decls.Load(tc.pattern)
			require.Error(t, err)
			assert.Nil(t, p)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestLoad_ReportsAMissingPackageOnce holds a refusal to its cause: a package
// the go command cannot find has no file either, and saying so too would only
// bury the reason.
func TestLoad_ReportsAMissingPackageOnce(t *testing.T) {
	t.Parallel()
	p, err := decls.Load("./testdata/nowhere")

	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "directory not found")
	assert.NotContains(t, err.Error(), "no production Go file")
}

// TestLoad_ReadsOnlyProductionGoFiles holds the load to production Go sources.
// Beside fixture.go, the fixture holds a test file, a test file no build
// includes and an assembly file no build includes. None is read, and none is
// refused as left out: no build reads a test file, and assembly declares
// nothing the type checker sees.
func TestLoad_ReadsOnlyProductionGoFiles(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"fixture.go"}, fileNames(fixture(t)))
}

// TestLoad_KeepsFilesInNameOrder holds Syntax to the order its doc promises,
// which the loader keeps although it parses the files concurrently.
func TestLoad_KeepsFilesInNameOrder(t *testing.T) {
	t.Parallel()
	p, err := decls.Load(goodDir + "/split")
	require.NoError(t, err)
	assert.Equal(t, []string{"a.go", "b.go", "c.go", "d.go"}, fileNames(p))
}

// fileNames returns the base name of each file in p.Syntax, in its order.
func fileNames(p *decls.Package) []string {
	out := make([]string, len(p.Syntax))
	for i, f := range p.Syntax {
		out[i] = filepath.Base(p.Fset.Position(f.Pos()).Filename)
	}
	return out
}

// TestLoad_ReportsTheGoCommandFailing covers the error packages.Load returns
// itself, as opposed to one it records on a package: the go command refusing
// to run, for the load and for the question LoadAll asks of each of several
// patterns. It sets the environment, so it cannot run in parallel.
func TestLoad_ReportsTheGoCommandFailing(t *testing.T) {
	t.Setenv("GOFLAGS", "-mod=bogus")
	for _, patterns := range [][]string{
		{goodDir + "/fixture"},
		{goodDir + "/fixture", goodDir + "/uses"},
	} {
		pkgs, err := decls.LoadAll(patterns...)

		require.Error(t, err, patterns)
		assert.Nil(t, pkgs)
		assert.Contains(t, err.Error(), "load "+strings.Join(patterns, " ")+": ")
		assert.Contains(t, err.Error(), "bogus", "the go command's own complaint is kept")
	}
}

// TestLoad_IgnoresAnExternalDriver holds the load to the go command, whatever
// driver the environment names: one that cannot run must not fail it, for the
// load or for the question LoadAll asks of each of several patterns. It sets
// the environment, so it cannot run in parallel.
func TestLoad_IgnoresAnExternalDriver(t *testing.T) {
	t.Setenv("GOPACKAGESDRIVER", filepath.Join(t.TempDir(), "gopackagesdriver"))
	for _, patterns := range [][]string{
		{goodDir + "/fixture"},
		{goodDir + "/fixture", goodDir + "/uses"},
	} {
		pkgs, err := decls.LoadAll(patterns...)

		require.NoError(t, err, patterns)
		assert.Len(t, pkgs, len(patterns))
	}
}

func TestLoadAll_RefusesNoPattern(t *testing.T) {
	t.Parallel()
	pkgs, err := decls.LoadAll()

	require.Error(t, err)
	assert.Nil(t, pkgs)
	assert.Contains(t, err.Error(), "no pattern")
}

// TestLoadAll_RefusesAPatternMatchingNothing holds each of several patterns to
// naming a package. The go command only warns about one that names none, so
// the load would otherwise come back with less than was asked for.
func TestLoadAll_RefusesAPatternMatchingNothing(t *testing.T) {
	t.Parallel()
	pkgs, err := decls.LoadAll(goodDir+"/fixture", noPackages)

	require.Error(t, err)
	assert.Nil(t, pkgs)
	assert.Contains(t, err.Error(), noPackages+" matched no packages")
}

// TestLoadAll_ReturnsPackagesSortedByPath holds the order to the import paths
// rather than to the order the patterns were given or the loader finished.
func TestLoadAll_ReturnsPackagesSortedByPath(t *testing.T) {
	t.Parallel()
	pkgs, err := decls.LoadAll(goodDir+"/uses", goodDir+"/split", goodDir+"/fixture")
	require.NoError(t, err)

	paths := make([]string, len(pkgs))
	for i, p := range pkgs {
		paths[i] = p.Types.Path()
	}
	prefix := "github.com/dexpace/morphic/internal/decls/testdata/good/"
	assert.Equal(t, []string{prefix + "fixture", prefix + "split", prefix + "uses"}, paths)
}

// TestLoadAll_SharesOneTypeUniverse holds what separate Load calls cannot give:
// the type a package declares is the type its importer sees, so identity is a
// types.Identical call and not a comparison of import path and name.
func TestLoadAll_SharesOneTypeUniverse(t *testing.T) {
	t.Parallel()
	pkgs, err := decls.LoadAll(goodDir+"/uses", goodDir+"/fixture")
	require.NoError(t, err)
	require.Len(t, pkgs, 2)
	declaring, importing := pkgs[0], pkgs[1]
	require.Equal(t, "fixture", declaring.Types.Name())
	require.Equal(t, "uses", importing.Types.Name())

	color, err := declaring.Named("Color")
	require.NoError(t, err)
	favourite := importing.Types.Scope().Lookup("Favourite")
	require.NotNil(t, favourite)
	assert.True(t, types.Identical(color, favourite.Type()), "the importer sees the declaring package's own Color")

	separate := fixture(t)
	other, err := separate.Named("Color")
	require.NoError(t, err)
	assert.False(t, types.Identical(color, other), "a separate load builds a universe of its own")
}

// TestStringConsts_ReadsEverySpelling holds the reader to the compiler: a
// constant typed through an alias, written as an expression, or converted from
// an untyped string is one of the type's, in the order the source declares it.
func TestStringConsts_ReadsEverySpelling(t *testing.T) {
	t.Parallel()
	got, err := fixture(t).StringConsts("Color")
	require.NoError(t, err)
	assert.Equal(t, []decls.Const{{"Red", "red"}, {"Green", "green"}, {"Blue", "blue"}}, got)

	viaAlias, err := fixture(t).StringConsts("Alias")
	require.NoError(t, err)
	assert.Equal(t, got, viaAlias, "an alias names the same type")
}

// TestStringConsts_FollowsDeclarationOrderAcrossFiles holds the order to the
// source, not to the names and not to the token positions the loader hands out,
// which order files by whichever it finished parsing first. The constants are
// declared in the reverse of their alphabetical order, one file each but the
// first, so each of those orders gives a different answer.
func TestStringConsts_FollowsDeclarationOrderAcrossFiles(t *testing.T) {
	t.Parallel()
	p, err := decls.Load(goodDir + "/split")
	require.NoError(t, err)

	got, err := p.StringConsts("Level")

	require.NoError(t, err)
	assert.Equal(t, []decls.Const{
		{"Zulu", "zulu"}, {"Alpha", "alpha"}, {"Yankee", "yankee"}, {"Xray", "xray"}, {"Whiskey", "whiskey"},
	}, got)
}

func TestStringConsts_Refusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ typeName, want string }{
		{"Count", "One has kind Int, not String"},
		{"Missing", "declares no type Missing"},
		{"Bytes", "Bytes is not a named type"},
		{"Hello", "declares no type Hello"},
	} {
		got, err := fixture(t).StringConsts(tc.typeName)
		require.Error(t, err, tc.typeName)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), tc.want)
	}
}

// TestObjects_YieldsOneKindInNameOrder holds Objects to its kind and to name
// order, and to stopping when the loop over it does.
func TestObjects_YieldsOneKindInNameOrder(t *testing.T) {
	t.Parallel()
	p := fixture(t)

	assert.Equal(t, []string{"Blue", "Green", "One", "Plain", "Red"}, names(p.Objects[*types.Const]()))
	assert.Equal(t, []string{"Alias", "Bytes", "Color", "Count", "Shade", "Wrapper"},
		names(p.Objects[*types.TypeName]()))
	assert.Equal(t, []string{"Hello"}, names(p.Objects[*types.Func]()))
	assert.Equal(t, []string{
		"Alias", "Blue", "Bytes", "Color", "Count", "Green", "Hello", "One", "Plain", "Red", "Shade", "Wrapper",
	}, names(p.Objects[types.Object]()))

	var first []string
	for c := range p.Objects[*types.Const]() {
		first = append(first, c.Name())
		break
	}
	assert.Equal(t, []string{"Blue"}, first)
}

// names collects the names of the objects seq yields.
func names[T types.Object](seq iter.Seq[T]) []string {
	var out []string
	for obj := range seq {
		out = append(out, obj.Name())
	}
	return out
}

// TestTypesDeclaring_CountsOnlyOwnMethods holds the reader to what a sealed
// sum needs: Wrapper reaches Method by embedding and is not listed, an alias
// adds no type, and a pointer receiver declares as much as a value one.
func TestTypesDeclaring_CountsOnlyOwnMethods(t *testing.T) {
	t.Parallel()
	p := fixture(t)
	assert.Equal(t, []string{"Color", "Shade"}, p.TypesDeclaring("Method"))
	assert.Empty(t, p.TypesDeclaring("Missing"))
}
