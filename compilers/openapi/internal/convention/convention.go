// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// Apply fills what doc's names imply, under each policy c honours, and returns
// what the readings report. c is the source's context, and doc the document
// its lowerings built, whose service tree and registry are whole.
func Apply(c lowering.Ctx, doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for i := range doc.Services {
		for _, op := range lowering.Operations(&doc.Services[i]) {
			diags = append(diags, operation(c, doc.Types, op)...)
		}
	}
	// Each member is read alone and none is reported, so the registry's order,
	// which no sort fixes here, decides nothing.
	for _, td := range doc.Types {
		m, ok := td.(*ir.Model)
		if !ok || m == nil {
			continue
		}
		for i := range m.Properties {
			redact(c, doc.Types, &m.Properties[i])
		}
	}
	return diags
}

// operation fills what op's names imply: its idempotency token, the redaction
// of its parameters and of every header it declares, and the text of each of
// its responses' headers.
func operation(c lowering.Ctx, reg ir.TypeRegistry, op *ir.Operation) []ir.Diagnostic {
	diags := idempotency(c, op)
	for i := range op.Params {
		p := &op.Params[i]
		redactUse(c, reg, p.Name, p.Type, p.Unmodeled, &p.Secret, &p.Provenance)
	}
	for _, hs := range responseHeaders(op) {
		for i := range hs {
			redact(c, reg, &hs[i].Property)
			diags = append(diags, httpDate(c, reg, &hs[i])...)
		}
	}
	for _, hs := range partHeaders(op) {
		for i := range hs {
			redact(c, reg, &hs[i].Property)
		}
	}
	return diags
}

// responseHeaders returns the header lists of op's responses, error cases and
// default, each sharing its array with op, so a write to an element lands
// there.
func responseHeaders(op *ir.Operation) [][]ir.Header {
	lists := make([][]ir.Header, 0, len(op.Responses)+len(op.Errors)+1)
	for i := range op.Responses {
		lists = append(lists, op.Responses[i].Headers)
	}
	for i := range op.Errors {
		lists = append(lists, op.Errors[i].Headers)
	}
	if op.Default != nil {
		lists = append(lists, op.Default.Headers)
	}
	return lists
}

// partHeaders returns the header lists the encodings of op's bodies give a part
// or a stream's items, in the order of their contents and, within one, of their
// parts' IDs. A part's encoding is a map value whose Headers still share their
// array with op.
func partHeaders(op *ir.Operation) [][]ir.Header {
	payloads := []*ir.Payload{op.Request}
	for i := range op.Responses {
		payloads = append(payloads, op.Responses[i].Payload)
	}
	for i := range op.Errors {
		payloads = append(payloads, op.Errors[i].Payload)
	}
	if op.Default != nil {
		payloads = append(payloads, op.Default.Payload)
	}
	var lists [][]ir.Header
	for _, p := range payloads {
		if p == nil {
			continue
		}
		for i := range p.Contents {
			content := &p.Contents[i]
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
	prim      ir.PrimKind
	encoded   bool
	sensitive bool
	spoken    bool
}

// readChain reads the chain from id through each scalar's Base, to
// maxScalarHops of them, and reports false where it ends at a type that is no
// primitive, at a type the registry does not hold, or past the bound.
func readChain(p lowering.Promoter, reg ir.TypeRegistry, id ir.TypeID) (chain, bool) {
	var ch chain
	for range maxScalarHops {
		td := reg[id]
		if ir.IsNilTypeDef(td) {
			return chain{}, false
		}
		common := td.Common()
		ch.sensitive = ch.sensitive || common.Sensitive
		ch.spoken = ch.spoken || p.Writes(common.Unmodeled, lowering.TargetRedaction)
		switch n := td.(type) {
		case *ir.Primitive:
			ch.prim = n.Prim
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
