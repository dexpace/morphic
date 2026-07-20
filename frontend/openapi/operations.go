package openapi

import (
	"strconv"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
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
		pi := resolvePathItem(rp)
		if pi == nil {
			continue
		}
		l.lowerPathItem(groups, path, pi)
	}
}

// lowerPathItem lowers each method operation on one path into its group,
// carrying along any callback operations registered under the same group.
func (l *lowerer) lowerPathItem(groups *serviceGroups, path string, pi *soa.PathItem) {
	for _, m := range httpMethods {
		src := m.get(pi)
		if src == nil {
			continue
		}
		key, name, docs, inferred := l.groupFor(src, path)
		ctx := opContext{
			method:        m.name,
			uriTemplate:   path,
			withCallbacks: true,
			inferred:      inferred,
			params:        mergeParameters(pi.GetParameters(), src.GetParameters()),
		}
		op, extra := l.lowerOperation(src, ctx, ptr("paths", path, m.name))
		l.applyPathServers(&op, pi)
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
		pi := resolvePathItem(rp)
		if pi == nil {
			continue
		}
		for _, m := range httpMethods {
			src := m.get(pi)
			if src == nil {
				continue
			}
			ctx := opContext{
				method:        m.name,
				uriTemplate:   name,
				isWebhook:     true,
				withCallbacks: true,
				params:        mergeParameters(pi.GetParameters(), src.GetParameters()),
			}
			op, extra := l.lowerOperation(src, ctx, ptr("webhooks", name, m.name))
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

// opContext carries the per-operation lowering inputs that do not come from the
// source Operation itself: its HTTP binding shape, grouping provenance, and the
// path-item-merged parameter list.
type opContext struct {
	method        string
	uriTemplate   string
	isWebhook     bool
	withCallbacks bool
	inferred      string
	params        []*soa.ReferencedParameter
}

// lowerOperation lowers one source operation at pointer into the neutral core
// plus its HTTP binding. It returns the operation and any callback operations
// that must be registered alongside it in the same group (ir-design §7.2, §8.1).
func (l *lowerer) lowerOperation(src *soa.Operation, ctx opContext, pointer string) (ir.Operation, []ir.Operation) {
	op := ir.Operation{
		ID:         opID(pointer),
		Name:       operationName(src, ctx.method, ctx.uriTemplate),
		Tags:       src.GetTags(),
		Auth:       l.lowerSecurityRequirements(src.Security),
		Provenance: ir.Provenance{Source: l.srcIndex, Pointer: pointer, Inferred: ctx.inferred},
	}
	fillOperationDocs(&op.Docs, src)
	if src.GetDeprecated() {
		op.Deprecation = &ir.Deprecation{}
	}
	params, bindings := l.lowerParameters(ctx.params, pointer)
	op.Params = params
	op.Responses, op.Errors = l.lowerResponses(src, pointer)
	hb := ir.HTTPBinding{
		Method:        strings.ToUpper(ctx.method),
		URITemplate:   ctx.uriTemplate,
		IsWebhook:     ctx.isWebhook,
		ParamBindings: bindings,
	}
	l.lowerRequestBody(&op, &hb, src, pointer)
	var extra []ir.Operation
	if ctx.withCallbacks {
		hb.Callbacks, extra = l.lowerCallbacks(src, pointer, ctx.inferred)
	}
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{hb}}
	if ext := l.operationExtensions(src); len(ext) > 0 {
		op.Extensions = ext
	}
	return op, extra
}

// operationName builds an operation's neutral naming: the operationId when
// present (source + canonical words), else an empty source with a method+path
// hint so backends can synthesize a name.
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
// Extensions.
func (l *lowerer) operationExtensions(src *soa.Operation) ir.Extensions {
	ext, diags := extensionsFrom(src.GetExtensions())
	l.diags = append(l.diags, diags...)
	return ext
}

// applyPathServers preserves path-item-level servers verbatim under Extensions
// on the operation: sub-document server scoping is out of the §10 server model,
// so it is kept raw with an info diagnostic rather than dropped.
func (l *lowerer) applyPathServers(op *ir.Operation, pi *soa.PathItem) {
	if len(pi.GetServers()) == 0 {
		return
	}
	raw := nodeToRaw(rawChildNode(pi.GetRootNode(), "servers"))
	if raw == nil {
		return
	}
	if op.Extensions == nil {
		op.Extensions = ir.Extensions{}
	}
	op.Extensions["openapi:servers"] = raw
	l.diags = append(l.diags, diagf(ir.SeverityInfo, codeDegradedConstruct, op.Provenance,
		"path-item servers preserved under extensions; sub-document server scoping is out of model"))
}

// lowerResponses splits an operation's responses into success responses
// (status < 400) and error cases (>= 400 and the default), each in source order
// with the default last (ir-design §7.2).
func (l *lowerer) lowerResponses(src *soa.Operation, opPointer string) ([]ir.Response, []ir.ErrorCase) {
	resps := src.GetResponses()
	if resps == nil {
		return nil, nil
	}
	var responses []ir.Response
	var errs []ir.ErrorCase
	for code, rr := range resps.All() {
		r := resolveResponse(rr)
		if r == nil {
			continue
		}
		rng := statusRange(code)
		rptr := opPointer + ptr("responses", code)
		if isErrorRange(rng) {
			errs = append(errs, l.lowerErrorCase(r, rng, rptr))
		} else {
			responses = append(responses, l.lowerResponse(r, rng, rptr))
		}
	}
	if def := resolveResponse(resps.GetDefault()); def != nil {
		errs = append(errs, l.lowerErrorCase(def, ir.StatusRange{}, opPointer+ptr("responses", "default")))
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
	if raw := nodeToRaw(rawChildNode(r.GetRootNode(), "links")); raw != nil {
		resp.Extensions = ir.Extensions{"openapi:links": raw}
	}
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
	return ec
}

// fillErrorType lowers every content entry's schema into the type registry
// (nothing dropped) and points ErrorCase.Type at the first. When more than one
// media type exists, the full content map is preserved raw with an info
// diagnostic, since ErrorCase.Type holds a single model reference (ir-design
// §7.2 clarification — surfaced in the PR description).
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
		if raw := nodeToRaw(rawChildNode(r.GetRootNode(), "content")); raw != nil {
			ec.Extensions = ir.Extensions{"openapi:content": raw}
		}
		l.diags = append(l.diags, diagf(ir.SeverityInfo, codeDegradedConstruct,
			ir.Provenance{Source: l.srcIndex, Pointer: rptr},
			"error response has multiple media types; full content map preserved under extensions"))
	}
}

// lowerCallbacks lowers each callback expression's path-item operations as
// Operations registered in the parent's group, and binds them to the parent via
// HTTPBinding.Callbacks keyed by the runtime expression (ir-design §8.1).
func (l *lowerer) lowerCallbacks(src *soa.Operation, opPointer, inferred string) ([]ir.Callback, []ir.Operation) {
	cbMap := src.GetCallbacks()
	if cbMap == nil || cbMap.Len() == 0 {
		return nil, nil
	}
	var callbacks []ir.Callback
	var ops []ir.Operation
	for cbName, rcb := range cbMap.All() {
		cb := resolveCallback(rcb)
		if cb == nil {
			continue
		}
		for expr, rp := range cb.All() {
			pi := resolvePathItem(rp)
			if pi == nil {
				continue
			}
			exprStr := string(expr)
			ids, cbOps := l.lowerCallbackOps(pi, opPointer+ptr("callbacks", cbName, exprStr), exprStr, inferred)
			callbacks = append(callbacks, ir.Callback{Expression: exprStr, Operations: ids})
			ops = append(ops, cbOps...)
		}
	}
	return callbacks, ops
}

// lowerCallbackOps lowers a callback expression's path-item operations. Callback
// operations do not recurse into their own callbacks (withCallbacks stays
// false), which bounds the lowering to the declared out-of-band set.
func (l *lowerer) lowerCallbackOps(pi *soa.PathItem, cbPtr, expr, inferred string) ([]ir.OpID, []ir.Operation) {
	var ids []ir.OpID
	var ops []ir.Operation
	for _, m := range httpMethods {
		src := m.get(pi)
		if src == nil {
			continue
		}
		ctx := opContext{
			method:      m.name,
			uriTemplate: expr,
			inferred:    inferred,
			params:      mergeParameters(pi.GetParameters(), src.GetParameters()),
		}
		op, _ := l.lowerOperation(src, ctx, cbPtr+ptr(m.name))
		ids = append(ids, op.ID)
		ops = append(ops, op)
	}
	return ids, ops
}

// mergeParameters merges path-item parameters with operation parameters using
// use-site precedence: an operation parameter with the same (name, in) overrides
// the path-item one; unshadowed path-item parameters follow in source order.
func mergeParameters(pathParams, opParams []*soa.ReferencedParameter) []*soa.ReferencedParameter {
	if len(pathParams) == 0 {
		return opParams
	}
	shadowed := make(map[string]bool, len(opParams))
	for _, p := range opParams {
		if key, ok := paramKey(p); ok {
			shadowed[key] = true
		}
	}
	merged := make([]*soa.ReferencedParameter, 0, len(opParams)+len(pathParams))
	merged = append(merged, opParams...)
	for _, p := range pathParams {
		if key, ok := paramKey(p); ok && shadowed[key] {
			continue
		}
		merged = append(merged, p)
	}
	return merged
}

// paramKey builds the (in, name) identity of a parameter for merge dedup.
func paramKey(rp *soa.ReferencedParameter) (string, bool) {
	p := resolveParameter(rp)
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
	for _, seg := range strings.Split(path, "/") {
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

// resolvePathItem returns the concrete PathItem of a reference-or-inline entry,
// preferring the inline object and falling back to the resolved target.
func resolvePathItem(rp *soa.ReferencedPathItem) *soa.PathItem {
	if rp == nil {
		return nil
	}
	if obj := rp.GetObject(); obj != nil {
		return obj
	}
	return rp.GetResolvedObject()
}

// resolveResponse returns the concrete Response of a reference-or-inline entry.
func resolveResponse(rr *soa.ReferencedResponse) *soa.Response {
	if rr == nil {
		return nil
	}
	if obj := rr.GetObject(); obj != nil {
		return obj
	}
	return rr.GetResolvedObject()
}

// resolveHeader returns the concrete Header of a reference-or-inline entry.
func resolveHeader(rh *soa.ReferencedHeader) *soa.Header {
	if rh == nil {
		return nil
	}
	if obj := rh.GetObject(); obj != nil {
		return obj
	}
	return rh.GetResolvedObject()
}

// resolveCallback returns the concrete Callback of a reference-or-inline entry.
func resolveCallback(rc *soa.ReferencedCallback) *soa.Callback {
	if rc == nil {
		return nil
	}
	if obj := rc.GetObject(); obj != nil {
		return obj
	}
	return rc.GetResolvedObject()
}

// resolveParameter returns the concrete Parameter of a reference-or-inline entry.
func resolveParameter(rp *soa.ReferencedParameter) *soa.Parameter {
	if rp == nil {
		return nil
	}
	if obj := rp.GetObject(); obj != nil {
		return obj
	}
	return rp.GetResolvedObject()
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
