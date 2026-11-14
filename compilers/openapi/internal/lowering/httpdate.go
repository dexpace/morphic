// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "github.com/dexpace/morphic/ir"

// HTTPDateInferenceHeuristic is the name a header's Provenance.Inferred carries
// where the HTTP-date policy set its Encoding. It is a constant for the reason
// StreamingInferenceHeuristic is: an auditor greps for it.
const HTTPDateInferenceHeuristic ir.Inferred = "http-date-inference"

// HTTPDateInference is the HTTP-date policy: whether a response header whose
// field an RFC defines as an HTTP-date, and whose schema declares a date-time
// with no encoding, is written and read as an HTTP-date.
//
// It is a policy because the reading departs from the document (architecture
// principle 6): `format: date-time` spells RFC 3339's text, which the field's
// own definition rules out.
type HTTPDateInference struct {
	// Disabled turns the reading off: such a header's text is then the one its
	// format names.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that the field's definition
// gives the header its text.
func (p HTTPDateInference) Honours() bool { return !p.Disabled }
