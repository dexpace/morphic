// This file is a package-level suite, not a per-source-file test: it runs the
// capability-conformance corpus (one minimal spec per ir-spec-matrix.md row)
// through the whole compiler to keep "lossless by default" honest, so it has no
// single source file to pair with.
package openapi_test // external test package — exercises only the public API

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// conformanceDir is the corpus of one minimal spec per capability row of
// ir-spec-matrix.md, addressed relative to this test file.
const conformanceDir = "../../testdata/conformance/openapi"

// TestConformance drives one minimal spec per OpenAPI-expressible capability
// through the full compiler and asserts lossless capture: a focused
// capability-specific assertion plus a byte-exact golden IR snapshot. Regenerate
// the goldens with `go test ./compilers/openapi -run TestConformance -update`.
//
// The corpus below is knowingly incomplete (GitHub #126): a set of IR fields
// the compiler assigns are never non-zero anywhere in it, among them
// Model.DiscriminatorValue and Discriminator.Property, so allOf inheritance
// under a discriminator has no corpus witness at all. Filling those in one case
// at a time is deliberately not the plan — a corpus that shares the compiler's
// blind spots is the thing to fix, so what #126 asks for is the reflective walk
// that produces the list, run as a test that fails when a field the compiler
// assigns has no witness here. Until that lands the set is not tracked by a
// number here, because nothing recomputes one.
func TestConformance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file   string
		assert func(*testing.T, *ir.Document, []ir.Diagnostic)
	}{
		{"named-types", assertNamedTypes},
		{"inline-types", assertInlineTypes},
		{"component-reuse", assertComponentReuse},
		{"allof-inheritance", assertAllOfInheritance},
		{"allof-mixins", assertAllOfMixins},
		{"allof-inline-merge", assertAllOfInlineMerge},
		{"allof-required-only", assertAllOfRequiredOnly},
		{"allof-oneof-cooccurrence", assertAllOfOneOfCooccurrence},
		{"oneof-discriminated", assertOneOfDiscriminated},
		{"anyof-untagged", assertAnyOfUntagged},
		{"negation-not", assertNegationNot},
		{"enum-string", assertEnumString},
		{"enum-numeric", assertEnumNumeric},
		{"scalar-format", assertScalarFormat},
		{"encoding-byte", assertEncodingByte},
		{"nullability-four-states", assertNullabilityFourStates},
		{"nullable-30", assertNullable30},
		{"nullable-31-ref", assertNullable31Ref},
		{"defaults", assertDefaults},
		{"yaml-timestamp-scalars", assertYAMLTimestampScalars},
		{"constraints", assertConstraints},
		{"numeric-precision", assertNumericPrecision},
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
		{"sequential-media", assertSequentialMedia},
		{"per-status-errors", assertPerStatusErrors},
		{"webhooks", assertWebhooks},
		{"callbacks", assertCallbacks},
		{"deprecation", assertDeprecation},
		{"examples", assertExamples},
		{"docs-summary-desc", assertDocsSummaryDesc},
		{"extensions-x", assertExtensionsX},
		{"inline-annotations", assertInlineAnnotations},
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
			assertJSONRoundTrip(t, doc)
		})
	}
}

// parseCorpus reads and parses one corpus spec through the full compiler.
func parseCorpus(t *testing.T, name string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conformanceDir, name+".yaml"))
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// assertJSONRoundTrip marshals doc, reads it back, and re-marshals, requiring
// the two encodings to be byte-identical — the serialized oracle fuzz_test.go
// and internal/harness already apply, compared as JSON so the unpreservable
// nil-vs-empty-collection distinction is ignored while real loss still shows.
//
// Invariant 7 is asserted per corpus document rather than only on the golden
// one because each spec reaches IR shapes the others never build — a reserved
// map key, a preserved raw payload, a sum-typed node — and only the shapes a
// document contains can round-trip wrongly.
func assertJSONRoundTrip(t *testing.T, doc *ir.Document) {
	t.Helper()
	first, err := json.Marshal(doc)
	require.NoError(t, err)
	var back ir.Document
	require.NoError(t, json.Unmarshal(first, &back))
	second, err := json.Marshal(&back)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second), "round-trip must preserve the document")
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

// propsByWire indexes a model's properties by wire name.
func propsByWire(props []ir.Property) map[string]ir.Property {
	out := make(map[string]ir.Property, len(props))
	for _, p := range props {
		out[p.WireName] = p
	}
	return out
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

// paramByName returns the operation's logical parameter with the given source
// name.
func paramByName(op ir.Operation, name string) (ir.Parameter, bool) {
	for _, p := range op.Params {
		if p.Name.Source == name {
			return p, true
		}
	}
	return ir.Parameter{}, false
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

// assertComponentReuse covers the non-schema half of `$ref`: OpenAPI lets a
// parameter, requestBody, response, header, callback, or whole path item be
// declared once under components and referenced from many operations. Each
// lowers at its declaration, so the shared node is interned once however many
// operations reach it, while the operations that reach it stay distinct.
func assertComponentReuse(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	widgets := operationAt(t, doc, "GET", "/widgets")
	gadgets := operationAt(t, doc, "GET", "/gadgets")
	order := operationAt(t, doc, "POST", "/orders")
	assert.NotEqual(t, widgets.ID, gadgets.ID, "one path item mounted twice is two operations")
	assert.Equal(t, widgets.Provenance.Pointer, gadgets.Provenance.Pointer,
		"...both written at the one component that declares them")

	// Every hoisted type in this document belongs to a component declaration:
	// no operation contributes a pointer of its own, because none declares a
	// schema of its own.
	for id, td := range doc.Types {
		if td.Common().Provenance.Pointer == "" {
			continue // shared primitives are pointerless by construction
		}
		assert.True(t, strings.HasPrefix(td.Common().Provenance.Pointer, "/components/"),
			"type %s hoists at a component declaration, not a use site", id)
	}

	sortID := ir.TypeID("t/anon/components/parameters/Sort/schema")
	require.Len(t, widgets.Params, 1)
	require.Len(t, gadgets.Params, 1)
	assert.Equal(t, sortID, widgets.Params[0].Type.Target)
	assert.Equal(t, sortID, gadgets.Params[0].Type.Target, "the shared parameter interns once")

	listedID := ir.TypeID("t/anon/components/responses/Listed/content/application~1json/schema")
	assert.Equal(t, listedID, widgets.Responses[0].Payload.Contents[0].Type.Target)
	assert.Equal(t, listedID, order.Responses[0].Payload.Contents[0].Type.Target,
		"a response reused across unrelated operations interns once")

	bodyID := ir.TypeID("t/anon/components/requestBodies/OrderBody/content/application~1json/schema")
	require.NotNil(t, order.Request)
	assert.Equal(t, bodyID, order.Request.Contents[0].Type.Target)
	assert.Equal(t, "OrderBody", doc.Types[bodyID].Common().Name.Hint,
		"a shared body is named after its component, not the operation that reached it first")

	require.Len(t, widgets.Responses[0].Headers, 1)
	header := widgets.Responses[0].Headers[0]
	assert.Equal(t, ir.TypeID("t/anon/components/headers/RateUnit/schema"), header.Type.Target)
	assert.Equal(t, "/components/responses/Listed/headers/X-Rate-Unit", header.Provenance.Pointer,
		"the header's identity is the map entry that binds its name")

	require.Len(t, order.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, "{$request.body#/callbackUrl}", order.Bindings.HTTP[0].Callbacks[0].Expression)
	_, ok := opByName(doc, "onShipped")
	assert.True(t, ok, "the shared callback's operation is registered alongside its parent")
}

// operationAt finds the operation bound to one HTTP method and URI template.
// Operations reached through a shared path item have no distinguishing name of
// their own, so their binding is what tells them apart.
func operationAt(t *testing.T, doc *ir.Document, method, uri string) ir.Operation {
	t.Helper()
	for _, op := range allOperations(doc) {
		for _, hb := range op.Bindings.HTTP {
			if hb.Method == method && hb.URITemplate == uri {
				return op
			}
		}
	}
	t.Fatalf("no operation bound to %s %s", method, uri)
	return ir.Operation{}
}

func assertAllOfInheritance(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	d, ok := doc.Types[namedID("Derived")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, d.Base, "sole allOf $ref becomes Base")
	assert.Equal(t, namedID("Base"), d.Base.Target)
	assert.Empty(t, d.Mixins)
	_, ok = propByWire(d, "extra")
	assert.True(t, ok, "property declared alongside allOf survives")
	// Model-level detail declared alongside allOf must be captured, not dropped.
	assert.Equal(t, "A Base specialized with one extra field.", d.Docs.Description,
		"composed schema keeps its description")
	assert.NotNil(t, d.Deprecation, "composed schema keeps deprecated")
	assert.Equal(t, ir.AdditionalClosed, d.Additional,
		"composed schema keeps additionalProperties: false")
	assert.Contains(t, d.Preserved, "openapi:x-team", "composed schema keeps x-* extensions")
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

func assertAllOfRequiredOnly(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	d, ok := doc.Types[namedID("Derived")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, d.Base, "the $ref branch still becomes Base")
	name, ok := propByWire(d, "name")
	require.True(t, ok, "the inline branch's own property survives")
	assert.True(t, name.Required,
		"a required-only allOf branch attaches across the whole composition, not just its own properties map (issue #29)")
}

// assertAllOfOneOfCooccurrence pins both halves of the co-declared composition
// rule: §4.3 distributes a union whose branches all name referents, and §4.8
// keeps one with an inline branch verbatim rather than distributing it halfway.
// The outside reference to a branch pointer pins the third thing: a composed
// variant is Morphic's own node, so it cannot be taken by, or take from, a $ref.
func assertAllOfOneOfCooccurrence(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	combo, ok := doc.Types[namedID("Combo")].(*ir.Union)
	require.True(t, ok, "the union is the value, not a preserved sibling")
	require.Len(t, combo.Variants, 2)
	for i, branch := range []string{"A", "B"} {
		v, ok := doc.Types[combo.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok, "variant %d composes as a model", i)
		require.NotNil(t, v.Base, "the allOf composition rides on every variant")
		assert.Equal(t, namedID("Base"), v.Base.Target)
		require.Len(t, v.Mixins, 1)
		assert.Equal(t, namedID(branch), v.Mixins[0].Target)
	}

	mixed, ok := doc.Types[namedID("MixedKinds")].(*ir.Model)
	require.True(t, ok, "the structural body survives")
	entry, ok := mixed.Preserved["openapi:oneOf"]
	require.True(t, ok, "and the union it could not absorb survives beside it")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)

	outsider, ok := doc.Types[namedID("Outsider")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, outsider.Properties, 1)
	branchNode, ok := doc.Types[outsider.Properties[0].Type.Target].(*ir.Scalar)
	require.True(t, ok, "the branch pointer hoists an alias of what the branch references")
	require.NotNil(t, branchNode.Base)
	assert.Equal(t, namedID("A"), branchNode.Base.Target,
		"the reference gets the branch schema, never the variant composed for that branch")
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
	raw, ok := m.Preserved["openapi:not"]
	require.True(t, ok, "not-keyword kept verbatim under Preserved")
	assert.JSONEq(t, `{"required":["b"]}`, string(raw.Value))
	assert.Equal(t, ir.ReasonValidationOnly, raw.Reason)
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
	states := propsByWire(m.Properties)
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

func assertNullable31Ref(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("Owner")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 2)
	byName := propsByWire(m.Properties)

	assert.True(t, byName["p"].Type.Nullable,
		"3.1's type-array null spelling normalizes to the same IR bit at a $ref site")
	assert.Equal(t, namedID("Target"), byName["p"].Type.Target, "the ref resolves to the named component")

	assert.True(t, byName["q"].Type.Nullable,
		"a union's null branch is stripped into the ref's Nullable bit, so the ref must carry it")
	assert.Equal(t, namedID("UnionTarget"), byName["q"].Type.Target, "the ref resolves to the union component")

	u, ok := doc.Types[namedID("UnionTarget")].(*ir.Union)
	require.True(t, ok)
	assert.Len(t, u.Variants, 2, "the null branch lifts to the ref rather than becoming a variant")
}

func assertDefaults(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	n, ok := propByWire(m, "n")
	require.True(t, ok)
	require.NotNil(t, n.Default)
	assert.Equal(t, ir.ValueNumber, n.Default.Kind)
	assert.Equal(t, ir.BigVal("9007199254740993"), n.Default.Num)

	// A declaration-site default reaches the referencing property, and the
	// declaration keeps it verbatim because the type node has no field for it.
	viaRef, ok := propByWire(m, "viaRef")
	require.True(t, ok)
	require.NotNil(t, viaRef.Default, "the referent's default binds the reference site")
	assert.Equal(t, ir.BigVal("41"), viaRef.Default.Num)

	decl, ok := doc.Types[namedID("DeclaredDefault")].(*ir.Scalar)
	require.True(t, ok)
	entry, ok := decl.Preserved["openapi:default"]
	require.True(t, ok, "the declaration's own default is kept as residue")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `41`, string(entry.Value))
}

// assertYAMLTimestampScalars covers a YAML 1.1 quirk: an unquoted date like
// 2021-01-01 resolves to tag !!timestamp, not !!str. It must survive as the
// literal string everywhere OpenAPI's JSON data model can carry one — enum,
// const, a property default, a schema-level example, and a media-type
// example — with nothing dropped or degraded to null.
func assertYAMLTimestampScalars(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assert.Empty(t, diags, "every unquoted date converts cleanly; nothing is dropped or degraded")

	d, ok := doc.Types[namedID("D")].(*ir.Enum)
	require.True(t, ok, "D stays a closed Enum of the real dates, not a union of null literals")
	assert.Equal(t, ir.PrimString, d.ValueType)
	require.Len(t, d.Members, 2)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, d.Members[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2022-02-02"}, d.Members[1].Value)

	k, ok := doc.Types[namedID("K")].(*ir.Literal)
	require.True(t, ok, "K's const hoists a real Literal, not the schemaless top type")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, k.Value)

	s, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, s.Properties, 1)
	prop := s.Properties[0]
	require.NotNil(t, prop.Default, "the property default is preserved")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *prop.Default)
	require.Len(t, prop.Examples, 1, "the schema-level example is preserved")
	require.NotNil(t, prop.Examples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *prop.Examples[0].Value)

	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	mediaExamples := op.Responses[0].Payload.Contents[0].Examples
	require.Len(t, mediaExamples, 1, "the media-type example is preserved")
	require.NotNil(t, mediaExamples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *mediaExamples[0].Value)
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

	// The object's own cardinality bound lands on the Model, not on a property
	// and not nowhere (GitHub #129).
	require.NotNil(t, m.Constraints)
	require.NotNil(t, m.Constraints.MinProps)
	require.NotNil(t, m.Constraints.MaxProps)
	assert.Equal(t, int64(1), *m.Constraints.MinProps)
	assert.Equal(t, int64(4), *m.Constraints.MaxProps)
}

func assertNumericPrecision(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)

	// Bounds beyond float64 range and a huge integer beyond int64, exact.
	bounded, ok := propByWire(m, "bounded")
	require.True(t, ok)
	require.NotNil(t, bounded.Constraints)
	require.NotNil(t, bounded.Constraints.Min)
	require.NotNil(t, bounded.Constraints.Max)
	require.NotNil(t, bounded.Constraints.MultipleOf)
	assert.Equal(t, ir.BigVal("1.8e308"), *bounded.Constraints.Min)
	assert.Equal(t, ir.BigVal("123456789012345678901234567890"), *bounded.Constraints.Max)
	assert.Equal(t, ir.BigVal("1e-30"), *bounded.Constraints.MultipleOf)

	// A leading-dot spelling is canonicalized to JSON form; a high-precision
	// decimal is kept to the last digit.
	exclusive, ok := propByWire(m, "exclusive")
	require.True(t, ok)
	require.NotNil(t, exclusive.Constraints)
	require.NotNil(t, exclusive.Constraints.Min)
	require.NotNil(t, exclusive.Constraints.Max)
	assert.True(t, exclusive.Constraints.ExclusiveMin)
	assert.True(t, exclusive.Constraints.ExclusiveMax)
	assert.Equal(t, ir.BigVal("0.5"), *exclusive.Constraints.Min)
	assert.Equal(t, ir.BigVal("0.12345678901234567890123456789"), *exclusive.Constraints.Max)

	// A default beyond float64 range is captured as a number, not a string.
	withDefault, ok := propByWire(m, "withDefault")
	require.True(t, ok)
	require.NotNil(t, withDefault.Default)
	assert.Equal(t, ir.ValueNumber, withDefault.Default.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), withDefault.Default.Num)

	// A const beyond float64 range hoists a Literal over the exact number.
	pinned, ok := propByWire(m, "pinned")
	require.True(t, ok)
	lit, ok := doc.Types[pinned.Type.Target].(*ir.Literal)
	require.True(t, ok)
	assert.Equal(t, ir.ValueNumber, lit.Value.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), lit.Value.Num)

	// Numeric enum members keep their exact value past int64 range.
	choice, ok := propByWire(m, "choice")
	require.True(t, ok)
	enum, ok := doc.Types[choice.Type.Target].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, ir.BigVal("123456789012345678901234567890"), enum.Members[1].Value.Num)

	// A leading-dot example is canonicalized losslessly.
	sampled, ok := propByWire(m, "sampled")
	require.True(t, ok)
	require.Len(t, sampled.Examples, 1)
	require.NotNil(t, sampled.Examples[0].Value)
	assert.Equal(t, ir.BigVal("0.5"), sampled.Examples[0].Value.Num)

	assertYAMLIntegerBases(t, doc, m)
}

// assertYAMLIntegerBases pins the value every numeric site stores for an integer
// whose YAML base comes from its prefix. Reading the source spelling as base 10
// would store 644 where the document said 420, and would do it silently.
func assertYAMLIntegerBases(t *testing.T, doc *ir.Document, m *ir.Model) {
	t.Helper()
	mode, ok := propByWire(m, "mode")
	require.True(t, ok)
	require.NotNil(t, mode.Default)
	assert.Equal(t, ir.BigVal("420"), mode.Default.Num, "0644 is octal in YAML")

	enum, ok := doc.Types[mode.Type.Target].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, ir.BigVal("420"), enum.Members[0].Value.Num)
	assert.Equal(t, ir.BigVal("493"), enum.Members[1].Value.Num)

	bounds, ok := propByWire(m, "bounds")
	require.True(t, ok)
	require.NotNil(t, bounds.Constraints)
	require.NotNil(t, bounds.Constraints.Min)
	require.NotNil(t, bounds.Constraints.Max)
	require.NotNil(t, bounds.Constraints.MultipleOf)
	assert.Equal(t, ir.BigVal("15"), *bounds.Constraints.Min, "017 is octal")
	assert.Equal(t, ir.BigVal("31"), *bounds.Constraints.Max, "0x1f is hex")
	assert.Equal(t, ir.BigVal("10"), *bounds.Constraints.MultipleOf, "0b1010 is binary")
	require.Len(t, bounds.Examples, 1)
	require.NotNil(t, bounds.Examples[0].Value)
	assert.Equal(t, ir.BigVal("30"), bounds.Examples[0].Value.Num, "0b11110 is binary")

	// "_" is a separator, and a leading zero YAML resolves as a decimal float
	// still has to come out as a JSON-valid literal.
	counted, ok := propByWire(m, "counted")
	require.True(t, ok)
	require.NotNil(t, counted.Default)
	assert.Equal(t, ir.BigVal("1000000"), counted.Default.Num)
	loose, ok := propByWire(m, "loose")
	require.True(t, ok)
	require.NotNil(t, loose.Default)
	assert.Equal(t, ir.BigVal("9"), loose.Default.Num)
}

func assertReadOnlyWriteOnly(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	r, ok := propByWire(m, "r")
	require.True(t, ok)
	w, ok := propByWire(m, "w")
	require.True(t, ok)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}, r.Visibility)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, w.Visibility)

	// A declaration-site readOnly reaches the referencing property, and the
	// declaration keeps it verbatim because the type node has no field for it.
	viaRef, ok := propByWire(m, "viaRef")
	require.True(t, ok)
	assert.Equal(t, r.Visibility, viaRef.Visibility, "the referent's readOnly binds the reference site")

	decl, ok := doc.Types[namedID("DeclaredReadOnly")].(*ir.Scalar)
	require.True(t, ok)
	entry, ok := decl.Preserved["openapi:readOnly"]
	require.True(t, ok, "the declaration's own readOnly is kept as residue")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `true`, string(entry.Value))
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

	// The path item's shared parameter merges into both of its operations and
	// interns its schema once, at the path item's own pointer (issue #36).
	clearSearch, ok := opByName(doc, "clearSearch")
	require.True(t, ok, "the path item's second operation is registered")
	searchReqID, ok := paramByName(op, "requestId")
	require.True(t, ok, "search inherits the path-item-level parameter")
	clearReqID, ok := paramByName(clearSearch, "requestId")
	require.True(t, ok, "clearSearch inherits the same path-item-level parameter")
	assert.Equal(t, searchReqID.Type.Target, clearReqID.Type.Target,
		"both operations resolve the shared path-item parameter to the same interned schema")
	assert.Equal(t, ir.TypeID("t/anon/paths/~1search/parameters/0/schema"), searchReqID.Type.Target,
		"the shared schema is hoisted at the path item's own pointer, not a per-operation one")
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
	var fileHeaders []ir.Property
	for _, pe := range enc {
		if pe.Filename {
			sawFile = true
			fileHeaders = pe.Headers
		}
		if pe.Multi {
			sawMulti = true
		}
	}
	assert.True(t, sawFile, "binary file part carries Filename")
	assert.True(t, sawMulti, "array part carries Multi")
	assertPartHeaders(t, doc, fileHeaders)
}

// assertPartHeaders pins encoding.<part>.headers, a TypeRef position reachable
// only through a PartEncoding and, before this case, exercised by no corpus
// spec. It covers both hops a header takes — an inline schema and one behind a
// $ref through components/headers — and requires each target to be in the
// registry, which is what gives the referential-closure walker something to
// check here.
func assertPartHeaders(t *testing.T, doc *ir.Document, headers []ir.Property) {
	t.Helper()
	require.Len(t, headers, 2, "the file part declares two per-part headers")
	byWire := make(map[string]ir.Property, len(headers))
	for _, h := range headers {
		byWire[h.WireName] = h
	}

	id, ok := byWire["X-Part-Id"]
	require.True(t, ok, "inline part header lowered; got %v", byWire)
	assert.Equal(t, ir.TypeID("t/prim/uuid"), id.Type.Target)
	assert.True(t, id.Required, "a required part header keeps its presence bit")
	assert.Equal(t, "per-part correlation id", id.Docs.Description)

	sum, ok := byWire["X-Part-Checksum"]
	require.True(t, ok, "$ref'd part header lowered; got %v", byWire)
	assert.Equal(t, namedID("Digest"), sum.Type.Target, "a $ref'd header takes the referent's schema")
	assert.Equal(t, "sha-256 digest of this part", sum.Docs.Description)
	for _, h := range headers {
		assert.Contains(t, doc.Types, h.Type.Target, "part header %q must reference a live node", h.WireName)
	}
}

// assertSequentialMedia pins the 3.2 sequential-media lowering, including the
// two forms per-item encoding takes: one Encoding governing every item lands in
// Content.ItemEncoding, while a positional prefixEncoding — which a single
// every-item encoding has no ordinals for — takes itself and the tail encoding
// beside it into Preserved instead.
func assertSequentialMedia(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	events, ok := opByName(doc, "streamEvents")
	require.True(t, ok)
	c := firstContent(t, events)
	require.NotNil(t, c.Item, "itemSchema becomes the element type")
	require.NotNil(t, c.ItemEncoding, "an encoding governing every item lowers structurally")
	assert.Equal(t, []string{"application/json"}, c.ItemEncoding.ContentTypes)
	assert.True(t, c.ItemEncoding.Multi, "the construct describes a repeated tail")
	assert.Empty(t, c.Preserved, "nothing is left over once it lowers")

	parts, ok := opByName(doc, "streamParts")
	require.True(t, ok)
	pc := firstContent(t, parts)
	assert.Nil(t, pc.ItemEncoding, "positional prefixes have no every-item form")
	for _, key := range []string{"openapi:prefixEncoding", "openapi:itemEncoding"} {
		entry, ok := pc.Preserved[key]
		require.True(t, ok, "%s kept verbatim; got %v", key, pc.Preserved)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	}
}

// firstContent returns an operation's single success-response content.
func firstContent(t *testing.T, op ir.Operation) ir.Content {
	t.Helper()
	require.Len(t, op.Responses, 1)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	return op.Responses[0].Payload.Contents[0]
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

	// A schema that reduced to a shared primitive keeps its examples on the
	// declaring property; one that hoists a node of its own keeps them there.
	n, ok := propByWire(m, "n")
	require.True(t, ok)
	assert.Len(t, n.Examples, 2)
	inner, ok := propByWire(m, "inner")
	require.True(t, ok)
	assert.Empty(t, inner.Examples, "an inline object holds its own examples")
	require.Len(t, doc.Types[inner.Type.Target].Common().Examples, 1)

	// A component's example belongs to the component, not to a use site.
	c, ok := doc.Types[namedID("C")]
	require.True(t, ok)
	require.Len(t, c.Common().Examples, 1)
	require.NotNil(t, c.Common().Examples[0].Value)
	assert.Equal(t, "component-level", c.Common().Examples[0].Value.Str)

	assertResponseExamples(t, doc)
}

// assertResponseExamples pins the response-side example sites: the plural map on
// a media type, in every spelling the spec allows, and a response header.
func assertResponseExamples(t *testing.T, doc *ir.Document) {
	t.Helper()
	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	ex := op.Responses[0].Payload.Contents[0].Examples
	require.Len(t, ex, 4, "every entry lowers, in source order")

	// An inline entry and one written as a $ref, which must resolve to the
	// referenced component's value. Both are named by their map key.
	require.NotNil(t, ex[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "hello"}, *ex[0].Value)
	assert.Equal(t, "inline", ex[0].Name)
	require.NotNil(t, ex[1].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "world"}, *ex[1].Value)
	assert.Equal(t, "shared", ex[1].Name)

	// The annotations that surround a value travel with it.
	assert.Equal(t, "a summary", ex[2].Summary)
	assert.Equal(t, "a description", ex[2].Description)

	// An externalValue entry carries no inline value, and is kept for the URL.
	assert.Equal(t, "https://example.com/e.json", ex[3].ExternalURL)
	assert.Nil(t, ex[3].Value)
	assert.Equal(t, "hosted elsewhere", ex[3].Summary)

	headers := op.Responses[0].Headers
	require.Len(t, headers, 1)
	require.Len(t, headers[0].Examples, 1)
	require.NotNil(t, headers[0].Examples[0].Value)
	assert.Equal(t, ir.BigVal("5"), headers[0].Examples[0].Value.Num)
}

func assertDocsSummaryDesc(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "ping")
	require.True(t, ok)
	assert.Equal(t, "Ping the server", op.Docs.Summary)
	assert.Equal(t, "Returns pong", op.Docs.Description)

	// A documented scalar alias is documented too: its docs must not depend on
	// what its body happens to lower to (GitHub #114).
	sc, ok := doc.Types[namedID("UserId")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, "User identifier", sc.Docs.Summary)
	assert.Equal(t, "Stable identifier for a user.", sc.Docs.Description)
}

func assertExtensionsX(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	raw, ok := m.Preserved["openapi:x-rate-limit"]
	require.True(t, ok, "x-* extensions are namespaced under openapi:")
	assert.JSONEq(t, "100", string(raw.Value))
	assert.Equal(t, ir.ReasonVendorExtension, raw.Reason)
}

// assertInlineAnnotations covers the positions with no ir.Property or
// ir.Parameter to carry what a declaration writes: each keeps its docs, bounds
// and x-* on a node of its own, while a position declaring nothing still
// resolves straight to the shared primitive.
func assertInlineAnnotations(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	elem := assertAnnotatedScalar(t, doc, "t/anon/components/schemas/Codes/items", 3)
	require.NotNil(t, elem.XML)
	assert.Equal(t, "Code", elem.XML.Name)
	assert.Contains(t, elem.Preserved, "openapi:x-facet")

	assertAnnotatedScalar(t, doc, "t/anon/components/schemas/CodeIndex/additionalProperties", 64)
	assertAnnotatedScalar(t, doc,
		"t/anon/paths/~1codes/get/responses/200/content/application~1json/schema", 8192)

	bare, ok := doc.Types[namedID("Bare")].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/string"), bare.Elem.Target,
		"an element declaring nothing must not grow a node of its own")

	op, ok := opByName(doc, "listCodes")
	require.True(t, ok)
	require.Len(t, op.Params, 1)
	assertCarriedAnnotations(t, op.Params[0].Docs, op.Params[0].Constraints, op.Params[0].Preserved, 4)
	require.Len(t, op.Responses[0].Headers, 1)
	h := op.Responses[0].Headers[0]
	assertCarriedAnnotations(t, h.Docs, h.Constraints, h.Preserved, 64)
}

// assertAnnotatedScalar requires the node at id to be a Scalar carrying a
// description and the given maxLength.
func assertAnnotatedScalar(t *testing.T, doc *ir.Document, id ir.TypeID, maxLength int64) *ir.Scalar {
	t.Helper()
	sc, ok := doc.Types[id].(*ir.Scalar)
	require.True(t, ok, "%s must own a Scalar; got %v", id, doc.Types[id])
	assert.NotEmpty(t, sc.Docs.Description)
	require.NotNil(t, sc.Constraints)
	require.NotNil(t, sc.Constraints.MaxLength)
	assert.Equal(t, maxLength, *sc.Constraints.MaxLength)
	return sc
}

// assertCarriedAnnotations requires a parameter's or header's own carrier to
// hold what its schema declared.
func assertCarriedAnnotations(t *testing.T, docs ir.Docs, c *ir.Constraints, p ir.Preserved, maxLength int64) {
	t.Helper()
	assert.NotEmpty(t, docs.Description)
	require.NotNil(t, c)
	require.NotNil(t, c.MaxLength)
	assert.Equal(t, maxLength, *c.MaxLength)
	assert.NotEmpty(t, p, "the schema's x-* rides on the carrier too")
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
