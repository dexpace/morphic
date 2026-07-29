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
		{"message binding channel", "ir/dangling-channel-ref", "/Bindings/Message/Channel",
			func(d *ir.Document, id string) { messageBinding(d).Channel = ir.ChannelID(id) },
			"chan/ghost/binding"},
		{"reply channel", "ir/dangling-channel-ref", "/Bindings/Message/Reply/Channel",
			func(d *ir.Document, id string) {
				ch := ir.ChannelID(id)
				reply(d).Channel = &ch
			}, "chan/ghost/reply"},
		{"otp binding process", "ir/dangling-channel-ref", "/Bindings/OTP/Process",
			func(d *ir.Document, id string) {
				firstOp(d).Bindings.OTP = &ir.OTPBinding{Behaviour: "gen_server", Kind: "call", Process: ir.ChannelID(id)}
			}, "chan/ghost/otp"},
	}
}

// messageRefSites are the fields referencing an entry of Document.Messages.
func messageRefSites() []idRefSite {
	return []idRefSite{
		{"message binding messages", "ir/dangling-message-ref", "/Bindings/Message/Messages/0",
			func(d *ir.Document, id string) {
				messageBinding(d).Messages = []ir.MessageID{ir.MessageID(id)}
			}, "msg/ghost/binding"},
		{"reply messages", "ir/dangling-message-ref", "/Bindings/Message/Reply/Messages/0",
			func(d *ir.Document, id string) {
				reply(d).Messages = []ir.MessageID{ir.MessageID(id)}
			}, "msg/ghost/reply"},
		{"channel messages", "ir/dangling-message-ref", "/Messages/0",
			func(d *ir.Document, id string) {
				putChannel(d, func(c *ir.Channel) { c.Messages = []ir.MessageID{ir.MessageID(id)} })
			}, "msg/ghost/channel"},
	}
}

// authRefSites are the fields referencing an entry of Document.Auth.
func authRefSites() []idRefSite {
	return []idRefSite{
		{"scheme use", "ir/dangling-auth-ref", "/Auth/0/Schemes/0/Scheme",
			func(d *ir.Document, id string) {
				service(d).Auth = []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: ir.AuthID(id)}}}}
			}, "auth/ghost/service"},
	}
}

// idRefSites is the full mutation set.
func idRefSites() []idRefSite {
	sites := append(channelRefSites(), messageRefSites()...)
	return append(sites, authRefSites()...)
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
// declares, so the clean half of the mutation proof plants a real reference.
func resolving(dangling string) string {
	switch dangling[:4] {
	case "chan":
		return "chan/a"
	case "msg/":
		return "msg/a"
	default:
		return "auth/a"
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

// TestValidate_TypedIDDiagnosticOrderIsDeterministic pins invariant 7 across
// reference classes: the walk reaches channels and messages through maps, whose
// iteration order Go randomizes, so sites are sorted by location before any
// diagnostic is built.
func TestValidate_TypedIDDiagnosticOrderIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := registryDoc()
	for i, tc := range idRefSites() {
		tc.plant(doc, fmt.Sprintf("%s/%02d", tc.id, i))
	}
	want := pointers(pass.Validate(doc))
	require.Len(t, want, len(idRefSites()))
	for range 8 {
		assert.Equal(t, want, pointers(pass.Validate(doc)))
	}
}

// TestValidate_RegistryKeysAndOwnIDsResolve pins the other side of deriving
// registries from Document's shape: a registry key and the node's own ID are both
// ChannelID/MessageID/AuthID-typed, so the walk reaches them, and each must
// resolve against its own entry rather than be reported as dangling.
func TestValidate_RegistryKeysAndOwnIDsResolve(t *testing.T) {
	t.Parallel()
	assert.Empty(t, pass.Validate(registryDoc()))
}

// TestValidate_RegistryKeyMismatchDangles pins the converse: an entry filed under
// a key that disagrees with its own ID leaves that ID resolving to nothing, which
// is a dangling reference and is reported as one.
func TestValidate_RegistryKeyMismatchDangles(t *testing.T) {
	t.Parallel()
	doc := registryDoc()
	doc.Channels["chan/a"] = ir.Channel{ID: "chan/b"}
	found := withCode(pass.Validate(doc), "ir/dangling-channel-ref")
	require.Len(t, found, 1)
	assert.Contains(t, found[0].Message, "chan/b")
}
