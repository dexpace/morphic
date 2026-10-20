package irfacts

import (
	"sync"

	"github.com/dexpace/morphic/ir"
)

// Index holds the facts of one [ir.Document]. Each of its indexes is built in
// full the first time one of its facts is asked for, and never written again.
//
// An Index never writes its document. Its answers do not follow later changes,
// so a caller that changes the document builds a new Index. It is safe for
// concurrent use. Its zero value indexes no document.
type Index struct {
	doc *ir.Document

	hierarchyOnce  sync.Once
	hierarchy      *hierarchy
	graphOnce      sync.Once
	graph          *typeGraph
	usageOnce      sync.Once
	usage          map[ir.TypeID]UsageFlags
	componentsOnce sync.Once
	components     *components
}

// New returns the index of doc's facts. It builds nothing: each index is built
// when one of its facts is first asked for. A nil doc is allowed and has no
// facts.
func New(doc *ir.Document) *Index {
	return &Index{doc: doc}
}

// IsSubtype answers ir.IsSubtype for the index's document. The first question
// indexes the whole hierarchy. After that a question is a few comparisons,
// unless one of base's subtypes has a supertype that is neither base nor below
// it, as only a model naming two supertypes can bring about; then a pair the
// comparisons leave open is walked as ir.IsSubtype walks it.
func (x *Index) IsSubtype(target, base ir.TypeID) bool {
	if sub, settled := x.hierarchyIndex().subtype(target, base); settled {
		return sub
	}
	return ir.IsSubtype(x.doc, target, base)
}

// Subtypes returns every type of the document that [Index.IsSubtype] holds a
// subtype of base, in sorted order, as a slice the caller owns. It is nil when
// there are none.
func (x *Index) Subtypes(base ir.TypeID) []ir.TypeID {
	return x.hierarchyIndex().descendants(base)
}

// registry returns the document's registry, nil when there is no document.
func (x *Index) registry() ir.TypeRegistry {
	if x.doc == nil {
		return nil
	}
	return x.doc.Types
}

// hierarchyIndex returns the subtype index, built once.
func (x *Index) hierarchyIndex() *hierarchy {
	x.hierarchyOnce.Do(func() { x.hierarchy = newHierarchy(x.registry()) })
	return x.hierarchy
}

// typeGraph returns the type graph, built once.
func (x *Index) typeGraph() *typeGraph {
	x.graphOnce.Do(func() { x.graph = newTypeGraph(x.registry(), x.hierarchyIndex()) })
	return x.graph
}

// usageIndex returns the usage of every type, derived once.
func (x *Index) usageIndex() map[ir.TypeID]UsageFlags {
	x.usageOnce.Do(func() { x.usage = newUsage(x.doc, x.typeGraph()) })
	return x.usage
}

// componentIndex returns the components of the type graph, built once.
func (x *Index) componentIndex() *components {
	x.componentsOnce.Do(func() { x.components = newComponents(x.typeGraph()) })
	return x.components
}
