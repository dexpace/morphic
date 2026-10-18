package scan

import (
	"encoding/json/jsontext"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
)

// Least returns the node of nodes whose JSON pointer in root's tree, as
// Pointers gives it, is least in byte order: the site a refusal with several
// candidates names, which no reordering of the source's declarations moves
// (GitHub #912). A node with none, under a `<<` merge key or a non-scalar key,
// ranks after every node with one, and among those the first given wins. It
// returns nil for no nodes.
//
// It indexes the tree once and climbs from each node, so it costs the tree's
// size however deep the nodes sit, not their depth each.
func Least(root *yaml.Node, nodes []*yaml.Node) *yaml.Node {
	if len(nodes) <= 1 {
		if len(nodes) == 0 {
			return nil
		}
		return nodes[0]
	}
	steps, _ := stepsIn(root, sourceindex.MaxIndexedNodes)
	top := nodeview.DocumentRoot(root)
	want := map[*yaml.Node]bool{}
	for _, n := range nodes {
		if s, held := steps[n]; held && (s.holder != nil || n == top) {
			want[n] = true
		}
	}
	if len(want) == 0 {
		return nodes[0]
	}

	under := pathsTo(steps, nodes, want)
	at := top
	for !want[at] {
		at = leastStep(steps, under[at], want) // one level down a path to a wanted node
	}
	return at
}

// pathsTo maps each node on the path from the top of the tree to a wanted
// node to the nodes one step below it on such paths, each once, in the order
// nodes reaches them. Each climb stops at a node an earlier one marked, so the
// climbs together read each node once.
func pathsTo(steps map[*yaml.Node]step, nodes []*yaml.Node, want map[*yaml.Node]bool) map[*yaml.Node][]*yaml.Node {
	under := map[*yaml.Node][]*yaml.Node{}
	climbed := map[*yaml.Node]bool{}
	for _, n := range nodes {
		if !want[n] {
			continue
		}
		for at := n; !climbed[at]; at = steps[at].holder {
			climbed[at] = true
			holder := steps[at].holder
			if holder == nil {
				break
			}
			under[holder] = append(under[holder], at)
		}
	}
	return under
}

// leastStep returns, of the nodes one step below a node, the one under which
// the least pointer lies. Their pointers share everything up to the step, so
// the step's escaped token decides, followed by the separator for a node only
// leading to a wanted one, since what lies under it carries one: a wanted node
// itself is the least pointer under it, its own a prefix of every other.
func leastStep(steps map[*yaml.Node]step, below []*yaml.Node, want map[*yaml.Node]bool) *yaml.Node {
	var least *yaml.Node
	var leastKey string
	for _, n := range below {
		key := string(jsontext.Pointer("").AppendToken(steps[n].token))
		if !want[n] {
			key += "/"
		}
		if least == nil || key < leastKey {
			least, leastKey = n, key
		}
	}
	return least
}
