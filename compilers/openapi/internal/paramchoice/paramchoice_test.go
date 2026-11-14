// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package paramchoice_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// key is the pointer of the default key on the operation of spec.
const key = "/paths/~1r/get/x-mutually-exclusive-parameter-groups"

// spec is a document of one operation, getR, taking a required query
// parameter req, optional query parameters a, b and c, and a header c, whose
// default key is groups, indented as the key's value.
func spec(groups string) string {
	return `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /r:
    get:
      operationId: getR
      parameters:
        - {name: req, in: query, required: true, schema: {type: string}}
        - {name: a, in: query, schema: {type: string}}
        - {name: b, in: query, schema: {type: string}}
        - {name: c, in: query, schema: {type: string}}
        - {name: c, in: header, schema: {type: string}}
      x-mutually-exclusive-parameter-groups:
` + groups + `      responses: {"204": {description: ok}}
`
}

// compile runs the compiler over src and returns getR and the reports of keys
// that fill nothing, by pointer and message.
func compile(t *testing.T, src string) (ir.Operation, map[string]string) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(src)},
		compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	found := map[string]string{}
	for _, d := range diags {
		if d.Code == diag.UnpromotedExtension {
			assert.Equal(t, ir.SeverityInfo, d.Severity)
			found[string(d.Provenance.Pointer)] = d.Message
		}
	}
	return openapitest.FindOp(t, doc, "getR"), found
}

// TestPromote_ReadsEachChoiceWholeOrNotAtAll pins each way a choice is
// refused, one report at the coordinate that says why, the others standing.
func TestPromote_ReadsEachChoiceWholeOrNotAtAll(t *testing.T) {
	t.Parallel()
	const good = "        good:\n          optional: true\n          variants: {x: [a], y: [b]}\n"
	for _, tc := range []struct {
		name, choice, at, why string
	}{
		{"no object", "        bad: 1\n", "/bad", `holds a number for choice "bad", not an object`},
		{"null", "        bad:\n", "/bad", `holds null for choice "bad", not an object`},
		{"no optional", "        bad: {variants: {x: [a], y: [b]}}\n", "/bad", `states no boolean "optional"`},
		{"optional null", "        bad: {optional: null, variants: {x: [a], y: [b]}}\n", "/bad/optional",
			`states no boolean "optional"`},
		{"no variants", "        bad: {optional: true}\n", "/bad", "states fewer than two variants"},
		{"variants a list", "        bad: {optional: true, variants: [a, b]}\n", "/bad/variants",
			"states fewer than two variants"},
		{"a variant of numbers", "        bad: {optional: true, variants: {x: [1], y: [b]}}\n", "/bad/variants/x",
			`names no parameter in variant "x"`},
		{"an unknown name", "        bad: {optional: true, variants: {x: [a], y: [z]}}\n", "/bad/variants/y/0",
			`names parameter "z", which the operation does not declare`},
		{"a name in two locations", "        bad: {optional: true, variants: {x: [a], y: [b, c]}}\n",
			"/bad/variants/y/1", `names parameter "c", which the operation declares in 2 locations`},
		{"a name twice by two spellings", "        bad: {optional: true, variants: {x: [a, query.a], y: [b]}}\n",
			"/bad/variants/x/1", `names parameter "query.a" twice in variant "x"`},
		{"a required parameter in some variants", "        bad: {optional: false, variants: {x: [req], y: [b]}}\n",
			"/bad", `names parameter "req", which every request carries, in some variants only`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op, found := compile(t, spec(tc.choice+good))
			require.Len(t, op.ParamChoices, 1, "the readable choice stands")
			assert.Equal(t, "good", op.ParamChoices[0].Name.Source)
			assert.Equal(t, map[string]string{key + tc.at: found[key+tc.at]}, found)
			assert.Contains(t, found[key+tc.at], tc.why)
			assert.Contains(t, found[key+tc.at], `so the choice "bad" is not filled`)
			assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
		})
	}
}

// TestPromote_FillsAChoiceEveryVariantOfWhichCarriesARequiredParameter pins
// that a parameter every request carries may sit in a choice where every
// variant names it, which the choice then says nothing false of, and that the
// choice's required flag is the negation of optional.
func TestPromote_FillsAChoiceEveryVariantOfWhichCarriesARequiredParameter(t *testing.T) {
	t.Parallel()
	op, found := compile(t, spec("        both: {optional: false, variants: {x: [req, a], y: [req, b]}}\n"))
	assert.Empty(t, found)
	require.Len(t, op.ParamChoices, 1)
	assert.True(t, op.ParamChoices[0].Required)
	assert.Equal(t, []ir.ParamID{"param/openapi/paths/~1r/get/parameters/req/query",
		"param/openapi/paths/~1r/get/parameters/a/query"}, op.ParamChoices[0].Variants[0].Params)
}

// TestPromote_ReadsNoKeyThatIsNoObject pins that a key holding no object of
// choices fills nothing and marks nothing, reported once at the key.
func TestPromote_ReadsNoKeyThatIsNoObject(t *testing.T) {
	t.Parallel()
	op, found := compile(t, spec("        - [a]\n        - [b]\n"))
	assert.Empty(t, op.ParamChoices)
	assert.Empty(t, op.Provenance.Inferred)
	assert.Equal(t, map[string]string{key: `extension "x-mutually-exclusive-parameter-groups" is a list, ` +
		`not an object of named choices, so no choice is filled`}, found)
}

// TestPromote_ReadsNothingWhereNoKeyIsWritten pins that an operation writing
// no key the policy maps states no choice and is not marked.
func TestPromote_ReadsNothingWhereNoKeyIsWritten(t *testing.T) {
	t.Parallel()
	op, found := compile(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /r:
    get:
      operationId: getR
      parameters:
        - {name: a, in: query, schema: {type: string}}
      responses: {"204": {description: ok}}
`)
	assert.Empty(t, op.ParamChoices)
	assert.Empty(t, op.Provenance.Inferred)
	assert.Empty(t, found)
}
