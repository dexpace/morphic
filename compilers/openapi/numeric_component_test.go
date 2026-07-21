package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// compileComponentSpec compiles an inline spec and fails on any error diagnostic.
func compileComponentSpec(t *testing.T, spec string) *ir.Document {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "t.yaml", Data: []byte(spec)}}, compilers.Options{})
	require.NoError(t, err)
	for _, d := range diags {
		require.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
	require.NotNil(t, doc)
	return doc
}

// TestCompile_ComponentScalarConstraintsPreserved pins that a top-level scalar
// component keeps its numeric constraints losslessly: a magnitude beyond float64
// range and a valid leading-dot spelling both survive to the exact BigVal,
// whether the component declares a type or not. A float64 path silently dropped
// the first and rejected the second, and the attachment path dropped both.
func TestCompile_ComponentScalarConstraintsPreserved(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents: {schemas: {N: "
	cases := []struct {
		name    string
		body    string
		wantMin ir.BigVal
	}{
		{"typeless-beyond-float64", "{minimum: 1.8e308}}}", "1.8e308"},
		{"leading-dot-canonicalized", "{minimum: .5}}}", "0.5"},
		{"typed-beyond-float64", "{type: number, minimum: 1.8e308}}}", "1.8e308"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := compileComponentSpec(t, head+tc.body+"\n")
			sc, ok := doc.Types["t/openapi/components/schemas/N"].(*ir.Scalar)
			require.True(t, ok, "N lowers to a scalar")
			require.NotNil(t, sc.Constraints, "component scalar keeps its constraints")
			require.NotNil(t, sc.Constraints.Min)
			assert.Equal(t, tc.wantMin, *sc.Constraints.Min)
		})
	}
}
