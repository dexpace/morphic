package annotation

import (
	"encoding/json"
	"errors"
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
var errMergeWantsMap = errors.New("map merge requires map or sequence of maps as the value")

// maxAliasHops bounds an alias chain followed outside the node walk. An anchor
// may name an alias node, so a key or a merge source can sit a hop or more from
// the node it means; a chain past this is a cycle, and these two readers resolve
// without the budget node() would otherwise charge for the hop.
const maxAliasHops = 100

// resolveAlias follows an alias chain to the node it names.
func resolveAlias(n *yaml.Node) (*yaml.Node, error) {
	for hops := 0; n.Kind == yaml.AliasNode; hops++ {
		if hops >= maxAliasHops {
			return nil, fmt.Errorf("alias %q chains past %d hops", n.Value, maxAliasHops)
		}
		if n.Alias == nil {
			return nil, fmt.Errorf("alias %q resolves to nothing", n.Value)
		}
		n = n.Alias
	}
	return n, nil
}

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
// The tag is read through ShortTag rather than off the node so that an untagged
// node resolves by its value, the way the Decode this replaced would have. A
// parser never hands one over — it resolves every scalar it emits, and writes
// the short form even for a spelled-out `!<tag:yaml.org,2002:int>` — but a
// caller assembling nodes can, and reading n.Tag would spell such a scalar as a
// string.
//
// A tag yaml.v3 assigns no type to keeps its text rather than being refused,
// which is also what that Decode did.
func (c *rawConv) scalar(n *yaml.Node) (json.RawMessage, error) {
	switch n.ShortTag() {
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
		return spliceNumber(n.Value, string(num))
	case "!!str":
		return jsonString(n.Value), nil
	case "!!timestamp", "!!binary":
		// The two tags YAML gives a type and JSON does not. Both keep the text
		// the source wrote, because the resolved form is derivable from the
		// spelling and the spelling is not derivable from the resolved form
		// (GitHub #242).
		//
		// Rendering the resolved form instead lost data both ways: a timestamp
		// came back RFC 3339, so `2021-1-1` acquired a padding, a time and a
		// zone the source never wrote, and a !!binary came back as its decoded
		// bytes, so `/w==` — the byte 0xFF — reached the IR as the U+FFFD
		// encoding/json substitutes for it, indistinguishable from a source
		// that wrote U+FFFD itself.
		//
		// The decode stays, and stays the tag check it has always been: a
		// scalar that does not satisfy the type it declares is refused exactly
		// as before, so only the kept spelling moved. Whether such a scalar
		// should instead survive as text — JSON can name it, and refusing costs
		// the caller the whole construct — is a question about what this walk
		// accepts rather than what it preserves, and is open as GitHub #245
		// rather than settled as a side effect here.
		return c.verbatimTagged(n)
	default:
		// A tag yaml.v3 cannot resolve carries no type it could decode to, so
		// its scalar comes back as the text it was written with — the behaviour
		// this walk replaced, and the lossless one: refusing would drop an
		// `!acme/thing` extension value the source did write.
		return jsonString(n.Value), nil
	}
}

// spliceNumber renders num — an already-validated NumericLiteral result — as
// raw JSON, refusing it if it is not one. This is the one caller that splices
// a numeric literal straight into a document rather than into an ir.Value
// field, so it is the one place BigVal's contract ("its text always renders
// as a JSON-valid number") has to hold rather than merely be assumed.
//
// NewBigVal enforces that contract itself now: it used to accept a binary
// exponent and store it verbatim, so `!!float 1p4` reached this splice
// unrefused (GitHub #45). No value NumericLiteral returns today can fail the
// check below, which is why it is exercised directly in
// TestSpliceNumber_RefusesANonJSONNumber rather than through scalar — the
// same reason scalar's routing into verbatimTagged is tested directly rather
// than through a caller. It stays rather than being deleted now that it is
// unreachable, because dropping it turns a future regression in BigVal's own
// promise into a document silently carrying a construct JSON cannot name,
// rather than a refusal.
func spliceNumber(source, num string) (json.RawMessage, error) {
	if !json.Valid([]byte(num)) {
		return nil, fmt.Errorf("numeric literal %q renders as %q, which is not JSON", source, num)
	}
	return json.RawMessage(num), nil
}

// verbatimTagged renders a scalar whose tag YAML resolves and JSON cannot name.
// Each arm only checks the tag, by the decode that used to supply the output,
// and the one return states the rule they share: what is kept is the source
// text. The two decode to different Go types and report differently — a
// timestamp names the offending text, a binary payload deliberately does not.
func (c *rawConv) verbatimTagged(n *yaml.Node) (json.RawMessage, error) {
	switch tag := n.ShortTag(); tag {
	case "!!timestamp":
		var when time.Time
		if err := n.Decode(&when); err != nil {
			return nil, fmt.Errorf("timestamp literal %q: %w", n.Value, err)
		}
	case "!!binary":
		// yaml.v3 base64-decodes a !!binary node into a string (it rejects a
		// []byte target), so the check decodes to string.
		var decoded string
		if err := n.Decode(&decoded); err != nil {
			return nil, fmt.Errorf("binary literal: %w", err)
		}
	default:
		// Unreachable through scalar, which routes only the two tags above
		// here. It stays so a third tag added to that arm is answered rather
		// than silently read as base64.
		return nil, fmt.Errorf("scalar tag %q is not kept verbatim", tag)
	}
	return jsonString(n.Value), nil
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
	named, err := resolveAlias(n)
	if err != nil {
		return nil, err
	}
	if named.Kind != yaml.MappingNode {
		return nil, errMergeWantsMap
	}
	return named, nil
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
	named, err := resolveAlias(n)
	if err != nil {
		return "", err
	}
	if named.Kind != yaml.ScalarNode || named.ShortTag() != "!!str" {
		return "", fmt.Errorf("mapping key %q is not a string", named.Value)
	}
	return named.Value, nil
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
