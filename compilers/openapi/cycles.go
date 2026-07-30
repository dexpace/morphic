package openapi

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// maxCycleDepth bounds every recursive descent in the cycle detector. It guards
// the walk against a runaway structure per the bounded-recursion rule; real
// specs nest far shallower, so the cap only ever fires on a detector bug.
const maxCycleDepth = 10000

// maxMergeDepth bounds how deep a chain of `<<` merge keys the mapping view
// expands. It is far tighter than maxCycleDepth: each merge level
// re-materializes every pair beneath it, so expanding a chain of depth d costs
// O(d²), and real specs nest merge keys one or two levels deep. A chain that
// hits the bound is reported via a diag.CycleScanFailed warning (see
// nodeView.expand and refCycles), not silently truncated.
const maxMergeDepth = 64

// maxCachedPairs bounds total expanded pairs one nodeView retains, roughly
// 50 MB at 2²¹. It complements maxMergeDepth: that bound caps one mapping's
// expansion depth, this one caps a document with many merged mappings. Past the
// budget the view still answers correctly — it just stops memoizing, trading a
// cache hit for a recomputation.
const maxCachedPairs = 1 << 21

// The pure-$ref scan reads schema positions and reference-object positions
// under different rules, because speakeasy's resolver guards them unevenly.
//
// In a schema position, any pure-$ref cycle overflows its stack, so every one
// is refused here. Outside a schema, the resolver refuses a cycle whose hops all
// name components ('#/components/responses/A' -> '.../B' -> '.../A') and reports
// the chain by name — but it has no guard for a hop that names a node by
// document position ('#/paths/~1a', '#/webhooks/onA'), and resolving one of
// those faults the process. So only the chains that leave the components section
// are refused here; the rest are left to the resolver's better message
// (refCycles, outsideCycle).
//
// A $ref inside example/default/enum data is never followed in either mode.
// Descending into those positions would refuse legal documents (GitHub #12's fix
// must not regress valid specs).
//
// This scoping encodes the resolver behavior of github.com/speakeasy-api/openapi
// v1.24.0 (see go.mod), not an OpenAPI guarantee, and covers only cycles inside
// one document — external ('other.yaml#/...') and cross-document refs are left
// to speakeasy's resolver, which reports them as resolve errors rather than
// faulting. Re-validate against FuzzCycleDetector (cycles_fuzz_test.go) on any
// bump to that dependency.
//
// Five key sets drive the walk. Four mirror every *JSONSchema[Referenceable]
// field of oas3.Schema (jsonschema/oas3/schema.go): subSchemaObjectKeys,
// subSchemaListKeys, subSchemaMapKeys, and schemaEntryMapKeys for schema maps
// that appear outside a schema. schemaDataKeys names positions the walk must
// never descend into. Two entries have no corresponding field at this
// version — additionalItems and definitions — and are real JSON Schema
// keywords the library doesn't type yet; re-check both on a dependency bump.

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

// detectCycles scans raw source bytes for degenerate reference structures that
// would otherwise crash or exhaust memory in the third-party parser (GitHub
// #12, GitHub #27), before soa.Unmarshal ever runs. It reports three classes as
// error diagnostics: a recursive YAML anchor, a pure-$ref cycle (a chain of
// schema $refs that never reaches a node without one), and alias amplification
// (a billion-laughs expansion). A source that doesn't decode as YAML yields no
// cycles — the main parser reports that as a parse problem — and the scan runs
// under recoverCycleScan so a detector bug degrades to "no cycle found" rather
// than aborting.
func detectCycles(srcIndex int, data []byte) []ir.Diagnostic {
	return recoverCycleScan(srcIndex, func() []ir.Diagnostic {
		return scanCycles(srcIndex, data)
	})
}

// recoverCycleScan runs scan and, on any panic from the recursive walks,
// degrades to a diag.CycleScanFailed warning instead of propagating — a bug in
// the detector must not crash the compiler on a degenerate spec (GitHub #12).
// The compile still proceeds to the parser; only the pre-parse guarantee is
// flagged incomplete for this source.
func recoverCycleScan(srcIndex int, scan func() []ir.Diagnostic) (diags []ir.Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			diags = []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.CycleScanFailed,
				ir.Provenance{Source: srcIndex},
				"cycle pre-scan aborted (%v); reference-cycle protection is incomplete for this source", r)}
		}
	}()
	return scan()
}

// scanCycles decodes source bytes and reports the first degenerate cycle found,
// or nil. documentRoot may return nil for an empty or malformed root; the anchor
// and ref walks both treat a nil root as "nothing to scan", so no explicit nil
// guard is needed here.
func scanCycles(srcIndex int, data []byte) []ir.Diagnostic {
	if len(data) == 0 {
		return nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil
	}
	docRoot := documentRoot(&root)
	if d, ok := anchorCycle(srcIndex, docRoot); ok {
		return []ir.Diagnostic{d}
	}

	// anchorCycle must run first: a recursive anchor makes expandedWeight
	// infinite, and having already refused those is what makes the alias
	// graph a DAG and the weigh walk below provably terminating.
	diags := refCycles(srcIndex, docRoot)
	if diag.HasError(diags) {
		return diags
	}

	// refCycles runs first: a genuine $ref cycle should win the diagnostic over
	// an amplification finding on the same document, and refCycles is safe to
	// run on an amplifying document since it never materializes the expansion.
	// Appending (rather than replacing) preserves any diag.CycleScanFailed
	// warning refCycles already produced, so a document that both truncates a
	// merge chain and amplifies reports both findings.
	if d, ok := aliasAmplification(srcIndex, docRoot); ok {
		return append(diags, d)
	}
	return diags
}

// documentRoot returns the effective root node to scan: the content of a
// document node, or the node itself otherwise. It returns nil for an empty
// document.
func documentRoot(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		return n.Content[0]
	}
	return n
}

// anchorCycle reports the first alias whose resolved target is one of its own
// ancestors — a recursive YAML anchor that expands without bound. Legal anchor
// reuse (an alias to a node that is not an ancestor) is left untouched.
func anchorCycle(srcIndex int, root *yaml.Node) (ir.Diagnostic, bool) {
	return walkAnchors(srcIndex, root, map[*yaml.Node]bool{}, 0)
}

// walkAnchors descends the node tree tracking the ancestor path; an alias
// pointing back into that path is a recursive anchor. It deliberately never
// resolves alias edges — doing so would destroy the very signal it detects,
// and keeps the walk bounded by the tree alone.
func walkAnchors(srcIndex int, n *yaml.Node, path map[*yaml.Node]bool, depth int) (ir.Diagnostic, bool) {
	if n == nil || depth > maxCycleDepth {
		return ir.Diagnostic{}, false
	}
	if n.Kind == yaml.AliasNode {
		if n.Alias != nil && path[n.Alias] {
			return cyclicDiag(srcIndex, n, "recursive YAML anchor %q references an ancestor node", anchorName(n)), true
		}
		return ir.Diagnostic{}, false
	}
	path[n] = true
	for _, child := range n.Content {
		if d, ok := walkAnchors(srcIndex, child, path, depth+1); ok {
			return d, true
		}
	}
	delete(path, n)
	return ir.Diagnostic{}, false
}

// anchorName is the anchor label an alias points at, for the diagnostic message.
func anchorName(alias *yaml.Node) string {
	if alias.Alias != nil && alias.Alias.Anchor != "" {
		return alias.Alias.Anchor
	}
	return alias.Value
}

// refCycles reports the first pure-$ref cycle: a chain of schema $refs followed
// until it revisits a node already on the chain, without ever reaching a node
// that carries no top-level $ref (which terminates the chain, matching where
// speakeasy stops resolving).
//
// If the mapping view hit maxMergeDepth, it returns a diag.CycleScanFailed
// warning instead of a clean nil: truncation only ever drops pairs, so a cycle
// found despite it is still real, but a clean result only means "no cycle found
// in what could be expanded."
func refCycles(srcIndex int, root *yaml.Node) []ir.Diagnostic {
	s := newRefScan()
	s.collect(root)
	for _, start := range s.out {
		if cyclic, _ := s.followRefChain(root, start); cyclic {
			return []ir.Diagnostic{cyclicDiag(srcIndex, start,
				"cyclic $ref: reference chain never reaches a node without a $ref")}
		}
	}
	if d, found := s.outsideCycle(srcIndex, root); found {
		return []ir.Diagnostic{d}
	}
	if s.view.exhausted {
		return []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.CycleScanFailed,
			ir.Provenance{Source: srcIndex},
			"cycle pre-scan stopped at its %d-level merge-key expansion bound; "+
				"reference-cycle protection is incomplete for this source",
			maxMergeDepth)}
	}
	return nil
}

// outsideCycle reports the first $ref cycle among the reference objects that
// live outside any schema — a path item, a response, a parameter — but only
// when the chain leaves the components section on some hop.
//
// The split is not cosmetic. speakeasy's resolver refuses a cycle whose every
// hop names a component ('#/components/responses/A' -> '.../B' -> '.../A') with
// its own diagnostic, and that message names the chain, so it is the better one
// to keep. It has no such guard for a hop that names a node by document
// position ('#/paths/~1a', '#/webhooks/onA', '#/paths/~1a/get/responses/200'):
// resolving one recurses until the stack overflows and takes the process with
// it. Those are the chains this must refuse before soa.Unmarshal ever sees them,
// and refusing only those leaves the resolver's coverage exactly as it was.
func (s *refScan) outsideCycle(srcIndex int, root *yaml.Node) (ir.Diagnostic, bool) {
	for _, start := range s.outside {
		cyclic, leftComponents := s.followRefChain(root, start)
		if cyclic && leftComponents {
			return cyclicDiag(srcIndex, start,
				"cyclic $ref: reference chain never reaches a node without a $ref"), true
		}
	}
	return ir.Diagnostic{}, false
}

// componentsRef reports whether a same-document $ref names a node in the
// components section, the only shape speakeasy's resolver refuses on its own.
func componentsRef(ref string) bool {
	return strings.HasPrefix(ref, "#/components/")
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
	view  *nodeView
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
	s := &refScan{view: newNodeView(), safe: map[*yaml.Node]bool{}}
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
	n = deref(n)
	if n == nil || s.seen[role][n] {
		return
	}
	s.seen[role][n] = true
	s.stack = append(s.stack, refTask{n: n, role: role})
}

// pushReversed enqueues nodes so that a LIFO pop yields them in their original
// order, preserving the depth-first pre-order collect documents.
func (s *refScan) pushReversed(nodes []*yaml.Node, role walkRole) {
	for i := len(nodes) - 1; i >= 0; i-- {
		s.push(nodes[i], role)
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
	pairs := s.view.mappingPairs(n)
	if _, ok := pureRefTargetOf(pairs); ok {
		s.outside = append(s.outside, n)
	}
	for i := len(pairs) - 1; i >= 0; i-- {
		p := pairs[i]
		switch {
		case strings.HasPrefix(p.key, "x-"), schemaDataKeys[p.key]:
			// extension or example/default data: not a schema position
		case p.key == "schema":
			s.push(p.val, roleSchema)
		case schemaEntryMapKeys[p.key]:
			s.push(p.val, roleSchemaMap)
		default:
			s.push(p.val, roleOutside)
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
	pairs := s.view.mappingPairs(n)
	if _, ok := pureRefTargetOf(pairs); ok {
		s.out = append(s.out, n)
	}
	for i := len(pairs) - 1; i >= 0; i-- {
		p := pairs[i]
		switch {
		case subSchemaObjectKeys[p.key]:
			s.push(p.val, roleSchema)
		case subSchemaMapKeys[p.key]:
			s.push(p.val, roleSchemaMap)
		case subSchemaListKeys[p.key]:
			s.push(p.val, roleSchemaList)
		}
	}
}

// visitSchemaMap reads each value of a name→schema mapping as a schema.
func (s *refScan) visitSchemaMap(n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		return
	}
	pairs := s.view.mappingPairs(n)
	for i := len(pairs) - 1; i >= 0; i-- {
		s.push(pairs[i].val, roleSchema)
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
func (s *refScan) followRefChain(root, start *yaml.Node) (cyclic, leftComponents bool) {
	onPath := make(map[*yaml.Node]bool)
	var path []*yaml.Node
	cur := start
	for depth := 0; depth <= maxCycleDepth; depth++ {
		if s.safe[cur] {
			markSafe(path, s.safe)
			return false, leftComponents // cur already proved chain-terminating — no cycle
		}
		if onPath[cur] {
			return true, leftComponents // revisited a node on this chain — cyclic
		}
		ref, ok := s.view.pureRefTarget(cur)
		if !ok {
			s.safe[cur] = true
			markSafe(path, s.safe)
			return false, leftComponents // reached a node without a top-level $ref — legal recursion
		}
		if !componentsRef(ref) {
			leftComponents = true
		}
		onPath[cur] = true
		path = append(path, cur)
		next := s.view.resolvePointer(root, ref)
		if next == nil {
			markSafe(path, s.safe)
			return false, leftComponents // dangling ref — reported downstream as unresolved
		}
		cur = next
	}
	return false, leftComponents // depth cap reached without a verdict — mark nothing
}

// markSafe records every node on a proven chain-terminating path so a later chain
// that reaches one stops immediately instead of re-walking it.
func markSafe(path []*yaml.Node, safe map[*yaml.Node]bool) {
	for _, n := range path {
		safe[n] = true
	}
}

// yamlPair is one effective key/value pair of a mapping node, after alias and
// merge-key resolution.
type yamlPair struct {
	key string
	val *yaml.Node
}

// nodeView reads a raw yaml.Node tree the way speakeasy's unmarshaller reads
// it: alias keys and values dereferenced, `<<` merge keys expanded
// (yml.ResolveAlias and yml.ResolveMergeKeys, applied per mapping in
// marshaller/unmarshaller.go). Every gap between the raw tree this scan reads
// and the resolved one speakeasy's resolver reads is a cycle that reaches the
// resolver and faults the process (GitHub #26) — so every mapping read in this
// file goes through a nodeView.
//
// It memoizes each mapping's expansion for the scan's lifetime: without that, a
// merge chain costs O(n) per expansion and O(n) expansions per walk, going
// cubic in chain length — a hang where the bug being fixed was a crash. A
// cached expansion is always the depth-0 expansion, independent of the path
// that first reached it. maxMergeDepth and maxCachedPairs bound the chain
// depth and cache size respectively, so unlimited memoization can't trade the
// crash for exhausted memory instead.
type nodeView struct {
	pairs       map[*yaml.Node][]yamlPair
	cachedPairs int
	inFlight    map[*yaml.Node]bool
	exhausted   bool
}

// newNodeView returns an empty view; a view must not outlive the node tree whose
// expansions it caches.
func newNodeView() *nodeView {
	return &nodeView{
		pairs:    map[*yaml.Node][]yamlPair{},
		inFlight: map[*yaml.Node]bool{},
	}
}

// mappingPairs returns the effective pairs of a mapping node, following
// speakeasy's precedence: an explicit key beats one from a merge regardless of
// where the `<<` appears, an earlier merge source beats a later one on a
// shared key (yml.resolveMergeKeys), and a key repeated explicitly resolves to
// its last value.
//
// n is dereferenced, so an alias standing in for a whole mapping can be passed
// directly; a non-mapping node (including nil) yields no pairs. The returned
// slice is the view's own memo — callers must treat it as read-only.
func (v *nodeView) mappingPairs(n *yaml.Node) []yamlPair {
	pairs, _ := v.expand(deref(n), 0)
	return pairs
}

// expand returns n's effective pairs and whether the expansion is complete —
// false if a merge cycle was broken or maxMergeDepth was reached. Only a
// complete expansion is memoized: caching an incomplete one could make one
// traversal order silently lose a $ref another would find.
//
// Truncation is not contagious — only the node that hit the bound is refused,
// every other mapping still expands in full — because truncation only ever
// drops pairs, never invents an edge. Letting one over-deep chain disable the
// whole view would let an attacker disable the scan by prefixing a document
// with one; refCycles records the fact via the exhausted flag instead.
//
// An incomplete expansion entered at depth 0 is memoized anyway, because at
// depth 0 nothing is in flight and the result is a deterministic function of n
// alone (unlike inside a chain, where how much survived depends on the entry
// depth). Every walk-level read enters at depth 0, so this is what stops a
// truncated chain from re-expanding once per node that references it.
//
// The in-flight (merge-cycle) case needs no bound of its own: it requires an
// alias to an ancestor, which anchorCycle already refuses before refCycles runs.
func (v *nodeView) expand(n *yaml.Node, depth int) ([]yamlPair, bool) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, true
	}
	if cached, ok := v.pairs[n]; ok {
		return cached, true
	}
	if v.inFlight[n] {
		return nil, false
	}
	if depth > maxMergeDepth {
		v.exhausted = true
		return nil, false
	}

	v.inFlight[n] = true
	pairs, complete := v.expandContent(n, depth)
	delete(v.inFlight, n)

	if complete || v.isEntryPoint(depth) {
		v.memoize(n, pairs)
	}
	return pairs, complete
}

// isEntryPoint reports whether an expansion that just finished at this depth was
// the outermost one, with no other expansion of the same view in flight around
// it — the condition under which even a truncated result is reproducible.
func (v *nodeView) isEntryPoint(depth int) bool {
	return depth == 0 && len(v.inFlight) == 0
}

// memoize retains a complete expansion while the view's pair budget allows.
// Declining to cache costs a recomputation and nothing else — the cache is pure
// memoization, so a miss recomputes exactly the same pairs — which makes the
// budget a memory bound the scan can enforce without touching what it reports.
func (v *nodeView) memoize(n *yaml.Node, pairs []yamlPair) {
	if v.cachedPairs+len(pairs) > maxCachedPairs {
		return
	}
	v.pairs[n] = pairs
	v.cachedPairs += len(pairs)
}

// expandContent splits a mapping's raw content into the pairs it declares itself
// and the pairs its `<<` keys merge in, then applies the two precedence rules
// that govern them. They point in opposite directions, so they cannot share one
// pass: a repeated explicit key resolves to its last value, while a merged key
// yields to an explicit one and to any earlier merge source.
func (v *nodeView) expandContent(n *yaml.Node, depth int) ([]yamlPair, bool) {
	var explicit, merged []yamlPair
	complete := true

	for i := 0; i+1 < len(n.Content); i += 2 {
		raw, val := n.Content[i], deref(n.Content[i+1])
		if isMergeKey(raw) {
			got, ok := v.mergeSource(val, depth+1)
			merged = append(merged, got...)
			complete = complete && ok
			continue
		}
		key := deref(raw)
		if key == nil || key.Kind != yaml.ScalarNode {
			continue // a non-scalar key (after deref) cannot name a schema keyword
		}
		explicit = append(explicit, yamlPair{key: key.Value, val: val})
	}

	return appendUnseen(dedupeLastWins(explicit), merged), complete
}

// dedupeLastWins keeps the last pair for each key, at that last occurrence's
// position, matching speakeasy: a mapping that repeats a key is ill-formed, but
// speakeasy neither refuses it nor keeps the first one — it unmarshals every
// occurrence in turn, so the final value is what the resolver then works from.
func dedupeLastWins(pairs []yamlPair) []yamlPair {
	last := make(map[string]int, len(pairs))
	for i, p := range pairs {
		last[p.key] = i
	}
	out := make([]yamlPair, 0, len(last))
	for i, p := range pairs {
		if last[p.key] == i {
			out = append(out, p)
		}
	}
	return out
}

// appendUnseen appends the pairs of add whose key is not already present,
// keeping the first contributor of each — the rule for merged keys, which yield
// both to an explicit key and to an earlier merge source.
func appendUnseen(base, add []yamlPair) []yamlPair {
	if len(add) == 0 {
		return base
	}
	seenKey := make(map[string]bool, len(base)+len(add))
	for _, p := range base {
		seenKey[p.key] = true
	}
	out := base
	for _, p := range add {
		if seenKey[p.key] {
			continue
		}
		seenKey[p.key] = true
		out = append(out, p)
	}
	return out
}

// mergeSource expands one `<<` value into the pairs it contributes: a mapping is
// a single merge source, a sequence is several with an earlier element taking
// precedence over a later one on a shared key.
func (v *nodeView) mergeSource(val *yaml.Node, depth int) ([]yamlPair, bool) {
	if val == nil || val.Kind != yaml.SequenceNode {
		return v.expand(val, depth)
	}
	var out []yamlPair
	complete := true
	for _, item := range val.Content {
		got, ok := v.expand(deref(item), depth)
		out = append(out, got...)
		complete = complete && ok
	}
	return dedupeFirstWins(out), complete
}

// mergeTag is the tag yaml.v3 resolves every `<<` merge key to, and the exact
// tag speakeasy's yml.IsMergeKey requires before treating one as a merge.
const mergeTag = "!!merge"

// isMergeKey reports whether a raw mapping key node is a `<<` merge key,
// applying the same test speakeasy does: yml.IsMergeKey (yml/yml.go), run over
// every mapping via yml.ResolveMergeKeys. The key is checked undereferenced (an
// alias standing in for the key is not a scalar) and by resolved tag (a quoted
// '<<' resolves to !!str) — speakeasy treats both as ordinary keys, and
// expanding them would invent pairs it never sees.
//
// yaml.v3's own decoder (isMerge in decode.go) is the wrong model to copy: it's
// laxer about the tag (also accepts an empty or non-specific one) and stricter
// about repetition (honors only the last `<<`, where speakeasy merges every
// one — why expandContent accumulates them all). Neither difference is
// reachable from a parsed document today, but re-check this against
// yml.IsMergeKey on any dependency bump.
func isMergeKey(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.Value == "<<" && n.Tag == mergeTag
}

// dedupeFirstWins keeps only the first pair for each key, preserving order — the
// rule across the elements of a merge sequence, where an earlier source outranks
// a later one.
func dedupeFirstWins(pairs []yamlPair) []yamlPair {
	return appendUnseen(nil, pairs)
}

// pureRefTarget reports the internal $ref target of a node that carries a
// top-level internal ('#/...') $ref. Sibling keys do not disqualify it:
// speakeasy follows a node's top-level $ref before any concrete sibling, so a
// $ref node with a type or properties sibling still drives the crash. The chain
// terminates only at a node with no top-level $ref at all.
func (v *nodeView) pureRefTarget(n *yaml.Node) (string, bool) {
	return pureRefTargetOf(v.mappingPairs(n))
}

// pureRefTargetOf is pureRefTarget over an already-expanded pair list, so a
// caller that needs both the pairs and the target expands the mapping once.
func pureRefTargetOf(pairs []yamlPair) (string, bool) {
	for _, p := range pairs {
		if p.key != "$ref" {
			continue
		}
		if p.val == nil || p.val.Kind != yaml.ScalarNode || !strings.HasPrefix(p.val.Value, "#/") {
			return "", false
		}
		return p.val.Value, true
	}
	return "", false
}

// resolvePointer resolves an internal JSON pointer ('#/a/b') against the root
// node, returning the targeted node or nil when the path does not exist. Alias
// nodes along the path are dereferenced so navigation follows structure.
func (v *nodeView) resolvePointer(root *yaml.Node, ref string) *yaml.Node {
	cur := deref(root)
	for raw := range strings.SplitSeq(strings.TrimPrefix(ref, "#"), "/") {
		if raw == "" {
			continue
		}
		cur = v.childByToken(deref(cur), ids.UnescapeSegment(raw))
		if cur == nil {
			return nil
		}
	}
	return deref(cur)
}

// childByToken returns the child of a mapping (by key) or sequence (by index)
// node named by one JSON pointer token, or nil when absent. The mapping arm
// reads through the view, so pointer navigation resolves an alias key and an
// aliased or merged value exactly as pureRefTarget does.
func (v *nodeView) childByToken(n *yaml.Node, token string) *yaml.Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		for _, p := range v.mappingPairs(n) {
			if p.key == token {
				return p.val
			}
		}
	case yaml.SequenceNode:
		idx, err := strconv.Atoi(token)
		if err != nil || idx < 0 || idx >= len(n.Content) {
			return nil
		}
		return n.Content[idx]
	}
	return nil
}

// deref follows AliasNode links to the anchored node, bounded against an alias
// chain that loops (the anchor-cycle detector reports those separately).
func deref(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && n.Kind == yaml.AliasNode && i <= maxCycleDepth; i++ {
		n = n.Alias
	}
	return n
}

// cyclicDiag builds a diag.CyclicRef error diagnostic anchored at a node's
// line:col position, matching the provenance convention of the resolve path.
func cyclicDiag(srcIndex int, n *yaml.Node, format string, args ...any) ir.Diagnostic {
	prov := ir.Provenance{Source: srcIndex}
	if n != nil {
		prov.Pointer = fmt.Sprintf("%d:%d", n.Line, n.Column)
	}
	return diag.Newf(ir.SeverityError, diag.CyclicRef, prov, format, args...)
}
