// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "github.com/dexpace/morphic/ir"

// SecretInferenceHeuristic is the name a parameter's, a header's or a
// property's Provenance.Inferred carries where the secret policy set its
// Secret. It is a constant for the reason StreamingInferenceHeuristic is: an
// auditor greps for it.
const SecretInferenceHeuristic ir.Inferred = "secret-inference"

// SecretInference is the secret policy: whether a parameter, a header
// or a property whose name says it holds a credential, and whose value is
// text, is redacted where the document asks for no redaction.
//
// It is a policy because the reading guesses at intent (architecture principle
// 6): a name is no declaration, though a credential the document leaves
// unmarked is printed wherever its value is.
type SecretInference struct {
	// Disabled turns the reading off: a value is then redacted only where the
	// document asks for it.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that a credential's name asks
// for its redaction.
func (p SecretInference) Honours() bool { return !p.Disabled }
