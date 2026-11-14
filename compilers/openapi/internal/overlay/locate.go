// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"errors"
	"fmt"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// locator finds the positions one action's targets name in the tree. Each
// path is numbered by the steps it takes, a prefix its targets share once,
// so a position selected twice is one target and each prefix is located,
// and made the action's own, once. A member's slot is read from its holder's
// index, built once. Every path's new link numbered and member indexed is
// charged to the application's steps first, so the work is linear in what
// the selection spent, whatever the paths' depth or a mapping's width.
type locator struct {
	g     *grafter
	steps *budget.Budget
	// ids numbers each path met by the link it ends in, and named each
	// position by the one holding it and the step from there.
	ids   map[jsonpath.Path]int
	named map[prefixKey]int
	// prefixes holds each numbered position, the root's at 0.
	prefixes []prefix
	indexes  map[*yaml.Node]memberIndex
}

// prefixKey names a position by the number of the one holding it and the step
// from there.
type prefixKey struct {
	step   jsonpath.Step
	parent int
}

// prefix is one numbered position: the step to it from the one holding it,
// and the node there once it is made the action's own, nil before.
type prefix struct {
	step   jsonpath.Step
	node   *yaml.Node
	parent int
	// target reports that a target of the action names the position.
	target bool
}

// memberIndex is a mapping's own members by key, each at the slot of its last
// copy, and whether the mapping writes a `<<` merge key.
type memberIndex struct {
	slots  map[string]int
	merges bool
}

// newLocator returns a locator over the tree g writes, charging what it
// does to steps. The tree holds a value (sequence.selected).
func newLocator(g *grafter, steps *budget.Budget) *locator {
	root := prefix{node: nodeview.Deref(nodeview.DocumentRoot(g.root))}
	return &locator{g: g, steps: steps, ids: map[jsonpath.Path]int{{}: 0}, named: map[prefixKey]int{},
		prefixes: []prefix{root}, indexes: map[*yaml.Node]memberIndex{}}
}

// spend charges n steps, refusing with jsonpath.ErrSteps once they are spent
// and with the context's error once it is done.
func (l *locator) spend(ctx context.Context, n int) error {
	if !l.steps.Spend(n) {
		return jsonpath.ErrSteps
	}
	return ctx.Err()
}

// number returns the number of the position p names, and whether a target
// named it before, then marks it named by one. Each link is numbered once, at
// a step, so a path costs only the links no path before it held.
func (l *locator) number(ctx context.Context, p jsonpath.Path) (int, bool, error) {
	var chain []jsonpath.Path
	at := p
	for {
		if _, known := l.ids[at]; known {
			break
		}
		chain = append(chain, at)
		at, _, _ = at.Parent() // the root's path is known, so at holds one
	}
	if err := l.spend(ctx, len(chain)); err != nil {
		return 0, false, err
	}
	id := l.ids[at]
	for _, link := range slices.Backward(chain) {
		_, step, _ := link.Parent()
		key := prefixKey{step: step, parent: id}
		next, known := l.named[key]
		if !known {
			next = len(l.prefixes)
			l.prefixes = append(l.prefixes, prefix{step: step, parent: id})
			l.named[key] = next
		}
		l.ids[link] = next
		id = next
	}
	before := l.prefixes[id].target
	l.prefixes[id].target = true
	return id, before, nil
}

// own makes each position on the way to the one numbered id the action's own
// to write, the first not yet made so onwards, and returns the node there.
func (l *locator) own(ctx context.Context, id int) (*yaml.Node, error) {
	var chain []int
	for ; l.prefixes[id].node == nil; id = l.prefixes[id].parent {
		if id == 0 {
			return nil, errors.New("it selects no value: the document holds none")
		}
		chain = append(chain, id)
	}
	node := l.prefixes[id].node
	for _, next := range slices.Backward(chain) {
		p := &l.prefixes[next]
		slot, err := l.slot(ctx, node, p.step, false)
		if err != nil {
			return nil, err
		}
		if node, err = l.g.ownSlot(ctx, node, slot); err != nil {
			return nil, err
		}
		p.node = node
	}
	return node, nil
}

// positions returns where each target ids numbers lies, for a remove, which
// takes a slot away: its holder made the action's own, and every member of
// that made its own, so removing one exposes none a merge brings in. Making a
// path its own may copy a node another target's slot holds, so the slots are
// read once every holder is its own. The root has no holder.
func (l *locator) positions(ctx context.Context, ids []int) ([]position, error) {
	holders := make([]*yaml.Node, len(ids))
	for i, id := range ids {
		if id == 0 {
			continue
		}
		holder, err := l.own(ctx, l.prefixes[id].parent)
		if err != nil {
			return nil, err
		}
		holders[i] = holder
	}
	out := make([]position, len(ids))
	for i, holder := range holders {
		if holder == nil {
			out[i] = position{node: l.prefixes[0].node}
			continue
		}
		slot, err := l.slot(ctx, holder, l.prefixes[ids[i]].step, true)
		if err != nil {
			return nil, err
		}
		out[i] = position{holder: holder, slot: slot, raw: holder.Content[slot], node: nodeview.Deref(holder.Content[slot])}
	}
	return out, nil
}

// slot returns the index in holder's Content of the value s names: an
// object's own member, at the last of its copies, or an array's element. A
// member a merge brings in is made the mapping's own first, as every member
// is where whole is set.
func (l *locator) slot(ctx context.Context, holder *yaml.Node, s jsonpath.Step, whole bool) (int, error) {
	if !s.Member {
		if holder.Kind == yaml.SequenceNode && s.Index < len(holder.Content) {
			return s.Index, nil
		}
		return 0, fmt.Errorf("the document no longer holds the element %d it selected", s.Index)
	}
	index, err := l.index(ctx, holder)
	if err != nil {
		return 0, err
	}
	slot, found := index.slots[s.Name]
	if index.merges && (whole || !found) {
		if err := l.g.flatten(ctx, holder); err != nil {
			return 0, err
		}
		delete(l.indexes, holder)
		if index, err = l.index(ctx, holder); err != nil {
			return 0, err
		}
		slot, found = index.slots[s.Name]
	}
	if !found {
		return 0, fmt.Errorf("the document no longer holds the member %q it selected", s.Name)
	}
	return slot, nil
}

// index returns the index of the mapping holder's own members, building it
// the first time, a step a member. A node that is no mapping has none.
func (l *locator) index(ctx context.Context, holder *yaml.Node) (memberIndex, error) {
	if index, built := l.indexes[holder]; built {
		return index, nil
	}
	if holder.Kind != yaml.MappingNode {
		return memberIndex{}, nil
	}
	if err := l.spend(ctx, len(holder.Content)/2); err != nil {
		return memberIndex{}, err
	}
	index := memberIndex{slots: make(map[string]int, len(holder.Content)/2)}
	for i := 0; i+1 < len(holder.Content); i += 2 {
		if key, own := nodeview.KeyOf(holder.Content[i]); own {
			index.slots[key] = i + 1
			continue
		}
		index.merges = true
	}
	l.indexes[holder] = index
	return index, nil
}
