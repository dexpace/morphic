// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// grafter writes the actions of one overlay into one tree as the Overlay
// Specification's Action Object says, recording each node it builds or
// rewrites, which is what the attribution later places.
//
// It never writes through an alias: a YAML alias shares its anchor's node, so
// writing into it would change every place the anchor is used, and an action
// selecting one is refused instead. What it builds holds no alias either: a
// copy is taken of what each alias names.
type grafter struct {
	version version
	root    *yaml.Node
	// written holds every node the overlay built or rewrote.
	written map[*yaml.Node]bool
	// parents holds the node holding each node of the tree, built the first
	// time a remove needs it and kept as the grafter builds, nil before.
	parents map[*yaml.Node]*yaml.Node
	// replaced holds the values the source declares that the action being
	// written replaced, for the sequence to report.
	replaced []replacement
}

// replacement is a primitive the source declares that an action replaced:
// what it held, where the source writes it, and the value that replaced it.
type replacement struct {
	was          string
	line, column int
	with         *yaml.Node
}

// newGrafter returns a grafter of the rules of version v over root.
func newGrafter(v version, root *yaml.Node) *grafter {
	return &grafter{version: v, root: root, written: map[*yaml.Node]bool{}}
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
// the tree. Under Overlay 1.1 the targets must be of one shape. An update the
// rules refuse is an error naming why, and ends the action where it stands.
func (g *grafter) write(targets []*yaml.Node, payload *yaml.Node) (bool, error) {
	payload = nodeview.Deref(payload)
	if payload == nil || payload.Kind == yaml.AliasNode {
		return false, errors.New("it writes an alias that names nothing")
	}
	if kinds := shapesOf(targets); g.version == v11 && len(kinds) > 1 {
		return false, fmt.Errorf("it selects %s, and an Overlay 1.1 update or copy selects nodes of one kind",
			strings.Join(kinds, " and "))
	}
	changed := false
	for _, t := range targets {
		c, err := g.writeInto(t, payload)
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
func (g *grafter) writeInto(target, payload *yaml.Node) (bool, error) {
	switch shapeOf(target) {
	case shapeObject:
		if shapeOf(payload) != shapeObject {
			return false, fmt.Errorf("it writes %s into the object at %s, which only an object merges into", shapeOf(payload), where(target))
		}
		return g.mergeObject(target, payload, 0)
	case shapeArray:
		if shapeOf(payload) == shapeArray {
			return g.concat(target, payload, 0)
		}
		return g.appendTo(target, payload, 0)
	case shapePrimitive:
		if g.version == v10 {
			return false, fmt.Errorf("it selects the primitive at %s, and an Overlay 1.0 target selects objects and arrays", where(target))
		}
		if shapeOf(payload) != shapePrimitive {
			return false, fmt.Errorf("it writes %s over the primitive at %s, which only a primitive replaces", shapeOf(payload), where(target))
		}
		return g.replace(target, payload), nil
	case shapeAlias:
		return false, fmt.Errorf("it selects the YAML alias at %s, whose anchor shares its value with every use of it", where(target))
	default:
		return false, errors.New("it selects no value")
	}
}

// mergeObject merges the members of update into the object target, depth
// levels below the node the action selected: a member target lacks is added,
// and one it holds is merged as mergeMember says. A member is matched by the
// key the parser reads, at the last of its copies, the one the parser keeps.
func (g *grafter) mergeObject(target, update *yaml.Node, depth int) (bool, error) {
	if depth > maxPayloadDepth {
		return false, fmt.Errorf("it writes an update nested past %d levels", maxPayloadDepth)
	}
	held := map[string]*yaml.Node{}
	for i := 0; i+1 < len(target.Content); i += 2 {
		if name, own := nodeview.KeyOf(target.Content[i]); own {
			held[name] = target.Content[i+1]
		}
	}
	changed := false
	for i := 0; i+1 < len(update.Content); i += 2 {
		key, value := update.Content[i], nodeview.Deref(update.Content[i+1])
		name, own := nodeview.KeyOf(key)
		current, found := held[name]
		if !own || !found {
			added, err := g.attach(target, key, value, depth)
			if err != nil {
				return false, err
			}
			if own {
				held[name] = added
			}
			changed = true
			continue
		}
		c, err := g.mergeMember(name, current, value, depth+1)
		if err != nil {
			return false, err
		}
		changed = changed || c
	}
	return changed, nil
}

// mergeMember merges value into current, the member name of an object both
// write: a primitive replaces a primitive, an array is concatenated with an
// array, an object is merged into an object, and any other pair is refused,
// as is a member the source writes as an alias.
func (g *grafter) mergeMember(name string, current, value *yaml.Node, depth int) (bool, error) {
	if value == nil || value.Kind == yaml.AliasNode {
		return false, fmt.Errorf("it writes, under %q, an alias that names nothing", name)
	}
	was, with := shapeOf(current), shapeOf(value)
	switch {
	case was == shapeAlias:
		return false, fmt.Errorf("it writes into %q, the YAML alias at %s, whose anchor shares its value with every use of it",
			name, where(current))
	case was != with:
		return false, fmt.Errorf("it writes %s under %q, where the target holds %s at %s", with, name, was, where(current))
	case was == shapePrimitive:
		return g.replace(current, value), nil
	case was == shapeArray:
		return g.concat(current, value, depth)
	default:
		return g.mergeObject(current, value, depth)
	}
}

// replace writes the primitive value over the primitive target in place,
// keeping its node, and reports whether that changed it. A value the source
// declares, one no earlier action wrote, is recorded as replaced.
func (g *grafter) replace(target, value *yaml.Node) bool {
	tag := value.ShortTag()
	if target.ShortTag() == tag && target.Value == value.Value {
		return false
	}
	if !g.written[target] {
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
	g.adopt(target, item)
	return true, nil
}

// attach adds a copy of the member key: value to the object target and
// returns the copy of value.
func (g *grafter) attach(target, key, value *yaml.Node, depth int) (*yaml.Node, error) {
	k, err := g.clone(key, depth+1)
	if err != nil {
		return nil, err
	}
	v, err := g.clone(value, depth+1)
	if err != nil {
		return nil, err
	}
	target.Content = append(target.Content, k, v)
	g.adopt(target, k)
	g.adopt(target, v)
	return v, nil
}

// clone returns a copy of n for the tree, depth levels below the selected
// node, recording each node it builds as written. The caller adopts the copy.
func (g *grafter) clone(n *yaml.Node, depth int) (*yaml.Node, error) {
	return copyOf(n, nil, depth, func(holder, built *yaml.Node) {
		g.written[built] = true
		if holder != nil {
			g.adopt(holder, built)
		}
	})
}

// adopt records holder as what holds n, once the parents are indexed.
func (g *grafter) adopt(holder, n *yaml.Node) {
	if g.parents != nil {
		g.parents[n] = holder
	}
}

// copyOf returns a copy of n, which holder is to hold, depth levels below the
// node an action selected. The copy holds no alias: an alias is replaced by a
// copy of what it names, which is the value it stands for. It keeps no anchor,
// since the node it named is not the one being written, and no line and
// column, since the document it lands in does not write it there. built, when
// set, is handed each node the copy builds and the node holding it, nil for
// the copy's own root.
func copyOf(n, holder *yaml.Node, depth int, built func(holder, n *yaml.Node)) (*yaml.Node, error) {
	if depth > maxPayloadDepth {
		return nil, fmt.Errorf("it writes a value nested past %d levels", maxPayloadDepth)
	}
	from := nodeview.Deref(n)
	if from == nil || from.Kind == yaml.AliasNode {
		return nil, errors.New("it writes an alias that names nothing")
	}
	out := &yaml.Node{Kind: from.Kind, Style: from.Style, Tag: from.Tag, Value: from.Value}
	if built != nil {
		built(holder, out)
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

// remove deletes each target from the object or array holding it, which the
// parents index names. A target nothing holds, the document's root, is
// refused: the specification removes a node from the map or array it is
// contained in.
func (g *grafter) remove(targets []*yaml.Node) error {
	for _, t := range targets {
		holder := g.parents[t]
		if holder == nil || holder.Kind == yaml.DocumentNode {
			return errors.New("it selects the document's root, which no object or array holds")
		}
		if !detach(holder, t) {
			return fmt.Errorf("it selects a node at %s that what holds it holds as no member or element", where(t))
		}
	}
	return nil
}

// detach removes n from holder's Content, with its key from a mapping, and
// reports whether holder held it as a member's value or an element. A query
// selects no key, so holding n otherwise is a stale index.
func detach(holder, n *yaml.Node) bool {
	i := slices.Index(holder.Content, n)
	switch {
	case i < 0:
		return false
	case holder.Kind == yaml.MappingNode && i%2 == 1:
		holder.Content = slices.Delete(holder.Content, i-1, i+1)
	case holder.Kind == yaml.SequenceNode:
		holder.Content = slices.Delete(holder.Content, i, i+1)
	default:
		return false
	}
	return true
}

// indexParents indexes the node holding each node of the tree, once, and
// reports whether ctx let it finish. The index covers the whole tree, whose
// size the node budget and the loader's refusals bound.
func (g *grafter) indexParents(ctx context.Context) bool {
	if g.parents != nil {
		return true
	}
	parents := map[*yaml.Node]*yaml.Node{}
	out := nodeview.Walk{Nodes: math.MaxInt, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		for _, child := range n.Content {
			parents[child] = n
		}
		return nodeview.Descend
	}}.Run(ctx, g.root)
	if out.Cut {
		return false
	}
	g.parents = parents
	return true
}
