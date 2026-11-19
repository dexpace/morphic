// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"errors"
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// grafter writes the actions of one overlay into one tree as the Overlay
// Specification's Action Object says, recording each node it builds or
// rewrites, which is what the attribution later places.
//
// It writes one position of the value at a time (value.go): a node the tree
// shares between positions, through an alias, an anchor an alias names or a
// `<<` merge, is copied where it is written, and the copy is charged to the
// node budget before it is built.
type grafter struct {
	version version
	root    *yaml.Node
	// written holds every node the overlay built or rewrote.
	written map[*yaml.Node]bool
	// replaced holds the values the source declares that the action being
	// written replaced, for the sequence to report.
	replaced []replacement
	// named counts the aliases naming each node, read once per application
	// and raised as a copy keeps an alias.
	named map[*yaml.Node]int
	// size is the sequence's count of the tree's nodes against the node
	// budget, nil for no budget.
	size *budget.Budget
	// copies holds the nodes a copy of a shared value built, which stand at
	// a position the source does not write them at.
	copies map[*yaml.Node]bool
	// detached reports that a node an alias may name left the tree, by a
	// remove or a copy taking its place, which repairAliases then mends.
	detached bool
}

// replacement is a primitive the source declares that an action replaced:
// what it held, where the source writes it, and the value that replaced it.
type replacement struct {
	was          string
	line, column int
	with         *yaml.Node
}

// newGrafter returns a grafter of the rules of version versionValue over root, its node
// budget unbounded until the sequence sets it.
func newGrafter(versionValue version, root *yaml.Node) *grafter {
	return &grafter{version: versionValue, root: root, written: map[*yaml.Node]bool{}, named: map[*yaml.Node]int{},
		copies: map[*yaml.Node]bool{}}
}

// shape is the kind of JSON value a node is, as the specification's rules
// tell them apart, or what keeps it from being one.
type shape uint8

const (
	shapeObject shape = iota
	shapeArray
	shapePrimitive
	// shapeAlias is a YAML alias, which shares its anchor's value.
	shapeAlias
	// shapeNone is no value: a document node holding nothing.
	shapeNone
)

// shapeOf returns n's shape.
func shapeOf(n *yaml.Node) shape {
	switch n.Kind {
	case yaml.MappingNode:
		return shapeObject
	case yaml.SequenceNode:
		return shapeArray
	case yaml.ScalarNode:
		return shapePrimitive
	case yaml.AliasNode:
		return shapeAlias
	default:
		return shapeNone
	}
}

// String names s in a finding.
func (s shape) String() string {
	switch s {
	case shapeObject:
		return "an object"
	case shapeArray:
		return "an array"
	case shapePrimitive:
		return "a primitive"
	case shapeAlias:
		return "a YAML alias"
	default:
		return "no value"
	}
}

// shapesOf names the shapes of nodes, each once, in the order the shapes are
// declared, so the names do not turn on the order the nodes are selected in.
func shapesOf(nodes []*yaml.Node) []string {
	var present [shapeNone + 1]bool
	for _, n := range nodes {
		present[shapeOf(n)] = true
	}
	var names []string
	for s, held := range present {
		if held {
			names = append(names, shape(s).String())
		}
	}
	return names
}

// where names n's place in the tree for a finding: the line and column the
// source writes it at, or that the overlay built it.
func where(n *yaml.Node) string {
	if n.Line == 0 {
		return "a position the overlay built"
	}
	return fmt.Sprintf("line %d, column %d", n.Line, n.Column)
}

// write writes payload into each of targets, and reports whether that changed
// the tree. Under Overlay 1.1 and 1.2 the targets must be of one shape. An
// update the rules refuse is an error naming why, and ends the action where
// it stands.
func (g *grafter) write(ctx context.Context, targets []*yaml.Node, payload *yaml.Node) (bool, error) {
	payload = nodeview.Deref(payload)
	if payload == nil || payload.Kind == yaml.AliasNode {
		return false, errors.New("it writes an alias that names nothing")
	}
	if kinds := shapesOf(targets); g.version != v10 && len(kinds) > 1 {
		return false, fmt.Errorf("it selects %s, and an Overlay 1.1 update or copy selects nodes of one kind",
			strings.Join(kinds, " and "))
	}
	changed := false
	for _, t := range targets {
		c, err := g.writeInto(ctx, t, payload)
		if err != nil {
			return false, err
		}
		changed = changed || c
	}
	return changed, nil
}

// writeInto writes payload into target by the rules for a selected node:
// merged into an object, concatenated with or appended to an array, or
// replacing a primitive.
func (g *grafter) writeInto(ctx context.Context, target, payload *yaml.Node) (bool, error) {
	switch shapeOf(target) {
	case shapeObject:
		if shapeOf(payload) != shapeObject {
			return false, fmt.Errorf("it writes %s into the object at %s, which only an object merges into", shapeOf(payload), where(target))
		}
		return g.mergeObject(ctx, target, payload, 0)
	case shapeArray:
		if shapeOf(payload) == shapeArray {
			return g.concat(target, payload, 0)
		}
		return g.appendTo(target, payload, 0)
	case shapePrimitive:
		if g.version == v10 {
			return false, fmt.Errorf("it selects the primitive at %s, and an Overlay 1.0 update targets objects and arrays; "+
				"target the object holding it and update the member, or declare overlay: 1.1.0", where(target))
		}
		if shapeOf(payload) != shapePrimitive {
			return false, fmt.Errorf("it writes %s over the primitive at %s, which only a primitive replaces", shapeOf(payload), where(target))
		}
		return g.replace(target, payload), nil
	default:
		// A target is made the action's own before it is written, so no
		// parse leaves an alias or a document node here.
		return false, fmt.Errorf("it selects %s at %s, which no update writes", shapeOf(target), where(target))
	}
}

// mergeObject merges the members of update into the object target, depth
// levels below the node the action selected: a member target lacks is added,
// and one it holds is merged as mergeMember says. The object is first made
// one whose members are all its own (flatten), so a member is matched by the
// key the parser reads, at the last of its copies, and a member it holds is
// made its own to write before it is written.
func (g *grafter) mergeObject(ctx context.Context, target, update *yaml.Node, depth int) (bool, error) {
	if depth > maxPayloadDepth {
		return false, fmt.Errorf("it writes an update nested past %d levels", maxPayloadDepth)
	}
	if err := g.flatten(ctx, target); err != nil {
		return false, err
	}
	held := map[string]int{}
	for i := 0; i+1 < len(target.Content); i += 2 {
		if name, own := nodeview.KeyOf(target.Content[i]); own {
			held[name] = i + 1
		}
	}
	changed := false
	for i := 0; i+1 < len(update.Content); i += 2 {
		key, value := update.Content[i], nodeview.Deref(update.Content[i+1])
		name, own := nodeview.KeyOf(key)
		slot, found := held[name]
		if !own || !found {
			if err := g.attach(target, key, value, depth); err != nil {
				return false, err
			}
			if own {
				held[name] = len(target.Content) - 1
			}
			changed = true
			continue
		}
		current, err := g.ownSlot(ctx, target, slot)
		if err != nil {
			return false, err
		}
		c, err := g.mergeMember(ctx, name, current, value, depth+1)
		if err != nil {
			return false, err
		}
		changed = changed || c
	}
	return changed, nil
}

// mergeMember merges value into current, the member name of an object both
// write: a primitive replaces a primitive, an array is concatenated with an
// array, an object is merged into an object, and any other pair is refused.
func (g *grafter) mergeMember(ctx context.Context, name string, current, value *yaml.Node, depth int) (bool, error) {
	if value == nil || value.Kind == yaml.AliasNode {
		return false, fmt.Errorf("it writes, under %q, an alias that names nothing", name)
	}
	was, with := shapeOf(current), shapeOf(value)
	switch {
	case was != with:
		return false, fmt.Errorf("it writes %s under %q, where the target holds %s at %s", with, name, was, where(current))
	case was == shapePrimitive:
		return g.replace(current, value), nil
	case was == shapeArray:
		return g.concat(current, value, depth)
	default:
		return g.mergeObject(ctx, current, value, depth)
	}
}

// replace writes the primitive value over the primitive target in place,
// keeping its node, and reports whether that changed it. A value the source
// declares where it stands, one no earlier action wrote and no copy moved, is
// recorded as replaced.
func (g *grafter) replace(target, value *yaml.Node) bool {
	tag := value.ShortTag()
	if target.ShortTag() == tag && target.Value == value.Value {
		return false
	}
	if !g.written[target] && !g.copies[target] {
		g.replaced = append(g.replaced, replacement{was: target.Value, line: target.Line, column: target.Column, with: value})
	}
	target.Tag, target.Value, target.Style = tag, value.Value, value.Style
	g.written[target] = true
	return true
}

// concat appends a copy of each element of the array value to the array
// target, and reports whether there were any.
func (g *grafter) concat(target, value *yaml.Node, depth int) (bool, error) {
	for _, item := range value.Content {
		if _, err := g.appendTo(target, item, depth); err != nil {
			return false, err
		}
	}
	return len(value.Content) > 0, nil
}

// appendTo appends a copy of value to the array target as one element.
func (g *grafter) appendTo(target, value *yaml.Node, depth int) (bool, error) {
	item, err := g.clone(value, depth+1)
	if err != nil {
		return false, err
	}
	target.Content = append(target.Content, item)
	return true, nil
}

// attach adds a copy of the member key: value to the object target.
func (g *grafter) attach(target, key, value *yaml.Node, depth int) error {
	k, err := g.clone(key, depth+1)
	if err != nil {
		return err
	}
	v, err := g.clone(value, depth+1)
	if err != nil {
		return err
	}
	target.Content = append(target.Content, k, v)
	return nil
}

// clone returns a copy of n for the tree, depth levels below the selected
// node, recording each node it builds as written.
func (g *grafter) clone(n *yaml.Node, depth int) (*yaml.Node, error) {
	return copyOf(n, nil, depth, func(_, _, built *yaml.Node) bool {
		g.written[built] = true
		return true
	})
}

// copyOf returns a copy of n, which holder is to hold, depth levels below the
// node an action selected. The copy holds no alias: an alias is replaced by a
// copy of what it names, the value it stands for. It keeps no anchor, since
// the node it named is not the one being written, and no line and column,
// since the document it lands in does not write it there. built, when set, is
// handed each node built, the node it copies and its holder, nil for the root,
// and stops the copy by answering false.
func copyOf(n, holder *yaml.Node, depth int, built func(from, holder, n *yaml.Node) bool) (*yaml.Node, error) {
	if depth > maxPayloadDepth {
		return nil, fmt.Errorf("it writes a value nested past %d levels", maxPayloadDepth)
	}
	from := nodeview.Deref(n)
	if from == nil || from.Kind == yaml.AliasNode {
		return nil, errors.New("it writes an alias that names nothing")
	}
	out := &yaml.Node{Kind: from.Kind, Style: from.Style, Tag: from.Tag, Value: from.Value}
	if built != nil && !built(from, holder, out) {
		return nil, errCopyPastBound
	}
	if len(from.Content) == 0 {
		return out, nil
	}
	out.Content = make([]*yaml.Node, len(from.Content))
	for i, child := range from.Content {
		c, err := copyOf(child, out, depth+1, built)
		if err != nil {
			return nil, err
		}
		out.Content[i] = c
	}
	return out, nil
}

// errCopyPastBound reports a copy its caller stopped at the bound it keeps.
var errCopyPastBound = errors.New("it copies past the bound on what one copy builds")

// remove deletes each position from the object or array holding it, in one
// pass over each holder, so removing many members of one wide object reads it
// once, not once a member. A position nothing holds, the document's root, is
// refused: the specification removes a node from the map or array it is
// contained in.
func (g *grafter) remove(positions []position) error {
	doomed := make(map[*yaml.Node]bool, len(positions))
	var holders []*yaml.Node
	seen := map[*yaml.Node]bool{}
	for _, position := range positions {
		if position.holder == nil {
			return errors.New("it selects the document's root, which no object or array holds")
		}
		if !seen[position.holder] {
			seen[position.holder] = true
			holders = append(holders, position.holder)
		}
		doomed[position.raw] = true
	}
	for _, holder := range holders {
		detach(holder, doomed)
	}
	g.detached = true
	return nil
}

// detach removes from holder's Content each member whose value, and each
// element, doomed holds, a member with its key and with every earlier copy of
// that key, which removing the last would expose, and returns how many it
// removed. A key doomed holds is no member's value, and stays.
func detach(holder *yaml.Node, doomed map[*yaml.Node]bool) int {
	kept := holder.Content[:0]
	switch holder.Kind {
	case yaml.MappingNode:
		gone := doomedKeys(holder, doomed)
		for i := 0; i < len(holder.Content); i += 2 {
			if i+1 < len(holder.Content) && doomed[holder.Content[i+1]] {
				continue
			}
			if key, own := nodeview.KeyOf(holder.Content[i]); own && gone[key] {
				continue
			}
			kept = append(kept, holder.Content[i:min(i+2, len(holder.Content))]...)
		}
	case yaml.SequenceNode:
		for _, n := range holder.Content {
			if !doomed[n] {
				kept = append(kept, n)
			}
		}
	default:
		return 0
	}
	removed := len(holder.Content) - len(kept)
	clear(holder.Content[len(kept):])
	holder.Content = kept
	if holder.Kind == yaml.MappingNode {
		removed /= 2
	}
	return removed
}

// doomedKeys returns the key of each member of the mapping holder whose value
// doomed holds.
func doomedKeys(holder *yaml.Node, doomed map[*yaml.Node]bool) map[string]bool {
	gone := map[string]bool{}
	for i := 0; i+1 < len(holder.Content); i += 2 {
		if key, own := nodeview.KeyOf(holder.Content[i]); own && doomed[holder.Content[i+1]] {
			gone[key] = true
		}
	}
	return gone
}
