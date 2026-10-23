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
	ir.ForEachOperation(doc, func(op ir.Operation) {
		diags = append(diags, checkBoundMessages(doc, op)...)
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
// there is no declared message set to hold it to (ir-design §8.3).
func checkBoundMessages(doc *ir.Document, op ir.Operation) []ir.Diagnostic {
	b := op.Bindings.Message
	if b == nil {
		return nil
	}
	at := string(op.ID) + "/bindings/message"
	diags := appendUncarriedMessageDiags(nil, doc, b.Channel, b.Messages, at+"/messages")
	if b.Reply == nil || b.Reply.Channel == nil {
		return diags
	}
	return appendUncarriedMessageDiags(diags, doc, *b.Reply.Channel, b.Reply.Messages,
		at+"/reply/messages")
}

// appendUncarriedMessageDiags appends to dst a diagnostic per message in used
// that channel does not carry, in declaration order; at is the pointer prefix the
// index is appended to.
//
// A channel that resolves nowhere is left alone: there is no set to compare
// against. One that names no declared channel is ir/dangling-channel-ref
// already (checkDanglingRefs), so a second diagnostic here would restate a
// defect rather than add a claim. One that names none at all is reported by
// irverify alone, as ir/empty-channel-ref: this pass does not report an empty
// channel yet (GitHub #575).
func appendUncarriedMessageDiags(dst []ir.Diagnostic, doc *ir.Document,
	channel ir.ChannelID, used []ir.MessageID, at string) []ir.Diagnostic {
	ch, declared := doc.Channels[channel]
	if !declared {
		return dst
	}
	carried := make(map[ir.MessageID]bool, len(ch.Messages))
	for _, id := range ch.Messages {
		carried[id] = true
	}
	for i, id := range used {
		if carried[id] {
			continue
		}
		site := fmt.Sprintf("%s/%d", at, i)
		dst = append(dst, diag(ir.SeverityError, "pass/message-not-in-channel",
			fmt.Sprintf("message %q at %s is not carried by channel %q", id, site, channel), site))
	}
	return dst
}
