package load

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"iter"
	"reflect"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/ir"
)

// refSite is where a reference object is written: a pointer of the source,
// whose doc is empty, or of another document, named as bind names it
// (sourceDocument.nameOf) and keyed as a request reads a URL (readKey), so
// that one the resolver and the binder spell apart is one document.
type refSite struct {
	doc     string
	pointer jsontext.Pointer
}

// inSource reports whether s is a position of the source.
func (s refSite) inSource() bool { return s.doc == "" }

// compareSites orders sites as a finding's placement is chosen by: the
// source's first, another document's by its name, then by pointer, so that
// which of several sites a finding lands at follows no declaration order.
func compareSites(a, b refSite) int {
	if a.inSource() != b.inSource() {
		if a.inSource() {
			return -1
		}
		return 1
	}
	if c := cmp.Compare(a.doc, b.doc); c != 0 {
		return c
	}
	return cmp.Compare(a.pointer, b.pointer)
}

// sitePlacer returns where a finding at a site is placed: at's Provenance for
// the source's, and for another document's its own Document.Sources entry. One
// no entry holds, which a document read for its content always has, is placed
// at the source's root rather than nowhere.
func sitePlacer(at func(jsontext.Pointer) ir.Provenance, others Others) func(refSite) ir.Provenance {
	return func(s refSite) ir.Provenance {
		if s.inSource() {
			return at(s.pointer)
		}
		o, ok := others.Named(s.doc)
		if !ok {
			return at("")
		}
		return ir.Provenance{Source: o.Index, Pointer: s.pointer}
	}
}

// reachedEnd is an object a resolution ended on in another document, whose
// reference objects no walk of the source meets (GitHub #751): where it is, a
// walk over it, and how a reference written in it is resolved, against that
// document as the resolver resolves a hop of a chain there.
type reachedEnd struct {
	at   refSite
	walk func(context.Context) iter.Seq[soa.WalkItem]
	opts references.ResolveOptions
}

// elsewhere is the work of resolving the reference objects in what the
// source's resolutions reached in other documents, and in what those reached.
//
// The library builds an object afresh for each resolution landing in a
// document it holds the bytes of, so one object is kept per node and kind
// (canonical) and handed to each later resolution landing there. Each is
// walked once, which bounds the work by what the admitted documents hold. An
// object a source reference reached is walked even when another was kept for
// its node, since the lowering reads it as it was reached.
type elsewhere struct {
	canonical map[endKey]any
	walked    map[any]bool
	queue     []reachedEnd
}

// endKey identifies an object a resolution ended on by the node it was built
// from and the kind of model built: a node one $ref reads as a response and
// another as a parameter is two objects.
type endKey struct {
	node *yaml.Node
	kind reflect.Type
}

// newElsewhere returns the work, holding nothing.
func newElsewhere() *elsewhere {
	return &elsewhere{canonical: map[endKey]any{}, walked: map[any]bool{}}
}

// reach notes the end of c, the chain of the reference at site resolved under
// opts, and queues it to be walked: unless it lies in the source, whose own
// walk meets its reference objects, or c ended on no object. A reference
// written outside the source is handed the object kept for its end, where one
// is (see elsewhere).
func (w *elsewhere) reach(site refSite, c chain, opts references.ResolveOptions, nameOf func(string) string) {
	if c.cut || c.stopped != "" || len(c.records) == 0 {
		return
	}
	last := c.records[len(c.records)-1]
	if last.inSource() || last.node == nil {
		return
	}
	key := endKey{node: last.node, kind: reflect.TypeOf(last.object)}
	if kept, ok := w.canonical[key]; ok && !site.inSource() && last.replace(kept) {
		return
	}
	if _, ok := w.canonical[key]; !ok {
		w.canonical[key] = last.object
	}
	if w.walked[last.object] {
		return
	}
	w.walked[last.object] = true
	opts.TargetLocation, opts.TargetDocument = *last.documentPath, *last.document
	w.queue = append(w.queue, reachedEnd{at: refSite{doc: readKey(nameOf(last.path)), pointer: last.pointer},
		walk: last.walk, opts: opts})
}

// next returns the next end queued, and false once none is.
func (w *elsewhere) next() (reachedEnd, bool) {
	if len(w.queue) == 0 {
		return reachedEnd{}, false
	}
	e := w.queue[0]
	w.queue = w.queue[1:]
	return e, true
}

// eachReferenceIn calls visit with each reference object the walk of e
// reaches, and the site writing it, in e's document. The walk starts at e's
// object, so each location is spelled from it (typed.PointerOf) and joined to
// e's pointer. A panic stops the walk, and is returned as an error, to be
// reported at e's object: the library spells no location of a walk started
// there that names its position.
func eachReferenceIn(ctx context.Context, e reachedEnd, visit func(refSite, resolvable)) error {
	_, err := eachSighting(e.walk(ctx), resolverPanics, func(_ jsontext.Pointer, loc soa.Locations, r resolvable) error {
		if r.IsReference() {
			visit(refSite{doc: e.at.doc, pointer: e.at.pointer + typed.PointerOf(loc)}, r)
		}
		return nil
	})
	return err
}
