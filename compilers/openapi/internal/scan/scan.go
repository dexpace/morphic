// Package scan refuses a source document before any of it is lowered.
//
// Three shapes would otherwise crash, hang or exhaust memory in the resolver or
// parser: a reference cycle that never reaches a concrete schema; a reference
// whose pointer passes through a reference being resolved, which deadlocks on
// that reference's own lock; and a YAML alias fan-out expanding to more nodes
// than the document declares. Each is read from the raw text through nodeview,
// before either library sees it.
//
// The caller supplies a sourceindex.Index over the same tree, so its size and
// alias-to-ancestor answers are not rederived.
package scan

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

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
// node's own line and column when there is a node. A node the parser did not
// place has neither, which is the zero Position: no position, rather than one
// at 0:0.
func InSource(srcIndex int) Locator {
	return func(n *yaml.Node) ir.Provenance {
		prov := ir.Provenance{Source: srcIndex}
		if n != nil {
			prov.Position = ir.Position{Line: n.Line, Column: n.Column}
		}
		return prov
	}
}

// InTree is InSource for a node of the tree pointers indexes (see Pointers),
// which also gives the node's JSON pointer there: a finding on a raw node is
// then placed where a lowering finding at the same position is, and a reader
// sees which declaration to edit (GitHub #803).
//
// The root's pointer is the empty one, which beside a position reads as none,
// so a finding on the document's top mapping is placed by position alone.
func InTree(srcIndex int, pointers func(*yaml.Node) (jsontext.Pointer, bool)) Locator {
	inSource := InSource(srcIndex)
	return func(n *yaml.Node) ir.Provenance {
		prov := inSource(n)
		prov.Pointer, _ = pointers(n)
		return prov
	}
}

// Pointers returns a node's JSON pointer in root's tree, its pairs keyed as
// the parser keys them (nodeview.KeyOf), and whether the tree holds the node.
// A key shares its member's pointer, and an alias's target is placed where it
// is written. A nil node or root is held by none, nor is a node under a `<<`
// merge key: the document the merge spells holds that pair at another pointer.
//
// The tree is indexed on the first node asked about, once, so a load that
// reports nothing never indexes it.
func Pointers(root *yaml.Node) func(*yaml.Node) (jsontext.Pointer, bool) {
	var steps map[*yaml.Node]step
	return func(n *yaml.Node) (jsontext.Pointer, bool) {
		if n == nil || root == nil {
			return "", false
		}
		if steps == nil {
			// Never cut short for a tree the loader reads: each passed a
			// sourceindex walk under the same bound first.
			steps, _ = stepsIn(root, sourceindex.MaxIndexedNodes)
		}
		if _, held := steps[n]; !held {
			return "", false
		}
		return pointerOf(steps, n), true
	}
}

// step is how a node of a tree is reached from the node holding it: that node,
// and the token naming the member or element. The document's own content has
// no holder.
type step struct {
	holder *yaml.Node
	token  string
}

// stepsIn indexes every node of root's tree, its keys included, by the step
// that first reaches it, and reports whether limit nodes held them all. An
// alias is a leaf: what it names is indexed where it is written. A node
// indexed already is not entered again, so a node placed twice ends the walk
// too; one past limit has no pointer.
//
// A pointer is not stored per node: that is the sum of every node's depth,
// which a long key over a wide subtree, or a deep one, makes quadratic in the
// document's size.
func stepsIn(root *yaml.Node, limit int) (map[*yaml.Node]step, bool) {
	out := map[*yaml.Node]step{root: {}}
	top := nodeview.DocumentRoot(root)
	if top == nil {
		return out, true // a document node holding nothing
	}
	out[top] = step{}
	stack := []*yaml.Node{top}
	entered, cut := 1, false
	reach := func(n *yaml.Node, s step) {
		if _, indexed := out[n]; indexed {
			return
		}
		if entered >= limit {
			cut = true
			return
		}
		entered++
		out[n] = s
		stack = append(stack, n)
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		reachChildren(n, reach)
	}
	return out, !cut
}

// reachChildren hands reach each child of n a pointer reaches, with the step
// that leads to it.
func reachChildren(n *yaml.Node, reach func(*yaml.Node, step)) {
	eachChild(n, func(c *yaml.Node, token string, held bool) {
		if held {
			reach(c, step{holder: n, token: token})
		}
	})
}

// eachChild hands visit each child of a mapping or sequence n, with the token
// naming it and whether a pointer reaches it there: a pair under a `<<` merge
// key, or under a key that is no scalar, is n's but has no pointer of its own.
// A scalar holds nothing, and an alias's target is written elsewhere.
func eachChild(n *yaml.Node, visit func(c *yaml.Node, token string, held bool)) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			token, own := nodeview.KeyOf(key)
			visit(key, token, own)
			visit(value, token, own)
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			visit(item, strconv.Itoa(i), true)
		}
	default:
	}
}

// pointerOf returns n's pointer in the tree steps indexes, or "" when the tree
// does not hold n. A holder is indexed before what it holds, so the walk up
// ends at the top in as many steps as n is deep.
func pointerOf(steps map[*yaml.Node]step, n *yaml.Node) jsontext.Pointer {
	var tokens []string
	for at := n; ; {
		s, ok := steps[at]
		if !ok {
			return ""
		}
		if s.holder == nil {
			break
		}
		tokens = append(tokens, s.token)
		at = s.holder
	}
	var p jsontext.Pointer
	for _, token := range slices.Backward(tokens) {
		p = p.AppendToken(token)
	}
	return p
}

// Cycles refuses, before parsing, structures that crash, hang or exhaust memory
// in the parser or resolver (GitHub #12, #27, speakeasy-api/openapi#231): a
// recursive anchor, a $ref cycle, a pointer re-entering its own chain, and
// alias amplification past the ratio or the surplus budget (zero for none). An
// incomplete scan yields a diag.CycleScanFailed warning.
//
// The index must cover the tree that will reach the parser, since an overlay
// can graft a cycle onto a clean document, and must not be truncated: a partial
// count refuses documents wrongly. yaml.v3's alias guard never fires on a
// yaml.Node decode, so nothing else bounds expansion (GitHub #479).
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
// its own size, the two refusals Cycles makes that concern YAML rather than
// OpenAPI. It serves overlays, whose applier follows aliases while cloning an
// update: an anchor naming its own ancestor would recurse until the stack ran
// out, which no recover converts, and an alias bomb would expand until memory
// did (GitHub #489). $ref chains are skipped; nothing resolves them in an
// overlay.
//
// The index must cover the whole document: an alias to an ancestor is visible
// only from the root. surplus is the alias budget.
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

// anchorCycle reports a recursive anchor the index found, if any, at the alias
// with the least pointer (Least). It is read before anything weighs the
// document's aliases: a recursive anchor makes the expanded weight infinite,
// and having refused those is what makes the alias graph a DAG and the weigh
// walk provably terminating.
func anchorCycle(locate Locator, idx sourceindex.Index) (ir.Diagnostic, bool) {
	cycles := idx.AnchorCycles()
	if len(cycles) == 0 {
		return ir.Diagnostic{}, false
	}
	alias := Least(idx.Root(), cycles)
	return cyclicDiag(locate, alias,
		"recursive YAML anchor %q references an ancestor node", anchorName(alias)), true
}

// scanIndex reports the degenerate structure the index's tree carries, or nil:
// a recursive anchor before anything else, then $ref chains, then alias
// amplification. The index's root is nil for a source with no document in it;
// the ref walk and the weigher both treat that as "nothing to scan", so no
// explicit nil guard is needed here.
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

// refCycles reports the degenerate chains among the collected references, at
// one site (see refusal): a chain followed until it revisits a node on it
// without reaching a node with no top-level $ref, where speakeasy stops
// resolving. Schema positions are refused on that alone; reference-object
// positions as outsideRefused judges them.
//
// If the mapping view hit nodeview.MergeDepthLimit it returns a
// diag.CycleScanFailed warning rather than a clean nil. Truncation only drops
// pairs, so a cycle found despite it is real, but a clean result means only
// that none was found in what could be expanded.
func refCycles(locate Locator, root *yaml.Node) []ir.Diagnostic {
	s := newRefScan()
	s.collect(root)
	chains := settleChains(s.view, root, slices.Concat(s.out, s.outside), sourceindex.MaxIndexedNodes)
	if d, found := s.refusal(locate, root, chains); found {
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

// refusal reports the chains refCycles refuses, or nothing when it refuses
// none. However many there are, one diagnostic names one site: of what the
// refused chains fail on (chainGraph.failsOn), the node with the least pointer
// (Least). A reference that only leads into a cycle is not named, and no
// declaration order moves the site (GitHub #912).
func (s *refScan) refusal(locate Locator, root *yaml.Node, chains *chainGraph) (ir.Diagnostic, bool) {
	failed := failures{chains: chains, loops: map[int]bool{}}
	for _, start := range s.out {
		if chains.outcome(start).verdict == chainCycles {
			failed.add(start)
		}
	}
	for _, start := range s.outside {
		if outsideRefused(chains.outcome(start)) {
			failed.add(start)
		}
	}
	if len(failed.sites) == 0 {
		return ir.Diagnostic{}, false
	}
	site := Least(root, failed.sites)
	if h := chains.hops[site]; h.loop != 0 && failed.loops[h.loop] {
		return cyclicDiag(locate, site, "cyclic $ref: reference chain never reaches a node without a $ref"), true
	}
	return cyclicDiag(locate, site, "cyclic $ref: reference resolves through itself"), true
}

// failures gathers what the refused chains fail on: each loop one cycles in,
// once, and each node one first re-enters.
type failures struct {
	chains *chainGraph
	loops  map[int]bool
	sites  []*yaml.Node
}

// add records what the chain from start fails on.
func (f *failures) add(start *yaml.Node) {
	loop, sites := f.chains.failsOn(start)
	if loop != 0 && f.loops[loop] {
		return
	}
	if loop != 0 {
		f.loops[loop] = true
	}
	f.sites = append(f.sites, sites...)
}

// outsideRefused reports whether the chain from a reference object outside any
// schema is one speakeasy's resolver cannot report itself.
//
// The resolver's cycle guard tracks completed hops: resolveObjectWithTracking
// extends its chain only after Reference.resolve returns. Whole-node hops under
// components are caught there, with a better message. A hop naming a node by
// document position ('#/paths/~1a') is not, so chainCycles is refused here once
// the chain has left components.
//
// A hop passing through a reference deadlocks before the tracker runs (see
// chainReenters), so that verdict is refused whatever the pointer names.
func outsideRefused(chain chainOutcome) bool {
	return chain.verdict == chainReenters || (chain.verdict == chainCycles && chain.leftComponents)
}

// componentsSection is the pointer of the components object itself. It repeats
// ids.componentsRoot because scan may not import ids.
const componentsSection jsontext.Pointer = "/components"

// componentsRef reports whether a same-document $ref names a node in the
// components section, the only shape speakeasy's resolver refuses on its own.
// The pointer is the normalized one nodeview.InternalPointer returns, so it
// carries no leading '#'.
func componentsRef(pointer jsontext.Pointer) bool {
	return pointer != componentsSection && componentsSection.Contains(pointer)
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

// chainOutcome is how the chain from a node ends: its verdict and, for
// chainCycles alone, whether a hop on the way named a node outside components,
// which separates a cycle speakeasy's resolver refuses from one it faults on
// (see outsideRefused). For any other verdict it is false.
type chainOutcome struct {
	verdict        chainVerdict
	leftComponents bool
}

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

// refScan holds the state of one pure-$ref cycle search, over a
// resolver-faithful view of the source tree.
//
// The collection walk is iterative. Aliases make a node reachable from many
// parents, so it needs memoization against exponential time, but memoization
// and a recursion depth cap are unsound together: a node first reached near the
// cap is truncated, then skipped from a shallower path, dropping refs. Without
// a cap, push enqueues each (node, role) pair once, so the loop runs at most
// roleCount times the node count.
type refScan struct {
	view  *nodeview.View
	stack []refTask
	seen  [roleCount]map[*yaml.Node]bool
	out   []*yaml.Node
	// outside holds the pure-$ref nodes found in reference-object positions
	// rather than schema ones. They are kept apart from out because the two are
	// reported under different rules — see outsideRefused.
	outside []*yaml.Node
}

// newRefScan returns a scan with every memoization map initialized. Building the
// visited sets as one array indexed by role — rather than a field per role —
// makes it impossible to add a role and forget its set.
func newRefScan() *refScan {
	s := &refScan{view: nodeview.New()}
	for i := range s.seen {
		s.seen[i] = map[*yaml.Node]bool{}
	}
	return s
}

// collect gathers every pure-$ref node reachable through a schema position into
// s.out, skipping reference objects and data subtrees that speakeasy never
// resolves as schema references. Nodes are appended in the depth-first
// pre-order a recursive walk would produce, which the chains are read in; what
// a refusal names is chosen by pointer, so it follows no such order.
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

// cyclicDiag builds a diag.CyclicRef error diagnostic anchored where locate
// puts the node.
func cyclicDiag(locate Locator, n *yaml.Node, format string, args ...any) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.CyclicRef, locate(n), format, args...)
}

// maxAliasAmplification bounds how many times larger a document's alias-
// expanded form may be than the document as parsed. An alias-free document
// expands to exactly its own size, so a large spec is never refused by it; what
// crosses it is the billion-laughs shape that exhausts memory inside
// soa.Unmarshal (GitHub #27).
//
// The highest ratio among 1,693 real specs is 3.728, so 128 leaves a 34x
// margin, wide on purpose: a `<<` merge chain inflates the ratio far past its
// cost (a 200-level chain measures 67 and compiles in 16 MiB). It is a limit on
// a shape, not a budget; the amount is the caller's surplus.
const maxAliasAmplification = 128

// minExpandedNodes is the expansion granted regardless of source size, so a
// small document with ordinary anchor reuse is never refused on a noisy ratio.
// It binds only under 256 raw nodes; at or above that,
// maxAliasAmplification*rawNodeCount already exceeds it. Real specs that small
// carry surpluses in the low hundreds, nowhere near this floor.
const minExpandedNodes = 1 << 15 // 32768

// aliasAmplification reports whether root's alias-substituted form exceeds what
// the document may expand to, with an error at the innermost node that crossed,
// the least by pointer of several. raw is the document's own node count.
//
// Two bounds apply. The ratio (maxAliasAmplification, floored at
// minExpandedNodes) catches compounding aliasing, as diag.AliasAmplification
// whatever the budget. surplus, the nodes aliasing may add beyond raw (zero for
// none), catches one anchor repeated in a flat list, whose ratio stays put
// while memory climbs, as diag.BudgetExceeded.
//
// Run it only once recursive anchors are refused, which makes the alias graph
// a DAG (see scanIndex).
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
	culprits := newAliasWeigher(bound).weigh(root)
	if len(culprits) == 0 {
		return ir.Diagnostic{}, false
	}
	if !budgeted {
		return aliasAmplificationDiag(locate, Least(root, culprits), shape, raw), true
	}

	// Past the budget, the document is weighed again against the ratio, so one
	// past both is named for the shape, which no budget admits. Only a refused
	// document pays for this walk.
	if bombs := newAliasWeigher(shape).weigh(root); len(bombs) > 0 {
		return aliasAmplificationDiag(locate, Least(root, bombs), shape, raw), true
	}
	return aliasBudgetDiag(locate, Least(root, culprits), surplus, raw), true
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
// root, stopping once one node's weight exceeds its allowance. A nil node
// weighs 0, an alias weighs what its target weighs, and any other node weighs 1
// plus the weight of its Content.
//
// That is what soa.Unmarshal pays: speakeasy v1.24.0 memoizes nothing on the
// shared *yaml.Node an alias resolves to, so it builds a model subtree per path
// reaching a node. The count is exact for aliases and an over-count, the safe
// side, for `<<` merge keys, whose pairs are deduplicated. Re-check it on a
// dependency bump.
type aliasWeigher struct {
	allowance int64
	ceiling   int64
	weight    map[*yaml.Node]int64
	inFlight  map[*yaml.Node]bool
	// cut holds each node a cycle reached while it was in flight, whose weight is
	// the ceiling it was saturated to rather than one weighed.
	cut map[*yaml.Node]bool

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
		cut:       map[*yaml.Node]bool{},
	}
}

// weigh computes expandedWeight for every node reachable from root, and returns
// in post-order every innermost amplifier: a node whose weight exceeds
// w.allowance though nothing it depends on does, the smallest structure already
// too large. It returns none for a document within the allowance. It weighs on
// past the first, since which comes first is the document's declaration order.
//
// Each distinct node enters the stack at most once, because pushChildren skips
// one whose weight is known or that is in flight, so the stack is bounded by
// the distinct nodes reachable from root.
func (w *aliasWeigher) weigh(root *yaml.Node) []*yaml.Node {
	if root == nil {
		return nil
	}
	var innermost []*yaml.Node
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
		if wt > w.allowance && !w.dependsOnAmplifier(top.n) {
			innermost = append(innermost, top.n)
		}
	}
	return innermost
}

// dependsOnAmplifier reports whether something n's weight depends on (see
// childrenOf), every one of which is weighed by the time n is, is itself past
// the allowance. A node a cycle cut is not weighed and never finishes, so the
// node depending on it is the innermost there is.
func (w *aliasWeigher) dependsOnAmplifier(n *yaml.Node) bool {
	return slices.ContainsFunc(childrenOf(n), func(c *yaml.Node) bool {
		return !w.cut[c] && w.weight[c] > w.allowance
	})
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
			w.weight[c], w.cut[c] = w.ceiling, true
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
