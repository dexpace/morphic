// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// Apply fills what compiledDocumentView's names imply, under each policy of
// policy it honours, and returns what the readings report. promoter is the
// run's promoter, whose keys a reading defers to, and compiledDocumentView the
// document its lowerings built, whose service tree and registry are whole.
func Apply(policy Policy, promoter promotion.Promoter, compiledDocumentView *compileddoc.CompiledDocumentView) (diags []ir.Diagnostic) {
	for _, irOperation := range compiledDocumentView.Operations {
		diags = append(diags, operation(policy, promoter, compiledDocumentView, irOperation)...)
	}
	// Each member is read alone and none is reported, so the registry's order,
	// which no sort fixes here, decides nothing.
	for _, td := range compiledDocumentView.Types {
		model, ok := td.(*ir.Model)
		if !ok || model == nil {
			continue
		}
		for i := range model.Properties {
			redact(policy.Secret, promoter, compiledDocumentView.Types, &model.Properties[i])
		}
	}
	return diags
}

// operation fills what irOperation's names imply: its idempotency token, the
// redaction of its parameters and of every header it declares, and the text of
// each of its header parameters and its responses' headers.
func operation(policy Policy, promoter promotion.Promoter, compiledDocumentView *compileddoc.CompiledDocumentView, irOperation *ir.Operation) []ir.Diagnostic {
	reg := compiledDocumentView.Types
	diags := idempotency(policy.Idempotency, compiledDocumentView, irOperation)
	for i := range irOperation.Params {
		param := &irOperation.Params[i]
		redactUse(policy.Secret, promoter, reg, param.Name, param.Type, param.Unmodeled, &param.Secret, &param.Provenance)
	}
	diags = append(diags, httpDateParam(policy.HTTPDate, promoter, compiledDocumentView, irOperation)...)
	for _, hs := range responseHeaders(irOperation) {
		for i := range hs {
			redact(policy.Secret, promoter, reg, &hs[i].Property)
			diags = append(diags, httpDate(policy.HTTPDate, promoter, reg, &hs[i])...)
		}
	}
	for _, hs := range partHeaders(irOperation) {
		for i := range hs {
			redact(policy.Secret, promoter, reg, &hs[i].Property)
		}
	}
	return diags
}

// responseHeaders returns the header lists of operation's responses, error
// cases and default, each sharing its array with operation, so a write to an
// element lands there.
func responseHeaders(operation *ir.Operation) [][]ir.Header {
	lists := make([][]ir.Header, 0, len(operation.Responses)+len(operation.Errors)+1)
	for i := range operation.Responses {
		lists = append(lists, operation.Responses[i].Headers)
	}
	for i := range operation.Errors {
		lists = append(lists, operation.Errors[i].Headers)
	}
	if operation.Default != nil {
		lists = append(lists, operation.Default.Headers)
	}
	return lists
}

// partHeaders returns the header lists the encodings of operation's bodies give
// a part or a stream's items, in the order of their contents and, within one,
// of their parts' IDs. A part's encoding is a map value whose Headers still
// share their array with operation.
func partHeaders(operation *ir.Operation) [][]ir.Header {
	payloads := []*ir.Payload{operation.Request}
	for i := range operation.Responses {
		payloads = append(payloads, operation.Responses[i].Payload)
	}
	for i := range operation.Errors {
		payloads = append(payloads, operation.Errors[i].Payload)
	}
	if operation.Default != nil {
		payloads = append(payloads, operation.Default.Payload)
	}
	var lists [][]ir.Header
	for _, payload := range payloads {
		if payload == nil {
			continue
		}
		for i := range payload.Contents {
			content := &payload.Contents[i]
			if content.ItemEncoding != nil {
				lists = append(lists, content.ItemEncoding.Headers)
			}
			for _, part := range slices.Sorted(maps.Keys(content.Encoding)) {
				lists = append(lists, content.Encoding[part].Headers)
			}
		}
	}
	return lists
}

// maxScalarHops bounds the scalars a type chain is read through. The IR
// permits a chain of any length, a cycle among them, and a document nests
// aliases a few deep, so a chain longer than this is read as ending at no
// primitive.
const maxScalarHops = 64

// chain is what the scalars from a type stand over: the primitive at the end of
// the chain, and whether a node along it names an encoding, asks for
// redaction, or writes a key the policy reads redaction from.
type chain struct {
	primitive ir.PrimitiveKind
	encoded   bool
	sensitive bool
	spoken    bool
}

// readChain reads the chain from id through each scalar's Base, to
// maxScalarHops of them, and reports false where it ends at a type that is no
// primitive, at a type the registry does not hold, or past the bound.
func readChain(promoter promotion.Promoter, reg ir.TypeRegistry, id ir.TypeID) (chain, bool) {
	var ch chain
	for range maxScalarHops {
		td := reg[id]
		if ir.IsNilTypeDef(td) {
			return chain{}, false
		}
		common := td.Common()
		ch.sensitive = ch.sensitive || common.Sensitive
		ch.spoken = ch.spoken || promoter.Writes(common.Unmodeled, promotion.TargetRedaction)
		switch n := td.(type) {
		case *ir.Primitive:
			ch.primitive = n.PrimitiveKind
			return ch, true
		case *ir.Scalar:
			ch.encoded = ch.encoded || n.Encoding != nil && n.Encoding.Name != ""
			if n.Base == nil {
				return chain{}, false
			}
			id = n.Base.Target
		default:
			return chain{}, false
		}
	}
	return chain{}, false
}
