// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import "github.com/dexpace/morphic/ir"

// Policy holds the policies of what a document's names imply, as the
// compiler's Options resolve them: one for each reading Apply makes.
type Policy struct {
	Idempotency IdempotencyInference
	Secret      SecretInference
	HTTPDate    HTTPDateInference
}

// IdempotencyInferenceHeuristic is the name an Operation's
// Provenance.Inferred carries where the idempotency policy read its
// Idempotency off a header parameter. It is a constant for the reason
// lowering.StreamingInferenceHeuristic is: an auditor greps for it.
const IdempotencyInferenceHeuristic ir.Inferred = "idempotency-inference"

// IdempotencyInference is the idempotency policy: whether an operation
// declaring an `Idempotency-Key` header parameter, and no idempotency, takes
// that parameter as its idempotency token.
//
// It is a policy because the reading guesses at intent (architecture principle
// 6): OpenAPI has no field for a token, and a header's name is all that says
// the server reads one.
type IdempotencyInference struct {
	// Disabled turns the reading off: the header is then a parameter like any
	// other, and the operation's idempotency what the document declares.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that the header names the
// operation's idempotency token.
func (p IdempotencyInference) Honours() bool { return !p.Disabled }

// SecretInferenceHeuristic is the name a parameter's, a header's or a
// property's Provenance.Inferred carries where the secret policy set its
// Secret. It is a constant for the reason lowering.StreamingInferenceHeuristic
// is: an auditor greps for it.
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

// HTTPDateInferenceHeuristic is the name a header's Provenance.Inferred carries
// where the HTTP-date policy set its Encoding. It is a constant for the reason
// lowering.StreamingInferenceHeuristic is: an auditor greps for it.
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
