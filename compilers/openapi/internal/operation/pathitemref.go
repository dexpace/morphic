package operation

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strconv"

	"github.com/speakeasy-api/openapi/extensions"
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// layer is one declaration a path item mount lowers: the fields one use site
// writes beside a $ref, or the item its chain ends at. A Path Item Object may
// carry fields beside its $ref (GitHub #577), so a mount reads the fields beside
// every $ref of its chain, nearest the use site first, then the item.
type layer struct {
	item *soa.PathItem
	// ptr is where the layer's constructs and operations are declared, and name
	// how a finding names that position: the pointer itself, but for an item
	// another document holds, which no pointer here addresses.
	ptr  jsontext.Pointer
	name string
	in   lowering.Ctx
}

// pathItemMount is one path item at one use site: its layers, nearest first,
// what reading them found, the pointer its operations are mounted under, and
// the binding facts every operation lowered there shares.
type pathItemMount struct {
	layers   []layer
	found    []ir.Diagnostic
	mountPtr jsontext.Pointer

	uriTemplate   string
	isWebhook     bool
	withCallbacks bool
}

// newMount returns the mount of the path item rp names, written at usePtr and
// read in use, whose chain ends at pi, declared at declPtr. The fields beside a
// $ref of the chain that another document writes are not modelled, so each
// such $ref is reported rather than dropped in silence.
func newMount(use lowering.Ctx, rp *soa.ReferencedPathItem, pi *soa.PathItem, usePtr, declPtr jsontext.Pointer,
) pathItemMount {
	source := use.InScope(use.RefScope().InSource())
	var layers []layer
	var diags []ir.Diagnostic
	reached, last := string(usePtr), rp
	for i, hop := range append([]*soa.ReferencedPathItem{rp}, resolve.Hops[soa.PathItem](rp)...) {
		if i > 0 {
			reached, last = string(last.GetReference()), hop
		}
		if sib, ok := use.SiblingsOf(hop); ok {
			ptr := sib.Pointer
			if i == 0 {
				ptr = usePtr // an alias's own position, as for an inline item
			}
			layers = append(layers, layer{item: sib.Item, ptr: ptr, name: string(ptr), in: source})
			continue
		}
		if use.SiblingsUnread(hop) {
			diags = append(diags, use.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, usePtr,
				"the path item at %s writes fields beside its $ref in another document, which this compiler "+
					"does not read; they are not lowered at this mount", reached))
		}
	}
	return pathItemMount{layers: append(layers, referent(use, rp, last, pi, declPtr, layers)), found: diags}
}

// referent returns the layer of pi, the item rp's chain ends at through last,
// declared at declPtr. A nearer layer declaring at declPtr too is the use site
// of an item another document holds, which no pointer here addresses: the item
// is then declared under the use site's "$ref", a position the source cannot
// name, so the two never intern a type at one pointer (invariant 3).
func referent(use lowering.Ctx, rp, last *soa.ReferencedPathItem, pi *soa.PathItem, declPtr jsontext.Pointer,
	nearer []layer,
) layer {
	ref := layer{item: pi, ptr: declPtr, name: string(declPtr), in: lowering.Within[soa.PathItem](use, rp)}
	if !slices.ContainsFunc(nearer, func(l layer) bool { return l.ptr == declPtr }) {
		return ref
	}
	ref.ptr += ids.Ptr("$ref")
	ref.name = string(last.GetReference())
	return ref
}

// construct keys name what a layer declares that a nearer one can take over.
// Two layers declaring one key are a collision, and only the nearer keeps it.
const (
	constructOperation = "operation"
	constructDoc       = "doc"
	constructServers   = "servers"
	constructExtension = "extension"
	constructKey       = "key"
	constructParameter = "parameter"
)

// constructOf returns the key of one construct of kind named name.
func constructOf(kind, name string) string { return kind + "\x00" + name }

// itemConstruct is one construct a layer declares: its key, how a finding
// names it, and where it is written relative to the layer.
type itemConstruct struct {
	key, label string
	rel        jsontext.Pointer
}

// itemConstructs returns what l declares, in the order its findings are reported.
// docs says whether the mount keeps an item's summary and description, which
// only one mounting an operation does, so an unmounted pair collides with none.
func itemConstructs(l layer, docs bool) []itemConstruct {
	var out []itemConstruct
	add := func(kind, name, label string, rel jsontext.Pointer) {
		out = append(out, itemConstruct{key: constructOf(kind, name), label: label, rel: rel})
	}
	for _, po := range pathOperations(l.item) {
		add(constructOperation, string(po.seg), "the "+po.method+" operation", po.seg)
	}
	for _, f := range pathItemDocFields {
		if docs && declaresText(l.item, f.keyword) {
			add(constructDoc, f.keyword, f.keyword, ids.Ptr(f.keyword))
		}
	}
	if len(l.item.GetServers()) > 0 {
		add(constructServers, "", "servers", ids.Ptr("servers"))
	}
	for _, name := range extensionNames(l.item) {
		add(constructExtension, name, "an extension named "+strconv.Quote(name), ids.Ptr(name))
	}
	for _, key := range slices.Sorted(slices.Values(undeclaredPathItemKeys(l.item))) {
		add(constructKey, key, "an undeclared key named "+strconv.Quote(key), ids.Ptr(key))
	}
	for i, rp := range l.item.GetParameters() {
		if key, ok := paramKey(rp); ok {
			p := resolve.Object[soa.Parameter](rp)
			add(constructParameter, key, fmt.Sprintf("the parameter %q in %s", p.GetName(), p.GetIn()),
				ids.Ptr("parameters", strconv.Itoa(i)))
		}
	}
	return out
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

// owners records which layer keeps each construct a mount's layers declare: the
// nearest one declaring it.
type owners map[string]int

// cedes returns whether layer i gives up the construct key, which a nearer
// layer keeps.
func (o owners) cedes(i int) func(key string) bool {
	return func(key string) bool {
		at, ok := o[key]
		return ok && at != i
	}
}

// precedence returns which layer keeps each construct, and one finding for each
// declaration a nearer one takes over, sited at the keeper's and naming the
// other's position, so neither is silent. One layer, every mount with nothing
// beside a $ref, keeps all it declares.
//
// A farther layer is looked up by key, never scanned, so the work is the
// constructs times the layers, which the reference chain bound caps.
func (m pathItemMount) precedence(docs bool) (owners, []ir.Diagnostic) {
	if len(m.layers) == 1 {
		return owners{}, nil
	}
	decls := make([][]itemConstruct, len(m.layers))
	first := make([]map[string]itemConstruct, len(m.layers))
	for i, l := range m.layers {
		decls[i] = itemConstructs(l, docs)
		first[i] = firstDeclared(decls[i])
	}
	owner := owners{}
	var diags []ir.Diagnostic
	for i, l := range m.layers {
		for _, d := range decls[i] {
			if _, kept := owner[d.key]; kept {
				continue
			}
			owner[d.key] = i
			for j := i + 1; j < len(m.layers); j++ {
				if other, ok := first[j][d.key]; ok {
					diags = append(diags, l.in.DiagAt(ir.SeverityWarning, diag.PathItemRefCollision, l.ptr+d.rel,
						"the field beside the path item's $ref declares %s here, which the path item it references "+
							"also declares at %s; the declaration here is the one kept at this mount",
						d.label, m.layers[j].name+string(other.rel)))
				}
			}
		}
	}
	return owner, diags
}

// firstDeclared indexes one layer's constructs by key, each to the first that
// declares it, which is the one a finding names.
func firstDeclared(decls []itemConstruct) map[string]itemConstruct {
	first := make(map[string]itemConstruct, len(decls))
	for _, d := range decls {
		if _, seen := first[d.key]; !seen {
			first[d.key] = d
		}
	}
	return first
}

// itemParams is the mount's path-item parameter list: each layer's entries,
// nearest first, less any (in, name) a nearer layer declares, each paired with
// the pointer of its own declaration and the scope its layer is read in, since
// one operation lowers every layer's.
func (m pathItemMount) itemParams() []sourcedParam {
	var params []sourcedParam
	shadowed := map[string]bool{}
	for _, l := range m.layers {
		n := len(params)
		params = appendSourced(params, l.item.GetParameters(), l.ptr, shadowed)
		scoped(params[n:], l.in.RefScope())
		for key := range shadowedKeys(l.item.GetParameters()) {
			shadowed[key] = true
		}
	}
	return params
}

// scoped marks each of params, in place, as read in scope.
func scoped(params []sourcedParam, scope resolve.Scope) {
	for i := range params {
		params[i].in = &scope
	}
}

// grouping is the group an operation lowered at a mount belongs to, and the
// provenance its grouping choice stamps. Each route names its own: a path's
// group is the operation's, a webhook's is synthesized, and a callback's
// operations join their parent's group, so it names only the mark.
type grouping struct {
	id   ir.GroupID
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

// lower lowers every operation the mount's layers declare, the referent's first,
// each layer's but for a method a nearer layer declares too, and carries every
// layer's item-level constructs onto each. group names an operation's group and
// must not be nil. A mount lowering none keeps those constructs in orphan.
//
// It is the one place a path item's operations are lowered, so the three routes
// and every layer of a $ref reach it alike.
func (m pathItemMount) lower(ctx context.Context, ts *compile.Types, anchors *schema.AnchorIndex,
	claims *operationIDClaims, group func(*soa.Operation) grouping, orphan *ir.Unmodeled,
) ([]mountedOp, []ir.Diagnostic) {
	mounted := slices.ContainsFunc(m.layers, func(l layer) bool { return len(pathOperations(l.item)) > 0 })
	owner, collisions := m.precedence(mounted)
	diags := append(slices.Clip(m.found), collisions...)
	params := m.itemParams()
	var mounts []mountedOp
	for i := len(m.layers) - 1; i >= 0; i-- {
		for _, po := range pathOperations(m.layers[i].item) {
			if owner.cedes(i)(constructOf(constructOperation, string(po.seg))) {
				continue
			}
			g := group(po.src)
			op, extra, opDiags := m.mountOperation(ctx, ts, anchors, claims, owner, i, po, params, g.mark)
			diags = append(diags, opDiags...)
			mounts = append(mounts, mountedOp{grouping: g, ops: append([]ir.Operation{op}, extra...)})
		}
	}
	if mounted {
		return mounts, diags
	}
	for i, l := range m.layers {
		diags = append(diags, preserveUnmountedPathItem(l.in, onNearestNode(l.in, orphan, m.mountPtr), l.item,
			m.mountPtr, l.ptr, owner.cedes(i))...)
	}
	return nil, diags
}

// mountOperation lowers one operation of the mount's i-th layer, mounted under
// the mount and declared under its layer, and applies to it the item-level
// constructs each layer keeps.
func (m pathItemMount) mountOperation(ctx context.Context, ts *compile.Types, anchors *schema.AnchorIndex,
	claims *operationIDClaims, owner owners, i int, po pathOperation, params []sourcedParam, inferred string,
) (ir.Operation, []ir.Operation, []ir.Diagnostic) {
	l := m.layers[i]
	ptrs := opPointers{mount: m.mountPtr + po.seg, decl: l.ptr + po.seg}
	opCtx := opContext{
		method:        po.method,
		uriTemplate:   m.uriTemplate,
		isWebhook:     m.isWebhook,
		withCallbacks: m.withCallbacks,
		inferred:      inferred,
		ptrs:          ptrs,
		params:        mergeRefParams(params, po.src.GetParameters(), ptrs.decl),
	}
	op, extra, diags := lowerOperation(ctx, l.in, ts, anchors, claims, po.src, opCtx)
	for k := len(m.layers) - 1; k >= 0; k-- {
		diags = append(diags, applyPathItem(m.layers[k].in, onOperation(&op), m.layers[k].item, m.layers[k].ptr,
			owner.cedes(k))...)
	}
	return op, extra, diags
}

// keptExtensions returns the x-* entries of ext that cedes does not give up:
// ext itself when it gives up none.
func keptExtensions(ext *extensions.Extensions, cedes func(string) bool) *extensions.Extensions {
	var kept []*extensions.Element
	ceded := false
	for name, node := range ext.All() {
		if cedes(constructOf(constructExtension, name)) {
			ceded = true
			continue
		}
		kept = append(kept, extensions.NewElem(name, node))
	}
	if !ceded {
		return ext
	}
	return extensions.New(kept...)
}

// keptKeys returns the undeclared keys that cedes does not give up.
func keptKeys(keys []string, cedes func(string) bool) []string {
	return slices.DeleteFunc(keys, func(key string) bool { return cedes(constructOf(constructKey, key)) })
}

// webhookGrouping puts every webhook operation in the one group the compiler
// synthesizes for them, and stamps no grouping provenance: a webhook is not
// grouped by anything the document declares. The name is a hint, not a source
// name, since no document declares the group (GitHub #184).
func webhookGrouping(*soa.Operation) grouping {
	return grouping{id: ids.WebhookGroup(), name: compile.NamingHint("webhooks")}
}

// mergeRefParams merges a mount's path-item parameters with an operation's own
// using use-site precedence: an operation parameter with the same (name, in)
// overrides the path-item one, and unshadowed path-item parameters follow in
// source order. Each entry keeps the pointer of its own declaration site rather
// than a position recomputed from the merged slice (issue #36), and a $ref'd
// path item's base is its own component pointer, not the path it is mounted at
// (issue #107).
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
