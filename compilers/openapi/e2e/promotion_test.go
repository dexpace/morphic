// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file covers promotion: reading a vendor extension into the typed IR
// field it is the only OpenAPI spelling for. It belongs to no single source
// file — the policy is on the compiler's options, the reading is one function
// beneath both walks, and it is applied at every carrier that can record it.
package e2e_test // external test package — exercises only the public API

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

	model, ok := doc.Types[componentID("Old")].(*ir.Model)
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
		assert.Equal(t, ir.Inferred("extension-promotion"), carrier.provenance.Inferred,
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
	assertKeyDeprecates(t, doc)
	assertMemberKeys(t, doc, diags)
}

// assertKeyDeprecates sweeps the carriers of a deprecation a key states with no
// keyword beside it. Each is a node the compiler builds on its own path, so a
// path that stops reading the key fails on its own row.
func assertKeyDeprecates(t *testing.T, doc *ir.Document) {
	t.Helper()
	op, ok := opByName(doc, "keyDeprecates")
	require.True(t, ok)
	param, ok := openapitest.ParamBySource(op, "byKey")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	header, ok := headerByWire(op.Responses[0].Headers, "X-By-Key")
	require.True(t, ok)
	model, ok := doc.Types[componentID("ByKey")].(*ir.Model)
	require.True(t, ok)
	prop, ok := propByWire(model, "p")
	require.True(t, ok)
	scheme, ok := doc.Auth["auth/openapi/components/securitySchemes/byKey"]
	require.True(t, ok)

	for name, carrier := range map[string]promotionCarrier{
		"operation":   {op.Deprecation, op.Provenance, op.Unmodeled},
		"parameter":   {param.Deprecation, param.Provenance, param.Unmodeled},
		"header":      {header.Deprecation, header.Provenance, header.Unmodeled},
		"type":        {model.Deprecation, model.Provenance, model.Unmodeled},
		"property":    {prop.Deprecation, prop.Provenance, prop.Unmodeled},
		"auth scheme": {scheme.Deprecation, scheme.Provenance, scheme.Unmodeled},
	} {
		require.NotNil(t, carrier.deprecation, "%s: the key is the only statement", name)
		assert.Equal(t, ir.Inferred("extension-promotion"), carrier.provenance.Inferred, name)
		assert.Contains(t, carrier.unmodeled, "openapi:x-fern-availability", name)
	}
	assert.Equal(t, "2031-01-01", param.Deprecation.RemovalDate, "a member fills what the key created")
}

// assertMemberKeys holds the default mapping's other spellings of a member, and
// the two ways an object-valued key misfits, to one report each.
func assertMemberKeys(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	for _, tc := range []struct {
		op   string
		want ir.Deprecation
	}{
		{"messageKey", ir.Deprecation{Message: "use getY instead", RemovalDate: "2030-06-01"}},
		{"objectKey", ir.Deprecation{Message: "goes away", RemovalDate: "2030-01-01"}},
		{"textForAnObject", ir.Deprecation{}},
		{"numberForAMember", ir.Deprecation{}},
		{"firstKeyWins", ir.Deprecation{Message: "the first key"}},
	} {
		op, ok := opByName(doc, tc.op)
		require.True(t, ok, tc.op)
		require.NotNil(t, op.Deprecation, tc.op)
		assert.Equal(t, tc.want, *op.Deprecation, tc.op)
	}
	info := []ir.Severity{ir.SeverityInfo}
	assert.Equal(t, info, diagsAt(diags, "openapi/degraded-construct", "/paths/~1members/post/x-cfDeprecation"),
		"one key read by two sources is one report")
	assert.Equal(t, info, diagsAt(diags, "openapi/degraded-construct", "/paths/~1members/patch/x-forge-sunset/date"),
		"located at the member")
	assert.Equal(t, info, diagsAt(diags, "openapi/degraded-construct",
		"/paths/~1members/delete/x-stainless-deprecation-message"), "the key that lost")
}

// assertEnumOpenness is the corpus row for GitHub #427 and the matrix's
// open-enums row. ir.Enum.Closed is exactly the fact x-extensible-enum states,
// and every enum the compiler builds is closed, so without the promotion the
// extension changed nothing an emitter or a differ could read.
//
// The schemas are the three answers the reading has: the convention's own
// spelling opens the enum, an explicit false declines to, and an enum that
// names no such key is untouched — the last so that the first is a promotion
// rather than a compiler that stopped closing enums. Tier opens one member.
func assertEnumOpenness(t *testing.T, doc *ir.Document) {
	t.Helper()
	tests := []struct {
		schema   string
		closed   bool
		inferred ir.Inferred
	}{
		{"Size", false, "extension-promotion"},
		{"Tier", false, "extension-promotion"},
		{"Shade", true, ""},
		{"Fixed", true, ""},
	}
	for _, tc := range tests {
		enum, ok := doc.Types[componentID(tc.schema)].(*ir.Enum)
		require.True(t, ok, "%s lowers to an enum", tc.schema)
		assert.Equal(t, tc.closed, enum.Closed, "%s openness", tc.schema)
		assert.Equal(t, tc.inferred, enum.Provenance.Inferred, "%s heuristic marker", tc.schema)
	}

	for _, schema := range []string{"Size", "Shade"} {
		enum, ok := doc.Types[componentID(schema)].(*ir.Enum)
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
	t.Helper()
	live, ok := opByName(doc, "getY")
	require.True(t, ok)
	assert.Nil(t, live.Deprecation, "the operation is not deprecated")
	assert.Contains(t, live.Unmodeled, "openapi:x-deprecated-reason")
	assert.Empty(t, live.Provenance.Inferred, "nothing was inferred, so nothing is marked")

	model, ok := doc.Types[componentID("Old")].(*ir.Model)
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

// TestPromotion_DisabledKeepsExtensionsAndNothingElse pins the off switch
// invariant 6 requires. Disabled means the typed field is not written at all,
// and the document compiles to exactly what it did before promotion existed.
func TestPromotion_DisabledKeepsExtensionsAndNothingElse(t *testing.T) {
	t.Parallel()
	doc := compileClean(t, deprecatedOpSpec,
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
		Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
			openapi.TargetDeprecationRemovalVersion: {{Key: "x-gone-in"}},
		},
	}}
	doc := compileClean(t, deprecatedOpSpec, own)
	op, ok := opByName(doc, "getX")
	require.True(t, ok)
	require.NotNil(t, op.Deprecation)
	assert.Equal(t, "9.0.0", op.Deprecation.RemovalVersion, "the caller's key is read")
	assert.Empty(t, op.Deprecation.Message, "a default the caller replaced is not read")
	assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
}

// defaultKeySpec is a document one default source fills one target of: the key,
// writing value at its member if it has one, sits on a deprecated operation, on
// an undeprecated one and on an enum, since the targets live on those carriers
// and a key reaching only the wrong one would read as a mapping that fills
// nothing.
func defaultKeySpec(src openapi.ExtensionSource, value string) string {
	written := "      " + src.Key + ": " + value + "\n"
	if src.Member != "" {
		written = "      " + src.Key + ": {" + src.Member + ": " + value + "}\n"
	}
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      deprecated: true
` + written + `      responses:
        "200":
          description: ok
    put:
      operationId: putX
` + written + `      responses:
        "200":
          description: ok
components:
  schemas:
    E:
      type: string
      enum: [a, b]
` + written
}

// defaultTargetRead says of each target what value states it and how to read the
// field it fills. The deprecation members are read off the deprecated operation,
// and the deprecation itself off the one with no keyword.
var defaultTargetRead = map[openapi.ExtensionTarget]struct {
	value string
	field func(doc *ir.Document, deprecated, plain ir.Operation) string
}{
	openapi.TargetDeprecation: {"deprecated", func(_ *ir.Document, _, plain ir.Operation) string {
		return filledWhen(plain.Deprecation != nil)
	}},
	openapi.TargetDeprecationMessage: {"filled", func(_ *ir.Document, op, _ ir.Operation) string {
		return op.Deprecation.Message
	}},
	openapi.TargetDeprecationSince: {"filled", func(_ *ir.Document, op, _ ir.Operation) string {
		return op.Deprecation.Since
	}},
	openapi.TargetDeprecationRemovalVersion: {"filled", func(_ *ir.Document, op, _ ir.Operation) string {
		return op.Deprecation.RemovalVersion
	}},
	openapi.TargetDeprecationRemovalDate: {"filled", func(_ *ir.Document, op, _ ir.Operation) string {
		return op.Deprecation.RemovalDate
	}},
	openapi.TargetEnumOpen: {"filled", func(doc *ir.Document, _, _ ir.Operation) string {
		enum, ok := doc.Types[componentID("E")].(*ir.Enum)
		return filledWhen(ok && !enum.Closed)
	}},
	openapi.TargetStability: {"beta", func(_ *ir.Document, _, plain ir.Operation) string {
		return filledWhen(plain.Stability == ir.StabilityBeta)
	}},
	openapi.TargetRedaction: {"true", func(doc *ir.Document, _, _ ir.Operation) string {
		return filledWhen(doc.Types[componentID("E")].Common().Sensitive)
	}},
}

// TestPromotion_DefaultTargetsAreTheOnesApplied holds the exported mapping to
// the one the compiler applies, one document per source. Two transcriptions of
// one mapping is one of them going stale unnoticed, and the exported one is what
// a caller changing it starts from.
//
// A default key is read off the mapping rather than listed, so a source this
// test does not know about fails here instead of going unread, and a target with
// no default key has to stay empty.
func TestPromotion_DefaultTargetsAreTheOnesApplied(t *testing.T) {
	t.Parallel()
	defaults := openapi.DefaultExtensionPromotions()
	require.NotEmpty(t, defaults, "an empty mapping would make this vacuous")

	for target, sources := range defaults {
		read, known := defaultTargetRead[target]
		require.True(t, known, "%s is a default target this test reads no field for", target)
		for _, src := range sources {
			t.Run(string(target)+"/"+src.String(), func(t *testing.T) {
				t.Parallel()
				doc, _, err := openapi.New().Compile(t.Context(),
					[]compilers.Source{{Path: "spec.yaml", Data: []byte(defaultKeySpec(src, read.value))}},
					compilers.Options{FormatOptions: openapi.Options{}})
				require.NoError(t, err)
				deprecated, ok := opByName(doc, "getX")
				require.True(t, ok)
				plain, ok := opByName(doc, "putX")
				require.True(t, ok)
				require.NotNil(t, deprecated.Deprecation)

				assert.Equal(t, "filled", read.field(doc, deprecated, plain), "%s is filled by %s", target, src)
			})
		}
	}
	for target := range defaultTargetRead {
		if _, mapped := defaults[target]; !mapped {
			assert.Equal(t, openapi.TargetDeprecationRemovalVersion, target,
				"only the removal version has no default key, as no convention spells one")
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

// TestPromotion_TwoKeysResolveByThePolicyNotTheirSpelling is the public face of
// the precedence a mapping states: on one document, naming two keys in either
// order fills the field from the first, and reports the other at its key. It
// was the key that sorted last, in silence.
func TestPromotion_TwoKeysResolveByThePolicyNotTheirSpelling(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      deprecated: true
      x-alpha-msg: FROM-ALPHA
      x-zulu-msg: FROM-ZULU
      responses:
        "200":
          description: ok
`
	alpha, zulu := openapi.ExtensionSource{Key: "x-alpha-msg"}, openapi.ExtensionSource{Key: "x-zulu-msg"}
	for _, tc := range []struct {
		order []openapi.ExtensionSource
		want  string
		loser string
	}{
		{[]openapi.ExtensionSource{alpha, zulu}, "FROM-ALPHA", "/paths/~1x/get/x-zulu-msg"},
		{[]openapi.ExtensionSource{zulu, alpha}, "FROM-ZULU", "/paths/~1x/get/x-alpha-msg"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: "spec.yaml", Data: []byte(spec)}},
				compilers.Options{FormatOptions: openapi.Options{Promotions: openapi.ExtensionPromotions{
					Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
						openapi.TargetDeprecationMessage: tc.order,
					},
				}}})
			require.NoError(t, err)
			op, ok := opByName(doc, "getX")
			require.True(t, ok)
			require.NotNil(t, op.Deprecation)
			assert.Equal(t, tc.want, op.Deprecation.Message)
			assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/degraded-construct", tc.loser))
			assert.Len(t, diags, 1)
		})
	}
}

// TestPromotion_AnEnumsKeysReportWhatTheyLoseToo holds the enum carrier to the
// reports every other carrier makes. The reading reports here as anywhere, and
// a declaration has to pass that on, so a disagreement or a key with no member
// to read reaches the compile's list rather than ending at the node.
func TestPromotion_AnEnumsKeysReportWhatTheyLoseToo(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Disagrees: {type: string, enum: [a, b], x-first: false, x-second: true}
    NotAnObject: {type: string, enum: [a, b], x-holder: open}
`
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(spec)}},
		compilers.Options{FormatOptions: openapi.Options{Promotions: openapi.ExtensionPromotions{
			Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
				openapi.TargetEnumOpen: {{Key: "x-first"}, {Key: "x-second"}, {Key: "x-holder", Member: "open"}},
			},
		}}})
	require.NoError(t, err)

	for _, name := range []string{"Disagrees", "NotAnObject"} {
		enum, ok := doc.Types[componentID(name)].(*ir.Enum)
		require.True(t, ok, name)
		assert.True(t, enum.Closed, name)
		assert.Empty(t, enum.Provenance.Inferred, name)
	}
	info := []ir.Severity{ir.SeverityInfo}
	assert.Equal(t, info, diagsAt(diags, "openapi/degraded-construct", "/components/schemas/Disagrees/x-second"))
	assert.Equal(t, info, diagsAt(diags, "openapi/degraded-construct", "/components/schemas/NotAnObject/x-holder"))
	assert.Len(t, diags, 2)
}

// TestPromotion_RemovalDateAndVersionAreSeparateFacts pins why a scheduled
// removal is two fields rather than one field carrying which spelling it holds
// (GitHub #417). A document can state both — a sunset date and the release it
// goes in — and one field would have to drop whichever it read second.
func TestPromotion_RemovalDateAndVersionAreSeparateFacts(t *testing.T) {
	t.Parallel()
	both := openapi.Options{Promotions: openapi.ExtensionPromotions{
		Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
			openapi.TargetDeprecationRemovalDate:    {{Key: "x-sunset"}},
			openapi.TargetDeprecationRemovalVersion: {{Key: "x-gone-in"}},
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
	doc := compileClean(t, spec, both)
	op, ok := opByName(doc, "getX")
	require.True(t, ok)
	require.NotNil(t, op.Deprecation)
	assert.Equal(t, "2026-08-01", op.Deprecation.RemovalDate)
	assert.Equal(t, "9.0.0", op.Deprecation.RemovalVersion,
		"both facts survive; neither overwrites the other")
}
