package openapi

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// maxCycleDepth bounds every recursive descent in the cycle detector. It guards
// the walk against a runaway structure per the bounded-recursion rule; real
// specs nest far shallower, so the cap only ever fires on a detector bug.
const maxCycleDepth = 10000

// The pure-$ref scan visits schema positions only, because the unrecoverable
// stack overflow is specific to speakeasy's schema resolver: a pure-$ref cycle
// among components/responses (or any non-schema reference object) is caught by
// the resolver's own cycle guard and surfaces as a resolve error, and a $ref
// inside example/default/enum data is never followed at all. Descending into
// those positions would refuse legal documents (GitHub #12 fix must not
// regress valid specs). The key sets below drive the schema-scoped walk.
//
// Scope and provenance: this scan classifies a single source document, matching
// the crash it prevents — an internal ('#/...') pure-$ref cycle inside one spec.
// External ('other.yaml#/...') and cross-document cycles are left to speakeasy's
// reference resolver, which reports them as resolve errors rather than faulting.
// The schema-position scoping encodes the resolver behavior of
// github.com/speakeasy-api/openapi v1.24.0 (see go.mod), not an OpenAPI
// guarantee: if a later bump changes which reference positions the resolver
// recurses through, re-validate against FuzzCycleDetector (cycles_fuzz_test.go),
// which drives arbitrary sources through Compile and faults if a degenerate cycle
// ever reaches the parser.
//
// The four key sets below mirror every *JSONSchema[Referenceable]-typed field
// of oas3.Schema at github.com/speakeasy-api/openapi v1.24.0
// (jsonschema/oas3/schema.go): object-valued fields in subSchemaObjectKeys,
// list-valued fields in subSchemaListKeys, map-valued fields in
// subSchemaMapKeys. additionalItems has no corresponding field in that
// version — it is kept as a real draft-07 keyword, harmless to recognize even
// though the library does not type it. A future bump of that dependency
// should re-check this mapping field-by-field.

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

// detectCycles scans raw source bytes for degenerate reference cycles that would
// otherwise crash the third-party parser with a fatal, unrecoverable stack
// overflow (GitHub #12). It runs BEFORE soa.Unmarshal so the anchor case never
// reaches the crashing parser, and reports two classes as error diagnostics: a
// recursive YAML anchor (an alias whose target is one of its own ancestors) and
// a pure-$ref cycle (a chain of schema $refs that never reaches a node without a
// top-level $ref). A source that does not decode as YAML yields no cycles: the
// main parser owns reporting that as a parse problem. The scan runs under
// recoverCycleScan so a detector bug degrades to "no cycle found", never aborts.
func detectCycles(srcIndex int, data []byte) []ir.Diagnostic {
	return recoverCycleScan(srcIndex, func() []ir.Diagnostic {
		return scanCycles(srcIndex, data)
	})
}

// recoverCycleScan runs scan and, on any panic from the recursive walks, degrades
// to a single non-fatal warning instead of propagating. The pre-parse pass exists
// so the compiler never crashes on a degenerate spec (GitHub #12); a bug in the
// detector itself must therefore never abort the caller's process. Rather than
// swallow the failure silently, it surfaces a codeCycleScanFailed warning so an
// incomplete scan is observable: the compile still proceeds to the parser and
// stays protected against every cycle the scan did classify — only the pre-parse
// guarantee is flagged as incomplete for this source.
func recoverCycleScan(srcIndex int, scan func() []ir.Diagnostic) (diags []ir.Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			diags = []ir.Diagnostic{diagf(ir.SeverityWarning, codeCycleScanFailed,
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
	if d, ok := refCycle(srcIndex, docRoot); ok {
		return []ir.Diagnostic{d}
	}
	return nil
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

// walkAnchors descends the node tree tracking the ancestor path. An alias node
// pointing back into that path is a recursive anchor; the walk never follows an
// alias edge structurally, so it stays bounded by the finite tree and the cap.
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

// refCycle reports the first pure-$ref cycle: a chain of schema $refs, followed
// until it returns to a node already on the chain without ever reaching a node
// that carries no top-level $ref. A $ref that reaches such a node terminates the
// chain and is not a cycle — that is exactly where speakeasy stops resolving.
func refCycle(srcIndex int, root *yaml.Node) (ir.Diagnostic, bool) {
	var refs []*yaml.Node
	collectSchemaRefs(root, &refs)
	safe := make(map[*yaml.Node]bool)
	for _, start := range refs {
		if followRefChain(root, start, safe) {
			return cyclicDiag(srcIndex, start, "cyclic $ref: reference chain never reaches a node without a $ref"), true
		}
	}
	return ir.Diagnostic{}, false
}

// refScan carries the shared output slice and one visited-node set per walk
// function for the ref-collection walk. mappingPairs (below) dereferences
// alias nodes, which lets one alias edge fan into shared substructure
// reachable from many parents; without memoization, a chained-alias document
// (&a1 {type: string}, &a2 {allOf: [*a1, *a1]}, &a3 {allOf: [*a2, *a2]}, ...)
// would make the walk exponential in the chain length instead of linear in
// the source tree — an availability regression that would trade a crash for
// a hang. Visiting a node at most once per set is sufficient: out holds node
// pointers and followRefChain later operates on node identity, so a repeat
// visit could only append a duplicate entry.
//
// Each of the four walk functions gets its own set — not one shared set per
// "context" — because walkSchema, walkSchemaMap, and walkSchemaList each
// interpret an incoming node differently: walkSchema treats the node itself
// as a schema (and calls pureRefTarget on it), walkSchemaMap treats the
// node's values as schemas, walkSchemaList treats its elements as schemas.
// An anchor reused in two of those roles is legal YAML (the same $ref node
// aliased once into a "properties" position and once used directly as a
// schema, say) and is exactly the shape that crashes speakeasy. Sharing one
// set across those roles previously let the first role to reach the node
// mark it seen, so the second role skipped it — silently dropping the
// pure-$ref node the chain walk needed and letting a real cycle reach the
// resolver uncaught (GitHub #26 follow-up). One set per function has no such
// gap: a node is only ever skipped on a second visit *in the same role*,
// where re-deriving the same result would just append a duplicate.
type refScan struct {
	out            *[]*yaml.Node
	outsideSeen    map[*yaml.Node]bool
	schemaSeen     map[*yaml.Node]bool
	schemaMapSeen  map[*yaml.Node]bool
	schemaListSeen map[*yaml.Node]bool
}

// seen reports whether n was already recorded in set, recording it if not.
// It is the memoization primitive that bounds refScan's walk to one visit per
// node per context.
func seen(set map[*yaml.Node]bool, n *yaml.Node) bool {
	if set[n] {
		return true
	}
	set[n] = true
	return false
}

// collectSchemaRefs gathers every pure-$ref node reachable through a schema
// position, skipping reference objects and data subtrees that speakeasy never
// resolves as schema references. The walk is split across walkOutsideSchema,
// walkSchema, walkSchemaMap, and walkSchemaList, each bounded by its own
// visited-node set (see the refScan doc comment for why they are separate).
func collectSchemaRefs(root *yaml.Node, out *[]*yaml.Node) {
	s := &refScan{
		out:            out,
		outsideSeen:    map[*yaml.Node]bool{},
		schemaSeen:     map[*yaml.Node]bool{},
		schemaMapSeen:  map[*yaml.Node]bool{},
		schemaListSeen: map[*yaml.Node]bool{},
	}
	s.walkOutsideSchema(root, 0)
}

// walkOutsideSchema descends the OpenAPI document outside any schema, entering
// schema context at schema-valued keys and never collecting refs from data or
// extension subtrees. n is dereferenced at entry so an alias-valued position
// resolves through; mappingPairs resolves alias keys/values and `<<` merges
// the way the decoder does. The finite tree, depth cap, and visited set bound
// the descent.
func (s *refScan) walkOutsideSchema(n *yaml.Node, depth int) {
	n = deref(n)
	if n == nil || depth > maxCycleDepth || seen(s.outsideSeen, n) {
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		for _, p := range mappingPairs(n) {
			switch {
			case strings.HasPrefix(p.key, "x-"), schemaDataKeys[p.key]:
				// extension or example/default data: not a schema position
			case p.key == "schema":
				s.walkSchema(p.val, depth+1)
			case schemaEntryMapKeys[p.key]:
				s.walkSchemaMap(p.val, depth+1)
			default:
				s.walkOutsideSchema(p.val, depth+1)
			}
		}
	case yaml.SequenceNode:
		for _, child := range n.Content {
			s.walkOutsideSchema(child, depth+1)
		}
	}
}

// walkSchema visits one schema object: it collects the node when it is a pure
// $ref, then recurses only into sub-schema positions — never into type, enum,
// example, or extension data — so ref-shaped values never masquerade as schema
// references. n is dereferenced at entry so an alias-valued schema node (an
// anchor reused directly as a schema) is followed to its target.
func (s *refScan) walkSchema(n *yaml.Node, depth int) {
	n = deref(n)
	if n == nil || depth > maxCycleDepth || seen(s.schemaSeen, n) {
		return
	}
	if n.Kind != yaml.MappingNode {
		return
	}
	if _, ok := pureRefTarget(n); ok {
		*s.out = append(*s.out, n)
	}
	for _, p := range mappingPairs(n) {
		switch {
		case subSchemaObjectKeys[p.key]:
			s.walkSchema(p.val, depth+1)
		case subSchemaMapKeys[p.key]:
			s.walkSchemaMap(p.val, depth+1)
		case subSchemaListKeys[p.key]:
			s.walkSchemaList(p.val, depth+1)
		}
	}
}

// walkSchemaMap visits each value of a name→schema mapping as a schema. It has
// its own visited set (schemaMapSeen), distinct from walkSchema's: the same
// anchored node can legally be reached once in the "whole node is a schema"
// role and once in the "node's values are schemas" role, and each role must
// be walked in full regardless of what the other already visited.
func (s *refScan) walkSchemaMap(n *yaml.Node, depth int) {
	n = deref(n)
	if n == nil || depth > maxCycleDepth || seen(s.schemaMapSeen, n) || n.Kind != yaml.MappingNode {
		return
	}
	for _, p := range mappingPairs(n) {
		s.walkSchema(p.val, depth+1)
	}
}

// walkSchemaList visits each element of a schema sequence as a schema. It has
// its own visited set (schemaListSeen) for the same reason walkSchemaMap does:
// the "node's elements are schemas" role is distinct from the other roles a
// shared anchor might otherwise be reached through.
func (s *refScan) walkSchemaList(n *yaml.Node, depth int) {
	n = deref(n)
	if n == nil || depth > maxCycleDepth || seen(s.schemaListSeen, n) || n.Kind != yaml.SequenceNode {
		return
	}
	for _, child := range n.Content {
		s.walkSchema(child, depth+1)
	}
}

// followRefChain follows pure-$ref edges from start and reports whether the chain
// loops back onto itself without reaching a node that has no top-level $ref. It
// stops on such a node, a dangling ref, or a node already on the current chain;
// the on-path set and depth cap bound it against any structure.
//
// safe memoizes nodes already proven to reach a $ref-free node across every chain
// in one scan: reaching one ends the walk immediately, and every node on a
// terminating chain is recorded, so the whole scan stays linear in the number of
// collected refs instead of re-walking shared tails. A node on a cycle is never
// marked safe, so memoization never hides a real cycle.
func followRefChain(root, start *yaml.Node, safe map[*yaml.Node]bool) bool {
	onPath := make(map[*yaml.Node]bool)
	var path []*yaml.Node
	cur := start
	for depth := 0; depth <= maxCycleDepth; depth++ {
		if safe[cur] {
			markSafe(path, safe)
			return false // cur already proved chain-terminating — no cycle
		}
		if onPath[cur] {
			return true // revisited a node on this chain — cyclic
		}
		ref, ok := pureRefTarget(cur)
		if !ok {
			safe[cur] = true
			markSafe(path, safe)
			return false // reached a node without a top-level $ref — legal recursion
		}
		onPath[cur] = true
		path = append(path, cur)
		next := resolvePointer(root, ref)
		if next == nil {
			markSafe(path, safe)
			return false // dangling ref — reported downstream as unresolved
		}
		cur = next
	}
	return false // depth cap reached without a verdict — mark nothing
}

// markSafe records every node on a proven chain-terminating path so a later chain
// that reaches one stops immediately instead of re-walking it.
func markSafe(path []*yaml.Node, safe map[*yaml.Node]bool) {
	for _, n := range path {
		safe[n] = true
	}
}

// pureRefTarget reports the internal $ref target of a node that carries a
// top-level internal ('#/...') $ref. Sibling keys do not disqualify it:
// speakeasy follows a node's top-level $ref before any concrete sibling, so a
// $ref node with a type or properties sibling still drives the crash. The chain
// terminates only at a node with no top-level $ref at all.
//
// It reads n through mappingPairs rather than n.Content directly, so a $ref
// carried by an alias key or value, or contributed by a `<<` merge, is seen
// exactly as the decoder would see it — not only a literal `$ref` scalar key
// paired with a literal scalar value.
func pureRefTarget(n *yaml.Node) (string, bool) {
	for _, p := range mappingPairs(n) {
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
func resolvePointer(root *yaml.Node, ref string) *yaml.Node {
	cur := deref(root)
	for _, raw := range strings.Split(strings.TrimPrefix(ref, "#"), "/") {
		if raw == "" {
			continue
		}
		cur = childByToken(deref(cur), unescapePointer(raw))
		if cur == nil {
			return nil
		}
	}
	return deref(cur)
}

// childByToken returns the child of a mapping (by key) or sequence (by index)
// node named by one JSON pointer token, or nil when absent. The mapping arm
// uses mappingPairs so pointer navigation resolves through an alias key or an
// aliased/merged value exactly as pureRefTarget does.
func childByToken(n *yaml.Node, token string) *yaml.Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		for _, p := range mappingPairs(n) {
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

// yamlPair is one effective key/value pair of a mapping node, after alias and
// merge-key resolution.
type yamlPair struct {
	key string
	val *yaml.Node
}

// mappingPairs returns the effective pairs of a mapping node the way the YAML
// decoder sees them: alias keys and values are dereferenced, and `<<` merge
// keys are expanded into the pairs they contribute. Precedence follows
// yaml.v3: an explicit key wins over one contributed by a merge, and within a
// merge sequence an earlier source wins over a later one on a shared key
// (duplicate explicit keys are invalid YAML; first-occurrence-wins there
// matches the single-$ref case the pre-merge-aware code handled). n is
// dereferenced by this call, so an alias-valued schema node can be passed
// directly; a node that is not a mapping (including nil) yields no pairs.
func mappingPairs(n *yaml.Node) []yamlPair {
	return collectMappingPairs(deref(n), map[*yaml.Node]bool{}, 0)
}

// collectMappingPairs implements mappingPairs' recursion into `<<` merge
// sources. visited and depth bound it against a self-referential merge (`&a
// {<<: *a}`): each mapping node is expanded into the effective-pairs
// computation at most once per top-level mappingPairs call, and the depth cap
// stops an unbounded merge chain. Reaching either bound simply stops
// collecting further merge pairs rather than panicking — anchorCycle
// independently reports the alias-cycle document that would trigger the
// visited bound in practice.
func collectMappingPairs(n *yaml.Node, visited map[*yaml.Node]bool, depth int) []yamlPair {
	if n == nil || n.Kind != yaml.MappingNode || depth > maxCycleDepth || visited[n] {
		return nil
	}
	visited[n] = true

	var explicit, merged []yamlPair
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := deref(n.Content[i]), deref(n.Content[i+1])
		if key != nil && key.Kind == yaml.ScalarNode && key.Value == "<<" {
			merged = append(merged, mergeSourcePairs(val, visited, depth+1)...)
			continue
		}
		if key == nil || key.Kind != yaml.ScalarNode {
			continue // a non-scalar key (after deref) cannot name a schema keyword
		}
		explicit = append(explicit, yamlPair{key: key.Value, val: val})
	}
	return dedupeFirstWins(append(explicit, merged...))
}

// mergeSourcePairs expands one `<<` merge value into the pairs it
// contributes: a single mapping is one merge source, a sequence is several
// sources with an earlier element taking precedence over a later one on a
// shared key.
func mergeSourcePairs(val *yaml.Node, visited map[*yaml.Node]bool, depth int) []yamlPair {
	if val == nil || val.Kind != yaml.SequenceNode {
		return collectMappingPairs(val, visited, depth)
	}
	var out []yamlPair
	for _, item := range val.Content {
		out = append(out, collectMappingPairs(deref(item), visited, depth)...)
	}
	return dedupeFirstWins(out)
}

// dedupeFirstWins keeps only the first pair for each key, preserving order —
// the precedence rule mappingPairs documents for both explicit-over-merged
// keys and earlier-over-later merge sources.
func dedupeFirstWins(pairs []yamlPair) []yamlPair {
	seenKey := make(map[string]bool, len(pairs))
	out := make([]yamlPair, 0, len(pairs))
	for _, p := range pairs {
		if seenKey[p.key] {
			continue
		}
		seenKey[p.key] = true
		out = append(out, p)
	}
	return out
}

// unescapePointer decodes the RFC 6901 escapes in one JSON pointer token.
func unescapePointer(token string) string {
	token = strings.ReplaceAll(token, "~1", "/")
	return strings.ReplaceAll(token, "~0", "~")
}

// cyclicDiag builds a codeCyclicRef error diagnostic anchored at a node's
// line:col position, matching the provenance convention of the resolve path.
func cyclicDiag(srcIndex int, n *yaml.Node, format string, args ...any) ir.Diagnostic {
	prov := ir.Provenance{Source: srcIndex}
	if n != nil {
		prov.Pointer = fmt.Sprintf("%d:%d", n.Line, n.Column)
	}
	return diagf(ir.SeverityError, codeCyclicRef, prov, format, args...)
}
