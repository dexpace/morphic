package annotation

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/value"
)

// Bounds on one conversion. Both exist because a YAML node is a graph, not a
// tree: an alias may point at an ancestor, and a chain of aliases each naming
// the one above it expands multiplicatively.
//
// Neither is this compiler's real defence against either shape — scan's cycle
// detector refuses both long before a node reaches here, on budgets calibrated
// against a 1,693-spec corpus, and its allowances are far tighter than these.
// They are here so the walk is bounded by its own terms rather than by what
// happens to run before it, which is the whole point of a backstop: nothing
// about this file's correctness should depend on the caller.
//
// maxRawDepth is yaml.v3's own parse-time nesting cap, so it binds only on
// input the parser could not have produced by nesting alone: an alias cycle.
// Nothing a spec can legally nest reaches it.
const (
	maxRawDepth = 10000
	maxRawNodes = 1 << 20
)

// errMergeWantsMap reports a `<<` whose value is neither a mapping nor a
// sequence of mappings, matching yaml.v3's own refusal to guess.
var errMergeWantsMap = fmt.Errorf("map merge requires map or sequence of maps as the value")

// rawConv renders one YAML node as canonical JSON. It carries the node budget
// shared across the whole walk; depth is a parameter because it is a property
// of the path rather than of the conversion.
//
// JSON is assembled here rather than by marshalling a Go tree, for the same
// reason jsonObject does it: the members are already encoded, so handing them
// back to encoding/json only reopens the question of how a number is spelled —
// which is the bug this file exists to close (GitHub #32).
type rawConv struct {
	nodes int
}

// node renders any YAML node as canonical JSON.
func (c *rawConv) node(n *yaml.Node, depth int) (json.RawMessage, error) {
	if n == nil {
		return nil, fmt.Errorf("nil yaml node")
	}
	if depth > maxRawDepth {
		return nil, fmt.Errorf("nesting exceeds %d", maxRawDepth)
	}
	c.nodes++
	if c.nodes > maxRawNodes {
		return nil, fmt.Errorf("expands past %d nodes", maxRawNodes)
	}

	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) != 1 {
			return nil, fmt.Errorf("document node holds %d children", len(n.Content))
		}
		return c.node(n.Content[0], depth+1)
	case yaml.AliasNode:
		if n.Alias == nil {
			return nil, fmt.Errorf("alias %q resolves to nothing", n.Value)
		}
		return c.node(n.Alias, depth+1)
	case yaml.ScalarNode:
		return c.scalar(n)
	case yaml.SequenceNode:
		return c.sequence(n, depth)
	case yaml.MappingNode:
		return c.mapping(n, depth)
	default:
		return nil, fmt.Errorf("unsupported yaml node kind %d", n.Kind)
	}
}

// scalar renders a scalar node by its resolved tag.
//
// Only the numeric tags read the source text; every other tag is rendered the
// way a whole-tree Decode would have, so a timestamp still normalizes to
// RFC 3339 and a !!binary still carries its decoded bytes rather than its
// base64 spelling. Those two are lossy against the source and deliberately
// left that way here: they are a different mechanism from the float64 rounding
// this change fixes, and moving them belongs with its own reasoning (#242).
func (c *rawConv) scalar(n *yaml.Node) (json.RawMessage, error) {
	switch n.Tag {
	case "!!null":
		return json.RawMessage("null"), nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return nil, fmt.Errorf("bool literal %q: %w", n.Value, err)
		}
		if b {
			return json.RawMessage("true"), nil
		}
		return json.RawMessage("false"), nil
	case "!!int", "!!float":
		// The whole point of this file: the literal's exact decimal, never a
		// float64 rounding of it (GitHub #32). NumericLiteral resolves YAML's
		// own bases too, so 0o17 still reads 15 rather than 17.
		num, err := value.NumericLiteral(n)
		if err != nil {
			return nil, fmt.Errorf("numeric literal %q: %w", n.Value, err)
		}
		// BigVal's contract is that its text is a JSON-valid number, and this is
		// the one caller that splices it straight into a document rather than
		// into an ir.Value field. NewBigVal does not yet hold that contract for
		// a binary exponent — `!!float 1p4` is stored verbatim (GitHub #45) — so
		// the splice checks rather than trusts. Refusing here keeps a construct
		// no JSON can name out of the IR, which is what the decode this replaced
		// did with the same input, and leaves #45 to be settled on its own terms.
		if !json.Valid([]byte(num)) {
			return nil, fmt.Errorf("numeric literal %q renders as %q, which is not JSON", n.Value, num)
		}
		return json.RawMessage(num), nil
	case "!!str":
		return jsonString(n.Value), nil
	case "!!timestamp":
		var t time.Time
		if err := n.Decode(&t); err != nil {
			return nil, fmt.Errorf("timestamp literal %q: %w", n.Value, err)
		}
		// The spelling time.Time's own MarshalJSON produces. It is reproduced
		// rather than called because that method reports an error for a year
		// outside [0,9999], which YAML's timestamp resolution cannot produce —
		// an unreachable branch is worse than an explicit format.
		return jsonString(t.Format(time.RFC3339Nano)), nil
	case "!!binary":
		// yaml.v3 base64-decodes a !!binary node into a string (it rejects a
		// []byte target), so decode to string and carry the bytes from there.
		var raw string
		if err := n.Decode(&raw); err != nil {
			return nil, fmt.Errorf("binary literal: %w", err)
		}
		return jsonString(raw), nil
	default:
		return nil, fmt.Errorf("unsupported scalar tag %q", n.Tag)
	}
}

// sequence renders a YAML sequence as a JSON array, in source order.
func (c *rawConv) sequence(n *yaml.Node, depth int) (json.RawMessage, error) {
	var b strings.Builder
	b.WriteByte('[')
	for i, child := range n.Content {
		if i > 0 {
			b.WriteByte(',')
		}
		item, err := c.node(child, depth+1)
		if err != nil {
			return nil, err
		}
		b.Write(item)
	}
	b.WriteByte(']')
	return json.RawMessage(b.String()), nil
}

// mapping renders a YAML mapping as a JSON object, members in sorted key order.
//
// Sorted rather than source order on purpose: it is what the decode this
// replaced produced, since encoding/json sorts a Go map, and it is what the
// IR's determinism invariant asks of every map it serializes.
func (c *rawConv) mapping(n *yaml.Node, depth int) (json.RawMessage, error) {
	members := map[string]json.RawMessage{}
	if err := c.mappingInto(members, n, depth); err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(members))
	for k := range members {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(jsonString(k))
		b.WriteByte(':')
		b.Write(members[k])
	}
	b.WriteByte('}')
	return json.RawMessage(b.String()), nil
}

// mappingInto fills dst from n, leaving keys dst already holds untouched. That
// one rule is the whole of YAML's merge precedence: a mapping's own keys are
// written before its `<<` is read, and a sequence of merge sources is read in
// order, so the nearer declaration always wins.
func (c *rawConv) mappingInto(dst map[string]json.RawMessage, n *yaml.Node, depth int) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("expected a mapping, got yaml node kind %d", n.Kind)
	}
	if err := checkUniqueKeys(n); err != nil {
		return err
	}

	var merge *yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if isMergeKey(key) {
			merge = val
			continue
		}
		name, err := mapKey(key)
		if err != nil {
			return err
		}
		if _, taken := dst[name]; taken {
			continue
		}
		enc, err := c.node(val, depth+1)
		if err != nil {
			return err
		}
		dst[name] = enc
	}

	if merge == nil {
		return nil
	}
	return c.merge(dst, merge, depth)
}

// merge folds a `<<` value into dst: a mapping, an alias to one, or a sequence
// of either.
func (c *rawConv) merge(dst map[string]json.RawMessage, merge *yaml.Node, depth int) error {
	if depth > maxRawDepth {
		return fmt.Errorf("nesting exceeds %d", maxRawDepth)
	}
	if merge.Kind == yaml.SequenceNode {
		for _, item := range merge.Content {
			src, err := mergeSource(item)
			if err != nil {
				return err
			}
			if err := c.mappingInto(dst, src, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	src, err := mergeSource(merge)
	if err != nil {
		return err
	}
	return c.mappingInto(dst, src, depth+1)
}

// mergeSource resolves one merge source to the mapping it names.
func mergeSource(n *yaml.Node) (*yaml.Node, error) {
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return nil, fmt.Errorf("alias %q resolves to nothing", n.Value)
		}
		n = n.Alias
	}
	if n.Kind != yaml.MappingNode {
		return nil, errMergeWantsMap
	}
	return n, nil
}

// checkUniqueKeys rejects a mapping that names one key twice, which yaml.v3
// rejects by default and this compiler has therefore always refused.
func checkUniqueKeys(n *yaml.Node) error {
	for i := 0; i < len(n.Content); i += 2 {
		for j := i + 2; j < len(n.Content); j += 2 {
			a, b := n.Content[i], n.Content[j]
			if a.Kind == b.Kind && a.Value == b.Value {
				return fmt.Errorf("mapping key %q already defined at line %d", b.Value, a.Line)
			}
		}
	}
	return nil
}

// mapKey gives a mapping key its JSON name, rejecting every key JSON has no
// name for. The rule matches yaml.v3's own: a mapping decodes into Go's JSON
// model only when every key is a string, and a non-string key is what makes the
// whole construct unrepresentable rather than merely awkward (GitHub #144).
func mapKey(n *yaml.Node) (string, error) {
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
		return "", fmt.Errorf("mapping key %q is not a string", n.Value)
	}
	return n.Value, nil
}

// isMergeKey reports whether a key node is YAML's `<<` merge key, by the same
// test yaml.v3 applies.
func isMergeKey(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Value == "<<" &&
		(n.Tag == "" || n.Tag == "!" || n.ShortTag() == "!!merge")
}

// jsonString encodes s as a JSON string. encoding/json cannot fail on a string
// — ill-formed UTF-8 is rewritten to U+FFFD rather than refused — so the error
// it declares is discarded here exactly as jsonObject discards it for a key.
func jsonString(s string) json.RawMessage {
	encoded, _ := json.Marshal(s)
	return encoded
}
