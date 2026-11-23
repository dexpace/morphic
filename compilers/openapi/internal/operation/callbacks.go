// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"context"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/ir"
)

// maxCallbackNesting bounds the levels of callbacks that lower below a path's
// or a webhook's operation: 4, so an operation nested in fewer callbacks lowers
// its own, and one nested in 4 keeps them verbatim. It stops a callback that
// names itself through a $ref from lowering without end, while admitting an
// exchange of several callbacks in turn.
const maxCallbackNesting = 4

// operationCallbacks lowers src's callbacks for the operation operationCtx
// describes (lowerCallbacks) while it is nested in fewer than
// maxCallbackNesting callbacks. Past that the map is kept verbatim, beside the
// callbacks' own extensions, and reported
// (docs/compilers/openapi.md#callbacks).
func operationCallbacks(serviceContext service.Ctx, loweringState operationLoweringState, src *soa.Operation, operationCtx operationContext) ([]ir.Callback, []ir.Operation, ir.Unmodeled, []ir.Diagnostic) {
	if src.GetCallbacks().Len() == 0 {
		return nil, nil, nil, nil
	}
	if operationCtx.nesting < maxCallbackNesting {
		return lowerCallbacks(serviceContext, loweringState, src, operationCtx)
	}
	at := operationCtx.ptrs.decl + ids.Ptr("callbacks")
	var kept ir.Unmodeled
	written, diags := annotation.PreserveNodeInto(&kept, annotation.EntryKey("", "callbacks"),
		nodeview.Child(src.GetRootNode(), "callbacks"), ir.ReasonDegradedLowering, serviceContext.Place().At(at))
	if written {
		schema.RecordReached(serviceContext.Ctx, at)
		diags = append(diags, serviceContext.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
			"callbacks nested deeper than %d levels are not lowered; they are kept verbatim on the HTTP binding "+
				"of the operation declaring them", maxCallbackNesting))
	}
	return nil, nil, kept, diags
}

// lowerCallbacks lowers each callback expression's path-item operations as
// Operations registered in the parent's group, whatever their own tags or
// grouping extension state (docs/compilers/openapi.md#callbacks), and binds
// them to the parent via HTTPBinding.Callbacks keyed by the runtime expression
// (docs/ir/http-binding.md). parent.ptrs.mount roots callback operation
// identity, so two parents sharing one $ref'd callback keep distinct callback
// operations; parent.ptrs.decl is the base a $ref'd callback or path item
// resolves against (issue #107).
func lowerCallbacks(serviceContext service.Ctx, loweringState operationLoweringState, src *soa.Operation, parent operationContext) ([]ir.Callback, []ir.Operation, ir.Unmodeled, []ir.Diagnostic) {
	var callbacks []ir.Callback
	var operations []ir.Operation
	var ext ir.Unmodeled
	var diags []ir.Diagnostic
	for cbName, rcb := range src.GetCallbacks().All() {
		use := parent.ptrs.decl + ids.Ptr("callbacks", cbName)
		// A callback has no docs, so a summary or description beside its $ref is
		// kept as what the chain writes, on the binding beside its extensions.
		beside, besideDiags := service.Beside[soa.Callback](serviceContext, rcb, use, false)
		ext = annotation.MergeUnmodeled(ext, annotation.Under(ids.Scope("callbacks", cbName), beside))
		diags = append(diags, besideDiags...)
		cb, serviceContextValue, cbDecl := service.Declared[soa.Callback](serviceContext, rcb, use)
		if cb == nil {
			continue
		}
		// A Callback Object's own x-* describe the callback rather than any
		// expression's path item, and ir.Callback holds no Unmodeled map. The HTTP
		// binding does, and is where the callbacks themselves live, so they are kept
		// there under the name the callback is mapped by.
		cbExt, cbExtDiags := annotation.ExtensionsUnder(cb.GetExtensions(), serviceContextValue.Place(), cbDecl, ids.Scope("callbacks", cbName))
		ext = annotation.MergeUnmodeled(ext, cbExt)
		diags = append(diags, cbExtDiags...)
		for expr, rp := range cb.All() {
			exprString := string(expr)
			layers := pathItemLayers(serviceContextValue, rp, cbDecl+ids.Ptr(exprString))
			if layers == nil {
				continue
			}
			mount := parent.ptrs.mount + ids.Ptr("callbacks", cbName, exprString)
			route := operationContext{
				key: exprString, template: exprString, inbound: ir.HTTPInboundCallback,
				inferred: parent.inferred, nesting: parent.nesting + 1,
			}
			at := mountSite{ptr: mount, use: serviceContextValue, usePtr: cbDecl + ids.Ptr(exprString)}
			operationIDs, cbOperations, orphan, cbDiags := lowerCallbackOperations(loweringState, layers, at, route)
			ext = annotation.MergeUnmodeled(ext, orphan)
			diags = append(diags, cbDiags...)
			callbacks = append(callbacks, ir.Callback{Name: cbName, Expression: exprString, Operations: operationIDs})
			operations = append(operations, cbOperations...)
		}
	}
	return callbacks, operations, ext, diags
}

// lowerCallbackOperations lowers the operations of a callback expression's
// path-item layers on route, each with the callbacks it declares in turn, one
// level deeper (operationCallbacks), the deeper callbacks' operations listed
// after the one declaring them. at.ptr is the expression's identity base,
// distinct per parent operation, while each layer keeps its declaration base,
// shared when the callback or its path item is $ref'd (issue #107).
//
// An expression whose item mounts no operation keeps what the item wrote on the
// returned map, which goes where the Callback Object's own extensions go, the
// parent's HTTP binding.
func lowerCallbackOperations(loweringState operationLoweringState, layers []layer, at mountSite, route operationContext) ([]ir.OperationID, []ir.Operation, ir.Unmodeled, []ir.Diagnostic) {
	var orphan ir.Unmodeled
	at.residue = &orphan
	mounted, diags := mountPathItem(loweringState, layers, at, route)
	operationIDs := make([]ir.OperationID, 0, len(mounted))
	operations := make([]ir.Operation, 0, len(mounted))
	for _, mountedOperation := range mounted {
		operationIDs = append(operationIDs, mountedOperation.operation.ID)
		operations = append(operations, mountedOperation.operation)
		operations = append(operations, mountedOperation.extra...)
	}
	return operationIDs, operations, orphan, diags
}

// lowerWebhooks lowers webhook path items into the dedicated "webhooks" group;
// each webhook operation is marked an inbound webhook on its HTTP binding, and its
// webhooks-map key, an event's name rather than a path, as WebhookName. It
// stops between webhooks when ctx is done, as lowerPaths does, and svc holds
// what a webhook item mounting no operation writes.
func lowerWebhooks(ctx context.Context, serviceContext service.Ctx, loweringState operationLoweringState, groups *serviceGroups, svc *ir.Service) []ir.Diagnostic {
	hooks := serviceContext.Doc.GetWebhooks()
	if hooks == nil || hooks.Len() == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	for name, rp := range hooks.All() {
		if ctx.Err() != nil {
			return diags
		}
		hookPtr := ids.Ptr("webhooks", name)
		layers := pathItemLayers(serviceContext, rp, hookPtr)
		if layers == nil {
			continue
		}
		route := operationContext{key: name, inbound: ir.HTTPInboundWebhook}
		at := mountSite{ptr: hookPtr, use: serviceContext, usePtr: hookPtr, residue: &svc.Unmodeled}
		mounted, hookDiags := mountPathItem(loweringState, layers, at, route)
		diags = append(diags, hookDiags...)
		for _, mountedOperation := range mounted {
			grp := groups.group(ids.WebhookGroup(), func() ir.OperationGroup {
				// A hint, not a source name: no document declares this group. The
				// compiler synthesizes it to hold webhook operations, exactly as
				// serviceGroups.filingBy synthesizes the "default" group, and
				// Naming.Source is the spelling the source used (GitHub #184).
				return ir.OperationGroup{Name: compile.NamingHint("webhooks")}
			})
			grp.Operations = append(grp.Operations, mountedOperation.operation)
			grp.Operations = append(grp.Operations, mountedOperation.extra...)
		}
	}
	return diags
}
