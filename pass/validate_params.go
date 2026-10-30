package pass

import (
	"fmt"
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// paramIDType is the reflect.Type of ir.ParamID, the class of identity a
// parameter declares.
var paramIDType = reflect.TypeFor[ir.ParamID]()

// checkParams holds every reference an operation makes to a parameter — its
// HTTP bindings, its pagination inputs and its idempotency token — to the
// parameters it declares. refs holds what the one walk of the document
// declared, which the dangling check resolves against too.
func checkParams(doc *ir.Document, refs *ir.IDReferences) []ir.Diagnostic {
	return append(checkParamBindings(doc, refs), checkParamReferences(doc, refs)...)
}

// danglingWalkSkips reports whether the dangling walk is silent on a reference
// to id, so that naming no parameter of its own operation is reported here. The
// walk resolves a declared ID, another operation's or a field argument's
// (Property.Args), and skips an empty one; it reports one the document declares
// nowhere, once. Past the walk's depth bound neither reports it, and
// ir/walk-truncated says why.
func danglingWalkSkips(id ir.ParamID, refs *ir.IDReferences) bool {
	return id == "" || refs.Declares(paramIDType, string(id))
}

// checkParamBindings validates each HTTP binding's parameter bindings against
// the operation's logical parameters. Every operation and binding is read
// through the same maps, so none costs a map of its own.
func checkParamBindings(doc *ir.Document, refs *ir.IDReferences) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var read bindingMaps
	ir.ForEachOperation(doc, func(op ir.Operation) {
		names := read.names.emptied()
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
	names       reusedMap[ir.ParamID, string]
	bound       reusedMap[ir.ParamID, int]
	perLocation reusedMap[paramLocation, int]
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
// error unless the dangling walk reports it (danglingWalkSkips), a param bound
// twice in one non-host location is an error, and an unbound parameter is a
// warning (body-carried operations bind nothing). The binding's location is
// spelled only for a diagnostic.
func checkHTTPParamBinding(op ir.Operation, idx int, names map[ir.ParamID]string, refs *ir.IDReferences,
	read *bindingMaps,
) []ir.Diagnostic {
	b := op.Bindings.HTTP[idx]
	var diags []ir.Diagnostic
	bound, perLocation := read.bound.emptied(), read.perLocation.emptied()
	for _, pb := range b.ParamBindings {
		name, own := names[pb.Param]
		if !own {
			if danglingWalkSkips(pb.Param, refs) {
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
// bindings, with the field of the operation it sits in, which spells where it
// is reported.
type paramRef struct {
	id    ir.ParamID
	field string
}

// appendParamRefs appends to dst the parameter references op carries besides
// its bindings: the pagination inputs and the idempotency token. An absent
// input is no reference, and neither is an empty token, which is how an
// operation that is not token-keyed spells having none. An input with an empty
// ID is one.
func appendParamRefs(dst []paramRef, op ir.Operation) []paramRef {
	if pg := op.Pagination; pg != nil {
		if pg.InputCursor != nil {
			dst = append(dst, paramRef{pg.InputCursor.Param, "pagination/inputCursor"})
		}
		if pg.InputLimit != nil {
			dst = append(dst, paramRef{pg.InputLimit.Param, "pagination/inputLimit"})
		}
	}
	if token := op.Idempotency.TokenParam; token != "" {
		dst = append(dst, paramRef{token, "idempotency/tokenParam"})
	}
	return dst
}

// checkParamReferences holds an operation's pagination inputs and idempotency
// token to the parameters that operation declares.
//
// Another operation's parameter is a well-formed reference, so the registry walk
// has nothing to say about it, while an emitter reads the pagination cursor off
// the wrong operation's parameter. What the dangling walk reports is left to it
// (danglingWalkSkips). Every operation is read through the same map and list,
// so none costs one of its own.
func checkParamReferences(doc *ir.Document, refs *ir.IDReferences) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var declared reusedMap[ir.ParamID, bool]
	var carried []paramRef
	ir.ForEachOperation(doc, func(op ir.Operation) {
		own := declared.emptied()
		for _, p := range op.Params {
			own[p.ID] = true
		}
		carried = appendParamRefs(carried[:0], op)
		for _, ref := range carried {
			if own[ref.id] || !danglingWalkSkips(ref.id, refs) {
				continue
			}
			where := string(op.ID) + "/" + ref.field
			diags = append(diags, diag(ir.SeverityError, "pass/param-binding-mismatch",
				fmt.Sprintf("%s names parameter %q, which this operation does not declare", where, ref.id),
				where))
		}
	})
	return diags
}
