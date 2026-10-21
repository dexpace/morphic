package merge

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/ir"
)

// declaration is one redeclaration of a property, as MergeProperty received it.
type declaration struct {
	prop   ir.Property
	source Source
}

// deferredFold is the redeclarations of one property whose judgment found a
// type the registry did not hold yet. A $ref names its component's ID without
// lowering it, so a component declared after the schema composing it is no node
// when the walk reaches the redeclaration, and judging then would follow
// declaration order (docs/compilers/openapi.md#allof-redeclared-property).
type deferredFold struct {
	m     *ir.Model
	e     *indexed
	decls []declaration
}

// foldOrDefer folds src into the property e indexes, or queues it where the
// decision reads a type the registry does not hold yet. A property with a
// redeclaration queued queues every later one too, so no fold compares against
// a property an earlier fold has not produced.
func foldOrDefer(g *Merger, m *ir.Model, e *indexed, src ir.Property, source Source) {
	dst := &m.Properties[e.pos]
	if e.pending == nil && (g.Defer == nil || !misses(g, dst, &e.origins, src)) {
		g.reconcileProperty(dst, &e.origins, src, source)
		return
	}
	if e.pending == nil {
		e.pending = &deferredFold{m: m, e: e}
		g.Defer(e.pending.settle)
	}
	e.pending.decls = append(e.pending.decls, declaration{prop: src, source: source})
}

// settle replays the redeclarations in order against the registry the walk
// left. It does nothing when m is no longer the node registered under its ID:
// a $ref reached it first and its declaration built it again, and the
// settlement that build queued holds the node now registered.
func (d *deferredFold) settle(reg ir.TypeRegistry, report Reporter) {
	if reg[d.m.ID] != ir.TypeDef(d.m) {
		return
	}
	g := Merger{
		Resolve: func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok },
		Report:  report,
	}
	for _, at := range d.decls {
		g.reconcileProperty(&d.m.Properties[d.e.pos], &d.e.origins, at.prop, at.source)
	}
}

// misses reports whether judging src against dst reads a type the registry does
// not hold. It takes the decision on copies, so it changes neither property and
// reports nothing, and every lookup the decision makes is counted: the
// judgment, the drop's classification and the defaults and examples it holds
// the merged type to.
func misses(g *Merger, dst *ir.Property, org *origins, src ir.Property) bool {
	missed := false
	trial := Merger{
		Resolve: func(id ir.TypeID) (ir.TypeDef, bool) {
			td, ok := g.Resolve(id)
			missed = missed || !ok
			return td, ok
		},
		Report: func(ir.Severity, string, jsontext.Pointer, string, ...any) {},
	}
	merged := *dst
	dropped, _, narrowed := trial.recordRedeclarationConflict(&merged, &src)
	trial.judgeAdopted(&merged, &src, narrowed, dropped, org)
	return missed
}
