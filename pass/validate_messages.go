package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// checkMessageBindings reports operations whose message binding uses a message
// the channel it binds does not carry.
//
// Channel.Messages is the channel's contract, which messages may travel on it
// (ir-design §8.3), and an operation names the subset it uses. A binding naming
// one outside that set is a well-formed reference, so the reference walk has
// nothing to say, while an emitter generates a publisher for a message the
// channel forbids. The code keeps the pass/ namespace: this is a containment
// judgement the pass owns outright, not a broken reference.
//
// It enters through ir.ForEachOperation, so it inherits the group-depth bound that
// checkGroupWalkTruncated reports.
func checkMessageBindings(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var carried reusedMap[ir.MessageID, bool]
	ir.ForEachOperation(doc, func(op ir.Operation) {
		diags = append(diags, checkBoundMessages(doc, op, &carried)...)
	})
	return diags
}

// checkBoundMessages holds every message set a binding names to the message set
// of the channel that set travels on. A binding names two: its own Messages, on
// Channel, and a reply's, on the reply channel. Both are the same containment
// claim — a reply payload the reply channel does not carry is as unroutable as a
// request the operation's channel does not — so neither is checked alone.
//
// A reply with no channel of its own is left alone: its address is dynamic, so
// there is no declared message set to hold it to (ir-design §8.3). Each
// channel's messages are read through carried.
func checkBoundMessages(doc *ir.Document, op ir.Operation, carried *reusedMap[ir.MessageID, bool]) []ir.Diagnostic {
	b := op.Bindings.Message
	if b == nil {
		return nil
	}
	diags := appendUncarriedMessageDiags(nil, doc, b.Channel, b.Messages, messagesAt{op.ID, "messages"}, carried)
	if b.Reply == nil || b.Reply.Channel == nil {
		return diags
	}
	return appendUncarriedMessageDiags(diags, doc, *b.Reply.Channel, b.Reply.Messages,
		messagesAt{op.ID, "reply/messages"}, carried)
}

// messagesAt is where a binding's message list sits: the operation, and the
// list's field below its message binding. It is spelled only for a diagnostic.
type messagesAt struct {
	op    ir.OpID
	field string
}

// appendUncarriedMessageDiags appends to dst a diagnostic per message in used
// that channel does not carry, in declaration order; at is where used sits, and
// carried what the channel's messages are read through.
//
// A channel that resolves nowhere is left alone: there is no set to compare
// against. One that names no declared channel is ir/dangling-channel-ref
// already (checkDanglingRefs), so a second diagnostic here would restate a
// defect rather than add a claim. One that names none at all is reported by
// irverify alone, as ir/empty-channel-ref: this pass does not report an empty
// channel yet (GitHub #575).
func appendUncarriedMessageDiags(dst []ir.Diagnostic, doc *ir.Document,
	channel ir.ChannelID, used []ir.MessageID, at messagesAt, carried *reusedMap[ir.MessageID, bool]) []ir.Diagnostic {
	ch, declared := doc.Channels[channel]
	if !declared {
		return dst
	}
	carries := carried.emptied()
	for _, id := range ch.Messages {
		carries[id] = true
	}
	for i, id := range used {
		if carries[id] {
			continue
		}
		site := fmt.Sprintf("%s/bindings/message/%s/%d", at.op, at.field, i)
		dst = append(dst, diag(ir.SeverityError, "pass/message-not-in-channel",
			fmt.Sprintf("message %q at %s is not carried by channel %q", id, site, channel), site))
	}
	return dst
}
