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
// ranks after every node with one. Of nodes sharing a pointer, as a repeated
// key's values can, or having none, the first given wins; nil for no nodes.
//
// It indexes the tree once and climbs from each node, so it costs the tree's
// size however deep the nodes sit.
func Least(root *yaml.Node, nodes []*yaml.Node) *yaml.Node {
	if len(nodes) <= 1 {
		if len(nodes) == 0 {
			return nil
		}
		return nodes[0]
	}
	steps, _ := stepsIn(root, sourceindex.MaxIndexedNodes)
	top := nodeview.DocumentRoot(root)
	want := map[*yaml.Node]int{} // each held node, and where nodes first gives it
	for i, n := range nodes {
		_, seen := want[n]
		if s, held := steps[n]; held && !seen && (s.holder != nil || n == top) {
			want[n] = i
		}
	}
	if len(want) == 0 {
		return nodes[0]
	}

	under := pathsTo(steps, nodes, want)
	// The nodes sharing the least pointer so far, a level further down each
	// turn; a repeated key gives one level more than one.
	for level := []*yaml.Node{top}; ; level = leastSteps(steps, level, under, want) {
		if at, found := firstWanted(level, want); found {
			return at
		}
	}
}

// firstWanted returns, of level's nodes, which share one pointer that is then
// less than every pointer below them, the wanted one given first.
func firstWanted(level []*yaml.Node, want map[*yaml.Node]int) (*yaml.Node, bool) {
	var first *yaml.Node
	for _, n := range level {
		if i, wanted := want[n]; wanted && (first == nil || i < want[first]) {
			first = n
		}
	}
	return first, first != nil
}

// pathsTo maps each node on the path from the top of the tree to a wanted
// node to the nodes one step below it on such paths, each once, in the order
// nodes reaches them. Each climb stops at a node an earlier one marked, so the
// climbs together read each node once.
func pathsTo(steps map[*yaml.Node]step, nodes []*yaml.Node, want map[*yaml.Node]int) map[*yaml.Node][]*yaml.Node {
	under := map[*yaml.Node][]*yaml.Node{}
	climbed := map[*yaml.Node]bool{}
	for _, n := range nodes {
		if _, wanted := want[n]; !wanted {
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

// leastSteps returns, of the nodes one step below those of level, which share
// one pointer, the ones under which the least pointer lies. Their pointers
// share everything up to the step, so the step's escaped token decides,
// followed by the separator for a node only leading to a wanted one, since
// what lies under it carries one: a wanted node itself is the least pointer
// under it, its own a prefix of every other.
func leastSteps(steps map[*yaml.Node]step, level []*yaml.Node, under map[*yaml.Node][]*yaml.Node,
	want map[*yaml.Node]int,
) []*yaml.Node {
	var least []*yaml.Node
	var leastKey string
	for _, at := range level {
		for _, n := range under[at] {
			key := string(jsontext.Pointer("").AppendToken(steps[n].token))
			if _, wanted := want[n]; !wanted {
				key += "/"
			}
			switch {
			case least == nil || key < leastKey:
				least, leastKey = append(least[:0], n), key
			case key == leastKey:
				least = append(least, n)
			}
		}
	}
	return least
}
