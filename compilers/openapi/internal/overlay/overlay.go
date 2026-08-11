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
	pointers map[string]bool
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
func (o Origin) IndexAt(pointer string, fallback int) int {
	if o.pointers[pointer] {
		return o.index
	}
	return fallback
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
	diags, applied := applyRecovered(doc, root, at, opts.Lax)
	if !applied {
		return Origin{}, diags
	}

	var pointers map[string]bool
	ok := false
	if complete {
		pointers, ok = attribute(root, before, budget)
	}
	if !ok {
		return Origin{}, append(diags, diag.Newf(ir.SeverityWarning, diag.OverlayOriginIncomplete, at,
			"overlay applied, but the document exceeds %d nodes; every position keeps the source as its origin", budget))
	}
	return Origin{index: index, source: sourceInfo(doc, opts), pointers: pointers}, diags
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
func applyRecovered(doc *soaoverlay.Overlay, root *yaml.Node, at ir.Provenance, lax bool) (diags []ir.Diagnostic, applied bool) {
	defer func() {
		if r := recover(); r != nil {
			diags = []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.OverlayFailed, at,
				"cannot apply overlay: overlay library panicked (%v)", r)}
			applied = false
		}
	}()
	return apply(doc, root, at, lax)
}

// apply runs doc over root under the caller's strictness, returning what it
// reported and whether the result is usable.
//
// Strict mode reports on two levels, and they are not the same finding. The
// per-action warnings say what each action did or failed to do; the error, when
// there is one, says a selector matched nothing at all. An action that matched
// and then changed nothing produces a warning and no error, so the compile
// proceeds — the fix that action describes is already in the source, which is
// worth saying and not worth refusing over. Lax mode reports neither level, and
// still fails on an overlay the library could not apply at all.
func apply(doc *soaoverlay.Overlay, root *yaml.Node, at ir.Provenance, lax bool) ([]ir.Diagnostic, bool) {
	if lax {
		if err := doc.ApplyTo(root); err != nil {
			return []ir.Diagnostic{failed(at, err)}, false
		}
		return nil, true
	}

	warnings, err := doc.ApplyToStrict(root)
	out := make([]ir.Diagnostic, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, diag.Newf(ir.SeverityWarning, diag.OverlayAction, at, "%s", w))
	}
	if err != nil {
		return append(out, failed(at, err)), false
	}
	return out, true
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

// attribute walks the overlaid tree and collects the pointer of every position
// the overlay is answerable for, reporting whether it visited the whole tree.
//
// A node the snapshot never saw was allocated while applying, and a node whose
// scalar value moved was rewritten in place; both mean the content at that
// position came from the overlay. The walk keeps descending past a match rather
// than stopping, which is what closes the set downwards: the library clones the
// subtrees it grafts, so every node beneath a grafted one is itself unknown to
// the snapshot and gets its own entry.
func attribute(root *yaml.Node, before map[*yaml.Node]string, budget int) (map[string]bool, bool) {
	pointers := map[string]bool{}
	stack := []frame{{node: nodeview.DocumentRoot(root)}}
	for ; len(stack) > 0; budget-- {
		if budget == 0 {
			return nil, false
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.node == nil {
			continue
		}
		if prior, known := before[f.node]; !known || prior != f.node.Value {
			pointers[f.pointer] = true
		}
		stack = append(stack, children(f)...)
	}
	return pointers, true
}

// frame is one node of the attribution walk together with the JSON pointer that
// addresses it.
type frame struct {
	node    *yaml.Node
	pointer string
}

// children returns the frames beneath f, addressed the way the compiler
// addresses them: a mapping's values under their escaped keys, a sequence's
// elements under their positions.
//
// Mapping keys are not walked in their own right. The library appends a new key
// and its value together, so a key the overlay introduced always arrives beside
// a value the walk already reaches through the pointer that names it. An alias
// node is a leaf here for the same reason its target is not followed: the
// content it stands for lives at the anchor's own position, which the walk
// reaches there.
func children(f frame) []frame {
	switch f.node.Kind {
	case yaml.MappingNode:
		out := make([]frame, 0, len(f.node.Content)/2)
		for i := 0; i+1 < len(f.node.Content); i += 2 {
			out = append(out, frame{
				node:    f.node.Content[i+1],
				pointer: f.pointer + ids.Ptr(f.node.Content[i].Value),
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
