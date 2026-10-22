package operation

import (
	"context"
	"encoding/json/jsontext"
	"slices"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// additionalOperationsField is the Path Item Object key OpenAPI 3.2 gives to
// operations whose method has no fixed field of its own, spelled once so the
// pointer such an operation is mounted under and the field it is read from
// cannot drift apart.
const additionalOperationsField = "additionalOperations"

// httpMethods is the set of HTTP method accessors that are fixed fields on a
// PathItem, iterated in this order so operation lowering is deterministic across
// runs. The name is the field name, which is the wire method in lowercase;
// pointers and IDs derive from it.
//
// It is closed, and the fields outside it are not: OpenAPI 3.2 added `query`
// here and put every other method under `additionalOperations`, which
// pathOperations reads beside this table.
//
// TestHTTPMethods_AgreesWithLibraryVocabulary holds this table to the
// library's own soa.IsStandardMethod, so a method the library learns before
// this table does reddens the build instead of being silently unmounted
// (GitHub #413).
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
// wire, the operation's pointer relative to its path item, its source node, and
// whether additionalOperations declares it rather than a fixed method field.
type pathOperation struct {
	method     string
	seg        jsontext.Pointer
	src        *soa.Operation
	additional bool
}

// pathOperations returns every operation a path item declares: the fixed method
// fields first, in httpMethods order, then any 3.2 additionalOperations in
// source order. One list serves the three walks that reach a path item, so an
// operation is lowered on all of them or none; reading the fixed fields alone
// silently dropped additionalOperations entries (GitHub #293).
//
// An additionalOperations key is the method verbatim, neither upper-cased nor
// neutralized, since OpenAPI reads method names case-sensitively; a fixed
// field's name is upper-cased. An empty key still lowers, as an empty method,
// because dropping the entry would lose what it declares; lowerOperation
// reports it.
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
			method:     method,
			seg:        ids.Ptr(additionalOperationsField, method),
			src:        src,
			additional: true,
		})
	}
	return ops
}

// lowerPaths lowers every path operation in source order into groups, stopping
// between path items when ctx is done. svc carries what a path item mounting no
// operation writes, which has no operation of its own to hold it.
func lowerPaths(ctx context.Context, c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, claims *operationIDClaims, groups *serviceGroups, svc *ir.Service) []ir.Diagnostic {
	paths := c.Doc.GetPaths()
	if paths == nil {
		return nil
	}
	var diags []ir.Diagnostic
	for path, rp := range paths.All() {
		if ctx.Err() != nil {
			return diags
		}
		// Judged before its item resolves: a key is a path or not, whatever
		// the item turns out to be.
		diags = append(diags, pathKeyDiags(c, path)...)
		layers, found := pathItemLayers(c, rp, ids.Ptr("paths", path))
		if layers == nil {
			continue
		}
		diags = append(diags, found...)
		diags = append(diags, lowerPathItem(c, ts, anchors, claims, groups, svc, path, layers)...)
	}
	return diags
}

// lowerPathItem lowers each method operation the layers of one path declare
// into its group, carrying along any callback operations registered under the
// same group. Each operation keeps its mount pointer (under path) as its
// identity, and lowers from where its layer declares it (see pathItemLayers).
func lowerPathItem(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, claims *operationIDClaims, groups *serviceGroups, svc *ir.Service, path string, layers []layer) []ir.Diagnostic {
	route := opContext{key: path, withCallbacks: true, inferred: groupingInferred(c)}
	mounted, diags := mountPathItem(ts, anchors, claims, layers, ids.Ptr("paths", path), route, &svc.Unmodeled)
	for _, m := range mounted {
		grp := groups.place(groups.groupFor(c, m.src, path))
		grp.Operations = append(grp.Operations, m.op)
		grp.Operations = append(grp.Operations, m.extra...)
	}
	return diags
}

// mountedOp is one operation a path item mounts, the callback operations
// lowered under it, and the source operation its route places it by.
type mountedOp struct {
	src   *soa.Operation
	op    ir.Operation
	extra []ir.Operation
}

// mountPathItem lowers every operation a path item's layers declare, mounted
// below mount, the one way all three routes to a path item (a path, a webhook,
// a callback expression) lower them. Each operation is lowered from the
// nearest layer declaring it, in that layer's context and below its
// declaration pointer, in mountedOperations' order; it takes every layer's item
// parameters merged into its own and what every layer keeps (applyLayers). A
// mount lowering none keeps what its layers declare in residue instead. route
// holds what the route sets on every operation; each brings its own method,
// pointers and parameters.
func mountPathItem(ts *compile.Types, anchors *schema.AnchorIndex, claims *operationIDClaims, layers []layer, mount jsontext.Pointer, route opContext, residue *ir.Unmodeled) ([]mountedOp, []ir.Diagnostic) {
	ops := mountedOperations(layers)
	owner, diags := precedence(layers, len(ops) > 0)
	params := itemParams(layers)
	mounted := make([]mountedOp, 0, len(ops))
	for _, lo := range ops {
		l, po := layers[lo.layer], lo.op
		opCtx := route
		opCtx.method = po.method
		opCtx.ptrs = opPointers{mount: mount + po.seg, decl: l.decl + po.seg}
		opCtx.params = mergeParameters(params, po.src.GetParameters(), opCtx.ptrs.decl)
		op, extra, opDiags := lowerOperation(l.in, ts, anchors, claims, po.src, opCtx)
		diags = append(diags, opDiags...)
		diags = append(diags, applyLayers(layers, owner, onOperation(&op))...)
		mounted = append(mounted, mountedOp{src: po.src, op: op, extra: extra})
	}
	if len(mounted) > 0 {
		return mounted, diags
	}
	for i, l := range layers {
		diags = append(diags, preserveUnmountedPathItem(l.in, onNearestNode(l.in, residue, mount), l.item, mount, l.decl,
			owner.cedes(i))...)
	}
	return nil, diags
}

// applyPathItem keeps what a path item declares that its operations have no
// home for: its servers, documentation, x-* extensions and undeclared keys,
// each written onto every operation the item declares, less what cedes says a
// nearer layer of the mount keeps.
//
// They share one entry point because every route that lowers a path item (a
// path, a webhook, a callback expression) must reach each, and a second call
// beside the first is a chance to forget one on a route added later (GitHub
// #39). The undeclared keys come from undeclaredPathItemKeys: the library's
// GetUnknownProperties is empty for this object.
func applyPathItem(c lowering.Ctx, into carrier, pi *soa.PathItem, declPtr jsontext.Pointer, cedes func(string) bool,
) []ir.Diagnostic {
	diags := applyPathServers(c, into, pi, declPtr, cedes)
	diags = append(diags, applyPathItemDocs(c, into, pi, declPtr, cedes)...)
	ext, extDiags := schema.ExtensionsIn(c, keptExtensions(pi.GetExtensions(), cedes), declPtr, into.scope)
	*into.unmodeled = annotation.MergeUnmodeled(*into.unmodeled, ext)
	diags = append(diags, extDiags...)
	return append(diags, annotation.UnknownKeysNamed(into.unmodeled, keptKeys(undeclaredPathItemKeys(pi), cedes),
		pi.GetRootNode(), c.ProvenanceAt, declPtr, into.scope)...)
}

// carrier is where a path item's own declarations are kept, and under what key
// scope. A path item lowers to no node of its own, so they are held by each
// operation it produced or, for an item that produced none, by the nearest node
// holding an Unmodeled map (see onNearestNode), usually the service, where the
// Paths Object's own extensions already go.
//
// scope carries the item's own pointer in the second case, because one service
// holds every such item and a bare "pathItem" prefix would make two collide on
// one key, the survivor decided by iteration order.
type carrier struct {
	unmodeled  *ir.Unmodeled
	provenance ir.Provenance
	// scope prefixes the keys built from a keyword name, as the extension and
	// undeclared-key writers do.
	scope string
	// serversKey is spelled in full rather than derived from scope, because the
	// servers entry has always been keyed by the bare keyword on an operation and
	// this must not move it.
	serversKey string
	// keepsDocs says whether the item's summary and description have a home here.
	// Only an operation does: pathItemDocFields keys them by bare keyword, which
	// two items on one service would collide on, and what a path item's
	// documentation means beside an operation's own is the question GitHub #383
	// is open on. An unmounted item's pair is dropped, as it was before this
	// carrier existed — the entry above widens what is kept, never narrows it.
	keepsDocs bool
}

// onOperation carries a path item's declarations on an operation it produced.
func onOperation(op *ir.Operation) carrier {
	return carrier{
		unmodeled:  &op.Unmodeled,
		provenance: withoutInferred(op.Provenance),
		scope:      "pathItem",
		serversKey: "openapi:servers",
		keepsDocs:  true,
	}
}

// onNearestNode carries them on the nearest node holding an Unmodeled map, for
// an item that produced no operation to hold them: the service for an item
// under paths or webhooks, the parent operation's HTTP binding for a callback
// expression's, which is where the callback itself lives.
//
// mountPtr is the item's own mount pointer, and keys it apart from every other
// item kept on that node. It is the provenance too: one node holds every such
// item, so the node's own pointer would name none of them and two items would
// produce one indistinguishable finding.
func onNearestNode(c lowering.Ctx, u *ir.Unmodeled, mountPtr jsontext.Pointer) carrier {
	return carrier{
		unmodeled:  u,
		provenance: c.ProvenanceAt(mountPtr),
		scope:      "pathItem" + string(mountPtr),
		serversKey: "openapi:pathItem" + string(mountPtr) + "/servers",
	}
}

// preserveUnmountedPathItem keeps what an item that produced no operation
// wrote, and says so: a reader seeing its declarations away from any operation
// needs to know why.
//
// It keeps what applyPathItem keeps on a carrier holding no docs; the summary
// and description are dropped (GitHub #383).
//
// The announcement follows what landed, not what the item wrote: a merge-key
// construct is read by the model but not by RawChildNode, so a model predicate
// would claim a preservation that did not happen (GitHub #384).
func preserveUnmountedPathItem(c lowering.Ctx, into carrier, pi *soa.PathItem, mountPtr, declPtr jsontext.Pointer,
	cedes func(string) bool,
) []ir.Diagnostic {
	before := len(*into.unmodeled)
	diags := applyPathItem(c, into, pi, declPtr, cedes)
	if len(*into.unmodeled) == before {
		return diags // nothing landed, so there is no preservation to explain
	}
	return append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, mountPtr,
		"path item declares no operation this compiler lowers; its servers, extensions and "+
			"undeclared keys are kept beside it, having no operation to hold them"))
}

// undeclaredPathItemKeys returns the keys a path item writes that the Path Item
// Object does not define. The library presents no one place that holds them
// all, so it reads two. The first is the item's operations map less every key
// naming a standard method: the unmarshaller folds a key it does not recognize
// into that map (docs/parser-library.md#embedded-map-keys). The second is the
// raw mapping less every key pathItemDeclares.
//
// A method spelled in any case but lowercase is undeclared, since OpenAPI fixes
// the field names.
//
// pi is already resolved through any $ref and is never nil: every route checks
// it before lowering.
func undeclaredPathItemKeys(pi *soa.PathItem) []string {
	var keys []string
	for method := range pi.All() {
		// The library's vocabulary, not httpMethods: that says what this compiler
		// lowers, while this asks whether the key names a method the specification
		// defines, and grading one as undeclared would be false.
		// TestHTTPMethods_AgreesWithLibraryVocabulary keeps the two in step.
		if soa.IsStandardMethod(string(method)) {
			continue
		}
		keys = append(keys, string(method))
	}
	// The unmarshaller skips a key whose value carries a YAML anchor, so `bogus:
	// &a {...}` is in neither the map nor a field (GitHub #412). Anchors are
	// cleared before a model is built (GitHub #459, #501, #538), short of an
	// external document past the hop bound that recovery follows; the raw node
	// stays the one place such a key would be written.
	for _, key := range annotation.RawMappingKeys(pi.GetRootNode()) {
		if pathItemDeclares(pi, key) {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

// pathItemFields are the keys the library's Path Item core model names a field
// for. The unmarshaller reads each into its field rather than folding it into the
// operations map, so a raw key spelling one is declared whether or not the map
// holds it. A hand-written list, held to the model's own key tags by
// TestPathItemFields_MatchTheLibraryModel so a field the library adds cannot
// start being reported as a key the document was not permitted to write.
var pathItemFields = []string{"summary", "description", "servers", "parameters", "additionalOperations"}

// pathItemDeclares reports whether key, read off a path item's raw mapping, is
// one the object accounts for: held in the operations map (the first reading of
// undeclaredPathItemKeys answers for it), naming a standard method, a field of
// the model, or an x- extension.
//
// A method held nowhere in the map is still declared: naming a standard method
// is enough. Where its anchor is left in place (see undeclaredPathItemKeys),
// `get: &g {...}` is skipped by the anchor rule and not lowered, but the
// specification defines the key, so the census cannot truthfully call it one
// the document may not write.
func pathItemDeclares(pi *soa.PathItem, key string) bool {
	return pi.Has(soa.HTTPMethod(key)) || soa.IsStandardMethod(key) ||
		strings.HasPrefix(key, "x-") || slices.Contains(pathItemFields, key)
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
// Unmodeled on each operation it holds (GitHub #292).
//
// They are kept, not merged into Docs: ir.Docs holds the operation's own, and
// merging would have to rank the pair against it and attribute to an operation
// text its author did not write (an inference; invariant 6). ReasonNoIRHome,
// not a boundary, since the IR could grow a home (GitHub #285); the cost is the
// pair repeated on every operation.
//
// Dropped silently: an unmounted item's pair (carrier.keepsDocs is false;
// GitHub #383) and a pair supplied through a merge key or alias, which
// RawChildNode does not read (GitHub #384).
func applyPathItemDocs(c lowering.Ctx, into carrier, pi *soa.PathItem, declPtr jsontext.Pointer,
	cedes func(string) bool,
) []ir.Diagnostic {
	if !into.keepsDocs || (pi.GetSummary() == "" && pi.GetDescription() == "") {
		return nil
	}
	var diags []ir.Diagnostic
	var kept bool
	for _, f := range pathItemDocFields {
		if cedes(constructOf(constructDoc, f.keyword)) {
			continue
		}
		wrote, fieldDiags := schema.PreserveNode(c, into.unmodeled, f.key,
			annotation.RawChildNode(pi.GetRootNode(), f.keyword), ir.ReasonNoIRHome,
			declPtr+ids.Ptr(f.keyword))
		diags = append(diags, fieldDiags...)
		kept = kept || wrote
	}
	if !kept {
		return diags
	}
	return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, into.provenance,
		"path-item documentation kept under Unmodeled; it documents the path rather than this "+
			"operation, and ir.Docs holds the operation's own summary and description"))
}

// applyPathServers preserves path-item-level servers verbatim under Unmodeled
// on the operation. §10 models servers as Document.Servers with per-scope index
// lists (Service.Servers, Channel.Servers); ir.Operation has no such list yet,
// so the scoping is kept raw with an info diagnostic, ReasonNoIRHome rather
// than a boundary because the IR can close the gap by adding one.
//
// This is the path-item half of the pair; applyOperationServers keeps the
// operation's own list, which overrides this one, under its own key.
func applyPathServers(c lowering.Ctx, into carrier, pi *soa.PathItem, declPtr jsontext.Pointer,
	cedes func(string) bool,
) []ir.Diagnostic {
	if len(pi.GetServers()) == 0 || cedes(constructOf(constructServers, "")) {
		return nil
	}
	kept, diags := schema.PreserveNode(c, into.unmodeled, into.serversKey,
		annotation.RawChildNode(pi.GetRootNode(), "servers"), ir.ReasonNoIRHome, declPtr+ids.Ptr("servers"))
	if !kept {
		return diags
	}
	return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, into.provenance,
		"path-item servers kept under Unmodeled; an operation has no server-scope list to bind them to"))
}
