package openapi

import (
	"strconv"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

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
func (l *lowerer) lowerService() ir.Service {
	svc := ir.Service{
		ID:         serviceID(l.srcIndex),
		Provenance: ir.Provenance{Source: l.srcIndex},
	}
	if info := l.doc.GetInfo(); info != nil {
		title := info.GetTitle()
		svc.Name = ir.Naming{Source: title, Canonical: canonicalWords(title)}
		svc.Docs.Description = info.GetDescription()
	}
	svc.Auth = l.lowerSecurityRequirements(l.doc.GetSecurity())
	l.lowerTagDefs()
	groups := newServiceGroups()
	l.lowerPaths(groups)
	l.lowerWebhooks(groups)
	svc.Groups = groups.finalize()
	return svc
}

// lowerTagDefs registers the document's declared tag metadata into TagDefs; tag
// membership itself stays as []string on each tagged operation.
func (l *lowerer) lowerTagDefs() {
	tags := l.doc.GetTags()
	if len(tags) == 0 {
		return
	}
	defs := make([]ir.TagDef, 0, len(tags))
	for _, t := range tags {
		if t == nil {
			continue
		}
		defs = append(defs, ir.TagDef{Name: t.GetName(), Docs: tagDocsFrom(t)})
	}
	l.out.TagDefs = defs
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
func (l *lowerer) lowerPaths(groups *serviceGroups) {
	paths := l.doc.GetPaths()
	if paths == nil {
		return
	}
	for path, rp := range paths.All() {
		pi, declPtr := resolveRefAt[soa.PathItem](l, rp, ptr("paths", path))
		if pi == nil {
			continue
		}
		l.lowerPathItem(groups, path, pi, declPtr)
	}
}

// lowerPathItem lowers each method operation on one path into its group,
// carrying along any callback operations registered under the same group.
// declPtr is pi's own declaration pointer: the path pointer for an inline
// path item, or a referenced path item's component pointer (issue #107) —
// shared parameters and bodies lower from there, while each operation keeps
// its mount pointer (under path) as its identity.
func (l *lowerer) lowerPathItem(groups *serviceGroups, path string, pi *soa.PathItem, declPtr string) {
	pathPtr := ptr("paths", path)
	for _, m := range httpMethods {
		src := m.get(pi)
		if src == nil {
			continue
		}
		key, name, docs, inferred := l.groupFor(src, path)
		ptrs := opPointers{mount: pathPtr + ptr(m.name), decl: declPtr + ptr(m.name)}
		ctx := opContext{
			method:        m.name,
			uriTemplate:   path,
			withCallbacks: true,
			inferred:      inferred,
			ptrs:          ptrs,
			params:        mergeParameters(pi.GetParameters(), src.GetParameters(), declPtr, ptrs.decl),
		}
		op, extra := l.lowerOperation(src, ctx)
		l.applyPathServers(&op, pi, declPtr)
		grp := groups.group(key, func() ir.OperationGroup { return ir.OperationGroup{Name: name, Docs: docs} })
		grp.Operations = append(grp.Operations, op)
		grp.Operations = append(grp.Operations, extra...)
	}
}

// lowerWebhooks lowers webhook path items into the dedicated "webhooks" group;
// each webhook operation carries IsWebhook on its HTTP binding.
func (l *lowerer) lowerWebhooks(groups *serviceGroups) {
	hooks := l.doc.GetWebhooks()
	if hooks == nil || hooks.Len() == 0 {
		return
	}
	for name, rp := range hooks.All() {
		hookPtr := ptr("webhooks", name)
		pi, declPtr := resolveRefAt[soa.PathItem](l, rp, hookPtr)
		if pi == nil {
			continue
		}
		for _, m := range httpMethods {
			src := m.get(pi)
			if src == nil {
				continue
			}
			ptrs := opPointers{mount: hookPtr + ptr(m.name), decl: declPtr + ptr(m.name)}
			ctx := opContext{
				method:        m.name,
				uriTemplate:   name,
				isWebhook:     true,
				withCallbacks: true,
				ptrs:          ptrs,
				params:        mergeParameters(pi.GetParameters(), src.GetParameters(), declPtr, ptrs.decl),
			}
			op, extra := l.lowerOperation(src, ctx)
			grp := groups.group("webhook", func() ir.OperationGroup {
				return ir.OperationGroup{Name: ir.Naming{Source: "webhooks"}}
			})
			grp.Operations = append(grp.Operations, op)
			grp.Operations = append(grp.Operations, extra...)
		}
	}
}

// groupFor resolves the group an operation belongs to under the active strategy.
// GroupByPathPrefix is a heuristic, so it stamps the inferred marker; grouping by
// declared tags is a declared fact and leaves it empty.
func (l *lowerer) groupFor(src *soa.Operation, path string) (key string, name ir.Naming, docs ir.Docs, inferred string) {
	if l.opts.Grouping == GroupByPathPrefix {
		seg := firstPathSegment(path)
		return "seg:" + seg, ir.Naming{Source: seg, Canonical: canonicalWords(seg)}, ir.Docs{}, "group-path-prefix"
	}
	tags := src.GetTags()
	if len(tags) == 0 {
		return "default", ir.Naming{Hint: "default"}, ir.Docs{}, ""
	}
	first := tags[0]
	return "tag:" + first, ir.Naming{Source: first, Canonical: canonicalWords(first)}, l.tagDocs(first), ""
}

// tagDocs returns the declared docs for a tag name, or empty when undeclared.
func (l *lowerer) tagDocs(name string) ir.Docs {
	for _, t := range l.doc.GetTags() {
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
func (l *lowerer) lowerOperation(src *soa.Operation, ctx opContext) (ir.Operation, []ir.Operation) {
	mount, decl := ctx.ptrs.mount, ctx.ptrs.decl
	op := ir.Operation{
		ID:   opID(mount),
		Name: operationName(src, ctx.method, ctx.uriTemplate),
		Tags: src.GetTags(),
		Auth: l.lowerSecurityRequirements(src.Security),
		// The ID is the mount — two mounts of one $ref'd path item are two
		// operations — but provenance is where the operation is written, which
		// for those two is one component. A mount pointer under a $ref'd path
		// item addresses no node at all, and ir-design §13 defines Pointer as a
		// pointer into the source, so it has to name the declaration.
		Provenance: ir.Provenance{Source: l.srcIndex, Pointer: decl, Inferred: ctx.inferred},
	}
	fillOperationDocs(&op.Docs, src)
	if src.GetDeprecated() {
		op.Deprecation = &ir.Deprecation{}
	}
	params, bindings := l.lowerParameters(ctx.params)
	op.Params = params
	op.Responses, op.Errors = l.lowerResponses(src, decl)
	hb := ir.HTTPBinding{
		Method:        strings.ToUpper(ctx.method),
		URITemplate:   ctx.uriTemplate,
		IsWebhook:     ctx.isWebhook,
		ParamBindings: bindings,
	}
	l.lowerRequestBody(&op, &hb, src, decl)
	var extra []ir.Operation
	if ctx.withCallbacks {
		hb.Callbacks, extra = l.lowerCallbacks(src, ctx.ptrs, ctx.inferred)
	}
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{hb}}
	if ext := l.operationExtensions(src, decl); len(ext) > 0 {
		op.Preserved = ext
	}
	l.checkOperationIDUnique(op, mount)
	return op, extra
}

// checkOperationIDUnique reports an operationId claimed by more than one
// operation. OpenAPI requires it to be unique across the whole API, and the
// resolver cannot see this shape: one path item declaring an operationId and
// mounted at two paths is written once but describes two operations. Names are
// presentation, so the IR still records what the document said (invariant 4) —
// but an emitter renders both under one identifier, so the collision has to be
// reported rather than discovered downstream.
func (l *lowerer) checkOperationIDUnique(op ir.Operation, mount string) {
	if op.Name.Source == "" {
		return // no operationId: emitters synthesize from the method and path
	}
	if first, seen := l.operationIDs[op.Name.Source]; seen {
		l.diag(ir.SeverityWarning, codeDuplicateOperationID, mount,
			"operationId %q is already used by the operation at %s; "+
				"OpenAPI requires it to be unique across the API", op.Name.Source, first)
		return
	}
	if l.operationIDs == nil {
		l.operationIDs = make(map[string]string)
	}
	l.operationIDs[op.Name.Source] = mount
}

// operationName builds an operation's neutral naming: the operationId when
// present (source + canonical words), else an empty source with a method+path
// hint so emitters can synthesize a name.
func operationName(src *soa.Operation, method, uriTemplate string) ir.Naming {
	if id := src.GetOperationID(); id != "" {
		return ir.Naming{Source: id, Canonical: canonicalWords(id)}
	}
	return ir.Naming{Hint: canonicalWords(method + " " + uriTemplate)}
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

// operationExtensions lowers an operation's x-* extensions into namespaced
// Preserved entries.
func (l *lowerer) operationExtensions(src *soa.Operation, declPtr string) ir.Preserved {
	return l.extensions(src.GetExtensions(), declPtr)
}

// applyPathServers preserves path-item-level servers verbatim under Preserved
// on the operation: sub-document server scoping is out of the §10 server model,
// so it is kept raw with an info diagnostic rather than dropped.
func (l *lowerer) applyPathServers(op *ir.Operation, pi *soa.PathItem, declPtr string) {
	if len(pi.GetServers()) == 0 {
		return
	}
	raw := nodeToRaw(rawChildNode(pi.GetRootNode(), "servers"))
	if raw == nil {
		return
	}
	l.preserve(&op.Preserved, "openapi:servers", raw, ir.ReasonNoIRHome, declPtr+ptr("servers"))
	l.diags = append(l.diags, diagf(ir.SeverityInfo, codeDegradedConstruct, op.Provenance,
		"path-item servers kept under Preserved; sub-document server scoping is out of model"))
}

// lowerResponses splits an operation's responses into success responses
// (status < 400) and error cases (>= 400 and the default), each in source order
// with the default last (ir-design §7.2). opDeclPtr is the operation's own
// declaration pointer, so a $ref'd response interns its content once at its
// component pointer rather than once per mount site (issue #107).
func (l *lowerer) lowerResponses(src *soa.Operation, opDeclPtr string) ([]ir.Response, []ir.ErrorCase) {
	// GetResponses never returns nil (it addresses an always-present map), so the
	// loop simply yields nothing when no responses are declared.
	resps := src.GetResponses()
	var responses []ir.Response
	var errs []ir.ErrorCase
	for code, rr := range resps.All() {
		r, rptr := resolveRefAt[soa.Response](l, rr, opDeclPtr+ptr("responses", code))
		if r == nil {
			continue
		}
		rng := statusRange(code)
		if isErrorRange(rng) {
			errs = append(errs, l.lowerErrorCase(r, rng, rptr))
		} else {
			responses = append(responses, l.lowerResponse(r, rng, rptr))
		}
	}
	def, dptr := resolveRefAt[soa.Response](l, resps.GetDefault(), opDeclPtr+ptr("responses", "default"))
	if def != nil {
		errs = append(errs, l.lowerErrorCase(def, ir.StatusRange{}, dptr))
	}
	return responses, errs
}

// lowerResponse lowers one success response: its status condition, payload (all
// media types), headers, docs, and any raw links preserved for later promotion.
func (l *lowerer) lowerResponse(r *soa.Response, rng ir.StatusRange, rptr string) ir.Response {
	resp := ir.Response{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{rng}},
		Payload:    l.lowerPayload(r.GetContent(), rptr, "response"),
		Headers:    l.lowerHeaders(r.GetHeaders(), rptr),
	}
	resp.Docs.Description = r.GetDescription()
	l.preserve(&resp.Preserved, "openapi:links", nodeToRaw(rawChildNode(r.GetRootNode(), "links")),
		ir.ReasonNoIRHome, rptr+ptr("links"))
	return resp
}

// lowerErrorCase lowers one error response into an ErrorCase, classifying its
// fault from the status range and lowering its error-model content.
func (l *lowerer) lowerErrorCase(r *soa.Response, rng ir.StatusRange, rptr string) ir.ErrorCase {
	ec := ir.ErrorCase{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{rng}},
		Fault:      faultFor(rng),
	}
	ec.Docs.Description = r.GetDescription()
	l.fillErrorType(&ec, r, rptr)
	l.preserveErrorHeaders(&ec, r, rptr)
	return ec
}

// preserveErrorHeaders keeps an error response's headers from being dropped:
// ir.ErrorCase has no Headers field (ir-design §7.2), so when the response
// declares headers they are kept verbatim under Preserved with one info
// diagnostic, mirroring the success path's structural header lowering.
func (l *lowerer) preserveErrorHeaders(ec *ir.ErrorCase, r *soa.Response, rptr string) {
	headers := r.GetHeaders()
	if headers == nil || headers.Len() == 0 {
		return
	}
	raw := nodeToRaw(rawChildNode(r.GetRootNode(), "headers"))
	if raw == nil {
		return
	}
	l.preserve(&ec.Preserved, "openapi:headers", raw, ir.ReasonNoIRHome, rptr+ptr("headers"))
	l.diag(ir.SeverityInfo, codeDegradedConstruct, rptr,
		"error response headers have no ErrorCase home; kept verbatim under Preserved")
}

// fillErrorType lowers every content entry's schema into the type registry
// (nothing dropped) and points ErrorCase.Type at the first. When more than one
// media type exists, the full content map is preserved raw with an info
// diagnostic, since ErrorCase.Type holds a single model reference (ir-design
// §7.2 clarification).
func (l *lowerer) fillErrorType(ec *ir.ErrorCase, r *soa.Response, rptr string) {
	content := r.GetContent()
	if content == nil || content.Len() == 0 {
		return
	}
	first := true
	for mt, media := range content.All() {
		ref := l.schemaRef(media.GetSchema(), rptr+ptr("content", mt, "schema"), "error")
		if first {
			ec.Type = ref
			first = false
		}
	}
	if content.Len() > 1 {
		l.preserve(&ec.Preserved, "openapi:content", nodeToRaw(rawChildNode(r.GetRootNode(), "content")),
			ir.ReasonNoIRHome, rptr+ptr("content"))
		l.diag(ir.SeverityInfo, codeDegradedConstruct, rptr,
			"error response has multiple media types; full content map kept under Preserved")
	}
}

// lowerCallbacks lowers each callback expression's path-item operations as
// Operations registered in the parent's group, and binds them to the parent via
// HTTPBinding.Callbacks keyed by the runtime expression (ir-design §8.1).
// parent.mount roots callback operation identity, so two parents sharing one
// $ref'd callback keep distinct callback operations; parent.decl is the base a
// $ref'd callback or path item resolves against (issue #107).
func (l *lowerer) lowerCallbacks(src *soa.Operation, parent opPointers, inferred string) ([]ir.Callback, []ir.Operation) {
	cbMap := src.GetCallbacks()
	if cbMap == nil || cbMap.Len() == 0 {
		return nil, nil
	}
	var callbacks []ir.Callback
	var ops []ir.Operation
	for cbName, rcb := range cbMap.All() {
		cb, cbDecl := resolveRefAt[soa.Callback](l, rcb, parent.decl+ptr("callbacks", cbName))
		if cb == nil {
			continue
		}
		for expr, rp := range cb.All() {
			exprStr := string(expr)
			pi, piDecl := resolveRefAt[soa.PathItem](l, rp, cbDecl+ptr(exprStr))
			if pi == nil {
				continue
			}
			cbPtrs := opPointers{mount: parent.mount + ptr("callbacks", cbName, exprStr), decl: piDecl}
			ids, cbOps := l.lowerCallbackOps(pi, cbPtrs, exprStr, inferred)
			callbacks = append(callbacks, ir.Callback{Expression: exprStr, Operations: ids})
			ops = append(ops, cbOps...)
		}
	}
	return callbacks, ops
}

// lowerCallbackOps lowers a callback expression's path-item operations. Callback
// operations do not recurse into their own callbacks (withCallbacks stays
// false), which bounds the lowering to the declared out-of-band set. cb pairs
// the expression's identity base (distinct per parent operation) with its
// declaration base (shared when the callback or its path item is $ref'd;
// issue #107).
func (l *lowerer) lowerCallbackOps(pi *soa.PathItem, cb opPointers, expr, inferred string) ([]ir.OpID, []ir.Operation) {
	var ids []ir.OpID
	var ops []ir.Operation
	for _, m := range httpMethods {
		src := m.get(pi)
		if src == nil {
			continue
		}
		ptrs := opPointers{mount: cb.mount + ptr(m.name), decl: cb.decl + ptr(m.name)}
		ctx := opContext{
			method:      m.name,
			uriTemplate: expr,
			inferred:    inferred,
			ptrs:        ptrs,
			params:      mergeParameters(pi.GetParameters(), src.GetParameters(), cb.decl, ptrs.decl),
		}
		op, _ := l.lowerOperation(src, ctx)
		ids = append(ids, op.ID)
		ops = append(ops, op)
	}
	return ids, ops
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
		dst = append(dst, sourcedParam{ref: p, pointer: base + ptr("parameters", strconv.Itoa(i))})
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

// rawChildNode returns the raw YAML value node of a mapping child keyed by the
// on-wire name, unwrapping a document node first; nil when absent. It reads exact
// literals the high-level model does not preserve (links, servers, content maps).
func rawChildNode(root *yaml.Node, key string) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	return nil
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
}](l *lowerer, ref R, usePtr string) (*T, string) {
	obj := resolveRef[T, S, R](ref)
	if obj == nil {
		return nil, usePtr
	}
	// cand advances one hop at a time but is adopted only once the chain ends
	// at a declaration written here, which is what keeps a chain that exits the
	// document from returning the last alias it passed through.
	pointer, cand := usePtr, usePtr
	for hop := 0; hop < maxRefChain; hop++ {
		// GetReferenceResolutionInfo is nil once the chain reaches a
		// non-reference entry — the terminator, and why an inline entry
		// exits on the first pass (couples this to that library contract).
		info := ref.GetReferenceResolutionInfo()
		if info == nil {
			pointer = cand
			break
		}
		target, ok := l.internalPointer(ref.GetReference().String())
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
