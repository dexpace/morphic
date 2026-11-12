// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"fmt"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/servers"
	"github.com/dexpace/morphic/ir"
)

// Resolve lowers every link l holds onto the response carrying it, now that
// svc holds every operation and reg is whole, and registers the server a link
// names in srv. A link whose target, parameters or values do not all resolve
// is kept verbatim on that response instead, with the finding that says why:
// a link is lowered whole or not at all, so no consumer reads one missing a
// value its source states. c is the source's context.
func (l *Links) Resolve(c lowering.Ctx, svc *ir.Service, reg ir.TypeRegistry, srv *servers.Registry) []ir.Diagnostic {
	if len(l.pending) == 0 {
		return nil
	}
	ix := indexOperations(c, svc)
	var diags []ir.Diagnostic
	for i := range l.pending {
		p := &l.pending[i]
		from, ok := ix.byID[p.from]
		if !ok {
			diags = append(diags, p.draft.at.DiagAt(ir.SeverityError, diag.InternalInvariant, p.draft.use,
				"link %q sits on a response of operation %q, which the service does not hold", p.draft.name, p.from))
			continue
		}
		diags = append(diags, place(ix, reg, srv, from, p)...)
	}
	return diags
}

// place lowers p onto the response of from it sits on, or keeps it verbatim
// there.
func place(ix opIndex, reg ir.TypeRegistry, srv *servers.Registry, from *ir.Operation, p *pendingLink) []ir.Diagnostic {
	d := &p.draft
	at := p.slot.in(from)
	link, failed := resolveLink(ix, reg, from, at, d)
	if failed != nil {
		kept, keepDiags := d.keep(at.unmodeled)
		format := "link %q %s"
		if kept {
			format += "; it is kept verbatim under Unmodeled"
		}
		return append([]ir.Diagnostic{d.at.DiagAt(failed.severity, failed.code, d.use, format, d.name, failed.reason)}, keepDiags...)
	}
	var diags []ir.Diagnostic
	if d.server != nil {
		id, serverDiags := srv.At(d.c, d.server, d.ptr+ids.Ptr("server"))
		link.Server = &id
		diags = serverDiags
	}
	*at.links = append(*at.links, link)
	return diags
}

// responseView is what resolving a link reads of the response carrying it, and
// where the link is written.
type responseView struct {
	links     *[]ir.OperationLink
	unmodeled *ir.Unmodeled
	payload   *ir.Payload
	headers   []ir.Header
}

// in returns the response s names on op. add took s as the response was
// appended, so the response is there.
func (s responseSlot) in(op *ir.Operation) responseView {
	switch s.list {
	case slotErrors:
		e := &op.Errors[s.index]
		return responseView{links: &e.Links, unmodeled: &e.Unmodeled, payload: e.Payload, headers: e.Headers}
	case slotDefault:
		r := op.Default
		return responseView{links: &r.Links, unmodeled: &r.Unmodeled, payload: r.Payload, headers: r.Headers}
	default:
		r := &op.Responses[s.index]
		return responseView{links: &r.Links, unmodeled: &r.Unmodeled, payload: r.Payload, headers: r.Headers}
	}
}

// linkFailure is why a link does not lower: the finding's code and severity,
// and the reason, which completes "link <name> ...".
type linkFailure struct {
	code     string
	severity ir.Severity
	reason   string
}

// invalid is a link naming nothing the document declares, naming it
// ambiguously, or breaking a grammar.
func invalid(format string, args ...any) *linkFailure {
	return &linkFailure{code: diag.InvalidLink, severity: ir.SeverityWarning, reason: fmt.Sprintf(format, args...)}
}

// untyped is a sound link reading what no ir.LinkValue spells.
func untyped(format string, args ...any) *linkFailure {
	return &linkFailure{code: diag.UntypedLink, severity: ir.SeverityInfo, reason: fmt.Sprintf(format, args...)}
}

// resolveLink resolves d, which a response of from carries, at.
func resolveLink(ix opIndex, reg ir.TypeRegistry, from *ir.Operation, at responseView, d *linkDraft) (ir.OperationLink, *linkFailure) {
	target, failed := ix.target(d)
	if failed != nil {
		return ir.OperationLink{}, failed
	}
	link := ir.OperationLink{
		Name: compile.NamingFor(d.name), Operation: target.ID, Docs: d.docs,
		Provenance: d.c.Place().At(d.ptr), Unmodeled: d.extra,
	}
	ex := exchange{reg: reg, from: from, at: at}
	given := map[ir.ParamID]string{}
	for _, dp := range d.params {
		id, failed := targetParam(target, dp.key)
		if failed != nil {
			return ir.OperationLink{}, failed
		}
		if first, twice := given[id]; twice {
			return ir.OperationLink{}, invalid("gives one parameter a value as %q and again as %q", first, dp.key)
		}
		given[id] = dp.key
		value, failed := ex.value(dp.value)
		if failed != nil {
			return ir.OperationLink{}, failed
		}
		link.Params = append(link.Params, ir.LinkParam{Param: id, Value: value})
	}
	if d.body != nil {
		body, failed := ex.value(d.body)
		if failed != nil {
			return ir.OperationLink{}, failed
		}
		link.Body = &body
	}
	return link, nil
}

// opIndex finds the operations of a service by ID, by operationId, and by
// where the source declares them.
type opIndex struct {
	byID   map[ir.OpID]*ir.Operation
	byName map[string][]*ir.Operation
	byDecl map[jsontext.Pointer][]*ir.Operation
}

// indexOperations indexes every operation svc holds, nested groups included,
// to ir.MaxGroupDepth. byDecl holds those the source declares, whose
// provenance names c's source.
func indexOperations(c lowering.Ctx, svc *ir.Service) opIndex {
	ix := opIndex{byID: map[ir.OpID]*ir.Operation{}, byName: map[string][]*ir.Operation{}, byDecl: map[jsontext.Pointer][]*ir.Operation{}}
	source := c.Place().Source
	type level struct {
		groups []ir.OperationGroup
		depth  int
	}
	stack := []level{{groups: svc.Groups}}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top.depth > ir.MaxGroupDepth {
			continue
		}
		for gi := range top.groups {
			g := &top.groups[gi]
			for oi := range g.Operations {
				ix.add(&g.Operations[oi], source)
			}
			stack = append(stack, level{groups: g.Groups, depth: top.depth + 1})
		}
	}
	return ix
}

// add indexes op, declared in source's document when its provenance says so.
func (ix opIndex) add(op *ir.Operation, source int) {
	ix.byID[op.ID] = op
	if name := op.Name.Source; name != "" {
		ix.byName[name] = append(ix.byName[name], op)
	}
	if op.Provenance.Source == source {
		ix.byDecl[op.Provenance.Pointer] = append(ix.byDecl[op.Provenance.Pointer], op)
	}
}

// target returns the operation d leads to, which it names by operationId or by
// operationRef, never both.
func (ix opIndex) target(d *linkDraft) (*ir.Operation, *linkFailure) {
	switch {
	case d.operationID != nil && d.operationRef != nil:
		return nil, invalid("names its operation by both operationId and operationRef")
	case d.operationID != nil:
		return one(ix.byName[*d.operationID], "operationId", *d.operationID)
	case d.operationRef != nil:
		return ix.byReference(d)
	default:
		return nil, invalid("names no operation")
	}
}

// byReference returns the operation d's operationRef names: the one mounted at
// the position it points to, else the one declared there. A reference into
// another document names an operation this compile does not lower, and a
// declaration mounted more than once names several.
func (ix opIndex) byReference(d *linkDraft) (*ir.Operation, *linkFailure) {
	ref := *d.operationRef
	pointer, internal := d.c.RefScope().InternalPointer(ref)
	if !internal {
		return nil, untyped("names operation %q, which is not one this document declares", ref)
	}
	if op, mounted := ix.byID[ids.Op(pointer)]; mounted {
		return op, nil
	}
	declared := ix.byDecl[pointer]
	if len(declared) > 1 {
		return nil, untyped("names operation %q, which is mounted %d times", ref, len(declared))
	}
	return one(declared, "operationRef", ref)
}

// one returns the one operation of ops, which a link's field names by spelled.
func one(ops []*ir.Operation, field, spelled string) (*ir.Operation, *linkFailure) {
	switch len(ops) {
	case 1:
		return ops[0], nil
	case 0:
		return nil, invalid("names %s %q, which no operation of the document declares", field, spelled)
	default:
		return nil, invalid("names %s %q, which %d operations declare", field, spelled, len(ops))
	}
}
