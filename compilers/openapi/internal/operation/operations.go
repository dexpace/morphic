// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
	"github.com/dexpace/morphic/compilers/openapi/internal/servers"
	"github.com/dexpace/morphic/ir"
)

// LowerService lowers one document's service, its operations grouped per the
// configured strategy (docs/ir/services.md), and its declared tags. It returns
// them beside every server it registers, every scheme in declared with each
// unresolved one a requirement names, the Document's kept entries and its
// links.
//
// ctx bounds the walk in time, for the reason LowerComponentSchemas takes one:
// a document mounts as many paths and webhooks as it likes. Cancellation stops
// both loops between path items and returns the groups filled so far; the
// compiler's run sees ctx.Err() at the phase boundary after this and refuses,
// rather than assembling a Document from them.
func LowerService(ctx context.Context, c lowering.Ctx, ts *compile.Types, declared map[ir.AuthID]ir.AuthScheme) (Lowered, []ir.Diagnostic) {
	reg := servers.NewRegistry()
	schemes := auth.RegistryOf(c, declared)
	svc := ir.Service{
		ID:         ids.Service(c.SrcIndex),
		Provenance: c.Place().At(""),
	}
	if info := c.Doc.GetInfo(); info != nil {
		title := info.GetTitle()
		svc.Name = ir.NamingFor(title)
		svc.Docs.Description = info.GetDescription()
	}
	svcAuth, diags := schemes.Requirements(c, c.Doc.GetSecurity(), "")
	svc.Auth = svcAuth
	svcServers, serverDiags := reg.Root(c)
	svc.Servers = svcServers
	diags = append(diags, serverDiags...)
	// The Paths Object admits x-* of its own, distinct from any path item's, and
	// lowers to no node — its entries become operations. The service is the
	// nearest node holding an Unmodeled map, so they are kept there under the
	// keyword they were written at.
	pathsExt, pathsDiags := annotation.ExtensionsUnder(c.Doc.GetPaths().GetExtensions(), c.Place(), ids.Ptr("paths"), "paths")
	svc.Unmodeled = annotation.MergeUnmodeled(svc.Unmodeled, pathsExt)
	diags = append(diags, pathsDiags...)
	groups := newServiceGroups(c.Doc.GetTags())
	w := walk{
		types: ts, claims: newOperationIDClaims(), servers: reg, schemes: schemes,
		kept: newKeptOnce(), links: &Links{},
	}
	diags = append(diags, lowerPaths(ctx, c, w, groups, &svc)...)
	diags = append(diags, lowerWebhooks(ctx, c, w, groups, &svc)...)
	// After both walks: whether an operationId is unique is a question about
	// every operation the service mounts, callbacks included.
	diags = append(diags, w.claims.report(c)...)
	groups.markSharedRoutes()
	svc.Groups = groups.finalize()
	return Lowered{
		Service: svc,
		TagDefs: lowerTagDefs(c),
		Servers: reg.Servers(),
		Auth:    schemes.Schemes(),
		Kept:    w.kept.entries,
		Links:   *w.links,
	}, diags
}

// Lowered is what LowerService builds from one document. Each field is the
// Document field of its name, bar Service, the one entry of Services, Kept,
// entries for the Document's Unmodeled, and Links, which Links.Resolve lowers
// onto Service once the type registry is whole.
type Lowered struct {
	Service ir.Service
	TagDefs []ir.TagDef
	Servers map[ir.ServerID]ir.Server
	Auth    map[ir.AuthID]ir.AuthScheme
	Kept    ir.Unmodeled
	Links   Links
}

// walk is what lowering a service's operations accumulates across all of them,
// whichever route mounts each: the type registry, the operationId claims judged
// once every operation has lowered, the server registry a path item's and an
// operation's servers go into, the scheme registry a requirement's names go
// into, what the walk keeps for the Document, and the links resolved once the
// service is whole. One is built per service and handed down by value, since
// each field is the shared accumulator itself.
type walk struct {
	types   *compile.Types
	claims  *operationIDClaims
	servers *servers.Registry
	schemes *auth.Registry
	kept    *keptOnce
	links   *Links
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
	// declares no operationId, whichever route mounted it, and a webhook's
	// binding (keyFields).
	key string
	// template is the URI template the route sends to: a path's key less its
	// fragment (pathRoute), a callback's expression as written, and none for a
	// webhook, whose key names an event.
	template string
	inbound  ir.HTTPInbound
	// nesting is how many callbacks the operation is declared in: none for a
	// path's or a webhook's, one more for each callback below one.
	nesting int
	// groups is what a path's route files each operation it mounts in, which
	// decides the operation's grouping marker, inferred; nil on a route filing
	// none, whose inferred is the route's own.
	groups   *serviceGroups
	inferred ir.Inferred
	params   []sourcedParam
	ptrs     opPointers
}

// keyFields returns the binding's route: a webhook's key names an event, so it
// is the WebhookName and the binding has no template; any other route's
// template is the URITemplate.
func (o opContext) keyFields() (uriTemplate, webhookName string) {
	if o.inbound == ir.HTTPInboundWebhook {
		return "", o.key
	}
	return o.template, ""
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
	opProv := c.Place().At(decl)
	opAuth, diags := w.schemes.Requirements(c, src.Security, decl)
	tags, tagDiags := operationTags(c, src.GetTags(), decl)
	diags = append(diags, tagDiags...)
	op := ir.Operation{
		ID:   ids.Op(mount),
		Name: operationName(src, opCtx.method, opCtx.key),
		Tags: tags,
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
	params, bindings, paramDiags := lowerParameters(c, w, mount, opCtx.params)
	diags = append(diags, paramDiags...)
	op.Params = params
	diags = append(diags, lowerResponses(c, w, &op, src, decl)...)
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
		Inbound:       opCtx.inbound,
		ParamBindings: bindings,
	}
	diags = append(diags, lowerRequestBody(c, w, &op, &hb, src, decl)...)
	// After both payload lowerings, which are what it reads.
	streaming := applyStreaming(c, w.types, &op, decl)
	op.Provenance.Inferred = ir.JoinInferred(op.Provenance.Inferred, opCtx.inferred, streaming)
	callbacks, extra, cbExt, cbDiags := operationCallbacks(c, w, src, opCtx)
	hb.Callbacks = callbacks
	hb.Unmodeled = annotation.MergeUnmodeled(hb.Unmodeled, cbExt)
	diags = append(diags, cbDiags...)
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{hb}}
	diags = append(diags, applyOperationAnnotations(c, &op, src, decl)...)
	// After the extensions are on the map, since that is what it reads.
	diags = append(diags, c.Promoter().Deprecation(op.Unmodeled, lowering.Referent{}, &op.Deprecation, &op.Provenance)...)
	diags = append(diags, c.Promoter().Stability(op.Unmodeled, &op.Stability, &op.Provenance)...)
	// mountPathItem names the path item's servers only where these name none,
	// since an operation's own override them.
	own, serverDiags := w.servers.Declared(c, src.GetServers(), decl)
	op.Servers = own
	diags = append(diags, serverDiags...)
	w.claims.add(src, opCtx.ptrs)
	return op, extra, diags
}

// operationTags returns the tags written, the list an operation declared at decl
// writes, each kept once, at its first occurrence, beside a warning at each
// repeat: a tag names a group the operation is a member of, which it joins once
// however often the list names it. Tags compare as the text written.
func operationTags(c lowering.Ctx, written []string, decl jsontext.Pointer) ([]string, []ir.Diagnostic) {
	tags, repeats := lowering.FirstOccurrences(written)
	diags := make([]ir.Diagnostic, 0, len(repeats))
	for _, r := range repeats {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.RepeatedEntry, decl+ids.Ptr("tags", strconv.Itoa(r.At)),
			"tag %q repeats the entry at index %d; it is kept once", written[r.At], r.First))
	}
	return tags, diags
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
	ext, diags := annotation.ExtensionsAt(c.Place(),
		annotation.ExtensionSite{Owner: decl, Ext: src.GetExtensions()},
		annotation.ExtensionSite{Scope: "externalDocs", Owner: docsPtr,
			Ext: src.GetExternalDocs().GetExtensions()},
		annotation.ExtensionSite{Scope: "responses", Owner: decl + ids.Ptr("responses"),
			Ext: src.GetResponses().GetExtensions()},
	)
	op.Unmodeled = annotation.MergeUnmodeled(op.Unmodeled, ext)
	diags = append(diags, annotation.UnknownKeysIn(&op.Unmodeled, src, c.Place(), decl)...)
	return append(diags, annotation.UnknownKeysUnder(&op.Unmodeled,
		src.GetExternalDocs(), c.Place(), docsPtr, "externalDocs")...)
}

// operationName builds an operation's neutral naming: the operationId when
// present (source + canonical words), else an empty source with a hint from
// the method and the route's key so emitters can synthesize a name.
func operationName(src *soa.Operation, method, key string) ir.Naming {
	if id := src.GetOperationID(); id != "" {
		return ir.NamingFor(id)
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
