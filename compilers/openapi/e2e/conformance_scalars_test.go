// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// assertEnumString pins a string enum and its one-member form, which is an
// Enum as well: an `enum` keyword lowers to an Enum whatever its member count,
// and only `const` lowers to a Literal (docs/ir/enums.md).
func assertEnumString(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	e, ok := doc.Types[componentID("Color")].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.PrimString, e.ValueType)
	assert.True(t, e.Closed)
	require.Len(t, e.Members, 3)
	assert.Equal(t, "red", e.Members[0].Value.Str)
	assert.Equal(t, "blue", e.Members[2].Value.Str)

	pinned, ok := doc.Types[componentID("Pinned")].(*ir.Enum)
	require.True(t, ok, "a one-member enum is an Enum; got %T", doc.Types[componentID("Pinned")])
	assert.Equal(t, ir.PrimString, pinned.ValueType)
	assert.True(t, pinned.Closed)
	require.Len(t, pinned.Members, 1)
	assert.Equal(t, "only", pinned.Members[0].Value.Str)
}

func assertEnumNumeric(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	e, ok := doc.Types[componentID("BigCodes")].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.PrimInteger, e.ValueType)
	require.Len(t, e.Members, 2)
	assert.Equal(t, ir.ValueNumber, e.Members[1].Value.Kind)
	// BigVal member value preserves the full 64-bit integer.
	assert.Equal(t, ir.BigVal("9007199254740993"), e.Members[1].Value.Num)

	sized, ok := doc.Types[componentID("SizedCodes")].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.PrimInt64, sized.ValueType)
	assert.NotContains(t, sized.Unmodeled, "openapi:format", "ValueType holds the format")

	oversized, ok := doc.Types[componentID("OversizedCodes")].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.PrimInteger, oversized.ValueType, "no int8 holds 1000")
	assert.Contains(t, oversized.Unmodeled, "openapi:format")
}

// assertTypeArrayNumeric pins that a type array's union admits what the array
// does: one naming integer and number is inclusive, as every integer is a number.
func assertTypeArrayNumeric(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	for name, want := range map[string]bool{"Measure": false, "Sized": false, "Label": true} {
		u, ok := doc.Types[componentID(name)].(*ir.Union)
		require.True(t, ok, name)
		require.Len(t, u.Variants, 2, name)
		assert.Equal(t, want, u.Exclusive, name)
	}
}

// assertEmptyEnum covers `enum: []`: legal JSON Schema whose value space holds
// no member, so the position accepts no instance. The IR says that exactly,
// since a closed Enum admits its members and nothing else. Reading the keyword
// off `len(enum) > 0` cannot tell an empty list from an absent one, so the
// position would widen to what its siblings admit (GitHub #278). Holder's
// `colour` is in the corpus so that a change reaching every enum, not only the
// empty one, reddens this.
func assertEmptyEnum(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	empty := map[string]ir.PrimKind{
		"Nothing":          ir.PrimString,
		"Bare":             ir.PrimAny,
		"BesideProperties": ir.PrimAny,
		"BesideAllOf":      ir.PrimAny,
	}
	for name, valueType := range empty {
		e, ok := doc.Types[componentID(name)].(*ir.Enum)
		require.True(t, ok, "%s lowers to an Enum, got %T", name, doc.Types[componentID(name)])
		assert.True(t, e.Closed, "%s admits its members and nothing else", name)
		assert.Empty(t, e.Members, "%s declares no member", name)
		assert.Equal(t, valueType, e.ValueType,
			"%s takes the declared scalar type where one is written", name)
		assert.Equal(t, []ir.Severity{ir.SeverityWarning},
			diagsAt(diags, "openapi/empty-enum", "/components/schemas/"+name),
			"%s is reported once, since nobody writes an empty member list on purpose", name)
	}

	// An Enum has no home for a property set or a composition, so what the empty
	// enum is written beside stays verbatim rather than being traded for it.
	props := unmodeledEntry(t, doc.Types[componentID("BesideProperties")].Common().Unmodeled, "openapi:properties")
	assert.Equal(t, ir.ReasonDegradedLowering, props.Reason)
	assert.JSONEq(t, `{"x":{"type":"string"}}`, string(props.Value))
	composed := unmodeledEntry(t, doc.Types[componentID("BesideAllOf")].Common().Unmodeled, "openapi:allOf")
	assert.Equal(t, ir.ReasonDegradedLowering, composed.Reason)
	assert.JSONEq(t, `[{"$ref":"#/components/schemas/Base"}]`, string(composed.Value))

	holder, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	colour, ok := propByWire(holder, "colour")
	require.True(t, ok)
	populated, ok := doc.Types[colour.Type.Target].(*ir.Enum)
	require.True(t, ok, "a populated enum beside the degenerate ones is unaffected")
	require.Len(t, populated.Members, 2)
	assert.Equal(t, ir.PrimString, populated.ValueType)
	assert.Empty(t, diagsAt(diags, "openapi/empty-enum", "/components/schemas/Holder/properties/colour"),
		"and reports nothing")
}

// assertMissingArrayItems pins that a 3.0 schema writing type array and no
// items is reported once, at its own pointer, wherever it sits and whatever it
// lowers to: Choice is a union of literals (GitHub #650). Copy's $ref names
// Holder's property coordinate and is declared first, so the property is
// lowered twice. Holder's typed and empty arrays write items, so the list is
// every report the document draws.
func assertMissingArrayItems(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	var got []string
	for _, d := range diags {
		if d.Code == "openapi/missing-array-items" {
			assert.Equal(t, ir.SeverityWarning, d.Severity, "at %s", d.Provenance.Pointer)
			got = append(got, string(d.Provenance.Pointer))
		}
	}
	slices.Sort(got)
	assert.Equal(t, []string{
		"/components/schemas/Bare",
		"/components/schemas/Choice",
		"/components/schemas/Holder/properties/tags",
		"/paths/~1things/get/parameters/0/schema",
	}, got)

	bare, ok := doc.Types[componentID("Bare")].(*ir.List)
	require.True(t, ok, "Bare still lowers to a List, got %T", doc.Types[componentID("Bare")])
	assert.Equal(t, ir.TypeID("t/prim/any"), bare.Elem.Target, "over the top type")
}

func assertScalarFormat(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	h, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	color, ok := propByWire(h, "color")
	require.True(t, ok)
	sc, ok := doc.Types[color.Type.Target].(*ir.Scalar)
	require.True(t, ok, "unknown format hoists a named Scalar")
	require.NotNil(t, sc.Encoding)
	assert.Equal(t, "hex-color", sc.Encoding.Name)
	require.NotNil(t, sc.Base)
	assert.Equal(t, ir.TypeID("t/prim/string"), sc.Base.Target)
	assert.False(t, color.Secret, "an ordinary format asks for no redaction")
	// The bound survives the hoist the unknown format forced (GitHub #147).
	require.NotNil(t, sc.Constraints, "the hoisted format scalar carries its own bounds")
	require.NotNil(t, sc.Constraints.MinLength)
	assert.Equal(t, int64(4), *sc.Constraints.MinLength)

	// format: password asks for the value to be redacted and names no shape, so
	// a property whose schema owns no node holds it, and its type is the bare
	// string's (secret-format covers every other position).
	token, ok := propByWire(h, "token")
	require.True(t, ok)
	assert.True(t, token.Secret)
	assert.Equal(t, ir.PrimTypeID(ir.PrimString), token.Type.Target)

	assertSelectedPrimitives(t, doc, h)

	// A float format on an integer admits only integral values, which no
	// primitive narrower than integer holds, so it hoists over integer.
	whole, ok := propByWire(h, "whole")
	require.True(t, ok)
	ws, ok := doc.Types[whole.Type.Target].(*ir.Scalar)
	require.True(t, ok, "a pairing no primitive holds hoists a Scalar")
	require.NotNil(t, ws.Base)
	assert.Equal(t, ir.PrimTypeID(ir.PrimInteger), ws.Base.Target)
	require.NotNil(t, ws.Encoding)
	assert.Equal(t, "double", ws.Encoding.Name)

	// time is an RFC 3339 full-time, whose offset the time primitive does not
	// hold, so it keeps its format on a Scalar over string.
	opens, ok := propByWire(h, "opens")
	require.True(t, ok)
	ts, ok := doc.Types[opens.Type.Target].(*ir.Scalar)
	require.True(t, ok, "time selects no primitive, so it hoists a Scalar")
	require.NotNil(t, ts.Base)
	assert.Equal(t, ir.PrimTypeID(ir.PrimString), ts.Base.Target)
	require.NotNil(t, ts.Encoding)
	assert.Equal(t, "time", ts.Encoding.Name)
}

// assertSelectedPrimitives pins the table's pairings this spec writes: each
// selects a shared primitive, an integer width whichever numeric type carries
// it, and so holds no node of its own. The primitive node has to be interned
// too, or the reference would dangle.
func assertSelectedPrimitives(t *testing.T, doc *ir.Document, h *ir.Model) {
	t.Helper()
	for wire, want := range map[string]ir.PrimKind{
		"small":  ir.PrimInt8,
		"count":  ir.PrimUint32,
		"i16":    ir.PrimInt16,
		"i32":    ir.PrimInt32,
		"i64":    ir.PrimInt64,
		"u8":     ir.PrimUint8,
		"u16":    ir.PrimUint16,
		"u64":    ir.PrimUint64,
		"f32":    ir.PrimFloat32,
		"f64":    ir.PrimFloat64,
		"amount": ir.PrimDecimal,
		"big":    ir.PrimDecimal128,
		"at":     ir.PrimDatetimeOffset,
		"starts": ir.PrimDatetime,
		"closes": ir.PrimTime,
		"ttl":    ir.PrimDuration,
	} {
		t.Run(wire, func(t *testing.T) {
			p, ok := propByWire(h, wire)
			require.True(t, ok, "the property is declared")
			assert.Equal(t, ir.PrimTypeID(want), p.Type.Target, "it selects the shared primitive")
			prim, ok := doc.Types[p.Type.Target].(*ir.Primitive)
			require.True(t, ok, "the primitive is interned")
			assert.Equal(t, want, prim.Prim, "the node is the primitive it selects")
		})
	}
}

// assertSecretFormat pins where `format: password` lands at every position the
// compiler reaches: Sensitive on the node a position owns, whatever its kind;
// Property.Secret at a property or header owning none; and verbatim on a
// parameter, which has no field. It reads the nodes rather than the golden's
// bytes, so a fact that moved to another carrier still fails (GitHub #579).
func assertSecretFormat(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	pw := requireSensitiveAlias(t, doc, componentID("Pw"), ir.PrimTypeID(ir.PrimString), "component schema")
	require.NotNil(t, pw.Constraints, "the bound written beside the format stays on the node")
	assert.Equal(t, new(int64(8)), pw.Constraints.MinLength)

	assertSecretCarriers(t, doc)
	assertSecretNodes(t, doc)
	assertSecretOperation(t, doc, diags)

	// The guard: a bare string keeps the shared primitive, and a shared node
	// never carries one declaration's annotations (GitHub #116).
	plain, ok := propByWire(secretModel(t, doc, "Login"), "plain")
	require.True(t, ok)
	assert.Equal(t, ir.PrimTypeID(ir.PrimString), plain.Type.Target)
	assert.False(t, plain.Secret)
	assert.False(t, doc.Types[ir.PrimTypeID(ir.PrimString)].Common().Sensitive)
	assert.False(t, doc.Types[componentID("Plain")].Common().Sensitive, "a referent beside which a use wrote it")
}

// assertSecretCarriers covers the properties of Login that own no node, where
// Property.Secret is the home, and the wrappers whose type reaches the
// Sensitive node instead.
func assertSecretCarriers(t *testing.T, doc *ir.Document) {
	t.Helper()
	login := secretModel(t, doc, "Login")
	for _, tc := range []struct {
		wire   string
		target ir.TypeID
	}{
		{"inline", ir.PrimTypeID(ir.PrimString)},
		{"pin", ir.PrimTypeID(ir.PrimInteger)},
		{"viaRef", componentID("Pw")},      // inherited from the referent, as deprecated is
		{"viaAlias", componentID("Chain")}, // from the end of the $ref chain
		{"beside", componentID("Plain")},   // written beside the $ref, binding the use
	} {
		p, ok := propByWire(login, tc.wire)
		require.True(t, ok, tc.wire)
		assert.True(t, p.Secret, "%s: the property is the carrier", tc.wire)
		assert.Equal(t, tc.target, p.Type.Target, "%s: the redaction names no shape", tc.wire)
	}
	inline, ok := propByWire(login, "inline")
	require.True(t, ok)
	assert.Equal(t, "shown on the property", inline.Docs.Description, "no node takes the docs away")

	// A wrapper's own schema writes no redaction; its type reaches the node
	// that does, along a nullable reference or a Base.
	nullable, ok := propByWire(login, "nullable")
	require.True(t, ok)
	assert.False(t, nullable.Secret, "the node holds it, not a copy on the use")
	assert.Equal(t, ir.TypeRef{Target: componentID("Pw"), Nullable: true}, nullable.Type)
	wrapped, ok := propByWire(login, "wrapped")
	require.True(t, ok)
	assert.False(t, wrapped.Secret, "the node holds it, not a copy on the use")
	m, ok := doc.Types[wrapped.Type.Target].(*ir.Model)
	require.True(t, ok, "an allOf over one $ref is a model")
	require.NotNil(t, m.Base)
	assert.Equal(t, componentID("Pw"), m.Base.Target)
	chain, ok := doc.Types[componentID("Chain")].(*ir.Scalar)
	require.True(t, ok, "an alias component is a Scalar")
	require.NotNil(t, chain.Base)
	assert.Equal(t, componentID("Pw"), chain.Base.Target)

	// A property whose schema owns a node leaves the redaction on it.
	pick, ok := propByWire(login, "pick")
	require.True(t, ok)
	assert.False(t, pick.Secret, "one home per declaration")
	assert.Equal(t, ir.KindEnum, requireSensitive(t, doc, pick.Type.Target, "enum property").Kind())
}

// assertSecretNodes covers the positions with no carrier: each owns a node, an
// alias over the bare type where the shape alone would own none.
func assertSecretNodes(t *testing.T, doc *ir.Document) {
	t.Helper()
	arr, ok := propByWire(secretModel(t, doc, "Login"), "arr")
	require.True(t, ok)
	list, ok := doc.Types[arr.Type.Target].(*ir.List)
	require.True(t, ok)
	items := requireSensitiveAlias(t, doc, list.Elem.Target, ir.PrimTypeID(ir.PrimString), "array items")
	require.NotNil(t, items.Constraints)
	assert.Equal(t, new(int64(64)), items.Constraints.MaxLength)

	bag := secretModel(t, doc, "Bag")
	require.NotNil(t, bag.AdditionalProps)
	requireSensitiveAlias(t, doc, bag.AdditionalProps.Value.Target, ir.PrimTypeID(ir.PrimString), "additionalProperties")
	require.Len(t, bag.AdditionalProps.Patterns, 1)
	requireSensitiveAlias(t, doc, bag.AdditionalProps.Patterns[0].Value.Target, ir.PrimTypeID(ir.PrimString),
		"patternProperties")

	pair, ok := doc.Types[componentID("Pair")].(*ir.Tuple)
	require.True(t, ok)
	require.Len(t, pair.Elems, 1)
	requireSensitiveAlias(t, doc, pair.Elems[0].Target, ir.PrimTypeID(ir.PrimString), "prefixItems")

	tokens, ok := doc.Types[componentID("Tokens")].(*ir.List)
	require.True(t, ok)
	requireSensitiveAlias(t, doc, tokens.Elem.Target, componentID("Plain"), "items beside a $ref")

	either, ok := doc.Types[componentID("Either")].(*ir.Union)
	require.True(t, ok)
	require.Len(t, either.Variants, 2)
	requireSensitiveAlias(t, doc, either.Variants[0].Type.Target, ir.PrimTypeID(ir.PrimString), "union branch")
	assert.False(t, either.Sensitive, "the branch is redacted, not the union")

	assert.Equal(t, ir.KindLiteral, requireSensitive(t, doc, componentID("Code"), "const").Kind())
	requireSensitiveAlias(t, doc, componentID("Opaque"), ir.PrimTypeID(ir.PrimAny), "untyped")
	encoded, ok := requireSensitive(t, doc, componentID("Encoded"), "beside contentEncoding").(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, encoded.Encoding)
	assert.Equal(t, "base64", encoded.Encoding.Name, "contentEncoding alone names the encoding")
}

// assertSecretOperation covers the parameters, bodies and headers.
func assertSecretOperation(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	op, ok := opByName(doc, "login")
	require.True(t, ok)

	token, ok := openapitest.ParamBySource(op, "X-Token")
	require.True(t, ok)
	assert.Equal(t, ir.PrimTypeID(ir.PrimString), token.Type.Target, "a parameter owns no node for it")
	assert.Equal(t, "the session token", token.Docs.Description)
	require.Contains(t, token.Unmodeled, "openapi:format", "ir.Parameter has no field (GitHub #578)")
	assert.Equal(t, ir.ReasonNoIRHome, token.Unmodeled["openapi:format"].Reason)
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/degraded-construct", "/paths/~1login/post/parameters/0/schema/format"))
	key, ok := openapitest.ParamBySource(op, "api_key")
	require.True(t, ok)
	assert.Equal(t, componentID("Pw"), key.Type.Target, "a referent's redaction is its own node's")
	assert.NotContains(t, key.Unmodeled, "openapi:format")

	str := ir.PrimTypeID(ir.PrimString)
	requireSensitiveAlias(t, doc, openapitest.BodyTarget(t, op.Request), str, "request content schema")
	require.Len(t, op.Responses, 1)
	requireSensitiveAlias(t, doc, openapitest.BodyTarget(t, op.Responses[0].Payload), str, "response content schema")

	require.Len(t, op.Responses[0].Headers, 2)
	header := op.Responses[0].Headers[0]
	assert.True(t, header.Secret, "a header is a property carrier")
	assert.Equal(t, ir.PrimTypeID(ir.PrimString), header.Type.Target)
	assert.Equal(t, ir.TypeRef{Target: componentID("Pw"), Nullable: true}, op.Responses[0].Headers[1].Type)
}

// secretModel returns the secret-format component name as a Model.
func secretModel(t *testing.T, doc *ir.Document, name string) *ir.Model {
	t.Helper()
	m, ok := doc.Types[componentID(name)].(*ir.Model)
	require.True(t, ok, "%s is a model", name)
	return m
}

// requireSensitive requires the node at id to exist and be Sensitive.
func requireSensitive(t *testing.T, doc *ir.Document, id ir.TypeID, position string) ir.TypeDef {
	t.Helper()
	node, ok := doc.Types[id]
	require.True(t, ok, "%s: nothing is interned at %s", position, id)
	assert.True(t, node.Common().Sensitive, "%s: whole-type redaction", position)
	return node
}

// requireSensitiveAlias requires the node at id to be a Sensitive alias: the
// Scalar over base a position owns where its shape alone is that shared base,
// with no encoding, since a redaction is none.
func requireSensitiveAlias(t *testing.T, doc *ir.Document, id, base ir.TypeID, position string) *ir.Scalar {
	t.Helper()
	sc, ok := requireSensitive(t, doc, id, position).(*ir.Scalar)
	require.True(t, ok, "%s: a Scalar", position)
	require.NotNil(t, sc.Base, "%s: a base", position)
	assert.Equal(t, base, sc.Base.Target, "%s: over its shape", position)
	assert.Nil(t, sc.Encoding, "%s: the redaction is no encoding", position)
	return sc
}

// assertXMLHints covers the XML wire shape, whose hints attach at two carriers
// with different scopes: a type node holds the root element shape, a property
// holds a per-use override. A position that owns a node keeps them there, so
// which carrier a hint lands on follows what the position lowered to.
func assertXMLHints(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	book, ok := doc.Types[componentID("Book")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, book.XML)
	assert.Equal(t, ir.XMLHints{Name: "book", Namespace: "urn:example:books", Prefix: "bk"}, *book.XML)

	isbn, ok := propByWire(book, "isbn")
	require.True(t, ok)
	require.NotNil(t, isbn.XML, "a scalar property has no node of its own, so the property carries them")
	assert.Equal(t, ir.XMLHints{Namespace: "urn:example:books", Prefix: "bk", NodeType: "attribute"},
		*isbn.XML, "attribute: true is the node type, not a flag of its own")

	authors, ok := propByWire(book, "authors")
	require.True(t, ok)
	assert.Nil(t, authors.XML, "an array owns a node, which is where its hints go")
	list, ok := doc.Types[authors.Type.Target].(*ir.List)
	require.True(t, ok)
	require.NotNil(t, list.XML)
	assert.True(t, list.XML.Wrapped, "wrapping is a property of the collection")
	elem, ok := doc.Types[list.Elem.Target]
	require.True(t, ok)
	require.NotNil(t, elem.Common().XML)
	assert.Equal(t, "author", elem.Common().XML.Name)
}

func assertEncodingByte(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	h, ok := doc.Types[componentID("Holder")].(*ir.Model)
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

	// A scalar that hoists a node because of its format must keep the bounds it
	// wrote beside it: owning the pointer is what stops the shared alias path
	// attaching them afterwards (GitHub #147).
	require.NotNil(t, sc.Constraints, "the hoisted byte scalar carries its own bounds")
	require.NotNil(t, sc.Constraints.MinLength)
	assert.Equal(t, int64(5), *sc.Constraints.MinLength)
	require.NotNil(t, sc.Constraints.MaxLength)
	assert.Equal(t, int64(9), *sc.Constraints.MaxLength)
}

// assertXMLNodeType32 pins GitHub #615's xml.nodeType: ir.XMLHints.NodeType
// already existed and its GoDoc already named the version, so the fix is the
// wiring, and the dialect's key is no longer reported as undefined.
func assertXMLNodeType32(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	report, ok := doc.Types[componentID("Report")].(*ir.Model)
	require.True(t, ok)
	id, ok := propByWire(report, "id")
	require.True(t, ok)
	require.NotNil(t, id.XML)
	assert.Equal(t, "attribute", id.XML.NodeType)

	body, ok := propByWire(report, "body")
	require.True(t, ok)
	require.NotNil(t, body.XML)
	assert.Equal(t, "text", body.XML.NodeType)
	assert.Equal(t, "B", body.XML.Name, "the object's other fields still lower as they did")
	assert.Equal(t, "p", body.XML.Prefix)

	assert.Empty(t, diagsAt(diags, diag.UnknownObjectKey, "/components/schemas/Report/properties/id/xml/nodeType"),
		"a key 3.2 defines is not an undefined key")
}
