package load

import (
	"encoding/json/jsontext"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// maxLoopReads bounds the hops loops reads in a compile. A hop is read once,
// however many references lead through it, so it is the number of positions
// the source's references name that the bound counts, not their chains.
const maxLoopReads = 1 << 18

// loops finds the schema references in the source whose chain never ends
// (GitHub #768). The resolver follows a hop it resolved before without
// tracking where the chain has been, so a chain closing on such hops recurses
// until the stack runs out, which nothing recovers from. The pre-parse scan
// reads a $ref that names the source by its file name as a reference to
// another document, so it never sees such a chain close.
//
// Each hop is read as the resolver reads it (see readHop) and settled once, so
// the references of a long chain cost the chain once between them.
type loops struct {
	self  sourceDocument
	model *soa.OpenAPI
	view  *nodeview.View
	// base is what a relative document part is read against.
	base string
	// known holds, for each hop read, whether it leads into a loop.
	known map[hop]bool
	// reads counts the hops read, against maxLoopReads.
	reads int
}

// hop is a position a chain reaches, and whether it read the source's tree or
// its model: a hop naming the source by its file name reads the tree from there
// on, so one pointer is two positions.
type hop struct {
	pointer string
	inTree  bool
}

// newLoops returns the reader of the chains in the source self, whose model is
// model.
func newLoops(self sourceDocument, model *soa.OpenAPI) *loops {
	base := model.GetSelf()
	if base == "" {
		base = self.path
	}
	return &loops{self: self, model: model, view: nodeview.New(), base: base, known: map[hop]bool{}}
}

// within returns the pointer ref names in the source, as the resolver reads it:
// a reference with no document part, or whose part opens the source (see
// external.Open) however it is spelled, which the lowering reads only as the
// source's own file name (see sourcePointer). A reference the resolver may read
// otherwise is no answer: one naming an anchor, a definition, or another
// document.
func (l *loops) within(ref references.Reference) (string, bool) {
	pointer := string(ref.GetJSONPointer())
	if !strings.HasPrefix(pointer, "/") || defs.IsPointer(jsontext.Pointer(pointer)) {
		return "", false
	}
	if ref.GetURI() == "" {
		return pointer, true
	}
	abs, err := references.ResolveAbsoluteReference(ref, l.base)
	if err != nil || !l.self.names(abs.AbsoluteReference) {
		return "", false
	}
	return pointer, true
}

// into reports whether the chain of ref, a schema $ref written in the model,
// provably never ends: some hop of it reached a position an earlier hop did. A
// hop the walk cannot read as the resolver does, one past the bound, and a
// source with no tree or model are no answer, and report false.
func (l *loops) into(ref references.Reference) bool {
	if l.self.root == nil || l.model == nil {
		return false
	}
	var path []hop
	onPath := map[hop]bool{}
	looped, settled := false, true
	inTree := false
	for {
		pointer, ok := l.within(ref)
		if !ok {
			break
		}
		inTree = inTree || ref.GetURI() != ""
		h := hop{pointer: pointer, inTree: inTree}
		if known, seen := l.known[h]; seen {
			looped = known
			break
		}
		if onPath[h] {
			looped = true
			break
		}
		if l.reads++; l.reads > maxLoopReads {
			settled = false
			break
		}
		onPath[h] = true
		path = append(path, h)
		var kind hopKind
		ref, kind = readHop(l.view, l.document(inTree), pointer)
		if kind != hopFollows {
			break
		}
	}
	if settled {
		for _, h := range path {
			l.known[h] = looped
		}
	}
	return looped
}

// document returns what a hop reads: the source's tree once a hop has named it
// by its file name, and its model before.
func (l *loops) document(inTree bool) any {
	if inTree {
		return l.self.root
	}
	return l.model
}
