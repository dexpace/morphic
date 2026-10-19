package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"reflect"
	"sync"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// restoring names the work of putting $refs back as written in a panic it
// recovers from.
const restoring = "restoring $refs as written"

// escapes holds each $ref value load respelled for the resolver in a tree it
// builds models from, by the node in the $ref's place, with the value written
// there, and each node it changed as it was (#911).
//
// The resolver reads '+' in a fragment as a space, and builds models from raw
// YAML as well as from the source's model. So the spelling is fixed in the
// trees before anything is built, and put back once resolution is done: the
// IR, verbatim copies and diagnostics see only what the source writes. A nil
// *escapes holds nothing, and its methods do nothing.
type escapes struct {
	mu      sync.Mutex // prepare can run on several readers of one compile
	written map[*yaml.Node]string
	changed []changed
}

// changed is a node escape changed in place, and the node as it was.
type changed struct {
	node *yaml.Node
	was  yaml.Node
}

// newEscapes returns a registry holding nothing.
func newEscapes() *escapes {
	return &escapes{written: map[*yaml.Node]string{}}
}

// escape respells, in the tree under root, each $ref value the resolver would
// misread to ForResolver's spelling, and records what it changed. A $ref
// written as an alias becomes a scalar of its own, so the scalar it names is
// not changed. One written as a scalar is changed, and an alias naming it
// elsewhere, which may be text, becomes a scalar holding it as written. Every
// node keeps its identity, which models and indexes of the tree are keyed by.
func (e *escapes) escape(root *yaml.Node) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	respelled := map[*yaml.Node]string{}
	eachRefValue(root, func(n *yaml.Node) bool {
		v := nodeview.Deref(n)
		spelled := fragment.ForResolver(v.Value)
		if spelled == v.Value {
			return true
		}
		e.written[n] = v.Value
		if n == v {
			respelled[n] = v.Value
		}
		e.becomeScalar(n, v, spelled)
		return true
	})
	if len(respelled) == 0 {
		return
	}
	for _, a := range aliasesOutsideRefs(root) {
		if was, ok := respelled[a.Alias]; ok {
			e.becomeScalar(a, a.Alias, was)
		}
	}
}

// becomeScalar makes n the scalar v holding value, keeping n's own position
// and anchor, and records n as it was.
func (e *escapes) becomeScalar(n, v *yaml.Node, value string) {
	e.changed = append(e.changed, changed{node: n, was: *n})
	line, column, anchor := n.Line, n.Column, n.Anchor
	*n = *v
	n.Value, n.Line, n.Column, n.Anchor = value, line, column, anchor
}

// rewritable reports whether the tree under root holds a $ref value escape
// would rewrite.
func rewritable(root *yaml.Node) bool {
	found := false
	eachRefValue(root, func(n *yaml.Node) bool {
		v := nodeview.Deref(n)
		found = fragment.ForResolver(v.Value) != v.Value
		return !found
	})
	return found
}

// eachRefValue calls visit with the node written as the value of each $ref
// key in the tree under root that is a scalar or an alias of one, until visit
// returns false. A value that is no scalar names no reference. The tree is
// walked by its content, never through an alias, so each mapping is read once.
func eachRefValue(root *yaml.Node, visit func(*yaml.Node) bool) {
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		stack = append(stack, n.Content...)
		for i, c := range n.Content {
			v := nodeview.Deref(c)
			if refValue(n, i) && v != nil && v.Kind == yaml.ScalarNode && !visit(c) {
				return
			}
		}
	}
}

// aliasesOutsideRefs returns every alias in the tree under root that is not
// the value of a $ref key, walked as eachRefValue walks.
func aliasesOutsideRefs(root *yaml.Node) []*yaml.Node {
	var out []*yaml.Node
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		stack = append(stack, n.Content...)
		for i, c := range n.Content {
			if c.Kind == yaml.AliasNode && !refValue(n, i) {
				out = append(out, c)
			}
		}
	}
	return out
}

// refValue reports whether the node at index i of n's content is the value of
// a $ref key, read as the parser reads a key.
func refValue(n *yaml.Node, i int) bool {
	if n.Kind != yaml.MappingNode || i%2 == 0 {
		return false
	}
	key, ok := nodeview.KeyOf(n.Content[i-1])
	return ok && key == "$ref"
}

// empty reports whether nothing was rewritten.
func (e *escapes) empty() bool {
	if e == nil {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.written) == 0
}

// original returns the value n was written with, or false when it was not
// rewritten.
func (e *escapes) original(n *yaml.Node) (string, bool) {
	if e == nil {
		return "", false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.written[n]
	return v, ok
}

// writtenOf returns the $ref model carries as the source writes it, for a
// diagnostic to quote. It reads the scalar the model was built from, which the
// resolver and the load phase never retarget, where the model's own value may
// be rewritten or, for a held "#/$defs/..." reference, be its definition's
// pointer. A model built from no scalar, such as one the compiler made, answers
// with its own.
func (e *escapes) writtenOf(model any) references.Reference {
	if node, _, ok := refCopies(model); ok {
		if n := nodeview.Deref(node); n != nil && n.Kind == yaml.ScalarNode {
			if v, escaped := e.original(n); escaped {
				return references.Reference(v)
			}
			return references.Reference(n.Value)
		}
	}
	own, _ := model.(interface{ GetReference() references.Reference })
	if own == nil {
		return ""
	}
	return own.GetReference()
}

// trailOf is c.trail() with the reference the chain stopped at spelled as
// written, since the trail is only ever rendered.
func (e *escapes) trailOf(c chain) trail {
	t := c.trail()
	if t.stopped != "" {
		t.stopped = e.writtenOf(c.stoppedAt)
	}
	return t
}

// restoreTrees puts every node escape changed back as it was, and forgets
// them, so a second call changes nothing.
func (e *escapes) restoreTrees() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.changed {
		*c.node = c.was
	}
	clear(e.written)
	e.changed = nil
}

// restore puts every tree and every model copy of a rewritten value back as
// written, ending the window in which the resolver read the rewritten
// spelling. The models are the ones the lowering reads (see restorer), and a
// fault finding them is returned with where it stopped; the trees are put back
// either way.
func (e *escapes) restore(ctx context.Context, doc *soa.OpenAPI, sibs Siblings, targets MappingTargets,
) (jsontext.Pointer, error) {
	return e.restoreFrom(ctx, walkSource(ctx, doc, sibs), targets)
}

// restoreFault reports the fault restore ended on, at the model it ended at,
// and nothing for no fault. It is no spec problem: the library faulted reading
// its own models.
func restoreFault(at func(jsontext.Pointer) ir.Provenance, site jsontext.Pointer, err error) []ir.Diagnostic {
	if err == nil {
		return nil
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.InternalInvariant, at(site), "internal: %s", err.Error())}
}

// restoreFrom is restore over a walk of the source the caller supplies, so a
// test can hand it one that panics.
func (e *escapes) restoreFrom(ctx context.Context, items iter.Seq[soa.WalkItem], targets MappingTargets,
) (jsontext.Pointer, error) {
	if e.empty() {
		return "", nil // no model holds a rewritten value, so nothing is walked
	}
	defer e.restoreTrees()
	r := &restorer{ctx: ctx, esc: e, seen: map[any]bool{}, walked: map[any]bool{}}
	site, err := eachSighting(items, restoring, func(_ jsontext.Pointer, _ soa.Locations, model any) error {
		r.add(model)
		return nil
	})
	if err != nil {
		return site, err
	}
	return "", r.finish(targets)
}

// restorer puts the model copies of rewritten values back, starting from the
// models the lowering reads and following what each reaches: the chain of
// every reference, and what each object a chain reached holds, since the
// resolver builds those from raw YAML or another document and no walk of the
// source shows them. Each reference is visited once, and each object walked
// once.
type restorer struct {
	ctx    context.Context
	esc    *escapes
	seen   map[any]bool
	walked map[any]bool
	queue  []record
}

// add puts model's reference back as written when it was rewritten, and
// queues its chain, and reports whether the model was new to the restorer.
// Anything but one of the library's reference kinds is ignored.
func (r *restorer) add(model any) bool {
	node, put, ok := refCopies(model)
	if !ok || r.seen[model] {
		return false
	}
	r.seen[model] = true
	if v, rewritten := r.esc.original(nodeview.Deref(node)); rewritten {
		put(v)
	}
	for _, rec := range resolutionChain(model).records {
		if rec.reached {
			r.queue = append(r.queue, rec)
		}
	}
	return true
}

// finish adds the mapping targets and then what everything added reaches, behind
// the barrier the walks have: the library faults on shapes it did not expect.
func (r *restorer) finish(targets MappingTargets) (err error) {
	defer recovered(&err, restoring)
	r.addTargets(targets)
	r.drain()
	return nil
}

// addTargets adds the references and schemas the mapping targets hold, which
// the lowering reads though the source's walk does not show them.
func (r *restorer) addTargets(targets MappingTargets) {
	for _, s := range targets.resolved {
		r.add(s.ref)
	}
	for _, js := range targets.byPointer {
		r.addObject(record{object: js})
	}
	for _, js := range targets.built {
		r.addObject(record{object: js})
	}
}

// drain adds the object of each queued hop and what it holds, until the queue
// is empty.
func (r *restorer) drain() {
	for len(r.queue) > 0 {
		rec := r.queue[0]
		r.queue = r.queue[1:]
		r.addObject(rec)
	}
}

// addObject adds the object rec reached, which need be no reference itself,
// and every model its walk yields.
func (r *restorer) addObject(rec record) {
	if r.walked[rec.object] {
		return
	}
	r.walked[rec.object] = true
	r.add(rec.object)
	eachWithin(r.ctx, rec, func(model any) { r.add(model) })
}

// eachWithin calls visit with each model the walk of rec's object yields. A
// schema is walked as one, as mappings.walk does, since the document's walk
// copies each item's whole location.
func eachWithin(ctx context.Context, rec record, visit func(model any)) {
	if js, ok := rec.object.(*schemaRef); ok {
		for item := range oas3.Walk(ctx, js) {
			// Match returns only what its matcher does, and this one never fails.
			_ = item.Match(oas3.SchemaMatcher{Any: func(model any) error {
				visit(model)
				return nil
			}})
		}
		return
	}
	for item := range rec.walk(ctx) {
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(soa.Matcher{Any: func(model any) error {
			visit(model)
			return nil
		}})
	}
}

// refCopies returns the scalar model's $ref was read from, and a function
// putting a value into the copies of it the model and its core hold. ok is
// false for any other kind, a typed nil, or a model built from no scalar.
//
// put gives each copy a new pointer rather than writing through the old one,
// which a held "#/$defs/..." reference still shares.
func refCopies(model any) (node *yaml.Node, put func(string), ok bool) {
	if v := reflect.ValueOf(model); v.Kind() == reflect.Pointer && v.IsNil() {
		return nil, nil, false
	}
	switch r := model.(type) {
	case *schemaRef:
		s := r.GetSchema()
		if s == nil {
			return nil, nil, false
		}
		return copiesOf(&s.Ref, &s.GetCore().Ref)
	case *soa.ReferencedPathItem:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedParameter:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedHeader:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedRequestBody:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedResponse:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedExample:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedLink:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedCallback:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedSecurityScheme:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	default:
		return nil, nil, false
	}
}

// copiesOf is refCopies over the two copies of a $ref a model keeps: its own,
// and the core's, which records the scalar it came from.
func copiesOf(model **references.Reference, core *marshaller.Node[*string]) (*yaml.Node, func(string), bool) {
	if core.ValueNode == nil {
		return nil, nil, false
	}
	return core.ValueNode, func(v string) {
		ref := references.Reference(v)
		*model = &ref
		core.Value = &v
	}, true
}
