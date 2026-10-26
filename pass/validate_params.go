package pass

import (
	"fmt"
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// checkParams holds every reference an operation makes to a parameter — its
// HTTP bindings, its pagination inputs and its idempotency token — to the
// parameters it declares. decls is what the one walk of the document declared,
// which the dangling check resolves against too.
func checkParams(doc *ir.Document, decls []ir.IDDeclaration) []ir.Diagnostic {
	declared := declaredParamIDs(decls)
	return append(checkParamBindings(doc, declared), checkParamReferences(doc, declared)...)
}

// declaredParamIDs returns the ID of every parameter decls holds: an
// operation's, and a field argument's (Property.Args) too, since the dangling
// walk resolves a reference against both.
func declaredParamIDs(decls []ir.IDDeclaration) map[ir.ParamID]bool {
	paramClass := reflect.TypeFor[ir.ParamID]()
	declared := map[ir.ParamID]bool{}
	for _, d := range decls {
		if d.Class == paramClass {
			declared[ir.ParamID(d.ID)] = true
		}
	}
	return declared
}

// danglingWalkSkips reports whether the dangling walk is silent on a reference
// to id, so that naming no parameter of its own operation is reported here. The
// walk resolves a declared ID, another operation's or a field argument's, and
// skips an empty one; it reports one the document declares nowhere, once. Past
// the walk's depth bound neither reports it, and ir/walk-truncated says why.
func danglingWalkSkips(id ir.ParamID, declared map[ir.ParamID]bool) bool {
	return id == "" || declared[id]
}

// checkParamBindings validates each HTTP binding's parameter bindings against
// the operation's logical parameters.
func checkParamBindings(doc *ir.Document, declared map[ir.ParamID]bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ir.ForEachOperation(doc, func(op ir.Operation) {
		for i := range op.Bindings.HTTP {
			diags = append(diags, checkHTTPParamBinding(op, i, declared)...)
		}
	})
	return diags
}

// paramLocation keys the per-location bound-once count: a parameter is
// identified by its ID, not its name, so two parameters sharing a name never
// count against each other.
type paramLocation struct {
	param    ir.ParamID
	location ir.HTTPLocation
}

// checkHTTPParamBinding validates one HTTP binding: a parameter this operation
// does not declare is an error unless the dangling walk reports it
// (danglingWalkSkips), a param bound twice in one non-host location is an
// error, and an unbound parameter is a warning (body-carried operations bind
// nothing).
func checkHTTPParamBinding(op ir.Operation, idx int, declared map[ir.ParamID]bool) []ir.Diagnostic {
	b := op.Bindings.HTTP[idx]
	where := fmt.Sprintf("%s/bindings/http/%d", op.ID, idx)
	names := make(map[ir.ParamID]string, len(op.Params))
	for _, p := range op.Params {
		names[p.ID] = p.Name.Source
	}
	var diags []ir.Diagnostic
	bound := make(map[ir.ParamID]int, len(op.Params))
	perLocation := make(map[paramLocation]int, len(b.ParamBindings))
	for _, pb := range b.ParamBindings {
		name, own := names[pb.Param]
		if !own {
			if danglingWalkSkips(pb.Param, declared) {
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
			diags = append(diags, diag(ir.SeverityError, "pass/param-binding-mismatch",
				fmt.Sprintf("parameter %q is bound more than once in location %q on %s", name, pb.Location, where),
				where))
		}
	}
	return append(diags, unboundParamWarnings(op, bound, where)...)
}

// unboundParamWarnings reports each logical parameter that no binding placed on
// the wire.
func unboundParamWarnings(op ir.Operation, bound map[ir.ParamID]int, where string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, p := range op.Params {
		if bound[p.ID] == 0 {
			diags = append(diags, diag(ir.SeverityWarning, "pass/param-binding-mismatch",
				fmt.Sprintf("parameter %q on %s is not bound to any HTTP location", p.Name.Source, where),
				where))
		}
	}
	return diags
}

// paramRef is one reference an operation makes to a parameter outside its HTTP
// bindings, with the location it is reported at.
type paramRef struct {
	id    ir.ParamID
	where string
}

// operationParamRefs lists the parameter references op carries besides its
// bindings: the pagination inputs and the idempotency token. An absent input is
// no reference, and neither is an empty token, which is how an operation that
// is not token-keyed spells having none. An input with an empty ID is one.
func operationParamRefs(op ir.Operation) []paramRef {
	var refs []paramRef
	if pg := op.Pagination; pg != nil {
		if pg.InputCursor != nil {
			refs = append(refs, paramRef{pg.InputCursor.Param, fmt.Sprintf("%s/pagination/inputCursor", op.ID)})
		}
		if pg.InputLimit != nil {
			refs = append(refs, paramRef{pg.InputLimit.Param, fmt.Sprintf("%s/pagination/inputLimit", op.ID)})
		}
	}
	if token := op.Idempotency.TokenParam; token != "" {
		refs = append(refs, paramRef{token, fmt.Sprintf("%s/idempotency/tokenParam", op.ID)})
	}
	return refs
}

// checkParamReferences holds an operation's pagination inputs and idempotency
// token to the parameters that operation declares.
//
// Another operation's parameter is a well-formed reference, so the registry walk
// has nothing to say about it, while an emitter reads the pagination cursor off
// the wrong operation's parameter. What the dangling walk reports is left to it
// (danglingWalkSkips).
func checkParamReferences(doc *ir.Document, declared map[ir.ParamID]bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ir.ForEachOperation(doc, func(op ir.Operation) {
		own := make(map[ir.ParamID]bool, len(op.Params))
		for _, p := range op.Params {
			own[p.ID] = true
		}
		for _, ref := range operationParamRefs(op) {
			if own[ref.id] || !danglingWalkSkips(ref.id, declared) {
				continue
			}
			diags = append(diags, diag(ir.SeverityError, "pass/param-binding-mismatch",
				fmt.Sprintf("%s names parameter %q, which this operation does not declare", ref.where, ref.id),
				ref.where))
		}
	})
	return diags
}
