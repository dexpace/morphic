// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"fmt"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/servers"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/ir"
)

// Resolve lowers every link l holds onto the response carrying it, now that
// compiledDocumentView, the document, is whole, and registers the server a link
// names in srv. A link whose target, parameters or values do not all resolve is
// kept verbatim on that response instead, with the finding that says why: a
// link is lowered whole or not at all, so no consumer reads one missing a value
// its source states. serviceContext is the source's context.
func (l *Links) Resolve(serviceContext service.Ctx, compiledDocumentView *compileddoc.CompiledDocumentView, srv *servers.Registry) []ir.Diagnostic {
	if len(l.pending) == 0 {
		return nil
	}
	ix := indexOperations(serviceContext, compiledDocumentView.Operations)
	var diags []ir.Diagnostic
	for i := range l.pending {
		pendingLink := &l.pending[i]
		from, ok := ix.byID[pendingLink.from]
		if !ok {
			diags = append(diags, pendingLink.draft.at.DiagAt(ir.SeverityError, diag.InternalInvariant, pendingLink.draft.use,
				"link %q sits on a response of operation %q, which the service does not hold", pendingLink.draft.name, pendingLink.from))
			continue
		}
		diags = append(diags, place(ix, compiledDocumentView, srv, from, pendingLink)...)
	}
	return diags
}

// place lowers pendingLinkValue onto the response of from it sits on, or keeps
// it verbatim there.
func place(ix operationIndex, compiledDocumentView *compileddoc.CompiledDocumentView, srv *servers.Registry, from *ir.Operation, pendingLinkValue *pendingLink) []ir.Diagnostic {
	linkDraft := &pendingLinkValue.draft
	at := pendingLinkValue.slot.in(from)
	link, failed := resolveLink(ix, compiledDocumentView, from, at, linkDraft)
	if failed != nil {
		kept, keepDiags := linkDraft.keep(at.unmodeled)
		format := "link %q %s"
		if kept {
			format += "; it is kept verbatim under Unmodeled"
		}
		return append([]ir.Diagnostic{linkDraft.at.DiagAt(failed.severity, failed.code, linkDraft.use, format, linkDraft.name, failed.reason)}, keepDiags...)
	}
	var diags []ir.Diagnostic
	if linkDraft.server != nil {
		id, serverDiags := srv.At(linkDraft.c, linkDraft.server, linkDraft.ptr+ids.Ptr("server"))
		link.Server = &id
		diags = serverDiags
	}
	*at.links = append(*at.links, link)
	return diags
}

// responseView is what resolving a link reads of the response carrying it, and
// where the link is written. key names the response, which
// compileddoc.CompiledDocumentView indexes its headers under.
type responseView struct {
	key       compileddoc.ResponseKey
	links     *[]ir.OperationLink
	unmodeled *ir.Unmodeled
	payload   *ir.Payload
	headers   []ir.Header
}

// in returns the response s names on operation. add took s as the response was
// appended, so the response is there.
func (s responseSlot) in(operation *ir.Operation) responseView {
	key := compileddoc.ResponseKey{Operation: operation.ID, List: s.list, Index: s.index}
	switch s.list {
	case compileddoc.ListErrors:
		errorCase := &operation.Errors[s.index]
		return responseView{key: key, links: &errorCase.Links, unmodeled: &errorCase.Unmodeled, payload: errorCase.Payload, headers: errorCase.Headers}
	case compileddoc.ListDefault:
		response := operation.Default
		return responseView{key: key, links: &response.Links, unmodeled: &response.Unmodeled, payload: response.Payload, headers: response.Headers}
	default:
		response := &operation.Responses[s.index]
		return responseView{key: key, links: &response.Links, unmodeled: &response.Unmodeled, payload: response.Payload, headers: response.Headers}
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

// resolveLink resolves linkDraftValue, which a response of from carries, at.
func resolveLink(ix operationIndex, compiledDocumentView *compileddoc.CompiledDocumentView, from *ir.Operation, at responseView, linkDraftValue *linkDraft) (ir.OperationLink, *linkFailure) {
	target, failed := ix.target(linkDraftValue)
	if failed != nil {
		return ir.OperationLink{}, failed
	}
	link := ir.OperationLink{
		Name: ir.NamingFor(linkDraftValue.name), Operation: target.ID, Docs: linkDraftValue.docs,
		Provenance: linkDraftValue.c.Place().At(linkDraftValue.ptr), Unmodeled: linkDraftValue.extra,
	}
	ex := exchange{w: compiledDocumentView, from: from, at: at}
	given := map[ir.ParameterID]string{}
	for _, dp := range linkDraftValue.params {
		id, failed := targetParam(compiledDocumentView, target, dp.key)
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
	if linkDraftValue.body != nil {
		body, failed := ex.value(linkDraftValue.body)
		if failed != nil {
			return ir.OperationLink{}, failed
		}
		link.Body = &body
	}
	return link, nil
}

// operationIndex finds the operations of a service by ID, by operationId, and by
// where the source declares them.
type operationIndex struct {
	byID   map[ir.OperationID]*ir.Operation
	byName map[string][]*ir.Operation
	byDecl map[jsontext.Pointer][]*ir.Operation
}

// indexOperations indexes operations, the document's operations
// (compileddoc.CompiledDocumentView.Ops). byDecl holds those the source
// declares, whose provenance names serviceContext's source.
func indexOperations(serviceContext service.Ctx, operations []*ir.Operation) operationIndex {
	ix := operationIndex{byID: map[ir.OperationID]*ir.Operation{}, byName: map[string][]*ir.Operation{}, byDecl: map[jsontext.Pointer][]*ir.Operation{}}
	source := serviceContext.Place().Source
	for _, operation := range operations {
		ix.add(operation, source)
	}
	return ix
}

// add indexes operation, declared in source's document when its provenance says so.
func (ix operationIndex) add(operation *ir.Operation, source int) {
	ix.byID[operation.ID] = operation
	if name := operation.Name.Source; name != "" {
		ix.byName[name] = append(ix.byName[name], operation)
	}
	if operation.Provenance.Source == source {
		ix.byDecl[operation.Provenance.Pointer] = append(ix.byDecl[operation.Provenance.Pointer], operation)
	}
}

// target returns the operation linkDraftValue leads to, which it names by
// operationId or by operationRef, never both.
func (ix operationIndex) target(linkDraftValue *linkDraft) (*ir.Operation, *linkFailure) {
	switch {
	case linkDraftValue.operationID != nil && linkDraftValue.operationRef != nil:
		return nil, invalid("names its operation by both operationId and operationRef")
	case linkDraftValue.operationID != nil:
		return one(ix.byName[*linkDraftValue.operationID], "operationId", *linkDraftValue.operationID)
	case linkDraftValue.operationRef != nil:
		return ix.byReference(linkDraftValue)
	default:
		return nil, invalid("names no operation")
	}
}

// byReference returns the operation linkDraftValue's operationRef names: the
// one mounted at the position it points to, else the one declared there. A
// reference whose fragment is no JSON pointer, a bare `#` among them, names no
// operation; one into another document names an operation this compile does not
// lower; and a declaration mounted more than once names several.
func (ix operationIndex) byReference(linkDraftValue *linkDraft) (*ir.Operation, *linkFailure) {
	ref := *linkDraftValue.operationRef
	if _, ok := componentlookup.FragmentPointer(ref); !ok {
		return nil, invalid("names operationRef %q, which is no JSON pointer to an operation", ref)
	}
	pointer, internal := linkDraftValue.c.RefScope().InternalPointer(ref)
	if !internal {
		return nil, untyped("names operation %q, which is not one this document declares", ref)
	}
	if operation, mounted := ix.byID[ids.Operation(pointer)]; mounted {
		return operation, nil
	}
	declared := ix.byDecl[pointer]
	if len(declared) > 1 {
		return nil, untyped("names operation %q, which is mounted %d times", ref, len(declared))
	}
	return one(declared, "operationRef", ref)
}

// one returns the one operation of operations, which a link's field names by spelled.
func one(operations []*ir.Operation, field, spelled string) (*ir.Operation, *linkFailure) {
	switch len(operations) {
	case 1:
		return operations[0], nil
	case 0:
		return nil, invalid("names %s %q, which no operation of the document declares", field, spelled)
	default:
		return nil, invalid("names %s %q, which %d operations declare", field, spelled, len(operations))
	}
}
