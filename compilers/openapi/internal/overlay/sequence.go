package overlay

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	soaoverlay "github.com/speakeasy-api/openapi/overlay"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
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

// sequence applies an overlay one action at a time, so the node budget is
// checked against each action's cost before it builds anything.
//
// The library applies a whole overlay in one call, and an action copies its
// update into every node its selector matches, paying that cost before anything
// can refuse it (GitHub #491). The count uses the tree as it stands, since an
// earlier action can graft nodes or set values a later selector matches.
//
// The result is unchanged. The library's whole-overlay reporting is rebuilt:
// warning numbering, one RFC 9535 note, and one error joining the selectors
// that matched nothing.
type sequence struct {
	doc  *soaoverlay.Overlay
	root *yaml.Node
	lax  bool

	// size is an upper bound on the tree's node count, against the node
	// budget: exact when taken, then raised by each action's cost, which is
	// what the action can add and no less. It is recounted before a refusal,
	// so an action that merges into keys already there — adding less than it
	// is charged — is never refused on the charge alone.
	size budget.Budget

	warnings []string // per-action warnings, renumbered against the whole overlay
	notes    []string // warnings about the overlay as a whole, each once
	missed   []string // selectors that matched nothing, in action order
}

// runSequence applies doc to root and reports what the library would have
// reported for the whole overlay, or refuses the first action that would build
// past maxNodes.
func runSequence(ctx context.Context, doc *soaoverlay.Overlay, root *yaml.Node, at ir.Provenance, lax bool,
	maxNodes budget.Limit,
) ([]ir.Diagnostic, bool) {
	s := &sequence{doc: doc, root: root, lax: lax, size: budget.New(maxNodes)}
	if s.budgeted() {
		s.measure(ctx)
	}
	for i, action := range doc.Actions {
		if over, ok := s.charge(ctx, action); !ok {
			reported, _ := s.finish(at)
			return append(reported, s.overBudget(at, i, over)), false
		}
		// The library applies the action reading no context, and a charge a
		// cancel cut short may have counted too few nodes, so the cancel is seen
		// before the action is applied. The tree is left half applied, which no
		// caller builds on.
		if ctx.Err() != nil {
			return nil, false
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
func (s *sequence) budgeted() bool {
	_, bounded := s.size.Limit().Max()
	return bounded
}

// measure takes the tree's node count afresh as the running size.
func (s *sequence) measure(ctx context.Context) {
	s.size = budget.New(s.size.Limit())
	s.size.Spend(countNodes(ctx, s.root, s.size.Limit()))
}

// charge adds what action can build to the running size and reports whether it
// still fits, and if not, the size it would reach. An action that builds
// nothing, a remove among them, fits however large the tree: the source may be
// past the budget until an action shrinks it, and the node budget is read of
// the tree the overlay leaves.
func (s *sequence) charge(ctx context.Context, action soaoverlay.Action) (int, bool) {
	if !s.budgeted() {
		return 0, true
	}
	cost := s.cost(action)
	if cost == 0 || s.size.Spend(cost) {
		return 0, true
	}
	// The running size only ever overstates, so take the real count before
	// refusing on it.
	s.measure(ctx)
	if s.size.Spend(cost) {
		return 0, true
	}
	return s.size.Spent(), false
}

// cost is the most nodes action can add to the tree: what it copies, times how
// many nodes its selector matches in the tree as it now stands. It is held at
// the first count past the node budget, since anything past it is refused
// whatever its size.
//
// A remove adds nothing. A selector that does not parse, or a copy whose source
// is not exactly one node, costs nothing here: the library refuses those with
// its own reason when the action runs, and nothing is copied first.
func (s *sequence) cost(action soaoverlay.Action) int {
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
	matches := len(s.query(action.Target))
	if matches == 0 {
		return 0
	}
	limit := s.size.Limit()
	per := newWeigher(limit).weigh(payload, 0)
	if per > limit.Past()/matches {
		return limit.Past() // the product is past the budget; computing it could overflow
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
	for _, w := range slices.Concat(s.warnings, s.notes) {
		out = append(out, diag.Newf(ir.SeverityWarning, diag.OverlayAction, at, "%s", w))
	}
	if len(s.missed) > 0 {
		return append(out, failed(at, errors.New(strictFailurePrefix+strings.Join(s.missed, ",")))), false
	}
	return out, true
}

// overBudget builds the refusal for the action that would have built past the
// budget, before it built anything.
func (s *sequence) overBudget(at ir.Provenance, i int, reached int) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, at,
		"overlay action %d of %d would grow the document to at least %d nodes, past the %d-node budget; nothing it would build was built",
		i+1, len(s.doc.Actions), reached, s.size.Limit().Cap())
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

// countNodes counts the nodes a Content walk from root reaches, no further than
// one past limit: a tree past limit is refused at any size, and a refusal
// already says "at least".
func countNodes(ctx context.Context, root *yaml.Node, limit budget.Limit) int {
	return nodeview.Walk{Nodes: limit.Past()}.Run(ctx, nodeview.DocumentRoot(root)).Visited
}

// weigher counts the nodes the library's clone would build from a node: every
// node beneath it, and through each alias everything the alias names, since the
// clone follows the alias and copies that too.
//
// Each node is weighed once, so an anchor named from many places costs one walk
// of it. A node met again on its own path is a cycle, which the clone would
// follow without end; it weighs the first count past the limit, putting the
// budget out of reach. The loader refuses such an overlay first (GitHub #489);
// this only terminates.
type weigher struct {
	memo   map[*yaml.Node]int
	onPath map[*yaml.Node]bool
	limit  budget.Limit
}

// newWeigher returns a weigher whose answers stop at the first count past
// limit.
func newWeigher(limit budget.Limit) *weigher {
	return &weigher{memo: map[*yaml.Node]int{}, onPath: map[*yaml.Node]bool{}, limit: limit}
}

// weigh returns how many nodes cloning n builds, held at the first count past
// the limit, which no sum of them overflows.
func (w *weigher) weigh(n *yaml.Node, depth int) int {
	if n == nil {
		return 0
	}
	if v, ok := w.memo[n]; ok {
		return v
	}
	if depth > maxWeighDepth || w.onPath[n] {
		return w.limit.Past()
	}
	w.onPath[n] = true
	defer delete(w.onPath, n)

	total := budget.New(w.limit)
	total.Spend(1)
	if n.Kind == yaml.AliasNode {
		total.Spend(w.weigh(n.Alias, depth+1))
	}
	for _, child := range n.Content {
		if total.Exhausted() {
			break
		}
		total.Spend(w.weigh(child, depth+1))
	}
	weight := min(total.Spent(), w.limit.Past())
	w.memo[n] = weight
	return weight
}
