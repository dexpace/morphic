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
// recurses through, re-validate against FuzzCompile (cycles_fuzz_test.go), which
// drives arbitrary sources through Compile and faults if a degenerate cycle ever
// reaches the parser.

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

// collectSchemaRefs gathers every pure-$ref node reachable through a schema
// position, skipping reference objects and data subtrees that speakeasy never
// resolves as schema references. The walk is split between schema context
// (walkSchema) and the surrounding document (walkOutsideSchema).
func collectSchemaRefs(root *yaml.Node, out *[]*yaml.Node) {
	walkOutsideSchema(root, out, 0)
}

// walkOutsideSchema descends the OpenAPI document outside any schema, entering
// schema context at schema-valued keys and never collecting refs from data or
// extension subtrees. The finite tree and depth cap bound the descent.
func walkOutsideSchema(n *yaml.Node, out *[]*yaml.Node, depth int) {
	if n == nil || depth > maxCycleDepth {
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, val := n.Content[i].Value, n.Content[i+1]
			switch {
			case strings.HasPrefix(key, "x-"), schemaDataKeys[key]:
				// extension or example/default data: not a schema position
			case key == "schema":
				walkSchema(val, out, depth+1)
			case schemaEntryMapKeys[key]:
				walkSchemaMap(val, out, depth+1)
			default:
				walkOutsideSchema(val, out, depth+1)
			}
		}
	case yaml.SequenceNode:
		for _, child := range n.Content {
			walkOutsideSchema(child, out, depth+1)
		}
	}
}

// walkSchema visits one schema object: it collects the node when it is a pure
// $ref, then recurses only into sub-schema positions — never into type, enum,
// example, or extension data — so ref-shaped values never masquerade as schema
// references.
func walkSchema(n *yaml.Node, out *[]*yaml.Node, depth int) {
	if n == nil || depth > maxCycleDepth || n.Kind != yaml.MappingNode {
		return
	}
	if _, ok := pureRefTarget(n); ok {
		*out = append(*out, n)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i].Value, n.Content[i+1]
		switch {
		case subSchemaObjectKeys[key]:
			walkSchema(val, out, depth+1)
		case subSchemaMapKeys[key]:
			walkSchemaMap(val, out, depth+1)
		case subSchemaListKeys[key]:
			walkSchemaList(val, out, depth+1)
		}
	}
}

// walkSchemaMap visits each value of a name→schema mapping as a schema.
func walkSchemaMap(n *yaml.Node, out *[]*yaml.Node, depth int) {
	if n == nil || depth > maxCycleDepth || n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		walkSchema(n.Content[i+1], out, depth+1)
	}
}

// walkSchemaList visits each element of a schema sequence as a schema.
func walkSchemaList(n *yaml.Node, out *[]*yaml.Node, depth int) {
	if n == nil || depth > maxCycleDepth || n.Kind != yaml.SequenceNode {
		return
	}
	for _, child := range n.Content {
		walkSchema(child, out, depth+1)
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
func pureRefTarget(n *yaml.Node) (string, bool) {
	if n == nil || n.Kind != yaml.MappingNode {
		return "", false
	}
	var ref string
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == "$ref" && n.Content[i+1].Kind == yaml.ScalarNode {
			ref = n.Content[i+1].Value
		}
	}
	if !strings.HasPrefix(ref, "#/") {
		return "", false
	}
	return ref, true
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
// node named by one JSON pointer token, or nil when absent.
func childByToken(n *yaml.Node, token string) *yaml.Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == token {
				return n.Content[i+1]
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
