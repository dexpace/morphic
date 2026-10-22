package operation

import (
	"context"
	"encoding/json/jsontext"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// LowerService lowers one document into a single Service: its identity and docs,
// the declared tag registry, and every path, webhook, and callback operation
// placed into groups per the configured grouping strategy (ir-design §7.1).
//
// ctx bounds the walk in time, for the reason LowerComponentSchemas takes one:
// a document mounts as many paths and webhooks as it likes. Cancellation stops
// the two loops between path items and returns the groups filled so far; the
// compiler's run sees ctx.Err() at the phase boundary after this and refuses,
// rather than assembling a Document out of them.
func LowerService(ctx context.Context, c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex) (ir.Service, []ir.TagDef, []ir.Diagnostic) {
	svc := ir.Service{
		ID:         ids.Service(c.SrcIndex),
		Provenance: c.ProvenanceAt(""),
	}
	if info := c.Doc.GetInfo(); info != nil {
		title := info.GetTitle()
		svc.Name = compile.NamingFor(title)
		svc.Docs.Description = info.GetDescription()
	}
	svcAuth, diags := auth.LowerSecurityRequirements(c, c.Doc.GetSecurity(), "")
	svc.Auth = svcAuth
	// The Paths Object admits x-* of its own, distinct from any path item's, and
	// lowers to no node — its entries become operations. The service is the
	// nearest node holding an Unmodeled map, so they are kept there under the
	// keyword they were written at.
	pathsExt, pathsDiags := schema.ExtensionsIn(c, c.Doc.GetPaths().GetExtensions(), ids.Ptr("paths"), "paths")
	svc.Unmodeled = annotation.MergeUnmodeled(svc.Unmodeled, pathsExt)
	diags = append(diags, pathsDiags...)
	groups := newServiceGroups(c.Doc.GetTags())
	claims := newOperationIDClaims()
	diags = append(diags, lowerPaths(ctx, c, ts, anchors, claims, groups, &svc)...)
	diags = append(diags, lowerWebhooks(ctx, c, ts, anchors, claims, groups, &svc)...)
	// After both walks: whether an operationId is unique is a question about
	// every operation the service mounts, callbacks included.
	diags = append(diags, claims.report(c)...)
	svc.Groups = groups.finalize()
	return svc, lowerTagDefs(c), diags
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
				// compiler synthesizes it to hold webhook operations, exactly as it
				// synthesizes the "default" group above, and Naming.Source is the
				// spelling the source used (GitHub #184).
				return ir.OperationGroup{Name: compile.NamingHint("webhooks")}
			})
			grp.Operations = append(grp.Operations, m.op)
			grp.Operations = append(grp.Operations, m.extra...)
		}
	}
	return diags
}

// opPointers pairs the two pointers every operation lowering needs. mount is
// where the operation is written into the document and fixes its identity —
// OpID, provenance, and the root its callback operations hang from. decl is
// where its body is declared: the same node for an inline operation, the
// component's own pointer when the operation was reached through a $ref'd path
// item or callback (issue #107). Passing them as one value is what keeps two
// same-typed pointer arguments from being transposed at a call site.
type opPointers struct {
	mount jsontext.Pointer
	decl  jsontext.Pointer
}

// opContext carries the per-operation lowering inputs that do not come from the
// source Operation itself: its own two pointers, its HTTP binding shape,
// grouping provenance, and the path-item-merged parameter list — each entry
// still carrying the pointer of its own declaration site rather than a position
// in the merged list.
type opContext struct {
	// method is the method as sent on the wire — a fixed field's name upper-cased,
	// or an additionalOperations key exactly as the source spelled it — so nothing
	// downstream has to know which of the two declared the operation.
	method string
	// key is the map key the route mounts its path item under: a path, a
	// webhooks-map key or a callback expression. It names an operation that
	// declares no operationId, whichever route mounted it; keyFields places it
	// in the binding.
	key           string
	isWebhook     bool
	withCallbacks bool
	inferred      string
	params        []sourcedParam
	ptrs          opPointers
}

// keyFields places key in the binding field its route selects. A webhook's
// key names an event, so it is the WebhookName and the binding has no
// template; any other key, a path or a callback expression, is the
// URITemplate.
func (o opContext) keyFields() (uriTemplate, webhookName string) {
	if o.isWebhook {
		return "", o.key
	}
	return o.key, ""
}

// lowerOperation lowers one source operation into the neutral core plus its HTTP
// binding. It returns the operation and any callback operations that must be
// registered alongside it in the same group (ir-design §7.2, §8.1).
func lowerOperation(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, claims *operationIDClaims, src *soa.Operation, opCtx opContext) (ir.Operation, []ir.Operation, []ir.Diagnostic) {
	mount, decl := opCtx.ptrs.mount, opCtx.ptrs.decl
	// Built through the context so the source index is spelled in one place. Its
	// heuristic marker is filled in below, once every lowering that can add one
	// has run: the grouping choice is known here, but the streaming reading is
	// not known until the payloads are lowered.
	opProv := c.ProvenanceAt(decl)
	opAuth, diags := auth.LowerSecurityRequirements(c, src.Security, decl)
	op := ir.Operation{
		ID:   ids.Op(mount),
		Name: operationName(src, opCtx.method, opCtx.key),
		Tags: src.GetTags(),
		Auth: opAuth,
		// The ID is the mount — two mounts of one $ref'd path item are two
		// operations — but provenance is where the operation is written, which
		// for those two is one component. A mount pointer under a $ref'd path
		// item addresses no node at all, and ir-design §13 defines Pointer as a
		// pointer into the source, so it has to name the declaration.
		Provenance: opProv,
	}
	fillOperationDocs(&op.Docs, src)
	if src.GetDeprecated() {
		op.Deprecation = &ir.Deprecation{}
	}
	params, bindings, paramDiags := lowerParameters(c, ts, anchors, mount, opCtx.params)
	diags = append(diags, paramDiags...)
	op.Params = params
	responses, errs, responseDiags := lowerResponses(c, ts, anchors, src, decl)
	diags = append(diags, responseDiags...)
	op.Responses, op.Errors = responses, errs
	if opCtx.method == "" {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.InvalidMethodKey, decl,
			"additionalOperations key names no method; the operation lowers with an empty "+
				"HTTP method, which no request can be sent with"))
	}
	uriTemplate, webhookName := opCtx.keyFields()
	hb := ir.HTTPBinding{
		Method:        opCtx.method,
		URITemplate:   uriTemplate,
		WebhookName:   webhookName,
		IsWebhook:     opCtx.isWebhook,
		ParamBindings: bindings,
	}
	diags = append(diags, lowerRequestBody(c, ts, anchors, &op, &hb, src, decl)...)
	// After both payload lowerings, which are what it reads.
	streaming, streamDiags := applyStreaming(c, &op, decl)
	diags = append(diags, streamDiags...)
	op.Provenance.Inferred = joinInferred(opCtx.inferred, streaming)
	var extra []ir.Operation
	if opCtx.withCallbacks {
		var cbExt ir.Unmodeled
		var cbDiags []ir.Diagnostic
		hb.Callbacks, extra, cbExt, cbDiags = lowerCallbacks(c, ts, anchors, claims, src, opCtx.ptrs, opCtx.inferred)
		hb.Unmodeled = annotation.MergeUnmodeled(hb.Unmodeled, cbExt)
		diags = append(diags, cbDiags...)
	}
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{hb}}
	diags = append(diags, applyOperationAnnotations(c, &op, src, decl)...)
	// After the extensions are on the map, since that is what it reads.
	diags = append(diags, c.PromoteDeprecation(op.Unmodeled, op.Deprecation, &op.Provenance)...)
	diags = append(diags, applyOperationServers(c, &op, src, decl)...)
	claims.add(src, opCtx.ptrs)
	return op, extra, diags
}

// applyOperationAnnotations keeps the operation's own x-* and undeclared keys,
// and those of the two objects beneath it that lower to no node of their own:
// its externalDocs, since ir.Link holds no Unmodeled map, and its Responses
// Object, whose extensions are the map's own rather than any one response's.
//
// The Responses Object contributes extensions but no census: its keys are the
// status codes the document chooses, so an undeclared key there reads as one
// more response.
//
// It merges rather than assigns, because the operation's map already carries
// whatever its parameters or callbacks wrote.
func applyOperationAnnotations(c lowering.Ctx, op *ir.Operation, src *soa.Operation, decl jsontext.Pointer) []ir.Diagnostic {
	docsPtr := decl + ids.Ptr("externalDocs")
	ext, diags := annotation.ExtensionsAt(c.ProvenanceAt,
		annotation.ExtensionSite{Owner: decl, Ext: src.GetExtensions()},
		annotation.ExtensionSite{Scope: "externalDocs", Owner: docsPtr,
			Ext: src.GetExternalDocs().GetExtensions()},
		annotation.ExtensionSite{Scope: "responses", Owner: decl + ids.Ptr("responses"),
			Ext: src.GetResponses().GetExtensions()},
	)
	op.Unmodeled = annotation.MergeUnmodeled(op.Unmodeled, ext)
	diags = append(diags, annotation.UnknownKeysIn(&op.Unmodeled, src, c.ProvenanceAt, decl)...)
	return append(diags, annotation.UnknownKeysUnder(&op.Unmodeled,
		src.GetExternalDocs(), c.ProvenanceAt, docsPtr, "externalDocs")...)
}

// applyOperationServers preserves an operation's own `servers` verbatim under
// Unmodeled with an info diagnostic, for the reason applyPathServers does for
// the path item's: ir.Operation has no server-scope list.
//
// It is the overriding half of the pair: an Operation Object's servers override
// the Path Item Object's, so keeping only the superseded list would route an
// emitter to the wrong host (GitHub #39). The two sit under separate keys
// because one key cannot hold both without the survivor depending on lowering
// order; the path item's keeps `openapi:servers`.
//
// Unlike applyPathServers, it is called from lowerOperation, so no route can
// forget it.
func applyOperationServers(c lowering.Ctx, op *ir.Operation, src *soa.Operation, declPtr jsontext.Pointer) []ir.Diagnostic {
	if len(src.GetServers()) == 0 {
		return nil
	}
	kept, diags := schema.PreserveNode(c, &op.Unmodeled, "openapi:operationServers",
		annotation.RawChildNode(src.GetRootNode(), "servers"), ir.ReasonNoIRHome, declPtr+ids.Ptr("servers"))
	if !kept {
		return diags
	}
	return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, op.Provenance,
		"operation servers kept under Unmodeled; an operation has no server-scope list to bind "+
			"them to, and these override any path-item servers kept beside them"))
}

// operationName builds an operation's neutral naming: the operationId when
// present (source + canonical words), else an empty source with a hint from
// the method and the route's key so emitters can synthesize a name.
func operationName(src *soa.Operation, method, key string) ir.Naming {
	if id := src.GetOperationID(); id != "" {
		return compile.NamingFor(id)
	}
	return compile.NamingHint(method + " " + key)
}

// fillOperationDocs maps an operation's summary, description, and externalDocs
// onto Docs.
func fillOperationDocs(d *ir.Docs, src *soa.Operation) {
	d.Summary = src.GetSummary()
	d.Description = src.GetDescription()
	if ed := src.GetExternalDocs(); ed != nil {
		d.ExternalDocs = append(d.ExternalDocs, ir.Link{URL: ed.GetURL(), Description: ed.GetDescription()})
	}
}

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

// sourcedParam pairs a parameter with its declaring list entry's pointer,
// because path-item parameters merge into every operation on the path item and
// so cannot borrow the operation's index space (issue #36). A $ref entry's own
// declaration lies one hop further on, resolved in lowerParameters.
type sourcedParam struct {
	ref     *soa.ReferencedParameter
	pointer jsontext.Pointer
	// in is the context a path-item entry is lowered in, its layer's (see
	// itemParams), whose document its pointer is in, or nil for an
	// operation's own entry, which is lowered in the operation's.
	in *lowering.Ctx
}

// mergeParameters merges a mount's path-item parameters with an operation's own
// using use-site precedence: an operation parameter with the same (name, in)
// overrides the path-item one; unshadowed path-item parameters follow in source
// order. Each entry keeps the pointer of its own declaration site rather than a
// position recomputed from the merged slice (issue #36), and a $ref'd path
// item's are under its own declaration pointer, not the path it is mounted at
// (issue #107).
func mergeParameters(pathParams []sourcedParam, opParams []*soa.ReferencedParameter, opDeclPtr jsontext.Pointer) []sourcedParam {
	merged := make([]sourcedParam, 0, len(opParams)+len(pathParams))
	merged = appendSourced(merged, opParams, opDeclPtr, nil, nil)
	if len(pathParams) == 0 {
		return merged
	}
	shadowed := shadowedKeys(opParams)
	for _, p := range pathParams {
		if key, ok := paramKey(p.ref); ok && shadowed[key] {
			continue
		}
		merged = append(merged, p)
	}
	return merged
}

// appendSourced appends params to dst, pairing each with the pointer of its own
// declaration position under base and the context in it is lowered in, and
// skipping any whose (name, in) key is in shadowed. A nil shadowed map reads as
// all-false, which is what the operation side passes: operation parameters are
// never shadowed, and are lowered in the operation's own context (a nil in).
func appendSourced(dst []sourcedParam, params []*soa.ReferencedParameter, base jsontext.Pointer,
	shadowed map[string]bool, in *lowering.Ctx,
) []sourcedParam {
	for i, p := range params {
		if key, ok := paramKey(p); ok && shadowed[key] {
			continue
		}
		dst = append(dst, sourcedParam{ref: p, pointer: base + ids.Ptr("parameters", strconv.Itoa(i)), in: in})
	}
	return dst
}

// shadowedKeys returns the (in, name) identities declared by opParams, so a
// path-item parameter sharing one is left out of the merge as a duplicate.
func shadowedKeys(opParams []*soa.ReferencedParameter) map[string]bool {
	shadowed := make(map[string]bool, len(opParams))
	for _, p := range opParams {
		if key, ok := paramKey(p); ok {
			shadowed[key] = true
		}
	}
	return shadowed
}

// paramKey builds the (in, name) identity of a parameter for merge dedup.
func paramKey(rp *soa.ReferencedParameter) (string, bool) {
	p := resolve.Object[soa.Parameter](rp)
	if p == nil {
		return "", false
	}
	return string(p.GetIn()) + "\x00" + p.GetName(), true
}
