package decls_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// fixture loads testdata/fixture, whose declarations every case reads.
func fixture(t *testing.T) *decls.Package {
	t.Helper()
	p, err := decls.Load("./testdata/fixture")
	require.NoError(t, err)
	return p
}

func TestLoad_RefusesWhatIsNotOnePackage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, pattern, want string
	}{
		{"a package that does not type-check", "./testdata/broken", "load ./testdata/broken: "},
		{"a pattern matching several", "unicode/...", "matched 3 packages, want 1"},
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

	p, err := decls.Load("./testdata/fixture")

	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "-mod=bogus")
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
