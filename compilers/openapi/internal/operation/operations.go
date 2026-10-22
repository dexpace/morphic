package operation

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"maps"
	"slices"
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

// lowerTagDefs registers the document's declared tag metadata into TagDefs; tag
// membership itself stays as []string on each tagged operation.
//
// Parent and Kind are recorded as declared (OpenAPI 3.2's tag hierarchy and tag
// role, both dropped until GitHub #613). Reading a role out of Kind is grouping
// policy rather than a property of the document, so it is not interpreted here.
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
		defs = append(defs, ir.TagDef{
			Name:   t.GetName(),
			Docs:   tagDocsFrom(t),
			Parent: t.GetParent(),
			Kind:   t.GetKind(),
		})
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

// groupFor resolves the groups an operation is filed under by the active
// strategy, outermost first, by identity: the ID is both the group's key here
// and its ID in the IR, so two operations share a group exactly when they share
// an ID. The untagged group's hint stays "default" beside a tag of that name:
// the IDs differ, and the emitter resolves name collisions by ID
// (emitter-design §4.12). By tags, the group is the first navigational tag's,
// nested under the tags its declared parents name (GitHub #613).
func (g *serviceGroups) groupFor(c lowering.Ctx, src *soa.Operation, path string) []groupLevel {
	if c.Grouping == lowering.GroupByPathPrefix {
		seg := firstPathSegment(path)
		return []groupLevel{{id: ids.PathPrefixGroup(seg), name: compile.NamingFor(seg)}}
	}
	for _, tag := range src.GetTags() {
		if g.tags.navigational(tag) {
			return g.tags.levels(tag)
		}
	}
	return []groupLevel{{id: ids.DefaultGroup(), name: compile.NamingHint("default")}}
}

// groupLevel is one group an operation is filed under: its ID, and the naming
// and docs it is created with.
type groupLevel struct {
	id   ir.GroupID
	name ir.Naming
	docs ir.Docs
}

// navigationalKind is the tag kind OpenAPI 3.2's registry defines for a tag
// that groups operations into sections.
const navigationalKind = "nav"

// tagIndex is a document's declared tags by name, the first declaration of a
// name winning, so which one supplies a tag's kind, parent and docs does not
// depend on the order operations are lowered in.
type tagIndex map[string]*soa.Tag

// indexTags indexes tags by name.
func indexTags(tags []*soa.Tag) tagIndex {
	out := make(tagIndex, len(tags))
	for _, t := range tags {
		if _, seen := out[t.GetName()]; t != nil && !seen {
			out[t.GetName()] = t
		}
	}
	return out
}

// navigational reports whether tag groups operations into sections: one the
// document does not declare, or one declaring no kind or the navigational one.
// Any other kind is skipped, registered or not, as reading a section out of an
// unregistered string would be an inference (invariant 6). Nothing is reported:
// TagDef keeps the kind and Operation.Tags every membership.
func (x tagIndex) navigational(tag string) bool {
	kind := x[tag].GetKind()
	return kind == "" || kind == navigationalKind
}

// parent returns the tag tag nests under: the parent it declares, when that is
// itself a declared navigational tag, and "" otherwise.
func (x tagIndex) parent(tag string) string {
	parent := x[tag].GetParent()
	if parent == "" || x[parent] == nil || !x.navigational(parent) {
		return ""
	}
	return parent
}

// chain returns the tags tag nests under, outermost first and ending with tag,
// following each declared parent. Where the walk meets a tag it already passed,
// the parents form a cycle: the chain starts at the first tag of the cycle it
// reached, so the tags on a cycle nest under none of one another while every
// tag outside it keeps its parent. Any suffix of a walk is the walk from that
// tag, so every tag's chain agrees with its descendants'. A walk passes each
// declared tag at most once, which bounds it.
func (x tagIndex) chain(tag string) []string {
	walk := []string{tag}
	at := map[string]int{tag: 0}
	for range len(x) {
		parent := x.parent(walk[len(walk)-1])
		if parent == "" {
			break
		}
		if i, seen := at[parent]; seen {
			walk = walk[:i+1]
			break
		}
		at[parent] = len(walk)
		walk = append(walk, parent)
	}
	slices.Reverse(walk)
	return walk
}

// levels returns the groups an operation filed under tag nests in, outermost
// first.
func (x tagIndex) levels(tag string) []groupLevel {
	chain := x.chain(tag)
	out := make([]groupLevel, 0, len(chain))
	for _, t := range chain {
		out = append(out, groupLevel{id: ids.TagGroup(t), name: compile.NamingFor(t), docs: tagDocsFrom(x[t])})
	}
	return out
}

// groupingInferred is the inferred marker groupFor's strategy stamps on a path
// operation: lowering.GroupByPathPrefix is a heuristic, so it stamps one, and
// grouping by declared tags is a declared fact, so it stamps none.
func groupingInferred(c lowering.Ctx) string {
	if c.Grouping == lowering.GroupByPathPrefix {
		return "group-path-prefix"
	}
	return ""
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

// lowerResponses splits an operation's responses into success responses
// (status < 400) and error cases (>= 400 and the default), each in source order
// with the default last (ir-design §7.2). opDeclPtr is the operation's own
// declaration pointer, so a $ref'd response interns its content once at its
// component pointer rather than once per mount site (issue #107).
func lowerResponses(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, src *soa.Operation, opDeclPtr jsontext.Pointer) ([]ir.Response, []ir.ErrorCase, []ir.Diagnostic) {
	// GetResponses never returns nil (it addresses an always-present map), so the
	// loop simply yields nothing when no responses are declared.
	resps := src.GetResponses()
	var responses []ir.Response
	var errs []ir.ErrorCase
	var diags []ir.Diagnostic
	// Every key read, by the range it resolved to: the map is keyed by the
	// spelling, so two spellings of one range are two entries here and one
	// condition in the IR. Collisions are reported after the loop, once per
	// range, so the report does not depend on which spelling came first.
	byRange := map[ir.StatusRange][]string{}
	for code, rr := range resps.All() {
		// The key's own entry, kept apart from rptr: a $ref'd response resolves
		// to its component, and a fault in the operation's map is sited at the
		// map. Sited at the component it named no operation, and the second
		// operation to make the same mistake lost its warning to the first,
		// since the diagnostic stream dedups on full identity.
		entry := opDeclPtr + ids.Ptr("responses", code)
		r, rc, rptr := lowering.Declared[soa.Response](c, rr, entry)
		if r == nil {
			continue
		}
		rng, named := statusRange(code)
		if !named {
			diags = append(diags, invalidStatusKeyDiag(c, code, entry))
		} else {
			byRange[rng] = append(byRange[rng], code)
		}
		// An unreadable key always takes the else branch, because statusRange pairs
		// a false with the zero range and that is no error range. It has to: an
		// ErrorCase would carry a fault classified from a range nothing derived,
		// and its Conditions would assert a status the key never named — where
		// statusConditions lets the success side record no status at all.
		// TestStatusRange_NamesNoStatus is what pins the pairing.
		if isErrorRange(rng) {
			ec, ecDiags := lowerErrorCase(rc, ts, anchors, r, code, rng, rptr, rr)
			diags = append(diags, ecDiags...)
			errs = append(errs, ec)
		} else {
			resp, respDiags := lowerResponse(rc, ts, anchors, r, code, statusConditions(rng, named), rptr, rr)
			diags = append(diags, respDiags...)
			responses = append(responses, resp)
		}
	}
	def, dc, dptr := lowering.Declared[soa.Response](c, resps.GetDefault(), opDeclPtr+ids.Ptr("responses", defaultResponseKey))
	if def != nil {
		ec, ecDiags := lowerErrorCase(dc, ts, anchors, def, defaultResponseKey, ir.StatusRange{}, dptr, resps.GetDefault())
		diags = append(diags, ecDiags...)
		errs = append(errs, ec)
	}
	return responses, errs, append(diags, duplicateStatusKeyDiags(c, byRange, opDeclPtr+ids.Ptr("responses"))...)
}

// duplicateStatusKeyDiags reports each status range more than one responses-map
// key resolved to, at the map itself. Ranges are visited in order and the keys
// of each sorted, so the same map reads the same however it was written.
func duplicateStatusKeyDiags(c lowering.Ctx, byRange map[ir.StatusRange][]string, mapPtr jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ranges := slices.SortedFunc(maps.Keys(byRange), func(a, b ir.StatusRange) int {
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To))
	})
	for _, rng := range ranges {
		keys := byRange[rng]
		if len(keys) < 2 {
			continue
		}
		slices.Sort(keys)
		diags = append(diags, duplicateStatusKeyDiag(c, keys, mapPtr))
	}
	return diags
}

// lowerResponse lowers one success response: its naming, status condition,
// payload (all media types), headers, docs, and any raw links preserved for
// later promotion. code is the responses-map key the response is declared under
// and conds is what that key resolved to, which is nothing at all when it named
// no status (see statusConditions). ref is the entry the document wrote there,
// which carries the summary and description a Reference Object may put beside
// its $ref.
func lowerResponse(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, r *soa.Response, code string, conds ir.ResponseConditions, rptr jsontext.Pointer, ref *soa.ReferencedResponse) (ir.Response, []ir.Diagnostic) {
	parts, diags := lowerResponseParts(c, ts, anchors, r, code, rptr, ref)
	resp := ir.Response{
		Name:       parts.name,
		Conditions: conds,
		Payload:    parts.payload,
		Headers:    parts.headers,
		Docs:       parts.docs,
		Unmodeled:  parts.unmodeled,
	}
	return resp, diags
}

// responseParts is everything a Response Object lowers to before its status
// class decides which node carries it: an ir.Response and an ir.ErrorCase are
// two lowerings of one source object, and this is the half they share.
type responseParts struct {
	name      ir.Naming
	payload   *ir.Payload
	headers   []ir.Property
	docs      ir.Docs
	unmodeled ir.Unmodeled
}

// lowerResponseParts lowers what a Response Object declares regardless of the
// status it answers to: naming, payload, headers, docs, and any raw links or
// extensions preserved for later promotion.
//
// It is one function for both status classes so nothing can depend on which
// class reached a declaration first: a response mounted as a success and an
// error interns its body once, so two naming fallbacks would rename it when
// paths are reordered. The use-site docs fold over ref, the entry written here,
// must agree across classes for the same reason (GitHub #610).
func lowerResponseParts(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, r *soa.Response, code string, rptr jsontext.Pointer, ref *soa.ReferencedResponse) (responseParts, []ir.Diagnostic) {
	headers, diags := lowerHeaders(c, ts, anchors, r.GetHeaders(), rptr)
	payload, payloadDiags := lowerPayload(c, ts, anchors, r.GetContent(), rptr, ids.DeclarationHint(rptr, "response"))
	diags = append(diags, payloadDiags...)
	parts := responseParts{name: responseName(code), payload: payload, headers: headers}
	summary, taken, summaryDiags := responseSummary(c, &parts.unmodeled, r, rptr)
	diags = append(diags, summaryDiags...)
	parts.docs = resolve.UseDocs[soa.Response](ref, ir.Docs{Summary: summary, Description: r.GetDescription()})
	return parts, append(diags, preserveResponseExtras(c, &parts.unmodeled, r, rptr, taken...)...)
}

// responseSummary reads the summary OpenAPI 3.2 gives a Response Object, which
// the parser's model names no member for (GitHub #615), and returns the keys it
// read for the census to leave alone. It reads at every version, as load
// reports the key below 3.2 (diag.LaterVersionConstruct).
func responseSummary(c lowering.Ctx, p *ir.Unmodeled, r *soa.Response, rptr jsontext.Pointer) (string, []string, []ir.Diagnostic) {
	summary, read, diags := annotation.TakeString(p, r.GetRootNode(), "summary", nil, c.ProvenanceAt, rptr, "")
	if !read {
		return "", nil, diags
	}
	return summary, []string{"summary"}, diags
}

// preserveResponseExtras keeps what a Response Object declares that has no home
// on its node: its links map, its x-* extensions, and the undefined keys other
// than taken (responseSummary's). One helper serves ir.Response and
// ir.ErrorCase, so neither keeps a construct the other drops (GitHub #275).
//
// The links entry carries ReasonNoIRHome and no diagnostic, since nothing is
// degraded and the IR could add a links field. A Link Object inside it gets no
// entry or census: this compiler lowers none, and a keyed entry would duplicate
// the node above. A components/links entry it names is kept on the document
// (GitHub #616).
func preserveResponseExtras(c lowering.Ctx, p *ir.Unmodeled, r *soa.Response, rptr jsontext.Pointer, taken ...string) []ir.Diagnostic {
	_, diags := schema.PreserveNode(c, p, "openapi:links",
		annotation.RawChildNode(r.GetRootNode(), "links"), ir.ReasonNoIRHome, rptr+ids.Ptr("links"))
	ext, extDiags := schema.ExtensionsOf(c, r.GetExtensions(), rptr)
	*p = annotation.MergeUnmodeled(*p, ext)
	diags = append(diags, extDiags...)
	return append(diags, annotation.UnknownKeysIn(p, r, c.ProvenanceAt, rptr, taken...)...)
}

// responseName builds a success response's neutral naming. OpenAPI names no
// response, so the hint ir-design §7.2 allows is the status key: "200", "2_xx",
// or the shared mint for a key with no word in it (GitHub #259).
//
// It is the key as declared, not the range it parses to, so "2XX" is not named
// by a spelling its document never used, nor a key naming no status by the
// catch-all range (GitHub #262). Source stays empty: a components/responses key
// names a reusable definition, not this mount of it.
//
// The error side calls this too, since ErrorCase.Name is Response.Name (GitHub
// #422).
func responseName(code string) ir.Naming {
	return compile.NamingHint(code)
}

// lowerErrorCase lowers one error response into an ErrorCase: naming, status
// condition, payload, headers, docs and fault classification, plus any raw
// links preserved for later promotion.
//
// Everything but the condition and the fault is lowerResponseParts' work, the
// helper the success side calls, so a shared declaration lowers one way (GitHub
// #422). code is the responses-map key, the only record of how the source
// spelled a status its range cannot state ("4XX" or "default"); ref is the
// entry written here.
func lowerErrorCase(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, r *soa.Response, code string, rng ir.StatusRange, rptr jsontext.Pointer, ref *soa.ReferencedResponse) (ir.ErrorCase, []ir.Diagnostic) {
	parts, diags := lowerResponseParts(c, ts, anchors, r, code, rptr, ref)
	ec := ir.ErrorCase{
		Name:       parts.name,
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{rng}},
		Payload:    parts.payload,
		Headers:    parts.headers,
		Fault:      faultFor(rng),
		Docs:       parts.docs,
		Unmodeled:  parts.unmodeled,
	}
	return ec, diags
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

// statusRange maps an OpenAPI response key to an inclusive status range: "200"
// → {200,200}, "4XX" → {400,499}, "default" → {0,0} (ir-design §7.2). ok
// reports whether the key named a status, and a false always comes paired with
// the zero range; callers route on that.
//
// Past "default", a key is three characters over one leading digit: OpenAPI
// defines exactly the wildcards 1XX through 5XX and HTTP codes 100 through 599,
// and every character is checked, so "1XY" and "9XX" are refused (GitHub #262).
//
// "default" is answered although no caller asks it today (soa.Responses carries
// it on its own Default field), so the function stays right for a caller that
// does.
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

// statusConditions records the status a response applies to, or no status at
// all when its key named none.
//
// Not the catch-all: {0,0} is what "default" lowers to, so folding an
// unreadable key into it makes a typo indistinguishable from a declared
// default, and a document with both gets two responses claiming the catch-all.
// Nothing is lost by declining to guess: the key survives as the response's
// name hint, and the warning beside it carries its pointer.
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
func invalidStatusKeyDiag(c lowering.Ctx, code string, entry jsontext.Pointer) ir.Diagnostic {
	return c.DiagAt(ir.SeverityWarning, diag.InvalidStatusKey, entry,
		"response key %q is no status code, no 1XX-5XX range, and not %s; "+
			"the response is kept with no status condition", code, defaultResponseKey)
}

// duplicateStatusKeyDiag reports the responses-map keys, sorted, that resolve
// to one status range.
//
// A warning, and every response is kept: no key is wrong on its own, and
// dropping one would choose a winner on declaration order. The caller is told
// the entries answer to one status and cannot be told apart by name or
// condition. It is sited at the map and names all the keys, so the report reads
// the same from either spelling of the map.
func duplicateStatusKeyDiag(c lowering.Ctx, keys []string, mapPtr jsontext.Pointer) ir.Diagnostic {
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = strconv.Quote(k)
	}
	return c.DiagAt(ir.SeverityWarning, diag.DuplicateStatusKey, mapPtr,
		"response keys %s name one status range; all are kept, "+
			"and they reach the IR with the same name and condition", strings.Join(quoted, ", "))
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

// serviceGroups accumulates operation groups by ID while preserving first-seen
// insertion order, so a group's operations gather across paths without
// reordering the groups themselves. A group nested under another (an OpenAPI
// 3.2 tag parent) is recorded in parentOf and attached to it by finalize.
type serviceGroups struct {
	tags     tagIndex
	order    []ir.GroupID
	byID     map[ir.GroupID]*ir.OperationGroup
	parentOf map[ir.GroupID]ir.GroupID
}

// newServiceGroups returns an empty group accumulator over the declared tags.
func newServiceGroups(tags []*soa.Tag) *serviceGroups {
	return &serviceGroups{
		tags:     indexTags(tags),
		byID:     make(map[ir.GroupID]*ir.OperationGroup),
		parentOf: make(map[ir.GroupID]ir.GroupID),
	}
}

// group returns the group with the given ID, creating it via mk on first sight
// and recording its insertion order. It stamps the ID itself, so a group's ID is
// its key by construction rather than by every caller repeating it.
func (g *serviceGroups) group(id ir.GroupID, mk func() ir.OperationGroup) *ir.OperationGroup {
	if existing, ok := g.byID[id]; ok {
		return existing
	}
	created := mk()
	created.ID = id
	g.byID[id] = &created
	g.order = append(g.order, id)
	return &created
}

// place returns the innermost group of levels, creating each level on first
// sight nested under the one before it. A level's parent is fixed when it is
// created, which is sound because every operation reaching a group reaches it
// through the same outer levels (tagIndex.chain), and it puts each parent
// before its children in the insertion order.
func (g *serviceGroups) place(levels []groupLevel) *ir.OperationGroup {
	var grp *ir.OperationGroup
	var parent ir.GroupID
	for _, l := range levels {
		if _, seen := g.byID[l.id]; !seen && parent != "" {
			g.parentOf[l.id] = parent
		}
		grp = g.group(l.id, func() ir.OperationGroup { return ir.OperationGroup{Name: l.name, Docs: l.docs} })
		parent = l.id
	}
	return grp
}

// finalize returns the accumulated groups in insertion order, each carrying the
// groups nested under it in insertion order, and only those with no parent at
// the top level. It builds from the end of the order, where a parent's children
// are already built, so it needs no recursion.
func (g *serviceGroups) finalize() []ir.OperationGroup {
	children := make(map[ir.GroupID][]ir.GroupID, len(g.parentOf))
	for _, id := range g.order {
		if parent, nested := g.parentOf[id]; nested {
			children[parent] = append(children[parent], id)
		}
	}
	built := make(map[ir.GroupID]ir.OperationGroup, len(g.order))
	for _, id := range slices.Backward(g.order) {
		grp := *g.byID[id]
		for _, child := range children[id] {
			grp.Groups = append(grp.Groups, built[child])
		}
		built[id] = grp
	}
	out := make([]ir.OperationGroup, 0, len(g.order))
	for _, id := range g.order {
		if _, nested := g.parentOf[id]; !nested {
			out = append(out, built[id])
		}
	}
	return out
}
