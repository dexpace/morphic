package operation

import (
	"context"
	"strconv"
	"strings"

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

const (
	// defaultResponseKey is the responses-map key OpenAPI reserves for the
	// catch-all response, spelled once so the pointer that response is lowered
	// under and the parse that recognizes the key cannot drift apart.
	defaultResponseKey = "default"
	// statusKeyLen is the width of every responses-map key that names a status,
	// whether digits ("200") or a wildcard range ("2XX").
	statusKeyLen = 3
	// additionalOperationsField is the Path Item Object key OpenAPI 3.2 gives to
	// operations whose method has no fixed field of its own, spelled once so the
	// pointer such an operation is mounted under and the field it is read from
	// cannot drift apart.
	additionalOperationsField = "additionalOperations"
)

// httpMethods is the set of HTTP method accessors that are fixed fields on a
// PathItem, iterated in this order so operation lowering is deterministic across
// runs. The name is the field name, which is the wire method in lowercase;
// pointers and IDs derive from it.
//
// It is closed, and the fields outside it are not: OpenAPI 3.2 added `query`
// here and put every other method under `additionalOperations`, which
// pathOperations reads beside this table.
var httpMethods = []struct {
	name string
	get  func(*soa.PathItem) *soa.Operation
}{
	{"get", (*soa.PathItem).Get},
	{"put", (*soa.PathItem).Put},
	{"post", (*soa.PathItem).Post},
	{"delete", (*soa.PathItem).Delete},
	{"options", (*soa.PathItem).Options},
	{"head", (*soa.PathItem).Head},
	{"patch", (*soa.PathItem).Patch},
	{"trace", (*soa.PathItem).Trace},
	{"query", (*soa.PathItem).Query},
}

// pathOperation is one operation a path item declares: the method as sent on the
// wire, the pointer segment the operation is written under, and its source node.
type pathOperation struct {
	method string
	seg    string
	src    *soa.Operation
}

// pathOperations returns every operation a path item declares — the fixed method
// fields first, in httpMethods order, then any 3.2 additionalOperations in
// source order.
//
// Reading the fixed fields alone dropped an additionalOperations entry whole:
// its operationId, parameters, request body, responses, and every type reachable
// only through them, with no diagnostic (GitHub #293). Yielding one list is what
// keeps that from recurring per route — the three walks that reach a path item
// all read this, so an operation is lowered on all of them or on none.
//
// A key is the method verbatim. ir.HTTPBinding.Method is the method as sent on
// the wire and OpenAPI reads a method name case-sensitively, so the key is
// neither upper-cased nor neutralized. A fixed field's name is a field name
// rather than a method, so that one is upper-cased into its wire spelling.
//
// Taking the key verbatim means an empty one reaches the binding as an empty
// method. It still lowers — dropping the entry would lose everything it declares
// — and lowerOperation reports it, which is where every route funnels through.
func pathOperations(pi *soa.PathItem) []pathOperation {
	ops := make([]pathOperation, 0, len(httpMethods))
	for _, m := range httpMethods {
		if src := m.get(pi); src != nil {
			ops = append(ops, pathOperation{method: strings.ToUpper(m.name), seg: ids.Ptr(m.name), src: src})
		}
	}
	extra := pi.GetAdditionalOperations()
	if extra == nil {
		return ops
	}
	for method, src := range extra.All() {
		if src == nil {
			continue
		}
		ops = append(ops, pathOperation{
			method: method,
			seg:    ids.Ptr(additionalOperationsField, method),
			src:    src,
		})
	}
	return ops
}

// LowerService lowers one document into a single Service: its identity and docs,
// the declared tag registry, and every path, webhook, and callback operation
// placed into groups per the configured grouping strategy (ir-design §7.1).
//
// ctx bounds the walk in time, for the reason LowerComponentSchemas takes one:
// a document mounts as many paths and webhooks as it likes. Cancellation stops
// the two loops between path items and returns the groups filled so far; the
// compiler's run sees ctx.Err() at the phase boundary after this and refuses,
// rather than assembling a Document out of them.
func LowerService(ctx context.Context, c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, operationIDs map[string]string) (ir.Service, []ir.TagDef, []ir.Diagnostic) {
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
	groups := newServiceGroups()
	diags = append(diags, lowerPaths(ctx, c, ts, anchors, operationIDs, groups)...)
	diags = append(diags, lowerWebhooks(ctx, c, ts, anchors, operationIDs, groups)...)
	svc.Groups = groups.finalize()
	return svc, lowerTagDefs(c), diags
}

// lowerTagDefs registers the document's declared tag metadata into TagDefs; tag
// membership itself stays as []string on each tagged operation.
func lowerTagDefs(c lowering.Ctx) []ir.TagDef {
	tags := c.Doc.GetTags()
	if len(tags) == 0 {
		return nil
	}
	defs := make([]ir.TagDef, 0, len(tags))
	for _, t := range tags {
		if t == nil {
			continue
		}
		defs = append(defs, ir.TagDef{Name: t.GetName(), Docs: tagDocsFrom(t)})
	}
	return defs
}

// tagDocsFrom maps a Tag's summary, description, and externalDocs onto Docs.
func tagDocsFrom(t *soa.Tag) ir.Docs {
	d := ir.Docs{Summary: t.GetSummary(), Description: t.GetDescription()}
	if ed := t.GetExternalDocs(); ed != nil {
		d.ExternalDocs = append(d.ExternalDocs, ir.Link{URL: ed.GetURL(), Description: ed.GetDescription()})
	}
	return d
}

// lowerPaths lowers every path operation in source order into groups, stopping
// between path items when ctx is done.
func lowerPaths(ctx context.Context, c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, operationIDs map[string]string, groups *serviceGroups) []ir.Diagnostic {
	paths := c.Doc.GetPaths()
	if paths == nil {
		return nil
	}
	var diags []ir.Diagnostic
	for path, rp := range paths.All() {
		if ctx.Err() != nil {
			return diags
		}
		pi, declPtr := resolve.ObjectAt[soa.PathItem](c.RefScope(), rp, ids.Ptr("paths", path))
		if pi == nil {
			continue
		}
		diags = append(diags, lowerPathItem(c, ts, anchors, operationIDs, groups, path, pi, declPtr)...)
	}
	return diags
}

// lowerPathItem lowers each method operation on one path into its group,
// carrying along any callback operations registered under the same group.
// declPtr is pi's own declaration pointer: the path pointer for an inline
// path item, or a referenced path item's component pointer (issue #107) —
// shared parameters and bodies lower from there, while each operation keeps
// its mount pointer (under path) as its identity.
func lowerPathItem(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, operationIDs map[string]string, groups *serviceGroups, path string, pi *soa.PathItem, declPtr string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	pathPtr := ids.Ptr("paths", path)
	for _, po := range pathOperations(pi) {
		key, name, docs, inferred := groupFor(c, po.src, path)
		ptrs := opPointers{mount: pathPtr + po.seg, decl: declPtr + po.seg}
		opCtx := opContext{
			method:        po.method,
			uriTemplate:   path,
			withCallbacks: true,
			inferred:      inferred,
			ptrs:          ptrs,
			params:        mergeParameters(pi.GetParameters(), po.src.GetParameters(), declPtr, ptrs.decl),
		}
		op, extra, opDiags := lowerOperation(c, ts, anchors, operationIDs, po.src, opCtx)
		diags = append(diags, opDiags...)
		diags = append(diags, applyPathItemResidue(c, &op, pi, declPtr)...)
		grp := groups.group(key, func() ir.OperationGroup { return ir.OperationGroup{Name: name, Docs: docs} })
		grp.Operations = append(grp.Operations, op)
		grp.Operations = append(grp.Operations, extra...)
	}
	return diags
}

// lowerWebhooks lowers webhook path items into the dedicated "webhooks" group;
// each webhook operation carries IsWebhook on its HTTP binding. It stops
// between webhooks when ctx is done, as lowerPaths does.
func lowerWebhooks(ctx context.Context, c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, operationIDs map[string]string, groups *serviceGroups) []ir.Diagnostic {
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
		pi, declPtr := resolve.ObjectAt[soa.PathItem](c.RefScope(), rp, hookPtr)
		if pi == nil {
			continue
		}
		for _, po := range pathOperations(pi) {
			ptrs := opPointers{mount: hookPtr + po.seg, decl: declPtr + po.seg}
			opCtx := opContext{
				method:        po.method,
				uriTemplate:   name,
				isWebhook:     true,
				withCallbacks: true,
				ptrs:          ptrs,
				params:        mergeParameters(pi.GetParameters(), po.src.GetParameters(), declPtr, ptrs.decl),
			}
			op, extra, opDiags := lowerOperation(c, ts, anchors, operationIDs, po.src, opCtx)
			diags = append(diags, opDiags...)
			diags = append(diags, applyPathItemResidue(c, &op, pi, declPtr)...)
			grp := groups.group("webhook", func() ir.OperationGroup {
				// A hint, not a source name: no document declares this group. The
				// compiler synthesizes it to hold webhook operations, exactly as it
				// synthesizes the "default" group above, and Naming.Source is the
				// spelling the source used (GitHub #184).
				return ir.OperationGroup{Name: compile.NamingHint("webhooks")}
			})
			grp.Operations = append(grp.Operations, op)
			grp.Operations = append(grp.Operations, extra...)
		}
	}
	return diags
}

// groupFor resolves the group an operation belongs to under the active strategy.
// lowering.GroupByPathPrefix is a heuristic, so it stamps the inferred marker; grouping by
// declared tags is a declared fact and leaves it empty.
func groupFor(c lowering.Ctx, src *soa.Operation, path string) (key string, name ir.Naming, docs ir.Docs, inferred string) {
	if c.Grouping == lowering.GroupByPathPrefix {
		seg := firstPathSegment(path)
		return "seg:" + seg, compile.NamingFor(seg), ir.Docs{}, "group-path-prefix"
	}
	tags := src.GetTags()
	if len(tags) == 0 {
		return "default", compile.NamingHint("default"), ir.Docs{}, ""
	}
	first := tags[0]
	return "tag:" + first, compile.NamingFor(first), tagDocs(c, first), ""
}

// tagDocs returns the declared docs for a tag name, or empty when undeclared.
func tagDocs(c lowering.Ctx, name string) ir.Docs {
	for _, t := range c.Doc.GetTags() {
		if t != nil && t.GetName() == name {
			return tagDocsFrom(t)
		}
	}
	return ir.Docs{}
}

// opPointers pairs the two pointers every operation lowering needs. mount is
// where the operation is written into the document and fixes its identity —
// OpID, provenance, and the root its callback operations hang from. decl is
// where its body is declared: the same node for an inline operation, the
// component's own pointer when the operation was reached through a $ref'd path
// item or callback (issue #107). Passing them as one value is what keeps two
// same-typed pointer arguments from being transposed at a call site.
type opPointers struct {
	mount string
	decl  string
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
	method        string
	uriTemplate   string
	isWebhook     bool
	withCallbacks bool
	inferred      string
	params        []sourcedParam
	ptrs          opPointers
}

// lowerOperation lowers one source operation into the neutral core plus its HTTP
// binding. It returns the operation and any callback operations that must be
// registered alongside it in the same group (ir-design §7.2, §8.1).
func lowerOperation(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, operationIDs map[string]string, src *soa.Operation, opCtx opContext) (ir.Operation, []ir.Operation, []ir.Diagnostic) {
	mount, decl := opCtx.ptrs.mount, opCtx.ptrs.decl
	// Built through the context so the source index is spelled in one place, then
	// marked inferred — the one provenance in this compiler that is.
	opProv := c.ProvenanceAt(decl)
	opProv.Inferred = opCtx.inferred
	opAuth, diags := auth.LowerSecurityRequirements(c, src.Security, decl)
	op := ir.Operation{
		ID:   ids.Op(mount),
		Name: operationName(src, opCtx.method, opCtx.uriTemplate),
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
	params, bindings, paramDiags := lowerParameters(c, ts, anchors, opCtx.params)
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
	hb := ir.HTTPBinding{
		Method:        opCtx.method,
		URITemplate:   opCtx.uriTemplate,
		IsWebhook:     opCtx.isWebhook,
		ParamBindings: bindings,
	}
	diags = append(diags, lowerRequestBody(c, ts, anchors, &op, &hb, src, decl)...)
	var extra []ir.Operation
	if opCtx.withCallbacks {
		var cbExt ir.Unmodeled
		var cbDiags []ir.Diagnostic
		hb.Callbacks, extra, cbExt, cbDiags = lowerCallbacks(c, ts, anchors, operationIDs, src, opCtx.ptrs, opCtx.inferred)
		hb.Unmodeled = annotation.MergeUnmodeled(hb.Unmodeled, cbExt)
		diags = append(diags, cbDiags...)
	}
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{hb}}
	diags = append(diags, applyOperationExtensions(c, &op, src, decl)...)
	diags = append(diags, applyOperationServers(c, &op, src, decl)...)
	return op, extra, append(diags, checkOperationIDUnique(c, operationIDs, op, mount)...)
}

// applyOperationExtensions keeps the operation's own x-* and those of the two
// objects beneath it that lower to no node of their own: its externalDocs,
// since ir.Link holds no Unmodeled map, and its Responses Object, whose
// extensions are the map's own rather than any one response's.
//
// It merges rather than assigns. The operation's map already carries whatever
// its parameters or callbacks wrote by the time this runs, and an assignment
// here would drop them — which is why the servers preservation used to have to
// run after it.
func applyOperationExtensions(c lowering.Ctx, op *ir.Operation, src *soa.Operation, decl string) []ir.Diagnostic {
	ext, diags := annotation.ExtensionsAt(c.SrcIndex,
		annotation.ExtensionSite{Owner: decl, Ext: src.GetExtensions()},
		annotation.ExtensionSite{Scope: "externalDocs", Owner: decl + ids.Ptr("externalDocs"),
			Ext: src.GetExternalDocs().GetExtensions()},
		annotation.ExtensionSite{Scope: "responses", Owner: decl + ids.Ptr("responses"),
			Ext: src.GetResponses().GetExtensions()},
	)
	op.Unmodeled = annotation.MergeUnmodeled(op.Unmodeled, ext)
	return diags
}

// applyOperationServers preserves an operation's own `servers` verbatim under
// Unmodeled, for the same reason applyPathServers preserves the path item's:
// §10 scopes servers by index list at service and channel, and ir.Operation has
// no such list yet, so the scoping is kept raw with an info diagnostic.
//
// It is the overriding half of the pair. OpenAPI says an Operation Object's
// servers override the Path Item Object's, so a document declaring both had the
// superseded list kept and the effective one dropped outright — an emitter
// reading the entry would route to the wrong host, and nothing said so
// (GitHub #39).
//
// The two are kept under separate keys because they are two declarations at two
// pointers, and one map key cannot hold both: writing them to a single key would
// make the surviving list depend on which lowering ran last, silently. The path
// item's keeps the plain `openapi:servers` it already shipped under, so this one
// names its own object rather than renaming what a golden already records.
//
// Unlike applyPathServers, this is called from lowerOperation rather than from
// each route: the operation is lowered in one place, so no route can be added
// later that forgets it — which is exactly how the path-item half came to be
// missing on two of its three routes.
func applyOperationServers(c lowering.Ctx, op *ir.Operation, src *soa.Operation, declPtr string) []ir.Diagnostic {
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

// checkOperationIDUnique reports an operationId claimed by more than one
// operation. OpenAPI requires it to be unique across the whole API, and the
// resolver cannot see this shape: one path item declaring an operationId and
// mounted at two paths is written once but describes two operations. Names are
// presentation, so the IR still records what the document said (invariant 4) —
// but an emitter renders both under one identifier, so the collision has to be
// reported rather than discovered downstream.
//
// operationIDs is the caller's, allocated where the lowering starts, so this
// only ever writes into it — the lazy make it used to carry was unreachable
// from newLowerer, which has always allocated the map up front.
func checkOperationIDUnique(c lowering.Ctx, operationIDs map[string]string, op ir.Operation, mount string) []ir.Diagnostic {
	if op.Name.Source == "" {
		return nil // no operationId: emitters synthesize from the method and path
	}
	if first, seen := operationIDs[op.Name.Source]; seen {
		return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DuplicateOperationID, mount,
			"operationId %q is already used by the operation at %s; "+
				"OpenAPI requires it to be unique across the API", op.Name.Source, first)}
	}
	operationIDs[op.Name.Source] = mount
	return nil
}

// operationName builds an operation's neutral naming: the operationId when
// present (source + canonical words), else an empty source with a method+path
// hint so emitters can synthesize a name.
func operationName(src *soa.Operation, method, uriTemplate string) ir.Naming {
	if id := src.GetOperationID(); id != "" {
		return compile.NamingFor(id)
	}
	return compile.NamingHint(ir.CanonicalWords(method + " " + uriTemplate))
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

// applyPathItemResidue keeps everything a path item declares that an
// ir.Operation has no field for: its servers, its own documentation, and its
// own x-* extensions. Each belongs to the path item rather than to any one
// operation on it, so each is written onto every operation the item declares.
//
// One call per route rather than one per construct. The servers half reached
// only the `paths` walk for exactly as long as each route spelled it out for
// itself (GitHub #39), so they are gathered here and every route calls this —
// a construct added here is then kept on all three routes or on none.
func applyPathItemResidue(c lowering.Ctx, op *ir.Operation, pi *soa.PathItem, declPtr string) []ir.Diagnostic {
	diags := applyPathServers(c, op, pi, declPtr)
	diags = append(diags, applyPathItemDocs(c, op, pi, declPtr)...)
	ext, extDiags := schema.ExtensionsIn(c, pi.GetExtensions(), declPtr, "pathItem")
	op.Unmodeled = annotation.MergeUnmodeled(op.Unmodeled, ext)
	return append(diags, extDiags...)
}

// pathItemDocFields pairs each Path Item Object documentation keyword with the
// Unmodeled key it is kept under. Both keys name the object the text came from:
// an operation's own summary and description are what ir.Docs holds, so a plain
// `openapi:summary` beside them would read as the operation's own.
var pathItemDocFields = []struct{ keyword, key string }{
	{"summary", "openapi:pathItemSummary"},
	{"description", "openapi:pathItemDescription"},
}

// applyPathItemDocs keeps a path item's summary and description verbatim under
// Unmodeled on each operation it holds. Nothing read either one, so both reached
// the IR in no form at all — no field, no Unmodeled entry, no diagnostic
// (GitHub #292).
//
// Kept rather than merged into Docs, deliberately. ir.Docs holds one summary and
// one description and they are the operation's own; a path item's pair documents
// the path. Merging would need a precedence rule against the operation's own,
// would attach to an operation documentation its author did not write — an
// inference, which invariant 6 places in injectable policy rather than in a
// lowering — and would leave an emitter unable to tell the two subjects apart
// afterwards. Preserving takes no position on precedence and loses nothing,
// which is what invariant 2 asks of a construct with no typed home.
//
// ReasonNoIRHome rather than a boundary: the IR could grow a home for this, and
// GitHub #285 is where that class of decision is tracked. The cost is that the
// pair is duplicated onto every operation under the path, exactly as the path
// item's servers already are — a path item is distributed across the operations
// it holds, and there is no ir.PathItem to attach it to.
//
// Two cases this does not reach, both shared with applyPathServers beside it:
//
//   - A path item that mounts no operation keeps nothing, because there is no
//     operation to keep it on. GitHub #383 records that gap for the whole class,
//     documentation included, and the unmounted-path-item work is where it lands.
//   - A pair supplied through a YAML merge key or an alias is read by the model
//     but not by RawChildNode, whose lookup is a plain mapping scan. GetSummary
//     is non-empty, the raw node is nil, and nothing is kept or reported —
//     GitHub #384.
func applyPathItemDocs(c lowering.Ctx, op *ir.Operation, pi *soa.PathItem, declPtr string) []ir.Diagnostic {
	if pi.GetSummary() == "" && pi.GetDescription() == "" {
		return nil
	}
	var diags []ir.Diagnostic
	var kept bool
	for _, f := range pathItemDocFields {
		wrote, fieldDiags := schema.PreserveNode(c, &op.Unmodeled, f.key,
			annotation.RawChildNode(pi.GetRootNode(), f.keyword), ir.ReasonNoIRHome,
			declPtr+ids.Ptr(f.keyword))
		diags = append(diags, fieldDiags...)
		kept = kept || wrote
	}
	if !kept {
		return diags
	}
	return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, op.Provenance,
		"path-item documentation kept under Unmodeled; it documents the path rather than this "+
			"operation, and ir.Docs holds the operation's own summary and description"))
}

// applyPathServers preserves path-item-level servers verbatim under Unmodeled
// on the operation. §10 models servers as Document.Servers with per-scope index
// lists (Service.Servers, Channel.Servers); ir.Operation just has no such list
// yet, so the scoping is kept raw with an info diagnostic — a gap the IR can
// close by adding one, hence ReasonNoIRHome rather than a boundary.
//
// Every route that lowers a path item reaches it through applyPathItemResidue:
// a path, a webhook, and a callback expression are the same object under three
// parents, and a document that overrides the server for one of the latter two
// was losing the override outright while the paths route reported it
// (GitHub #39).
//
// This is the path-item half of the pair; applyOperationServers keeps the
// operation's own list, which overrides this one, under its own key.
func applyPathServers(c lowering.Ctx, op *ir.Operation, pi *soa.PathItem, declPtr string) []ir.Diagnostic {
	if len(pi.GetServers()) == 0 {
		return nil
	}
	kept, diags := schema.PreserveNode(c, &op.Unmodeled, "openapi:servers",
		annotation.RawChildNode(pi.GetRootNode(), "servers"), ir.ReasonNoIRHome, declPtr+ids.Ptr("servers"))
	if !kept {
		return diags
	}
	return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, op.Provenance,
		"path-item servers kept under Unmodeled; an operation has no server-scope list to bind them to"))
}

// lowerResponses splits an operation's responses into success responses
// (status < 400) and error cases (>= 400 and the default), each in source order
// with the default last (ir-design §7.2). opDeclPtr is the operation's own
// declaration pointer, so a $ref'd response interns its content once at its
// component pointer rather than once per mount site (issue #107).
func lowerResponses(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, src *soa.Operation, opDeclPtr string) ([]ir.Response, []ir.ErrorCase, []ir.Diagnostic) {
	// GetResponses never returns nil (it addresses an always-present map), so the
	// loop simply yields nothing when no responses are declared.
	resps := src.GetResponses()
	var responses []ir.Response
	var errs []ir.ErrorCase
	var diags []ir.Diagnostic
	for code, rr := range resps.All() {
		r, rptr := resolve.ObjectAt[soa.Response](c.RefScope(), rr, opDeclPtr+ids.Ptr("responses", code))
		if r == nil {
			continue
		}
		rng, named := statusRange(code)
		if !named {
			diags = append(diags, invalidStatusKeyDiag(c, code, rptr))
		}
		// An unreadable key always takes the else branch, because statusRange pairs
		// a false with the zero range and that is no error range. It has to: an
		// ErrorCase would carry a fault classified from a range nothing derived,
		// and it holds no naming to record the key under either.
		// TestStatusRange_NamesNoStatus is what pins the pairing.
		if isErrorRange(rng) {
			ec, ecDiags := lowerErrorCase(c, ts, anchors, r, rng, rptr)
			diags = append(diags, ecDiags...)
			errs = append(errs, ec)
		} else {
			resp, respDiags := lowerResponse(c, ts, anchors, r, code, statusConditions(rng, named), rptr)
			diags = append(diags, respDiags...)
			responses = append(responses, resp)
		}
	}
	def, dptr := resolve.ObjectAt[soa.Response](c.RefScope(), resps.GetDefault(), opDeclPtr+ids.Ptr("responses", defaultResponseKey))
	if def != nil {
		ec, ecDiags := lowerErrorCase(c, ts, anchors, def, ir.StatusRange{}, dptr)
		diags = append(diags, ecDiags...)
		errs = append(errs, ec)
	}
	return responses, errs, diags
}

// lowerResponse lowers one success response: its naming, status condition,
// payload (all media types), headers, docs, and any raw links preserved for
// later promotion. code is the responses-map key the response is declared under
// and conds is what that key resolved to, which is nothing at all when it named
// no status (see statusConditions).
func lowerResponse(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, r *soa.Response, code string, conds ir.ResponseConditions, rptr string) (ir.Response, []ir.Diagnostic) {
	headers, diags := lowerHeaders(c, ts, anchors, r.GetHeaders(), rptr)
	payload, payloadDiags := lowerPayload(c, ts, anchors, r.GetContent(), rptr, "response")
	diags = append(diags, payloadDiags...)
	resp := ir.Response{
		Name:       responseName(code),
		Conditions: conds,
		Payload:    payload,
		Headers:    headers,
	}
	resp.Docs.Description = r.GetDescription()
	return resp, append(diags, preserveResponseExtras(c, &resp.Unmodeled, r, rptr)...)
}

// preserveResponseExtras keeps what a Response Object declares that has no home
// on the node it lowered to: its links map, and its own x-* extensions.
//
// One helper for both branches on purpose. ir.Response and ir.ErrorCase are two
// lowerings of the same source object, and each construct kept on only one of
// them makes a declaration survive or vanish on nothing but its status code:
// links were kept on a 2xx and dropped on a 4xx, and extensions were read at
// neither (GitHub #275). Adding a construct here reaches both by construction.
//
// The links entry carries ReasonNoIRHome and no diagnostic, as it always has:
// nothing is degraded, the map is in the document, and the gap is one the IR can
// close by growing a links field.
func preserveResponseExtras(c lowering.Ctx, p *ir.Unmodeled, r *soa.Response, rptr string) []ir.Diagnostic {
	_, diags := schema.PreserveNode(c, p, "openapi:links",
		annotation.RawChildNode(r.GetRootNode(), "links"), ir.ReasonNoIRHome, rptr+ids.Ptr("links"))
	ext, extDiags := schema.ExtensionsOf(c, r.GetExtensions(), rptr)
	*p = annotation.MergeUnmodeled(*p, ext)
	return append(diags, extDiags...)
}

// responseName builds a success response's neutral naming. OpenAPI names no
// response — it keys them by status code — which is the case ir-design §7.2
// gives Name a hint for, so the hint is that key: "200", "2_xx", and the shared
// mint for a key with no word in it (GitHub #259).
//
// The key as declared rather than the range it parses to, so a response written
// "2XX" is not named by a spelling its document never used, and one written
// under no status code at all is not named by the catch-all range it silently
// degrades to (GitHub #262).
//
// Source stays empty even for a $ref'd response: §7.2 fills it only "for formats
// with named outputs", and a components/responses key names a reusable
// definition rather than this mount of it — the same component reached at two
// status codes is two responses, told apart by condition. ErrorCase carries no
// Naming at all and so has no counterpart here, and would be held by the
// presence rule at once if it gained one, since irverify does not exempt it.
func responseName(code string) ir.Naming {
	return compile.NamingHint(ir.CanonicalWords(code))
}

// lowerErrorCase lowers one error response into an ErrorCase, classifying its
// fault from the status range and lowering its error-model content.
func lowerErrorCase(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, r *soa.Response, rng ir.StatusRange, rptr string) (ir.ErrorCase, []ir.Diagnostic) {
	ec := ir.ErrorCase{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{rng}},
		Fault:      faultFor(rng),
	}
	ec.Docs.Description = r.GetDescription()
	diags := fillErrorType(c, ts, anchors, &ec, r, rptr)
	diags = append(diags, preserveErrorHeaders(c, &ec, r, rptr)...)
	return ec, append(diags, preserveResponseExtras(c, &ec.Unmodeled, r, rptr)...)
}

// preserveErrorHeaders keeps an error response's headers from being dropped:
// ir.ErrorCase has no Headers field (ir-design §7.2), so when the response
// declares headers they are kept verbatim under Unmodeled with one info
// diagnostic, mirroring the success path's structural header lowering.
func preserveErrorHeaders(c lowering.Ctx, ec *ir.ErrorCase, r *soa.Response, rptr string) []ir.Diagnostic {
	headers := r.GetHeaders()
	if headers == nil || headers.Len() == 0 {
		return nil
	}
	kept, diags := schema.PreserveNode(c, &ec.Unmodeled, "openapi:headers",
		annotation.RawChildNode(r.GetRootNode(), "headers"), ir.ReasonNoIRHome, rptr+ids.Ptr("headers"))
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, rptr,
		"error response headers have no ErrorCase home; kept verbatim under Unmodeled"))
}

// fillErrorType lowers every content entry's schema into the type registry
// (nothing dropped) and points ErrorCase.Type at the first, then keeps the
// content map beside it, since ErrorCase.Type holds a single model reference
// (ir-design §7.2 clarification).
func fillErrorType(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, ec *ir.ErrorCase, r *soa.Response, rptr string) []ir.Diagnostic {
	content := r.GetContent()
	if content == nil || content.Len() == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	first := true
	for mt, media := range content.All() {
		ref, refDiags := schema.Ref(c, ts, anchors, schema.TopLevelDepth, media.GetSchema(), rptr+ids.Ptr("content", mt, "schema"), "error")
		diags = append(diags, refDiags...)
		if first {
			ec.Type = ref
			first = false
		}
	}
	return append(diags, preserveErrorContent(c, ec, r, rptr, content.Len())...)
}

// preserveErrorContent keeps an error response's content map verbatim under
// Unmodeled, whatever its arity.
//
// ir.ErrorCase holds a TypeRef and no media type at all, so one entry loses the
// media type it was keyed by just as surely as several lose the entries past the
// first: an error declared only as application/problem+json reached the IR
// indistinguishable from one declared as application/json. Only the multi-entry
// case used to be kept, which made the single-entry loss the quieter of two
// halves of one gap rather than a different kind of thing (GitHub #39).
//
// n is the entry count, and picks which of the two the diagnostic names, so a
// reader is told what was actually lost rather than a message covering both.
func preserveErrorContent(c lowering.Ctx, ec *ir.ErrorCase, r *soa.Response, rptr string, n int) []ir.Diagnostic {
	kept, diags := schema.PreserveNode(c, &ec.Unmodeled, "openapi:content",
		annotation.RawChildNode(r.GetRootNode(), "content"), ir.ReasonNoIRHome, rptr+ids.Ptr("content"))
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, rptr,
			"%s", errorContentMessage(n)))
	}
	return diags
}

// errorContentMessage names which loss the kept content map stands for: entries
// past the first when there are several, and the sole media type's own key when
// there is one.
func errorContentMessage(n int) string {
	if n > 1 {
		return "error response has multiple media types; full content map kept under Unmodeled"
	}
	return "error response media type has no ErrorCase home; content map kept under Unmodeled"
}

// lowerCallbacks lowers each callback expression's path-item operations as
// Operations registered in the parent's group, and binds them to the parent via
// HTTPBinding.Callbacks keyed by the runtime expression (ir-design §8.1).
// parent.mount roots callback operation identity, so two parents sharing one
// $ref'd callback keep distinct callback operations; parent.decl is the base a
// $ref'd callback or path item resolves against (issue #107).
func lowerCallbacks(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, operationIDs map[string]string, src *soa.Operation, parent opPointers, inferred string) ([]ir.Callback, []ir.Operation, ir.Unmodeled, []ir.Diagnostic) {
	cbMap := src.GetCallbacks()
	if cbMap == nil || cbMap.Len() == 0 {
		return nil, nil, nil, nil
	}
	var callbacks []ir.Callback
	var ops []ir.Operation
	var ext ir.Unmodeled
	var diags []ir.Diagnostic
	for cbName, rcb := range cbMap.All() {
		cb, cbDecl := resolve.ObjectAt[soa.Callback](c.RefScope(), rcb, parent.decl+ids.Ptr("callbacks", cbName))
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
			pi, piDecl := resolve.ObjectAt[soa.PathItem](c.RefScope(), rp, cbDecl+ids.Ptr(exprStr))
			if pi == nil {
				continue
			}
			cbPtrs := opPointers{mount: parent.mount + ids.Ptr("callbacks", cbName, exprStr), decl: piDecl}
			opIDs, cbOps, cbDiags := lowerCallbackOps(c, ts, anchors, operationIDs, pi, cbPtrs, exprStr, inferred)
			diags = append(diags, cbDiags...)
			callbacks = append(callbacks, ir.Callback{Expression: exprStr, Operations: opIDs})
			ops = append(ops, cbOps...)
		}
	}
	return callbacks, ops, ext, diags
}

// lowerCallbackOps lowers a callback expression's path-item operations. Callback
// operations do not recurse into their own callbacks (withCallbacks stays
// false), which bounds the lowering to the declared out-of-band set. cb pairs
// the expression's identity base (distinct per parent operation) with its
// declaration base (shared when the callback or its path item is $ref'd;
// issue #107).
func lowerCallbackOps(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, operationIDs map[string]string, pi *soa.PathItem, cb opPointers, expr, inferred string) ([]ir.OpID, []ir.Operation, []ir.Diagnostic) {
	declared := pathOperations(pi)
	opIDs := make([]ir.OpID, 0, len(declared))
	ops := make([]ir.Operation, 0, len(declared))
	var diags []ir.Diagnostic
	for _, po := range declared {
		ptrs := opPointers{mount: cb.mount + po.seg, decl: cb.decl + po.seg}
		opCtx := opContext{
			method:      po.method,
			uriTemplate: expr,
			inferred:    inferred,
			ptrs:        ptrs,
			params:      mergeParameters(pi.GetParameters(), po.src.GetParameters(), cb.decl, ptrs.decl),
		}
		op, _, opDiags := lowerOperation(c, ts, anchors, operationIDs, po.src, opCtx)
		diags = append(diags, opDiags...)
		diags = append(diags, applyPathItemResidue(c, &op, pi, cb.decl)...)
		opIDs = append(opIDs, op.ID)
		ops = append(ops, op)
	}
	return opIDs, ops, diags
}

// sourcedParam pairs a parameter with its declaring list entry's pointer,
// because path-item parameters merge into every operation on the path item and
// so cannot borrow the operation's index space (issue #36). A $ref entry's own
// declaration lies one hop further on, resolved in lowerParameters.
type sourcedParam struct {
	ref     *soa.ReferencedParameter
	pointer string
}

// mergeParameters merges path-item parameters with operation parameters using
// use-site precedence: an operation parameter with the same (name, in) overrides
// the path-item one; unshadowed path-item parameters follow in source order.
// Each entry keeps the pointer of its own declaration site rather than a
// position recomputed from the merged slice (issue #36). Both bases are
// declaration pointers — a $ref'd path item's own component pointer, not the
// path it is mounted at (issue #107).
func mergeParameters(pathParams, opParams []*soa.ReferencedParameter, pathDeclPtr, opDeclPtr string) []sourcedParam {
	merged := make([]sourcedParam, 0, len(opParams)+len(pathParams))
	merged = appendSourced(merged, opParams, opDeclPtr, nil)
	if len(pathParams) == 0 {
		return merged
	}
	return appendSourced(merged, pathParams, pathDeclPtr, shadowedKeys(opParams))
}

// appendSourced appends params to dst, pairing each with the pointer of its own
// declaration position under base and skipping any whose (name, in) key is in
// shadowed. A nil shadowed map reads as all-false, which is what the operation
// side passes: operation parameters are never shadowed.
func appendSourced(dst []sourcedParam, params []*soa.ReferencedParameter, base string, shadowed map[string]bool) []sourcedParam {
	for i, p := range params {
		if key, ok := paramKey(p); ok && shadowed[key] {
			continue
		}
		dst = append(dst, sourcedParam{ref: p, pointer: base + ids.Ptr("parameters", strconv.Itoa(i))})
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

// statusRange maps an OpenAPI response key to an inclusive status range: "200" →
// {200,200}, "4XX" → {400,499}, "default" → {0,0} (ir-design §7.2). ok reports
// whether the key named a status at all, and a false always comes paired with
// the zero range — callers route on that, so it is a postcondition rather than
// an incidental return.
//
// Past "default", both forms are three characters over the same leading digit:
// OpenAPI defines exactly the wildcards 1XX through 5XX, and HTTP defines codes
// 100 through 599. Every character is checked, which is what the two loosenings
// this replaced did not do — the wildcard test read the first two and ignored
// the third, so "1XY" lowered as 1XX, and it admitted any leading digit up to 9,
// so "9XX" lowered as a range no OpenAPI document can declare (GitHub #262).
//
// "default" is answered although no caller asks it today: soa.Responses carries
// that entry on its own Default field, so it never appears in the map
// lowerResponses walks. The vocabulary belongs to the function rather than to
// its current caller — answering false for a key OpenAPI reserves would warn on
// a correct document the moment anything did pass it.
func statusRange(code string) (ir.StatusRange, bool) {
	if code == defaultResponseKey {
		return ir.StatusRange{}, true
	}
	if len(code) != statusKeyLen || code[0] < '1' || code[0] > '5' {
		return ir.StatusRange{}, false
	}
	base := int(code[0]-'0') * 100
	if isWildcard(code[1]) && isWildcard(code[2]) {
		return ir.StatusRange{From: base, To: base + 99}, true
	}
	rest, ok := twoDigits(code[1], code[2])
	if !ok {
		return ir.StatusRange{}, false
	}
	return ir.StatusRange{From: base + rest, To: base + rest}, true
}

// isWildcard reports whether b is the wildcard character of a status range.
// OpenAPI writes it uppercase; lowercase is read too, because the reading is
// unambiguous and documents in the wild spell it both ways.
func isWildcard(b byte) bool { return b == 'X' || b == 'x' }

// twoDigits reads the tens and units of a status-code key, reporting false when
// either character is not a digit. It is what separates "200" from "20A" and
// from the half-wildcard "2X0", neither of which names a status.
func twoDigits(tens, units byte) (int, bool) {
	if tens < '0' || tens > '9' || units < '0' || units > '9' {
		return 0, false
	}
	return int(tens-'0')*10 + int(units-'0'), true
}

// statusConditions records the status a response applies to, or no status at all
// when its key named none.
//
// Not the catch-all: {0,0} is the range "default" lowers to, so folding an
// unreadable key into it makes a typo indistinguishable from a declared default,
// and a document carrying both produces two responses claiming the catch-all.
// Recording nothing is the closest true statement available — ResponseConditions
// holds the statuses a response applies to, and none could be read.
//
// Nothing is lost by declining to guess: the key survives as the response's name
// hint, and the warning beside it carries the pointer the key is spelled in.
func statusConditions(rng ir.StatusRange, ok bool) ir.ResponseConditions {
	if !ok {
		return ir.ResponseConditions{}
	}
	return ir.ResponseConditions{StatusCodes: []ir.StatusRange{rng}}
}

// invalidStatusKeyDiag reports a responses-map key that names no status.
//
// A warning rather than an error: the response itself lowers completely — body,
// headers, docs, name — and only the status it answers to is unknown, so the
// document still compiles to IR worth having. It also keeps a fixture carrying
// one reachable, since harness.Check returns at the first error diagnostic and
// FuzzCompile skips an input that produces one, which would put every oracle
// past this point out of reach of the case that provokes it.
func invalidStatusKeyDiag(c lowering.Ctx, code, rptr string) ir.Diagnostic {
	return c.DiagAt(ir.SeverityWarning, diag.InvalidStatusKey, rptr,
		"response key %q is no status code, no 1XX-5XX range, and not %s; "+
			"the response is kept with no status condition", code, defaultResponseKey)
}

// isErrorRange reports whether a status range denotes an error (>= 400).
func isErrorRange(r ir.StatusRange) bool { return r.From >= 400 }

// faultFor classifies a status range as a client or server fault; the catch-all
// default range ({0,0}) is unclassified (ir-design §7.2).
func faultFor(r ir.StatusRange) string {
	switch {
	case r.From >= 400 && r.From <= 499:
		return "client"
	case r.From >= 500 && r.From <= 599:
		return "server"
	default:
		return ""
	}
}

// firstPathSegment returns the first non-empty segment of a path, or "" when the
// path has none.
func firstPathSegment(path string) string {
	for seg := range strings.SplitSeq(path, "/") {
		if seg != "" {
			return seg
		}
	}
	return ""
}

// serviceGroups accumulates operation groups keyed by a namespaced key while
// preserving first-seen insertion order, so a group's operations gather across
// paths without reordering the groups themselves.
type serviceGroups struct {
	order []string
	byKey map[string]*ir.OperationGroup
}

// newServiceGroups returns an empty group accumulator.
func newServiceGroups() *serviceGroups {
	return &serviceGroups{byKey: make(map[string]*ir.OperationGroup)}
}

// group returns the group for key, creating it via mk on first sight and
// recording its insertion order.
func (g *serviceGroups) group(key string, mk func() ir.OperationGroup) *ir.OperationGroup {
	if existing, ok := g.byKey[key]; ok {
		return existing
	}
	g.byKey[key] = new(mk())
	g.order = append(g.order, key)
	return g.byKey[key]
}

// finalize returns the accumulated groups in insertion order.
func (g *serviceGroups) finalize() []ir.OperationGroup {
	out := make([]ir.OperationGroup, 0, len(g.order))
	for _, k := range g.order {
		out = append(out, *g.byKey[k])
	}
	return out
}
