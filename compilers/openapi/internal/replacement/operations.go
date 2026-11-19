// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// service fills the replacements of every operation of the document, of its
// parameters, and of the headers its responses and its bodies' encodings
// declare.
func (r *resolver) service() []ir.Diagnostic {
	operations := r.w.Operations
	byName := map[string][]ir.OperationID{}
	for _, operation := range operations {
		if name := operation.Name.Source; name != "" {
			byName[name] = append(byName[name], operation.ID)
		}
	}
	operation := func(name string) (ir.EntityRef, int) {
		found := byName[name]
		if len(found) != 1 {
			return ir.EntityRef{}, len(found)
		}
		return ir.EntityRef{Operation: found[0]}, 1
	}
	var diags []ir.Diagnostic
	for _, irOperation := range operations {
		diags = append(diags, r.fill(irOperation.Unmodeled, irOperation.Deprecation, &irOperation.Provenance, ir.EntityRef{Operation: irOperation.ID},
			"operation", operation)...)
		diags = append(diags, r.params(irOperation)...)
		diags = append(diags, r.payload(irOperation.Request)...)
		for i := range irOperation.Responses {
			diags = append(diags, r.response(irOperation.Responses[i].Headers, irOperation.Responses[i].Payload)...)
		}
		for i := range irOperation.Errors {
			diags = append(diags, r.response(irOperation.Errors[i].Headers, irOperation.Errors[i].Payload)...)
		}
		if irOperation.Default != nil {
			diags = append(diags, r.response(irOperation.Default.Headers, irOperation.Default.Payload)...)
		}
	}
	return diags
}

// response fills the replacements of a response's headers, hs, and of those its
// body, payload, declares.
func (r *resolver) response(hs []ir.Header, payload *ir.Payload) []ir.Diagnostic {
	return append(r.headers(hs), r.payload(payload)...)
}

// payload fills the replacements of the headers each media type of payload
// declares for a part or a stream's items, each among the headers beside it. A
// part's encoding is a map value, whose Headers still share their array with
// payload.
func (r *resolver) payload(payload *ir.Payload) []ir.Diagnostic {
	if payload == nil {
		return nil
	}
	var diags []ir.Diagnostic
	for i := range payload.Contents {
		content := &payload.Contents[i]
		if content.ItemEncoding != nil {
			diags = append(diags, r.headers(content.ItemEncoding.Headers)...)
		}
		for _, part := range slices.Sorted(maps.Keys(content.Encoding)) {
			diags = append(diags, r.headers(content.Encoding[part].Headers)...)
		}
	}
	return diags
}

// params fills each parameter's replacements, among operation's parameters.
func (r *resolver) params(operation *ir.Operation) []ir.Diagnostic {
	param := r.paramFinder(operation)
	diags := make([]ir.Diagnostic, 0, len(operation.Params))
	for i := range operation.Params {
		parameter := &operation.Params[i]
		diags = append(diags, r.fill(parameter.Unmodeled, parameter.Deprecation, &parameter.Provenance, ir.EntityRef{Param: parameter.ID}, "parameter", param)...)
	}
	return diags
}

// paramFinder returns what finds a parameter of operation by name, as a link's
// key names one (compileddoc.ParamIndex.Named).
func (r *resolver) paramFinder(operation *ir.Operation) func(string) (ir.EntityRef, int) {
	return func(name string) (ir.EntityRef, int) {
		id, matched := r.w.Params(operation).Named(name)
		return ir.EntityRef{Param: id}, matched
	}
}
