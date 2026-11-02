// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"encoding/json/v2"
	"fmt"
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
		{"message headers", ".Headers", func(d *ir.Document, t ir.TypeID) {
			putMessage(d, func(m *ir.Message) { m.Headers = &ir.TypeRef{Target: t} })
		}, "t/ghost/message-headers"},
		{"message correlation root", ".CorrelationID.Root", func(d *ir.Document, t ir.TypeID) {
			putMessage(d, func(m *ir.Message) { m.CorrelationID = &ir.PropPath{Root: &ir.TypeRef{Target: t}} })
		}, "t/ghost/correlation-root"},
		{"channel params", ".Params[0]", func(d *ir.Document, t ir.TypeID) {
			putChannel(d, func(c *ir.Channel) {
				c.Params = append(c.Params, ir.Parameter{Name: ir.Naming{Source: "room"}, Type: ir.TypeRef{Target: t}})
			})
		}, "t/ghost/channel-param"},
		{"request stream initial", ".RequestStream.Initial", func(d *ir.Document, t ir.TypeID) {
			firstOp(d).RequestStream = &ir.StreamDetail{Initial: &ir.TypeRef{Target: t}}
		}, "t/ghost/stream-initial"},
		{"response stream events", ".ResponseStream.Events", func(d *ir.Document, t ir.TypeID) {
			firstOp(d).ResponseStream = &ir.StreamDetail{Events: &ir.TypeRef{Target: t}}
		}, "t/ghost/stream-events"},
		{"long-running polling type", ".LongRunning.PollingType", func(d *ir.Document, t ir.TypeID) {
			longRunning(d).PollingType = &ir.TypeRef{Target: t}
		}, "t/ghost/lro-polling"},
		{"long-running final type", ".LongRunning.FinalType", func(d *ir.Document, t ir.TypeID) {
			longRunning(d).FinalType = &ir.TypeRef{Target: t}
		}, "t/ghost/lro-final"},
		{"long-running result path root", ".LongRunning.ResultPath.Root", func(d *ir.Document, t ir.TypeID) {
			longRunning(d).ResultPath = &ir.PropPath{Root: &ir.TypeRef{Target: t}}
		}, "t/ghost/lro-result"},
		{"rpc binding input type", ".Bindings.RPC.InputType", func(d *ir.Document, t ir.TypeID) {
			firstOp(d).Bindings.RPC = &ir.RPCBinding{System: "grpc", InputType: &ir.TypeRef{Target: t}}
		}, "t/ghost/rpc-input"},
	}
}

// payloadAndVersioningSites are the fields reachable from an ordinary HTTP
// document: file bodies, multipart part headers, field arguments, values,
// renames, discriminator defaults, and the versioning timeline.
func payloadAndVersioningSites() []refSite {
	return []refSite{
		{"content file contents", ".File.Contents", func(d *ir.Document, t ir.TypeID) {
			requestContent(d).File = &ir.FileInfo{Contents: &ir.TypeRef{Target: t}}
		}, "t/ghost/file-contents"},
		{"multipart part encoding headers", ".Encoding[", func(d *ir.Document, t ir.TypeID) {
			// The key names t/m's own property, so only the header target dangles:
			// an unknown key is its own defect (checkEncodingKeys).
			requestContent(d).Encoding = map[ir.PropID]ir.PartEncoding{"p/m/a": {Headers: []ir.Header{{
				ID: "p/hdr", Name: ir.Naming{Source: "X-Meta"}, Type: ir.TypeRef{Target: t},
			}}}}
		}, "t/ghost/part-header"},
		{"property args", ".Args[0]", func(d *ir.Document, t ir.TypeID) {
			model(d).Properties[0].Args = []ir.Parameter{
				{Name: ir.Naming{Source: "first"}, Type: ir.TypeRef{Target: t}},
			}
		}, "t/ghost/field-arg"},
		{"property default value ref", ".Default.Ref.Type", func(d *ir.Document, t ir.TypeID) {
			model(d).Properties[0].Default = &ir.Value{
				Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: t, Member: "a"},
			}
		}, "t/ghost/value-ref"},
		{"availability versioned type", ".Availability.TypeChangedFrom[0]", func(d *ir.Document, t ir.TypeID) {
			model(d).Properties[0].Availability = &ir.Availability{
				TypeChangedFrom: []ir.VersionedType{{Version: "v1", Type: ir.TypeRef{Target: t}}},
			}
		}, "t/ghost/versioned-type"},
		{"discriminator default", ".Discriminator.Default", func(d *ir.Document, t ir.TypeID) {
			d.Types["t/u"] = &ir.Union{
				ID:            "t/u",
				Discriminator: &ir.Discriminator{PropertyName: "kind", Default: t},
			}
		}, "t/ghost/disc-default"},
		{"service rename key", ".Renames[", func(d *ir.Document, t ir.TypeID) {
			service(d).Renames = map[ir.TypeID]ir.Naming{t: {Source: "Ghost"}}
		}, "t/ghost/rename-key"},
		{"service common errors", ".CommonErrors[0]", func(d *ir.Document, t ir.TypeID) {
			service(d).CommonErrors = []ir.ErrorCase{{
				Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: t}}}},
			}}
		}, "t/ghost/common-error"},
	}
}

// unwalkedRefSites is the full mutation set.
func unwalkedRefSites() []refSite {
	return append(eventAndBindingSites(), payloadAndVersioningSites()...)
}

// model returns validDoc's model so a case can plant into its first property.
func model(doc *ir.Document) *ir.Model {
	return doc.Types["t/m"].(*ir.Model)
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

// firstOp returns the document's single operation, creating it on first use.
func firstOp(doc *ir.Document) *ir.Operation {
	return &service(doc).Groups[0].Operations[0]
}

// longRunning returns the operation's long-running block, creating it on first
// use so the three long-running cases compose in one document.
func longRunning(doc *ir.Document) *ir.LongRunning {
	op := firstOp(doc)
	if op.LongRunning == nil {
		op.LongRunning = &ir.LongRunning{FinalStateVia: "status-monitor"}
	}
	return op.LongRunning
}

// requestContent returns the operation's single request content, creating it
// with a resolving type so only the planted target can dangle.
func requestContent(doc *ir.Document) *ir.Content {
	op := firstOp(doc)
	if op.Request == nil {
		op.Request = &ir.Payload{Contents: []ir.Content{{
			MediaType: "multipart/form-data", Type: ir.TypeRef{Target: "t/m"},
		}}}
	}
	return &op.Request.Contents[0]
}

// putMessage read-modify-writes the document's single message; Messages holds
// values, so a case cannot take a pointer into it.
func putMessage(doc *ir.Document, mutate func(*ir.Message)) {
	msg := doc.Messages["msg/a"]
	msg.ID = "msg/a"
	mutate(&msg)
	if doc.Messages == nil {
		doc.Messages = map[ir.MessageID]ir.Message{}
	}
	doc.Messages["msg/a"] = msg
}

// putChannel read-modify-writes the document's single channel.
func putChannel(doc *ir.Document, mutate func(*ir.Channel)) {
	ch := doc.Channels["chan/a"]
	ch.ID = "chan/a"
	mutate(&ch)
	if doc.Channels == nil {
		doc.Channels = map[ir.ChannelID]ir.Channel{}
	}
	doc.Channels["chan/a"] = ch
}

// danglingDiags returns the dangling-type-ref diagnostics among diags.
func danglingDiags(diags []ir.Diagnostic) []ir.Diagnostic {
	out := make([]ir.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if d.Code == "ir/dangling-type-ref" {
			out = append(out, d)
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

// aliasRuns is how many times TestValidate_AliasedPointerIsDeterministic repeats
// the same call. Go randomizes map iteration per range statement, so a two-entry
// registry disagrees with itself within a few runs; the flaw this pins split
// 443/57 across two outputs over this many.
const aliasRuns = 500

// aliasedDoc returns a document whose two registry entries share one *TypeRef.
// That is the shape the reference walk prunes: the shared pointer is descended
// into at whichever entry the walk reaches first, so the surviving diagnostic
// names t/a's Base on one run and t/b's on the next unless the walk's order is
// fixed. The shared target dangles so the walk has something to report.
func aliasedDoc() *ir.Document {
	shared := &ir.TypeRef{Target: "t/ghost/aliased"}
	a := &ir.Model{ID: "t/a", Base: shared}
	b := &ir.Model{ID: "t/b", Base: shared}
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
// on any walk whose map iteration is left to Go.
func TestValidate_AliasedPointerIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := aliasedDoc()
	want := diagJSON(t, validated(t, doc))
	require.Contains(t, want, "t/ghost/aliased", "the shared target must dangle, or nothing is being compared")

	for i := range aliasRuns {
		require.Equal(t, want, diagJSON(t, validated(t, doc)), "run %d disagrees with the first", i)
	}
}

// pointers returns each diagnostic's IR-space location, in order.
func pointers(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Provenance.Node)
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
	"doc.Channels[chan/a].Params[0].Type.Target",
	"doc.Messages[msg/a].CorrelationID.Root.Target",
	"doc.Messages[msg/a].Headers.Target",
	"doc.Services[0].CommonErrors[0].Payload.Contents[0].Type.Target",
	"doc.Services[0].Groups[0].Operations[0].Bindings.RPC.InputType.Target",
	"doc.Services[0].Groups[0].Operations[0].LongRunning.FinalType.Target",
	"doc.Services[0].Groups[0].Operations[0].LongRunning.PollingType.Target",
	"doc.Services[0].Groups[0].Operations[0].LongRunning.ResultPath.Root.Target",
	"doc.Services[0].Groups[0].Operations[0].Request.Contents[0].Encoding[p/m/a].Headers[0].Type.Target",
	"doc.Services[0].Groups[0].Operations[0].Request.Contents[0].File.Contents.Target",
	"doc.Services[0].Groups[0].Operations[0].RequestStream.Initial.Target",
	"doc.Services[0].Groups[0].Operations[0].ResponseStream.Events.Target",
	"doc.Services[0].Renames[t/ghost/15].key",
	"doc.Types[t/m].Properties[0].Args[0].Type.Target",
	"doc.Types[t/m].Properties[0].Availability.TypeChangedFrom[0].Type.Target",
	"doc.Types[t/m].Properties[0].Default.Ref.Type",
	"doc.Types[t/u].Discriminator.Default",
}

// TestValidate_DiagnosticOrderIsDeterministic pins invariant 7 for the
// reflection walk: it reaches references through maps, whose iteration order Go
// randomizes per range, so the sites must be sorted by location before any
// diagnostic is built. Both halves are asserted — the order is the pinned one,
// and every further run repeats it.
func TestValidate_DiagnosticOrderIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	for i, tc := range unwalkedRefSites() {
		tc.plant(doc, ir.TypeID(fmt.Sprintf("t/ghost/%02d", i)))
	}
	require.Equal(t, sortedRefPointers, pointers(danglingDiags(validated(t, doc))))
	for range 8 {
		assert.Equal(t, sortedRefPointers, pointers(danglingDiags(validated(t, doc))))
	}
}

// TestValidate_ResolvedRefsInEveryFieldAreClean is the other half of the
// mutation proof: with every one of those fields populated by a target that
// does resolve, Validate reports no dangling reference. A check that cannot
// stay silent is no better than one that cannot fire.
func TestValidate_ResolvedRefsInEveryFieldAreClean(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	for _, tc := range unwalkedRefSites() {
		tc.plant(doc, "t/prim/string")
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
		tc.plant(doc, ir.TypeID(fmt.Sprintf("t/ghost/%02d", i)))
	}

	var got []string
	for _, v := range irverify.Verify(doc) {
		if v.Code == "ir/dangling-type-ref" {
			got = append(got, v.Path)
		}
	}
	require.Equal(t, sortedRefPointers, got, "irverify must name the same locations")
	assert.Equal(t, got, pointers(danglingDiags(validated(t, doc))))
}

func TestValidate_DanglingTypeRef(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	m := doc.Types["t/m"].(*ir.Model)
	m.Properties[0].Type.Target = "t/nowhere"
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
	m := doc.Types["t/m"].(*ir.Model)
	m.Docs = ir.Docs{Description: "{t:t/nowhere} reads Pet", Refs: []ir.DocRef{{StartByte: 20, EndByte: 23, Type: "t/pet"}}}
	found := withCode(validated(t, doc), "ir/dangling-type-ref")
	require.Len(t, found, 1, "the token-shaped text is not a reference, and the span is")
	assert.Contains(t, found[0].Message, "t/pet")
	assert.Equal(t, "doc.Types[t/m].Docs.Refs[0].Type", found[0].Provenance.Node)
}

// TestValidate_DanglingRefInMessagePayload exercises the Messages walk in
// checkDanglingRefs: a message whose payload content references a missing type
// must be reported against the message location.
func TestValidate_DanglingRefInMessagePayload(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Messages = map[ir.MessageID]ir.Message{
		"msg/a": {
			ID: "msg/a",
			Payload: ir.Payload{Contents: []ir.Content{
				{MediaType: "application/json", Type: ir.TypeRef{Target: "t/ghost"}},
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
	doc.Types["t/scalar"] = &ir.Scalar{
		ID:       "t/scalar",
		Base:     &ir.TypeRef{Target: "t/ghost-base"},
		Encoding: &ir.Encoding{Name: "x", WireType: &ir.TypeRef{Target: "t/ghost-wire"}},
	}
	doc.Types["t/list"] = &ir.List{
		ID:       "t/list",
		Elem:     ir.TypeRef{Target: "t/ghost-elem"},
		Encoding: &ir.Encoding{Name: "packed", WireType: &ir.TypeRef{Target: "t/ghost-lwire"}},
	}
	doc.Types["t/map"] = &ir.MapT{
		ID:    "t/map",
		Key:   &ir.TypeRef{Target: "t/ghost-key"},
		Value: ir.TypeRef{Target: "t/ghost-val"},
	}
	doc.Types["t/tuple"] = &ir.Tuple{
		ID:    "t/tuple",
		Elems: []ir.TypeRef{{Target: "t/ghost-e0"}},
		Rest:  &ir.TypeRef{Target: "t/ghost-rest"},
	}
	diags := validated(t, doc)
	// Each dangling target above yields exactly one dangling-type-ref diagnostic.
	assert.Equal(t, 8, countCode(t, diags, "ir/dangling-type-ref"))
}

// TestValidate_ModelImplementsAndAdditionalPropsWalked covers a model's
// Implements references and the Value, Key and Patterns references its
// AdditionalProps carries.
func TestValidate_ModelImplementsAndAdditionalPropsWalked(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	m := doc.Types["t/m"].(*ir.Model)
	m.Implements = []ir.TypeRef{{Target: "t/ghost-iface"}}
	m.AdditionalProps = &ir.AdditionalProps{
		Value: ir.TypeRef{Target: "t/ghost-apval"},
		Key:   &ir.TypeRef{Target: "t/ghost-apkey"},
		Patterns: []ir.PatternProps{
			{Pattern: "^x", Value: ir.TypeRef{Target: "t/ghost-appat"}},
		},
	}
	diags := validated(t, doc)
	assert.Equal(t, 4, countCode(t, diags, "ir/dangling-type-ref"))
}

// TestValidate_OperationHeadersAndItemWalked covers an operation's response
// headers and the per-media-type Item a content declares.
func TestValidate_OperationHeadersAndItemWalked(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID: "op",
		Request: &ir.Payload{Contents: []ir.Content{{
			MediaType: "application/json",
			Type:      ir.TypeRef{Target: "t/prim/string"},
			Item:      &ir.TypeRef{Target: "t/ghost-item"},
		}}},
		Responses: []ir.Response{{
			Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "t/prim/string"}}}},
			Headers: []ir.Header{{
				ID: "p/h", Name: ir.Naming{Source: "X-Trace"}, WireName: "X-Trace",
				Type: ir.TypeRef{Target: "t/ghost-hdr"},
			}},
		}},
		Errors: []ir.ErrorCase{{
			Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "t/ghost-err"}}}},
			// The error side's headers, which nothing reached until now: skipping
			// ErrorCase.Headers in the reflection walk left the suite green, where
			// skipping the Response.Headers above reddens this test.
			Headers: []ir.Header{{
				ID: "p/eh", Name: ir.Naming{Source: "X-Retry"}, WireName: "X-Retry",
				Type: ir.TypeRef{Target: "t/ghost-err-hdr"},
			}},
		}},
	}
	diags := validated(t, docWithOperation(op))
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
			func(d *ir.Document, id string) { messageBinding(d).Channel = ir.ChannelID(id) },
			"chan/ghost/binding"},
		{"reply channel", "ir/dangling-channel-ref", ".Bindings.Message.Reply.Channel",
			func(d *ir.Document, id string) {
				ch := ir.ChannelID(id)
				reply(d).Channel = &ch
			}, "chan/ghost/reply"},
		{"otp binding process", "ir/dangling-channel-ref", ".Bindings.OTP.Process",
			func(d *ir.Document, id string) {
				firstOp(d).Bindings.OTP = &ir.OTPBinding{Behaviour: "gen_server", Kind: "call", Process: ir.ChannelID(id)}
			}, "chan/ghost/otp"},
	}
}

// messageRefSites are the fields referencing an entry of Document.Messages.
func messageRefSites() []idRefSite {
	return []idRefSite{
		{"message binding messages", "ir/dangling-message-ref", ".Bindings.Message.Messages[0]",
			func(d *ir.Document, id string) {
				messageBinding(d).Messages = []ir.MessageID{ir.MessageID(id)}
			}, "msg/ghost/binding"},
		{"reply messages", "ir/dangling-message-ref", ".Bindings.Message.Reply.Messages[0]",
			func(d *ir.Document, id string) {
				reply(d).Messages = []ir.MessageID{ir.MessageID(id)}
			}, "msg/ghost/reply"},
		{"channel messages", "ir/dangling-message-ref", ".Messages[0]",
			func(d *ir.Document, id string) {
				putChannel(d, func(c *ir.Channel) { c.Messages = []ir.MessageID{ir.MessageID(id)} })
			}, "msg/ghost/channel"},
	}
}

// authRefSites are the fields referencing an entry of Document.Auth.
func authRefSites() []idRefSite {
	return []idRefSite{
		{"scheme use", "ir/dangling-auth-ref", ".Auth[0].Schemes[0].Scheme",
			func(d *ir.Document, id string) {
				service(d).Auth = []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: ir.AuthID(id)}}}}
			}, "auth/ghost/service"},
	}
}

// serverRefSites are the fields referencing an entry of Document.Servers: the
// scopes that name the servers serving them.
func serverRefSites() []idRefSite {
	return []idRefSite{
		{"service servers", "ir/dangling-server-ref", ".Servers[0]",
			func(d *ir.Document, id string) { service(d).Servers = []ir.ServerID{ir.ServerID(id)} },
			"server/ghost/service"},
		{"channel servers", "ir/dangling-server-ref", ".Servers[0]",
			func(d *ir.Document, id string) {
				putChannel(d, func(c *ir.Channel) { c.Servers = []ir.ServerID{ir.ServerID(id)} })
			}, "server/ghost/channel"},
		{"operation servers", "ir/dangling-server-ref", ".Operations[0].Servers[0]",
			func(d *ir.Document, id string) { firstOp(d).Servers = []ir.ServerID{ir.ServerID(id)} },
			"server/ghost/operation"},
	}
}

// opRefSites are the fields referencing an operation the document declares.
//
// An operation is declared inside the Service→OperationGroup tree rather than in
// an ID-keyed map on Document, so no map-keyed registry exists for the class and
// every one of these resolved against nothing at all until the operations the
// document declares supplied one (GitHub #50).
func opRefSites() []idRefSite {
	return []idRefSite{
		{"overload of", "ir/dangling-op-ref", ".OverloadOf",
			func(d *ir.Document, id string) {
				op := ir.OpID(id)
				firstOp(d).OverloadOf = &op
			}, "op/ghost/overload"},
		{"polling operation", "ir/dangling-op-ref", ".LongRunning.PollingOperation",
			func(d *ir.Document, id string) {
				op := ir.OpID(id)
				longRunning(d).PollingOperation = &op
			}, "op/ghost/polling"},
		{"final operation", "ir/dangling-op-ref", ".LongRunning.FinalOperation",
			func(d *ir.Document, id string) {
				op := ir.OpID(id)
				longRunning(d).FinalOperation = &op
			}, "op/ghost/final"},
		{"callback operations", "ir/dangling-op-ref", ".Bindings.HTTP[0].Callbacks[0].Operations[0]",
			func(d *ir.Document, id string) {
				firstOp(d).Bindings.HTTP = []ir.HTTPBinding{{Method: "GET", URITemplate: "/x",
					Callbacks: []ir.Callback{{Expression: "$request.body#/cb", Operations: []ir.OpID{ir.OpID(id)}}}}}
			}, "op/ghost/callback"},
		{"resource lifecycle", "ir/dangling-op-ref", ".Groups[0].Resource.Lifecycle[read]",
			func(d *ir.Document, id string) {
				resource(d).Lifecycle = map[string]ir.OpID{"read": ir.OpID(id)}
			}, "op/ghost/lifecycle"},
		{"resource instance ops", "ir/dangling-op-ref", ".Groups[0].Resource.InstanceOps[0]",
			func(d *ir.Document, id string) {
				resource(d).InstanceOps = []ir.OpID{ir.OpID(id)}
			}, "op/ghost/instance"},
		{"resource collection ops", "ir/dangling-op-ref", ".Groups[0].Resource.CollectionOps[0]",
			func(d *ir.Document, id string) {
				resource(d).CollectionOps = []ir.OpID{ir.OpID(id)}
			}, "op/ghost/collection"},
	}
}

// serviceRefSites are the fields referencing a service the document declares.
// Document.Services is a slice, so this class has no map registry either.
func serviceRefSites() []idRefSite {
	return []idRefSite{
		{"service extends", "ir/dangling-service-ref", ".Extends[0]",
			func(d *ir.Document, id string) {
				service(d).Extends = []ir.ServiceID{ir.ServiceID(id)}
			}, "s/ghost/base"},
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
	sites = append(sites, opRefSites()...)
	sites = append(sites, serverRefSites()...)
	return append(sites, serviceRefSites()...)
}

// messageBinding returns the operation's message binding, creating it on first
// use so the channel, message-set and reply cases compose in one document.
func messageBinding(doc *ir.Document) *ir.MessageBinding {
	op := firstOp(doc)
	if op.Bindings.Message == nil {
		op.Bindings.Message = &ir.MessageBinding{Direction: ir.MsgDirectionSend}
	}
	return op.Bindings.Message
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
	doc.Channels = map[ir.ChannelID]ir.Channel{"chan/a": {ID: "chan/a"}}
	doc.Messages = map[ir.MessageID]ir.Message{"msg/a": {ID: "msg/a"}}
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
	switch dangling[:4] {
	case "chan":
		return "chan/a"
	case "msg/":
		return "msg/a"
	case "auth":
		return "auth/a"
	case "serv":
		return "server/a"
	case "op/g":
		return "op"
	default:
		return "s"
	}
}

// withCode returns the diagnostics carrying the given code.
func withCode(diags []ir.Diagnostic, code string) []ir.Diagnostic {
	out := make([]ir.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d)
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

// TestValidate_DanglingOpRefWithNoOperationDeclared drives the case the cases
// above cannot: a document that declares no operation at all. A Smithy resource
// names its lifecycle operations, so the reference exists whether or not any
// ir.Operation does.
//
// Which classes resolve has to come from the IR's shape rather than from the
// declarations a document happens to carry. Read off the declarations, a class
// nothing declares gets no registry, and a site whose class has no registry is
// not an unresolved reference — it is no reference at all, which is the silence
// GitHub #50 is about.
func TestValidate_DanglingOpRefWithNoOperationDeclared(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{{
			Resource: &ir.ResourceInfo{InstanceOps: []ir.OpID{"op/ghost/instance"}},
		}},
	}}}

	found := withCode(validated(t, doc), "ir/dangling-op-ref")
	require.Len(t, found, 1)
	assert.Equal(t, ir.SeverityError, found[0].Severity)
	assert.Contains(t, found[0].Message, "op/ghost/instance")
}

// sortedIDRefPointers is the location order every run must produce: ascending by
// pointer, across every reference class rather than grouped by class.
//
// It is written out rather than captured from a first run for the reason
// sortedRefPointers is: a captured order proves only that two runs agree, which
// any deterministic order satisfies, including the walk's own.
var sortedIDRefPointers = []string{
	"doc.Channels[chan/a].Messages[0]",
	"doc.Channels[chan/a].Servers[0]",
	"doc.Services[0].Auth[0].Schemes[0].Scheme",
	"doc.Services[0].Extends[0]",
	"doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[0].Callbacks[0].Operations[0]",
	"doc.Services[0].Groups[0].Operations[0].Bindings.Message.Channel",
	"doc.Services[0].Groups[0].Operations[0].Bindings.Message.Messages[0]",
	"doc.Services[0].Groups[0].Operations[0].Bindings.Message.Reply.Channel",
	"doc.Services[0].Groups[0].Operations[0].Bindings.Message.Reply.Messages[0]",
	"doc.Services[0].Groups[0].Operations[0].Bindings.OTP.Process",
	"doc.Services[0].Groups[0].Operations[0].LongRunning.FinalOperation",
	"doc.Services[0].Groups[0].Operations[0].LongRunning.PollingOperation",
	"doc.Services[0].Groups[0].Operations[0].OverloadOf",
	"doc.Services[0].Groups[0].Operations[0].Servers[0]",
	"doc.Services[0].Groups[0].Resource.CollectionOps[0]",
	"doc.Services[0].Groups[0].Resource.InstanceOps[0]",
	"doc.Services[0].Groups[0].Resource.Lifecycle[read]",
	"doc.Services[0].Servers[0]",
}

// TestValidate_TypedIDDiagnosticOrderIsDeterministic pins invariant 7 across
// reference classes: the walk reaches channels and messages through maps, whose
// iteration order Go randomizes, so sites are sorted by location before any
// diagnostic is built. Both halves are asserted — the order is the pinned one,
// and every further run repeats it.
func TestValidate_TypedIDDiagnosticOrderIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := registryDoc()
	for i, tc := range idRefSites() {
		tc.plant(doc, fmt.Sprintf("%s/%02d", tc.id, i))
	}
	require.Len(t, sortedIDRefPointers, len(idRefSites()), "one pinned location per planted site")
	require.Equal(t, sortedIDRefPointers, pointers(validated(t, doc)))
	for range 8 {
		assert.Equal(t, sortedIDRefPointers, pointers(validated(t, doc)))
	}
}

// TestValidate_RegistryOwnIDsResolveAndMismatchesDangle pins the other side of
// deriving registries from Document's shape: a node's own ID is typed like any
// cross-reference, so it must resolve against its own entry rather than dangle.
//
// Each registry is driven from both ends, since the silent half would pass on a
// walk that never reached these nodes: an entry filed under a key that
// disagrees with its own ID leaves that ID resolving to nothing, which must be
// reported. Keys cannot be driven so, as a key resolves to its own entry by
// construction; the Service.Renames case in payloadAndVersioningSites pins
// that keys are reached.
func TestValidate_RegistryOwnIDsResolveAndMismatchesDangle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		code     string
		orphanID string
		mismatch func(doc *ir.Document)
	}{
		{"channel", "ir/dangling-channel-ref", "chan/b", func(d *ir.Document) {
			d.Channels["chan/a"] = ir.Channel{ID: "chan/b"}
		}},
		{"message", "ir/dangling-message-ref", "msg/b", func(d *ir.Document) {
			d.Messages["msg/a"] = ir.Message{ID: "msg/b"}
		}},
		{"auth", "ir/dangling-auth-ref", "auth/b", func(d *ir.Document) {
			d.Auth["auth/a"] = ir.AuthScheme{ID: "auth/b", Kind: ir.AuthKindAPIKey}
		}},
		{"server", "ir/dangling-server-ref", "server/b", func(d *ir.Document) {
			d.Servers["server/a"] = ir.Server{ID: "server/b", URLTemplate: "https://a.example"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Empty(t, validated(t, registryDoc()), "a consistent registry reports nothing")

			doc := registryDoc()
			tc.mismatch(doc)
			found := withCode(validated(t, doc), tc.code)
			require.Len(t, found, 1, "the orphaned own ID must be the only dangling reference")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Contains(t, found[0].Message, tc.orphanID)
		})
	}
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
	op := ir.Operation{
		ID: "op",
		Auth: []ir.AuthRequirement{{Schemes: []ir.SchemeUse{
			{Scheme: ""},          // empty scheme is skipped
			{Scheme: "auth/nope"}, // dangling override
		}}},
	}
	diags := validated(t, docWithOperation(op))
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
	for _, d := range diags {
		assert.NotEqual(t, "ir/dangling-op-ref", d.Code,
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
	doc := &ir.Document{Types: ir.TypeRegistry{"t/nil": nil}}
	assert.Empty(t, danglingRefs(t, doc))
}

// TestPropIDSites_KeepOnlyWhatIsNotYetDeclared holds the PropID walk to
// settling what it can as it goes. A model's discriminator follows its
// properties, so naming one is settled on the spot; a pagination path sits in a
// service, which the walk reaches before any type, so it is kept until every
// property is read, and then only the ghost dangles. Keeping one costs no
// allocation, as its path is spelled only once it dangles. It is not run in
// parallel: AllocsPerRun measures the process.
func TestPropIDSites_KeepOnlyWhatIsNotYetDeclared(t *testing.T) {
	doc := docWithOperation(ir.Operation{ID: "op/o", Pagination: &ir.Pagination{
		Items: &ir.PropPath{Segments: []ir.PropID{"p/m/a", "p/ghost"}},
	}})
	model, ok := doc.Types["t/m"].(*ir.Model)
	require.True(t, ok)
	model.Discriminator = &ir.Discriminator{Property: "p/m/a"}

	kept := []string{
		"doc.Services[0].Groups[0].Operations[0].Pagination.Items.Segments[0]",
		"doc.Services[0].Groups[0].Operations[0].Pagination.Items.Segments[1]",
	}
	assert.Equal(t, kept, pass.KeptPropIDs(t, doc))
	assert.Empty(t, irtest.AllocatingVisits(doc, pass.WalkVisitors(t, doc)...),
		"keeping a reference spells no path")
	var dangling []string
	for _, d := range validated(t, doc) {
		if d.Code == "ir/dangling-prop-ref" {
			dangling = append(dangling, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"doc.Services[0].Groups[0].Operations[0].Pagination.Items.Segments[1]"}, dangling)
}

// TestCheckPropIDRefs_SettlingAKeptReferenceCostsNothing holds settling a
// PropID the walk kept to spelling nothing once it resolves: a pagination path
// sits in a service, which the walk reaches before the property it names. It
// is not run in parallel: AllocsPerRun measures the process.
func TestCheckPropIDRefs_SettlingAKeptReferenceCostsNothing(t *testing.T) {
	doc := docWithOperation(ir.Operation{ID: "op/o", Pagination: &ir.Pagination{
		Items: &ir.PropPath{Segments: []ir.PropID{"p/m/a"}},
	}})
	require.NotEmpty(t, pass.KeptPropIDs(t, doc), "the reference is kept, or nothing is settled")
	settle := pass.PropIDRefs(t, doc)
	require.Empty(t, settle(), "the kept reference resolves once every property is read")
	assert.Zero(t, testing.AllocsPerRun(5, func() { _ = settle() }))
}

// danglingRefs runs checkDanglingRefs over what one walk of doc gathers, as
// Validate does.
func danglingRefs(t *testing.T, doc *ir.Document) []ir.Diagnostic {
	t.Helper()
	return pass.DanglingRefs(t, doc)
}
