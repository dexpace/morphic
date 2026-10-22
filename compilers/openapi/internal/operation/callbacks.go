package operation

import (
	"context"
	"encoding/json/jsontext"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// lowerCallbacks lowers each callback expression's path-item operations as
// Operations registered in the parent's group, and binds them to the parent via
// HTTPBinding.Callbacks keyed by the runtime expression (ir-design §8.1).
// parent.mount roots callback operation identity, so two parents sharing one
// $ref'd callback keep distinct callback operations; parent.decl is the base a
// $ref'd callback or path item resolves against (issue #107).
func lowerCallbacks(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, claims *operationIDClaims, src *soa.Operation, parent opPointers, inferred string) ([]ir.Callback, []ir.Operation, ir.Unmodeled, []ir.Diagnostic) {
	cbMap := src.GetCallbacks()
	if cbMap == nil || cbMap.Len() == 0 {
		return nil, nil, nil, nil
	}
	var callbacks []ir.Callback
	var ops []ir.Operation
	var ext ir.Unmodeled
	var diags []ir.Diagnostic
	for cbName, rcb := range cbMap.All() {
		cb, c, cbDecl := lowering.Declared[soa.Callback](c, rcb, parent.decl+ids.Ptr("callbacks", cbName))
		if cb == nil {
			continue
		}
		// A Callback Object's own x-* describe the callback rather than any
		// expression's path item, and ir.Callback holds no Unmodeled map. The HTTP
		// binding does, and is where the callbacks themselves live, so they are kept
		// there under the name the callback is mapped by.
		cbExt, cbExtDiags := schema.ExtensionsIn(c, cb.GetExtensions(), cbDecl, ids.Scope("callbacks", cbName))
		ext = annotation.MergeUnmodeled(ext, cbExt)
		diags = append(diags, cbExtDiags...)
		for expr, rp := range cb.All() {
			exprStr := string(expr)
			layers, found := pathItemLayers(c, rp, cbDecl+ids.Ptr(exprStr))
			if layers == nil {
				continue
			}
			diags = append(diags, found...)
			mount := parent.mount + ids.Ptr("callbacks", cbName, exprStr)
			opIDs, cbOps, orphan, cbDiags := lowerCallbackOps(ts, anchors, claims, layers, mount, exprStr, inferred)
			ext = annotation.MergeUnmodeled(ext, orphan)
			diags = append(diags, cbDiags...)
			callbacks = append(callbacks, ir.Callback{Expression: exprStr, Operations: opIDs})
			ops = append(ops, cbOps...)
		}
	}
	return callbacks, ops, ext, diags
}

// lowerCallbackOps lowers the operations of a callback expression's path-item
// layers. They do not recurse into their own callbacks (withCallbacks stays
// false), bounding the lowering to the declared out-of-band set. mount is the
// expression's identity base, distinct per parent operation, while each layer
// keeps its declaration base, shared when the callback or its path item is
// $ref'd (issue #107).
//
// An expression whose item mounts no operation keeps what the item wrote on the
// returned map, which goes where the Callback Object's own extensions go, the
// parent's HTTP binding.
func lowerCallbackOps(ts *compile.Types, anchors *schema.AnchorIndex, claims *operationIDClaims, layers []layer, mount jsontext.Pointer, expr, inferred string) ([]ir.OpID, []ir.Operation, ir.Unmodeled, []ir.Diagnostic) {
	var orphan ir.Unmodeled
	mounted, diags := mountPathItem(ts, anchors, claims, layers, mount, opContext{key: expr, inferred: inferred}, &orphan)
	opIDs := make([]ir.OpID, 0, len(mounted))
	ops := make([]ir.Operation, 0, len(mounted))
	for _, m := range mounted {
		opIDs = append(opIDs, m.op.ID)
		ops = append(ops, m.op)
	}
	return opIDs, ops, orphan, diags
}

// lowerWebhooks lowers webhook path items into the dedicated "webhooks" group;
// each webhook operation carries IsWebhook on its HTTP binding, and its
// webhooks-map key, an event's name rather than a path, as WebhookName. It
// stops between webhooks when ctx is done, as lowerPaths does, and svc holds
// what a webhook item mounting no operation writes.
func lowerWebhooks(ctx context.Context, c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, claims *operationIDClaims, groups *serviceGroups, svc *ir.Service) []ir.Diagnostic {
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
		route := opContext{key: name, isWebhook: true, withCallbacks: true}
		mounted, hookDiags := mountPathItem(ts, anchors, claims, layers, hookPtr, route, &svc.Unmodeled)
		diags = append(diags, hookDiags...)
		for _, m := range mounted {
			grp := groups.group(ids.WebhookGroup(), func() ir.OperationGroup {
				// A hint, not a source name: no document declares this group. The
				// compiler synthesizes it to hold webhook operations, exactly as
				// groupFor synthesizes the "default" group, and Naming.Source is the
				// spelling the source used (GitHub #184).
				return ir.OperationGroup{Name: compile.NamingHint("webhooks")}
			})
			grp.Operations = append(grp.Operations, m.op)
			grp.Operations = append(grp.Operations, m.extra...)
		}
	}
	return diags
}
