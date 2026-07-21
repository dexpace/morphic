package protobuf_test // external test package — exercises only the public API

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/protobuf"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// conformanceDir is the corpus of one minimal .proto per capability row of
// ir-spec-matrix.md that protobuf/gRPC can express, addressed relative to this
// test file.
const conformanceDir = "../../testdata/conformance/protobuf"

// TestConformance drives one minimal spec per protobuf/gRPC-expressible
// capability through the full compiler and asserts lossless capture: a focused
// capability-specific assertion plus a byte-exact golden IR snapshot. Regenerate
// the goldens with `go test ./compilers/protobuf -run TestConformance -update`.
func TestConformance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file   string
		assert func(*testing.T, *ir.Document, []ir.Diagnostic)
	}{
		{"messages", assertMessages},
		{"nested", assertNested},
		{"enum-open", assertEnumOpen},
		{"enum-closed", assertEnumClosed},
		{"oneof", assertOneof},
		{"proto3-optional", assertProto3Optional},
		{"map", assertMap},
		{"repeated", assertRepeated},
		{"scalar-encoding", assertScalarEncoding},
		{"presence", assertPresence},
		{"defaults", assertDefaults},
		{"extensions", assertExtensions},
		{"reserved", assertReserved},
		{"package", assertPackage},
		{"well-known", assertWellKnown},
		{"services", assertServices},
		{"deprecation", assertDeprecation},
		{"comments", assertComments},
		{"custom-options", assertCustomOptions},
		{"rich-options", assertRichOptions},
		{"file-options", assertFileOptions},
		{"no-package", assertNoPackage},
		{"editions", assertEditions},
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

// parseCorpus reads and compiles one corpus spec through the full compiler.
func parseCorpus(t *testing.T, name string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conformanceDir, name+".proto"))
	require.NoError(t, err)
	doc, diags, err := protobuf.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".proto", Data: data}}, compilers.Options{})
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

// typeID is the stable TypeID of a message or enum by fully-qualified name.
func typeID(fullName string) ir.TypeID { return ir.TypeID("t/protobuf/" + fullName) }

// modelOf resolves a Model by fully-qualified name.
func modelOf(t *testing.T, doc *ir.Document, fullName string) *ir.Model {
	t.Helper()
	m, ok := doc.Types[typeID(fullName)].(*ir.Model)
	require.True(t, ok, "message %s present as a Model", fullName)
	return m
}

// enumOf resolves an Enum by fully-qualified name.
func enumOf(t *testing.T, doc *ir.Document, fullName string) *ir.Enum {
	t.Helper()
	e, ok := doc.Types[typeID(fullName)].(*ir.Enum)
	require.True(t, ok, "enum %s present as an Enum", fullName)
	return e
}

// propByName returns the property of m whose source name matches.
func propByName(t *testing.T, m *ir.Model, name string) ir.Property {
	t.Helper()
	for _, p := range m.Properties {
		if p.Name.Source == name {
			return p
		}
	}
	require.Failf(t, "property not found", "model %s has no property %q", m.Name.Source, name)
	return ir.Property{}
}

// allOperations flattens every operation across a document's service groups.
func allOperations(doc *ir.Document) []ir.Operation {
	var out []ir.Operation
	for _, svc := range doc.Services {
		for _, g := range svc.Groups {
			out = append(out, g.Operations...)
		}
	}
	return out
}

// opByName finds an operation by its source rpc name.
func opByName(t *testing.T, doc *ir.Document, name string) ir.Operation {
	t.Helper()
	for _, op := range allOperations(doc) {
		if op.Name.Source == name {
			return op
		}
	}
	require.Failf(t, "operation not found", "no rpc named %q", name)
	return ir.Operation{}
}

func assertMessages(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	product := modelOf(t, doc, "shop.Product")
	assert.False(t, product.Anonymous)
	assert.Equal(t, []string{"shop"}, product.Namespace)
	id := propByName(t, product, "id")
	require.NotNil(t, id.WireID)
	assert.Equal(t, 1, *id.WireID, "field number becomes the wire ID")
	assert.Equal(t, ir.PresenceImplicit, id.Presence, "proto3 no-label field is implicit-presence")
	assert.Equal(t, ir.TypeID("t/prim/int64"), id.Type.Target)
	category := propByName(t, product, "category")
	assert.Equal(t, typeID("shop.Category"), category.Type.Target)
	assert.Equal(t, ir.PresenceExplicit, category.Presence, "message fields always have presence")
	_ = modelOf(t, doc, "shop.Category")
}

func assertNested(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	outer := modelOf(t, doc, "nested.Outer")
	inner := modelOf(t, doc, "nested.Outer.Inner")
	assert.Equal(t, []string{"nested"}, inner.Namespace, "nested type keeps the file package as namespace")
	assert.Equal(t, typeID("nested.Outer.Inner"), propByName(t, outer, "inner").Type.Target)
	kind := enumOf(t, doc, "nested.Outer.Kind")
	assert.Equal(t, typeID("nested.Outer.Kind"), propByName(t, outer, "kind").Type.Target)
	assert.Len(t, kind.Members, 2)
}

func assertEnumOpen(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	color := enumOf(t, doc, "enums.Color")
	assert.False(t, color.Closed, "proto3 enums are open")
	assert.Equal(t, ir.PrimInt32, color.ValueType)
	require.Len(t, color.Members, 4)
	assert.Equal(t, "COLOR_UNSPECIFIED", color.Members[0].Name.Source)
	assert.Equal(t, ir.BigVal("2"), color.Members[2].Value.Num)
}

func assertEnumClosed(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	status := enumOf(t, doc, "enums2.Status")
	assert.True(t, status.Closed, "proto2 enums are closed")
	require.Len(t, status.Members, 4)
	// allow_alias: STARTED and RUNNING share value 1, kept as distinct members.
	assert.Equal(t, ir.BigVal("1"), status.Members[1].Value.Num)
	assert.Equal(t, ir.BigVal("1"), status.Members[2].Value.Num)
	assert.Equal(t, "RUNNING", status.Members[2].Name.Source)
}

func assertOneof(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	shape := modelOf(t, doc, "oneofs.Shape")
	form := propByName(t, shape, "form")
	assert.True(t, form.Flatten, "the oneof wrapper hoists its members to top-level wire fields")
	u, ok := doc.Types[form.Type.Target].(*ir.Union)
	require.True(t, ok, "oneof lowers to a Union node")
	assert.True(t, u.Exclusive)
	assert.True(t, u.WireTagged, "protobuf oneof is tagged on the wire")
	require.Len(t, u.Variants, 4)
	require.NotNil(t, u.Variants[0].WireID)
	assert.Equal(t, 1, *u.Variants[0].WireID, "variant keeps its field number")
	legacy := u.Variants[3]
	assert.NotNil(t, legacy.Deprecation, "a deprecated oneof member keeps its deprecation")
	assert.Equal(t, "legacy", legacy.WireName, "an explicit json_name becomes the variant wire name")
	// The non-oneof field remains an ordinary property alongside the wrapper.
	_ = propByName(t, shape, "name")
}

func assertProto3Optional(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	config := modelOf(t, doc, "opt3.Config")
	assert.Equal(t, ir.PresenceExplicit, propByName(t, config, "name").Presence,
		"proto3 optional distinguishes unset")
	assert.Equal(t, ir.PresenceImplicit, propByName(t, config, "count").Presence)
	for id, td := range doc.Types {
		assert.NotEqual(t, ir.KindUnion, td.Kind(),
			"a synthetic proto3-optional oneof must not lower to a Union (%s)", id)
	}
}

func assertMap(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	dict := modelOf(t, doc, "maps.Dict")
	counts, ok := doc.Types[propByName(t, dict, "counts").Type.Target].(*ir.MapT)
	require.True(t, ok, "map field lowers to a MapT")
	assert.Equal(t, ir.TypeID("t/prim/string"), counts.Key.Target)
	assert.Equal(t, ir.TypeID("t/prim/int32"), counts.Value.Target)
	names, ok := doc.Types[propByName(t, dict, "names").Type.Target].(*ir.MapT)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/int64"), names.Key.Target)
	// The synthetic map-entry message is never hoisted as a model.
	for id := range doc.Types {
		assert.NotContains(t, string(id), "Entry", "map-entry message must not be hoisted")
	}
}

func assertRepeated(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	series := modelOf(t, doc, "rep.Series")
	packed := listOf(t, doc, propByName(t, series, "packed_values"))
	require.NotNil(t, packed.Encoding)
	assert.Equal(t, "packed", packed.Encoding.Name, "proto3 repeated scalars pack by default")
	expanded := listOf(t, doc, propByName(t, series, "expanded_values"))
	require.NotNil(t, expanded.Encoding)
	assert.Equal(t, "expanded", expanded.Encoding.Name, "[packed=false] lowers to expanded")
	labels := listOf(t, doc, propByName(t, series, "labels"))
	assert.Nil(t, labels.Encoding, "string lists carry no packing encoding")
	// A repeated encoded scalar wraps its element in a Scalar so the zigzag
	// encoding survives where a bare element ref has no encoding slot.
	deltas := listOf(t, doc, propByName(t, series, "deltas"))
	elem, ok := doc.Types[deltas.Elem.Target].(*ir.Scalar)
	require.True(t, ok, "sint64 list element hoists a Scalar")
	require.NotNil(t, elem.Encoding)
	assert.Equal(t, "zigzag", elem.Encoding.Name)
}

// listOf resolves the List node a property references.
func listOf(t *testing.T, doc *ir.Document, p ir.Property) *ir.List {
	t.Helper()
	l, ok := doc.Types[p.Type.Target].(*ir.List)
	require.True(t, ok, "property %s references a List", p.Name.Source)
	return l
}

func assertScalarEncoding(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	nums := modelOf(t, doc, "enc.Nums")
	a := propByName(t, nums, "a")
	assert.Equal(t, ir.TypeID("t/prim/int32"), a.Type.Target)
	require.NotNil(t, a.Encoding)
	assert.Equal(t, "zigzag", a.Encoding.Name, "sint32 is a zigzag encoding of int32")
	c := propByName(t, nums, "c")
	assert.Equal(t, ir.TypeID("t/prim/uint32"), c.Type.Target)
	require.NotNil(t, c.Encoding)
	assert.Equal(t, "fixed", c.Encoding.Name, "fixed32 is a fixed encoding of uint32")
	d := propByName(t, nums, "d")
	assert.Equal(t, ir.TypeID("t/prim/int64"), d.Type.Target)
	require.NotNil(t, d.Encoding)
	assert.Equal(t, "fixed", d.Encoding.Name)
}

func assertPresence(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	rec := modelOf(t, doc, "pres.Rec")
	id := propByName(t, rec, "id")
	assert.True(t, id.Required, "proto2 required maps to Required")
	assert.Equal(t, ir.PresenceRequired, id.Presence)
	assert.Equal(t, ir.PresenceExplicit, propByName(t, rec, "note").Presence, "proto2 optional is explicit")
	tags := propByName(t, rec, "tags")
	assert.False(t, tags.Required)
	assert.Equal(t, ir.PresenceDefault, tags.Presence, "repeated fields carry no presence discipline")
}

func assertDefaults(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	s := modelOf(t, doc, "def.Settings")
	assert.Equal(t, ir.BigVal("3"), propByName(t, s, "retries").Default.Num)
	assert.Equal(t, "hello", propByName(t, s, "label").Default.Str)
	assert.True(t, propByName(t, s, "enabled").Default.Bool)
	assert.Equal(t, ir.BigVal("2.5"), propByName(t, s, "ratio").Default.Num,
		"floating default is an exact decimal string, never float64")
	mode := propByName(t, s, "mode").Default
	require.Equal(t, ir.ValueRefKind, mode.Kind, "enum default references a member")
	require.NotNil(t, mode.Ref)
	assert.Equal(t, typeID("def.Mode"), mode.Ref.Type)
	assert.Equal(t, "SLOW", mode.Ref.Member)
	assert.Equal(t, ir.BigVal("42"), propByName(t, s, "limit").Default.Num, "unsigned default")
	salt := propByName(t, s, "salt").Default
	require.Equal(t, ir.ValueBytes, salt.Kind, "bytes default")
	assert.Equal(t, []byte{1, 2}, salt.Bytes)
	assert.Nil(t, propByName(t, s, "unbounded").Default, "a non-finite default is dropped")
}

func assertExtensions(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	base := modelOf(t, doc, "ext.Base")
	require.Len(t, base.ExtensionRanges, 1)
	assert.Equal(t, ir.WireIDRange{From: 100, To: 200}, base.ExtensionRanges[0])
	priority := propByName(t, base, "priority")
	require.NotNil(t, priority.WireID)
	assert.Equal(t, 100, *priority.WireID)
	assert.Equal(t, "ext", priority.ExtensionOf, "extend field records its declaring scope")
	tag := propByName(t, base, "tag")
	assert.Equal(t, "ext", tag.ExtensionOf)
}

func assertReserved(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	m := modelOf(t, doc, "resv.M")
	raw, ok := m.Extensions["protobuf:reserved"]
	require.True(t, ok, "reserved numbers/names preserved in Extensions")
	assert.JSONEq(t, `{"ranges":[{"from":2,"to":2},{"from":15,"to":15},{"from":9,"to":11}],"names":["foo","bar"]}`,
		string(raw))
	e := enumOf(t, doc, "resv.E")
	rawE, ok := e.Extensions["protobuf:reserved"]
	require.True(t, ok)
	assert.JSONEq(t, `{"ranges":[{"from":2,"to":2},{"from":5,"to":7}],"names":["OLD"]}`, string(rawE))
	var found bool
	for _, d := range diags {
		if d.Code == "protobuf/reserved" {
			found = true
			assert.Equal(t, ir.SeverityInfo, d.Severity)
		}
	}
	assert.True(t, found, "reserved constructs raise an info diagnostic")
}

func assertPackage(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	thing := modelOf(t, doc, "deep.nested.pkg.Thing")
	assert.Equal(t, []string{"deep", "nested", "pkg"}, thing.Namespace)
	assert.Equal(t, "deep.nested.pkg", doc.Name)
	require.Len(t, doc.Services, 1)
	assert.Equal(t, []string{"deep", "nested", "pkg"}, doc.Services[0].Namespace)
}

func assertWellKnown(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	b := modelOf(t, doc, "wkt.Bundle")
	assert.Equal(t, ir.TypeID("t/prim/datetime"), propByName(t, b, "ts").Type.Target)
	assert.Equal(t, ir.TypeID("t/prim/duration"), propByName(t, b, "dur").Type.Target)
	assert.Equal(t, ir.TypeID("t/protobuf/any"), propByName(t, b, "any").Type.Target)
	assert.Equal(t, ir.TypeID("t/protobuf/any"), propByName(t, b, "props").Type.Target,
		"Struct lowers to the schemaless Any node")
	mask := propByName(t, b, "mask")
	ext, ok := doc.Types[mask.Type.Target].(*ir.External)
	require.True(t, ok, "FieldMask lowers to an External")
	assert.Equal(t, "google.protobuf.FieldMask", ext.Identity)
	count := propByName(t, b, "count")
	assert.Equal(t, ir.TypeID("t/prim/int32"), count.Type.Target)
	assert.True(t, count.Type.Nullable, "Int32Value lowers to a nullable primitive")
	note := propByName(t, b, "note")
	assert.Equal(t, ir.TypeID("t/prim/string"), note.Type.Target)
	assert.True(t, note.Type.Nullable)
	_, ok = doc.Types[propByName(t, b, "nothing").Type.Target].(*ir.External)
	assert.True(t, ok, "Empty as a field type lowers to an External")
	ctx, ok := doc.Types[propByName(t, b, "ctx").Type.Target].(*ir.External)
	require.True(t, ok, "an unmapped well-known type falls back to External")
	assert.Equal(t, "google.protobuf.SourceContext", ctx.Identity)
}

func assertServices(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	assert.Equal(t, "Echo", doc.Services[0].Groups[0].Name.Source)

	unary := opByName(t, doc, "Unary")
	assert.Equal(t, ir.StreamingMode(""), unary.Streaming, "unary rpc has no streaming mode")
	require.NotNil(t, unary.Bindings.RPC)
	assert.Equal(t, "grpc", unary.Bindings.RPC.System)
	assert.Equal(t, "/svc.Echo/Unary", unary.Bindings.RPC.FullMethod)
	require.NotNil(t, unary.Bindings.RPC.InputType)

	client := opByName(t, doc, "ClientStream")
	assert.Equal(t, ir.StreamingClient, client.Streaming)
	assert.NotNil(t, client.RequestStream)
	assert.Nil(t, client.ResponseStream)

	server := opByName(t, doc, "ServerStream")
	assert.Equal(t, ir.StreamingServer, server.Streaming)
	assert.NotNil(t, server.ResponseStream)

	bidi := opByName(t, doc, "BidiStream")
	assert.Equal(t, ir.StreamingBidi, bidi.Streaming)
	assert.NotNil(t, bidi.RequestStream)
	assert.NotNil(t, bidi.ResponseStream)

	fetch := opByName(t, doc, "Fetch")
	assert.Equal(t, ir.IdempotencySafe, fetch.Idempotency.Kind)
	assert.Equal(t, "NO_SIDE_EFFECTS", fetch.Bindings.RPC.IdempotencyLevel)
	assert.Equal(t, ir.IdempotencyIdempotent, opByName(t, doc, "Replace").Idempotency.Kind)

	notify := opByName(t, doc, "Notify")
	require.Len(t, notify.Responses, 1)
	assert.Nil(t, notify.Responses[0].Payload, "an Empty response carries no payload")
	assert.NotNil(t, notify.Request, "Notify still has a request payload")

	drain := opByName(t, doc, "Drain")
	assert.Nil(t, drain.Request, "an Empty request lowers to no payload")
	assert.Nil(t, drain.Bindings.RPC.InputType, "and no RPC input type")

	touch := opByName(t, doc, "Touch")
	assert.NotNil(t, touch.Deprecation, "a deprecated rpc keeps its deprecation")
	assert.Equal(t, ir.IdempotencyUnknown, touch.Idempotency.Kind, "no idempotency level declared")
}

func assertDeprecation(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	old := modelOf(t, doc, "dep.Old")
	assert.NotNil(t, old.Deprecation, "deprecated message")
	assert.NotNil(t, propByName(t, old, "id").Deprecation, "deprecated field")
	legacy := enumOf(t, doc, "dep.Legacy")
	require.Len(t, legacy.Members, 2)
	assert.NotNil(t, legacy.Members[1].Deprecation, "deprecated enum value")
}

func assertComments(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	d := modelOf(t, doc, "docs.Doc")
	assert.Equal(t, "A documented message.", d.Docs.Description)
	assert.Equal(t, "The identifier.", propByName(t, d, "id").Docs.Description)
}

func assertCustomOptions(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	account := modelOf(t, doc, "copt.Account")
	audited, ok := account.Extensions["protobuf:option:copt.audited"]
	require.True(t, ok, "message custom option preserved in Extensions")
	assert.JSONEq(t, "true", string(audited))
	ssn := propByName(t, account, "ssn")
	sensitivity, ok := ssn.Extensions["protobuf:option:copt.sensitivity"]
	require.True(t, ok, "field custom option preserved in Extensions")
	assert.JSONEq(t, `"high"`, string(sensitivity))
	_, ok = doc.Extensions["protobuf:custom-option:copt.sensitivity"]
	assert.True(t, ok, "custom-option definition preserved at document level")
	var found bool
	for _, d := range diags {
		if d.Code == "protobuf/custom-option-definition" {
			found = true
		}
	}
	assert.True(t, found)
}

func assertRichOptions(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	widget := modelOf(t, doc, "ropt.Widget")
	meta, ok := widget.Extensions["protobuf:option:ropt.meta"]
	require.True(t, ok, "a message-valued custom option renders as a nested object")
	// score (a NaN scalar) is dropped; points and ratios keep their NaN elements
	// dropped, leaving an empty array and object.
	assert.JSONEq(t,
		`{"owner":"team","labels":["a","b"],"weights":{"a":2,"x":1},"nested":{"flag":true},`+
			`"kind":"K_B","sig":"QUE9PQ==","big":42,"points":[],"ratios":{}}`,
		string(meta))
	reviewers, ok := widget.Extensions["protobuf:option:ropt.reviewers"]
	require.True(t, ok, "a repeated custom option renders as an array")
	assert.JSONEq(t, `["alice","bob"]`, string(reviewers))
	id := propByName(t, widget, "id")
	assert.JSONEq(t, `"K_A"`, string(id.Extensions["protobuf:option:ropt.field_kind"]))
	// A group field lowers to a delimited-encoded message property.
	detail := propByName(t, widget, "detail")
	require.NotNil(t, detail.Encoding)
	assert.Equal(t, "delimited", detail.Encoding.Name, "proto2 group is delimited-encoded")
	// An enum-value custom option is preserved on the member.
	kind := enumOf(t, doc, "ropt.Kind")
	require.Len(t, kind.Members, 2)
	assert.JSONEq(t, `"beta"`, string(kind.Members[1].Extensions["protobuf:option:ropt.label"]))
	// A service-level custom option lands on its operation group; a method-level
	// one lands on its operation.
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	team, ok := doc.Services[0].Groups[0].Extensions["protobuf:option:ropt.team"]
	require.True(t, ok, "service custom option preserved on the group")
	assert.JSONEq(t, `"platform"`, string(team))
	do := opByName(t, doc, "Do")
	assert.JSONEq(t, `"yes"`, string(do.Extensions["protobuf:option:ropt.audit"]))
}

func assertFileOptions(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	raw, ok := doc.Extensions["protobuf:file"]
	require.True(t, ok)
	// Only the options that are set appear; unset ones are omitted.
	assert.JSONEq(t, `{
		"syntax": "proto3",
		"goPackage": "example.com/fopt",
		"javaPackage": "com.example.fopt",
		"javaMultipleFiles": true,
		"deprecated": true
	}`, string(raw))
}

func assertNoPackage(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	assert.Empty(t, doc.Name, "a package-less file has no document name")
	require.Len(t, doc.Services, 1)
	assert.Empty(t, doc.Services[0].Namespace, "no package means no namespace")
	item := modelOf(t, doc, "Item")
	assert.Empty(t, item.Namespace)
	// The service identity falls back to the source path.
	assert.Equal(t, ir.ServiceID("s/protobuf/no-package.proto"), doc.Services[0].ID)
}

func assertEditions(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	rec := modelOf(t, doc, "ed.Rec")
	assert.Equal(t, ir.PresenceExplicit, propByName(t, rec, "id").Presence,
		"editions default field presence is explicit")
	assert.Equal(t, ir.PresenceImplicit, propByName(t, rec, "count").Presence,
		"features.field_presence = IMPLICIT resolves to implicit presence")
	raw, ok := doc.Extensions["protobuf:file"]
	require.True(t, ok)
	assert.Contains(t, string(raw), "editions")
	assert.Contains(t, string(raw), "EDITION_2023")
}
