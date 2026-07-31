package openapi

import (
	"strconv"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// httpMethods is the fixed set of HTTP method accessors on a PathItem, iterated
// in this order so operation lowering is deterministic across runs. The name is
// the wire method in lowercase; pointers and IDs derive from it.
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
}

// lowerService lowers one document into a single Service: its identity and docs,
// the declared tag registry, and every path, webhook, and callback operation
// placed into groups per the configured grouping strategy (ir-design §7.1).
func lowerService(c lowerCtx, ts *compile.Types, anchors *anchorIndex, operationIDs map[string]string) (ir.Service, []ir.TagDef, []ir.Diagnostic) {
	svc := ir.Service{
		ID:         ids.Service(c.SrcIndex),
		Provenance: c.provenanceAt(""),
	}
	if info := c.Doc.GetInfo(); info != nil {
		title := info.GetTitle()
		svc.Name = compile.NamingFor(title)
		svc.Docs.Description = info.GetDescription()
	}
	svcAuth, diags := lowerSecurityRequirements(c, c.Doc.GetSecurity())
	svc.Auth = svcAuth
	groups := newServiceGroups()
	diags = append(diags, lowerPaths(c, ts, anchors, operationIDs, groups)...)
	diags = append(diags, lowerWebhooks(c, ts, anchors, operationIDs, groups)...)
	svc.Groups = groups.finalize()
	return svc, lowerTagDefs(c), diags
}

// lowerTagDefs registers the document's declared tag metadata into TagDefs; tag
// membership itself stays as []string on each tagged operation.
func lowerTagDefs(c lowerCtx) []ir.TagDef {
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

// lowerPaths lowers every path operation in source order into groups.
func lowerPaths(c lowerCtx, ts *compile.Types, anchors *anchorIndex, operationIDs map[string]string, groups *serviceGroups) []ir.Diagnostic {
	paths := c.Doc.GetPaths()
	if paths == nil {
		return nil
	}
	var diags []ir.Diagnostic
	for path, rp := range paths.All() {
		pi, declPtr := resolveRefAt[soa.PathItem](c, rp, ids.Ptr("paths", path))
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
func lowerPathItem(c lowerCtx, ts *compile.Types, anchors *anchorIndex, operationIDs map[string]string, groups *serviceGroups, path string, pi *soa.PathItem, declPtr string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	pathPtr := ids.Ptr("paths", path)
	for _, m := range httpMethods {
		src := m.get(pi)
		if src == nil {
			continue
		}
		key, name, docs, inferred := groupFor(c, src, path)
		ptrs := opPointers{mount: pathPtr + ids.Ptr(m.name), decl: declPtr + ids.Ptr(m.name)}
		opCtx := opContext{
			method:        m.name,
			uriTemplate:   path,
			withCallbacks: true,
			inferred:      inferred,
			ptrs:          ptrs,
			params:        mergeParameters(pi.GetParameters(), src.GetParameters(), declPtr, ptrs.decl),
		}
		op, extra, opDiags := lowerOperation(c, ts, anchors, operationIDs, src, opCtx)
		diags = append(diags, opDiags...)
		diags = append(diags, applyPathServers(c, &op, pi, declPtr)...)
		grp := groups.group(key, func() ir.OperationGroup { return ir.OperationGroup{Name: name, Docs: docs} })
		grp.Operations = append(grp.Operations, op)
		grp.Operations = append(grp.Operations, extra...)
	}
	return diags
}

// lowerWebhooks lowers webhook path items into the dedicated "webhooks" group;
// each webhook operation carries IsWebhook on its HTTP binding.
func lowerWebhooks(c lowerCtx, ts *compile.Types, anchors *anchorIndex, operationIDs map[string]string, groups *serviceGroups) []ir.Diagnostic {
	hooks := c.Doc.GetWebhooks()
	if hooks == nil || hooks.Len() == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	for name, rp := range hooks.All() {
		hookPtr := ids.Ptr("webhooks", name)
		pi, declPtr := resolveRefAt[soa.PathItem](c, rp, hookPtr)
		if pi == nil {
			continue
		}
		for _, m := range httpMethods {
			src := m.get(pi)
			if src == nil {
				continue
			}
			ptrs := opPointers{mount: hookPtr + ids.Ptr(m.name), decl: declPtr + ids.Ptr(m.name)}
			opCtx := opContext{
				method:        m.name,
				uriTemplate:   name,
				isWebhook:     true,
				withCallbacks: true,
				ptrs:          ptrs,
				params:        mergeParameters(pi.GetParameters(), src.GetParameters(), declPtr, ptrs.decl),
			}
			op, extra, opDiags := lowerOperation(c, ts, anchors, operationIDs, src, opCtx)
			diags = append(diags, opDiags...)
			grp := groups.group("webhook", func() ir.OperationGroup {
				// A hint, not a source name: no document declares this group. The
				// compiler synthesizes it to hold webhook operations, exactly as it
				// synthesizes the "default" group above, and Naming.Source is the
				// spelling the source used (GitHub #184).
				return ir.OperationGroup{Name: ir.Naming{Hint: "webhooks"}}
			})
			grp.Operations = append(grp.Operations, op)
			grp.Operations = append(grp.Operations, extra...)
		}
	}
	return diags
}

// groupFor resolves the group an operation belongs to under the active strategy.
// GroupByPathPrefix is a heuristic, so it stamps the inferred marker; grouping by
// declared tags is a declared fact and leaves it empty.
func groupFor(c lowerCtx, src *soa.Operation, path string) (key string, name ir.Naming, docs ir.Docs, inferred string) {
	if c.Opts.Grouping == GroupByPathPrefix {
		seg := firstPathSegment(path)
		return "seg:" + seg, compile.NamingFor(seg), ir.Docs{}, "group-path-prefix"
	}
	tags := src.GetTags()
	if len(tags) == 0 {
		return "default", ir.Naming{Hint: "default"}, ir.Docs{}, ""
	}
	first := tags[0]
	return "tag:" + first, compile.NamingFor(first), tagDocs(c, first), ""
}

// tagDocs returns the declared docs for a tag name, or empty when undeclared.
func tagDocs(c lowerCtx, name string) ir.Docs {
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
func lowerOperation(c lowerCtx, ts *compile.Types, anchors *anchorIndex, operationIDs map[string]string, src *soa.Operation, opCtx opContext) (ir.Operation, []ir.Operation, []ir.Diagnostic) {
	mount, decl := opCtx.ptrs.mount, opCtx.ptrs.decl
	// Built through the context so the source index is spelled in one place, then
	// marked inferred — the one provenance in this compiler that is.
	opProv := c.provenanceAt(decl)
	opProv.Inferred = opCtx.inferred
	opAuth, diags := lowerSecurityRequirements(c, src.Security)
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
	hb := ir.HTTPBinding{
		Method:        strings.ToUpper(opCtx.method),
		URITemplate:   opCtx.uriTemplate,
		IsWebhook:     opCtx.isWebhook,
		ParamBindings: bindings,
	}
	diags = append(diags, lowerRequestBody(c, ts, anchors, &op, &hb, src, decl)...)
	var extra []ir.Operation
	if opCtx.withCallbacks {
		var cbDiags []ir.Diagnostic
		hb.Callbacks, extra, cbDiags = lowerCallbacks(c, ts, anchors, operationIDs, src, opCtx.ptrs, opCtx.inferred)
		diags = append(diags, cbDiags...)
	}
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{hb}}
	ext, extDiags := extensionsOf(c, src.GetExtensions(), decl)
	diags = append(diags, extDiags...)
	if len(ext) > 0 {
		op.Unmodeled = ext
	}
	return op, extra, append(diags, checkOperationIDUnique(c, operationIDs, op, mount)...)
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
func checkOperationIDUnique(c lowerCtx, operationIDs map[string]string, op ir.Operation, mount string) []ir.Diagnostic {
	if op.Name.Source == "" {
		return nil // no operationId: emitters synthesize from the method and path
	}
	if first, seen := operationIDs[op.Name.Source]; seen {
		return []ir.Diagnostic{c.diagAt(ir.SeverityWarning, diag.DuplicateOperationID, mount,
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
	return ir.Naming{Hint: ir.CanonicalWords(method + " " + uriTemplate)}
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

// applyPathServers preserves path-item-level servers verbatim under Unmodeled
// on the operation. §10 models servers as Document.Servers with per-scope index
// lists (Service.Servers, Channel.Servers); ir.Operation just has no such list
// yet, so the scoping is kept raw with an info diagnostic — a gap the IR can
// close by adding one, hence ReasonNoIRHome rather than a boundary.
func applyPathServers(c lowerCtx, op *ir.Operation, pi *soa.PathItem, declPtr string) []ir.Diagnostic {
	if len(pi.GetServers()) == 0 {
		return nil
	}
	kept, diags := preserveNode(c, &op.Unmodeled, "openapi:servers",
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
func lowerResponses(c lowerCtx, ts *compile.Types, anchors *anchorIndex, src *soa.Operation, opDeclPtr string) ([]ir.Response, []ir.ErrorCase, []ir.Diagnostic) {
	// GetResponses never returns nil (it addresses an always-present map), so the
	// loop simply yields nothing when no responses are declared.
	resps := src.GetResponses()
	var responses []ir.Response
	var errs []ir.ErrorCase
	var diags []ir.Diagnostic
	for code, rr := range resps.All() {
		r, rptr := resolveRefAt[soa.Response](c, rr, opDeclPtr+ids.Ptr("responses", code))
		if r == nil {
			continue
		}
		rng := statusRange(code)
		if isErrorRange(rng) {
			ec, ecDiags := lowerErrorCase(c, ts, anchors, r, rng, rptr)
			diags = append(diags, ecDiags...)
			errs = append(errs, ec)
		} else {
			resp, respDiags := lowerResponse(c, ts, anchors, r, rng, rptr)
			diags = append(diags, respDiags...)
			responses = append(responses, resp)
		}
	}
	def, dptr := resolveRefAt[soa.Response](c, resps.GetDefault(), opDeclPtr+ids.Ptr("responses", "default"))
	if def != nil {
		ec, ecDiags := lowerErrorCase(c, ts, anchors, def, ir.StatusRange{}, dptr)
		diags = append(diags, ecDiags...)
		errs = append(errs, ec)
	}
	return responses, errs, diags
}

// lowerResponse lowers one success response: its status condition, payload (all
// media types), headers, docs, and any raw links preserved for later promotion.
func lowerResponse(c lowerCtx, ts *compile.Types, anchors *anchorIndex, r *soa.Response, rng ir.StatusRange, rptr string) (ir.Response, []ir.Diagnostic) {
	headers, diags := lowerHeaders(c, ts, anchors, r.GetHeaders(), rptr)
	payload, payloadDiags := lowerPayload(c, ts, anchors, r.GetContent(), rptr, "response")
	diags = append(diags, payloadDiags...)
	resp := ir.Response{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{rng}},
		Payload:    payload,
		Headers:    headers,
	}
	resp.Docs.Description = r.GetDescription()
	_, linkDiags := preserveNode(c, &resp.Unmodeled, "openapi:links",
		annotation.RawChildNode(r.GetRootNode(), "links"), ir.ReasonNoIRHome, rptr+ids.Ptr("links"))
	return resp, append(diags, linkDiags...)
}

// lowerErrorCase lowers one error response into an ErrorCase, classifying its
// fault from the status range and lowering its error-model content.
func lowerErrorCase(c lowerCtx, ts *compile.Types, anchors *anchorIndex, r *soa.Response, rng ir.StatusRange, rptr string) (ir.ErrorCase, []ir.Diagnostic) {
	ec := ir.ErrorCase{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{rng}},
		Fault:      faultFor(rng),
	}
	ec.Docs.Description = r.GetDescription()
	diags := fillErrorType(c, ts, anchors, &ec, r, rptr)
	return ec, append(diags, preserveErrorHeaders(c, &ec, r, rptr)...)
}

// preserveErrorHeaders keeps an error response's headers from being dropped:
// ir.ErrorCase has no Headers field (ir-design §7.2), so when the response
// declares headers they are kept verbatim under Unmodeled with one info
// diagnostic, mirroring the success path's structural header lowering.
func preserveErrorHeaders(c lowerCtx, ec *ir.ErrorCase, r *soa.Response, rptr string) []ir.Diagnostic {
	headers := r.GetHeaders()
	if headers == nil || headers.Len() == 0 {
		return nil
	}
	kept, diags := preserveNode(c, &ec.Unmodeled, "openapi:headers",
		annotation.RawChildNode(r.GetRootNode(), "headers"), ir.ReasonNoIRHome, rptr+ids.Ptr("headers"))
	if !kept {
		return diags
	}
	return append(diags, c.diagAt(ir.SeverityInfo, diag.DegradedConstruct, rptr,
		"error response headers have no ErrorCase home; kept verbatim under Unmodeled"))
}

// fillErrorType lowers every content entry's schema into the type registry
// (nothing dropped) and points ErrorCase.Type at the first. When more than one
// media type exists, the full content map is preserved raw with an info
// diagnostic, since ErrorCase.Type holds a single model reference (ir-design
// §7.2 clarification).
func fillErrorType(c lowerCtx, ts *compile.Types, anchors *anchorIndex, ec *ir.ErrorCase, r *soa.Response, rptr string) []ir.Diagnostic {
	content := r.GetContent()
	if content == nil || content.Len() == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	first := true
	for mt, media := range content.All() {
		ref, refDiags := schemaRef(c, ts, anchors, topLevelDepth, media.GetSchema(), rptr+ids.Ptr("content", mt, "schema"), "error")
		diags = append(diags, refDiags...)
		if first {
			ec.Type = ref
			first = false
		}
	}
	if content.Len() > 1 {
		kept, keptDiags := preserveNode(c, &ec.Unmodeled, "openapi:content",
			annotation.RawChildNode(r.GetRootNode(), "content"), ir.ReasonNoIRHome, rptr+ids.Ptr("content"))
		diags = append(diags, keptDiags...)
		if kept {
			diags = append(diags, c.diagAt(ir.SeverityInfo, diag.DegradedConstruct, rptr,
				"error response has multiple media types; full content map kept under Unmodeled"))
		}
	}
	return diags
}

// lowerCallbacks lowers each callback expression's path-item operations as
// Operations registered in the parent's group, and binds them to the parent via
// HTTPBinding.Callbacks keyed by the runtime expression (ir-design §8.1).
// parent.mount roots callback operation identity, so two parents sharing one
// $ref'd callback keep distinct callback operations; parent.decl is the base a
// $ref'd callback or path item resolves against (issue #107).
func lowerCallbacks(c lowerCtx, ts *compile.Types, anchors *anchorIndex, operationIDs map[string]string, src *soa.Operation, parent opPointers, inferred string) ([]ir.Callback, []ir.Operation, []ir.Diagnostic) {
	cbMap := src.GetCallbacks()
	if cbMap == nil || cbMap.Len() == 0 {
		return nil, nil, nil
	}
	var callbacks []ir.Callback
	var ops []ir.Operation
	var diags []ir.Diagnostic
	for cbName, rcb := range cbMap.All() {
		cb, cbDecl := resolveRefAt[soa.Callback](c, rcb, parent.decl+ids.Ptr("callbacks", cbName))
		if cb == nil {
			continue
		}
		for expr, rp := range cb.All() {
			exprStr := string(expr)
			pi, piDecl := resolveRefAt[soa.PathItem](c, rp, cbDecl+ids.Ptr(exprStr))
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
	return callbacks, ops, diags
}

// lowerCallbackOps lowers a callback expression's path-item operations. Callback
// operations do not recurse into their own callbacks (withCallbacks stays
// false), which bounds the lowering to the declared out-of-band set. cb pairs
// the expression's identity base (distinct per parent operation) with its
// declaration base (shared when the callback or its path item is $ref'd;
// issue #107).
func lowerCallbackOps(c lowerCtx, ts *compile.Types, anchors *anchorIndex, operationIDs map[string]string, pi *soa.PathItem, cb opPointers, expr, inferred string) ([]ir.OpID, []ir.Operation, []ir.Diagnostic) {
	var opIDs []ir.OpID
	var ops []ir.Operation
	var diags []ir.Diagnostic
	for _, m := range httpMethods {
		src := m.get(pi)
		if src == nil {
			continue
		}
		ptrs := opPointers{mount: cb.mount + ids.Ptr(m.name), decl: cb.decl + ids.Ptr(m.name)}
		opCtx := opContext{
			method:      m.name,
			uriTemplate: expr,
			inferred:    inferred,
			ptrs:        ptrs,
			params:      mergeParameters(pi.GetParameters(), src.GetParameters(), cb.decl, ptrs.decl),
		}
		op, _, opDiags := lowerOperation(c, ts, anchors, operationIDs, src, opCtx)
		diags = append(diags, opDiags...)
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
	p := resolveRef[soa.Parameter](rp)
	if p == nil {
		return "", false
	}
	return string(p.GetIn()) + "\x00" + p.GetName(), true
}

// statusRange maps an OpenAPI response key to an inclusive status range: "200" →
// {200,200}, "4XX" → {400,499}, "default" → {0,0} (ir-design §7.2).
func statusRange(code string) ir.StatusRange {
	if code == "default" {
		return ir.StatusRange{}
	}
	if len(code) == 3 && (code[1] == 'X' || code[1] == 'x') && code[0] >= '1' && code[0] <= '9' {
		base := int(code[0]-'0') * 100
		return ir.StatusRange{From: base, To: base + 99}
	}
	n, err := strconv.Atoi(code)
	if err != nil {
		return ir.StatusRange{}
	}
	return ir.StatusRange{From: n, To: n}
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

// referencedEntry is the method set every soa "Referenced*" alias exposes: it
// is the generic form of speakeasy's Reference[T, V, C], which underlies
// ReferencedPathItem, ReferencedResponse, ReferencedHeader, ReferencedCallback,
// ReferencedParameter, ReferencedRequestBody, ReferencedExample, and
// ReferencedSecurityScheme alike. Naming the shape once here lets resolveRef
// stand in for what would otherwise be one resolveX per aliased type. S is the
// reference's own type, Reference[T, V, C]; resolveRefAt walks it through
// GetReferenceResolutionInfo to follow a chain of component aliases.
type referencedEntry[T, S any] interface {
	GetObject() *T
	GetResolvedObject() *T
	GetReference() references.Reference
	GetReferenceResolutionInfo() *references.ResolveResult[S]
}

// resolveRef returns the concrete value of a reference-or-inline entry,
// preferring the inline object and falling back to the resolved target. The
// *S term constrains R to a pointer type so `ref == nil` is legal in generic
// code; interfaces.Validator[T] is unavailable here (it lives in the
// library's internal/ tree), which is why R is expressed via *S rather than
// V directly.
func resolveRef[T, S any, R interface {
	*S
	referencedEntry[T, S]
}](ref R) *T {
	if ref == nil {
		return nil
	}
	if obj := ref.GetObject(); obj != nil {
		return obj
	}
	// GetResolvedObject is itself nil-safe and delegates to GetObject, but the
	// fallback stays explicit rather than coupling this compiler to that
	// undocumented nil-tolerance.
	return ref.GetResolvedObject()
}

// maxRefChain bounds how many $ref hops resolveRefAt follows to a declaration
// (styleguide bounded-everything rule). A $ref cycle among the non-schema
// components this walks is refused before lowering by speakeasy's resolver, not
// by cycles.go — that scan covers schema positions only and delegates these (see
// its header) — and a refused chain resolves to nothing, so resolveRefAt returns
// before the loop. The bound therefore only ever fires on an absurd alias chain.
const maxRefChain = 32

// resolveRefAt returns a reference-or-inline entry's concrete value together
// with the pointer of the declaration it resolves to, walking through any
// chained component aliases to the last one written in this document (issue
// #107). usePtr — the entry's own position — stands whenever the chain has no
// declaration addressable here: an inline entry, a reference that leaves this
// document, or one that outruns maxRefChain. An alias pointer is never
// returned on its own, since a one-key $ref object has no children to hoist.
func resolveRefAt[T, S any, R interface {
	*S
	referencedEntry[T, S]
}](c lowerCtx, ref R, usePtr string) (*T, string) {
	obj := resolveRef[T, S, R](ref)
	if obj == nil {
		return nil, usePtr
	}
	// cand advances one hop at a time but is adopted only once the chain ends
	// at a declaration written here, which is what keeps a chain that exits the
	// document from returning the last alias it passed through.
	pointer, cand := usePtr, usePtr
	for range maxRefChain {
		// GetReferenceResolutionInfo is nil once the chain reaches a
		// non-reference entry — the terminator, and why an inline entry
		// exits on the first pass (couples this to that library contract).
		info := ref.GetReferenceResolutionInfo()
		if info == nil {
			pointer = cand
			break
		}
		target, ok := c.refScope().InternalPointer(ref.GetReference().String())
		if !ok {
			break // another document: nothing addressable here, so usePtr stands
		}
		cand = target
		// obj != nil means the whole chain resolved, so info.Object is non-nil
		// at every hop this loop takes; a nil one would still be safe, since
		// the getters above are nil-receiver tolerant and end the walk.
		ref = R(info.Object)
	}
	return obj, pointer
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
