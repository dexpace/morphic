// Package overlay applies an OpenAPI Overlay document to the parsed node tree,
// and records which positions in the result the overlay is answerable for.
//
// It sits on the entry side beside the loader: it mutates the tree in place and
// knows nothing about lowering. Spec problems leave as ir.Diagnostic values,
// not a Go error, since an overlay can only be wrong as an input document.
//
// Mutating the tree rather than re-serialised bytes keeps each untouched node's
// line and column as parsed, so diagnostics name positions in a file that
// exists. Origin names the positions the overlay introduced.
package overlay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"strconv"

	soaoverlay "github.com/speakeasy-api/openapi/overlay"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// maxNodes bounds the walks around an overlay's application: the "before"
// snapshot, graft repair and attribution; the application spends the caller's
// Options.MaxNodes. It counts nodes visited, not depth, because the walks are
// iterative, and a document this large is pathological.
//
// A walk that exhausts it abandons the whole attempt rather than return a
// partial answer, since a half-taken snapshot would blame the overlay for every
// unvisited node; every position then keeps the source as its origin. The
// exception is substituting grafts: a partly repaired tree is worse than none,
// so running out there refuses the overlay.
const maxNodes = 1 << 21

// Options is one pre-read overlay document and how strictly to apply it.
type Options struct {
	// Path names the overlay document. It is recorded as the overlay's SourceInfo
	// path and never opened.
	Path string
	// Data is the overlay document's bytes. The caller reads them; nothing here
	// performs I/O, so the compiler stays pure (compilers.Source's contract).
	Data []byte
	// Lax turns off strict application.
	//
	// Strict — the zero value — is the default because an action whose selector
	// matches nothing is nearly always a typo in a JSONPath, and an overlay that
	// silently does nothing ships an SDK missing the very fix it was written to
	// make. Under strict, such an action is reported and the compile refuses;
	// under lax it is not reported at all.
	Lax bool
	// MaxNodes bounds the document the overlay may grow the source to, checked
	// before each action builds anything; zero or negative applies without one.
	// It is the compile's node budget: the loader checks the patched tree
	// against the same number afterwards, and this is what stops an overlay
	// spending memory to build a tree that check would refuse (GitHub #491).
	MaxNodes int
}

// Origin answers which input document supplied a lowered position.
//
// The zero value is the answer for a compile with no overlay: nothing was
// applied, and every position belongs to whichever source the caller names.
type Origin struct {
	// index is the overlay's position in Document.Sources.
	index int
	// source is the overlay's identity as an input document.
	source ir.SourceInfo
	// applied reports that the overlay was applied, whether or not it changed
	// anything and whether or not its positions could be attributed to it.
	applied bool
	// pointers holds every position the overlay introduced or rewrote, closed
	// downwards: a cloned subtree contributes each of its own nodes, so a lookup
	// is one map hit rather than a walk up the pointer's prefixes.
	//
	// It is nil when no position is attributed to the overlay: it did not
	// apply, or it applied past the node budget.
	pointers map[jsontext.Pointer]bool
	// nodes holds the same positions keyed by the node that sits at each — the
	// value the walk attributed, and the key beside it when the overlay
	// introduced that too. A diagnostic raised on a raw node has the node and
	// not the pointer; this is what lets it be answered at all, since a grafted
	// node carries no line and column of its own (the library's clone keeps
	// neither) and would otherwise be reported at 0:0 in the source.
	nodes map[*yaml.Node]jsontext.Pointer
}

// Applied reports whether an overlay was applied to the document at all. It
// does not say its positions were attributed: past the node budget the overlay
// still applies and every position keeps the source as its origin, but the
// overlay remains an input the document was built from, and the warning that
// says so names it.
func (o Origin) Applied() bool { return o.applied }

// Source is the overlay's identity as an input document, for Document.Sources.
// It is meaningful only when Applied reports true.
func (o Origin) Source() ir.SourceInfo { return o.source }

// IndexAt returns the index of the source that supplied the position at pointer:
// the overlay's, if the overlay introduced or rewrote it, and fallback
// otherwise.
//
// A pointer the walk never produced — a position reached through an alias or a
// `<<` merge key, which the node tree holds once at the anchor's own position —
// falls back, so an unrecognized pointer under-attributes rather than
// misattributes.
func (o Origin) IndexAt(pointer jsontext.Pointer, fallback int) int {
	if o.pointers[pointer] {
		return o.index
	}
	return fallback
}

// IndexOf is IndexAt for a construct no single position addresses, recorded at
// pointer and assembled from the positions in from: the index of the source that
// supplied every one of them when one source did, and IndexAt's answer for
// pointer otherwise, which is also the answer when from is empty.
//
// A construct the overlay wrote entirely is the overlay's, and one it only added
// to stays with the document that declares it, as a mapping it merged into does
// (GitHub #534).
func (o Origin) IndexOf(pointer jsontext.Pointer, from []jsontext.Pointer, fallback int) int {
	own := o.IndexAt(pointer, fallback)
	if len(from) == 0 {
		return own
	}
	index := o.IndexAt(from[0], fallback)
	for _, p := range from[1:] {
		if o.IndexAt(p, fallback) != index {
			return own
		}
	}
	return index
}

// At returns the provenance of a node the overlay introduced or rewrote — the
// overlay's index and the JSON pointer the node sits at, as IndexAt answers for
// that pointer — and false for any other node, including nil.
//
// It answers a diagnostic anchored on a raw node rather than a lowered
// position, which would otherwise read the node's line and column; a grafted
// node has none, so the finding would name the source at 0:0 (GitHub #476).
func (o Origin) At(n *yaml.Node) (ir.Provenance, bool) {
	pointer, ok := o.nodes[n]
	if !ok {
		return ir.Provenance{}, false
	}
	return ir.Provenance{Source: o.index, Pointer: pointer}, true
}

// Apply applies opts to root in place and returns the attribution of what it
// changed, with index as the overlay's place in Document.Sources.
//
// An error-severity diagnostic means root was not usefully overlaid and the
// caller must refuse to lower: the library applies actions in order and does not
// undo the ones that landed, so a tree left behind by a failed application is
// neither the source nor what the overlay asked for.
func Apply(index int, root *yaml.Node, opts Options) (Origin, []ir.Diagnostic) {
	return applyWithin(index, root, opts, maxNodes)
}

// applyWithin is Apply with the node budget as a parameter.
//
// The budget is the walk's, not the caller's: Apply is the only production entry
// and always spends maxNodes, so nothing outside this package can weaken the
// bound. It is a parameter so the degradation at the bound is reachable from a
// test with a small tree, rather than only from a document large enough that no
// test would build one — an unreachable branch is an unverified claim.
func applyWithin(index int, root *yaml.Node, opts Options, budget int) (Origin, []ir.Diagnostic) {
	at := ir.Provenance{Source: index}

	doc, err := soaoverlay.ParseReader(bytes.NewReader(opts.Data))
	if err != nil {
		return Origin{}, []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.OverlayInvalid, at,
			"cannot parse overlay: %s", diag.OneLine(err))}
	}
	if err := doc.Validate(); err != nil {
		return Origin{}, []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.OverlayInvalid, at,
			"invalid overlay: %s", diag.OneLine(err))}
	}

	before, complete := snapshot(root, budget)
	diags, applied := applyRecovered(doc, root, at, opts.Lax, opts.MaxNodes)
	if !applied {
		return Origin{}, diags
	}
	normalized, safe := repairGrafts(root, budget)
	if !safe {
		return Origin{}, append(diags, diag.Newf(ir.SeverityError, diag.OverlayFailed, at,
			"overlay applied, but the content it grafted expands past %d nodes when its aliases are resolved", budget))
	}
	complete = complete && normalized

	var pointers map[jsontext.Pointer]bool
	var nodes map[*yaml.Node]jsontext.Pointer
	ok := false
	if complete {
		pointers, nodes, ok = attribute(root, before, budget)
	}
	if !ok {
		// Applied without attribution: the source keeps every position, but the
		// overlay stays in Document.Sources, since the warning below names it.
		degraded := Origin{index: index, source: sourceInfo(doc, opts), applied: true}
		return degraded, append(diags, diag.Newf(ir.SeverityWarning, diag.OverlayOriginIncomplete, at,
			"overlay applied, but the document exceeds %d nodes; every position keeps the source as its origin", budget))
	}
	return Origin{index: index, source: sourceInfo(doc, opts), applied: true, pointers: pointers, nodes: nodes}, diags
}

// applyRecovered runs the application under a barrier, converting a panic from
// the library into a refusal so the no-panics-escape invariant holds. The
// library faults on node shapes it accepts elsewhere, such as a document node
// holding no root, and what reaches its selector is not this package's to
// bound. The recover resets the named returns so a half-applied tree is never
// reported as applied.
//
// A recover reaches panics only. The library's clone follows aliases, so a
// recursive anchor exhausts the stack and an alias bomb exhausts memory, both
// fatal; the loader refuses those shapes before the overlay is applied
// (GitHub #489).
func applyRecovered(doc *soaoverlay.Overlay, root *yaml.Node, at ir.Provenance, lax bool, maxNodes int) (diags []ir.Diagnostic, applied bool) {
	defer func() {
		if r := recover(); r != nil {
			diags = []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.OverlayFailed, at,
				"cannot apply overlay: overlay library panicked (%v)", r)}
			applied = false
		}
	}()
	return runSequence(doc, root, at, lax, maxNodes)
}

// failed builds the diagnostic for an overlay the library refused to apply.
func failed(at ir.Provenance, err error) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.OverlayFailed, at, "cannot apply overlay: %s", err)
}

// sourceInfo derives the overlay's identity as an input document. The format tag
// carries the overlay dialect the document declares, so a reader of
// Document.Sources can tell an overlay entry from the spec beside it.
func sourceInfo(doc *soaoverlay.Overlay, opts Options) ir.SourceInfo {
	sum := sha256.Sum256(opts.Data)
	return ir.SourceInfo{
		Format: "overlay@" + doc.Version,
		Path:   opts.Path,
		Hash:   hex.EncodeToString(sum[:]),
	}
}

// repairGrafts replaces every alias the application left pointing outside the
// tree with the content it stands for.
//
// The library's clone copies an alias's target too, leaving an alias to a node
// in no Content list. Readers here walk Content and treat an alias as a leaf,
// so the budget, cycle scan and attribution miss that content while the parser
// follows it (GitHub #477).
//
// normalized reports whether the tree was walked within budget; if not,
// attribution is given up. safe is false when a graft could not be substituted;
// a partly repaired tree is worse than refusing, so the caller refuses.
func repairGrafts(root *yaml.Node, budget int) (normalized, safe bool) {
	content := nodeview.DocumentRoot(root)
	if content == nil {
		return true, true
	}
	reachable, walked := reachableNodes(content, budget)
	if !walked {
		return false, true
	}
	if !substituteGrafts(content, reachable, &budget) {
		return false, false
	}
	return true, true
}

// reachableNodes collects every node a Content walk from root reaches, which is
// every node this compiler's other readings can see.
func reachableNodes(root *yaml.Node, budget int) (map[*yaml.Node]bool, bool) {
	reachable := make(map[*yaml.Node]bool)
	stack := []*yaml.Node{root}
	for ; len(stack) > 0; budget-- {
		if budget == 0 {
			return nil, false
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || reachable[n] {
			continue
		}
		reachable[n] = true
		stack = append(stack, n.Content...)
	}
	return reachable, true
}

// substituteGrafts walks the tree and replaces each child that is an alias to a
// node outside reachable with the content that alias stands for.
//
// A node is replaced in its parent's Content, which is why the walk reads
// children rather than the node it is at: the tree's own root is a mapping the
// application never replaces wholesale, so no alias can sit there.
func substituteGrafts(n *yaml.Node, reachable map[*yaml.Node]bool, budget *int) bool {
	for i, child := range n.Content {
		if *budget <= 0 {
			return false
		}
		*budget--
		if child == nil {
			continue
		}
		if child.Kind == yaml.AliasNode && child.Alias != nil && !reachable[child.Alias] {
			expanded, ok := expandAlias(child.Alias, budget)
			if !ok {
				return false
			}
			n.Content[i] = expanded
			continue
		}
		if !substituteGrafts(child, reachable, budget) {
			return false
		}
	}
	return true
}

// expandAlias returns the content an alias stands for, as a copy holding no
// aliases of its own: one an alias inside it names is substituted too, since it
// would be no more reachable than the one that led here.
//
// The copy drops the anchor it came from. An anchor is a name for a node, and
// the node naming it is not the one being written; leaving the name on a copy
// would spell an anchor twice in one document, which is not a document yaml.v3
// would have produced.
func expandAlias(target *yaml.Node, budget *int) (*yaml.Node, bool) {
	if *budget <= 0 {
		return nil, false
	}
	*budget--

	if target.Kind == yaml.AliasNode {
		if target.Alias == nil {
			return nil, false // an alias naming nothing stands for nothing to graft
		}
		return expandAlias(target.Alias, budget)
	}

	out := *target
	out.Anchor = ""
	out.Content = nil
	if len(target.Content) > 0 {
		out.Content = make([]*yaml.Node, len(target.Content))
		for i, child := range target.Content {
			expanded, ok := expandAlias(child, budget)
			if !ok {
				return nil, false
			}
			out.Content[i] = expanded
		}
	}
	return &out, true
}

// snapshot records every node reachable from root against its scalar value,
// reporting whether it visited the whole tree.
//
// The value is recorded because the library rewrites a scalar in place: an
// overlay that replaces info.title keeps that node's identity and changes only
// what it holds, so identity alone would read the new title as the source's.
//
// It reaches a superset of what attribute reaches: both start at the document
// root, and this one also descends into mapping keys. A superset is required: a
// node attribute reaches that this one missed would read as introduced by the
// overlay.
func snapshot(root *yaml.Node, budget int) (map[*yaml.Node]string, bool) {
	before := make(map[*yaml.Node]string)
	stack := []*yaml.Node{nodeview.DocumentRoot(root)}
	for ; len(stack) > 0; budget-- {
		if budget == 0 {
			return nil, false
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		before[n] = n.Value
		stack = append(stack, n.Content...)
	}
	return before, true
}

// attribute collects every position the overlay is answerable for, as pointers
// and as the nodes sitting at them, reporting whether it visited the whole
// tree.
//
// A node absent from the snapshot is new, and one whose value moved was
// rewritten in place; either came from the overlay. The walk descends past a
// match, closing the set downwards: a grafted subtree is cloned, so its nodes
// are all new.
//
// A key the overlay introduced is recorded under its member's pointer too: its
// line and column are the overlay document's, which a finding on it would read
// as the source's.
func attribute(root *yaml.Node, before map[*yaml.Node]string, budget int) (map[jsontext.Pointer]bool, map[*yaml.Node]jsontext.Pointer, bool) {
	changed := func(n *yaml.Node) bool {
		prior, known := before[n]
		return !known || prior != n.Value
	}

	pointers := map[jsontext.Pointer]bool{}
	nodes := map[*yaml.Node]jsontext.Pointer{}
	stack := []frame{{node: nodeview.DocumentRoot(root)}}
	for ; len(stack) > 0; budget-- {
		if budget == 0 {
			return nil, nil, false
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.node == nil {
			continue
		}
		if changed(f.node) {
			pointers[f.pointer] = true
			nodes[f.node] = f.pointer
		}
		if f.key != nil && changed(f.key) {
			nodes[f.key] = f.pointer
		}
		stack = append(stack, children(f)...)
	}
	return pointers, nodes, true
}

// frame is one node of the attribution walk together with the JSON pointer that
// addresses it and, for a mapping's value, the key it sits under.
type frame struct {
	node    *yaml.Node
	pointer jsontext.Pointer
	key     *yaml.Node
}

// children returns the frames beneath f, addressed the way the compiler
// addresses them: a mapping's values under their escaped keys, a sequence's
// elements under their positions.
//
// Mapping keys are not walked in their own right; each rides on its value's
// frame, since the library appends a new key and its value together. An alias
// node is a leaf, because the content it stands for lives at the anchor's own
// position, which the walk reaches there; repairGrafts has already replaced the
// aliases the library grafted (GitHub #477).
func children(f frame) []frame {
	switch f.node.Kind {
	case yaml.MappingNode:
		out := make([]frame, 0, len(f.node.Content)/2)
		for i := 0; i+1 < len(f.node.Content); i += 2 {
			key := f.node.Content[i]
			out = append(out, frame{
				node:    f.node.Content[i+1],
				pointer: f.pointer + ids.Ptr(key.Value),
				key:     key,
			})
		}
		return out
	case yaml.SequenceNode:
		out := make([]frame, 0, len(f.node.Content))
		for i, child := range f.node.Content {
			out = append(out, frame{node: child, pointer: f.pointer + ids.Ptr(strconv.Itoa(i))})
		}
		return out
	default:
		return nil
	}
}
