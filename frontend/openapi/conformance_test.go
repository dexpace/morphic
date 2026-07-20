package openapi_test // external test package — exercises only the public API

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/frontend/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// conformanceDir is the corpus of one minimal spec per capability row of
// ir-spec-matrix.md, addressed relative to this test file.
const conformanceDir = "../../testdata/conformance/openapi"

// TestConformance drives one minimal spec per OpenAPI-expressible capability
// through the full frontend and asserts lossless capture: a focused
// capability-specific assertion plus a byte-exact golden IR snapshot. Regenerate
// the goldens with `go test ./frontend/openapi -run TestConformance -update`.
func TestConformance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file   string
		assert func(*testing.T, *ir.Document, []ir.Diagnostic)
	}{
		{"named-types", assertNamedTypes},
		{"inline-types", assertInlineTypes},
		{"allof-inheritance", assertAllOfInheritance},
		{"allof-mixins", assertAllOfMixins},
		{"allof-inline-merge", assertAllOfInlineMerge},
		{"oneof-discriminated", assertOneOfDiscriminated},
		{"anyof-untagged", assertAnyOfUntagged},
		{"negation-not", assertNegationNot},
		{"enum-string", assertEnumString},
		{"enum-numeric", assertEnumNumeric},
		{"scalar-format", assertScalarFormat},
		{"encoding-byte", assertEncodingByte},
		{"nullability-four-states", assertNullabilityFourStates},
		{"nullable-30", assertNullable30},
		{"defaults", assertDefaults},
		{"constraints", assertConstraints},
		{"readonly-writeonly", assertReadOnlyWriteOnly},
		{"recursive", assertRecursive},
		{"maps", assertMaps},
		{"tuples-prefixitems", assertTuples},
		{"literal-const", assertLiteralConst},
		{"tags-grouping", assertTagsGrouping},
		{"http-binding", assertHTTPBinding},
		{"param-styles", assertParamStyles},
		{"multi-content", assertMultiContent},
		{"multipart-encoding", assertMultipartEncoding},
		{"per-status-errors", assertPerStatusErrors},
		{"webhooks", assertWebhooks},
		{"callbacks", assertCallbacks},
		{"deprecation", assertDeprecation},
		{"examples", assertExamples},
		{"docs-summary-desc", assertDocsSummaryDesc},
		{"extensions-x", assertExtensionsX},
		{"servers-variables", assertServersVariables},
		{"security-schemes", assertSecuritySchemes},
		{"security-or-and", assertSecurityOrAnd},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseCorpus(t, tc.file)
			assertNoErrorDiags(t, diags)
			tc.assert(t, doc, diags)
			irtest.CompareGolden(t, filepath.Join(conformanceDir, tc.file+".golden.json"), doc)
		})
	}
}

// parseCorpus reads and parses one corpus spec through the full frontend.
func parseCorpus(t *testing.T, name string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conformanceDir, name+".yaml"))
	require.NoError(t, err)
	doc, diags, err := openapi.New().Parse(t.Context(),
		[]frontend.Source{{Path: name + ".yaml", Data: data}}, frontend.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// assertNoErrorDiags fails when any diagnostic has error severity.
func assertNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
}

// namedID is the stable TypeID of a components-named schema.
func namedID(name string) ir.TypeID {
	return ir.TypeID("t/openapi/components/schemas/" + name)
}

// allOperations flattens every operation across a document's service groups.
func allOperations(doc *ir.Document) []ir.Operation {
	var out []ir.Operation
	var walk func(gs []ir.OperationGroup)
	walk = func(gs []ir.OperationGroup) {
		for _, g := range gs {
			out = append(out, g.Operations...)
			walk(g.Groups)
		}
	}
	for _, svc := range doc.Services {
		walk(svc.Groups)
	}
	return out
}

// opByName finds an operation by its source operationId.
func opByName(doc *ir.Document, source string) (ir.Operation, bool) {
	for _, op := range allOperations(doc) {
		if op.Name.Source == source {
			return op, true
		}
	}
	return ir.Operation{}, false
}

// propByWire returns the property of m with the given wire name.
func propByWire(m *ir.Model, wire string) (ir.Property, bool) {
	for _, p := range m.Properties {
		if p.WireName == wire {
			return p, true
		}
	}
	return ir.Property{}, false
}

func assertNamedTypes(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	user, ok := doc.Types[namedID("User")].(*ir.Model)
	require.True(t, ok, "named schema User present under its pointer-derived ID")
	assert.False(t, user.Anonymous)
	addr, ok := propByWire(user, "address")
	require.True(t, ok)
	assert.Equal(t, namedID("Address"), addr.Type.Target)
	_, ok = doc.Types[namedID("Address")].(*ir.Model)
	assert.True(t, ok, "referenced Address resolves in the registry")
}

func assertInlineTypes(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	order, ok := doc.Types[namedID("Order")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, order.Properties, 1)
	shipping := order.Properties[0]
	// hoisted exactly once, under its pointer-derived anonymous ID.
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Order/properties/shipping"),
		shipping.Type.Target)
	inline, ok := doc.Types[shipping.Type.Target].(*ir.Model)
	require.True(t, ok, "the inline object was hoisted as its own type")
	assert.True(t, inline.Anonymous)
	assert.Equal(t, "shipping", inline.Name.Hint)
}

func assertAllOfInheritance(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	d, ok := doc.Types[namedID("Derived")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, d.Base, "sole allOf $ref becomes Base")
	assert.Equal(t, namedID("Base"), d.Base.Target)
	assert.Empty(t, d.Mixins)
	_, ok = propByWire(d, "extra")
	assert.True(t, ok, "property declared alongside allOf survives")
}

func assertAllOfMixins(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	c, ok := doc.Types[namedID("C")].(*ir.Model)
	require.True(t, ok)
	assert.Nil(t, c.Base, "multiple non-hierarchy refs stay Mixins, none is Base")
	require.Len(t, c.Mixins, 2)
	assert.Equal(t, namedID("A"), c.Mixins[0].Target)
	assert.Equal(t, namedID("B"), c.Mixins[1].Target)
}

func assertAllOfInlineMerge(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("Merged")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, m.Base)
	assert.Equal(t, namedID("Base"), m.Base.Target)
	_, ok = propByWire(m, "name")
	assert.True(t, ok, "inline allOf branch contributes its properties")
}

func assertOneOfDiscriminated(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	pet, ok := doc.Types[namedID("Pet")].(*ir.Union)
	require.True(t, ok, "oneOf survives as a Union node, never collapsed")
	assert.True(t, pet.Exclusive)
	require.Len(t, pet.Variants, 2)
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, "petType", pet.Discriminator.PropertyName)
	assert.Equal(t, namedID("Cat"), pet.Discriminator.Mapping["cat"])
	assert.Equal(t, namedID("Dog"), pet.Discriminator.Mapping["dog"])
}

func assertAnyOfUntagged(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	u, ok := doc.Types[namedID("StringOrNumber")].(*ir.Union)
	require.True(t, ok)
	assert.False(t, u.Exclusive, "anyOf is a non-exclusive union")
	assert.Nil(t, u.Discriminator)
	assert.Len(t, u.Variants, 2)
}

func assertNegationNot(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	m, ok := doc.Types[namedID("NotFoo")].(*ir.Model)
	require.True(t, ok)
	raw, ok := m.Extensions["openapi:not"]
	require.True(t, ok, "not-keyword preserved verbatim in Extensions")
	assert.JSONEq(t, `{"required":["b"]}`, string(raw))
	var found bool
	for _, d := range diags {
		if d.Code == "openapi/validation-only-keyword" {
			found = true
			assert.Equal(t, ir.SeverityInfo, d.Severity)
		}
	}
	assert.True(t, found, "expected a validation-only-keyword info diagnostic")
}

func assertEnumString(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	e, ok := doc.Types[namedID("Color")].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.PrimString, e.ValueType)
	assert.True(t, e.Closed)
	require.Len(t, e.Members, 3)
	assert.Equal(t, "red", e.Members[0].Value.Str)
	assert.Equal(t, "blue", e.Members[2].Value.Str)
}

func assertEnumNumeric(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	e, ok := doc.Types[namedID("BigCodes")].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.PrimInteger, e.ValueType)
	require.Len(t, e.Members, 2)
	assert.Equal(t, ir.ValueNumber, e.Members[1].Value.Kind)
	// BigVal member value preserves the full 64-bit integer.
	assert.Equal(t, ir.BigVal("9007199254740993"), e.Members[1].Value.Num)
}

func assertScalarFormat(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	h, ok := doc.Types[namedID("Holder")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, h.Properties, 1)
	sc, ok := doc.Types[h.Properties[0].Type.Target].(*ir.Scalar)
	require.True(t, ok, "unknown format hoists a named Scalar")
	require.NotNil(t, sc.Encoding)
	assert.Equal(t, "hex-color", sc.Encoding.Name)
	require.NotNil(t, sc.Base)
	assert.Equal(t, ir.TypeID("t/prim/string"), sc.Base.Target)
}

func assertEncodingByte(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	h, ok := doc.Types[namedID("Holder")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, h.Properties, 1)
	sc, ok := doc.Types[h.Properties[0].Type.Target].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, sc.Encoding)
	assert.Equal(t, "base64", sc.Encoding.Name)
	require.NotNil(t, sc.Encoding.WireType)
	assert.Equal(t, ir.TypeID("t/prim/string"), sc.Encoding.WireType.Target)
	require.NotNil(t, sc.Base)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), sc.Base.Target)
}

func assertNullabilityFourStates(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 4)
	states := map[string]ir.Property{}
	for _, p := range m.Properties {
		states[p.WireName] = p
	}
	assert.True(t, states["reqPlain"].Required)
	assert.False(t, states["reqPlain"].Type.Nullable)
	assert.True(t, states["reqNull"].Required)
	assert.True(t, states["reqNull"].Type.Nullable)
	assert.False(t, states["optPlain"].Required)
	assert.False(t, states["optPlain"].Type.Nullable)
	assert.False(t, states["optNull"].Required)
	assert.True(t, states["optNull"].Type.Nullable)
}

func assertNullable30(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	assert.True(t, m.Properties[0].Type.Nullable, "3.0 nullable lowers to the same IR bit")
}

func assertDefaults(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	require.NotNil(t, m.Properties[0].Default)
	assert.Equal(t, ir.ValueNumber, m.Properties[0].Default.Kind)
	assert.Equal(t, ir.BigVal("9007199254740993"), m.Properties[0].Default.Num)
}

func assertConstraints(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	c := m.Properties[0].Constraints
	require.NotNil(t, c)
	// Exact decimal strings — a float64 path would corrupt all three.
	require.NotNil(t, c.Min)
	require.NotNil(t, c.Max)
	require.NotNil(t, c.MultipleOf)
	assert.Equal(t, ir.BigVal("0.30000000000000004"), *c.Min)
	assert.Equal(t, ir.BigVal("9007199254740993"), *c.Max)
	assert.Equal(t, ir.BigVal("0.1"), *c.MultipleOf)
}

func assertReadOnlyWriteOnly(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	r, ok := propByWire(m, "r")
	require.True(t, ok)
	w, ok := propByWire(m, "w")
	require.True(t, ok)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead}}, r.Visibility)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, w.Visibility)
}

func assertRecursive(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	n, ok := doc.Types[namedID("Node")].(*ir.Model)
	require.True(t, ok)
	next, ok := propByWire(n, "next")
	require.True(t, ok)
	assert.Equal(t, namedID("Node"), next.Type.Target, "self-reference terminates on the interned ID")
}

func assertMaps(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	om, ok := doc.Types[namedID("OpenMap")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, om.AdditionalProps)
	assert.Equal(t, ir.TypeID("t/prim/integer"), om.AdditionalProps.Value.Target)
	pm, ok := doc.Types[namedID("PatternMap")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, pm.AdditionalProps)
	require.Len(t, pm.AdditionalProps.Patterns, 1)
	assert.Equal(t, "^x-", pm.AdditionalProps.Patterns[0].Pattern)
	cr, ok := doc.Types[namedID("ClosedRecord")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.AdditionalClosed, cr.Additional)
	ca, ok := doc.Types[namedID("ClosedAfter")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.AdditionalClosedAfterComposition, ca.Additional)
}

func assertTuples(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	p, ok := doc.Types[namedID("Pair")].(*ir.Tuple)
	require.True(t, ok, "prefixItems hoists a Tuple")
	require.Len(t, p.Elems, 2)
	assert.Equal(t, ir.TypeID("t/prim/string"), p.Elems[0].Target)
	assert.Equal(t, ir.TypeID("t/prim/integer"), p.Elems[1].Target)
}

func assertLiteralConst(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	l, ok := doc.Types[namedID("Version")].(*ir.Literal)
	require.True(t, ok, "const hoists a Literal")
	assert.Equal(t, ir.ValueString, l.Value.Kind)
	assert.Equal(t, "v1", l.Value.Str)
}

func assertTagsGrouping(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	assert.Equal(t, "pets", doc.Services[0].Groups[0].Name.Source)
	require.Len(t, doc.TagDefs, 1)
	assert.Equal(t, "pets", doc.TagDefs[0].Name)
}

func assertHTTPBinding(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	hb := op.Bindings.HTTP[0]
	assert.Equal(t, "GET", hb.Method)
	assert.Equal(t, "/items/{id}", hb.URITemplate)
}

func assertParamStyles(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "search")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	byParam := map[string]ir.HTTPParamBinding{}
	for _, pb := range op.Bindings.HTTP[0].ParamBindings {
		byParam[pb.Param] = pb
	}
	q := byParam["q"]
	assert.Equal(t, "form", q.Style, "query default style is form")
	require.NotNil(t, q.Explode)
	assert.True(t, *q.Explode, "form default explodes")
	assert.Equal(t, "deepObject", byParam["filter"].Style)
	assert.Equal(t, "application/json", byParam["payload"].ContentType, "content-style param records its media type")
}

func assertMultiContent(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "getData")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 2, "both media types kept, no primary selection")
	assert.Equal(t, "application/json", op.Responses[0].Payload.Contents[0].MediaType)
	assert.Equal(t, "application/xml", op.Responses[0].Payload.Contents[1].MediaType)
}

func assertMultipartEncoding(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "upload")
	require.True(t, ok)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	enc := op.Request.Contents[0].Encoding
	require.NotEmpty(t, enc, "multipart parts carry PartEncoding keyed by PropID")
	var sawFile, sawMulti bool
	for _, pe := range enc {
		if pe.Filename {
			sawFile = true
		}
		if pe.Multi {
			sawMulti = true
		}
	}
	assert.True(t, sawFile, "binary file part carries Filename")
	assert.True(t, sawMulti, "array part carries Multi")
}

func assertPerStatusErrors(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "getWidgets")
	require.True(t, ok)
	require.Len(t, op.Responses, 1, "the 2xx success response")
	faults := map[string]ir.StatusRange{}
	var sawDefault bool
	for _, ec := range op.Errors {
		require.Len(t, ec.Conditions.StatusCodes, 1)
		rng := ec.Conditions.StatusCodes[0]
		if rng.From == 0 && rng.To == 0 {
			sawDefault = true
			assert.Empty(t, ec.Fault, "the default catch-all is unclassified")
			continue
		}
		faults[ec.Fault] = rng
	}
	assert.Equal(t, ir.StatusRange{From: 404, To: 404}, faults["client"])
	assert.Equal(t, ir.StatusRange{From: 500, To: 599}, faults["server"])
	assert.True(t, sawDefault, "the default response becomes a catch-all error case")
}

func assertWebhooks(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "onNewPet")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	assert.True(t, op.Bindings.HTTP[0].IsWebhook, "webhook operation carries IsWebhook")
}

func assertCallbacks(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "subscribe")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, "{$request.body#/callbackUrl}", op.Bindings.HTTP[0].Callbacks[0].Expression)
	assert.NotEmpty(t, op.Bindings.HTTP[0].Callbacks[0].Operations)
	_, ok = opByName(doc, "onEvent")
	assert.True(t, ok, "the callback operation is registered alongside its parent")
}

func assertDeprecation(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "oldOp")
	require.True(t, ok)
	assert.NotNil(t, op.Deprecation)
	m, ok := doc.Types[namedID("OldModel")].(*ir.Model)
	require.True(t, ok)
	assert.NotNil(t, m.Deprecation)
	require.Len(t, m.Properties, 1)
	assert.NotNil(t, m.Properties[0].Deprecation)
}

func assertExamples(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	assert.Len(t, m.Properties[0].Examples, 2)
}

func assertDocsSummaryDesc(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "ping")
	require.True(t, ok)
	assert.Equal(t, "Ping the server", op.Docs.Summary)
	assert.Equal(t, "Returns pong", op.Docs.Description)
}

func assertExtensionsX(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	raw, ok := m.Extensions["openapi:x-rate-limit"]
	require.True(t, ok, "x-* extensions are namespaced under openapi:")
	assert.JSONEq(t, "100", string(raw))
}

func assertServersVariables(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Servers, 1)
	assert.Equal(t, "https://{env}.example.com/v1", doc.Servers[0].URLTemplate)
	require.Len(t, doc.Servers[0].Variables, 1)
	v := doc.Servers[0].Variables[0]
	assert.Equal(t, "env", v.Name)
	assert.Equal(t, "api", v.Default)
	assert.Equal(t, []string{"api", "staging"}, v.Enum)
}

func assertSecuritySchemes(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	kinds := map[ir.AuthKind]bool{}
	for _, s := range doc.Auth {
		kinds[s.Kind] = true
	}
	assert.True(t, kinds[ir.AuthKindAPIKey])
	assert.True(t, kinds[ir.AuthKindHTTPBasic])
	assert.True(t, kinds[ir.AuthKindHTTPBearer])
	assert.True(t, kinds[ir.AuthKindOAuth2])
	assert.True(t, kinds[ir.AuthKindOpenIDConnect])
	assert.True(t, kinds[ir.AuthKindMutualTLS])
}

func assertSecurityOrAnd(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	auth := doc.Services[0].Auth
	require.Len(t, auth, 3, "OR of three options in source order")
	assert.Len(t, auth[0].Schemes, 1)
	assert.Len(t, auth[1].Schemes, 2, "the ANDed option requires two schemes together")
	assert.Empty(t, auth[2].Schemes, "the empty option means no auth is acceptable")
	op, ok := opByName(doc, "publicOp")
	require.True(t, ok)
	assert.Empty(t, op.Auth, "security: [] makes the operation explicitly public")
}
