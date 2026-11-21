// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package stream

import (
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/ir"
)

// The outcomes a report of a key that fills nothing names.
const (
	noTerminator = "no stream terminator is filled"
	noCondition  = "no stream condition is filled"
)

// PromoteDetail fills each operation's ResponseStream.Terminator and Condition from
// the keys the policy's sources of promotion.TargetStreamTerminator and
// promotion.TargetStreamCondition name, marking the node whose key filled it,
// and reports each key that fills nothing as info
// diag.UnpromotedExtension. Every key stays as written. promoter is the run's
// promoter.
func PromoteDetail(promoter promotion.Promoter, compiledDocumentView *compileddoc.CompiledDocumentView) (diags []ir.Diagnostic) {
	for _, operation := range compiledDocumentView.Operations {
		diags = append(diags, terminator(promoter, operation)...)
		diags = append(diags, condition(promoter, compiledDocumentView, operation)...)
	}
	return diags
}

// keyed is a streaming content of a success response and what its keys state
// of the terminator.
type keyed struct {
	content *ir.Content
	st      promotion.TextStatement
}

// terminator fills operation's terminator, all or none: from the operation's
// key where it states one and no streaming content states another, else from
// the streaming contents of its success responses where every one states the
// same text. Where they disagree, one report names the later key by pointer.
func terminator(promoter promotion.Promoter, operation *ir.Operation) []ir.Diagnostic {
	own, diags := promoter.TextOf(promotion.TargetStreamTerminator, operation.Unmodeled)
	if operation.ResponseStream == nil {
		if own.Text != "" {
			diags = append(diags, promotion.Unpromoted(own.Source.String(), own.At,
				"states a terminator of operation "+strconv.Quote(string(operation.ID))+", which streams no response",
				noTerminator))
		}
		return diags
	}
	contents, contentDiags := streamingContents(promoter, operation)
	diags = append(diags, contentDiags...)
	if own.Text != "" {
		return append(diags, fromOperation(operation, own, contents)...)
	}
	return append(diags, fromContents(operation, contents)...)
}

// streamingContents reads the terminator each streaming content of operation's
// success responses states.
func streamingContents(promoter promotion.Promoter, operation *ir.Operation) ([]keyed, []ir.Diagnostic) {
	var out []keyed
	var diags []ir.Diagnostic
	for i := range operation.Responses {
		if operation.Responses[i].Payload == nil {
			continue
		}
		cs := operation.Responses[i].Payload.Contents
		for j := range cs {
			if cs[j].Stream == nil {
				continue
			}
			st, readDiags := promoter.TextOf(promotion.TargetStreamTerminator, cs[j].Unmodeled)
			out = append(out, keyed{content: &cs[j], st: st})
			diags = append(diags, readDiags...)
		}
	}
	return out, diags
}

// fromOperation fills the terminator own states, the operation's, where no
// content states another, reporting each that does. A content stating the
// same text is marked beside the operation.
func fromOperation(operation *ir.Operation, own promotion.TextStatement, contents []keyed) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyed := range contents {
		if keyed.st.Text != "" && keyed.st.Text != own.Text {
			diags = append(diags, promotion.Unpromoted(keyed.st.Source.String(), keyed.st.At,
				"states "+strconv.Quote(keyed.st.Text)+" where operation "+strconv.Quote(string(operation.ID))+
					" states "+strconv.Quote(own.Text), noTerminator))
		}
	}
	if len(diags) > 0 {
		return diags
	}
	fillTerminator(operation, own.Text, contents)
	operation.Provenance.Inferred = ir.JoinInferred(operation.Provenance.Inferred, promotion.ExtensionPromotionHeuristic)
	return nil
}

// fromContents fills the terminator the streaming contents state where each
// states the same text, and reports, once, at the later key by pointer, where
// one states another or none beside one that states it.
func fromContents(operation *ir.Operation, contents []keyed) []ir.Diagnostic {
	var text string
	var later *promotion.TextStatement
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
		return []ir.Diagnostic{promotion.Unpromoted(later.Source.String(), later.At,
			"states "+strconv.Quote(later.Text)+", but the streaming contents of operation "+
				strconv.Quote(string(operation.ID))+" do not all state one text", noTerminator)}
	}
	fillTerminator(operation, text, contents)
	return nil
}

// fillTerminator sets operation's terminator to text and marks each content
// whose key states it.
func fillTerminator(operation *ir.Operation, text string, contents []keyed) {
	operation.ResponseStream.Terminator = &text
	for _, keyed := range contents {
		if keyed.st.Text == text {
			keyed.content.Inferred = ir.JoinInferred(keyed.content.Inferred, promotion.ExtensionPromotionHeuristic)
		}
	}
}

// condition fills operation's stream condition from the operation's key: an
// expression naming a boolean input of the request, which asks for the stream
// when true, where operation streams a success response that also offers a
// content that does not stream. Any other key is reported and fills nothing.
func condition(promoter promotion.Promoter, compiledDocumentView *compileddoc.CompiledDocumentView, operation *ir.Operation) []ir.Diagnostic {
	st, diags := promoter.TextOf(promotion.TargetStreamCondition, operation.Unmodeled)
	if st.Text == "" {
		return diags
	}
	in, typ, why := runtimeexpr.RequestValue(compiledDocumentView, operation, st.Text)
	switch {
	case why != "":
	case !namesBoolean(compiledDocumentView, typ):
		why = "names an input of operation " + strconv.Quote(string(operation.ID)) + " that is no boolean"
	case !choosesStream(operation):
		why = "names an input of operation " + strconv.Quote(string(operation.ID)) +
			", which offers no success response a choice between a stream and a content that does not stream"
	default:
		operation.ResponseStream.Condition = &ir.StreamCondition{Input: *in, Value: ir.Value{Kind: ir.ValueBool, Bool: true}}
		operation.Provenance.Inferred = ir.JoinInferred(operation.Provenance.Inferred, promotion.ExtensionPromotionHeuristic)
		return diags
	}
	return append(diags, promotion.Unpromoted(st.Source.String(), st.At, why, noCondition))
}

// namesBoolean reports whether id stands for the boolean primitive.
func namesBoolean(compiledDocumentView *compileddoc.CompiledDocumentView, id ir.TypeID) bool {
	primitive, ok := compiledDocumentView.Stands(id).(*ir.Primitive)
	return ok && primitive.PrimitiveKind == ir.PrimitiveBool
}

// choosesStream reports whether operation streams a response and a success
// response offers a content that does not stream beside one that does.
func choosesStream(operation *ir.Operation) bool {
	if operation.ResponseStream == nil {
		return false
	}
	for i := range operation.Responses {
		if operation.Responses[i].Payload == nil {
			continue
		}
		streams, single := false, false
		for _, content := range operation.Responses[i].Payload.Contents {
			streams = streams || content.Stream != nil
			single = single || content.Stream == nil
		}
		if streams && single {
			return true
		}
	}
	return false
}
