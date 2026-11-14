// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
)

// TestCompile_ABaseTenIntegerIsReadInBaseTen holds a number written with a leading
// zero to YAML 1.2's core schema, which reads it in base ten, wherever a
// number is read: a default and a bound. The spellings the core schema does not
// know as numbers are strings, and give the IR their quoted twin gives.
func TestCompile_ABaseTenIntegerIsReadInBaseTen(t *testing.T) {
	t.Parallel()
	body := func(mask, big string) string {
		return srMin + "components:\n  schemas:\n    S:\n      type: object\n      properties:\n" +
			"        mode: {type: integer, default: 0644, maximum: 0777}\n" +
			"        mask: {type: string, default: " + mask + "}\n" +
			"        big: {type: string, default: " + big + "}\n"
	}

	doc, diags := compileText(t, "bases.yaml", body("0b1010", "1_000"), openapi.Options{})
	twin, twinDiags := compileText(t, "bases.yaml", body(`"0b1010"`, `"1_000"`), openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, byCode(diags, srVersionReading), "a document declaring nothing draws no warning")
	assert.Empty(t, diags)
	tree := irTree(t, doc)
	mode := findByID(tree, "p/openapi/components/schemas/S/properties/mode")
	require.NotNil(t, mode)
	assert.Equal(t, map[string]any{"kind": "number", "num": "644"}, mode["default"], "0644 is 644, not 420")
	assert.True(t, containsJSON(t, doc, `"max":"777"`), "0777 is 777, not 511")
	assert.Equal(t, map[string]any{"kind": "string", "str": "0b1010"},
		findByID(tree, "p/openapi/components/schemas/S/properties/mask")["default"])
	assert.Equal(t, map[string]any{"kind": "string", "str": "1_000"},
		findByID(tree, "p/openapi/components/schemas/S/properties/big")["default"])
	assert.Equal(t, jsonOf(t, twin), jsonOf(t, doc))
	assert.Equal(t, lines(twinDiags), lines(diags))
}

// TestCompile_NoScalarTextOrKeyIsRewrittenByTheCoreSchema holds a number-like
// key, a required entry that names it and a $ref to it to the text written, as
// they were before the scalars were resolved by the core schema.
func TestCompile_NoScalarTextOrKeyIsRewrittenByTheCoreSchema(t *testing.T) {
	t.Parallel()
	keys := srMin + "components:\n  schemas:\n    S:\n      type: object\n      required: [\"007\"]\n" +
		"      properties:\n        007: {type: string, maxLength: 3}\n        1: {type: string}\n        01: {type: integer}\n" +
		"    T: {$ref: \"#/components/schemas/S/properties/007\"}\n"

	doc, diags := compileText(t, "keys.yaml", keys, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags)
	tree := irTree(t, doc)
	for _, k := range []string{"007", "1", "01"} {
		assert.NotNil(t, findByID(tree, "p/openapi/components/schemas/S/properties/"+k), k)
	}
	assert.Equal(t, true, findByID(tree, "p/openapi/components/schemas/S/properties/007")["required"])
	assert.NotNil(t, findByID(tree, "t/openapi/components/schemas/T"), "the reference to 007 resolves")

	version := "openapi: 3.1.0\ninfo: {title: t, version: 007}\npaths: {}\ncomponents:\n  schemas:\n    S:\n" +
		"      type: object\n      required: [007]\n      properties:\n        007: {type: string}\n"
	doc, diags = compileText(t, "version.yaml", version, openapi.Options{})

	require.NotNil(t, doc)
	assert.Equal(t, "007", doc.Version)
	tree = irTree(t, doc)
	assert.Equal(t, true, findByID(tree, "p/openapi/components/schemas/S/properties/007")["required"])
	require.Len(t, diags, 2, "%+v", diags)
	for i, pointer := range []string{"/info/version", "/components/schemas/S/required/0"} {
		assert.Equal(t, "openapi/validation/validation-type-mismatch", diags[i].Code)
		assert.Equal(t, pointer, string(diags[i].Provenance.Pointer))
		assert.True(t, strings.HasSuffix(diags[i].Message, "it is the number 007"), diags[i].Message)
	}
}

// TestCompile_ADiscriminatorMappingKeepsItsNumberLikeKeys holds the mapping's
// keys to the text written.
func TestCompile_ADiscriminatorMappingKeepsItsNumberLikeKeys(t *testing.T) {
	t.Parallel()
	src := srMin + "components:\n  schemas:\n    P:\n      type: object\n      properties: {k: {type: string}}\n" +
		"      discriminator: {propertyName: k, mapping: {01: \"#/components/schemas/A\", 02: \"#/components/schemas/B\"}}\n" +
		"    A: {allOf: [{$ref: \"#/components/schemas/P\"}]}\n    B: {allOf: [{$ref: \"#/components/schemas/P\"}]}\n"

	doc, diags := compileText(t, "mapping.yaml", src, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.True(t, containsJSON(t, doc, `"mapping":{"01":"t/openapi/components/schemas/A","02":"t/openapi/components/schemas/B"}`))
}

// TestCompile_ACoreScalarReadsTheSameWrittenQuotedOrAsItsTwin holds each
// spelling the core schema reads as text to the IR the same text quoted gives,
// each integer it reads to the IR of its base ten twin, and an integer the
// document tags itself to the same reading.
func TestCompile_ACoreScalarReadsTheSameWrittenQuotedOrAsItsTwin(t *testing.T) {
	t.Parallel()
	schema := func(maxLength, def, minimum, maximum, multipleOf, enum, constant string) string {
		return srMin + "components:\n  schemas:\n    S:\n      type: integer\n" +
			"      maxLength: " + maxLength + "\n      default: " + def + "\n      minimum: " + minimum + "\n" +
			"      maximum: " + maximum + "\n      multipleOf: " + multipleOf + "\n      example: 2021-01-01\n" +
			"      enum: " + enum + "\n      const: " + constant + "\n"
	}
	written := schema("0b11", "001", "0o17", "+0x1F", "089",
		"[1e3, .5, -.inf, 0X1F, 1_0.5, 0o1_7, -007, +12]", "!!int 0644")
	twin := schema(`"0b11"`, "1", "0o17", `"+0x1F"`, "89",
		`[1e3, .5, -.inf, "0X1F", "1_0.5", "0o1_7", -7, +12]`, "644")

	doc, diags := compileText(t, "scalars.yaml", written, openapi.Options{})
	twinDoc, twinDiags := compileText(t, "scalars.yaml", twin, openapi.Options{})

	require.NotNil(t, doc)
	require.NotNil(t, twinDoc)
	assert.Equal(t, lines(twinDiags), lines(diags), "the same findings, at other columns")
	assert.Empty(t, byCode(diags, srVersionReading))
	assert.Equal(t, jsonOf(t, twinDoc), jsonOf(t, doc))
	assert.True(t, containsJSON(t, doc, `"openapi:minimum":{"reason":"degraded_lowering","value":15`), "0o17 is 15")
	assert.True(t, containsJSON(t, doc, `"value":{"kind":"number","num":"644"}`), "!!int 0644 is 644, not 420")
}

// TestCompile_ACountIsTakenFromItsLiteral holds a count to the literal written:
// 010 is ten, 08 is eight, and a count that is no integer, or past 64 bits, is
// no constraint.
func TestCompile_ACountIsTakenFromItsLiteral(t *testing.T) {
	t.Parallel()
	src := srMin + "components:\n  schemas:\n    S: {type: string, maxLength: 010, minLength: 08, maxItems: 1.5, " +
		"minItems: 99999999999999999999}\n"

	doc, diags := compileText(t, "counts.yaml", src, openapi.Options{})

	require.NotNil(t, doc)
	s := findByID(irTree(t, doc), "t/openapi/components/schemas/S")
	require.NotNil(t, s)
	c, ok := s["constraints"].(map[string]any)
	require.True(t, ok, "the schema holds constraints")
	assert.InDelta(t, 10, c["maxLength"], 0, "010 is ten, not the parser's eight")
	assert.InDelta(t, 8, c["minLength"], 0)
	assert.NotContains(t, c, "maxItems")
	assert.NotContains(t, c, "minItems")
	mismatch := byCode(diags, "openapi/validation/validation-type-mismatch")
	require.Len(t, mismatch, 1, "%+v", diags)
	assert.Equal(t, "/components/schemas/S/maxItems", string(mismatch[0].Provenance.Pointer))
}

// TestCompile_AJSONIntegerPastSixtyFourBitsKeepsItsDigits holds a JSON text to
// the tags the JSON reader gives, and the integer past 64 bits to the number
// it was written as.
func TestCompile_AJSONIntegerPastSixtyFourBitsKeepsItsDigits(t *testing.T) {
	t.Parallel()
	src := `{"openapi":"3.1.0","info":{"title":"t","version":"1"},"paths":{},"x-n":[1E3,-0,0.5e-2,99999999999999999999]}`

	doc, diags := compileText(t, "spec.json", src, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.True(t, containsJSON(t, doc, `"value":[1E3,0,0.5e-2,99999999999999999999]`), "the last is the number written")
}
