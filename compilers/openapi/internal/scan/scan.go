// Package scan refuses a source document before any of it is lowered.
//
// The refusals share a phase and a subject. A reference cycle that never reaches
// a concrete schema recurses without bound inside the resolver. A reference whose
// pointer passes through a reference already being resolved deadlocks it, since
// the resolver holds that reference's own lock across the pointer walk. A YAML
// alias fan-out that expands to far more nodes than the document declares
// exhausts memory inside the parser. Each reads the raw text through nodeview,
// and each runs before the document is handed to either.
//
// What the tree says about itself — its size, and whether an alias points back
// at one of its own ancestors — is not rederived here. The caller supplies a
// sourceindex.Index built over the same tree, so the questions that need only a
// walk are answered once for every refusal that asks them.
package scan

import (
	"fmt"
	"slices"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// maxCycleDepth bounds how many hops one pure-$ref chain is followed. It guards
// the walk against a runaway structure per the bounded-recursion rule; real
// specs chain far shorter, so nothing short of a document built to reach it — or
// a detector bug — ever does.
const maxCycleDepth = 10000

// schemaEntryMapKeys name a mapping of schemas encountered outside a schema
// (e.g. components.schemas, $defs): every value is a schema root.
var schemaEntryMapKeys = map[string]bool{
	"schemas": true, "$defs": true, "definitions": true,
}

// subSchemaObjectKeys name single sub-schemas within a schema object.
var subSchemaObjectKeys = map[string]bool{
	"items": true, "not": true, "additionalProperties": true,
	"additionalItems": true, "propertyNames": true, "contains": true,
	"if": true, "then": true, "else": true,
	"unevaluatedItems": true, "unevaluatedProperties": true,
	"contentSchema": true,
}

// subSchemaMapKeys name a mapping of name→schema within a schema object.
var subSchemaMapKeys = map[string]bool{
	"properties": true, "patternProperties": true, "dependentSchemas": true,
	"$defs": true, "definitions": true,
}

// subSchemaListKeys name a sequence of schemas within a schema object.
var subSchemaListKeys = map[string]bool{
	"allOf": true, "oneOf": true, "anyOf": true, "prefixItems": true,
}

// schemaDataKeys name value-bearing keys whose subtree is data, not schema — a
// $ref-shaped mapping under one of them is an opaque value, never resolved.
var schemaDataKeys = map[string]bool{
	"example": true, "examples": true, "default": true,
	"const": true, "enum": true,
}

// Locator answers where a diagnostic's subject is. Given a node it returns the
// provenance to report the node at; given nil it returns the source alone, the
// answer for a finding about the document rather than a position in it.
//
// It is a parameter rather than a source index because a node's own line and
// column are not always the answer: a node an overlay grafted has none, and the
// caller is the one holding the attribution that can say where it came from
// (GitHub #476).
type Locator func(n *yaml.Node) ir.Provenance

// InSource is the Locator for a document nothing has patched: srcIndex, and the
// node's own line and column when there is a node.
func InSource(srcIndex int) Locator {
	return func(n *yaml.Node) ir.Provenance {
		prov := ir.Provenance{Source: srcIndex}
		if n != nil {
			prov.Pointer = fmt.Sprintf("%d:%d", n.Line, n.Column)
		}
		return prov
	}
}

// Cycles scans an indexed source tree for degenerate reference structures that
// would otherwise crash, hang or exhaust memory in the third-party parser and
// resolver (GitHub #12, GitHub #27, speakeasy-api/openapi#231), before
// soa.Unmarshal ever runs. It reports as error diagnostics: a recursive YAML
// anchor, a pure-$ref cycle (a chain of schema $refs that never reaches a node
// without one), a reference whose pointer resolves through a reference already
// being resolved, alias amplification (a billion-laughs expansion), and aliases
// adding more nodes than the surplus budget allows (see aliasAmplification).
// The scan runs under recoverCycleScan so a detector bug degrades to "no cycle
// found" rather than aborting.
//
// The index is the caller's, built over the tree that will reach the parser: an
// overlay can graft a $ref cycle onto a document that had none, so a patched
// tree is re-indexed and re-scanned rather than trusted to the bytes that
// reached the overlay. The caller must not hand over a truncated index — every
// answer in one is partial, and the alias-expansion allowance derived from a
// partial node count would refuse documents on a bound they never crossed.
//
// Nothing bounds alias expansion ahead of this scan, so the weigher it runs is
// the refusal and not a backstop to one. yaml.v3's excessive-aliasing guard
// counts expansions during a decode into a Go value; the decode that produced
// this tree targets a yaml.Node, which holds an alias as one node pointing at
// its anchor, so nothing is expanded there and the guard never fires — the
// bomb fixture decodes into a node tree without error (GitHub #479). The
// expansion happens in the parser this runs ahead of, which follows aliases as
// it builds the model.
//
// surplus is the caller's alias budget: how many nodes aliasing may add beyond
// the document's own, or zero for no budget. See aliasAmplification for how it
// sits beside the ratio rule, which holds whatever the budget.
func Cycles(locate Locator, idx sourceindex.Index, surplus int64) []ir.Diagnostic {
	return recoverCycleScan(locate, func() []ir.Diagnostic {
		return scanIndex(locate, idx, surplus)
	})
}

// recoverCycleScan runs scan and, on any panic from the recursive walks,
// degrades to a diag.CycleScanFailed warning instead of propagating — a bug in
// the detector must not crash the compiler on a degenerate spec (GitHub #12).
// The compile still proceeds to the parser; only the pre-parse guarantee is
// flagged incomplete for this source.
func recoverCycleScan(locate Locator, scan func() []ir.Diagnostic) (diags []ir.Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			diags = []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.CycleScanFailed,
				locate(nil),
				"cycle pre-scan aborted (%v); reference-cycle protection is incomplete for this source", r)}
		}
	}()
	return scan()
}

// Aliases refuses a YAML document whose aliases expand without end or far past
// its own size — the two refusals Cycles makes that are about YAML rather than
// OpenAPI, for a document that is not an OpenAPI one.
//
// An overlay is that document. The library applying one copies each update by
// cloning it, and its clone follows an alias into what the alias names, so an
// anchor naming one of its own ancestors recursed until the stack ran out — a
// fatal error, which no recover converts — and an alias bomb expanded until
// memory did (GitHub #489). Both are shapes the source has been refused for
// since GitHub #12 and #27, on the same tree shape, the same ratio and the
// same alias budget, so the overlay is held to exactly those.
//
// It leaves out the $ref chains Cycles follows. Those are what an OpenAPI
// resolver does with a document; nothing resolves an overlay's references, and
// reading its update values as components would find cycles in text no parser
// will ever follow.
//
// The index is the caller's, built over the whole document rather than any one
// value in it: an anchor may sit outside the value that names it, and a cycle
// through it is visible as an alias to an ancestor only from the root. surplus
// is the alias budget, as in Cycles.
func Aliases(locate Locator, idx sourceindex.Index, surplus int64) []ir.Diagnostic {
	return recoverCycleScan(locate, func() []ir.Diagnostic {
		if d, ok := anchorCycle(locate, idx); ok {
			return []ir.Diagnostic{d}
		}
		if d, ok := aliasAmplification(locate, idx.Root(), idx.Nodes(), surplus); ok {
			return []ir.Diagnostic{d}
		}
		return nil
	})
}

// anchorCycle reports the recursive anchor the index found, if any. It is read
// before anything weighs the document's aliases: a recursive anchor makes the
// expanded weight infinite, and having refused those is what makes the alias
// graph a DAG and the weigh walk provably terminating.
func anchorCycle(locate Locator, idx sourceindex.Index) (ir.Diagnostic, bool) {
	alias, ok := idx.AnchorCycle()
	if !ok {
		return ir.Diagnostic{}, false
	}
	return cyclicDiag(locate, alias,
		"recursive YAML anchor %q references an ancestor node", anchorName(alias)), true
}

// scanIndex reports the first degenerate cycle the index's tree carries, or nil.
// The index's root is nil for a source with no document in it; the ref walk and
// the weigher both treat that as "nothing to scan", so no explicit nil guard is
// needed here.
func scanIndex(locate Locator, idx sourceindex.Index, surplus int64) []ir.Diagnostic {
	if d, ok := anchorCycle(locate, idx); ok {
		return []ir.Diagnostic{d}
	}

	root := idx.Root()
	diags := refCycles(locate, root)
	if diag.HasError(diags) {
		return diags
	}

	// refCycles runs first: a genuine $ref cycle should win the diagnostic over
	// an amplification finding on the same document, and refCycles is safe to
	// run on an amplifying document since it never materializes the expansion.
	// Appending (rather than replacing) preserves any diag.CycleScanFailed
	// warning refCycles already produced, so a document that both truncates a
	// merge chain and amplifies reports both findings.
	if d, ok := aliasAmplification(locate, root, idx.Nodes(), surplus); ok {
		return append(diags, d)
	}
	return diags
}

// anchorName is the anchor label an alias points at, for the diagnostic message.
func anchorName(alias *yaml.Node) string {
	if alias.Alias != nil && alias.Alias.Anchor != "" {
		return alias.Alias.Anchor
	}
	return alias.Value
}

// refCycles reports the first degenerate chain among the collected references:
// one followed until it revisits a node already on it, without ever reaching a
// node that carries no top-level $ref (which terminates the chain, matching
// where speakeasy stops resolving).
//
// Schema positions are refused on that alone. Reference-object positions carry
// a second rule and one exemption, both of which turn on what the resolver can
// see rather than on where the pointer points — see outsideCycle.
//
// If the mapping view hit nodeview.MergeDepthLimit, it returns a diag.CycleScanFailed
// warning instead of a clean nil: truncation only ever drops pairs, so a cycle
// found despite it is still real, but a clean result only means "no cycle found
// in what could be expanded."
func refCycles(locate Locator, root *yaml.Node) []ir.Diagnostic {
	s := newRefScan()
	s.collect(root)
	for _, start := range s.out {
		if verdict, _ := s.followRefChain(root, start); verdict == chainCycles {
			return []ir.Diagnostic{cyclicDiag(locate, start,
				"cyclic $ref: reference chain never reaches a node without a $ref")}
		}
	}
	if d, found := s.outsideCycle(locate, root); found {
		return []ir.Diagnostic{d}
	}
	if s.view.Exhausted() {
		return []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.CycleScanFailed,
			locate(nil),
			"cycle pre-scan stopped at its %d-level merge-key expansion bound; "+
				"reference-cycle protection is incomplete for this source",
			nodeview.MergeDepthLimit)}
	}
	return nil
}

// outsideCycle reports the first degenerate chain among the reference objects
// that live outside any schema — a path item, a response, a parameter. Two rules
// apply, and they are split on what speakeasy's resolver can see rather than on
// where the pointer points.
//
// Its cycle guard tracks *completed* hops: resolveObjectWithTracking appends a
// reference to its chain only after Reference.resolve returns, then compares the
// next one against that chain. So a cycle whose every hop resolves to a whole
// node is caught there, and for the all-components spelling
// ('#/components/responses/A' -> '.../B' -> '.../A') its message names the chain
// and is the better one to keep. A hop that names a node by document position
// ('#/paths/~1a', '#/webhooks/onA') is not caught, so chainCycles is refused
// here once the chain has left components.
//
// A hop that passes *through* a reference never completes at all: the pointer
// walk read-locks a reference whose resolve already holds its write lock, and
// the process deadlocks before the tracker is consulted. Nothing upstream can
// report that, and the components spelling deadlocks exactly like the
// document-position one, so chainReenters is refused whatever it names.
func (s *refScan) outsideCycle(locate Locator, root *yaml.Node) (ir.Diagnostic, bool) {
	for _, start := range s.outside {
		verdict, leftComponents := s.followRefChain(root, start)
		switch {
		case verdict == chainReenters:
			return cyclicDiag(locate, start,
				"cyclic $ref: reference resolves through itself"), true
		case verdict == chainCycles && leftComponents:
			return cyclicDiag(locate, start,
				"cyclic $ref: reference chain never reaches a node without a $ref"), true
		}
	}
	return ir.Diagnostic{}, false
}

// componentsRef reports whether a same-document $ref names a node in the
// components section, the only shape speakeasy's resolver refuses on its own.
// The pointer is the normalized one nodeview.InternalPointer returns, so it
// carries no leading '#'.
func componentsRef(pointer string) bool {
	return strings.HasPrefix(pointer, "/components/")
}

// chainVerdict is how following a pure-$ref chain ends. The two failing cases
// are kept apart because the resolver treats them differently, not for
// description's sake: only one of them is a shape speakeasy can report itself.
type chainVerdict int

const (
	// chainTerminates: the chain reached a node with no top-level $ref, or a
	// pointer that names nothing. The resolver stops either way.
	chainTerminates chainVerdict = iota

	// chainCycles: every hop resolved to a whole node, and the chain revisited
	// one already on it. Each hop completes, so speakeasy extends its own
	// reference chain and its cycle check sees the loop.
	chainCycles

	// chainReenters: a hop's pointer passes *through* a node already on the
	// chain. speakeasy cannot see this one. Reference.resolve holds that
	// reference's write lock across the pointer walk, and the walk read-locks
	// every reference it passes through, so re-entering one self-deadlocks on a
	// non-reentrant RWMutex — inside a hop that never completes, which is why
	// the resolver's own cycle check never runs (v1.24.0, openapi/reference.go
	// resolve at :537 and GetObject at :293). Refused whatever the pointer's
	// spelling: unlike chainCycles, a components-only chain deadlocks too.
	chainReenters
)

// walkRole is how the ref-collection walk reads the node it is visiting. The
// same node can legally occupy more than one role — an anchored pure-$ref
// mapping aliased once into a "properties" position and once used directly as a
// schema, say — and each role must be walked in full independently, so every
// role carries its own visited set.
type walkRole int

const (
	roleOutside    walkRole = iota // a document position outside any schema
	roleSchema                     // the node itself is a schema object
	roleSchemaMap                  // the node's values are schemas
	roleSchemaList                 // the node's elements are schemas

	roleCount // number of roles; sizes refScan.seen
)

// refTask is one unit of the ref-collection walk: a node and the role to read it
// in.
type refTask struct {
	n    *yaml.Node
	role walkRole
}

// refScan holds the state of one pure-$ref cycle search: the resolver-faithful
// view of the source tree, the worklist and per-role visited sets of the
// collection walk, the collected pure-$ref nodes, and the chain walk's memo of
// nodes already proven to terminate.
//
// The collection walk is iterative rather than recursive. Aliases make one node
// reachable from many parents, so the walk needs memoization or a chained-alias
// document goes exponential in chain length — trading a crash for a hang. But
// memoization and a recursion depth cap are unsound together: a node first
// reached near the cap has its descent truncated, then gets skipped when a
// shallower path reaches it again, silently dropping refs. Going iterative
// removes the cap: push() enqueues each (node, role) pair at most once, so the
// loop runs at most roleCount times the number of tree nodes.
type refScan struct {
	view  *nodeview.View
	stack []refTask
	seen  [roleCount]map[*yaml.Node]bool
	out   []*yaml.Node
	// outside holds the pure-$ref nodes found in reference-object positions
	// rather than schema ones. They are kept apart from out because the two are
	// reported under different rules — see outsideCycle.
	outside []*yaml.Node
	safe    map[*yaml.Node]bool
}

// newRefScan returns a scan with every memoization map initialized. Building the
// visited sets as one array indexed by role — rather than a field per role —
// makes it impossible to add a role and forget its set.
func newRefScan() *refScan {
	s := &refScan{view: nodeview.New(), safe: map[*yaml.Node]bool{}}
	for i := range s.seen {
		s.seen[i] = map[*yaml.Node]bool{}
	}
	return s
}

// collect gathers every pure-$ref node reachable through a schema position into
// s.out, skipping reference objects and data subtrees that speakeasy never
// resolves as schema references. Nodes are appended in the depth-first
// pre-order a recursive walk would produce, which keeps the reported cycle
// stable for a document containing more than one.
func (s *refScan) collect(root *yaml.Node) {
	s.push(root, roleOutside)
	for len(s.stack) > 0 {
		t := s.stack[len(s.stack)-1]
		s.stack = s.stack[:len(s.stack)-1]
		switch t.role {
		case roleOutside:
			s.visitOutside(t.n)
		case roleSchema:
			s.visitSchema(t.n)
		case roleSchemaMap:
			s.visitSchemaMap(t.n)
		case roleSchemaList:
			s.visitSchemaList(t.n)
		default:
			// Unreachable by construction: push is the only producer of tasks
			// and every declared role has a case above. A role added without
			// one is a programmer error, so fail loudly rather than silently
			// walking it as the wrong kind of node — recoverCycleScan turns
			// this into the diag.CycleScanFailed warning, never a crash.
			panic(fmt.Sprintf("cycle scan: unhandled walk role %d", t.role))
		}
	}
}

// push enqueues n in role unless it is nil or that exact pair was already
// enqueued. Dereferencing here is what lets an alias stand in for a whole schema
// (or for any position outside one) and still be followed. Marking at push time
// is what bounds collect: no pair is ever enqueued twice.
func (s *refScan) push(n *yaml.Node, role walkRole) {
	n = nodeview.Deref(n)
	if n == nil || s.seen[role][n] {
		return
	}
	s.seen[role][n] = true
	s.stack = append(s.stack, refTask{n: n, role: role})
}

// pushReversed enqueues nodes so that a LIFO pop yields them in their original
// order, preserving the depth-first pre-order collect documents.
func (s *refScan) pushReversed(nodes []*yaml.Node, role walkRole) {
	for _, node := range slices.Backward(nodes) {
		s.push(node, role)
	}
}

// visitOutside reads a document position outside any schema, entering schema
// context at schema-valued keys and never collecting refs from data or extension
// subtrees.
func (s *refScan) visitOutside(n *yaml.Node) {
	if n.Kind == yaml.SequenceNode {
		s.pushReversed(n.Content, roleOutside)
		return
	}
	pairs := s.view.MappingPairs(n)
	if _, ok := nodeview.PureRefTargetOf(pairs); ok {
		s.outside = append(s.outside, n)
	}
	for _, p := range slices.Backward(pairs) {
		switch {
		case strings.HasPrefix(p.Key, "x-"), schemaDataKeys[p.Key]:
			// extension or example/default data: not a schema position
		case p.Key == "schema":
			s.push(p.Val, roleSchema)
		case schemaEntryMapKeys[p.Key]:
			s.push(p.Val, roleSchemaMap)
		default:
			s.push(p.Val, roleOutside)
		}
	}
}

// visitSchema reads one schema object: it collects the node when it is a pure
// $ref, then descends only into sub-schema positions — never into type, enum,
// example, or extension data — so ref-shaped values never masquerade as schema
// references.
func (s *refScan) visitSchema(n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		return
	}
	pairs := s.view.MappingPairs(n)
	if _, ok := nodeview.PureRefTargetOf(pairs); ok {
		s.out = append(s.out, n)
	}
	for _, p := range slices.Backward(pairs) {
		switch {
		case subSchemaObjectKeys[p.Key]:
			s.push(p.Val, roleSchema)
		case subSchemaMapKeys[p.Key]:
			s.push(p.Val, roleSchemaMap)
		case subSchemaListKeys[p.Key]:
			s.push(p.Val, roleSchemaList)
		}
	}
}

// visitSchemaMap reads each value of a name→schema mapping as a schema.
func (s *refScan) visitSchemaMap(n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		return
	}
	pairs := s.view.MappingPairs(n)
	for _, pair := range slices.Backward(pairs) {
		s.push(pair.Val, roleSchema)
	}
}

// visitSchemaList reads each element of a schema sequence as a schema.
func (s *refScan) visitSchemaList(n *yaml.Node) {
	if n.Kind != yaml.SequenceNode {
		return
	}
	s.pushReversed(n.Content, roleSchema)
}

// followRefChain follows pure-$ref edges from start and reports whether the
// chain loops back onto itself without reaching a node that has no top-level
// $ref. It stops on such a node, a dangling ref, or a node already on the
// current chain; the on-path set and depth cap bound it against any structure.
//
// leftComponents reports whether any edge it followed named a node outside the
// components section, which is what tells a cycle speakeasy's resolver refuses
// from one it faults on (outsideCycle). It is only meaningful alongside
// cyclic=true: a chain that terminates was never a candidate either way, and
// the s.safe short-circuit can return before the whole chain is walked.
//
// s.safe memoizes nodes already proven to reach a $ref-free node, so the scan
// stays linear in the number of collected refs instead of re-walking shared
// tails. A node on a cycle is never marked safe, so memoization can't hide a
// real cycle.
func (s *refScan) followRefChain(root, start *yaml.Node) (chainVerdict, bool) {
	onPath := make(map[*yaml.Node]bool)
	var path []*yaml.Node
	leftComponents, memoizable := false, true
	cur := start

	for depth := 0; depth <= maxCycleDepth; depth++ {
		if s.safe[cur] {
			s.markSafe(path, memoizable)
			return chainTerminates, leftComponents // cur already proved chain-terminating
		}
		if onPath[cur] {
			return chainCycles, leftComponents // revisited a node on this chain
		}
		ref, ok := s.view.PureRefTarget(cur)
		if !ok {
			s.safe[cur] = true
			s.markSafe(path, memoizable)
			return chainTerminates, leftComponents // no top-level $ref — legal recursion
		}
		if !componentsRef(ref) {
			leftComponents = true
		}
		onPath[cur] = true
		path = append(path, cur)

		next, reenters, viaRef := s.traverse(root, ref, onPath)
		if reenters {
			return chainReenters, leftComponents
		}
		if viaRef {
			memoizable = false
		}
		if next == nil {
			s.markSafe(path, memoizable)
			return chainTerminates, leftComponents // dangling ref — unresolved downstream
		}
		cur = next
	}
	return chainTerminates, leftComponents // depth cap reached without a verdict
}

// traverse follows one pointer from the chain position carrying it and reports
// where it lands (nil when it names nothing), whether it passed through a node
// already on the chain, and whether any node it passed through is itself a
// reference.
//
// The destination is excluded from "passed through": arriving at an on-chain
// node is chainCycles, which speakeasy reports itself. A pointer that does not
// resolve has no destination, so every node it reached counts — that is the case
// the old dangling-ref branch called harmless, and the one that hangs.
func (s *refScan) traverse(root *yaml.Node, ref string, onPath map[*yaml.Node]bool) (dest *yaml.Node, reenters, viaRef bool) {
	hop, complete := s.view.PointerPath(root, ref)
	through := hop
	if complete {
		through = hop[:len(hop)-1]
	}
	for _, n := range through {
		if onPath[n] {
			return nil, true, viaRef
		}
		if _, isRef := s.view.PureRefTarget(n); isRef {
			viaRef = true
		}
	}
	if !complete {
		return nil, false, viaRef
	}
	return hop[len(hop)-1], false, viaRef
}

// markSafe records every node on a proven chain-terminating path so a later
// chain that reaches one stops immediately instead of re-walking it.
//
// It declines when a hop passed through a reference. Re-entrancy is a property
// of a pointer and the chain reading it, not of a node alone, so a chain proved
// terminating from one start says nothing about a chain that reaches it by
// another route — memoizing it there would make the refusal depend on which
// declaration order the walk happened to take. A hop that passes through no
// reference can never re-enter one whichever chain follows it, which is every
// hop in a real document: pointers pass through mappings like `components` and
// `schemas`, never through a $ref node. So the memo stays in force exactly where
// it earns its keep, and lapses only on the shapes it cannot answer for.
func (s *refScan) markSafe(path []*yaml.Node, memoizable bool) {
	if !memoizable {
		return
	}
	for _, n := range path {
		s.safe[n] = true
	}
}

// cyclicDiag builds a diag.CyclicRef error diagnostic anchored where locate
// puts the node.
func cyclicDiag(locate Locator, n *yaml.Node, format string, args ...any) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.CyclicRef, locate(n), format, args...)
}

// maxAliasAmplification bounds how many times larger a document's alias-
// expanded form may be than the document as parsed. An alias-free document
// expands to exactly its own node count, so a large spec is never refused by
// this rule; what crosses it is a few hundred bytes standing in, through
// nested aliases, for a structure vastly larger — the billion-laughs shape
// that exhausts memory inside soa.Unmarshal (GitHub #27).
//
// Calibrated against 1,693 real OpenAPI and Swagger specs (1,491 from
// APIs.guru, 199 hand-authored anchor-using ones, plus GitHub's, Stripe's and
// Kubernetes' flagship specs), whose highest ratio is 3.728. 128 leaves a 34x
// margin — wide on purpose: a `<<` merge chain inflates this ratio far past
// its cost, since merged pairs are deduplicated by key rather than turned into
// objects (a 200-level chain measures ratio 67 while compiling in 16 MiB).
//
// It is a constant and not a budget because it describes a shape rather than
// an amount of memory: nothing an author writes on purpose expands to more
// than a hundred times itself, so a caller has nothing to want more of. The
// amount is the caller's, as the surplus budget aliasAmplification takes.
const maxAliasAmplification = 128

// minExpandedNodes is the expansion granted regardless of source size, so a
// small document with ordinary anchor reuse is never refused on a noisy ratio.
// It binds only under 256 raw nodes; at or above that,
// maxAliasAmplification*rawNodeCount already exceeds it. Real specs that small
// carry surpluses in the low hundreds, nowhere near this floor.
const minExpandedNodes = 1 << 15 // 32768

// aliasAmplification reports whether root's alias-substituted form would
// contain more nodes than the document may expand to, and if so returns an
// error diagnostic anchored at the innermost node that crossed — the one the
// post-order walk finishes first, and a useful place to point the author.
//
// Two bounds apply, and a document must clear both, because each bounds a
// shape the other cannot. The ratio (maxAliasAmplification, floored at
// minExpandedNodes) catches nested, compounding aliasing. surplus — the nodes
// aliasing may add beyond raw, zero for no bound — catches one anchor repeated
// without limit in a flat list, whose ratio converges to the anchor's own size
// and stops growing while memory keeps climbing with the repetitions.
//
// Which one a document crossed decides the code. Past the ratio it is
// diag.AliasAmplification, a bomb whatever budget the caller set; inside the
// ratio and past only the surplus it is diag.BudgetExceeded, a document that is
// really that large once expanded, which a caller with the memory may admit.
//
// raw is the document's own node count, which the source index already
// established; the expansion is weighed against it rather than re-deriving it.
//
// Callers must run this only after a recursive YAML anchor has been refused:
// that is what makes the alias graph a DAG and this walk's termination provable
// without a cap of its own. See scanIndex for the ordering, and
// aliasWeigher.pushChildren for the defensive guard kept anyway.
func aliasAmplification(locate Locator, root *yaml.Node, raw, surplus int64) (ir.Diagnostic, bool) {
	shape := shapeAllowance(raw)
	// A surplus the ratio already bounds more tightly can never be the bound
	// crossed, and leaving it out also keeps raw+surplus from overflowing.
	budgeted := surplus > 0 && surplus < shape-raw
	bound := shape
	if budgeted {
		bound = raw + surplus
	}

	// One walk against whichever bound binds, so a document refused by neither —
	// every document a compile goes on to lower — costs a single walk.
	culprit, exceeded := newAliasWeigher(bound).weigh(root)
	if !exceeded {
		return ir.Diagnostic{}, false
	}
	if !budgeted {
		return aliasAmplificationDiag(locate, culprit, shape, raw), true
	}

	// Past the budget, the document is weighed again against the ratio, so one
	// past both is named for the shape, which no budget admits. Only a refused
	// document pays for this walk.
	if bomb, pastShape := newAliasWeigher(shape).weigh(root); pastShape {
		return aliasAmplificationDiag(locate, bomb, shape, raw), true
	}
	return aliasBudgetDiag(locate, culprit, surplus, raw), true
}

// shapeAllowance returns the expanded weight the ratio rule admits for a
// document of raw nodes: maxAliasAmplification times its size, floored at
// minExpandedNodes.
func shapeAllowance(raw int64) int64 {
	return max(maxAliasAmplification*raw, minExpandedNodes)
}

// aliasAmplificationDiag builds a diag.AliasAmplification error diagnostic
// anchored where locate puts the node whose expansion first crossed allowance.
// The reported node count is a lower bound ("at least"), not the exact
// expansion: aliasWeigher saturates its arithmetic at the allowance, so the
// true expansion of a severe bomb (the 10-level x 10-way fixture expands past
// 37 billion nodes) is never actually computed, only proven to exceed the
// allowance.
func aliasAmplificationDiag(locate Locator, n *yaml.Node, allowance, raw int64) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.AliasAmplification, locate(n),
		"YAML alias expansion reaches at least %d nodes, past the %d-node allowance for a %d-node document",
		allowance+1, allowance, raw)
}

// aliasBudgetDiag builds the diag.BudgetExceeded error diagnostic for a
// document whose aliases add more nodes than the surplus budget, anchored as
// aliasAmplificationDiag anchors its own and a lower bound for the same reason.
func aliasBudgetDiag(locate Locator, n *yaml.Node, surplus, raw int64) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, locate(n),
		"YAML aliases add at least %d nodes to a %d-node document, past the %d-node alias budget",
		surplus+1, raw, surplus)
}

// weighFrame is one entry of aliasWeigher's iterative post-order stack: a
// node awaiting the weight of what it depends on (its Content, or for an
// alias node, its target), and whether that dependency step has already run.
type weighFrame struct {
	n        *yaml.Node
	expanded bool
}

// aliasWeigher computes expandedWeight(n) for every node reachable from a
// root, stopping the instant one node's weight exceeds its allowance: nil
// weighs 0, an alias node weighs whatever its target weighs (an alias stands
// in for a copy of its target, not a reference to it), and every other node
// weighs 1 plus the weight of its own Content.
//
// That count is what soa.Unmarshal actually pays, not an estimate of it.
// speakeasy v1.24.0's yml.ResolveAlias returns the one shared *yaml.Node per
// alias, but nothing in marshaller/ or jsonschema/ memoizes on that pointer,
// so unmarshalModel builds a fresh model subtree for every path reaching a
// node — one object per path, which is exactly what this walk counts. It is
// exact for alias substitution and an over-count, the safe direction, for `<<`
// merge keys, whose pairs are deduplicated by key rather than turned into
// objects. A dependency bump should re-check that, as nodeview.IsMergeKey's comment in
// cycles.go does for the resolver behavior it depends on.
type aliasWeigher struct {
	allowance int64
	ceiling   int64
	weight    map[*yaml.Node]int64
	inFlight  map[*yaml.Node]bool

	// computations counts how many times computeWeight actually ran, as
	// opposed to how many times a node's weight was merely read from the
	// memo. It exists so the memoization invariant — every distinct node is
	// computed at most once, however many aliases point to it — is a plain
	// equality check (computations == len(weight)) rather than something only
	// a timing bound can observe.
	computations int64
}

// newAliasWeigher returns a weigher that refuses at allowance, capping every
// intermediate sum at allowance+1 so no addition can overflow regardless of
// how large the document's true expansion is.
func newAliasWeigher(allowance int64) *aliasWeigher {
	return &aliasWeigher{
		allowance: allowance,
		ceiling:   allowance + 1,
		weight:    map[*yaml.Node]int64{},
		inFlight:  map[*yaml.Node]bool{},
	}
}

// weigh computes expandedWeight for every node reachable from root and
// returns the first node (in post-order) whose weight exceeds w.allowance,
// or nil if none does. Post-order is what makes the returned node the
// innermost amplifier: a node's weight is finished, and checked, before any
// of its ancestors' — so the walk exits at the smallest structure already
// known to be too large, rather than only at the document root.
//
// Each distinct node enters the stack at most once: pushChildren skips a node
// whose weight is already known or that is already in flight, so the stack is
// bounded by the number of distinct nodes reachable from root.
func (w *aliasWeigher) weigh(root *yaml.Node) (*yaml.Node, bool) {
	if root == nil {
		return nil, false
	}
	stack := []*weighFrame{{n: root}}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		if _, done := w.weight[top.n]; done {
			stack = stack[:len(stack)-1]
			continue
		}
		if !top.expanded {
			top.expanded = true
			w.inFlight[top.n] = true
			w.pushChildren(&stack, top.n)
			continue
		}

		delete(w.inFlight, top.n)
		wt := w.computeWeight(top.n)
		w.computations++
		w.weight[top.n] = wt
		stack = stack[:len(stack)-1]
		if wt > w.allowance {
			return top.n, true
		}
	}
	return nil, false
}

// pushChildren enqueues the nodes n's weight depends on (see childrenOf). A
// child whose weight is already known is not re-pushed. A child already in
// flight is a cycle that slipped past anchorCycle having refused every
// recursive anchor — unreachable from a parsed document, but defended against
// anyway: rather than re-enter it and loop, its weight is saturated at
// w.ceiling, which is always large enough to carry the document to refusal
// without ever pretending the cyclic subtree is small.
func (w *aliasWeigher) pushChildren(stack *[]*weighFrame, n *yaml.Node) {
	for _, c := range childrenOf(n) {
		if c == nil {
			continue
		}
		if w.inFlight[c] {
			w.weight[c] = w.ceiling
			continue
		}
		if _, done := w.weight[c]; done {
			continue
		}
		*stack = append(*stack, &weighFrame{n: c})
	}
}

// computeWeight returns n's expandedWeight, given that every node it depends
// on (via childrenOf) already has a recorded weight. An alias node's weight is
// exactly its target's — substituting a copy of the target, unexpanded
// further, is what an alias stands in for. Every other node's weight is 1
// (itself) plus its children's, saturated at w.ceiling so the running sum can
// never overflow, with the loop exiting the moment the total already exceeds
// the allowance, since nothing further down the same Content list can change
// that verdict.
func (w *aliasWeigher) computeWeight(n *yaml.Node) int64 {
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return 0
		}
		return w.weight[n.Alias]
	}
	total := int64(1)
	for _, c := range n.Content {
		total = saturatingAdd(total, w.weight[c], w.ceiling)
		if total > w.allowance {
			return total
		}
	}
	return total
}

// childrenOf returns the nodes n's expandedWeight depends on: an alias node
// depends only on its target, never on its own (always empty) Content, and
// every other node depends on its Content. This is the one place the two node
// kinds are told apart, so every other method can treat "the nodes n depends
// on" uniformly.
func childrenOf(n *yaml.Node) []*yaml.Node {
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return nil
		}
		return []*yaml.Node{n.Alias}
	}
	return n.Content
}

// saturatingAdd returns a+b clamped to ceiling. Both aliasWeigher's addends
// are always non-negative and individually at most ceiling, which is what
// makes the comparison overflow-free: b >= ceiling-a can be evaluated without
// a+b ever being computed when it would exceed ceiling.
func saturatingAdd(a, b, ceiling int64) int64 {
	if b >= ceiling-a {
		return ceiling
	}
	return a + b
}
