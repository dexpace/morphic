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

// concreteSchemaKeys are the mapping keys whose presence makes a schema node
// concrete rather than a bare reference. A node carrying any of them terminates
// a $ref chain even when it also has a $ref sibling — that is how legal
// recursion bottoms out on a real type.
var concreteSchemaKeys = map[string]bool{
	"type": true, "properties": true, "items": true, "prefixItems": true,
	"allOf": true, "oneOf": true, "anyOf": true, "not": true,
	"additionalProperties": true, "patternProperties": true,
	"enum": true, "const": true, "format": true,
	"$dynamicRef": true, "$recursiveRef": true,
}

// detectCycles scans raw source bytes for degenerate reference cycles that would
// otherwise crash the third-party parser with a fatal, unrecoverable stack
// overflow (GitHub #12). It runs BEFORE soa.Unmarshal so the anchor case never
// reaches the crashing parser, and reports two classes as error diagnostics: a
// recursive YAML anchor (an alias whose target is one of its own ancestors) and
// a pure-$ref cycle (a chain of $ref-only schemas that never reaches a concrete
// node). A source that does not decode as YAML yields no cycles: the main parser
// owns reporting that as a parse problem. The recover is a bounded-recursion
// backstop — a detector bug must degrade to "no cycle found", never abort.
func detectCycles(srcIndex int, data []byte) (diags []ir.Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			diags = nil
		}
	}()
	if len(data) == 0 {
		return nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil
	}
	docRoot := documentRoot(&root)
	if docRoot == nil {
		return nil
	}
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

// refCycle reports the first pure-$ref cycle: a chain of schemas whose only
// meaningful content is an internal $ref, followed until it returns to a node
// already on the chain. A $ref that reaches a concrete schema terminates the
// chain and is not a cycle.
func refCycle(srcIndex int, root *yaml.Node) (ir.Diagnostic, bool) {
	var refs []*yaml.Node
	collectPureRefs(root, &refs, 0)
	for _, start := range refs {
		if followRefChain(root, start) {
			return cyclicDiag(srcIndex, start, "cyclic $ref: schema reference chain never reaches a concrete type"), true
		}
	}
	return ir.Diagnostic{}, false
}

// collectPureRefs gathers every pure-reference schema node in the tree. Alias
// nodes are leaves here, so the descent stays bounded by the finite tree.
func collectPureRefs(n *yaml.Node, out *[]*yaml.Node, depth int) {
	if n == nil || depth > maxCycleDepth {
		return
	}
	if _, ok := pureRefTarget(n); ok {
		*out = append(*out, n)
	}
	for _, child := range n.Content {
		collectPureRefs(child, out, depth+1)
	}
}

// followRefChain follows pure-$ref edges from start and reports whether the
// chain loops back onto itself without reaching a concrete schema. It stops on a
// concrete node, a dangling ref, or a revisited node; the seen set and depth cap
// bound it against any structure.
func followRefChain(root, start *yaml.Node) bool {
	seen := map[*yaml.Node]bool{}
	cur := start
	for depth := 0; depth <= maxCycleDepth; depth++ {
		ref, ok := pureRefTarget(cur)
		if !ok {
			return false // reached a concrete schema — legal recursion
		}
		seen[cur] = true
		next := resolvePointer(root, ref)
		if next == nil {
			return false // dangling ref — reported downstream as unresolved
		}
		if seen[next] {
			return true // chain looped without ever reaching a concrete node
		}
		cur = next
	}
	return false
}

// pureRefTarget reports the internal $ref target of a pure-reference schema node.
// A node qualifies when it is a mapping carrying an internal ('#/...') $ref and
// no key that would make it a concrete schema; annotation-only siblings such as
// description or title do not disqualify it.
func pureRefTarget(n *yaml.Node) (string, bool) {
	if n == nil || n.Kind != yaml.MappingNode {
		return "", false
	}
	var ref string
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		if concreteSchemaKeys[key] {
			return "", false
		}
		if key == "$ref" && n.Content[i+1].Kind == yaml.ScalarNode {
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
