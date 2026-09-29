package operation

import (
	"context"
	"encoding/json/jsontext"
	"slices"
	"strconv"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// refKey is the Path Item Object key that makes an entry a reference, spelled
// once so the read that removes it from the sibling set and nothing else can
// drift from it.
const refKey = "$ref"

// siblingPathItem is what a path item's use site writes beside its $ref: the
// model rebuilt from those keys, and the pointer they are written at.
//
// The model is rebuilt because an operation can be lowered only from a
// *soa.Operation, and the reference wrapper around a $ref'd item erases every
// sibling: GetCore().Object is nil on the $ref branch of the library's
// core.Reference unmarshal, so the operations, item parameters, servers, x-*
// and undeclared keys written beside the reference reached the IR in no form at
// all (GitHub #577).
type siblingPathItem struct {
	item   *soa.PathItem
	usePtr jsontext.Pointer
}

// pathItemRefSiblings reads, models and resolves the fields a path item writes
// beside its $ref at usePtr. It returns nil for an item that is not a reference,
// or writes nothing beside one — the shape every existing fixture has, and the
// one that must keep lowering byte-identically.
//
// The read is raw, through nodeview, because the parsed model holds none of
// these keys: MappingPairs follows an alias and expands a `<<` merge key exactly
// as speakeasy's resolver does, so the pairs are the ones the source's own
// reading produces.
func pathItemRefSiblings(ctx context.Context, c lowering.Ctx, usePtr jsontext.Pointer) (*siblingPathItem, []ir.Diagnostic) {
	pairs := siblingPairs(c, usePtr)
	if len(pairs) == 0 {
		return nil, nil
	}
	item := soa.NewPathItem()
	// The rebuild's own findings are not this lowering's channel: the loader
	// never modelled these keys, and what it reports is what the compiler cannot
	// carry, which every lowering here reports for itself.
	_, _ = marshaller.UnmarshalNode(ctx, "", siblingNode(pairs), item)
	return &siblingPathItem{item: item, usePtr: usePtr}, resolveSiblingRefs(ctx, c, item, usePtr)
}

// siblingPairs returns the effective pairs a path item's use site writes beside
// its $ref, or nil when there is no $ref at that position or nothing beside it.
//
// The empty result is what keeps a document without siblings on the path the
// compiler already had: nothing here is read, resolved or lowered unless the
// source actually wrote a field beside a reference.
func siblingPairs(c lowering.Ctx, usePtr jsontext.Pointer) []nodeview.Pair {
	view := nodeview.New()
	path, complete := view.DocumentPath(c.Doc.GetRootNode(), usePtr)
	if !complete {
		return nil
	}
	return withoutRef(view.MappingPairs(path[len(path)-1]))
}

// withoutRef returns pairs less the $ref key, or nil when that leaves nothing: a
// one-key `{$ref: …}` object is a pure reference and has no siblings to lower.
func withoutRef(pairs []nodeview.Pair) []nodeview.Pair {
	out := make([]nodeview.Pair, 0, len(pairs))
	referred := false
	for _, p := range pairs {
		if p.Key == refKey {
			referred = true
			continue
		}
		out = append(out, p)
	}
	if !referred || len(out) == 0 {
		return nil
	}
	return out
}

// siblingNode builds the mapping the sibling model is read from: the effective
// pairs keyed by synthesized key nodes, while every value stays the very node
// the document wrote.
//
// Keeping the document's value nodes is what makes the rest of the lowering work
// unchanged: a preserved construct is read raw off its node by RawChildNode, and
// the node's line and column are what a position names, so a copy would lose
// both.
func siblingNode(pairs []nodeview.Pair) *yaml.Node {
	content := make([]*yaml.Node, 0, len(pairs)*2)
	for _, p := range pairs {
		content = append(content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p.Key}, p.Val)
	}
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: content}
}

// resolvableRef is the method set every Referenced* wrapper shares: a reference
// that resolves in place, and the reference text it was written with.
type resolvableRef interface {
	GetReference() references.Reference
	Resolve(ctx context.Context, opts soa.ResolveOptions) ([]error, error)
}

// siblingRefMatcher resolves every reference kind a path item's subtree can
// reach. One field per kind, so a kind the walk yields is either resolved or
// visibly missing here; TestSiblingRefMatcher_CoversTheLibraryVocabulary holds
// the set to the library's own Matcher fields.
func siblingRefMatcher(note func(string, resolvableRef)) soa.Matcher {
	return soa.Matcher{
		ReferencedPathItem:       func(r *soa.ReferencedPathItem) error { note("path item", r); return nil },
		ReferencedParameter:      func(r *soa.ReferencedParameter) error { note("parameter", r); return nil },
		ReferencedHeader:         func(r *soa.ReferencedHeader) error { note("header", r); return nil },
		ReferencedExample:        func(r *soa.ReferencedExample) error { note("example", r); return nil },
		ReferencedRequestBody:    func(r *soa.ReferencedRequestBody) error { note("request body", r); return nil },
		ReferencedResponse:       func(r *soa.ReferencedResponse) error { note("response", r); return nil },
		ReferencedLink:           func(r *soa.ReferencedLink) error { note("link", r); return nil },
		ReferencedCallback:       func(r *soa.ReferencedCallback) error { note("callback", r); return nil },
		ReferencedSecurityScheme: func(r *soa.ReferencedSecurityScheme) error { note("security scheme", r); return nil },
		Schema:                   func(r *oas3.JSONSchema[oas3.Referenceable]) error { note("schema", r); return nil },
	}
}

// resolveSiblingRefs resolves every reference the sibling subtree declares,
// against this document and under the compile's own external-reference policy,
// reporting each one that did not resolve.
//
// It has to happen here rather than in the loader: the loader walks the
// document's model, and the model holds none of these nodes, so nothing else in
// the compile ever sees them. The walk starts at the *soa.ReferencedPathItem
// wrapper because *soa.PathItem has no match handler registered with the
// library's walk, and because a $ref'd callback's own expressions are reached
// only from the mount, never from a document-level walk.
func resolveSiblingRefs(ctx context.Context, c lowering.Ctx, item *soa.PathItem, usePtr jsontext.Pointer) []ir.Diagnostic {
	opts := soa.ResolveOptions{
		RootDocument:        c.Doc,
		TargetLocation:      c.Source.Path,
		DisableExternalRefs: !c.AllowExternalRefs(),
	}
	var diags []ir.Diagnostic
	note := func(kind string, r resolvableRef) {
		unresolved, err := r.Resolve(ctx, opts)
		if err != nil {
			unresolved = append(unresolved, err)
		}
		for _, e := range unresolved {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef, usePtr,
				"%s written beside the path item's $ref (%s) could not be resolved: %s",
				kind, r.GetReference(), diag.OneLine(e)))
		}
	}
	for w := range soa.Walk(ctx, &soa.ReferencedPathItem{Object: item}) {
		_ = w.Match(siblingRefMatcher(note))
	}
	return diags
}

// pathItemMount is one path item at one use site: the item it references, the
// pointers the operations being lowered are mounted and declared at, the fields
// the use site wrote beside the $ref, and the binding facts every operation at
// that mount shares.
//
// ref is the referenced item, not the item being lowered: both sides of a $ref
// carry the referent's item-level constructs onto their operations, so the
// carrier reads it from the mount rather than from the operation's source.
type pathItemMount struct {
	ref    *soa.PathItem
	refPtr jsontext.Pointer

	mountPtr jsontext.Pointer
	declPtr  jsontext.Pointer

	// sib is what the use site wrote beside the $ref, or nil.
	sib *siblingPathItem
	// params is the mount's path-item parameter list, each entry already paired
	// with the pointer of its own declaration site.
	params []sourcedParam

	uriTemplate   string
	isWebhook     bool
	withCallbacks bool
}

// grouping is the group an operation lowered at a mount belongs to, and the
// provenance its grouping choice stamps. A route that registers its operations
// with a group of its own passes one; the callback walk, whose operations join
// the parent operation's group, passes none.
type grouping struct {
	key  string
	name ir.Naming
	docs ir.Docs
	mark string
}

// mountedOp is one operation lowered at a mount, with the callback operations
// registered beside it and the group all of them belong to.
type mountedOp struct {
	grouping
	ops []ir.Operation
}

// mountOperation lowers one operation of a path item mounted at one use site,
// applying to it the item-level constructs of both the use site and the item it
// references.
//
// It is the one place a path-item operation is lowered, so all three routes and
// both sides of a $ref reach it alike: a construct a later route forgets is a
// construct missing from every route rather than from the one that was edited.
func mountOperation(ctx context.Context, c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex,
	claims *operationIDClaims, m pathItemMount, po pathOperation, inferred string,
) (ir.Operation, []ir.Operation, []ir.Diagnostic) {
	ptrs := opPointers{mount: m.mountPtr + po.seg, decl: m.declPtr + po.seg}
	opCtx := opContext{
		method:        po.method,
		uriTemplate:   m.uriTemplate,
		isWebhook:     m.isWebhook,
		withCallbacks: m.withCallbacks,
		inferred:      inferred,
		ptrs:          ptrs,
		params:        mergeRefParams(m.params, po.src.GetParameters(), ptrs.decl),
	}
	op, extra, diags := lowerOperation(ctx, c, ts, anchors, claims, po.src, opCtx)
	diags = append(diags, applyPathItem(c, onOperation(&op), m.ref, m.refPtr)...)
	if m.sib != nil {
		// The use site's constructs land last, so on a collision they are the
		// ones the carrier holds — the use-site precedence the compiler already
		// applies to a $ref-adjacent schema sibling.
		diags = append(diags, applyPathItem(c, onOperation(&op), m.sib.item, m.sib.usePtr)...)
	}
	return op, extra, diags
}

// mountedOperations lowers every operation a path item declares at one mount, in
// the order pathOperations yields them, and reports what each lowering found.
// skip names the method keys the collision pass took over, which only the
// referent side is ever given.
//
// group is how the caller names each operation's group, or nil for a route with
// no group accumulator. It is the caller's because the group a route builds is
// the route's own: a webhook's group is synthesized, and a callback's operations
// join their parent's.
func mountedOperations(ctx context.Context, c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex,
	claims *operationIDClaims, m pathItemMount, item *soa.PathItem, skip map[string]bool,
	group func(*soa.Operation) grouping,
) ([]mountedOp, []ir.Diagnostic) {
	declared := pathOperations(item)
	mounts := make([]mountedOp, 0, len(declared))
	var diags []ir.Diagnostic
	for _, po := range declared {
		if skip[string(po.seg)] {
			continue
		}
		g := group(po.src)
		op, extra, opDiags := mountOperation(ctx, c, ts, anchors, claims, m, po, g.mark)
		diags = append(diags, opDiags...)
		mounts = append(mounts, mountedOp{grouping: g, ops: append([]ir.Operation{op}, extra...)})
	}
	return mounts, diags
}

// siblingRefMount reads the fields a use site writes beside its $ref and judges
// what the two declarations say about one another: the sibling model (nil when
// there is nothing beside the reference), the method keys the use site takes
// over, and one finding per colliding construct naming both positions.
//
// It is the one place the seam is opened, so all three routes judge a collision
// the same way and none can lower a reference's own operations while losing the
// ones written beside it.
func siblingRefMount(ctx context.Context, c lowering.Ctx, ref *soa.PathItem, refPtr,
	usePtr jsontext.Pointer,
) (*siblingPathItem, map[string]bool, []ir.Diagnostic) {
	sib, diags := pathItemRefSiblings(ctx, c, usePtr)
	if sib == nil {
		return nil, nil, diags
	}
	skip, collisions := pathItemRefCollisions(c, ref, refPtr, sib)
	return sib, skip, append(diags, collisions...)
}

// webhookGrouping puts every webhook operation in the one group the compiler
// synthesizes for them, and stamps no grouping provenance: a webhook is not
// grouped by anything the document declares.
func webhookGrouping(*soa.Operation) grouping {
	return grouping{key: "webhook", name: compile.NamingHint("webhooks")}
}

// mergeRefParams puts an operation's own parameters ahead of its mount's
// path-item list and drops any path-item entry the operation shadows, which is
// the merge mergeParameters performs for a path-item list already paired with
// the pointer of each entry's own declaration site.
func mergeRefParams(pathParams []sourcedParam, opParams []*soa.ReferencedParameter, opDeclPtr jsontext.Pointer) []sourcedParam {
	merged := appendSourced(nil, opParams, opDeclPtr, nil)
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

// combinedPathParams is the path-item parameter list at a mount whose use site
// declares siblings: the use site's entries first and unshadowed, then the
// referent's, each carrying the pointer of its own declaration site. A shared
// (name, in) is the use site's, the precedence every other colliding construct
// gets, and the collision pass reports the pair.
func combinedPathParams(ref *soa.PathItem, refPtr jsontext.Pointer, sib *siblingPathItem) []sourcedParam {
	siblings := sib.item.GetParameters()
	params := appendSourced(nil, siblings, sib.usePtr, nil)
	return appendSourced(params, ref.GetParameters(), refPtr, shadowedKeys(siblings))
}

// refMountParams is the path-item parameter list at a mount without siblings:
// the referent's own list, each entry paired with its own declaration pointer.
func refMountParams(ref *soa.PathItem, refPtr jsontext.Pointer) []sourcedParam {
	return appendSourced(nil, ref.GetParameters(), refPtr, nil)
}

// pathItemRefCollisions reports every construct the referent and the use site
// both declare at one mount, and returns the set of method keys the use site's
// declaration takes over — the operations the referent must not lower here.
//
// The loser is never silent: each finding is sited at the use site's own key and
// names the referent's, so both positions read off one diagnostic.
func pathItemRefCollisions(c lowering.Ctx, ref *soa.PathItem, refPtr jsontext.Pointer,
	sib *siblingPathItem,
) (map[string]bool, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	note := func(label string, sibPtr, refDecl jsontext.Pointer) {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.PathItemRefCollision, sibPtr,
			"the field beside the path item's $ref declares %s here, which the path item it "+
				"references also declares at %s; the use site's declaration is the one lowered "+
				"at this mount", label, refDecl))
	}
	taken := map[string]bool{}
	for _, po := range pathOperations(sib.item) {
		taken[string(po.seg)] = true
	}
	for _, po := range pathOperations(ref) {
		if taken[string(po.seg)] {
			note("the "+po.method+" operation", sib.usePtr+po.seg, refPtr+po.seg)
		}
	}
	for _, keyword := range []string{"summary", "description"} {
		if declaresText(ref, keyword) && declaresText(sib.item, keyword) {
			note(keyword, sib.usePtr+ids.Ptr(keyword), refPtr+ids.Ptr(keyword))
		}
	}
	if len(ref.GetServers()) > 0 && len(sib.item.GetServers()) > 0 {
		note("servers", sib.usePtr+ids.Ptr("servers"), refPtr+ids.Ptr("servers"))
	}
	collideOnKeys(note, "an extension named", extensionNames(ref), extensionNames(sib.item),
		sib.usePtr, refPtr)
	collideOnKeys(note, "an undeclared key named", undeclaredPathItemKeys(ref),
		undeclaredPathItemKeys(sib.item), sib.usePtr, refPtr)
	collideOnParams(note, ref, refPtr, sib)
	return taken, diags
}

// declaresText reports whether an item declares a text keyword at all, as a
// non-empty value: an empty pair is nothing to keep, so it cannot collide with
// anything.
func declaresText(pi *soa.PathItem, keyword string) bool {
	if keyword == "summary" {
		return pi.GetSummary() != ""
	}
	return pi.GetDescription() != ""
}

// extensionNames returns the x-* names an item declares, sorted so the findings
// that name them do not depend on the order a map iterates in.
func extensionNames(pi *soa.PathItem) []string {
	var names []string
	for name := range pi.GetExtensions().All() {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// collideOnKeys reports each name both key sets hold, at the pointer the use
// site writes it at.
func collideOnKeys(note func(string, jsontext.Pointer, jsontext.Pointer), label string,
	sibKeys, refKeys []string, sibPtr, refPtr jsontext.Pointer,
) {
	present := make(map[string]bool, len(refKeys))
	for _, k := range refKeys {
		present[k] = true
	}
	var shared []string
	for _, k := range sibKeys {
		if present[k] {
			shared = append(shared, k)
		}
	}
	slices.Sort(shared)
	for _, k := range shared {
		note(label+" "+strconv.Quote(k), sibPtr+ids.Ptr(k), refPtr+ids.Ptr(k))
	}
}

// collideOnParams reports each (name, in) pair both item parameter lists
// declare, at the sibling entry's own index in the use site's list.
func collideOnParams(note func(string, jsontext.Pointer, jsontext.Pointer),
	ref *soa.PathItem, refPtr jsontext.Pointer, sib *siblingPathItem,
) {
	referenced := map[string]int{}
	for i, p := range ref.GetParameters() {
		if key, ok := paramKey(p); ok {
			referenced[key] = i
		}
	}
	for i, p := range sib.item.GetParameters() {
		key, ok := paramKey(p)
		if !ok {
			continue
		}
		if at, dup := referenced[key]; dup {
			note("the parameter "+strconv.Quote(key), sibParamPtr(sib.usePtr, i), refParamPtr(refPtr, at))
		}
	}
}

// sibParamPtr is the pointer of one entry of a use site's item parameter list.
func sibParamPtr(usePtr jsontext.Pointer, i int) jsontext.Pointer {
	return usePtr + ids.Ptr("parameters", strconv.Itoa(i))
}

// refParamPtr is the pointer of one entry of a referent's item parameter list.
func refParamPtr(declPtr jsontext.Pointer, i int) jsontext.Pointer {
	return declPtr + ids.Ptr("parameters", strconv.Itoa(i))
}
