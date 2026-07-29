package pass_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
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
		{"message headers", "/Headers", func(d *ir.Document, t ir.TypeID) {
			putMessage(d, func(m *ir.Message) { m.Headers = &ir.TypeRef{Target: t} })
		}, "t/ghost/message-headers"},
		{"message correlation root", "/CorrelationID/Root", func(d *ir.Document, t ir.TypeID) {
			putMessage(d, func(m *ir.Message) { m.CorrelationID = &ir.PropPath{Root: &ir.TypeRef{Target: t}} })
		}, "t/ghost/correlation-root"},
		{"channel params", "/Params/0", func(d *ir.Document, t ir.TypeID) {
			putChannel(d, func(c *ir.Channel) {
				c.Params = append(c.Params, ir.Parameter{Name: ir.Naming{Source: "room"}, Type: ir.TypeRef{Target: t}})
			})
		}, "t/ghost/channel-param"},
		{"request stream initial", "/RequestStream/Initial", func(d *ir.Document, t ir.TypeID) {
			firstOp(d).RequestStream = &ir.StreamDetail{Initial: &ir.TypeRef{Target: t}}
		}, "t/ghost/stream-initial"},
		{"response stream events", "/ResponseStream/Events", func(d *ir.Document, t ir.TypeID) {
			firstOp(d).ResponseStream = &ir.StreamDetail{Events: &ir.TypeRef{Target: t}}
		}, "t/ghost/stream-events"},
		{"long-running polling type", "/LongRunning/PollingType", func(d *ir.Document, t ir.TypeID) {
			longRunning(d).PollingType = &ir.TypeRef{Target: t}
		}, "t/ghost/lro-polling"},
		{"long-running final type", "/LongRunning/FinalType", func(d *ir.Document, t ir.TypeID) {
			longRunning(d).FinalType = &ir.TypeRef{Target: t}
		}, "t/ghost/lro-final"},
		{"long-running result path root", "/LongRunning/ResultPath/Root", func(d *ir.Document, t ir.TypeID) {
			longRunning(d).ResultPath = &ir.PropPath{Root: &ir.TypeRef{Target: t}}
		}, "t/ghost/lro-result"},
		{"rpc binding input type", "/Bindings/RPC/InputType", func(d *ir.Document, t ir.TypeID) {
			firstOp(d).Bindings.RPC = &ir.RPCBinding{System: "grpc", InputType: &ir.TypeRef{Target: t}}
		}, "t/ghost/rpc-input"},
	}
}

// payloadAndVersioningSites are the fields reachable from an ordinary HTTP
// document: file bodies, multipart part headers, field arguments, values,
// renames, discriminator defaults, and the versioning timeline.
func payloadAndVersioningSites() []refSite {
	return []refSite{
		{"content file contents", "/File/Contents", func(d *ir.Document, t ir.TypeID) {
			requestContent(d).File = &ir.FileInfo{Contents: &ir.TypeRef{Target: t}}
		}, "t/ghost/file-contents"},
		{"multipart part encoding headers", "/Encoding[", func(d *ir.Document, t ir.TypeID) {
			requestContent(d).Encoding = map[string]ir.PartEncoding{"p/part": {Headers: []ir.Property{{
				ID: "p/hdr", Name: ir.Naming{Source: "X-Meta"}, Type: ir.TypeRef{Target: t},
			}}}}
		}, "t/ghost/part-header"},
		{"property args", "/Args/0", func(d *ir.Document, t ir.TypeID) {
			model(d).Properties[0].Args = []ir.Parameter{
				{Name: ir.Naming{Source: "first"}, Type: ir.TypeRef{Target: t}},
			}
		}, "t/ghost/field-arg"},
		{"property default value ref", "/Default/Ref/Type", func(d *ir.Document, t ir.TypeID) {
			model(d).Properties[0].Default = &ir.Value{
				Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: t, Member: "a"},
			}
		}, "t/ghost/value-ref"},
		{"availability versioned type", "/Availability/TypeChangedFrom/0", func(d *ir.Document, t ir.TypeID) {
			model(d).Properties[0].Availability = &ir.Availability{
				TypeChangedFrom: []ir.VersionedType{{Version: "v1", Type: ir.TypeRef{Target: t}}},
			}
		}, "t/ghost/versioned-type"},
		{"discriminator default", "/Discriminator/Default", func(d *ir.Document, t ir.TypeID) {
			d.Types["t/u"] = &ir.Union{
				TypeCommon:    ir.TypeCommon{ID: "t/u"},
				Discriminator: &ir.Discriminator{PropertyName: "kind", Default: t},
			}
		}, "t/ghost/disc-default"},
		{"service rename key", "/Renames[", func(d *ir.Document, t ir.TypeID) {
			service(d).Renames = map[ir.TypeID]ir.Naming{t: {Source: "Ghost"}}
		}, "t/ghost/rename-key"},
		{"service common errors", "/CommonErrors/0", func(d *ir.Document, t ir.TypeID) {
			service(d).CommonErrors = []ir.ErrorCase{{Type: ir.TypeRef{Target: t}}}
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
			found := danglingDiags(pass.Validate(doc))
			require.Len(t, found, 1, "exactly the planted target must dangle")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Contains(t, found[0].Message, string(tc.target))
			assert.Contains(t, found[0].Provenance.Pointer, tc.where)
		})
	}
}

// pointers returns each diagnostic's provenance pointer, in order.
func pointers(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Provenance.Pointer)
	}
	return out
}

// TestValidate_DiagnosticOrderIsDeterministic pins invariant 7 for the
// reflection walk: it reaches references through maps, whose iteration order Go
// randomizes per range, so the sites must be sorted by location before any
// diagnostic is built.
func TestValidate_DiagnosticOrderIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	for i, tc := range unwalkedRefSites() {
		tc.plant(doc, ir.TypeID(fmt.Sprintf("t/ghost/%02d", i)))
	}
	want := pointers(pass.Validate(doc))
	require.NotEmpty(t, want)
	for range 8 {
		assert.Equal(t, want, pointers(pass.Validate(doc)))
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
	assert.Empty(t, danglingDiags(pass.Validate(doc)))
}
