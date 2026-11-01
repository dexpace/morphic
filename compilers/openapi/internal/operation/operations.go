// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"context"
	"encoding/json/jsontext"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/servers"
	"github.com/dexpace/morphic/ir"
)

// LowerService lowers one document into a single Service: its identity, docs
// and servers, the declared tag registry, and every path, webhook, and callback
// operation placed into groups per the configured grouping strategy
// (docs/ir/services.md), registering every server it meets in reg.
//
// ctx bounds the walk in time, for the reason LowerComponentSchemas takes one:
// a document mounts as many paths and webhooks as it likes. Cancellation stops
// the two loops between path items and returns the groups filled so far; the
// compiler's run sees ctx.Err() at the phase boundary after this and refuses,
// rather than assembling a Document out of them.
func LowerService(ctx context.Context, c lowering.Ctx, ts *compile.Types, reg *servers.Registry) (ir.Service, []ir.TagDef, []ir.Diagnostic) {
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
	svcServers, serverDiags := reg.Root(c)
	svc.Servers = svcServers
	diags = append(diags, serverDiags...)
	// The Paths Object admits x-* of its own, distinct from any path item's, and
	// lowers to no node — its entries become operations. The service is the
	// nearest node holding an Unmodeled map, so they are kept there under the
	// keyword they were written at.
	pathsExt, pathsDiags := annotation.ExtensionsUnder(c.Doc.GetPaths().GetExtensions(), c.ProvenanceAt, ids.Ptr("paths"), "paths")
	svc.Unmodeled = annotation.MergeUnmodeled(svc.Unmodeled, pathsExt)
	diags = append(diags, pathsDiags...)
	groups := newServiceGroups(c.Doc.GetTags())
	w := walk{types: ts, claims: newOperationIDClaims(), servers: reg}
	diags = append(diags, lowerPaths(ctx, c, w, groups, &svc)...)
	diags = append(diags, lowerWebhooks(ctx, c, w, groups, &svc)...)
	// After both walks: whether an operationId is unique is a question about
	// every operation the service mounts, callbacks included.
	diags = append(diags, w.claims.report(c)...)
	svc.Groups = groups.finalize()
	return svc, lowerTagDefs(c), diags
}

// walk is what lowering a service's operations accumulates across all of them,
// whichever route mounts each: the type registry, the operationId claims judged
// once every operation has lowered, and the server registry a path item's and
// an operation's servers go into. One is built per service and handed down by
// value, since each field is the shared accumulator itself.
type walk struct {
	types   *compile.Types
	claims  *operationIDClaims
	servers *servers.Registry
}

// opPointers pairs the two pointers every operation lowering needs. mount is
// where the operation is mounted and fixes its identity — OpID, its parameters'
// IDs, and the root its callback operations hang from. decl is where its body
// is declared, which its provenance names: the same node for an inline
// operation, the component's own pointer when the operation was reached through
// a $ref'd path item or callback (issue #107). Passing them as one value is
// what keeps two same-typed pointer arguments from being transposed at a call
// site.
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
	inferred      ir.Inferred
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
// registered alongside it in the same group (docs/ir/operations.md,
// docs/ir/http-binding.md).
func lowerOperation(c lowering.Ctx, w walk, src *soa.Operation, opCtx opContext) (ir.Operation, []ir.Operation, []ir.Diagnostic) {
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
		// item addresses no node at all, and docs/ir/provenance.md defines
		// Pointer as a pointer into the source, so it has to name the
		// declaration.
		Provenance: opProv,
	}
	fillOperationDocs(&op.Docs, src)
	if src.GetDeprecated() {
		op.Deprecation = &ir.Deprecation{}
	}
	params, bindings, paramDiags := lowerParameters(c, w.types, mount, opCtx.params)
	diags = append(diags, paramDiags...)
	op.Params = params
	diags = append(diags, lowerResponses(c, w.types, &op, src, decl)...)
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
	diags = append(diags, lowerRequestBody(c, w.types, &op, &hb, src, decl)...)
	// After both payload lowerings, which are what it reads.
	streaming, streamDiags := applyStreaming(c, &op, decl)
	diags = append(diags, streamDiags...)
	op.Provenance.Inferred = ir.JoinInferred(op.Provenance.Inferred, opCtx.inferred, streaming)
	var extra []ir.Operation
	if opCtx.withCallbacks {
		var cbExt ir.Unmodeled
		var cbDiags []ir.Diagnostic
		hb.Callbacks, extra, cbExt, cbDiags = lowerCallbacks(c, w, src, opCtx.ptrs, opCtx.inferred)
		hb.Unmodeled = annotation.MergeUnmodeled(hb.Unmodeled, cbExt)
		diags = append(diags, cbDiags...)
	}
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{hb}}
	diags = append(diags, applyOperationAnnotations(c, &op, src, decl)...)
	// After the extensions are on the map, since that is what it reads.
	diags = append(diags, c.Promoter().Deprecation(op.Unmodeled, &op.Deprecation, &op.Provenance)...)
	diags = append(diags, c.Promoter().Stability(op.Unmodeled, &op.Stability, &op.Provenance)...)
	// mountPathItem names the path item's servers only where these name none,
	// since an operation's own override them.
	own, serverDiags := w.servers.Declared(c, src.GetServers(), decl)
	op.Servers = own
	diags = append(diags, serverDiags...)
	w.claims.add(src, opCtx.ptrs)
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

// withoutInferred returns p with no Inferred marker, for a finding about what an
// operation declares. op.Provenance names the heuristics that shaped the
// operation, such as path-prefix grouping or inferred streaming, and a finding
// about its declared servers or documentation owes nothing to any of them.
func withoutInferred(p ir.Provenance) ir.Provenance {
	p.Inferred = ""
	return p
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
