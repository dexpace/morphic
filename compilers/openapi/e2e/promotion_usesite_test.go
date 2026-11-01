// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file covers what a promotion reads through a $ref. Annotations merge from
// a $ref's target onto the property, parameter or header naming it, field by
// field and by one rule at every carrier, so a fact a key states on the target
// reaches the use site as the keyword's fact does. The reading happens at three
// construction sites, which is why each case below runs at all three.
package e2e_test // external test package — exercises only the public API

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// useSiteComponents declares every target the cases below name. Spent writes no
// `deprecated`, so a use reads its key as the target's own. Dated is not
// deprecated at all, so its date is only for a use that is. Mid sits between a
// use and Spent and lends nothing, as a schema between the two ends of a chain
// lends no annotation.
const useSiteComponents = `components:
  schemas:
    Old: {type: string, deprecated: true, x-deprecated-reason: use New, x-sunset: "2030-01-01"}
    Spent: {type: string, x-fern-availability: deprecated, x-sunset: "2031-01-01"}
    Dated: {type: string, x-sunset: "2032-01-01"}
    Tok: {type: string, x-sensitive: true}
    Plain: {type: string}
    Mid: {$ref: '#/components/schemas/Spent', x-sunset: "1999-01-01", x-sensitive: true}
`

// useSiteSpec declares schema, a flow-style schema object, as one property, one
// query parameter and one response header.
func useSiteSpec(schema string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    post:
      operationId: x
      parameters:
        - {name: use, in: query, schema: ` + schema + `}
      responses:
        "200":
          description: ok
          headers:
            X-Use: {schema: ` + schema + `}
          content:
            application/json:
              schema:
                type: object
                properties:
                  use: ` + schema + `
` + useSiteComponents
}

// compileUse compiles src under the default options, leaving its diagnostics to
// the test.
func compileUse(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
		compilers.Options{FormatOptions: openapi.Options{}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// useSite is the three fields a promotion writes on a carrier.
type useSite struct {
	deprecation *ir.Deprecation
	secret      bool
	inferred    ir.Inferred
}

// useSitesOf reads the three carriers useSiteSpec declares.
func useSitesOf(t *testing.T, doc *ir.Document) map[string]useSite {
	t.Helper()
	op, ok := opByName(doc, "x")
	require.True(t, ok)
	param, ok := openapitest.ParamBySource(op, "use")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	header, ok := headerByWire(op.Responses[0].Headers, "X-Use")
	require.True(t, ok)
	model, ok := doc.Types[openapitest.BodyTarget(t, op.Responses[0].Payload)].(*ir.Model)
	require.True(t, ok)
	prop, ok := propByWire(model, "use")
	require.True(t, ok)
	return map[string]useSite{
		"parameter": {param.Deprecation, param.Secret, param.Provenance.Inferred},
		"header":    {header.Deprecation, header.Secret, header.Provenance.Inferred},
		"property":  {prop.Deprecation, prop.Secret, prop.Provenance.Inferred},
	}
}

// TestPromotion_AUseReadsItsReferentsKeys is the rule of
// docs/compilers/openapi.md#ref-use-site at the three carriers: a key on the
// target merges as its keyword would, field by field, and one beside the $ref
// wins. Without it the node reads `removalDate` and the use reads an empty
// Deprecation, though every other annotation merged.
func TestPromotion_AUseReadsItsReferentsKeys(t *testing.T) {
	t.Parallel()
	const marker ir.Inferred = "extension-promotion"
	for _, tc := range []struct {
		name   string
		schema string
		want   *ir.Deprecation
		secret bool
		marked bool
	}{
		{"a referent's members", `{$ref: '#/components/schemas/Old'}`,
			&ir.Deprecation{Message: "use New", RemovalDate: "2030-01-01"}, false, true},
		{"a key beside the $ref wins field by field", `{$ref: '#/components/schemas/Old', x-deprecated-reason: mine}`,
			&ir.Deprecation{Message: "mine", RemovalDate: "2030-01-01"}, false, true},
		{"a key stating the deprecation", `{$ref: '#/components/schemas/Spent'}`,
			&ir.Deprecation{RemovalDate: "2031-01-01"}, false, true},
		{"a keyword beside the $ref decides over the referent's key", `{$ref: '#/components/schemas/Spent', deprecated: false}`,
			nil, false, false},
		{"a keyword deprecating gives the referent's members a home", `{$ref: '#/components/schemas/Dated', deprecated: true}`,
			&ir.Deprecation{RemovalDate: "2032-01-01"}, false, true},
		{"a key deprecating does too", `{$ref: '#/components/schemas/Dated', x-fern-availability: deprecated}`,
			&ir.Deprecation{RemovalDate: "2032-01-01"}, false, true},
		{"a referent's member annotates nothing on a live use", `{$ref: '#/components/schemas/Dated'}`,
			nil, false, false},
		{"a referent's redaction", `{$ref: '#/components/schemas/Tok'}`, nil, true, true},
		{"a false beside the $ref clears no redaction", `{$ref: '#/components/schemas/Tok', x-sensitive: false}`,
			nil, true, true},
		{"a target stating nothing", `{$ref: '#/components/schemas/Plain'}`, nil, false, false},
		{"a schema between the two lends nothing", `{$ref: '#/components/schemas/Mid'}`,
			&ir.Deprecation{RemovalDate: "2031-01-01"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileUse(t, useSiteSpec(tc.schema))
			assert.Empty(t, diags, "a referent reports where it is written, and these write nothing wrong")
			for carrier, got := range useSitesOf(t, doc) {
				assert.Equal(t, tc.want, got.deprecation, "%s deprecation", carrier)
				assert.Equal(t, tc.secret, got.secret, "%s secret", carrier)
				assert.Equal(t, tc.marked, got.inferred == marker, "%s marker", carrier)
			}
		})
	}
}

// TestPromotion_AReferentReportsOnceWhereItIsWritten holds a misfit on a target
// to one report at the target, however many uses read it: the use site reads
// silently, and it fills nothing from a value the target could not hold.
func TestPromotion_AReferentReportsOnceWhereItIsWritten(t *testing.T) {
	t.Parallel()
	spec := useSiteSpec(`{$ref: '#/components/schemas/Bad'}`) +
		"    Bad: {type: string, deprecated: true, x-sunset: 7, x-sensitive: \"yes\"}\n"
	doc, diags := compileUse(t, spec)
	for carrier, got := range useSitesOf(t, doc) {
		require.NotNil(t, got.deprecation, carrier)
		assert.Equal(t, ir.Deprecation{}, *got.deprecation, carrier)
		assert.False(t, got.secret, carrier)
		assert.Empty(t, got.inferred, carrier)
	}
	at := make([]string, 0, len(diags))
	for _, d := range diags {
		at = append(at, string(d.Provenance.Pointer))
	}
	assert.ElementsMatch(t, []string{
		"/components/schemas/Bad/x-sunset", "/components/schemas/Bad/x-sensitive",
	}, at, "one report per key, at the key")
}

// TestPromotion_TheObjectAroundASchemaIsItsOwnKeywordSite covers the two carriers
// with an object of their own around the schema, which writes annotations a
// property has no place for. `deprecated: true` on either and on its schema each
// deprecate it, so a referent's date reaches the first, and neither's false
// clears what a key on the referent states.
func TestPromotion_TheObjectAroundASchemaIsItsOwnKeywordSite(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: x
      parameters:
        - {name: deprecates, in: query, deprecated: true, schema: {$ref: '#/components/schemas/Dated'}}
        - {name: declines, in: query, deprecated: false, schema: {$ref: '#/components/schemas/Spent'}}
      responses:
        "200":
          description: ok
          headers:
            X-Deprecates: {deprecated: true, schema: {$ref: '#/components/schemas/Dated'}}
            X-Declines: {deprecated: false, schema: {$ref: '#/components/schemas/Spent'}}
` + useSiteComponents
	doc := compileClean(t, spec, openapi.Options{})
	op, ok := opByName(doc, "x")
	require.True(t, ok)
	deprecates, ok := openapitest.ParamBySource(op, "deprecates")
	require.True(t, ok)
	declines, ok := openapitest.ParamBySource(op, "declines")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	headerDeprecates, ok := headerByWire(op.Responses[0].Headers, "X-Deprecates")
	require.True(t, ok)
	headerDeclines, ok := headerByWire(op.Responses[0].Headers, "X-Declines")
	require.True(t, ok)

	assert.Equal(t, &ir.Deprecation{RemovalDate: "2032-01-01"}, deprecates.Deprecation)
	assert.Equal(t, &ir.Deprecation{RemovalDate: "2031-01-01"}, declines.Deprecation)
	assert.Equal(t, &ir.Deprecation{RemovalDate: "2032-01-01"}, headerDeprecates.Deprecation)
	assert.Equal(t, &ir.Deprecation{RemovalDate: "2031-01-01"}, headerDeclines.Deprecation)
}

// TestPromotion_ADecoratorMatchesTheRefItStandsFor pins the two spellings of one
// use to one answer. A 3.0 document annotates a use by wrapping the $ref in an
// allOf, and an emitter folds the wrapped type's annotations onto the carrier
// (docs/emitters/refine/decorators.md), so the node has to say what the $ref's
// carrier says: the key deprecates the target, not only a use of it.
func TestPromotion_ADecoratorMatchesTheRefItStandsFor(t *testing.T) {
	t.Parallel()
	doc := compileClean(t, useSiteSpec(`{$ref: '#/components/schemas/Spent', description: d}`), openapi.Options{})
	for carrier, got := range useSitesOf(t, doc) {
		assert.NotNil(t, got.deprecation, carrier)
	}

	wrapped := compileClean(t, useSiteSpec(`{allOf: [{$ref: '#/components/schemas/Spent'}], description: d}`), openapi.Options{})
	op, ok := opByName(wrapped, "x")
	require.True(t, ok)
	param, ok := openapitest.ParamBySource(op, "use")
	require.True(t, ok)
	decorator, ok := wrapped.Types[param.Type.Target].(*ir.Model)
	require.True(t, ok, "an allOf over one $ref is a decorator")
	base, decorated := ir.Decorated(decorator)
	require.True(t, decorated)
	assert.Equal(t, componentID("Spent"), base.Target)
	target := wrapped.Types[componentID("Spent")].Common()
	assert.Equal(t, &ir.Deprecation{RemovalDate: "2031-01-01"}, target.Deprecation,
		"what an emitter folds onto the carrier")
}

// TestPromotion_UsesHoldTheirOwnAnswerInEitherOrder diffs a document against
// itself with every mapping reversed. The order oracle reads no operation and no
// parameter, so a promotion that depended on which use or which declaration
// lowered first would pass it; two uses of one target, on either side of the
// target's own declaration, are what would tell. Reversing the keys of one
// operation also shows its disagreement resolves by the policy and not by the
// document.
func TestPromotion_UsesHoldTheirOwnAnswerInEitherOrder(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      x-stability: beta
      x-fern-availability: generally-available
      parameters:
        - {name: p, in: query, schema: {$ref: '#/components/schemas/Spent'}}
      responses:
        "200": {description: ok, headers: {X-H: {schema: {$ref: '#/components/schemas/Tok'}}}}
  /b:
    get:
      operationId: b
      x-fern-availability: deprecated
      parameters:
        - {name: p, in: query, schema: {$ref: '#/components/schemas/Tok'}}
      responses:
        "200": {description: ok, headers: {X-H: {schema: {$ref: '#/components/schemas/Spent'}}}}
components:
  schemas:
    Spent: {type: string, x-fern-availability: deprecated, x-sunset: "2031-01-01"}
    Tok: {type: string, x-sensitive: true}
    Holder:
      type: object
      properties:
        s: {$ref: '#/components/schemas/Spent'}
        t: {$ref: '#/components/schemas/Tok'}
`
	forward, forwardDiags := compileUse(t, src)
	backward, backwardDiags := compileUse(t, string(reversed(t, src)))

	assertPromotionsAgree(t, forward, backward)
	for _, diags := range [][]ir.Diagnostic{forwardDiags, backwardDiags} {
		require.Len(t, diags, 1, "the disagreement, whichever way the keys are written")
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/x-fern-availability"), diags[0].Provenance.Pointer)
	}
	a, ok := opByName(backward, "a")
	require.True(t, ok)
	assert.Equal(t, ir.StabilityBeta, a.Stability)
}

// assertPromotionsAgree compares what a promotion writes on every operation, its
// parameters and headers by name, and every component named, across two
// compiles of one document in two orders.
func assertPromotionsAgree(t *testing.T, a, b *ir.Document) {
	t.Helper()
	for _, name := range []string{"a", "b"} {
		x, ok := opByName(a, name)
		require.True(t, ok)
		y, ok := opByName(b, name)
		require.True(t, ok)
		assert.Equal(t, x.Stability, y.Stability, name)
		assert.Equal(t, x.Deprecation, y.Deprecation, name)
		assert.Equal(t, x.Provenance.Inferred, y.Provenance.Inferred, name)
		assert.Equal(t, x.Params[0].Deprecation, y.Params[0].Deprecation, name)
		assert.Equal(t, x.Params[0].Secret, y.Params[0].Secret, name)
		assert.Equal(t, x.Params[0].Provenance.Inferred, y.Params[0].Provenance.Inferred, name)
		assert.Equal(t, x.Responses[0].Headers[0].Deprecation, y.Responses[0].Headers[0].Deprecation, name)
		assert.Equal(t, x.Responses[0].Headers[0].Secret, y.Responses[0].Headers[0].Secret, name)
	}
	for _, id := range []ir.TypeID{componentID("Spent"), componentID("Tok"), componentID("Holder")} {
		assert.Equal(t, a.Types[id].Common().Deprecation, b.Types[id].Common().Deprecation, id)
		assert.Equal(t, a.Types[id].Common().Sensitive, b.Types[id].Common().Sensitive, id)
		assert.Equal(t, a.Types[id].Common().Provenance.Inferred, b.Types[id].Common().Provenance.Inferred, id)
	}
	x, ok := a.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	y, ok := b.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	for _, wire := range []string{"s", "t"} {
		p, ok := propByWire(x, wire)
		require.True(t, ok)
		q, ok := propByWire(y, wire)
		require.True(t, ok)
		assert.Equal(t, p.Deprecation, q.Deprecation, wire)
		assert.Equal(t, p.Secret, q.Secret, wire)
		assert.Equal(t, p.Provenance.Inferred, q.Provenance.Inferred, wire)
	}
}
