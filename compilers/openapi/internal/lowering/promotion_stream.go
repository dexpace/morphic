// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "github.com/dexpace/morphic/ir"

// The targets filling an operation's ir.StreamDetail
// (docs/compilers/openapi.md#streaming-media).
const (
	// TargetStreamTerminator fills ir.StreamDetail.Terminator, from a key on a
	// streaming content of a success response or on the operation.
	TargetStreamTerminator ExtensionTarget = "stream.terminator"
	// TargetStreamCondition fills ir.StreamDetail.Condition from a key on the
	// operation holding an expression that names an input of its request.
	TargetStreamCondition ExtensionTarget = "stream.condition"
)

// TextStatement is what the sources of one target state on one node, as text.
type TextStatement struct {
	// Text is what the first source stating a text states; empty where none
	// does.
	Text string
	// Source is the source Text is read from, unset with it.
	Source ExtensionSource
	// At is where Source's value sits, which a report names.
	At ir.Provenance
}

// TextOf reads what p's sources of target state on the node keeping
// unmodeled: the text of the first to state one, by the policy's order
// (firstValue). A value that is no text or an empty text states nothing, and
// is reported at its own key.
func (p Promoter) TextOf(target ExtensionTarget, unmodeled ir.Unmodeled) (TextStatement, []ir.Diagnostic) {
	st, written, diags := p.firstValue(target, unmodeled, refuseText)
	if !written {
		return TextStatement{}, diags
	}
	text, _ := extensionText(st.Value)
	return TextStatement{Text: text, Source: st.Source, At: st.At}, diags
}

// refuseText says why raw states no text, or "" where it does.
func refuseText(raw ir.RawValue) string {
	text, ok := extensionText(raw)
	switch {
	case !ok:
		return "is no text"
	case text == "":
		return "is an empty text"
	default:
		return ""
	}
}
