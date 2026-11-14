// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package stream

import (
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// The outcomes a report of a key that fills nothing names.
const (
	noTerminator = "no stream terminator is filled"
	noCondition  = "no stream condition is filled"
)

// PromoteDetail fills each operation's ResponseStream.Terminator and Condition from
// the keys the policy's sources of lowering.TargetStreamTerminator and
// lowering.TargetStreamCondition name, marking the node whose key filled it,
// and reports each key that fills nothing as info
// diag.UnpromotedExtension. Every key stays as written.
func PromoteDetail(c lowering.Ctx, w *whole.Document) (diags []ir.Diagnostic) {
	p := c.Promoter()
	for _, op := range w.Ops {
		diags = append(diags, terminator(p, op)...)
		diags = append(diags, condition(p, w, op)...)
	}
	return diags
}

// keyed is a streaming content of a success response and what its keys state
// of the terminator.
type keyed struct {
	content *ir.Content
	st      lowering.TextStatement
}

// terminator fills op's terminator, all or none: from the operation's key
// where it states one and no streaming content states another, else from the
// streaming contents of its success responses where every one states the same
// text. Where they disagree, one report names the later key by pointer.
func terminator(p lowering.Promoter, op *ir.Operation) []ir.Diagnostic {
	own, diags := p.TextOf(lowering.TargetStreamTerminator, op.Unmodeled)
	if op.ResponseStream == nil {
		if own.Text != "" {
			diags = append(diags, lowering.Unpromoted(own.Source.String(), own.At,
				"states a terminator of operation "+strconv.Quote(string(op.ID))+", which streams no response",
				noTerminator))
		}
		return diags
	}
	contents, contentDiags := streamingContents(p, op)
	diags = append(diags, contentDiags...)
	if own.Text != "" {
		return append(diags, fromOperation(op, own, contents)...)
	}
	return append(diags, fromContents(op, contents)...)
}

// streamingContents reads the terminator each streaming content of op's
// success responses states.
func streamingContents(p lowering.Promoter, op *ir.Operation) ([]keyed, []ir.Diagnostic) {
	var out []keyed
	var diags []ir.Diagnostic
	for i := range op.Responses {
		if op.Responses[i].Payload == nil {
			continue
		}
		cs := op.Responses[i].Payload.Contents
		for j := range cs {
			if cs[j].Stream == nil {
				continue
			}
			st, readDiags := p.TextOf(lowering.TargetStreamTerminator, cs[j].Unmodeled)
			out = append(out, keyed{content: &cs[j], st: st})
			diags = append(diags, readDiags...)
		}
	}
	return out, diags
}

// fromOperation fills the terminator own states, the operation's, where no
// content states another, reporting each that does. A content stating the
// same text is marked beside the operation.
func fromOperation(op *ir.Operation, own lowering.TextStatement, contents []keyed) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, k := range contents {
		if k.st.Text != "" && k.st.Text != own.Text {
			diags = append(diags, lowering.Unpromoted(k.st.Source.String(), k.st.At,
				"states "+strconv.Quote(k.st.Text)+" where operation "+strconv.Quote(string(op.ID))+
					" states "+strconv.Quote(own.Text), noTerminator))
		}
	}
	if len(diags) > 0 {
		return diags
	}
	fillTerminator(op, own.Text, contents)
	op.Provenance.Inferred = ir.JoinInferred(op.Provenance.Inferred, lowering.ExtensionPromotionHeuristic)
	return nil
}

// fromContents fills the terminator the streaming contents state where each
// states the same text, and reports, once, at the later key by pointer, where
// one states another or none beside one that states it.
func fromContents(op *ir.Operation, contents []keyed) []ir.Diagnostic {
	var text string
	var later *lowering.TextStatement
	agree := true
	for i := range contents {
		st := &contents[i].st
		switch {
		case st.Text == "":
			agree = false
			continue
		case text == "":
			text = st.Text
		case st.Text != text:
			agree = false
		}
		if later == nil || st.At.Pointer > later.At.Pointer {
			later = st
		}
	}
	if later == nil {
		return nil
	}
	if !agree {
		return []ir.Diagnostic{lowering.Unpromoted(later.Source.String(), later.At,
			"states "+strconv.Quote(later.Text)+", but the streaming contents of operation "+
				strconv.Quote(string(op.ID))+" do not all state one text", noTerminator)}
	}
	fillTerminator(op, text, contents)
	return nil
}

// fillTerminator sets op's terminator to text and marks each content whose key states
// it.
func fillTerminator(op *ir.Operation, text string, contents []keyed) {
	op.ResponseStream.Terminator = &text
	for _, k := range contents {
		if k.st.Text == text {
			k.content.Inferred = ir.JoinInferred(k.content.Inferred, lowering.ExtensionPromotionHeuristic)
		}
	}
}

// condition fills op's stream condition from the operation's key: an
// expression naming a boolean input of the request, which asks for the stream
// when true, where op streams a success response that also offers a content
// that does not stream. Any other key is reported and fills nothing.
func condition(p lowering.Promoter, w *whole.Document, op *ir.Operation) []ir.Diagnostic {
	st, diags := p.TextOf(lowering.TargetStreamCondition, op.Unmodeled)
	if st.Text == "" {
		return diags
	}
	in, typ, why := runtimeexpr.RequestValue(w, op, st.Text)
	switch {
	case why != "":
	case !namesBoolean(w, typ):
		why = "names an input of operation " + strconv.Quote(string(op.ID)) + " that is no boolean"
	case !choosesStream(op):
		why = "names an input of operation " + strconv.Quote(string(op.ID)) +
			", which offers no success response a choice between a stream and a content that does not stream"
	default:
		op.ResponseStream.Condition = &ir.StreamCondition{Input: *in, Value: ir.Value{Kind: ir.ValueBool, Bool: true}}
		op.Provenance.Inferred = ir.JoinInferred(op.Provenance.Inferred, lowering.ExtensionPromotionHeuristic)
		return diags
	}
	return append(diags, lowering.Unpromoted(st.Source.String(), st.At, why, noCondition))
}

// namesBoolean reports whether id stands for the boolean primitive.
func namesBoolean(w *whole.Document, id ir.TypeID) bool {
	prim, ok := w.Stands(id).(*ir.Primitive)
	return ok && prim.Prim == ir.PrimBool
}

// choosesStream reports whether op streams a response and a success response
// offers a content that does not stream beside one that does.
func choosesStream(op *ir.Operation) bool {
	if op.ResponseStream == nil {
		return false
	}
	for i := range op.Responses {
		if op.Responses[i].Payload == nil {
			continue
		}
		streams, single := false, false
		for _, c := range op.Responses[i].Payload.Contents {
			streams = streams || c.Stream != nil
			single = single || c.Stream == nil
		}
		if streams && single {
			return true
		}
	}
	return false
}
