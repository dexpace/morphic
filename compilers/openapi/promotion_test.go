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
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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
	require.Len(t, op.Params, 1)
	param := op.Params[0]

	model, ok := doc.Types[namedID("Old")].(*ir.Model)
	require.True(t, ok)
	prop, ok := propByWire(model, "p")
	require.True(t, ok)

	scheme, ok := doc.Auth["auth/openapi/components/securitySchemes/k"]
	require.True(t, ok)

	return map[string]promotionCarrier{
		"operation":   {op.Deprecation, op.Provenance, op.Unmodeled},
		"parameter":   {param.Deprecation, param.Provenance, param.Unmodeled},
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
		"parameter":   "use filter instead",
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
	assert.Equal(t, "2026-08-01", op.Deprecation.RemovalDate,
		"x-sunset is a date, so it reaches the date field")
	assert.Empty(t, op.Deprecation.RemovalVersion,
		"a sunset date does not land in the field a consumer reads as a version")

	require.Len(t, op.Params, 1)
	require.NotNil(t, op.Params[0].Deprecation)
	assert.Equal(t, "2027-01-15", op.Params[0].Deprecation.RemovalDate,
		"the parameter's own x-sunset reaches its own removal date, not the operation's")

	assertEnumOpenness(t, doc)
	assertPromotionDeclined(t, doc, diags)
}

// assertEnumOpenness is the corpus row for GitHub #427 and the matrix's
// open-enums row. ir.Enum.Closed is exactly the fact x-extensible-enum states,
// and every enum the compiler builds is closed, so without the promotion the
// extension changed nothing an emitter or a differ could read.
//
// The three schemas are the three answers the reading has: the convention's own
// spelling opens the enum, an explicit false declines to, and an enum that
// names no such key is untouched — the last so that the first is a promotion
// rather than a compiler that stopped closing enums.
func assertEnumOpenness(t *testing.T, doc *ir.Document) {
	tests := []struct {
		schema   string
		closed   bool
		inferred string
	}{
		{"Size", false, "extension-promotion"},
		{"Shade", true, ""},
		{"Fixed", true, ""},
	}
	for _, tc := range tests {
		enum, ok := doc.Types[namedID(tc.schema)].(*ir.Enum)
		require.True(t, ok, "%s lowers to an enum", tc.schema)
		assert.Equal(t, tc.closed, enum.Closed, "%s openness", tc.schema)
		assert.Equal(t, tc.inferred, enum.Provenance.Inferred, "%s heuristic marker", tc.schema)
	}

	for _, schema := range []string{"Size", "Shade"} {
		enum, ok := doc.Types[namedID(schema)].(*ir.Enum)
		require.True(t, ok)
		entry, kept := enum.Unmodeled["openapi:x-extensible-enum"]
		require.True(t, kept, "%s keeps the extension whether or not it was read", schema)
		assert.Equal(t, ir.ReasonVendorExtension, entry.Reason,
			"%s promotion does not reclassify what it read", schema)
	}
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
	assert.True(t, openapitest.HasDiag(diags, "openapi/degraded-construct"),
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

	// Every default key is written twice, on a deprecated operation and on an
	// enum, because the targets live on two carriers and a key reaching only the
	// wrong one would read as a mapping that fills nothing.
	keys := ""
	for key := range defaults {
		keys += "      " + key + ": filled\n"
	}
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      deprecated: true
` + keys + `      responses:
        "200":
          description: ok
components:
  schemas:
    E:
      type: string
      enum: [a, b]
` + keys
	doc := compilePromotionSpec(t, spec, openapi.Options{})
	op, ok := opByName(doc, "getX")
	require.True(t, ok)
	require.NotNil(t, op.Deprecation)
	enum, ok := doc.Types[namedID("E")].(*ir.Enum)
	require.True(t, ok)

	// Read off the defaults rather than listing the pairs, so a mapping this
	// test does not know about fails here instead of going unread.
	fields := map[openapi.ExtensionTarget]string{
		openapi.TargetDeprecationMessage:        op.Deprecation.Message,
		openapi.TargetDeprecationSince:          op.Deprecation.Since,
		openapi.TargetDeprecationRemovalVersion: op.Deprecation.RemovalVersion,
		openapi.TargetDeprecationRemovalDate:    op.Deprecation.RemovalDate,
		openapi.TargetEnumOpen:                  filledWhen(!enum.Closed),
	}
	named := map[openapi.ExtensionTarget]bool{}
	for key, target := range defaults {
		got, known := fields[target]
		require.True(t, known, "%s is a default target this test reads no field for", target)
		assert.Equal(t, "filled", got, "%s is filled by its default key %s", target, key)
		named[target] = true
	}
	for target, got := range fields {
		if !named[target] {
			assert.Empty(t, got, "%s is filled by no default key, so it stays empty", target)
		}
	}
}

// filledWhen renders a target whose field is not text as the "filled" the text
// ones carry, so one table can read every default target rather than growing an
// arm per field type.
func filledWhen(promoted bool) string {
	if promoted {
		return "filled"
	}
	return ""
}

// TestPromotion_RemovalDateAndVersionAreSeparateFacts pins why a scheduled
// removal is two fields rather than one field carrying which spelling it holds
// (GitHub #417). A document can state both — a sunset date and the release it
// goes in — and one field would have to drop whichever it read second.
func TestPromotion_RemovalDateAndVersionAreSeparateFacts(t *testing.T) {
	t.Parallel()
	both := openapi.Options{Promotions: openapi.ExtensionPromotions{
		Targets: map[string]openapi.ExtensionTarget{
			"x-sunset":  openapi.TargetDeprecationRemovalDate,
			"x-gone-in": openapi.TargetDeprecationRemovalVersion,
		},
	}}
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      deprecated: true
      x-sunset: "2026-08-01"
      x-gone-in: "9.0.0"
      responses:
        "200":
          description: ok
`
	doc := compilePromotionSpec(t, spec, both)
	op, ok := opByName(doc, "getX")
	require.True(t, ok)
	require.NotNil(t, op.Deprecation)
	assert.Equal(t, "2026-08-01", op.Deprecation.RemovalDate)
	assert.Equal(t, "9.0.0", op.Deprecation.RemovalVersion,
		"both facts survive; neither overwrites the other")
}
