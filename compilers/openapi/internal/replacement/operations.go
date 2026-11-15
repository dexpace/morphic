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
	ops := r.w.Ops
	byName := map[string][]ir.OpID{}
	for _, op := range ops {
		if name := op.Name.Source; name != "" {
			byName[name] = append(byName[name], op.ID)
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
	for _, op := range ops {
		diags = append(diags, r.fill(op.Unmodeled, op.Deprecation, &op.Provenance, ir.EntityRef{Operation: op.ID},
			"operation", operation)...)
		diags = append(diags, r.params(op)...)
		diags = append(diags, r.payload(op.Request)...)
		for i := range op.Responses {
			diags = append(diags, r.response(op.Responses[i].Headers, op.Responses[i].Payload)...)
		}
		for i := range op.Errors {
			diags = append(diags, r.response(op.Errors[i].Headers, op.Errors[i].Payload)...)
		}
		if op.Default != nil {
			diags = append(diags, r.response(op.Default.Headers, op.Default.Payload)...)
		}
	}
	return diags
}

// response fills the replacements of a response's headers, hs, and of those its
// body, p, declares.
func (r *resolver) response(hs []ir.Header, p *ir.Payload) []ir.Diagnostic {
	return append(r.headers(hs), r.payload(p)...)
}

// payload fills the replacements of the headers each media type of p declares
// for a part or a stream's items, each among the headers beside it. A part's
// encoding is a map value, whose Headers still share their array with p.
func (r *resolver) payload(p *ir.Payload) []ir.Diagnostic {
	if p == nil {
		return nil
	}
	var diags []ir.Diagnostic
	for i := range p.Contents {
		c := &p.Contents[i]
		if c.ItemEncoding != nil {
			diags = append(diags, r.headers(c.ItemEncoding.Headers)...)
		}
		for _, part := range slices.Sorted(maps.Keys(c.Encoding)) {
			diags = append(diags, r.headers(c.Encoding[part].Headers)...)
		}
	}
	return diags
}

// params fills each parameter's replacements, among op's parameters.
func (r *resolver) params(op *ir.Operation) []ir.Diagnostic {
	param := r.paramFinder(op)
	diags := make([]ir.Diagnostic, 0, len(op.Params))
	for i := range op.Params {
		p := &op.Params[i]
		diags = append(diags, r.fill(p.Unmodeled, p.Deprecation, &p.Provenance, ir.EntityRef{Param: p.ID}, "parameter", param)...)
	}
	return diags
}

// paramFinder returns what finds a parameter of op by name, as a link's key
// names one (whole.ParamIndex.Named).
func (r *resolver) paramFinder(op *ir.Operation) func(string) (ir.EntityRef, int) {
	return func(name string) (ir.EntityRef, int) {
		id, matched := r.w.Params(op).Named(name)
		return ir.EntityRef{Param: id}, matched
	}
}
