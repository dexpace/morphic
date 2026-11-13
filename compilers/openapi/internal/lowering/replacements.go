// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "github.com/dexpace/morphic/ir"

// ReplacementInferenceHeuristic is the name a deprecated node's
// Provenance.Inferred carries where the replacement policy read its
// Deprecation.ReplacedBy out of the deprecation's message. It is a constant for
// the reason StreamingMediaTypeHeuristic is: an auditor greps for it.
const ReplacementInferenceHeuristic ir.Inferred = "replacement-inference"

// ReplacementInference is the replacement policy: whether a deprecation whose
// message names what to use instead, in a form the document's own entities
// resolve, names it in ReplacedBy too, where no extension does.
//
// It is a policy because the reading guesses at intent (architecture principle
// 6): a message is prose, and an entity it names need not be the replacement.
type ReplacementInference struct {
	// Disabled turns the reading off: a replacement the message names then
	// stays in the message alone.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that a message's replacements
// fill ReplacedBy.
func (p ReplacementInference) Honours() bool { return !p.Disabled }
