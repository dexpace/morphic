// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

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

// TextOf reads what p's sources of target state on the node keeping unmodeled:
// the text of the first to state one, by the policy's order. A value that is
// no text, an empty text and a later source stating another text are reported
// at their own keys as info diag.UnpromotedExtension, and state nothing.
func (p Promoter) TextOf(target ExtensionTarget, unmodeled ir.Unmodeled) (TextStatement, []ir.Diagnostic) {
	var st TextStatement
	var diags []ir.Diagnostic
	for _, src := range p.targets[target] {
		entry, written := unmodeled[extensionKeyPrefix+src.Key]
		if !written {
			continue
		}
		raw, at, _, located := src.value(entry)
		text, ok := extensionText(raw)
		switch {
		case located != states || !ok:
			diags = append(diags, Unpromoted(src.String(), at, "is no text", "no "+targetNoun(target)+" is filled"))
		case text == "":
			diags = append(diags, Unpromoted(src.String(), at, "is an empty text", "no "+targetNoun(target)+" is filled"))
		case st.Text == "":
			st.Text, st.Source, st.At = text, src, at
		case text != st.Text:
			diags = append(diags, Unpromoted(src.String(), at,
				"states another text than "+strconv.Quote(st.Source.String())+", which the policy reads first",
				"it fills no "+targetNoun(target)))
		}
	}
	return st, diags
}

// Unpromoted is the info diag.UnpromotedExtension reporting the key key at at,
// which states nothing usable, for why, so outcome.
func Unpromoted(key string, at ir.Provenance, why, outcome string) ir.Diagnostic {
	return diag.Newf(ir.SeverityInfo, diag.UnpromotedExtension, at, "extension %q %s, so %s", key, why, outcome)
}

// targetNoun names the field target, a stream target, fills in a report.
func targetNoun(target ExtensionTarget) string {
	if target == TargetStreamCondition {
		return "stream condition"
	}
	return "stream terminator"
}
