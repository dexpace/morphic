package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// messageBindingDoc returns a document whose channel chan/a carries carried and
// whose single operation binds channel using used. Both message ids are
// registered, so nothing but the containment claim can report on them.
func messageBindingDoc(channel ir.ChannelID, carried, used []ir.MessageID) *ir.Document {
	doc := validDoc()
	doc.Messages = map[ir.MessageID]ir.Message{
		"msg/a": {ID: "msg/a", Name: ir.Naming{Source: "a"}},
		"msg/b": {ID: "msg/b", Name: ir.Naming{Source: "b"}},
	}
	putChannel(doc, func(c *ir.Channel) { c.Messages = carried })
	firstOp(doc).Bindings.Message = &ir.MessageBinding{Channel: channel, Messages: used}
	return doc
}

// TestValidate_MessageNotCarriedByChannel is the containment claim ir-design §8.3
// makes for MessageBinding.Messages: an operation may use only the messages its
// channel carries. Both ids resolve, so the reference walk is silent and a
// publisher for a message the channel contract forbids validated clean.
func TestValidate_MessageNotCarriedByChannel(t *testing.T) {
	t.Parallel()
	diags := pass.Validate(messageBindingDoc("chan/a",
		[]ir.MessageID{"msg/a"}, []ir.MessageID{"msg/b"}))

	assert.Equal(t, 0, countCode(t, diags, "ir/dangling-message-ref"),
		"both messages are registered, so the reference walk has nothing to say")
	assert.Contains(t, messageForCode(t, diags, "pass/message-not-in-channel"), "msg/b")
}

// TestValidate_MessagesCarriedByChannelAreClean is the silent half. A binding
// using a subset of the channel's messages, and one naming none at all, both
// report nothing — a check that cannot stay silent is no better than one that
// cannot fire.
func TestValidate_MessagesCarriedByChannelAreClean(t *testing.T) {
	t.Parallel()
	carried := []ir.MessageID{"msg/a", "msg/b"}

	assert.Empty(t, pass.Validate(messageBindingDoc("chan/a", carried, []ir.MessageID{"msg/b"})))
	assert.Empty(t, pass.Validate(messageBindingDoc("chan/a", carried, nil)),
		"a binding naming no message uses nothing the channel forbids")
}

// TestValidate_MessageBindingWithoutAResolvableChannel pins that an unresolvable
// channel claims nothing here. There is no message set to compare against, so
// the containment check has no answer to give and ir/dangling-channel-ref owns
// the defect on its own.
func TestValidate_MessageBindingWithoutAResolvableChannel(t *testing.T) {
	t.Parallel()
	codes := codes(pass.Validate(messageBindingDoc("chan/ghost",
		[]ir.MessageID{"msg/a"}, []ir.MessageID{"msg/b"})))

	assert.Contains(t, codes, "ir/dangling-channel-ref")
	assert.NotContains(t, codes, "pass/message-not-in-channel")
}

// TestValidate_OperationWithoutAMessageBindingIsClean covers the arm every HTTP
// operation takes: no MessageBinding at all, so there is no channel to hold it
// to.
func TestValidate_OperationWithoutAMessageBindingIsClean(t *testing.T) {
	t.Parallel()
	assert.NotContains(t, codes(pass.Validate(docWithOperation(ir.Operation{ID: "op"}))),
		"pass/message-not-in-channel")
}

// replyBindingDoc returns a document whose channel chan/a carries carried and
// whose single operation binds it legally, with a reply on replyChannel using
// replyUsed. A nil replyChannel is the dynamic-address spelling.
func replyBindingDoc(replyChannel *ir.ChannelID, carried, replyUsed []ir.MessageID) *ir.Document {
	doc := messageBindingDoc("chan/a", carried, nil)
	firstOp(doc).Bindings.Message.Reply = &ir.Reply{Channel: replyChannel, Messages: replyUsed}
	return doc
}

// TestValidate_ReplyMessageNotCarriedByChannel holds the reply half of a binding
// to the containment claim its request half already carries. A reply names a
// channel and the payloads travelling back on it, so a reply message the reply
// channel does not carry is as unroutable as a request message the operation's
// channel does not — and every id resolves, so nothing else can catch it.
func TestValidate_ReplyMessageNotCarriedByChannel(t *testing.T) {
	t.Parallel()
	replyCh := ir.ChannelID("chan/a")
	diags := pass.Validate(replyBindingDoc(&replyCh,
		[]ir.MessageID{"msg/a"}, []ir.MessageID{"msg/b"}))

	assert.Equal(t, 0, countCode(t, diags, "ir/dangling-message-ref"),
		"msg/b is registered, so the reference walk has nothing to say")
	msg := messageForCode(t, diags, "pass/message-not-in-channel")
	assert.Contains(t, msg, "msg/b")
	assert.Contains(t, msg, "/bindings/message/reply/messages/0",
		"the reply's own position, not the request set's")
}

// TestValidate_ReplyMessagesCarriedAreClean is the silent half, including the
// reply whose address is dynamic: ir-design §8.3 leaves such a reply's channel
// nil, so there is no declared message set to hold it to and a check that
// reported there would refuse legal IR.
func TestValidate_ReplyMessagesCarriedAreClean(t *testing.T) {
	t.Parallel()
	carried := []ir.MessageID{"msg/a", "msg/b"}
	replyCh := ir.ChannelID("chan/a")

	assert.Empty(t, pass.Validate(replyBindingDoc(&replyCh, carried, []ir.MessageID{"msg/b"})))
	assert.Empty(t, pass.Validate(replyBindingDoc(nil, carried, []ir.MessageID{"msg/b"})),
		"a reply with a dynamic address declares no channel to be a subset of")
}

// replyOnItsOwnChannelDoc returns the shape a request-reply pair normally takes:
// the operation binds chan/a carrying msg/a, and answers on chan/reply carrying
// msg/reply. The two message sets are disjoint, so which channel the reply is
// judged against is observable.
func replyOnItsOwnChannelDoc(replyUsed []ir.MessageID) *ir.Document {
	doc := messageBindingDoc("chan/a", []ir.MessageID{"msg/a"}, []ir.MessageID{"msg/a"})
	doc.Messages["msg/reply"] = ir.Message{ID: "msg/reply", Name: ir.Naming{Source: "reply"}}
	doc.Channels["chan/reply"] = ir.Channel{ID: "chan/reply", Messages: []ir.MessageID{"msg/reply"}}
	replyCh := ir.ChannelID("chan/reply")
	firstOp(doc).Bindings.Message.Reply = &ir.Reply{Channel: &replyCh, Messages: replyUsed}
	return doc
}

// TestValidate_ReplyIsHeldToItsOwnChannel pins which channel the reply half
// reads, which a reply sharing the operation's channel cannot: with one channel
// both spellings agree, so a check that reused the operation's channel passes
// such a case and still reports the wrong set on every real request-reply pair.
//
// The illegal half is the one that separates them. msg/a is carried by the
// operation's channel and not by the reply's, so reading the wrong channel
// reports nothing at all here.
func TestValidate_ReplyIsHeldToItsOwnChannel(t *testing.T) {
	t.Parallel()
	assert.Empty(t, pass.Validate(replyOnItsOwnChannelDoc([]ir.MessageID{"msg/reply"})),
		"the reply channel carries msg/reply, so the reply names nothing it forbids")

	msg := messageForCode(t, pass.Validate(replyOnItsOwnChannelDoc([]ir.MessageID{"msg/a"})),
		"pass/message-not-in-channel")
	assert.Contains(t, msg, "msg/a")
	assert.Contains(t, msg, "chan/reply",
		"judged against the channel the reply travels on, not the one the operation binds")
}
