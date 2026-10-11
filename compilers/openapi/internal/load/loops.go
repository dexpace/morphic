package load

import (
	"encoding/json/jsontext"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// maxLoopWork bounds the steps loops takes reading hops in a compile: a step
// for each hop it reads, each token it reads in the model, and its own steps
// reading raw YAML, with each mapping's keys once, when a read first indexes
// them (see loops.readAt). A hop is read once, however many references lead
// through it, so the work follows what the references name, not their chains
// or the widths their reads pass (GitHub #775). The keys are charged after the
// read that indexed them, so the last read can pass the bound by those.
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
	// maxLoopWork; rawReads reads raw YAML for them, and counts that work.
	reads, work int
	rawReads    *treeReads
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
		next: map[hop]references.Reference{}, rawReads: newTreeReads()}
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
		var kind hopKind
		var steps int
		ref, kind, steps = l.readAt(pointer, inTree, maxLoopWork-l.work)
		if l.work += steps; l.work > maxLoopWork {
			settled, cut = false, true
			break
		}
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

// readAt reads the hop naming pointer as the resolver reads it, and returns
// the $ref its target carries and the steps the read took, past limit where it
// stopped short: in the source's tree once a hop has named it by its file
// name, and in its model before, or where the tree holds no node, as the
// resolver may read it there too. Raw YAML is read through treeReads' index,
// which finds what GetTarget finds without scanning each mapping, so n hops
// into one mapping cost n reads and the mapping once.
func (l *loops) readAt(pointer string, inTree bool, limit int) (references.Reference, hopKind, int) {
	steps := 1
	if inTree {
		read, target := l.rawReads.read(l.self.root, pointer, limit-steps)
		steps += read
		if target != nil || steps > limit {
			next, kind := hopTo(l.view, target)
			return next, kind, steps + l.rawReads.drain()
		}
	}
	next, kind, read := l.readModel(pointer, limit-steps)
	return next, kind, steps + read + l.rawReads.drain()
}

// readModel is readAt's read of pointer in the model: a step a token, through
// navigation.Walk, and where the walk leaves the model, the raw YAML it leaves
// for, read through treeReads.
func (l *loops) readModel(pointer string, limit int) (references.Reference, hopKind, int) {
	tokens, ok := navigation.Tokens(jsontext.Pointer(pointer))
	if !ok {
		return "", hopEnds, 0
	}
	at, rest, err := navigation.Walk(l.model, tokens)
	steps := len(tokens) - len(rest)
	if err != nil {
		return "", settledRead(err), steps + 1
	}
	raw, leaves := at.(*yaml.Node)
	if !leaves || len(rest) == 0 {
		next, kind := hopTo(l.view, at)
		return next, kind, steps
	}
	read, target := l.rawReads.read(raw, joinTokens(rest), limit-steps)
	if target == nil {
		return "", hopEnds, steps + read
	}
	next, kind := hopTo(l.view, target)
	return next, kind, steps + read
}
