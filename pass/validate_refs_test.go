// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/ir/irverify"
	"github.com/dexpace/morphic/pass"
)

// refSite is one TypeRef-bearing field that the hand-written walk predating
// checkDanglingRefs never visited. plant writes target into that field and
// nothing else, so a diagnostic naming target attributes the find to that field
// alone; where is a fragment of the location the diagnostic must point at.
type refSite struct {
	name   string
	where  string
	plant  func(doc *ir.Document, target ir.TypeID)
	target ir.TypeID
}

// eventAndBindingSites are the event/messaging, streaming, long-running and RPC
// binding fields — the cluster no implemented compiler populates yet.
func eventAndBindingSites() []refSite {
	return []refSite{
		{"message headers", ".Headers", func(document *ir.Document, typeID ir.TypeID) {
			putMessage(document, func(message *ir.Message) { message.Headers = &ir.TypeRef{Target: typeID} })
		}, "type/ghost/message-headers"},
		{"message correlation root", ".CorrelationID.Root", func(document *ir.Document, typeID ir.TypeID) {
			putMessage(document, func(message *ir.Message) { message.CorrelationID = &ir.PropertyPath{Root: &ir.TypeRef{Target: typeID}} })
		}, "type/ghost/correlation-root"},
		{"channel params", ".Params[0]", func(document *ir.Document, typeID ir.TypeID) {
			putChannel(document, func(channel *ir.Channel) {
				channel.Params = append(channel.Params, ir.Parameter{Name: ir.Naming{Source: "room"}, Type: ir.TypeRef{Target: typeID}})
			})
		}, "type/ghost/channel-param"},
		{"request stream initial", ".RequestStream.Initial", func(document *ir.Document, typeID ir.TypeID) {
			firstOperation(document).RequestStream = &ir.StreamDetail{Initial: &ir.TypeRef{Target: typeID}}
		}, "type/ghost/stream-initial"},
		{"response stream events", ".ResponseStream.Events", func(document *ir.Document, typeID ir.TypeID) {
			firstOperation(document).ResponseStream = &ir.StreamDetail{Events: &ir.TypeRef{Target: typeID}}
		}, "type/ghost/stream-events"},
		{"long-running polling type", ".LongRunning.PollingType", func(document *ir.Document, typeID ir.TypeID) {
			longRunning(document).PollingType = &ir.TypeRef{Target: typeID}
		}, "type/ghost/lro-polling"},
		{"long-running final type", ".LongRunning.FinalType", func(document *ir.Document, typeID ir.TypeID) {
			longRunning(document).FinalType = &ir.TypeRef{Target: typeID}
		}, "type/ghost/lro-final"},
		{"long-running result path root", ".LongRunning.ResultPath.Root", func(document *ir.Document, typeID ir.TypeID) {
			longRunning(document).ResultPath = &ir.PropertyPath{Root: &ir.TypeRef{Target: typeID}}
		}, "type/ghost/lro-result"},
		{"rpc binding input type", ".Bindings.RPC.InputType", func(document *ir.Document, typeID ir.TypeID) {
			firstOperation(document).Bindings.RPC = &ir.RPCBinding{System: "grpc", InputType: &ir.TypeRef{Target: typeID}}
		}, "type/ghost/rpc-input"},
	}
}

// payloadAndVersioningSites are the fields reachable from an ordinary HTTP
// document: file bodies, multipart part headers, field arguments, values,
// renames, discriminator defaults, and the versioning timeline.
func payloadAndVersioningSites() []refSite {
	return []refSite{
		{"content file contents", ".File.Contents", func(document *ir.Document, typeID ir.TypeID) {
			requestContent(document).File = &ir.FileInfo{Contents: &ir.TypeRef{Target: typeID}}
		}, "type/ghost/file-contents"},
		{"multipart part encoding headers", ".Encoding[", func(document *ir.Document, typeID ir.TypeID) {
			// The key names typeID/m's own property, so only the header target
			// dangles: an unknown key is its own defect (checkEncodingKeys).
			requestContent(document).Encoding = map[ir.PropertyID]ir.PartEncoding{"property/m/a": {Headers: []ir.Header{{
				ID: "property/hdr", Name: ir.Naming{Source: "X-Meta"}, Type: ir.TypeRef{Target: typeID},
			}}}}
		}, "type/ghost/part-header"},
		{"property args", ".Args[0]", func(document *ir.Document, typeID ir.TypeID) {
			model(document).Properties[0].Args = []ir.Parameter{
				{Name: ir.Naming{Source: "first"}, Type: ir.TypeRef{Target: typeID}},
			}
		}, "type/ghost/field-arg"},
		{"property default value ref", ".Default.Ref.Type", func(document *ir.Document, typeID ir.TypeID) {
			model(document).Properties[0].Default = &ir.Value{
				Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: typeID, Member: "a"},
			}
		}, "type/ghost/value-ref"},
		{"availability versioned type", ".Availability.TypeChangedFrom[0]", func(document *ir.Document, typeID ir.TypeID) {
			model(document).Properties[0].Availability = &ir.Availability{
				TypeChangedFrom: []ir.VersionedType{{Version: "v1", Type: ir.TypeRef{Target: typeID}}},
			}
		}, "type/ghost/versioned-type"},
		{"discriminator default", ".Discriminator.Default", func(document *ir.Document, typeID ir.TypeID) {
			document.Types["type/u"] = &ir.Union{
				ID:            "type/u",
				Discriminator: &ir.Discriminator{PropertyName: "kind", Default: typeID},
			}
		}, "type/ghost/disc-default"},
		{"service rename key", ".Renames[", func(document *ir.Document, typeID ir.TypeID) {
			service(document).Renames = map[ir.TypeID]ir.Naming{typeID: {Source: "Ghost"}}
		}, "type/ghost/rename-key"},
		{"service common errors", ".CommonErrors[0]", func(document *ir.Document, typeID ir.TypeID) {
			service(document).CommonErrors = []ir.ErrorCase{{
				Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: typeID}}}},
			}}
		}, "type/ghost/common-error"},
	}
}

// unwalkedRefSites is the full mutation set.
func unwalkedRefSites() []refSite {
	return append(eventAndBindingSites(), payloadAndVersioningSites()...)
}

// model returns validDoc's model so a case can plant into its first property.
func model(doc *ir.Document) *ir.Model {
	return doc.Types["type/m"].(*ir.Model)
}

// service returns the document's single service, creating it on first use so
// several cases can plant into one document.
func service(doc *ir.Document) *ir.Service {
	if len(doc.Services) == 0 {
		doc.Services = []ir.Service{{
			ID:     "s",
			Groups: []ir.OperationGroup{{Operations: []ir.Operation{{ID: "op"}}}},
		}}
	}
	return &doc.Services[0]
}

// firstOperation returns the document's single operation, creating it on first use.
func firstOperation(doc *ir.Document) *ir.Operation {
	return &service(doc).Groups[0].Operations[0]
}

// longRunning returns the operation's long-running block, creating it on first
// use so the three long-running cases compose in one document.
func longRunning(doc *ir.Document) *ir.LongRunning {
	operation := firstOperation(doc)
	if operation.LongRunning == nil {
		operation.LongRunning = &ir.LongRunning{FinalStateVia: "status-monitor"}
	}
	return operation.LongRunning
}

// requestContent returns the operation's single request content, creating it
// with a resolving type so only the planted target can dangle.
func requestContent(doc *ir.Document) *ir.Content {
	operation := firstOperation(doc)
	if operation.Request == nil {
		operation.Request = &ir.Payload{Contents: []ir.Content{{
			MediaType: "multipart/form-data", Type: ir.TypeRef{Target: "type/m"},
		}}}
	}
	return &operation.Request.Contents[0]
}

// putMessage read-modify-writes the document's single message; Messages holds
// values, so a case cannot take a pointer into it.
func putMessage(doc *ir.Document, mutate func(*ir.Message)) {
	message := doc.Messages["message/a"]
	message.ID = "message/a"
	mutate(&message)
	if doc.Messages == nil {
		doc.Messages = map[ir.MessageID]ir.Message{}
	}
	doc.Messages["message/a"] = message
}

// putChannel read-modify-writes the document's single channel.
func putChannel(doc *ir.Document, mutate func(*ir.Channel)) {
	ch := doc.Channels["channel/a"]
	ch.ID = "channel/a"
	mutate(&ch)
	if doc.Channels == nil {
		doc.Channels = map[ir.ChannelID]ir.Channel{}
	}
	doc.Channels["channel/a"] = ch
}

// danglingDiags returns the dangling-type-ref diagnostics among diags.
func danglingDiags(diags []ir.Diagnostic) []ir.Diagnostic {
	out := make([]ir.Diagnostic, 0, len(diags))
	for _, diagnostic := range diags {
		if diagnostic.Code == "ir/dangling-type-ref" {
			out = append(out, diagnostic)
		}
	}
	return out
}

// TestValidate_DanglingRefInPreviouslyUnwalkedField plants one dangling target
// per ref-bearing field the old enumeration skipped and requires Validate to
// report it, at that field's location. Before the reflection walk every case
// here produced no diagnostic at all.
func TestValidate_DanglingRefInPreviouslyUnwalkedField(t *testing.T) {
	t.Parallel()
	for _, tc := range unwalkedRefSites() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			tc.plant(doc, tc.target)
			found := danglingDiags(validated(t, doc))
			require.Len(t, found, 1, "exactly the planted target must dangle")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Contains(t, found[0].Message, string(tc.target))
			assert.Contains(t, found[0].Provenance.Node, tc.where)
		})
	}
}

// aliasedDoc returns a document whose two registry entries share one *TypeRef.
// That is the shape the reference walk prunes: the shared pointer is descended
// into at whichever entry the walk reaches first, so the surviving diagnostic
// names t/a's Base on one run and t/b's on the next unless the walk's order is
// fixed. The shared target dangles so the walk has something to report.
func aliasedDoc() *ir.Document {
	shared := &ir.TypeRef{Target: "type/ghost/aliased"}
	a := &ir.Model{ID: "type/a", Base: shared}
	b := &ir.Model{ID: "type/b", Base: shared}
	return &ir.Document{Types: ir.TypeRegistry{a.ID: a, b.ID: b}}
}

// diagJSON renders diags whole, so the comparison covers every field rather than
// a chosen one.
func diagJSON(t *testing.T, diags []ir.Diagnostic) string {
	t.Helper()
	b, err := json.Marshal(diags)
	require.NoError(t, err)
	return string(b)
}

// TestValidate_AliasedPointerIsDeterministic pins invariant 7 against pointer
// aliasing: repeated runs over one document must produce byte-identical
// diagnostics. Sorting cannot deliver that on its own — a randomized walk of an
// aliased graph varies which sites exist, not merely their order — so this fails
// on any walk whose map iteration is left to Go. validated reads the document
// until it has said so determinismRuns times.
func TestValidate_AliasedPointerIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := aliasedDoc()
	got := diagJSON(t, validated(t, doc))

	require.Contains(t, got, "type/ghost/aliased", "the shared target must dangle, or nothing is being compared")
}

// pointers returns each diagnostic's IR-space location, in order.
func pointers(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		out = append(out, diagnostic.Provenance.Node)
	}
	return out
}

// sortedRefPointers is the location order every run must produce: ascending by
// pointer, which is what sorting the sites before building diagnostics yields.
//
// It is written out rather than captured from a first run because a captured
// order proves only that two runs agree — a walk that emitted these in its own
// traversal order would satisfy that and still leak map iteration into the
// output the moment a document made the traversal order vary.
var sortedRefPointers = []string{
	"document.Channels[channel/a].Params[0].Type.Target",
	"document.Messages[message/a].CorrelationID.Root.Target",
	"document.Messages[message/a].Headers.Target",
	"document.Services[0].CommonErrors[0].Payload.Contents[0].Type.Target",
	"document.Services[0].Groups[0].Operations[0].Bindings.RPC.InputType.Target",
	"document.Services[0].Groups[0].Operations[0].LongRunning.FinalType.Target",
	"document.Services[0].Groups[0].Operations[0].LongRunning.PollingType.Target",
	"document.Services[0].Groups[0].Operations[0].LongRunning.ResultPath.Root.Target",
	"document.Services[0].Groups[0].Operations[0].Request.Contents[0].Encoding[property/m/a].Headers[0].Type.Target",
	"document.Services[0].Groups[0].Operations[0].Request.Contents[0].File.Contents.Target",
	"document.Services[0].Groups[0].Operations[0].RequestStream.Initial.Target",
	"document.Services[0].Groups[0].Operations[0].ResponseStream.Events.Target",
	"document.Services[0].Renames[type/ghost/15].key",
	"document.Types[type/m].Properties[0].Args[0].Type.Target",
	"document.Types[type/m].Properties[0].Availability.TypeChangedFrom[0].Type.Target",
	"document.Types[type/m].Properties[0].Default.Ref.Type",
	"document.Types[type/u].Discriminator.Default",
}

// TestValidate_DiagnosticOrderIsDeterministic pins invariant 7 for the
// reflection walk: it reaches references through maps, whose iteration order Go
// randomizes per range, so the sites must be sorted by location before any
// diagnostic is built. The order is the pinned one, and validated holds
// every further run to it.
func TestValidate_DiagnosticOrderIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	for i, tc := range unwalkedRefSites() {
		tc.plant(doc, ir.TypeID(fmt.Sprintf("type/ghost/%02d", i)))
	}
	require.Equal(t, sortedRefPointers, pointers(danglingDiags(validated(t, doc))))
}

// TestValidate_ResolvedRefsInEveryFieldAreClean is the other half of the
// mutation proof: with every one of those fields populated by a target that
// does resolve, Validate reports no dangling reference. A check that cannot
// stay silent is no better than one that cannot fire.
func TestValidate_ResolvedRefsInEveryFieldAreClean(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	for _, tc := range unwalkedRefSites() {
		tc.plant(doc, "type/primitive/string")
	}
	assert.Empty(t, danglingDiags(validated(t, doc)))
}

// TestValidate_LocatesDanglingRefsWhereIrverifyDoes pins the location half of
// the package doc's one-defect-one-code rule: the two checkers agree on the code
// already, and a caller running both can only dedupe if they also agree on
// where. The pinned list is the same one Validate is held to above, so a
// spelling change on either side breaks this rather than quietly producing two
// reports of one defect.
//
// The two walks stay separate implementations — the layering permits pass to
// import ir/irverify, but a diagnostic and a violation are different types with
// different audiences — so nothing except this test keeps them in step.
func TestValidate_LocatesDanglingRefsWhereIrverifyDoes(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	for i, tc := range unwalkedRefSites() {
		tc.plant(doc, ir.TypeID(fmt.Sprintf("type/ghost/%02d", i)))
	}

	var got []string
	for _, violation := range irverify.Verify(doc) {
		if violation.Code == "ir/dangling-type-ref" {
			got = append(got, violation.Path)
		}
	}
	require.Equal(t, sortedRefPointers, got, "irverify must name the same locations")
	assert.Equal(t, got, pointers(danglingDiags(validated(t, doc))))
}

func TestValidate_DanglingTypeRef(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	model := doc.Types["type/m"].(*ir.Model)
	model.Properties[0].Type.Target = "type/nowhere"
	diags := validated(t, doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, irtest.DiagCodes(diags), "ir/dangling-type-ref")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

// TestValidate_DanglingDocRef pins the check prose could not have while its
// references were tokens inside it: a reference out of band is a TypeID like any
// other, so one naming no type is reported where it sits, and text shaped like a
// token is only text.
func TestValidate_DanglingDocRef(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	model := doc.Types["type/m"].(*ir.Model)
	model.Docs = ir.Docs{Description: "{t:t/nowhere} reads Pet", Refs: []ir.DocRef{{StartByte: 20, EndByte: 23, Type: "type/pet"}}}
	found := withCode(validated(t, doc), "ir/dangling-type-ref")
	require.Len(t, found, 1, "the token-shaped text is not a reference, and the span is")
	assert.Contains(t, found[0].Message, "type/pet")
	assert.Equal(t, "document.Types[type/m].Docs.Refs[0].Type", found[0].Provenance.Node)
}

// TestValidate_DanglingRefInMessagePayload exercises the Messages walk in
// checkDanglingRefs: a message whose payload content references a missing type
// must be reported against the message location.
func TestValidate_DanglingRefInMessagePayload(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Messages = map[ir.MessageID]ir.Message{
		"message/a": {
			ID: "message/a",
			Payload: ir.Payload{Contents: []ir.Content{
				{MediaType: "application/json", Type: ir.TypeRef{Target: "type/ghost"}},
			}},
		},
	}
	diags := validated(t, doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, irtest.DiagCodes(diags), "ir/dangling-type-ref")
}

// TestValidate_DanglingRefsAcrossContainerKinds gives every reference a
// container TypeDef carries (Scalar base/encoding, List elem, MapT key+value,
// Tuple elems and rest) a dangling target, so each must be reported once.
func TestValidate_DanglingRefsAcrossContainerKinds(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["type/scalar"] = &ir.Scalar{
		ID:       "type/scalar",
		Base:     &ir.TypeRef{Target: "type/ghost-base"},
		Encoding: &ir.Encoding{Name: "x", WireType: &ir.TypeRef{Target: "type/ghost-wire"}},
	}
	doc.Types["type/list"] = &ir.List{
		ID:          "type/list",
		ElementType: ir.TypeRef{Target: "type/ghost-elem"},
		Encoding:    &ir.Encoding{Name: "packed", WireType: &ir.TypeRef{Target: "type/ghost-lwire"}},
	}
	doc.Types["type/map"] = &ir.MapT{
		ID:    "type/map",
		Key:   &ir.TypeRef{Target: "type/ghost-key"},
		Value: ir.TypeRef{Target: "type/ghost-val"},
	}
	doc.Types["type/tuple"] = &ir.Tuple{
		ID:           "type/tuple",
		ElementTypes: []ir.TypeRef{{Target: "type/ghost-e0"}},
		Rest:         &ir.TypeRef{Target: "type/ghost-rest"},
	}
	diags := validated(t, doc)
	// Each dangling target above yields exactly one dangling-type-ref diagnostic.
	assert.Equal(t, 8, countCode(t, diags, "ir/dangling-type-ref"))
}

// TestValidate_ModelImplementsAndAdditionalPropertiesWalked covers a model's
// Implements references and the Value, Key and Patterns references its
// AdditionalProperties carries.
func TestValidate_ModelImplementsAndAdditionalPropertiesWalked(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	model := doc.Types["type/m"].(*ir.Model)
	model.Implements = []ir.TypeRef{{Target: "type/ghost-iface"}}
	model.AdditionalProperties = &ir.AdditionalProperties{
		Value: &ir.TypeRef{Target: "type/ghost-apval"},
		Key:   &ir.TypeRef{Target: "type/ghost-apkey"},
		Patterns: []ir.PatternProperties{
			{Pattern: "^x", Value: ir.TypeRef{Target: "type/ghost-appat"}},
		},
	}
	diags := validated(t, doc)
	assert.Equal(t, 4, countCode(t, diags, "ir/dangling-type-ref"))
}

// TestValidate_OperationHeadersAndItemWalked covers an operation's response
// headers and the per-media-type Item a content declares.
func TestValidate_OperationHeadersAndItemWalked(t *testing.T) {
	t.Parallel()
	operation := ir.Operation{
		ID: "op",
		Request: &ir.Payload{Contents: []ir.Content{{
			MediaType: "application/json",
			Type:      ir.TypeRef{Target: "type/primitive/string"},
			Item:      &ir.TypeRef{Target: "type/ghost-item"},
		}}},
		Responses: []ir.Response{{
			Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "type/primitive/string"}}}},
			Headers: []ir.Header{{
				ID: "property/h", Name: ir.Naming{Source: "X-Trace"}, WireName: "X-Trace",
				Type: ir.TypeRef{Target: "type/ghost-hdr"},
			}},
		}},
		Errors: []ir.ErrorCase{{
			Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "type/ghost-err"}}}},
			// The error side's headers, which nothing reached until now: skipping
			// ErrorCase.Headers in the reflection walk left the suite green, where
			// skipping the Response.Headers above reddens this test.
			Headers: []ir.Header{{
				ID: "property/eh", Name: ir.Naming{Source: "X-Retry"}, WireName: "X-Retry",
				Type: ir.TypeRef{Target: "type/ghost-err-hdr"},
			}},
		}},
	}
	diags := validated(t, docWithOperation(operation))
	// item, response header, error payload and error header targets all dangle.
	assert.Equal(t, 4, countCode(t, diags, "ir/dangling-type-ref"))
}

// idRefSite is one field carrying a typed-ID reference of a class other than
// ir.TypeID.
//
// The sites are listed by hand from the ir package's declarations: every field
// whose type is one of those classes. A node's own ID (Channel.ID,
// Operation.ID, Service.ID and their siblings) and a registry key resolve
// against their own declaration by construction, so only cross-references
// appear. A class added to the IR stays untested until listed, as the operation
// and service classes did (GitHub #50).
type idRefSite struct {
	name  string
	code  string
	where string
	plant func(doc *ir.Document, id string)
	id    string
}

// channelRefSites are the fields referencing an entry of Document.Channels.
func channelRefSites() []idRefSite {
	return []idRefSite{
		{"message binding channel", "ir/dangling-channel-ref", ".Bindings.Message.Channel",
			func(document *ir.Document, id string) { messageBinding(document).Channel = ir.ChannelID(id) },
			"channel/ghost/binding"},
		{"reply channel", "ir/dangling-channel-ref", ".Bindings.Message.Reply.Channel",
			func(document *ir.Document, id string) {
				ch := ir.ChannelID(id)
				reply(document).Channel = &ch
			}, "channel/ghost/reply"},
		{"otp binding process", "ir/dangling-channel-ref", ".Bindings.OTP.Process",
			func(document *ir.Document, id string) {
				firstOperation(document).Bindings.OTP = &ir.OTPBinding{Behaviour: "gen_server", Kind: "call", Process: ir.ChannelID(id)}
			}, "channel/ghost/otp"},
	}
}

// messageRefSites are the fields referencing an entry of Document.Messages.
func messageRefSites() []idRefSite {
	return []idRefSite{
		{"message binding messages", "ir/dangling-message-ref", ".Bindings.Message.Messages[0]",
			func(document *ir.Document, id string) {
				messageBinding(document).Messages = []ir.MessageID{ir.MessageID(id)}
			}, "message/ghost/binding"},
		{"reply messages", "ir/dangling-message-ref", ".Bindings.Message.Reply.Messages[0]",
			func(document *ir.Document, id string) {
				reply(document).Messages = []ir.MessageID{ir.MessageID(id)}
			}, "message/ghost/reply"},
		{"channel messages", "ir/dangling-message-ref", ".Messages[0]",
			func(document *ir.Document, id string) {
				putChannel(document, func(channel *ir.Channel) { channel.Messages = []ir.MessageID{ir.MessageID(id)} })
			}, "message/ghost/channel"},
	}
}

// authRefSites are the fields referencing an entry of Document.Auth.
func authRefSites() []idRefSite {
	return []idRefSite{
		{"scheme use", "ir/dangling-auth-ref", ".Auth[0].Schemes[0].Scheme",
			func(document *ir.Document, id string) {
				service(document).Auth = []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: ir.AuthID(id)}}}}
			}, "auth/ghost/service"},
	}
}

// serverRefSites are the fields referencing an entry of Document.Servers: the
// scopes that name the servers serving them.
func serverRefSites() []idRefSite {
	return []idRefSite{
		{"service servers", "ir/dangling-server-ref", ".Servers[0]",
			func(document *ir.Document, id string) { service(document).Servers = []ir.ServerID{ir.ServerID(id)} },
			"server/ghost/service"},
		{"channel servers", "ir/dangling-server-ref", ".Servers[0]",
			func(document *ir.Document, id string) {
				putChannel(document, func(channel *ir.Channel) { channel.Servers = []ir.ServerID{ir.ServerID(id)} })
			}, "server/ghost/channel"},
		{"operation servers", "ir/dangling-server-ref", ".Operations[0].Servers[0]",
			func(document *ir.Document, id string) {
				firstOperation(document).Servers = []ir.ServerID{ir.ServerID(id)}
			},
			"server/ghost/operation"},
	}
}

// operationRefSites are the fields referencing an operation the document declares.
//
// An operation is declared inside the Service→OperationGroup tree rather than in
// an ID-keyed map on Document, so no map-keyed registry exists for the class and
// every one of these resolved against nothing at all until the operations the
// document declares supplied one (GitHub #50).
func operationRefSites() []idRefSite {
	return []idRefSite{
		{"overload of", "ir/dangling-operation-ref", ".OverloadOf",
			func(document *ir.Document, id string) {
				operation := ir.OperationID(id)
				firstOperation(document).OverloadOf = &operation
			}, "operation/ghost/overload"},
		{"polling operation", "ir/dangling-operation-ref", ".LongRunning.PollingOperation",
			func(document *ir.Document, id string) {
				operation := ir.OperationID(id)
				longRunning(document).PollingOperation = &operation
			}, "operation/ghost/polling"},
		{"final operation", "ir/dangling-operation-ref", ".LongRunning.FinalOperation",
			func(document *ir.Document, id string) {
				operation := ir.OperationID(id)
				longRunning(document).FinalOperation = &operation
			}, "operation/ghost/final"},
		{"callback operations", "ir/dangling-operation-ref", ".Bindings.HTTP[0].Callbacks[0].Operations[0]",
			func(document *ir.Document, id string) {
				firstOperation(document).Bindings.HTTP = []ir.HTTPBinding{{Method: "GET", URITemplate: "/x",
					Callbacks: []ir.Callback{{Expression: "$request.body#/cb", Operations: []ir.OperationID{ir.OperationID(id)}}}}}
			}, "operation/ghost/callback"},
		{"resource lifecycle", "ir/dangling-operation-ref", ".Groups[0].Resource.Lifecycle[read]",
			func(document *ir.Document, id string) {
				resource(document).Lifecycle = map[string]ir.OperationID{"read": ir.OperationID(id)}
			}, "operation/ghost/lifecycle"},
		{"resource instance ops", "ir/dangling-operation-ref", ".Groups[0].Resource.InstanceOperations[0]",
			func(document *ir.Document, id string) {
				resource(document).InstanceOperations = []ir.OperationID{ir.OperationID(id)}
			}, "operation/ghost/instance"},
		{"resource collection ops", "ir/dangling-operation-ref", ".Groups[0].Resource.CollectionOperations[0]",
			func(document *ir.Document, id string) {
				resource(document).CollectionOperations = []ir.OperationID{ir.OperationID(id)}
			}, "operation/ghost/collection"},
	}
}

// serviceRefSites are the fields referencing a service the document declares.
// Document.Services is a slice, so this class has no map registry either.
func serviceRefSites() []idRefSite {
	return []idRefSite{
		{"service extends", "ir/dangling-service-ref", ".Extends[0]",
			func(document *ir.Document, id string) {
				service(document).Extends = []ir.ServiceID{ir.ServiceID(id)}
			}, "service/ghost/base"},
	}
}

// resource returns the operation group's resource block, creating it on first use
// so the three resource cases compose in one document.
func resource(doc *ir.Document) *ir.ResourceInfo {
	grp := &service(doc).Groups[0]
	if grp.Resource == nil {
		grp.Resource = &ir.ResourceInfo{}
	}
	return grp.Resource
}

// idRefSites is the full mutation set.
func idRefSites() []idRefSite {
	sites := append(channelRefSites(), messageRefSites()...)
	sites = append(sites, authRefSites()...)
	sites = append(sites, operationRefSites()...)
	sites = append(sites, serverRefSites()...)
	return append(sites, serviceRefSites()...)
}

// messageBinding returns the operation's message binding, creating it on first
// use so the channel, message-set and reply cases compose in one document.
func messageBinding(doc *ir.Document) *ir.MessageBinding {
	operation := firstOperation(doc)
	if operation.Bindings.Message == nil {
		operation.Bindings.Message = &ir.MessageBinding{Direction: ir.MessageDirectionSend}
	}
	return operation.Bindings.Message
}

// reply returns the message binding's reply block, creating it on first use.
func reply(doc *ir.Document) *ir.Reply {
	binding := messageBinding(doc)
	if binding.Reply == nil {
		binding.Reply = &ir.Reply{}
	}
	return binding.Reply
}

// registryDoc is validDoc plus one resolving entry in each non-type registry, so
// a case can plant either a dangling ID or one that resolves.
func registryDoc() *ir.Document {
	doc := validDoc()
	doc.Channels = map[ir.ChannelID]ir.Channel{"channel/a": {ID: "channel/a"}}
	doc.Messages = map[ir.MessageID]ir.Message{"message/a": {ID: "message/a"}}
	doc.Auth = map[ir.AuthID]ir.AuthScheme{"auth/a": {ID: "auth/a", Kind: ir.AuthKindAPIKey}}
	doc.Servers = map[ir.ServerID]ir.Server{"server/a": {ID: "server/a", URLTemplate: "https://a.example"}}
	return doc
}

// resolving returns the ID in the same registry as dangling that registryDoc
// declares, so the clean half of the mutation proof plants a real reference. The
// operation and service cases resolve against the nodes registryDoc declares
// rather than against a map: "op" and "s" are the IDs the lazily built service
// and its single operation carry.
func resolving(dangling string) string {
	switch {
	case strings.HasPrefix(dangling, "channel/"):
		return "channel/a"
	case strings.HasPrefix(dangling, "message/"):
		return "message/a"
	case strings.HasPrefix(dangling, "auth/"):
		return "auth/a"
	case strings.HasPrefix(dangling, "server/"):
		return "server/a"
	case strings.HasPrefix(dangling, "operation/g"):
		return "op"
	default:
		return "s"
	}
}

// withCode returns the diagnostics carrying the given code.
func withCode(diags []ir.Diagnostic, code string) []ir.Diagnostic {
	out := make([]ir.Diagnostic, 0, len(diags))
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			out = append(out, diagnostic)
		}
	}
	return out
}

// TestValidate_DanglingTypedIDRef plants one dangling reference per field in
// idRefSites and requires Validate to report it, at that field's location.
//
// Two rounds of silence are pinned here. Before checkDanglingRefs, only the auth
// case produced a diagnostic at all — Validate resolved no ChannelID or MessageID
// anywhere. Before references resolved against the identities the document
// declares, the operation and service cases resolved against nothing either,
// because neither class has a map on Document for a registry to be derived from
// (GitHub #50).
func TestValidate_DanglingTypedIDRef(t *testing.T) {
	t.Parallel()
	for _, tc := range idRefSites() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := registryDoc()
			tc.plant(doc, tc.id)
			found := withCode(validated(t, doc), tc.code)
			require.Len(t, found, 1, "exactly the planted reference must dangle")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Contains(t, found[0].Message, tc.id)
			assert.Contains(t, found[0].Provenance.Node, tc.where)
		})
	}
}

// TestValidate_ResolvedTypedIDRefsAreClean is the other half of the proof: the
// same fields filled with references that resolve must report nothing. A check
// that cannot stay silent is no better than one that cannot fire.
func TestValidate_ResolvedTypedIDRefsAreClean(t *testing.T) {
	t.Parallel()
	doc := registryDoc()
	for _, tc := range idRefSites() {
		tc.plant(doc, resolving(tc.id))
	}
	assert.Empty(t, validated(t, doc))
}

// TestValidate_DanglingOperationRefWithNoOperationDeclared drives the case the cases
// above cannot: a document that declares no operation at all. A Smithy resource
// names its lifecycle operations, so the reference exists whether or not any
// ir.Operation does.
//
// Which classes resolve has to come from the IR's shape rather than from the
// declarations a document happens to carry. Read off the declarations, a class
// nothing declares gets no registry, and a site whose class has no registry is
// not an unresolved reference — it is no reference at all, which is the silence
// GitHub #50 is about.
func TestValidate_DanglingOperationRefWithNoOperationDeclared(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{{
		ID: "service/x",
		Groups: []ir.OperationGroup{{
			Resource: &ir.ResourceInfo{InstanceOperations: []ir.OperationID{"operation/ghost/instance"}},
		}},
	}}}

	found := withCode(validated(t, doc), "ir/dangling-operation-ref")
	require.Len(t, found, 1)
	assert.Equal(t, ir.SeverityError, found[0].Severity)
	assert.Contains(t, found[0].Message, "operation/ghost/instance")
}

// sortedIDRefPointers is the location order every run must produce: ascending by
// pointer, across every reference class rather than grouped by class.
//
// It is written out rather than captured from a first run for the reason
// sortedRefPointers is: a captured order proves only that two runs agree, which
// any deterministic order satisfies, including the walk's own.
var sortedIDRefPointers = []string{
	"document.Channels[channel/a].Messages[0]",
	"document.Channels[channel/a].Servers[0]",
	"document.Services[0].Auth[0].Schemes[0].Scheme",
	"document.Services[0].Extends[0]",
	"document.Services[0].Groups[0].Operations[0].Bindings.HTTP[0].Callbacks[0].Operations[0]",
	"document.Services[0].Groups[0].Operations[0].Bindings.Message.Channel",
	"document.Services[0].Groups[0].Operations[0].Bindings.Message.Messages[0]",
	"document.Services[0].Groups[0].Operations[0].Bindings.Message.Reply.Channel",
	"document.Services[0].Groups[0].Operations[0].Bindings.Message.Reply.Messages[0]",
	"document.Services[0].Groups[0].Operations[0].Bindings.OTP.Process",
	"document.Services[0].Groups[0].Operations[0].LongRunning.FinalOperation",
	"document.Services[0].Groups[0].Operations[0].LongRunning.PollingOperation",
	"document.Services[0].Groups[0].Operations[0].OverloadOf",
	"document.Services[0].Groups[0].Operations[0].Servers[0]",
	"document.Services[0].Groups[0].Resource.CollectionOperations[0]",
	"document.Services[0].Groups[0].Resource.InstanceOperations[0]",
	"document.Services[0].Groups[0].Resource.Lifecycle[read]",
	"document.Services[0].Servers[0]",
}

// TestValidate_TypedIDDiagnosticOrderIsDeterministic pins invariant 7 across
// reference classes: the walk reaches channels and messages through maps, whose
// iteration order Go randomizes, so sites are sorted by location before any
// diagnostic is built. The order is the pinned one, and validated holds
// every further run to it.
func TestValidate_TypedIDDiagnosticOrderIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := registryDoc()
	for i, tc := range idRefSites() {
		tc.plant(doc, fmt.Sprintf("%s/%02d", tc.id, i))
	}
	require.Len(t, sortedIDRefPointers, len(idRefSites()), "one pinned location per planted site")
	require.Equal(t, sortedIDRefPointers, pointers(validated(t, doc)))
}

// TestValidate_ARegistryNodesOwnIDIsNoReference pins that a registry node's own
// ID is its declaration and no reference: a node filed under a key other than
// its ID is irverify's finding alone (ir/<noun>-id-mismatch), and Validate says
// nothing at that ID. Each registry is driven from both ends, since the silence
// would pass on a walk that never reached these nodes: a reference held inside
// the same mismatched node to something undeclared must still dangle, at the
// reference. A server holds none, so its row is the silence alone.
func TestValidate_ARegistryNodesOwnIDIsNoReference(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, key string
		mismatch  func(doc *ir.Document, key, id string)
		code, at  string // the dangling reference inside the node, "" for none
	}{
		{"channel", "channel/a", func(document *ir.Document, key, id string) {
			document.Channels[ir.ChannelID(key)] = ir.Channel{ID: ir.ChannelID(id), Servers: []ir.ServerID{"server/none"}}
		}, "ir/dangling-server-ref", "document.Channels[channel/a].Servers[0]"},
		{"message", "message/a", func(document *ir.Document, key, id string) {
			document.Messages[ir.MessageID(key)] = ir.Message{ID: ir.MessageID(id), Headers: &ir.TypeRef{Target: "type/none"}}
		}, "ir/dangling-type-ref", "document.Messages[message/a].Headers.Target"},
		{"auth", "auth/a", func(document *ir.Document, key, id string) {
			document.Auth[ir.AuthID(key)] = ir.AuthScheme{ID: ir.AuthID(id), Kind: ir.AuthKindAPIKey, Credentials: &ir.TypeRef{Target: "type/none"}}
		}, "ir/dangling-type-ref", "document.Auth[auth/a].Credentials.Target"},
		{"server", "server/a", func(document *ir.Document, key, id string) {
			document.Servers[ir.ServerID(key)] = ir.Server{ID: ir.ServerID(id), URLTemplate: "https://a.example"}
		}, "", ""},
		{"type", "type/a", func(document *ir.Document, key, id string) {
			document.Types[ir.TypeID(key)] = &ir.Model{ID: ir.TypeID(id), Base: &ir.TypeRef{Target: "type/none"}}
		}, "ir/dangling-type-ref", "document.Types[type/a].Base.Target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := []string{}
			if tc.code != "" {
				want = []string{tc.at}
			}
			mismatched := registryDoc()
			tc.mismatch(mismatched, tc.key, tc.key[:len(tc.key)-1]+"b")
			got := validated(t, mismatched)
			assert.Equal(t, want, pointers(got), "nothing is said at the node's own ID")
			for _, diagnostic := range got {
				assert.Equal(t, tc.code, diagnostic.Code)
				assert.Equal(t, ir.SeverityError, diagnostic.Severity)
			}

			keyed := registryDoc()
			tc.mismatch(keyed, tc.key, tc.key)
			assert.Equal(t, want, pointers(validated(t, keyed)), "keyed by its ID, the node draws the same")
		})
	}
}

// TestValidate_AMismatchedTypeDrawsOneDanglingReference is the document of a
// model filed under t/x/A whose ID is t/x/B and whose Base names t/x/C: the
// dangling Base is the one finding, and neither the key nor the ID is reported.
func TestValidate_AMismatchedTypeDrawsOneDanglingReference(t *testing.T) {
	t.Parallel()
	build := func(base ir.TypeID) *ir.Document {
		return &ir.Document{Types: ir.TypeRegistry{
			"type/x/A": &ir.Model{ID: "type/x/B", Base: &ir.TypeRef{Target: base}},
			"type/x/C": &ir.Model{ID: "type/x/C"},
		}}
	}
	got := validated(t, build("type/x/D"))
	require.Len(t, got, 1)
	assert.Equal(t, "ir/dangling-type-ref", got[0].Code)
	assert.Equal(t, "document.Types[type/x/A].Base.Target", got[0].Provenance.Node)

	assert.Empty(t, validated(t, build("type/x/C")), "a Base naming a declared type draws none")
}

func TestValidate_DanglingAuthRef(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Services = []ir.Service{{
		ID:   "s",
		Auth: []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: "auth/nope"}}}},
	}}
	diags := validated(t, doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, irtest.DiagCodes(diags), "ir/dangling-auth-ref")
}

// TestValidate_PerOperationAuthOverride covers a per-operation auth override,
// including the empty-scheme skip and a dangling override scheme.
func TestValidate_PerOperationAuthOverride(t *testing.T) {
	t.Parallel()
	operation := ir.Operation{
		ID: "op",
		Auth: []ir.AuthRequirement{{Schemes: []ir.SchemeUse{
			{Scheme: ""},          // empty scheme is skipped
			{Scheme: "auth/nope"}, // dangling override
		}}},
	}
	diags := validated(t, docWithOperation(operation))
	require.NotEmpty(t, diags)
	assert.Contains(t, irtest.DiagCodes(diags), "ir/dangling-auth-ref")
}

// TestCheckDanglingRefs_TruncatedWalkClaimsNoDeclarations holds the one direction
// a declaration-derived registry can be wrong in. The operation the shallow
// reference names is declared, but past the walk cap, so a registry built from
// what the walk reached answers "not declared" for a node that is — and a false
// dangling error is worse than the silence it replaced. The registries Document
// keys maps by are read off the field rather than off the walk and are unaffected.
func TestCheckDanglingRefs_TruncatedWalkClaimsNoDeclarations(t *testing.T) {
	t.Parallel()
	deep, target := irtest.NestedOverloadDoc(ir.MaxWalkDepth)
	diags := danglingRefs(t, deep)
	require.NotEmpty(t, diags)
	assert.Equal(t, "ir/walk-truncated", diags[0].Code)
	for _, diagnostic := range diags {
		assert.NotEqual(t, "ir/dangling-operation-ref", diagnostic.Code,
			"%s is declared past the cap, not undeclared", target)
	}

	// The other half: the same shape within the cap resolves, so the case above
	// is not passing on a walk that collects no reference at all.
	shallow, _ := irtest.NestedOverloadDoc(0)
	assert.Empty(t, danglingRefs(t, shallow))
}

// TestCheckDanglingRefs_NilTypeDefIsNotFollowed pins report-only behaviour on
// a malformed registry: a nil entry is skipped rather than dereferenced, so the
// pass reports what it can instead of panicking.
func TestCheckDanglingRefs_NilTypeDefIsNotFollowed(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"type/nil": nil}}
	assert.Empty(t, danglingRefs(t, doc))
}

// TestPropertyIDSites_KeepOnlyWhatIsNotYetDeclared holds the PropertyID walk to
// settling what it can as it goes. A model's discriminator follows its
// properties, so naming one is settled on the spot; a pagination path sits in a
// service, which the walk reaches before any type, so it is kept until every
// property is read, and then only the ghost dangles. Keeping one costs no
// allocation, as its path is spelled only once it dangles. It is not run in
// parallel: AllocsPerRun measures the process.
func TestPropertyIDSites_KeepOnlyWhatIsNotYetDeclared(t *testing.T) {
	doc := docWithOperation(ir.Operation{ID: "operation/o", Pagination: &ir.Pagination{
		Items: &ir.PropertyPath{Segments: []ir.PropertyID{"property/m/a", "property/ghost"}},
	}})
	model, ok := doc.Types["type/m"].(*ir.Model)
	require.True(t, ok)
	model.Discriminator = &ir.Discriminator{Property: "property/m/a"}

	kept := []string{
		"document.Services[0].Groups[0].Operations[0].Pagination.Items.Segments[0]",
		"document.Services[0].Groups[0].Operations[0].Pagination.Items.Segments[1]",
	}
	assert.Equal(t, kept, pass.KeptPropertyIDs(t, doc))
	assert.Empty(t, irtest.AllocatingVisits(doc, pass.WalkVisitors(t, doc)...),
		"keeping a reference spells no path")
	var dangling []string
	for _, diagnostic := range validated(t, doc) {
		if diagnostic.Code == "ir/dangling-property-ref" {
			dangling = append(dangling, diagnostic.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"document.Services[0].Groups[0].Operations[0].Pagination.Items.Segments[1]"}, dangling)
}

// TestCheckPropertyIDRefs_SettlingAKeptReferenceCostsNothing holds settling a
// PropertyID the walk kept to spelling nothing once it resolves: a pagination
// path sits in a service, which the walk reaches before the property it names.
// It is not run in parallel: AllocsPerRun measures the process.
func TestCheckPropertyIDRefs_SettlingAKeptReferenceCostsNothing(t *testing.T) {
	doc := docWithOperation(ir.Operation{ID: "operation/o", Pagination: &ir.Pagination{
		Items: &ir.PropertyPath{Segments: []ir.PropertyID{"property/m/a"}},
	}})
	require.NotEmpty(t, pass.KeptPropertyIDs(t, doc), "the reference is kept, or nothing is settled")
	settle := pass.PropertyIDRefs(t, doc)
	require.Empty(t, settle(), "the kept reference resolves once every property is read")
	assert.Zero(t, testing.AllocsPerRun(5, func() { _ = settle() }))
}

// danglingRefs runs checkDanglingRefs over what one walk of doc gathers, as
// Validate does.
func danglingRefs(t *testing.T, doc *ir.Document) []ir.Diagnostic {
	t.Helper()
	return pass.DanglingRefs(t, doc)
}
