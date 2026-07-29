package ir_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestOperation_ZeroValueShape pins Operation's omitempty contract. Name,
// Docs, OneWay, Idempotency, Bindings, and Provenance carry no omitempty
// because every operation has a naming, docs, a one-way flag, an idempotency
// classification, a (possibly empty) bindings struct, and provenance. Auth
// carries no omitempty for the same reason as Service.Auth and Server.Auth:
// an empty non-nil slice ("explicitly public") must differ from nil ("inherit
// the service default").
func TestOperation_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Operation{},
		`{"name":{},"docs":{},"oneWay":false,"idempotency":{},"auth":null,"bindings":{},"provenance":{"source":0}}`)
}

// TestOperation_PopulatedRoundTrip pins that a fully populated Operation —
// request/response payloads, errors, streaming, pagination, long-running
// semantics, idempotency, auth override, visibility overrides, and bindings —
// round-trips.
func TestOperation_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	overloadOf := ir.OpID("op/base")
	want := ir.Operation{
		ID:           "op/openapi/paths/~1users/get",
		Name:         populatedNaming(),
		Docs:         populatedDocs(),
		Deprecation:  populatedDeprecation(),
		Availability: populatedAvailability(),
		Params: []ir.Parameter{
			{Name: ir.Naming{Source: "limit"}, Type: populatedTypeRef(), Required: true},
			{Name: ir.Naming{Source: "offset"}, Type: populatedTypeRef(), Required: false},
		},
		Request: &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: populatedTypeRef()}}},
		Responses: []ir.Response{
			{
				Name:       ir.Naming{Source: "ok"},
				Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 200, To: 200}}},
				Payload:    &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: populatedTypeRef()}}},
			},
			{
				Name:       ir.Naming{Source: "accepted"},
				Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 202, To: 202}}},
			},
		},
		Errors: []ir.ErrorCase{
			{Type: populatedTypeRef(), Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 400, To: 499}}}, Fault: "client"},
		},
		OneWay:    false,
		Streaming: ir.StreamingBidi,
		RequestStream: &ir.StreamDetail{
			Events:         &ir.TypeRef{Target: "t/req-events"},
			RequiresLength: true,
		},
		ResponseStream: &ir.StreamDetail{
			Events: &ir.TypeRef{Target: "t/resp-events"},
		},
		Pagination: &ir.Pagination{
			Strategy:    ir.PageStrategyCursor,
			InputCursor: &ir.ParamPath{Param: "cursor"},
			Items:       &ir.PropPath{Segments: []ir.PropID{"p/items"}},
			NextCursor:  &ir.PropPath{Segments: []ir.PropID{"p/nextCursor"}},
		},
		LongRunning: &ir.LongRunning{
			FinalStateVia: "operation-location",
			ResultPath:    &ir.PropPath{Segments: []ir.PropID{"p/result"}},
		},
		Idempotency: ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: "idempotency-key"},
		Auth: []ir.AuthRequirement{
			{Schemes: []ir.SchemeUse{{Scheme: "auth/apiKey"}}},
		},
		Tags:                 []string{"users", "public"},
		ParameterVisibility:  []ir.Lifecycle{ir.LifecycleCreate},
		ReturnTypeVisibility: []ir.Lifecycle{ir.LifecycleRead},
		OverloadOf:           &overloadOf,
		Bindings:             ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "GET", URITemplate: "/users"}}},
		Examples: []ir.Example{
			{Name: "ex1", Input: &ir.Value{Kind: ir.ValueString, Str: "in"}, Output: &ir.Value{Kind: ir.ValueString, Str: "out"}},
		},
		Preserved:  populatedPreserved(),
		Provenance: populatedProvenance(),
	}
	assertRoundTrip(t, want)
}

// TestStreamingMode_Constants pins the on-disk spelling of every StreamingMode
// value.
func TestStreamingMode_Constants(t *testing.T) {
	t.Parallel()
	assertConstantSpellings(t, map[ir.StreamingMode]string{
		ir.StreamingNone:   "none",
		ir.StreamingClient: "client",
		ir.StreamingServer: "server",
		ir.StreamingBidi:   "bidi",
	}, "unspecified")
}

// TestIdempotencyKind_Constants pins the on-disk spelling of every
// IdempotencyKind value, including the empty-string "unknown" state.
func TestIdempotencyKind_Constants(t *testing.T) {
	t.Parallel()
	assertConstantSpellings(t, map[ir.IdempotencyKind]string{
		ir.IdempotencyUnknown:    "",
		ir.IdempotencySafe:       "safe",
		ir.IdempotencyIdempotent: "idempotent",
		ir.IdempotencyToken:      "idempotency_token",
	}, "unknown")
}

// TestPageStrategy_Constants pins the on-disk spelling of every PageStrategy
// value.
func TestPageStrategy_Constants(t *testing.T) {
	t.Parallel()
	assertConstantSpellings(t, map[ir.PageStrategy]string{
		ir.PageStrategyCursor:     "cursor",
		ir.PageStrategyOffset:     "offset",
		ir.PageStrategyPage:       "page",
		ir.PageStrategyLinkHeader: "link_header",
		ir.PageStrategyNextLink:   "next_link",
		ir.PageStrategyToken:      "token",
	}, "unspecified")
}

// TestParameter_JSONContract pins Parameter's omitempty contract — Name,
// Type, Required, and Docs carry no omitempty since every parameter has a
// naming, a type, a required flag, and a docs object; everything else is
// optional — and that a fully populated Parameter round-trips.
func TestParameter_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Parameter{},
		`{"name":{},"type":{"target":"","nullable":false},"required":false,"docs":{}}`,
		ir.Parameter{
			Name:         populatedNaming(),
			Type:         populatedTypeRef(),
			Required:     true,
			Default:      &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("10")},
			Constraints:  populatedConstraints(),
			ValueFrom:    &ir.PropPath{In: "header", Segments: []ir.PropID{"p/x"}},
			Docs:         populatedDocs(),
			Deprecation:  populatedDeprecation(),
			Availability: populatedAvailability(),
			Examples: []ir.Example{
				{Name: "ex1", Value: &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("1")}},
				{Name: "ex2", Value: &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("2")}},
			},
			Preserved: populatedPreserved(),
		})
}

// TestPayload_JSONContract pins Payload's omitempty contract (both fields are
// optional) and that a Payload with multiple media-type contents round-trips,
// all kept per the "no primary-response selection" invariant.
func TestPayload_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Payload{}, `{}`, ir.Payload{
		Contents: []ir.Content{
			{MediaType: "application/json", Type: populatedTypeRef()},
			{MediaType: "application/xml", Type: populatedTypeRef()},
		},
		Preserved: populatedPreserved(),
	})
}

// TestContent_JSONContract pins Content's omitempty contract — Type carries
// no omitempty, every other field is optional — and that a fully populated
// Content — item schema for sequential streaming, per-part encodings, and
// file info — round-trips.
func TestContent_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Content{}, `{"type":{"target":"","nullable":false}}`, ir.Content{
		MediaType:    "multipart/form-data",
		SchemaFormat: "application/vnd.apache.avro+json",
		Type:         populatedTypeRef(),
		Item:         &ir.TypeRef{Target: "t/item"},
		ItemEncoding: &ir.PartEncoding{ContentTypes: []string{"application/json"}, Multi: true},
		Encoding: map[string]ir.PartEncoding{
			"p/file": {Filename: true, Multi: true},
			"p/name": {Style: "form"},
		},
		File: &ir.FileInfo{IsText: true, ContentTypes: []string{"text/plain"}},
		Examples: []ir.Example{
			{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, Str: "v1"}},
		},
		Preserved: populatedPreserved(),
	})
}

// TestContent_EncodingDeterministic pins Class C for Content's one map field:
// Encoding must marshal with keys in sorted order on every run.
func TestContent_EncodingDeterministic(t *testing.T) {
	t.Parallel()
	content := ir.Content{
		Type: populatedTypeRef(),
		Encoding: map[string]ir.PartEncoding{
			"z-part": {Style: "z"}, "m-part": {Style: "m"}, "a-part": {Style: "a"},
			"q-part": {Style: "q"}, "b-part": {Style: "b"},
		},
	}
	got := assertDeterministicMarshal(t, content)
	assert.Contains(t, got, `"a-part"`)
	aIdx, bIdx := strings.Index(got, `"a-part"`), strings.Index(got, `"b-part"`)
	mIdx, qIdx := strings.Index(got, `"m-part"`), strings.Index(got, `"q-part"`)
	zIdx := strings.Index(got, `"z-part"`)
	assert.True(t, aIdx < bIdx && bIdx < mIdx && mIdx < qIdx && qIdx < zIdx, "map keys must appear in sorted order: %s", got)
}

// TestContent_ItemEncodingSingular pins that ItemEncoding encodes as one bare
// object rather than a keyed envelope: it states the encoding of every item of
// a sequential stream, so it is singular like the Item it encodes.
func TestContent_ItemEncodingSingular(t *testing.T) {
	t.Parallel()
	content := ir.Content{
		Type:         populatedTypeRef(),
		ItemEncoding: &ir.PartEncoding{Multi: true, Style: "a"},
	}
	got := assertDeterministicMarshal(t, content)
	assert.Contains(t, got, `"itemEncoding":{"multi":true,"filename":false,"style":"a"}`)
}

// TestPartEncoding_JSONContract pins PartEncoding's omitempty contract — Multi
// and Filename are plain bools that must always serialize as declared facts
// ("does not repeat", "carries no filename") — and that a fully populated
// PartEncoding round-trips.
func TestPartEncoding_JSONContract(t *testing.T) {
	t.Parallel()
	explode := false
	assertJSONContract(t, ir.PartEncoding{}, `{"multi":false,"filename":false}`, ir.PartEncoding{
		ContentTypes: []string{"image/png", "image/jpeg"},
		Headers:      []ir.Property{populatedProperty()},
		Multi:        true,
		Filename:     true,
		Style:        "form",
		Explode:      &explode,
	})
}

// TestFileInfo_JSONContract pins FileInfo's omitempty contract (IsText is a
// plain bool that always serializes as a declared fact, "binary by default")
// and that a fully populated FileInfo round-trips.
func TestFileInfo_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.FileInfo{}, `{"isText":false}`, ir.FileInfo{
		IsText:             true,
		Contents:           &ir.TypeRef{Target: "t/prim/string"},
		ContentTypes:       []string{"image/png", "image/jpeg"},
		ContentTypeDefault: "image/png",
		FilenameLocation:   "header",
		FilenameWireName:   "X-Filename",
	})
}

// TestResponse_JSONContract pins Response's omitempty contract (Name,
// Conditions, and Docs carry no omitempty; every other field is optional)
// and that a fully populated Response — status-code source, headers, and
// status-code member path — round-trips.
func TestResponse_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Response{}, `{"name":{},"conditions":{},"docs":{}}`, ir.Response{
		Name:           ir.Naming{Source: "ok"},
		Conditions:     ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 200, To: 200}, {From: 202, To: 202}}},
		Payload:        &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: populatedTypeRef()}}},
		Headers:        []ir.Property{populatedProperty()},
		StatusCodeProp: &ir.PropPath{Segments: []ir.PropID{"p/status"}},
		Docs:           populatedDocs(),
		Preserved:      populatedPreserved(),
	})
}

// TestResponseConditions_JSONContract pins ResponseConditions' omitempty
// contract (StatusCodes is optional, "empty = unconditional") and that a
// populated ResponseConditions round-trips with its ranges in source order.
func TestResponseConditions_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.ResponseConditions{}, `{}`,
		ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 400, To: 499}, {From: 500, To: 599}}})
}

// TestStatusRange_ZeroValueShape pins StatusRange's contract that both bounds
// carry no omitempty: {0,0} is the meaningful "default/catch-all" range per
// the source comment, distinct from the field being absent.
func TestStatusRange_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.StatusRange{}, `{"from":0,"to":0}`)
}

// TestStatusRange_PopulatedRoundTrip pins that a populated StatusRange
// round-trips, and that the {0,0} default/catch-all range is distinguishable
// from an explicit 200-200 range.
func TestStatusRange_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want ir.StatusRange
	}{
		{name: "default catch-all", want: ir.StatusRange{From: 0, To: 0}},
		{name: "single code", want: ir.StatusRange{From: 200, To: 200}},
		{name: "range", want: ir.StatusRange{From: 400, To: 499}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertRoundTrip(t, tt.want)
		})
	}
}

// TestErrorCase_JSONContract pins ErrorCase's omitempty contract (Type,
// Conditions, and Docs carry no omitempty; every other field is optional)
// and that a fully populated ErrorCase — fault classification,
// retryable/throttling tri-state pointers — round-trips.
func TestErrorCase_JSONContract(t *testing.T) {
	t.Parallel()
	retryable := true
	throttling := false
	assertJSONContract(t, ir.ErrorCase{},
		`{"type":{"target":"","nullable":false},"conditions":{},"docs":{}}`,
		ir.ErrorCase{
			Type:       populatedTypeRef(),
			Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 429, To: 429}}},
			Fault:      "client",
			Retryable:  &retryable,
			Throttling: &throttling,
			Docs:       populatedDocs(),
			Preserved:  populatedPreserved(),
		})
}

// TestStreamDetail_JSONContract pins StreamDetail's contract that
// RequiresLength carries no omitempty ("no minimum length requirement" is a
// declared fact, not an absence) and that a fully populated StreamDetail
// round-trips.
func TestStreamDetail_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.StreamDetail{}, `{"requiresLength":false}`, ir.StreamDetail{
		Events:         &ir.TypeRef{Target: "t/events", Nullable: true},
		Initial:        &ir.TypeRef{Target: "t/initial"},
		RequiresLength: true,
	})
}

// TestIdempotency_ZeroValueShape pins Idempotency's omitempty contract: both
// fields are optional (TokenParam is set only for the token kind).
func TestIdempotency_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Idempotency{}, `{}`)
}

// TestIdempotency_PopulatedRoundTrip pins that a populated Idempotency
// round-trips for each kind, including the token kind's TokenParam.
func TestIdempotency_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	tests := map[string]ir.Idempotency{
		"safe":       {Kind: ir.IdempotencySafe},
		"idempotent": {Kind: ir.IdempotencyIdempotent},
		"token":      {Kind: ir.IdempotencyToken, TokenParam: "idempotency-key"},
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRoundTrip(t, want)
		})
	}
}

// TestPagination_JSONContract pins Pagination's contract that Inferred
// carries no omitempty — whether a pagination scheme was declared or
// heuristically detected is always a fact worth recording (ir-design §7.3),
// never an absence — and that a fully populated Pagination — every
// navigation-link path — round-trips.
func TestPagination_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Pagination{}, `{"inferred":false}`, ir.Pagination{
		Strategy:    ir.PageStrategyCursor,
		Inferred:    true,
		InputCursor: &ir.ParamPath{Param: "cursor", Segments: []ir.PropID{"p/cursor"}},
		InputLimit:  &ir.ParamPath{Param: "limit"},
		Items:       &ir.PropPath{Segments: []ir.PropID{"p/items"}},
		NextCursor:  &ir.PropPath{Segments: []ir.PropID{"p/nextCursor"}},
		NextLink:    &ir.PropPath{Segments: []ir.PropID{"p/nextLink"}},
		PrevLink:    &ir.PropPath{Segments: []ir.PropID{"p/prevLink"}},
		FirstLink:   &ir.PropPath{Segments: []ir.PropID{"p/firstLink"}},
		LastLink:    &ir.PropPath{Segments: []ir.PropID{"p/lastLink"}},
		TotalCount:  &ir.PropPath{Segments: []ir.PropID{"p/totalCount"}},
	})
}

// TestPropPath_JSONContract pins PropPath's omitempty contract (every field
// is optional; nil Root = context-determined, per the source comment) and
// that a fully populated PropPath — rooted at an explicit type, addressed
// into a header — round-trips with its segments in walk order.
func TestPropPath_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.PropPath{}, `{}`, ir.PropPath{
		Root:     &ir.TypeRef{Target: "t/response"},
		In:       "header",
		Segments: []ir.PropID{"p/outer", "p/inner"},
	})
}

// TestParamPath_JSONContract pins ParamPath's omitempty contract (both fields
// are optional) and that a populated ParamPath round-trips with its segments
// in walk order.
func TestParamPath_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.ParamPath{}, `{}`,
		ir.ParamPath{Param: "filter", Segments: []ir.PropID{"p/outer", "p/inner"}})
}

// TestLongRunning_JSONContract pins LongRunning's omitempty contract (every
// field is optional) and that a fully populated LongRunning — polling and
// final operation references, polling/final types, and result path —
// round-trips.
func TestLongRunning_JSONContract(t *testing.T) {
	t.Parallel()
	pollOp := ir.OpID("op/poll")
	finalOp := ir.OpID("op/final")
	assertJSONContract(t, ir.LongRunning{}, `{}`, ir.LongRunning{
		FinalStateVia:    "operation-location",
		PollingOperation: &pollOp,
		FinalOperation:   &finalOp,
		PollingType:      &ir.TypeRef{Target: "t/status-monitor"},
		FinalType:        &ir.TypeRef{Target: "t/final-result"},
		ResultPath:       &ir.PropPath{Segments: []ir.PropID{"p/result"}},
	})
}
