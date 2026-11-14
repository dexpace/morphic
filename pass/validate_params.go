// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/reuse"
)

// paramIDType is the reflect.Type of ir.ParamID, the class of identity a
// parameter declares.
var paramIDType = reflect.TypeFor[ir.ParamID]()

// checkParams holds every reference an operation makes to a parameter — its
// HTTP bindings, its pagination inputs, the inputs selecting a stream, its
// idempotency token and what its links read of the request — to the
// parameters it declares, and each link's parameters to the operation it leads
// to. refs holds what the one walk of the document declared, which the
// dangling check resolves against too.
func checkParams(doc *ir.Document, refs *ir.IDReferences) []ir.Diagnostic {
	diags := append(checkParamBindings(doc, refs), checkParamReferences(doc, refs)...)
	return append(diags, checkLinkTargets(doc, refs)...)
}

// reportedHere reports whether a reference to id that names no parameter of its
// own operation is reported here: one the document declares, another
// operation's or a field argument's (Property.Args). The dangling walk reports
// one the document declares nowhere, and checkEmptyRefs an empty one, each
// once. Past the walk's bounds none reports it, and ir/walk-truncated says
// why.
func reportedHere(id ir.ParamID, refs *ir.IDReferences) bool {
	return id != "" && refs.Declares(paramIDType, string(id))
}

// checkParamBindings validates each HTTP binding's parameter bindings against
// the operation's logical parameters. Every operation and binding is read
// through the same maps, so none costs a map of its own.
func checkParamBindings(doc *ir.Document, refs *ir.IDReferences) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var read bindingMaps
	ir.ForEachOperation(doc, func(op ir.Operation) {
		names := read.names.Emptied()
		for _, p := range op.Params {
			names[p.ID] = p.Name.Source
		}
		for i := range op.Bindings.HTTP {
			diags = append(diags, checkHTTPParamBinding(op, i, names, refs, &read)...)
		}
	})
	return diags
}

// bindingMaps are what checkParamBindings reads an operation and each of its
// HTTP bindings through.
type bindingMaps struct {
	names       reuse.Map[ir.ParamID, string]
	bound       reuse.Map[ir.ParamID, int]
	perLocation reuse.Map[paramLocation, int]
}

// paramLocation keys the per-location bound-once count: a parameter is
// identified by its ID, not its name, so two parameters sharing a name never
// count against each other.
type paramLocation struct {
	param    ir.ParamID
	location ir.HTTPLocation
}

// checkHTTPParamBinding validates HTTP binding idx of op, whose parameters'
// names names holds by ID: a parameter this operation does not declare is an
// error unless another family reports it (reportedHere), a param bound
// twice in one non-host location is an error, and an unbound parameter is a
// warning (body-carried operations bind nothing). The binding's location is
// spelled only for a diagnostic.
func checkHTTPParamBinding(op ir.Operation, idx int, names map[ir.ParamID]string, refs *ir.IDReferences,
	read *bindingMaps,
) []ir.Diagnostic {
	b := op.Bindings.HTTP[idx]
	var diags []ir.Diagnostic
	bound, perLocation := read.bound.Emptied(), read.perLocation.Emptied()
	for _, pb := range b.ParamBindings {
		name, own := names[pb.Param]
		if !own {
			if reportedHere(pb.Param, refs) {
				where := bindingWhere(op.ID, idx)
				diags = append(diags, diag(ir.SeverityError, "pass/param-binding-mismatch",
					fmt.Sprintf("binding on %s names parameter %q, which this operation does not declare", where, pb.Param), where))
			}
			continue
		}
		bound[pb.Param]++
		if pb.Location == ir.HTTPLocationHost {
			continue // host labels are additive; a param may fill several.
		}
		key := paramLocation{param: pb.Param, location: pb.Location}
		if perLocation[key]++; perLocation[key] == 2 {
			where := bindingWhere(op.ID, idx)
			diags = append(diags, diag(ir.SeverityError, "pass/param-binding-mismatch",
				fmt.Sprintf("parameter %q is bound more than once in location %q on %s", name, pb.Location, where),
				where))
		}
	}
	return append(diags, unboundParamWarnings(op, idx, bound)...)
}

// bindingWhere spells where HTTP binding idx of operation op sits, for a
// diagnostic to be placed there.
func bindingWhere(op ir.OpID, idx int) string {
	return fmt.Sprintf("%s/bindings/http/%d", op, idx)
}

// unboundParamWarnings reports each logical parameter that HTTP binding idx of
// op placed nowhere on the wire.
func unboundParamWarnings(op ir.Operation, idx int, bound map[ir.ParamID]int) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, p := range op.Params {
		if bound[p.ID] == 0 {
			where := bindingWhere(op.ID, idx)
			diags = append(diags, diag(ir.SeverityWarning, "pass/param-binding-mismatch",
				fmt.Sprintf("parameter %q on %s is not bound to any HTTP location", p.Name.Source, where),
				where))
		}
	}
	return diags
}

// paramRef is one reference an operation makes to a parameter outside its HTTP
// bindings, with where it sits: the field of the operation it is in, or, for a
// key of a scenario's input, no field and the index of the example holding it.
type paramRef struct {
	id      ir.ParamID
	field   string
	example int
}

// where spells where r sits in operation op, for a report.
func (r paramRef) where(op ir.OpID) string {
	if r.field == "" {
		return string(op) + "/examples/" + strconv.Itoa(r.example) + "/input"
	}
	return string(op) + "/" + r.field
}

// appendParamRefs appends to dst the parameter references op carries besides
// its bindings: the pagination inputs, the inputs that select a stream, the
// idempotency token, the request values its links read and the keys of each
// scenario's input, each input's in sorted order so no map order reaches a
// report. An absent input is no reference, and neither is one into the request
// body, nor an empty token, which is how an operation that is not token-keyed
// spells having none. An input with an empty ID is one.
func appendParamRefs(dst []paramRef, op ir.Operation) []paramRef {
	if pg := op.Pagination; pg != nil {
		dst = appendInputRef(dst, pg.InputCursor, "pagination/inputCursor")
		dst = appendInputRef(dst, pg.InputLimit, "pagination/inputLimit")
	}
	if s := op.RequestStream; s != nil && s.Condition != nil {
		dst = appendInputRef(dst, &s.Condition.Input, "requestStream/condition")
	}
	if s := op.ResponseStream; s != nil && s.Condition != nil {
		dst = appendInputRef(dst, &s.Condition.Input, "responseStream/condition")
	}
	if token := op.Idempotency.TokenParam; token != "" {
		dst = append(dst, paramRef{id: token, field: "idempotency/tokenParam"})
	}
	dst = appendLinkInputRefs(dst, &op)
	for i := range op.Examples {
		in := op.Examples[i].Input
		if in == nil {
			continue
		}
		first := len(dst)
		for id := range in.Params {
			dst = append(dst, paramRef{id: id, example: i})
		}
		slices.SortFunc(dst[first:], func(a, b paramRef) int { return strings.Compare(string(a.id), string(b.id)) })
	}
	return dst
}

// appendInputRef appends the parameter in names, under field, when in roots in
// a parameter.
func appendInputRef(dst []paramRef, in *ir.InputPath, field string) []paramRef {
	if in == nil || in.Param == nil {
		return dst
	}
	return append(dst, paramRef{id: *in.Param, field: field})
}

// checkParamReferences holds the references appendParamRefs lists to the
// parameters the operation carrying them declares.
//
// Another operation's parameter is a well-formed reference, so the registry walk
// has nothing to say about it, while an emitter reads the pagination cursor off
// the wrong operation's parameter. What another family reports is left to it
// (reportedHere). Every operation is read through the same map and list,
// so none costs one of its own.
func checkParamReferences(doc *ir.Document, refs *ir.IDReferences) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var declared reuse.Map[ir.ParamID, bool]
	var carried []paramRef
	ir.ForEachOperation(doc, func(op ir.Operation) {
		own := declared.Emptied()
		for _, p := range op.Params {
			own[p.ID] = true
		}
		carried = appendParamRefs(carried[:0], op)
		for _, ref := range carried {
			if own[ref.id] || !reportedHere(ref.id, refs) {
				continue
			}
			where := ref.where(op.ID)
			diags = append(diags, diag(ir.SeverityError, "pass/param-binding-mismatch",
				fmt.Sprintf("%s names parameter %q, which this operation does not declare", where, ref.id),
				where))
		}
	})
	return diags
}
