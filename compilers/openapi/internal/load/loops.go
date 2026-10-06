package load

import (
	"encoding/json/jsontext"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// maxLoopWork bounds the steps loops takes reading hops in a compile: a step
// for each hop it reads, and the steps of the library's read of it (see
// loops.priced). A hop is read once, however many references lead through it,
// so the bound counts the positions the source's references name, priced by
// the widths their reads scan, not their chains (GitHub #775).
const maxLoopWork = 1 << 28

// loops finds the schema references in the source whose chain never ends
// (GitHub #768). The resolver follows a hop it resolved before without
// tracking where the chain has been, so a chain closing on such hops recurses
// until the stack runs out, which nothing recovers from. The pre-parse scan
// reads a $ref that names the source by its file name as a reference to
// another document, so it never sees such a chain close.
//
// Each hop is read as the resolver reads it (see readAt) and settled once, so
// the references of a long chain cost the chain once between them.
type loops struct {
	self  sourceDocument
	model *soa.OpenAPI
	view  *nodeview.View
	// base is what a relative document part is read against: the source's
	// path, which a schema $ref resolves against whatever $self says.
	base string
	// known holds, for each hop read, whether it leads into a loop, and next
	// the $ref each hop read followed to, which a later chain through it takes
	// unread: only a hop whose read is pending (see settledRead) is read again.
	known map[hop]bool
	next  map[hop]references.Reference
	// reads counts the hops read, and work the steps they took against
	// maxLoopWork; costs prices each read.
	reads, work int
	costs       *treeReads
}

// hop is a position a chain reaches, and whether it is read in the source's
// tree or its model, so one pointer is two positions: a hop naming the source
// by its file name reads the tree (see loops.readAt, mappings.hopOf).
type hop struct {
	pointer string
	inTree  bool
}

// newLoops returns the reader of the chains in the source self, whose model is
// model. The library rebases a reference by $self only for the kinds that are
// not schemas, and loops reads schema chains alone, so its base is the path.
func newLoops(self sourceDocument, model *soa.OpenAPI) *loops {
	return &loops{self: self, model: model, view: nodeview.New(), base: self.path, known: map[hop]bool{},
		next: map[hop]references.Reference{}, costs: newTreeReads()}
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
// source with no tree or model are no answer, and report false. A hop into
// another document is one, so a cycle entered from there is not refused, and a
// second $ref entering it runs the stack out (GitHub #771).
func (l *loops) into(ref references.Reference) bool {
	looped, _ := l.read(ref)
	return looped
}

// read is into, and whether the bound cut the read short of an answer.
func (l *loops) read(ref references.Reference) (looped, cut bool) {
	if l.self.root == nil || l.model == nil {
		return false, false
	}
	var path []hop
	onPath := map[hop]bool{}
	settled := true
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
		onPath[h] = true
		path = append(path, h)
		if next, read := l.next[h]; read {
			ref = next
			continue
		}
		l.reads++
		if l.work += l.priced(pointer, inTree); l.work > maxLoopWork {
			settled, cut = false, true
			break
		}
		var kind hopKind
		ref, kind = l.readAt(pointer, inTree)
		settled = settled && kind != hopPending
		if kind != hopFollows {
			break
		}
		l.next[h] = ref
	}
	if settled {
		for _, h := range path {
			l.known[h] = looped
		}
	}
	return looped, cut
}

// held reports whether the source is held under its file name, where a chain
// through that name reaches what the resolver has resolved already.
func (l *loops) held() bool {
	return len(l.self.keys()) > 0
}

// forget drops every verdict the reads settled, but not the hops they followed.
// A held "#/$defs/..." reference has no $ref while the walk reads, and names
// its definition once retargeted, so a chain read through it may go on now.
func (l *loops) forget() {
	if l != nil {
		clear(l.known)
	}
}

// incomplete reports the bound the reads stopped at, once, at the document, as
// the cycle scans report theirs: the compile goes on, and says its protection
// against a reference cycle is incomplete.
func (l *loops) incomplete(at func(jsontext.Pointer) ir.Provenance) []ir.Diagnostic {
	if l == nil || l.work <= maxLoopWork {
		return nil
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.CycleScanFailed, at(""),
		"reference-chain read stopped at its %d-step bound; reference-cycle protection is incomplete for this source",
		maxLoopWork)}
}

// readAt reads the hop naming pointer: in the source's tree once a hop has
// named it by its file name, and in its model before. A pointer the tree holds
// no node at is read in the model too, as the resolver may: it answers one it
// resolved in the model from its cache, and the model reads through a
// reference where the tree stops at the $ref. The tree is read once, for both
// the node and whether there is one.
func (l *loops) readAt(pointer string, inTree bool) (references.Reference, hopKind) {
	if inTree {
		if target, err := readTarget(l.self.root, pointer); err == nil {
			return hopTo(l.view, target)
		}
	}
	return readHop(l.view, l.model, pointer)
}

// priced returns the steps readAt takes on the hop naming pointer: a step, the
// library's read of the tree, and of the model where it reads that too, with
// the keys indexed to count them.
func (l *loops) priced(pointer string, inTree bool) int {
	steps := 1
	var inTreeAt *yaml.Node
	if inTree {
		read, target := l.costs.cost(l.self.root, pointer, maxLoopWork-l.work)
		steps, inTreeAt = steps+read, target
	}
	if inTreeAt == nil {
		steps += l.costs.modelCost(l.model, jsontext.Pointer(pointer), maxLoopWork-l.work)
	}
	return steps + l.costs.drain()
}
