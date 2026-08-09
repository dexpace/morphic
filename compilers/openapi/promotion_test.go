// This file covers promotion: reading a vendor extension into the typed IR
// field it is the only OpenAPI spelling for. It belongs to no single source
// file — the policy is on the compiler's options, the reading is one function
// beneath both walks, and it is applied at every carrier that can record it.
package openapi_test // external test package — exercises only the public API

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// promotionCarrier is one node the corpus spec deprecates, reduced to the three
// things a promotion touches: the field it fills, the provenance that has to
// record it, and the entries it must not consume.
type promotionCarrier struct {
	deprecation *ir.Deprecation
	provenance  ir.Provenance
	unmodeled   ir.Unmodeled
}

// promotionCarriers picks out every deprecated node extension-promotion.yaml
// declares. Naming them here rather than asserting one is what makes this the
// sweep: a construction site that stops promoting fails on its own row instead
// of being covered by a neighbour.
func promotionCarriers(t *testing.T, doc *ir.Document) map[string]promotionCarrier {
	t.Helper()
	op, ok := opByName(doc, "getX")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)
	header := op.Responses[0].Headers[0]

	model, ok := doc.Types[namedID("Old")].(*ir.Model)
	require.True(t, ok)
	prop, ok := propByWire(model, "p")
	require.True(t, ok)

	scheme, ok := doc.Auth["auth/openapi/components/securitySchemes/k"]
	require.True(t, ok)

	return map[string]promotionCarrier{
		"operation":   {op.Deprecation, op.Provenance, op.Unmodeled},
		"header":      {header.Deprecation, header.Provenance, header.Unmodeled},
		"type":        {model.Deprecation, model.Provenance, model.Unmodeled},
		"property":    {prop.Deprecation, prop.Provenance, prop.Unmodeled},
		"auth scheme": {scheme.Deprecation, scheme.Provenance, scheme.Unmodeled},
	}
}

// assertExtensionPromotion is the corpus row for GitHub #252: OpenAPI names no
// keyword for why something was deprecated, when, or until when, so an x-* key
// is the only spelling there is — and it used to sit in Unmodeled while
// ir.Deprecation stayed empty at every site the compiler builds one.
func assertExtensionPromotion(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	want := map[string]string{
		"operation":   "use getY instead",
		"header":      "header goes away",
		"type":        "replaced by New",
		"property":    "field goes away",
		"auth scheme": "rotate to oauth",
	}
	for name, carrier := range promotionCarriers(t, doc) {
		require.NotNil(t, carrier.deprecation, "%s is deprecated", name)
		assert.Equal(t, want[name], carrier.deprecation.Message, "%s message", name)
		assert.Equal(t, "extension-promotion", carrier.provenance.Inferred,
			"%s records that the field was read by a heuristic", name)
		entry, kept := carrier.unmodeled["openapi:x-deprecated-reason"]
		require.True(t, kept, "%s keeps the extension it was promoted from", name)
		assert.Equal(t, ir.ReasonVendorExtension, entry.Reason,
			"%s promotion does not reclassify what it read", name)
	}

	op, ok := opByName(doc, "getX")
	require.True(t, ok)
	assert.Equal(t, "1.2.0", op.Deprecation.Since)
	assert.Equal(t, "2.0.0", op.Deprecation.RemovalVersion)

	assertPromotionDeclined(t, doc, diags)
}

// assertPromotionDeclined pins the two shapes promotion refuses, both of which
// leave the extension exactly where it was: a key on a node that never said it
// was deprecated annotates nothing, and a value that is not text is a document
// meaning something else by the key.
func assertPromotionDeclined(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	live, ok := opByName(doc, "getY")
	require.True(t, ok)
	assert.Nil(t, live.Deprecation, "the operation is not deprecated")
	assert.Contains(t, live.Unmodeled, "openapi:x-deprecated-reason")
	assert.Empty(t, live.Provenance.Inferred, "nothing was inferred, so nothing is marked")

	model, ok := doc.Types[namedID("Old")].(*ir.Model)
	require.True(t, ok)
	numeric, ok := propByWire(model, "n")
	require.True(t, ok)
	require.NotNil(t, numeric.Deprecation)
	assert.Empty(t, numeric.Deprecation.Message, "a non-string value fills no field")
	assert.Contains(t, numeric.Unmodeled, "openapi:x-deprecated-reason")
	assert.Empty(t, numeric.Provenance.Inferred)
	assert.True(t, hasDiagCode(diags, "openapi/degraded-construct"),
		"declining a value the policy cannot read is reported; got %+v", diags)
}

// deprecatedOpSpec is one deprecated operation carrying the two extensions the
// default mapping reads, for the tests that vary the policy rather than the
// document.
const deprecatedOpSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      deprecated: true
      x-deprecated-reason: use getY instead
      x-gone-in: "9.0.0"
      responses:
        "200":
          description: ok
`

// compilePromotionSpec compiles src with opts and requires no error diagnostic.
func compilePromotionSpec(t *testing.T, src string, opts openapi.Options) *ir.Document {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
		compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assertNoErrorDiags(t, diags)
	return doc
}

// TestPromotion_DisabledKeepsExtensionsAndNothingElse pins the off switch
// invariant 6 requires. Disabled means the typed field is not written at all,
// and the document compiles to exactly what it did before promotion existed.
func TestPromotion_DisabledKeepsExtensionsAndNothingElse(t *testing.T) {
	t.Parallel()
	doc := compilePromotionSpec(t, deprecatedOpSpec,
		openapi.Options{Promotions: openapi.ExtensionPromotions{Disabled: true}})
	op, ok := opByName(doc, "getX")
	require.True(t, ok)
	require.NotNil(t, op.Deprecation)
	assert.Empty(t, op.Deprecation.Message)
	assert.Empty(t, op.Provenance.Inferred)
	assert.Contains(t, op.Unmodeled, "openapi:x-deprecated-reason")
}

// TestPromotion_TargetsReplaceTheDefaults pins that a stated mapping is the
// whole mapping. A caller whose documents spell the key differently gets their
// spelling, and does not silently keep the defaults beside it — which is the
// difference between a default and a standard.
func TestPromotion_TargetsReplaceTheDefaults(t *testing.T) {
	t.Parallel()
	own := openapi.Options{Promotions: openapi.ExtensionPromotions{
		Targets: map[string]openapi.ExtensionTarget{
			"x-gone-in": openapi.TargetDeprecationRemovalVersion,
		},
	}}
	doc := compilePromotionSpec(t, deprecatedOpSpec, own)
	op, ok := opByName(doc, "getX")
	require.True(t, ok)
	require.NotNil(t, op.Deprecation)
	assert.Equal(t, "9.0.0", op.Deprecation.RemovalVersion, "the caller's key is read")
	assert.Empty(t, op.Deprecation.Message, "a default the caller replaced is not read")
	assert.Equal(t, "extension-promotion", op.Provenance.Inferred)
}

// TestPromotion_DefaultTargetsAreTheOnesApplied holds the exported mapping to
// the one the compiler applies. Two transcriptions of one mapping is one of
// them going stale unnoticed, and the exported one is what a caller changing it
// starts from.
func TestPromotion_DefaultTargetsAreTheOnesApplied(t *testing.T) {
	t.Parallel()
	defaults := openapi.DefaultExtensionPromotions()
	require.NotEmpty(t, defaults, "an empty mapping would make this vacuous")

	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      deprecated: true
`
	for key := range defaults {
		spec += "      " + key + ": filled\n"
	}
	spec += `      responses:
        "200":
          description: ok
`
	doc := compilePromotionSpec(t, spec, openapi.Options{})
	op, ok := opByName(doc, "getX")
	require.True(t, ok)
	require.NotNil(t, op.Deprecation)
	for _, got := range map[openapi.ExtensionTarget]string{
		openapi.TargetDeprecationMessage:        op.Deprecation.Message,
		openapi.TargetDeprecationSince:          op.Deprecation.Since,
		openapi.TargetDeprecationRemovalVersion: op.Deprecation.RemovalVersion,
	} {
		assert.Equal(t, "filled", got, "every default target is filled by its default key")
	}
}
