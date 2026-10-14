package operation

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strconv"

	"github.com/speakeasy-api/openapi/extensions"
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// layer is one declaration a path item mount lowers: the fields one use site
// writes beside a $ref, or the item its chain ends at. A Path Item Object may
// carry fields beside its $ref (GitHub #577), so a mount reads the fields beside
// every $ref of its chain, nearest the use site first, then the item.
type layer struct {
	item *soa.PathItem
	// decl is where the layer's constructs are declared, and name how a finding
	// names that position: decl itself, but for an item another document holds,
	// which no pointer here addresses.
	decl jsontext.Pointer
	name string
	in   lowering.Ctx
}

// pathItemLayers returns the layers of the path item rp names at usePtr, read
// in use, whose chain ends at pi, declared at declPtr. The fields beside a $ref
// of the chain that the load phase did not model are reported rather than
// dropped in silence (see unreadFields).
func pathItemLayers(use lowering.Ctx, rp *soa.ReferencedPathItem, pi *soa.PathItem, usePtr, declPtr jsontext.Pointer,
) ([]layer, []ir.Diagnostic) {
	sibs := use.Siblings()
	var layers []layer
	var diags []ir.Diagnostic
	// visit reads one $ref of the chain: decl is where its fields are declared,
	// or "" for where the walk first met them, and reached how a report names it.
	visit := func(hop *soa.ReferencedPathItem, decl jsontext.Pointer, reached string, foreign bool) {
		if sib, ok := sibs.Of(hop); ok {
			if decl == "" {
				decl = sib.Pointer
			}
			in := use.InScope(use.RefScope().InSource())
			layers = append(layers, layer{item: sib.Item, decl: decl, name: string(decl), in: in})
			return
		}
		if sibs.Unread(hop) {
			diags = append(diags, unreadFields(use, usePtr, reached, foreign))
		}
	}
	visit(rp, usePtr, string(usePtr), use.RefScope().Foreign) // an alias's own position, as for an inline item
	last := rp
	if !resolve.Chained[soa.PathItem](rp) {
		return append(layers, referent(use, rp, last, pi, declPtr, layers)), diags
	}
	// A scope a layer or Hops keeps outlives the call, taking the context's
	// closures with it, so each is read where it is kept, not for every mount.
	for _, hop := range resolve.Hops[soa.PathItem](use.RefScope(), rp) {
		visit(hop.Ref, "", string(last.GetReference()), hop.Foreign)
		last = hop.Ref
	}
	return append(layers, referent(use, rp, last, pi, declPtr, layers)), diags
}

// unreadFields reports, at the mount's use site, the fields written beside the
// $ref of the path item at reached that the load phase did not model: another
// document's, which it does not walk, or those at a position of this one its
// walk does not reach, such as under an x-* key (GitHub #823).
func unreadFields(use lowering.Ctx, usePtr jsontext.Pointer, reached string, foreign bool) ir.Diagnostic {
	where := "at a position the compiler's walk of this document does not reach"
	if foreign {
		where = "in another document, which this compiler does not read"
	}
	return use.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, usePtr,
		"the path item at %s writes fields beside its $ref %s; they are not lowered at this mount", reached, where)
}

// referent returns the layer of pi, the item rp's chain ends at through last,
// declared at declPtr. A nearer layer declaring at declPtr too is the use site
// of an item another document holds, which no pointer here addresses: the item
// is then declared under the use site's "$ref", a position the source cannot
// name, so the two never intern a type at one pointer (invariant 3). A finding
// names its positions by the reference, given a fragment when it has none so a
// pointer appended to it reads as one.
func referent(use lowering.Ctx, rp, last *soa.ReferencedPathItem, pi *soa.PathItem, declPtr jsontext.Pointer,
	nearer []layer,
) layer {
	ref := layer{item: pi, decl: declPtr, name: string(declPtr), in: lowering.Within[soa.PathItem](use, rp)}
	if !slices.ContainsFunc(nearer, func(l layer) bool { return l.decl == declPtr }) {
		return ref
	}
	ref.decl += ids.Ptr("$ref")
	ref.name = string(last.GetReference())
	if !last.GetReference().HasJSONPointer() {
		ref.name += "#"
	}
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

// itemConstructs returns what l declares, in the order its findings are
// reported. docs says whether the mount keeps an item's summary and
// description, which only one mounting an operation does, so an unmounted pair
// collides with none.
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
			add(constructDoc, f.keyword, "the "+f.keyword, ids.Ptr(f.keyword))
		}
	}
	if len(l.item.GetServers()) > 0 {
		add(constructServers, "", "the servers list", ids.Ptr("servers"))
	}
	for _, name := range extensionNames(l.item) {
		add(constructExtension, name, "the extension "+strconv.Quote(name), ids.Ptr(name))
	}
	for _, key := range slices.Sorted(slices.Values(undeclaredPathItemKeys(l.item))) {
		add(constructKey, key, "the undeclared key "+strconv.Quote(key), ids.Ptr(key))
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

// precedence returns which of layers keeps each construct, and one finding for
// each declaration a nearer layer takes over, sited at the keeper's and naming
// the other's position, so neither is silent. docs says whether the mount keeps
// summaries and descriptions (see itemConstructs). One layer, every mount with
// nothing beside a $ref, keeps all it declares.
//
// A farther layer is looked up by key, never scanned, so the work is the
// constructs times the layers, which the reference chain bound caps.
func precedence(layers []layer, docs bool) (owners, []ir.Diagnostic) {
	if len(layers) == 1 {
		return nil, nil
	}
	decls := make([][]itemConstruct, len(layers))
	first := make([]map[string]itemConstruct, len(layers))
	for i, l := range layers {
		decls[i] = itemConstructs(l, docs)
		first[i] = firstDeclared(decls[i])
	}
	owner := owners{}
	var diags []ir.Diagnostic
	for i, l := range layers {
		for _, d := range decls[i] {
			if _, kept := owner[d.key]; kept {
				continue
			}
			owner[d.key] = i
			for j := i + 1; j < len(layers); j++ {
				if other, ok := first[j][d.key]; ok {
					diags = append(diags, l.in.DiagAt(ir.SeverityWarning, diag.PathItemRefCollision, l.decl+d.rel,
						"%s is declared here, beside the path item's $ref, and again at %s in the item it "+
							"references; OpenAPI leaves which applies undefined, and this mount keeps the one declared here",
						d.label, layers[j].name+string(other.rel)))
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

// itemParams is a mount's path-item parameter list: each layer's entries,
// nearest first, less any (in, name) a nearer layer declares, each paired with
// the pointer of its own declaration and the scope its layer is read in, since
// one operation lowers every layer's.
func itemParams(layers []layer) []sourcedParam {
	if len(layers) == 1 {
		// One layer's entries are read in its operations' own scope.
		return appendSourced(nil, layers[0].item.GetParameters(), layers[0].decl, nil, nil)
	}
	var params []sourcedParam
	shadowed := map[string]bool{}
	for _, l := range layers {
		scope := l.in.RefScope()
		params = appendSourced(params, l.item.GetParameters(), l.decl, shadowed, &scope)
		for key := range shadowedKeys(l.item.GetParameters()) {
			shadowed[key] = true
		}
	}
	return params
}

// layerOp is one operation a mount lowers and the index of the layer declaring
// it.
type layerOp struct {
	layer int
	op    pathOperation
}

// mountedOperations returns the operations a mount's layers declare, each from
// the nearest layer declaring it, which is the one precedence lets keep it. They
// come in the order one item holding them all would list them (pathOperations):
// the fixed methods in httpMethods order, then the additionalOperations keys,
// the farthest layer's first, each layer's in source order, a nearer layer's
// declaration taking the place of the key it repeats. So a mount lists the
// methods written beside a $ref where the same fields written inline would put
// them.
func mountedOperations(layers []layer) []layerOp {
	if len(layers) == 1 {
		declared := pathOperations(layers[0].item)
		out := make([]layerOp, 0, len(declared))
		for _, po := range declared {
			out = append(out, layerOp{op: po})
		}
		return out
	}
	declared := make([]map[jsontext.Pointer]pathOperation, len(layers))
	var extra []jsontext.Pointer
	listed := map[jsontext.Pointer]bool{}
	for i, l := range slices.Backward(layers) {
		declared[i] = map[jsontext.Pointer]pathOperation{}
		for _, po := range pathOperations(l.item) {
			declared[i][po.seg] = po
			if po.additional && !listed[po.seg] {
				listed[po.seg] = true
				extra = append(extra, po.seg)
			}
		}
	}
	var out []layerOp
	take := func(seg jsontext.Pointer) {
		for i := range layers {
			if po, ok := declared[i][seg]; ok {
				out = append(out, layerOp{layer: i, op: po})
				return
			}
		}
	}
	for _, m := range httpMethods {
		take(ids.Ptr(m.name))
	}
	for _, seg := range extra {
		take(seg)
	}
	return out
}

// applyLayers keeps on into what every layer declares and keeps at the mount,
// farthest first, as applyPathItem does for one item.
func applyLayers(layers []layer, owner owners, into carrier) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for k, l := range slices.Backward(layers) {
		diags = append(diags, applyPathItem(l.in, into, l.item, l.decl, owner.cedes(k))...)
	}
	return diags
}

// keptExtensions returns the x-* entries of ext that cedes does not give up:
// ext itself when it gives up none.
func keptExtensions(ext *extensions.Extensions, cedes func(string) bool) *extensions.Extensions {
	cedesAny := false
	for name := range ext.All() {
		cedesAny = cedesAny || cedes(constructOf(constructExtension, name))
	}
	if !cedesAny {
		return ext
	}
	var kept []*extensions.Element
	for name, node := range ext.All() {
		if !cedes(constructOf(constructExtension, name)) {
			kept = append(kept, extensions.NewElem(name, node))
		}
	}
	return extensions.New(kept...)
}

// keptKeys returns the undeclared keys that cedes does not give up.
func keptKeys(keys []string, cedes func(string) bool) []string {
	return slices.DeleteFunc(keys, func(key string) bool { return cedes(constructOf(constructKey, key)) })
}
