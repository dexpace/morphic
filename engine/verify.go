// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
	"github.com/dexpace/morphic/pass"
)

const (
	// maxListed bounds how many violations a VerifyError's message names.
	maxListed = 3
	// maxPath bounds the runes of a violation's path in that message. A path
	// carries the document's own keys, which no limit holds to a length.
	maxPath = 200
	// codeUnencodable is the code of the violation a document that will not
	// encode as JSON adds to the ones irverify reports: invariant 7, that the
	// whole Document round-trips, is the one it cannot check by walking.
	codeUnencodable = "engine/unencodable"
)

// errInputChanged reports a pass that broke its contract by changing the
// document it was handed (RunOptions.Verify). The next stage and the caller
// read that document as the compiler left it.
var errInputChanged = errors.New("changed the document it was handed")

// VerifyError reports a stage whose output breaks the IR's structural
// invariants, found under RunOptions.Verify. These are not the spec's problems
// but the stage's: a compiler or pass returned a document, or findings, it
// should not have, so it wraps the stage's contract error, compilers.ErrContract
// or pass.ErrContract, and none for a document handed to RunDocument.
type VerifyError struct {
	// Stage names what returned the document: a compile of a source, a pass, or
	// the document RunDocument was handed.
	Stage string
	// Violations is every invariant the document breaks, as irverify reports
	// them, with a violation of its own code for a document that will not encode.
	Violations []irverify.Violation
	// Findings reports that the violations are in the findings the stage
	// reported, on its document or beside it, which each Path reads as
	// findings[i] (irverify.VerifyFindings), rather than in the document.
	Findings bool
	// cause is the contract error the stage's breach wraps, or nil.
	cause error
}

// Error names the stage and the first few violations on one line. Violations
// holds them all.
func (e *VerifyError) Error() string {
	listed := min(len(e.Violations), maxListed)
	parts := make([]string, 0, listed+1)
	for _, v := range e.Violations[:listed] {
		parts = append(parts, fmt.Sprintf("%s at %.*q", v.Code, maxPath, v.Path))
	}
	if more := len(e.Violations) - listed; more > 0 {
		parts = append(parts, fmt.Sprintf("and %d more", more))
	}
	broken := "the document breaks"
	if e.Findings {
		broken = "its findings break"
	}
	msg := fmt.Sprintf("engine: verify %s: %s %d invariants: %s",
		e.Stage, broken, len(e.Violations), strings.Join(parts, "; "))
	if e.cause != nil {
		return msg + ": " + e.cause.Error()
	}
	return msg
}

// Unwrap returns the contract error the breach wraps, or nil.
func (e *VerifyError) Unwrap() error { return e.cause }

// verifier lists the invariants a document breaks. An engine holds irverify.Verify.
type verifier func(*ir.Document) []irverify.Violation

// fingerprint is the SHA-256 of a document's canonical JSON, which is the whole
// of what the document says (invariant 7): two documents with one fingerprint
// are the same document to every consumer.
type fingerprint [sha256.Size]byte

// fingerprintOf encodes doc, as ir.Document encodes itself, into a hash, so the
// bytes are never held. It fails for a document that will not encode.
func fingerprintOf(doc *ir.Document) (fingerprint, error) {
	h := sha256.New()
	if err := json.MarshalWrite(h, doc); err != nil {
		return fingerprint{}, err
	}
	return fingerprint(h.Sum(nil)), nil
}

// hold verifies doc, which stage returned, and fingerprints it, on a plan that
// verifies; on one that does not it does neither. cause is the contract error a
// breach wraps. The fingerprint comes back with the error: it is the zero one
// for a document that will not encode.
func (p plan) hold(stage string, cause error, doc *ir.Document) (fingerprint, error) {
	if p.verify == nil {
		return fingerprint{}, nil
	}
	violations := p.verify(doc)
	sum, err := fingerprintOf(doc)
	if err != nil {
		violations = append(violations, irverify.Violation{
			Code:    codeUnencodable,
			Message: strconv.Quote(err.Error()),
			Path:    ir.DocumentPath,
		})
	}
	if len(violations) > 0 {
		return sum, &VerifyError{Stage: stage, Violations: violations, cause: cause}
	}
	return sum, nil
}

// holdFindings holds diags, the findings stage reported, to the invariants
// irverify holds a document's findings to, read against sources, on a plan
// that verifies; on one that does not it does nothing. cause is the contract
// error a breach wraps.
func (p plan) holdFindings(stage string, cause error, diags []ir.Diagnostic, sources []ir.SourceInfo) error {
	if p.verify == nil {
		return nil
	}
	if violations := irverify.VerifyFindings(diags, sources); len(violations) > 0 {
		return &VerifyError{Stage: stage, Violations: violations, Findings: true, cause: cause}
	}
	return nil
}

// afterPass holds what np returned to what a verified run asks of it beyond the
// contract every run checks: that doc, the document np was handed, is as np
// found it, that next, the one it returned, keeps the IR's invariants, and that
// diags, its findings, keep them too. before is doc's fingerprint when np
// began, and the fingerprint of next comes back.
func (p plan) afterPass(np namedPass, before fingerprint, doc, next *ir.Document, diags []ir.Diagnostic) (fingerprint, error) {
	if p.verify == nil {
		return before, nil
	}
	after, err := fingerprintOf(doc)
	if err != nil {
		return fingerprint{}, np.breach(fmt.Errorf("it no longer encodes (%w): %w", err, errInputChanged))
	}
	if after != before {
		return fingerprint{}, np.breach(errInputChanged)
	}
	stage := fmt.Sprintf("pass %q", np.name)
	sum := before
	if next != doc {
		if sum, err = p.hold(stage, pass.ErrContract, next); err != nil {
			return sum, err
		}
	}
	return sum, p.holdFindings(stage, pass.ErrContract, diags, next.Sources)
}
