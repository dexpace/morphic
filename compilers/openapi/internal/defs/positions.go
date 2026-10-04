package defs

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// positions locates the containers of one document's tree as the library's
// CoreModel.GetJSONPointer does, without searching the tree for each: that
// search is a walk of the whole document per object, so asking it of every
// reference costs the square of the document. This walks once, recording where
// each container is first met in the order that search visits them, which is
// the pointer it answers.
type positions struct {
	root    *yaml.Node
	origins map[*yaml.Node]origin
	// visits counts the nodes the walk examined.
	visits int
}

// origin is the container a node was first met in, and the token that leads
// from that container to it.
type origin struct {
	parent *yaml.Node
	token  string
}

// frame is a container the walk is inside, and the next child to visit.
type frame struct {
	node *yaml.Node
	next int
}

// pointerEscaper escapes a token as RFC 6901 does.
var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// indexPositions walks the tree under root once. Aliases are followed, as the
// library's search follows them, and a container met a second time, through an
// alias, is not walked again: every position inside it was first met the first
// time, so what the walk records is the same and its cost is the tree's.
func indexPositions(root *yaml.Node) *positions {
	p := &positions{origins: map[*yaml.Node]origin{}}
	start := resolveAlias(root)
	if start != nil && start.Kind == yaml.DocumentNode {
		start = documentContent(start)
	}
	if start == nil {
		return p
	}
	p.root = start

	stack := []frame{{node: start}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		parent := top.node
		child, token, ok := top.nextChild()
		if !ok {
			stack = stack[:len(stack)-1]
			continue
		}
		p.visits++
		child = resolveAlias(child)
		if child == nil || !isContainer(child) {
			continue
		}
		if _, met := p.origins[child]; met || child == start {
			continue
		}
		p.origins[child] = origin{parent: parent, token: token}
		stack = append(stack, frame{node: child})
	}
	return p
}

// nextChild returns the next child of the frame's container and the token that
// leads to it. A mapping entry whose key is no scalar, or is empty, is skipped
// whole, as the library's search skips it.
func (f *frame) nextChild() (child *yaml.Node, token string, ok bool) {
	n := f.node
	switch n.Kind {
	case yaml.MappingNode:
		for f.next+1 < len(n.Content) {
			i := f.next
			f.next += 2
			if key := keyString(n.Content[i]); key != "" {
				return n.Content[i+1], key, true
			}
		}
	case yaml.SequenceNode:
		if f.next < len(n.Content) {
			i := f.next
			f.next++
			return n.Content[i], strconv.Itoa(i), true
		}
	}
	return nil, "", false
}

// pointerOf returns the pointer to node, "/" for the root, or "" when the tree
// holds no such container. Each step leads to the container met before this one,
// which bounds the walk.
func (p *positions) pointerOf(node *yaml.Node) jsontext.Pointer {
	node = resolveAlias(node)
	if node == nil || p.root == nil {
		return ""
	}
	if node == p.root {
		return "/"
	}
	var tokens []string
	for at := node; at != p.root; {
		o, ok := p.origins[at]
		if !ok {
			return ""
		}
		tokens = append(tokens, pointerEscaper.Replace(o.token))
		at = o.parent
	}
	var sb strings.Builder
	for i := len(tokens) - 1; i >= 0; i-- {
		sb.WriteByte('/')
		sb.WriteString(tokens[i])
	}
	return jsontext.Pointer(sb.String())
}

// documentContent returns what a document node holds, or nil when it is empty.
func documentContent(doc *yaml.Node) *yaml.Node {
	if len(doc.Content) == 0 {
		return nil
	}
	return resolveAlias(doc.Content[0])
}

// isContainer reports whether n is a mapping or a sequence, the only nodes a
// parsed object is built from and so the only ones worth locating.
func isContainer(n *yaml.Node) bool {
	return n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode
}

// keyString returns the key a mapping key node names, or "" when it is no
// scalar.
func keyString(key *yaml.Node) string {
	if key = resolveAlias(key); key == nil || key.Kind != yaml.ScalarNode {
		return ""
	}
	return key.Value
}

// resolveAlias follows an alias to the node it stands for.
func resolveAlias(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}
