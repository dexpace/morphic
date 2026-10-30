// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
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

// TestCompile_ANumericFieldIsReadPastARationalsRange pins the validation table's
// reading of a numeric field through the compiler's front door: a magnitude
// math/big will not build as a rational, or no BigVal holds, is judged by its
// sign and integrality alone, and a bound past what a BigVal holds is the
// lowering's to refuse. Only diagnostic codes are asserted, never the lowered
// maxLength, which #1050 tracks.
func TestCompile_ANumericFieldIsReadPastARationalsRange(t *testing.T) {
	t.Parallel()
	const (
		head     = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents: {schemas: {N: "
		mismatch = "openapi/validation/validation-type-mismatch"
		invalid  = "openapi/invalid-numeric-literal"
	)
	cases := []struct {
		name, body string
		want       []string // diagnostic codes of error severity, in order
		pointer    string   // suffix of the first error's pointer, when there is one
	}{
		{"an integer length past a rational", "{type: string, maxLength: 1e1000001}}}", nil, ""},
		{"an integer length past what a BigVal holds", "{type: string, maxLength: 1e1000000000000000000}}}", nil, ""},
		{"a negative step past a rational", "{type: number, multipleOf: -1e1000001}}}", []string{mismatch}, "/multipleOf"},
		{"a bound past a binary float", "{type: number, minimum: 1e2000000000}}}", nil, ""},
		{"a bound past what a BigVal holds", "{type: number, minimum: 1e1000000000000000000}}}", []string{invalid}, "/schemas/N"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: "t.yaml", Data: []byte(head + tc.body + "\n")}}, compilers.Options{})
			require.NoError(t, err)
			require.NotNil(t, doc)

			var got []string
			for _, d := range diags {
				if d.Severity == ir.SeverityError {
					got = append(got, d.Code)
					assert.True(t, strings.HasSuffix(string(d.Provenance.Pointer), tc.pointer), "pointer %q", d.Provenance.Pointer)
				}
			}
			assert.Equal(t, tc.want, got)
		})
	}

	doc := compileComponentSpec(t, head+"{type: number, minimum: 1e2000000000}}}\n")
	sc, ok := doc.Types["t/openapi/components/schemas/N"].(*ir.Scalar)
	require.True(t, ok, "N lowers to a scalar")
	require.NotNil(t, sc.Constraints)
	require.NotNil(t, sc.Constraints.Min)
	assert.Equal(t, ir.BigVal("1e2000000000"), *sc.Constraints.Min)
}

// TestCompile_HoistedSubSchemaConstraintsPreserved pins that a $ref to an internal
// scalar sub-schema keeps that sub-schema's numeric constraints on the hoisted
// alias node the reference resolves to. The hoisted node is distinct from the
// property whose position the sub-schema also occupies, so before the alias
// carried constraints a $ref to a bounded scalar sub-schema silently dropped them.
func TestCompile_HoistedSubSchemaConstraintsPreserved(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\n" +
		"info: {title: t, version: \"1\"}\n" +
		"paths: {}\n" +
		"components:\n" +
		"  schemas:\n" +
		"    Foo:\n" +
		"      type: object\n" +
		"      properties:\n" +
		"        bar: {type: number, minimum: 5, maximum: 10}\n" +
		"    Uses:\n" +
		"      type: object\n" +
		"      properties:\n" +
		"        b: {$ref: '#/components/schemas/Foo/properties/bar'}\n"
	doc := compileComponentSpec(t, spec)

	uses, ok := doc.Types["t/openapi/components/schemas/Uses"].(*ir.Model)
	require.True(t, ok, "Uses lowers to a model")
	var target ir.TypeID
	for _, p := range uses.Properties {
		if p.WireName == "b" {
			target = p.Type.Target
		}
	}
	require.NotEmpty(t, target, "property b resolves to the hoisted sub-schema")

	sc, ok := doc.Types[target].(*ir.Scalar)
	require.True(t, ok, "the hoisted sub-schema is a scalar alias")
	require.NotNil(t, sc.Constraints, "the hoisted scalar keeps its constraints")
	require.NotNil(t, sc.Constraints.Min)
	require.NotNil(t, sc.Constraints.Max)
	assert.Equal(t, ir.BigVal("5"), *sc.Constraints.Min)
	assert.Equal(t, ir.BigVal("10"), *sc.Constraints.Max)
}
