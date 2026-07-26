package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestOpBindings_ZeroValueShape pins OpBindings' omitempty contract: every
// protocol binding is a pointer or slice that is nil when the operation does
// not speak that protocol, so the zero value (no bindings at all) marshals to
// an empty object.
func TestOpBindings_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.OpBindings{}, `{}`)
}

// TestOpBindings_PopulatedRoundTrip pins that an OpBindings carrying every
// protocol binding simultaneously — legal per the source comment ("a Smithy
// service exposed over both HTTP and RPC") — round-trips.
func TestOpBindings_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.OpBindings{
		HTTP: []ir.HTTPBinding{
			{Method: "GET", URITemplate: "/users"},
			{Method: "POST", URITemplate: "/users", SharedRoute: true},
		},
		RPC:     &ir.RPCBinding{System: "grpc", FullMethod: "/pkg.Service/Method"},
		Message: &ir.MessageBinding{Channel: "c/x", Direction: ir.MsgDirectionSend},
		GraphQL: &ir.GraphQLBinding{Kind: "query", FieldPath: []string{"users", "list"}},
		OTP:     &ir.OTPBinding{Behaviour: "gen_server", Kind: "call"},
	}
	assertRoundTrip(t, want)
}

// TestHTTPBinding_ZeroValueShape pins HTTPBinding's omitempty contract:
// SharedRoute, ChecksumRequired, and IsWebhook are plain bools that must
// always serialize as declared facts ("not shared", "no checksum required",
// "not a webhook"), while every other field stays optional.
func TestHTTPBinding_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.HTTPBinding{},
		`{"sharedRoute":false,"checksumRequired":false,"isWebhook":false}`)
}

// TestHTTPBinding_PopulatedRoundTrip pins that a fully populated HTTPBinding —
// param bindings, content types, response body path, success-status map,
// compression, PATCH optionality override, and callbacks — round-trips.
func TestHTTPBinding_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	patchOpt := false
	want := ir.HTTPBinding{
		Method:      "PATCH",
		URITemplate: "/users/{id}",
		HostPrefix:  "{region}.",
		SharedRoute: true,
		ParamBindings: []ir.HTTPParamBinding{
			{Param: "id", Location: ir.HTTPLocationPath, WireName: "id"},
			{Param: "filter", Location: ir.HTTPLocationQuery, WireName: "filter", Style: "form"},
		},
		RequestContentTypes:      []string{"application/json", "application/merge-patch+json"},
		ResponseBodyPath:         &ir.PropPath{Segments: []ir.PropID{"p/result"}},
		SuccessStatus:            map[int]int{0: 200, 1: 202},
		Compression:              &ir.RequestCompression{Encodings: []string{"gzip", "br"}},
		ChecksumRequired:         true,
		PatchImplicitOptionality: &patchOpt,
		IsWebhook:                true,
		Callbacks: []ir.Callback{
			{Expression: "$request.body#/callbackUrl", Operations: []ir.OpID{"op/callback"}},
		},
		Extensions: populatedExtensions(),
	}
	assertRoundTrip(t, want)
}

// TestHTTPBinding_SuccessStatusDeterministic pins Class C for HTTPBinding's
// map field: SuccessStatus is keyed by int, not string, but must still
// marshal identically on every run (invariant #7).
func TestHTTPBinding_SuccessStatusDeterministic(t *testing.T) {
	t.Parallel()
	binding := ir.HTTPBinding{
		SuccessStatus: map[int]int{5: 500, 1: 100, 3: 300, 4: 400, 2: 200},
	}
	got := assertDeterministicMarshal(t, binding)
	assert.Contains(t, got, `"successStatus":{"1":100,"2":200,"3":300,"4":400,"5":500}`)
}

// TestHTTPLocation_Constants pins the on-disk spelling of every HTTPLocation
// value: these strings are read back by every emitter that dispatches on
// param location, so a typo fix later would be a silent breaking change.
func TestHTTPLocation_Constants(t *testing.T) {
	t.Parallel()
	tests := map[ir.HTTPLocation]string{
		ir.HTTPLocationPath:         "path",
		ir.HTTPLocationQuery:        "query",
		ir.HTTPLocationQuerystring:  "querystring",
		ir.HTTPLocationHeader:       "header",
		ir.HTTPLocationCookie:       "cookie",
		ir.HTTPLocationBody:         "body",
		ir.HTTPLocationBodyProperty: "body_property",
		ir.HTTPLocationHost:         "host",
	}
	for loc, want := range tests {
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, ir.HTTPLocation(want), loc)
		})
	}
}

// TestRequestCompression_ZeroValueShape pins RequestCompression's omitempty
// contract: Encodings is optional.
func TestRequestCompression_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.RequestCompression{}, `{}`)
}

// TestRequestCompression_PopulatedRoundTrip pins that a populated
// RequestCompression round-trips with its encoding priority order preserved.
func TestRequestCompression_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.RequestCompression{Encodings: []string{"gzip", "br", "deflate"}})
}

// TestHTTPParamBinding_ZeroValueShape pins HTTPParamBinding's omitempty
// contract: AllowReserved is a plain bool that must always serialize as a
// declared fact, while every other field stays optional.
func TestHTTPParamBinding_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.HTTPParamBinding{}, `{"allowReserved":false}`)
}

// TestHTTPParamBinding_PopulatedRoundTrip pins that a fully populated
// HTTPParamBinding — including the multi-segment gRPC-transcoding param path
// and body path — round-trips.
func TestHTTPParamBinding_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	explode := true
	want := ir.HTTPParamBinding{
		Param:         "book",
		ParamPath:     []ir.PropID{"p/book", "p/name"},
		Location:      ir.HTTPLocationQuery,
		WireName:      "book.name",
		Style:         "deepObject",
		Explode:       &explode,
		AllowReserved: true,
		PathPattern:   "shelves/*/books/*",
		Prefix:        "x-",
		ContentType:   "application/json",
		BodyPath:      []ir.PropID{"p/body", "p/nested"},
	}
	assertRoundTrip(t, want)
}

// TestCallback_ZeroValueShape pins Callback's omitempty contract: both fields
// are optional.
func TestCallback_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Callback{}, `{}`)
}

// TestCallback_PopulatedRoundTrip pins that a populated Callback round-trips.
func TestCallback_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.Callback{
		Expression: "$request.body#/callbackUrl",
		Operations: []ir.OpID{"op/onEvent", "op/onError"},
	}
	assertRoundTrip(t, want)
}

// TestRPCBinding_ZeroValueShape pins RPCBinding's omitempty contract: every
// field is optional.
func TestRPCBinding_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.RPCBinding{}, `{}`)
}

// TestRPCBinding_PopulatedRoundTrip pins that a fully populated RPCBinding
// round-trips.
func TestRPCBinding_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.RPCBinding{
		System:           "grpc",
		FullMethod:       "/pkg.Service/Method",
		InputType:        &ir.TypeRef{Target: "t/request", Nullable: false},
		ParamStructure:   "by_position",
		IdempotencyLevel: "idempotent",
		Extensions:       populatedExtensions(),
	}
	assertRoundTrip(t, want)
}

// TestMessageBinding_ZeroValueShape pins MessageBinding's omitempty contract:
// every field is optional.
func TestMessageBinding_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.MessageBinding{}, `{}`)
}

// TestMessageBinding_PopulatedRoundTrip pins that a fully populated
// MessageBinding — reply semantics and protocol-scoped bindings — round-trips.
func TestMessageBinding_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	replyChannel := ir.ChannelID("c/reply")
	want := ir.MessageBinding{
		Channel:   "c/orders",
		Direction: ir.MsgDirectionReceive,
		Messages:  []ir.MessageID{"m/OrderPlaced", "m/OrderCancelled"},
		Reply: &ir.Reply{
			Channel:  &replyChannel,
			Address:  &ir.PropPath{In: "header", Segments: []ir.PropID{"p/replyTo"}},
			Messages: []ir.MessageID{"m/OrderConfirmed"},
			Docs:     populatedDocs(),
		},
		Bindings: map[string]ir.Extensions{
			"kafka": {"groupId": ir.RawValue(`"g1"`)},
			"amqp":  {"exchange": ir.RawValue(`"orders"`)},
		},
		Extensions: populatedExtensions(),
	}
	assertRoundTrip(t, want)
}

// TestMessageBinding_BindingsDeterministic pins Class C for MessageBinding's
// map field: Bindings must marshal with keys in sorted order on every run.
func TestMessageBinding_BindingsDeterministic(t *testing.T) {
	t.Parallel()
	binding := ir.MessageBinding{
		Bindings: map[string]ir.Extensions{
			"z-proto": {"k": ir.RawValue(`1`)},
			"m-proto": {"k": ir.RawValue(`2`)},
			"a-proto": {"k": ir.RawValue(`3`)},
			"q-proto": {"k": ir.RawValue(`4`)},
			"b-proto": {"k": ir.RawValue(`5`)},
		},
	}
	got := assertDeterministicMarshal(t, binding)
	assert.Contains(t, got,
		`"bindings":{"a-proto":{"k":3},"b-proto":{"k":5},"m-proto":{"k":2},"q-proto":{"k":4},"z-proto":{"k":1}}`)
}

// TestMsgDirection_Constants pins the on-disk spelling of both MsgDirection
// values.
func TestMsgDirection_Constants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.MsgDirection("send"), ir.MsgDirectionSend)
	assert.Equal(t, ir.MsgDirection("receive"), ir.MsgDirectionReceive)
}

// TestReply_ZeroValueShape pins Reply's contract that Docs carries no
// omitempty, since every reply has a (possibly empty) docs object, while
// Channel, Address, and Messages stay optional (a dynamic-only reply has a
// nil Channel per the source comment).
func TestReply_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Reply{}, `{"docs":{}}`)
}

// TestReply_PopulatedRoundTrip pins that a fully populated Reply round-trips.
func TestReply_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	channel := ir.ChannelID("c/reply")
	want := ir.Reply{
		Channel:  &channel,
		Address:  &ir.PropPath{In: "header", Segments: []ir.PropID{"p/replyTo"}},
		Messages: []ir.MessageID{"m/Reply1", "m/Reply2"},
		Docs:     populatedDocs(),
	}
	assertRoundTrip(t, want)
}

// TestGraphQLBinding_ZeroValueShape pins GraphQLBinding's omitempty contract:
// every field is optional.
func TestGraphQLBinding_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.GraphQLBinding{}, `{}`)
}

// TestGraphQLBinding_PopulatedRoundTrip pins that a populated GraphQLBinding —
// a nested field path — round-trips in source order.
func TestGraphQLBinding_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.GraphQLBinding{
		Kind:       "subscription",
		FieldPath:  []string{"namespace", "onUserSignup"},
		Extensions: populatedExtensions(),
	}
	assertRoundTrip(t, want)
}

// TestOTPBinding_ZeroValueShape pins OTPBinding's omitempty contract: every
// field is optional.
func TestOTPBinding_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.OTPBinding{}, `{}`)
}

// TestOTPBinding_PopulatedRoundTrip pins that a fully populated OTPBinding —
// including its symbol-valued RequestTag — round-trips.
func TestOTPBinding_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.OTPBinding{
		Behaviour:  "gen_server",
		Kind:       "call",
		Process:    "c/otp/worker",
		RequestTag: &ir.Value{Kind: ir.ValueSymbol, Str: "get"},
		Extensions: populatedExtensions(),
	}
	assertRoundTrip(t, want)
}
