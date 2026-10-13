package decls_test

import (
	"go/types"
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

func TestLoad_RefusesWhatIsNotOnePackage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, pattern, want string
	}{
		{"a package that does not type-check", "./testdata/broken", "load ./testdata/broken: "},
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

// TestLoad_ReportsTheGoCommandFailing covers the error packages.Load returns
// itself, as opposed to one it records on a package: the go command refusing
// to run. It sets the environment, so it cannot run in parallel.
func TestLoad_ReportsTheGoCommandFailing(t *testing.T) {
	t.Setenv("GOFLAGS", "-mod=bogus")

	p, err := decls.Load(goodDir + "/fixture")

	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "-mod=bogus")
}

func TestLoadAll_RefusesNoPattern(t *testing.T) {
	t.Parallel()
	pkgs, err := decls.LoadAll()

	require.Error(t, err)
	assert.Nil(t, pkgs)
	assert.Contains(t, err.Error(), "no pattern")
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
		{"Count", "One is a Int constant, not a string"},
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

// TestTypesDeclaring_CountsOnlyOwnMethods holds the reader to what a sealed
// sum needs: Wrapper reaches Method by embedding and is not listed, an alias
// adds no type, and a pointer receiver declares as much as a value one.
func TestTypesDeclaring_CountsOnlyOwnMethods(t *testing.T) {
	t.Parallel()
	p := fixture(t)
	assert.Equal(t, []string{"Color", "Shade"}, p.TypesDeclaring("Method"))
	assert.Empty(t, p.TypesDeclaring("Missing"))
}

func TestFuncDecl(t *testing.T) {
	t.Parallel()
	p := fixture(t)

	fd, err := p.FuncDecl("Hello")
	require.NoError(t, err)
	assert.Equal(t, "Hello", fd.Name.Name)

	for _, name := range []string{"Missing", "Color", "Method"} {
		fd, err := p.FuncDecl(name)
		require.Error(t, err, name)
		assert.Nil(t, fd)
		assert.Contains(t, err.Error(), "declares no function "+name)
	}
}
