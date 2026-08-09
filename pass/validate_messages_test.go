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
