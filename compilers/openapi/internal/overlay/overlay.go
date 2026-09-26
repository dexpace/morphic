// Package overlay applies an OpenAPI Overlay document to the parsed node tree,
// and records which positions in the result the overlay is answerable for.
//
// It sits on the entry side beside the loader: it reads bytes the caller has
// already read, mutates the node tree in place, and knows nothing about
// lowering. Spec problems in the overlay leave as ir.Diagnostic values; there is
// no Go error return, because every way an overlay can be wrong is a problem
// with an input document rather than with the program.
//
// Applying to the node tree rather than to re-serialised bytes is the point.
// Round-tripping the document through a marshaller renumbers every line in it,
// so every diagnostic about the source would name a position in a file nobody
// has; mutating the tree in place leaves each untouched node's line and column
// exactly as the parser read them, and confines the loss to the positions the
// overlay actually introduced — which is what Origin then names.
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

// maxNodes bounds each of the two attribution walks, per the bounded-everything
// rule. It is a budget on nodes visited rather than a depth cap because the
// walks are iterative: what has to be bounded is the total work, and a document
// with this many nodes is pathological rather than large.
//
// Exhausting it costs attribution, never correctness. Both walks abandon the
// whole attempt rather than return a partial answer — a half-taken "before"
// snapshot would read every unvisited node as freshly introduced and blame the
// overlay for the entire document — so the fallback is that every position keeps
// the source as its origin, which is what a compile with no overlay reports.
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
	// pointers holds every position the overlay introduced or rewrote, closed
	// downwards: a cloned subtree contributes each of its own nodes, so a lookup
	// is one map hit rather than a walk up the pointer's prefixes.
	//
	// Its nil-ness is what Applied reports, so a successful application of an
	// overlay that changed nothing still yields a non-nil empty map.
	pointers map[jsontext.Pointer]bool
	// nodes holds the same positions keyed by the node that sits at each — the
	// value the walk attributed, and the key beside it when the overlay
	// introduced that too. A diagnostic raised on a raw node has the node and
	// not the pointer; this is what lets it be answered at all, since a grafted
	// node carries no line and column of its own (the library's clone keeps
	// neither) and would otherwise be reported at 0:0 in the source.
	nodes map[*yaml.Node]jsontext.Pointer
}

// Applied reports whether an overlay was applied to the document at all.
func (o Origin) Applied() bool { return o.pointers != nil }

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
// overlay's index and the JSON pointer of the position the node sits at, the
// same answer IndexAt gives the lowering for that pointer — and false for any
// other node, including nil.
//
// It is the answer for a diagnostic anchored on a raw node rather than on a
// lowered position. Such a diagnostic would otherwise read the node's line and
// column, and a grafted node has none: the library's clone copies neither, so
// the finding would name the source at 0:0 (GitHub #476). A node reached only
// through a grafted alias is not answered, for the reason IndexAt gives — the
// clone points the alias at a detached copy of its target that no walk over
// the tree reaches (GitHub #477).
func (o Origin) At(n *yaml.Node) (ir.Provenance, bool) {
	pointer, ok := o.nodes[n]
	if !ok {
		return ir.Provenance{}, false
	}
	return ir.Provenance{Source: o.index, Pointer: string(pointer)}, true
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
		return Origin{}, append(diags, diag.Newf(ir.SeverityWarning, diag.OverlayOriginIncomplete, at,
			"overlay applied, but the document exceeds %d nodes; every position keeps the source as its origin", budget))
	}
	return Origin{index: index, source: sourceInfo(doc, opts), pointers: pointers, nodes: nodes}, diags
}

// applyRecovered runs the application under a barrier, converting a panic from
// the third-party library into a refusal so the compiler upholds the
// no-panics-escape invariant instead of crashing the caller's process.
//
// It is the overlay-side counterpart to the barriers around the parser and the
// resolver, and it is here for the same reason those are: the library faults on
// node shapes it accepts elsewhere. A document node holding no root is one —
// yamlpath indexes its first child unconditionally — and an overlay can be
// pointed at any tree, so what reaches the selector is not this package's to
// bound. The named returns are reset in the recover so a half-applied tree is
// never reported as applied.
//
// A recover reaches panics and nothing else. The library's clone follows an
// alias into what it names, so a recursive anchor in the overlay exhausts the
// stack and an alias bomb exhausts memory — both fatal errors, which end the
// process without passing through here. Those shapes are refused before the
// overlay is applied, by the loader, which reaches the scans that recognize
// them (GitHub #489); this barrier cannot stand in for that refusal.
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
// tree with the content it stands for, and reports whether it finished within
// budget.
//
// The library clones the subtrees it grafts, and its clone copies an alias by
// copying its target too — `newNode.Alias = clone(node.Alias)` — so the graft
// arrives holding an alias that points at a node sitting in no Content list
// anywhere. Every reader in this compiler walks Content and treats an alias as
// a leaf, on the stated grounds that what it stands for lives at its anchor's
// own position; that is true of every tree a parse produces and false of this
// one. The parser is not so restrained: it follows the alias and reads the
// content nobody else could see, so a tagged mapping hidden there faulted it on
// a goroutine no recover reaches (GitHub #477), and the node budget, the cycle
// scan and the overlay's own attribution all answered for a document missing
// whatever the graft carried.
//
// Substituting the content puts it back in Content, where the readings that
// exist to see it can. It is done here rather than to the overlay document
// because an update is not the only graft: a copy action clones a subtree of
// the source the same way, through the same clone.
//
// The two ways it can run out of budget mean different things, so they are
// reported differently. Failing to walk the tree says only that the tree is
// past what this package reads — the same thing the attribution walks say, and
// the same answer: give up the attribution, keep the compile, and say so.
// Failing to substitute says something else: a graft was found that cannot be
// made safe, and passing on a tree repaired in part is worse than either
// repairing it or refusing, so the caller refuses.
//
// normalized reports whether the tree was walked; safe reports whether nothing
// was found that could not be substituted.
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
// The value is recorded, not just the node's presence, because the library
// rewrites a scalar in place: an overlay that replaces info.title leaves that
// node's identity untouched and changes only what it holds, so identity alone
// would read the new title as the source's own.
//
// It reaches a superset of what the attribution walk reaches: both start at the
// document root rather than at the document node wrapping it, and this one
// descends into mapping keys the other addresses no pointer for. A superset is
// the safe direction and the required one — a node the attribution walk reaches
// that this one missed would read as introduced by the overlay — so the two need
// only share a starting point, not a definition of what is worth visiting.
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

// attribute walks the overlaid tree and collects every position the overlay is
// answerable for — as the set of pointers, and as the nodes sitting at them —
// reporting whether it visited the whole tree.
//
// A node the snapshot never saw was allocated while applying, and a node whose
// scalar value moved was rewritten in place; both mean the content at that
// position came from the overlay. The walk keeps descending past a match rather
// than stopping, which is what closes the set downwards: the library clones the
// subtrees it grafts, so every node beneath a grafted one is itself unknown to
// the snapshot and gets its own entry.
//
// A key the overlay introduced is recorded under its member's pointer as well.
// It adds no pointer — the value beside it is already attributed — but it is a
// node a finding can be anchored on, and the library appends it uncloned from
// the overlay document, so it carries that document's line and column: read as
// the source's, a worse answer than none.
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
// frame. The library appends a new key and its value together, so a key the
// overlay introduced always arrives beside a value the walk already reaches
// through the pointer that names it. An alias node is a leaf here for the same
// reason its target is not followed: the content it stands for lives at the
// anchor's own position, which the walk reaches there. That holds for every
// alias a parse produced and not for one the library grafted, whose clone
// points at a detached copy of the target (GitHub #477).
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
