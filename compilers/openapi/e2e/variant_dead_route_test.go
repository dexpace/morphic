// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// deadRouteSpelling is one place a discriminator stands with a union holding a
// dead branch: the schema it is written in, what the dead branch's mapping
// entry is kept under on the Union, and where that entry is written.
type deadRouteSpelling struct {
	name, schema, key, at string
}

// deadRouteSpellings writes P's union of branches under keyword, with a
// discriminator whose mapping is mapping: beside an object body, on an allOf
// entry beside the union, and beside the allOf whose entry writes the union.
func deadRouteSpellings(keyword, branches, mapping string) []deadRouteSpelling {
	disc := "discriminator: {propertyName: kind" + mapping + "}"
	union := keyword + ": [" + branches + "]"
	return []deadRouteSpelling{
		{name: "beside", schema: "P: {type: object, properties: {kind: {type: string}}, " + union + ", " + disc + "}",
			key: "openapi:discriminator/mapping/s", at: "/components/schemas/P/discriminator/mapping/s"},
		{name: "entry", schema: "P: {allOf: [{$ref: '#/components/schemas/Kind'}, {" + union + ", " + disc + "}]}",
			key: "openapi:allOf/1/discriminator/mapping/s", at: "/components/schemas/P/allOf/1/discriminator/mapping/s"},
		{name: "allOf", schema: "P: {allOf: [{$ref: '#/components/schemas/Kind'}, {" + union + "}], " + disc + "}",
			key: "openapi:discriminator/mapping/s", at: "/components/schemas/P/discriminator/mapping/s"},
	}
}

// deadRouteComponents are the schemas the cases name: an object branch, a
// string branch no object satisfies, the body an entry's union conjoins with,
// and an object no branch names.
var deadRouteComponents = []string{
	"A: {type: object, properties: {kind: {type: string}, a: {type: string}}}",
	"Str: {type: string}",
	"Kind: {type: object, properties: {kind: {type: string}}}",
	"Other: {type: object, properties: {kind: {type: string}}}",
}

// TestCompile_AMappingToADeadBranchIsNoRoute holds that a discriminator
// mapping entry naming the component a dead branch names routes nowhere: the
// Discriminator leaves it out, the Union keeps it verbatim under the key its
// path from the position gives, and the one warning of the dead branch names
// it, with the name OpenAPI routes by, so the document compiles, its passes
// run, with no error. The live branch's entry still routes. Under oneOf and
// anyOf, at 3.0.3 and 3.1.0, in both orders of the branches, through the
// compiler and the engine (docs/compilers/openapi.md#discriminator).
func TestCompile_AMappingToADeadBranchIsNoRoute(t *testing.T) {
	t.Parallel()
	mapping := ", mapping: {a: '#/components/schemas/A', s: '#/components/schemas/Str'}"
	for _, version := range bothVersions {
		for _, keyword := range []string{"oneOf", "anyOf"} {
			for _, order := range deadRouteOrders {
				for _, sp := range deadRouteSpellings(keyword, order, mapping) {
					spec := admissionSpec(version, "", append([]string{sp.schema}, deadRouteComponents...)...)
					t.Run(strings.Join([]string{version, keyword, sp.name, order}, "/"), func(t *testing.T) {
						t.Parallel()
						res := harness.Check(t.Context(), "spec.yaml", []byte(spec))
						require.Equal(t, harness.OutcomeOK, res.Outcome, res.Detail)
						doc, diags := parseVerified(t, spec)
						requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct,
							pointer: "/components/schemas/P", contains: []string{
								`no tag of the discriminator routes to it (mapping "s", the name "Str")`,
								"the routes it writes kept verbatim under Unmodeled"}}})
						union, ok := kindNode(t, doc, componentID("P")).(*ir.Union)
						require.True(t, ok)
						require.Len(t, union.Variants, 1)
						require.NotNil(t, union.Discriminator)
						assert.Equal(t, map[string]ir.TypeID{"a": union.Variants[0].Type.Target},
							union.Discriminator.Mapping, "the live branch routes, the dead one does not")
						kept, ok := union.Unmodeled[sp.key]
						require.True(t, ok, "the entry is kept on the union: %v", keptOn(union))
						assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
						assert.JSONEq(t, `"#/components/schemas/Str"`, string(kept.Value))
						assert.Equal(t, sp.at, string(kept.Provenance.Pointer))
						compiled, compiledDiags := compileAnnotationSpec(t, "route", spec)
						requireErrorsAndWarnings(t, compiledDiags, []wantFinding{{code: diag.DegradedConstruct,
							pointer: "/components/schemas/P"}})
						assert.Equal(t, union, compiled.Types[componentID("P")], "the passes change nothing here")
					})
				}
			}
		}
	}
}

// deadRouteOrders are the two orders of a live and a dead branch.
var deadRouteOrders = []string{"{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/Str'}",
	"{$ref: '#/components/schemas/Str'}, {$ref: '#/components/schemas/A'}"}

// TestCompile_TheNameOfADeadBranchIsNoRoute holds that the route OpenAPI
// implies by the name of the component a dead branch names, with no mapping
// entry keying it, is none either: the Mapping holds the live branch's name
// alone, nothing but the branch is kept, as nothing else is written, and the
// warning names it, in both orders of the branches.
func TestCompile_TheNameOfADeadBranchIsNoRoute(t *testing.T) {
	t.Parallel()
	for _, version := range bothVersions {
		for _, order := range deadRouteOrders {
			for _, sp := range deadRouteSpellings("oneOf", order, "") {
				spec := admissionSpec(version, "", append([]string{sp.schema}, deadRouteComponents...)...)
				t.Run(strings.Join([]string{version, sp.name, order}, "/"), func(t *testing.T) {
					t.Parallel()
					doc, diags := parseVerified(t, spec)
					requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct,
						pointer: "/components/schemas/P", contains: []string{
							`no tag of the discriminator routes to it (the name "Str")`}}})
					for _, diagnostic := range diags {
						assert.NotContains(t, diagnostic.Message, "routes it writes", "nothing is written to keep")
					}
					union, ok := kindNode(t, doc, componentID("P")).(*ir.Union)
					require.True(t, ok)
					require.Len(t, union.Variants, 1)
					assert.Equal(t, map[string]ir.TypeID{"A": union.Variants[0].Type.Target},
						union.Discriminator.Mapping)
					kept := keptOn(union)
					require.Len(t, kept, 1, "the dead branch alone is kept: %v", kept)
					for key := range kept {
						assert.True(t, strings.HasPrefix(key, "openapi:oneOf/"), key)
					}
				})
			}
		}
	}
}

// TestCompile_ADefaultMappingToADeadBranchIsNoRoute holds that a 3.2
// defaultMapping naming the component a dead branch names is no default: the
// Discriminator holds none, the Union keeps it verbatim, and the warning names
// it, with no error.
func TestCompile_ADefaultMappingToADeadBranchIsNoRoute(t *testing.T) {
	t.Parallel()
	for _, order := range deadRouteOrders {
		sp := deadRouteSpellings("oneOf", order, ", defaultMapping: '#/components/schemas/Str'")[0]
		spec := admissionSpec("3.2.0", "", append([]string{sp.schema}, deadRouteComponents...)...)
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseVerified(t, spec)
			requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct,
				pointer: "/components/schemas/P", contains: []string{
					`no tag of the discriminator routes to it (defaultMapping, the name "Str")`}}})
			union, ok := kindNode(t, doc, componentID("P")).(*ir.Union)
			require.True(t, ok)
			require.NotNil(t, union.Discriminator)
			assert.Empty(t, union.Discriminator.Default)
			kept, ok := union.Unmodeled["openapi:discriminator/defaultMapping"]
			require.True(t, ok, "%v", keptOn(union))
			assert.JSONEq(t, `"#/components/schemas/Str"`, string(kept.Value))
			assert.Equal(t, "/components/schemas/P/discriminator/defaultMapping", string(kept.Provenance.Pointer))
		})
	}
}

// TestCompile_ADiscriminatorBesideAUnionNoBranchOfWhichLivesIsKeptWhole holds
// that where every branch is dead, so the union is kept whole, the
// discriminator standing with it routes nothing and is kept whole too, as the
// tree keeps one beside a union it cannot route: on the Model beside the body
// or the allOf, and with its entry on an entry, with no error.
func TestCompile_ADiscriminatorBesideAUnionNoBranchOfWhichLivesIsKeptWhole(t *testing.T) {
	t.Parallel()
	mapping := ", mapping: {s: '#/components/schemas/Str'}"
	for _, version := range bothVersions {
		for _, order := range []string{"{$ref: '#/components/schemas/Str'}, {type: boolean}",
			"{type: boolean}, {$ref: '#/components/schemas/Str'}"} {
			for _, sp := range deadRouteSpellings("oneOf", order, mapping) {
				spec := admissionSpec(version, "", append([]string{sp.schema}, deadRouteComponents...)...)
				t.Run(strings.Join([]string{version, sp.name, order}, "/"), func(t *testing.T) {
					t.Parallel()
					doc, diags := parseVerified(t, spec)
					for _, diagnostic := range diags {
						assert.NotEqual(t, ir.SeverityError, diagnostic.Severity, "%s %s", diagnostic.Code, diagnostic.Message)
					}
					model, ok := kindNode(t, doc, componentID("P")).(*ir.Model)
					require.True(t, ok)
					assert.Nil(t, model.Discriminator, "the discriminator routes nothing")
					kept, ok := model.Unmodeled["openapi:discriminator"]
					require.True(t, ok, "it is kept whole: %v", keptOn(model))
					assert.Equal(t, strings.TrimSuffix(sp.at, "/mapping/s"), string(kept.Provenance.Pointer))
				})
			}
		}
	}
}
