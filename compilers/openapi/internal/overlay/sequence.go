package overlay

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	soaoverlay "github.com/speakeasy-api/openapi/overlay"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// strictFailurePrefix is how the library opens the error it returns for an
// overlay whose selectors matched nothing, before the messages it joins. It is
// read back off each action's error so the messages can be joined across
// actions the way the library joins them within one overlay.
const strictFailurePrefix = "error applying overlay (strict): "

// maxWeighDepth bounds how deep weigh descends, per the bounded-recursion rule.
// yaml.v3 refuses to parse nesting past 10,000, and an alias adds one level per
// hop, so no document this compiler is handed comes near it; a weigh that
// reaches it answers "past any budget" rather than descending further.
const maxWeighDepth = 1 << 14

// sequence applies an overlay one action at a time, checking before each that
// what the action is about to build fits the budget.
//
// The library applies a whole overlay in one call, and an action copies its
// update into every node its selector matches: the cost is the update's size
// times the number of matches, paid in full before anything can refuse it. A
// 2,000-node update over 1,000 path items spent 1 GB building a tree the node
// budget then refused (GitHub #491).
//
// The count has to be taken on the tree as it stands when the action runs, not
// on the source. Actions apply in order, so an earlier one can graft the very
// nodes a later selector matches, or set the value a later filter selects on
// across nodes that were there all along; a count taken up front misses both,
// by as much as the budget squared. Applying one action at a time is what makes
// the count exact, and it changes nothing about the result: each action is
// still the library's, applied to the same tree in the same order, and the one
// step the library runs once at the end — restyling folded scalars — changes a
// scalar's style and never its value, which is all a selector reads.
//
// What does change is where the reporting comes from. The library numbers each
// action's warnings against the whole overlay, says once that a filter needs
// RFC 9535, and joins every selector that matched nothing into one error; run
// action by action it would number each as the only one, say the filter note
// each time, and fail per action. The sequence puts those back as the library
// writes them — which the differential test holds it to, against the library's
// own whole-overlay application.
type sequence struct {
	doc      *soaoverlay.Overlay
	root     *yaml.Node
	lax      bool
	maxNodes int

	// size is an upper bound on the tree's node count: exact when taken, then
	// raised by each action's cost, which is what the action can add and no
	// less. It is recounted before a refusal, so an action that merges into keys
	// already there — adding less than it is charged — is never refused on the
	// charge alone.
	size int64

	warnings []string // per-action warnings, renumbered against the whole overlay
	notes    []string // warnings about the overlay as a whole, each once
	missed   []string // selectors that matched nothing, in action order
}

// runSequence applies doc to root and reports what the library would have
// reported for the whole overlay, or refuses the first action that would build
// past maxNodes. A zero or negative maxNodes applies with no budget.
func runSequence(doc *soaoverlay.Overlay, root *yaml.Node, at ir.Provenance, lax bool, maxNodes int) ([]ir.Diagnostic, bool) {
	s := &sequence{doc: doc, root: root, lax: lax, maxNodes: maxNodes}
	if s.budgeted() {
		s.size = countNodes(root)
	}
	for i, action := range doc.Actions {
		if over, ok := s.charge(action); !ok {
			reported, _ := s.finish(at)
			return append(reported, s.overBudget(at, i, over)), false
		}
		if err := s.step(i, action); err != nil {
			// A hard error ends the library's own loop with nothing else
			// reported: warnings gathered so far are dropped with it.
			return []ir.Diagnostic{failed(at, err)}, false
		}
	}
	return s.finish(at)
}

// budgeted reports whether the application has a budget to keep to.
func (s *sequence) budgeted() bool { return s.maxNodes > 0 }

// charge adds what action can build to the running size and reports whether it
// still fits, and if not, the size it would reach.
func (s *sequence) charge(action soaoverlay.Action) (int64, bool) {
	if !s.budgeted() {
		return 0, true
	}
	limit := int64(s.maxNodes)
	cost := s.cost(action, limit)
	if s.size+cost > limit {
		// The running size only ever overstates, so take the real count before
		// refusing on it.
		s.size = countNodes(s.root)
		if s.size+cost > limit {
			return s.size + cost, false
		}
	}
	s.size += cost
	return 0, true
}

// cost is the most nodes action can add to the tree: what it copies, times how
// many nodes its selector matches in the tree as it now stands. The limit caps
// the arithmetic, since anything past it is refused whatever its size.
//
// A remove adds nothing. A selector that does not parse, or a copy whose source
// is not exactly one node, costs nothing here: the library refuses those with
// its own reason when the action runs, and nothing is copied first.
func (s *sequence) cost(action soaoverlay.Action, limit int64) int64 {
	var payload *yaml.Node
	switch {
	case action.Remove:
		return 0
	case !action.Update.IsZero():
		payload = &action.Update
	case action.Copy != "":
		sources := s.query(action.Copy)
		if len(sources) != 1 {
			return 0
		}
		payload = sources[0]
	default:
		return 0
	}
	matches := int64(len(s.query(action.Target)))
	if matches == 0 {
		return 0
	}
	per := newWeigher(limit).weigh(payload, 0)
	if per > (limit+1)/matches {
		return limit + 1 // the product is past the budget; computing it could overflow
	}
	return per * matches
}

// query runs a selector the way the library will when the action applies —
// through the overlay's own path constructor, so the JSONPath dialect it picks
// is the one the library picks.
func (s *sequence) query(selector string) []*yaml.Node {
	path, err := s.doc.NewPath(selector, nil)
	if err != nil {
		return nil
	}
	return path.Query(s.root)
}

// step applies one action through the library and files what it reported.
// It returns an error only for a hard failure, the kind that ends the library's
// own loop; a selector that matched nothing is filed and the sequence goes on,
// as the library's does.
func (s *sequence) step(i int, action soaoverlay.Action) error {
	one := *s.doc
	one.Actions = []soaoverlay.Action{action}
	if s.lax {
		return one.ApplyTo(s.root)
	}

	warnings, err := one.ApplyToStrict(s.root)
	if err != nil && warnings == nil {
		return err
	}
	s.file(i, action, warnings)
	if err != nil {
		// Were the library to stop opening this error with its prefix, the whole
		// message would be kept rather than lost, and the compile would still
		// refuse on it; the differential test is what notices the change.
		s.missed = append(s.missed, strings.TrimPrefix(err.Error(), strictFailurePrefix))
	}
	return nil
}

// file sorts one action's warnings into the ones about the action, renumbered
// against the whole overlay, and the ones about the overlay, kept once.
func (s *sequence) file(i int, action soaoverlay.Action, warnings []string) {
	alone := fmt.Sprintf("%s action (1 / 1) target=%s: ", actionType(action), action.Target)
	placed := fmt.Sprintf("%s action (%d / %d) target=%s: ", actionType(action), i+1, len(s.doc.Actions), action.Target)
	for _, w := range warnings {
		if rest, ok := strings.CutPrefix(w, alone); ok {
			s.warnings = append(s.warnings, placed+rest)
			continue
		}
		if !slices.Contains(s.notes, w) {
			s.notes = append(s.notes, w)
		}
	}
}

// finish reports what the library reports at the end of an overlay: every
// action's warnings, then the notes about the overlay, then — if any selector
// matched nothing — the one error joining them all.
func (s *sequence) finish(at ir.Provenance) ([]ir.Diagnostic, bool) {
	var out []ir.Diagnostic
	for _, w := range append(append([]string(nil), s.warnings...), s.notes...) {
		out = append(out, diag.Newf(ir.SeverityWarning, diag.OverlayAction, at, "%s", w))
	}
	if len(s.missed) > 0 {
		return append(out, failed(at, errors.New(strictFailurePrefix+strings.Join(s.missed, ",")))), false
	}
	return out, true
}

// overBudget builds the refusal for the action that would have built past the
// budget, before it built anything.
func (s *sequence) overBudget(at ir.Provenance, i int, reached int64) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, at,
		"overlay action %d of %d would grow the document to at least %d nodes, past the %d-node budget; nothing it would build was built",
		i+1, len(s.doc.Actions), reached, s.maxNodes)
}

// actionType names an action the way the library names it in its warnings,
// which is also the precedence it applies them by: remove, then update, then
// copy.
func actionType(action soaoverlay.Action) string {
	switch {
	case action.Remove:
		return "remove"
	case !action.Update.IsZero():
		return "update"
	case action.Copy != "":
		return "copy"
	default:
		return "unknown"
	}
}

// countNodes counts the nodes a Content walk from root reaches.
func countNodes(root *yaml.Node) int64 {
	var n int64
	stack := []*yaml.Node{nodeview.DocumentRoot(root)}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node == nil {
			continue
		}
		n++
		stack = append(stack, node.Content...)
	}
	return n
}

// weigher counts the nodes the library's clone would build from a node: every
// node beneath it, and through each alias everything the alias names, since the
// clone follows the alias and copies that too.
//
// Each node is weighed once, so an anchor named from many places costs one walk
// of it rather than one per name. A node met again on its own path is a cycle,
// which the clone would follow without end; it weighs the ceiling, which puts
// any budget out of reach. The loader refuses such an overlay before it gets
// here (GitHub #489) — this only has to terminate.
type weigher struct {
	memo    map[*yaml.Node]int64
	onPath  map[*yaml.Node]bool
	ceiling int64
}

// newWeigher returns a weigher whose answers stop at limit+1, the first count
// past the budget.
func newWeigher(limit int64) *weigher {
	return &weigher{memo: map[*yaml.Node]int64{}, onPath: map[*yaml.Node]bool{}, ceiling: limit + 1}
}

// weigh returns how many nodes cloning n builds, capped at the ceiling.
func (w *weigher) weigh(n *yaml.Node, depth int) int64 {
	if n == nil {
		return 0
	}
	if v, ok := w.memo[n]; ok {
		return v
	}
	if depth > maxWeighDepth || w.onPath[n] {
		return w.ceiling
	}
	w.onPath[n] = true
	defer delete(w.onPath, n)

	total := int64(1)
	if n.Kind == yaml.AliasNode {
		total = w.add(total, w.weigh(n.Alias, depth+1))
	}
	for _, child := range n.Content {
		if total >= w.ceiling {
			break
		}
		total = w.add(total, w.weigh(child, depth+1))
	}
	w.memo[n] = total
	return total
}

// add sums two weights without passing the ceiling, so no budget a caller can
// set makes the arithmetic overflow.
func (w *weigher) add(a, b int64) int64 {
	if b >= w.ceiling-a {
		return w.ceiling
	}
	return a + b
}
