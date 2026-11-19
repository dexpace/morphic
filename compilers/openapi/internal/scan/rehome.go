// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package scan

import (
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// rehome places each node aliases mount at the least of its places: where it
// is written and where each alias of it is, pointers compared token by token
// (lessTokens), whichever the source writes first (GitHub #1796); what it
// holds moves with it. Targets are settled outermost first, each once. A tree
// whose aliases lead back into what holds them settles nothing: it is refused
// at the cycle. It returns how many targets it settled.
func rehome(steps map[*yaml.Node]step) int {
	mounts := map[*yaml.Node][]*yaml.Node{}
	for n := range steps {
		if n.Kind != yaml.AliasNode || n.Alias == nil {
			continue
		}
		if _, held := steps[n.Alias]; held {
			mounts[n.Alias] = append(mounts[n.Alias], n)
		}
	}
	order, complete := settleOrder(steps, mounts)
	if !complete {
		return 0
	}
	for _, target := range order {
		best := steps[target]
		bestTokens := tokensOf(steps, target)
		for _, alias := range mounts[target] {
			if tokens := tokensOf(steps, alias); lessTokens(tokens, bestTokens) {
				best, bestTokens = steps[alias], tokens
			}
		}
		steps[target] = best
	}
	return len(order)
}

// settleOrder orders the targets mounts names so that each comes after every
// target holding it or one of its aliases, as steps writes the tree, and
// reports whether it ordered them all: a target that holds itself so, through
// an alias inside it, is never ordered.
func settleOrder(steps map[*yaml.Node]step, mounts map[*yaml.Node][]*yaml.Node) ([]*yaml.Node, bool) {
	waits := make(map[*yaml.Node]int, len(mounts))
	frees := map[*yaml.Node][]*yaml.Node{}
	for target, aliases := range mounts {
		holders := holdingTargets(steps, mounts, append([]*yaml.Node{target}, aliases...))
		waits[target] = len(holders)
		for h := range holders {
			frees[h] = append(frees[h], target)
		}
	}
	var ready, order []*yaml.Node
	for target, n := range waits {
		if n == 0 {
			ready = append(ready, target)
		}
	}
	for len(ready) > 0 {
		target := ready[len(ready)-1]
		ready = ready[:len(ready)-1]
		order = append(order, target)
		for _, next := range frees[target] {
			if waits[next]--; waits[next] == 0 {
				ready = append(ready, next)
			}
		}
	}
	return order, len(order) == len(mounts)
}

// holdingTargets returns the targets mounts names that hold one of places, as
// steps writes the tree.
func holdingTargets(steps map[*yaml.Node]step, mounts map[*yaml.Node][]*yaml.Node, places []*yaml.Node) map[*yaml.Node]bool {
	holders := map[*yaml.Node]bool{}
	for _, n := range places {
		for at := steps[n].holder; at != nil; at = steps[at].holder {
			if _, shared := mounts[at]; shared {
				holders[at] = true
			}
		}
	}
	return holders
}

// tokensOf returns the tokens of n's pointer in the tree steps indexes, from
// the top down.
func tokensOf(steps map[*yaml.Node]step, n *yaml.Node) []string {
	var tokens []string
	for at := n; ; {
		step, ok := steps[at]
		if !ok || step.holder == nil {
			break
		}
		tokens = append(tokens, step.token)
		at = step.holder
	}
	slices.Reverse(tokens)
	return tokens
}

// lessTokens reports whether the pointer a spells comes before b's, token by
// token, each token compared by its bytes as written, and a pointer that is a
// prefix of another first: `/A/x` before `/A-/x`, where the bytes of the whole
// strings would put `-` before `/`.
func lessTokens(a, b []string) bool {
	for i := range min(len(a), len(b)) {
		if c := strings.Compare(a[i], b[i]); c != 0 {
			return c < 0
		}
	}
	return len(a) < len(b)
}
