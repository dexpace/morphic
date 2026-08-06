package pass_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// idRefSite is one field carrying a typed-ID reference other than an ir.TypeID —
// the class Validate resolved nowhere before checkDanglingRefs.
//
// The set below is derived from the ir package's own declarations: every field
// whose type mentions ChannelID, MessageID or AuthID. Fields holding a node's own
// ID (Channel.ID, Message.ID, AuthScheme.ID) and registry keys resolve against
// their own entry by construction, so only the cross-references are listed.
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

// opRefSites are the fields referencing an operation the document declares.
//
// An operation is declared inside the Service→OperationGroup tree rather than in
// an ID-keyed map on Document, so ir.DocumentRegistries derives no registry for
// the class and every one of these resolved against nothing at all until
// ir.Registries.WithDeclarations supplied one (GitHub #50).
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

// TestValidate_DanglingTypedIDRef plants one dangling channel, message or auth
// reference per field that carries one and requires Validate to report it, at
// that field's location. Before checkDanglingRefs, only the auth case produced a
// diagnostic at all — Validate resolved no ChannelID or MessageID anywhere.
func TestValidate_DanglingTypedIDRef(t *testing.T) {
	t.Parallel()
	for _, tc := range idRefSites() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := registryDoc()
			tc.plant(doc, tc.id)
			found := withCode(pass.Validate(doc), tc.code)
			require.Len(t, found, 1, "exactly the planted reference must dangle")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Contains(t, found[0].Message, tc.id)
			assert.Contains(t, found[0].Provenance.Pointer, tc.where)
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
	assert.Empty(t, pass.Validate(doc))
}

// sortedIDRefPointers is the location order every run must produce: ascending by
// pointer, across all three reference classes rather than grouped by class.
//
// It is written out rather than captured from a first run for the reason
// sortedRefPointers is (validate_refs_test.go): a captured order proves only
// that two runs agree, which any deterministic order satisfies, including the
// walk's own.
var sortedIDRefPointers = []string{
	"doc.Channels[chan/a].Messages[0]",
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
	"doc.Services[0].Groups[0].Resource.CollectionOps[0]",
	"doc.Services[0].Groups[0].Resource.InstanceOps[0]",
	"doc.Services[0].Groups[0].Resource.Lifecycle[read]",
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
	require.Equal(t, sortedIDRefPointers, pointers(pass.Validate(doc)))
	for range 8 {
		assert.Equal(t, sortedIDRefPointers, pointers(pass.Validate(doc)))
	}
}

// TestValidate_RegistryOwnIDsResolveAndMismatchesDangle pins the other side of
// deriving registries from Document's shape. A node's own ID is
// ChannelID/MessageID/AuthID-typed like any cross-reference, so the walk reaches
// it and it must resolve against its own entry rather than be reported as
// dangling.
//
// Each registry is driven from both ends, because the silent half alone would
// pass just as well on a walk that never reached these nodes at all: filing an
// entry under a key that disagrees with its own ID leaves that ID resolving to
// nothing, and that must be reported. Registry keys are the other half and
// cannot be driven this way — a key resolves to its own entry by construction,
// so no key can be made to dangle. That the walk reaches map keys at all is
// pinned by the Service.Renames case in validate_refs_test.go, where the key is
// the reference and the value is not.
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Empty(t, pass.Validate(registryDoc()), "a consistent registry reports nothing")

			doc := registryDoc()
			tc.mismatch(doc)
			found := withCode(pass.Validate(doc), tc.code)
			require.Len(t, found, 1, "the orphaned own ID must be the only dangling reference")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Contains(t, found[0].Message, tc.orphanID)
		})
	}
}
