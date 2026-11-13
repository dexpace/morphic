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
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// maxCallbackNesting bounds the levels of callbacks that lower below a path's
// or a webhook's operation: 4, so an operation nested in fewer callbacks lowers
// its own, and one nested in 4 keeps them verbatim. It stops a callback that
// names itself through a $ref from lowering without end, while admitting an
// exchange of several callbacks in turn.
const maxCallbackNesting = 4

// operationCallbacks lowers src's callbacks for the operation opCtx describes
// (lowerCallbacks) while it is nested in fewer than maxCallbackNesting
// callbacks. Past that the map is kept verbatim, beside the callbacks' own
// extensions, and reported (docs/compilers/openapi.md#callbacks).
func operationCallbacks(c lowering.Ctx, w walk, src *soa.Operation, opCtx opContext) ([]ir.Callback, []ir.Operation, ir.Unmodeled, []ir.Diagnostic) {
	if src.GetCallbacks().Len() == 0 {
		return nil, nil, nil, nil
	}
	if opCtx.nesting < maxCallbackNesting {
		return lowerCallbacks(c, w, src, opCtx)
	}
	at := opCtx.ptrs.decl + ids.Ptr("callbacks")
	var kept ir.Unmodeled
	written, diags := annotation.PreserveNodeInto(&kept, annotation.EntryKey("", "callbacks"),
		nodeview.Child(src.GetRootNode(), "callbacks"), ir.ReasonDegradedLowering, c.Place().At(at))
	if written {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
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
func lowerCallbacks(c lowering.Ctx, w walk, src *soa.Operation, parent opContext) ([]ir.Callback, []ir.Operation, ir.Unmodeled, []ir.Diagnostic) {
	var callbacks []ir.Callback
	var ops []ir.Operation
	var ext ir.Unmodeled
	var diags []ir.Diagnostic
	for cbName, rcb := range src.GetCallbacks().All() {
		cb, c, cbDecl := lowering.Declared[soa.Callback](c, rcb, parent.ptrs.decl+ids.Ptr("callbacks", cbName))
		if cb == nil {
			continue
		}
		// A Callback Object's own x-* describe the callback rather than any
		// expression's path item, and ir.Callback holds no Unmodeled map. The HTTP
		// binding does, and is where the callbacks themselves live, so they are kept
		// there under the name the callback is mapped by.
		cbExt, cbExtDiags := annotation.ExtensionsUnder(cb.GetExtensions(), c.Place(), cbDecl, ids.Scope("callbacks", cbName))
		ext = annotation.MergeUnmodeled(ext, cbExt)
		diags = append(diags, cbExtDiags...)
		for expr, rp := range cb.All() {
			exprStr := string(expr)
			layers, found := pathItemLayers(c, rp, cbDecl+ids.Ptr(exprStr))
			if layers == nil {
				continue
			}
			diags = append(diags, found...)
			mount := parent.ptrs.mount + ids.Ptr("callbacks", cbName, exprStr)
			route := opContext{
				key: exprStr, template: exprStr, inbound: ir.HTTPInboundCallback,
				inferred: parent.inferred, nesting: parent.nesting + 1,
			}
			at := mountSite{ptr: mount, use: c, usePtr: cbDecl + ids.Ptr(exprStr)}
			opIDs, cbOps, orphan, cbDiags := lowerCallbackOps(w, layers, at, route)
			ext = annotation.MergeUnmodeled(ext, orphan)
			diags = append(diags, cbDiags...)
			callbacks = append(callbacks, ir.Callback{Name: cbName, Expression: exprStr, Operations: opIDs})
			ops = append(ops, cbOps...)
		}
	}
	return callbacks, ops, ext, diags
}

// lowerCallbackOps lowers the operations of a callback expression's path-item
// layers on route, each with the callbacks it declares in turn, one level
// deeper (operationCallbacks), the deeper callbacks' operations listed after
// the one declaring them. at.ptr is the expression's identity base, distinct
// per parent operation, while each layer keeps its declaration base, shared
// when the callback or its path item is $ref'd (issue #107).
//
// An expression whose item mounts no operation keeps what the item wrote on the
// returned map, which goes where the Callback Object's own extensions go, the
// parent's HTTP binding.
func lowerCallbackOps(w walk, layers []layer, at mountSite, route opContext) ([]ir.OpID, []ir.Operation, ir.Unmodeled, []ir.Diagnostic) {
	var orphan ir.Unmodeled
	at.residue = &orphan
	mounted, diags := mountPathItem(w, layers, at, route)
	opIDs := make([]ir.OpID, 0, len(mounted))
	ops := make([]ir.Operation, 0, len(mounted))
	for _, m := range mounted {
		opIDs = append(opIDs, m.op.ID)
		ops = append(ops, m.op)
		ops = append(ops, m.extra...)
	}
	return opIDs, ops, orphan, diags
}

// lowerWebhooks lowers webhook path items into the dedicated "webhooks" group;
// each webhook operation is marked an inbound webhook on its HTTP binding, and its
// webhooks-map key, an event's name rather than a path, as WebhookName. It
// stops between webhooks when ctx is done, as lowerPaths does, and svc holds
// what a webhook item mounting no operation writes.
func lowerWebhooks(ctx context.Context, c lowering.Ctx, w walk, groups *serviceGroups, svc *ir.Service) []ir.Diagnostic {
	hooks := c.Doc.GetWebhooks()
	if hooks == nil || hooks.Len() == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	for name, rp := range hooks.All() {
		if ctx.Err() != nil {
			return diags
		}
		hookPtr := ids.Ptr("webhooks", name)
		layers, found := pathItemLayers(c, rp, hookPtr)
		if layers == nil {
			continue
		}
		diags = append(diags, found...)
		route := opContext{key: name, inbound: ir.HTTPInboundWebhook}
		at := mountSite{ptr: hookPtr, use: c, usePtr: hookPtr, residue: &svc.Unmodeled}
		mounted, hookDiags := mountPathItem(w, layers, at, route)
		diags = append(diags, hookDiags...)
		for _, m := range mounted {
			grp := groups.group(ids.WebhookGroup(), func() ir.OperationGroup {
				// A hint, not a source name: no document declares this group. The
				// compiler synthesizes it to hold webhook operations, exactly as
				// serviceGroups.filingBy synthesizes the "default" group, and
				// Naming.Source is the spelling the source used (GitHub #184).
				return ir.OperationGroup{Name: compile.NamingHint("webhooks")}
			})
			grp.Operations = append(grp.Operations, m.op)
			grp.Operations = append(grp.Operations, m.extra...)
		}
	}
	return diags
}
