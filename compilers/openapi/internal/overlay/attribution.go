// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"slices"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
)

// attribute records where each node the overlay wrote sits in root's tree,
// reporting whether it visited the whole tree within walks.
//
// A position is recorded only on the path to a written node, as the position
// holding it and the token leading there, the first time a node at it was
// written. So what it records grows with what the overlay wrote, and with no
// key's length (GitHub #875). A key the overlay introduced is placed at its
// member's position too.
func attribute(ctx context.Context, root *yaml.Node, written map[*yaml.Node]bool, walks budget.Limit,
) (*provenance.Positions, bool) {
	positions := provenance.NewPositions()
	top := nodeview.DocumentRoot(root)
	if top == nil {
		return positions, true
	}
	stack := []*frame{{node: top, at: positions.Root()}}
	work := budget.New(walks)
	for len(stack) > 0 {
		if !work.Spend(1) || ctx.Err() != nil {
			return nil, false
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.node == nil {
			continue
		}
		if written[f.node] {
			at := f.position(positions)
			at.Write()
			positions.Place(f.node, at)
		}
		if f.key != nil && written[f.key] {
			positions.Place(f.key, f.position(positions))
		}
		stack = append(stack, children(f)...)
	}
	return positions, true
}

// frame is one node of the attribution walk: the frame holding it and what
// names it there, a mapping value's key or a sequence element's index, and,
// for a mapping's value, the key node it sits under. merged marks a `<<`
// merge's value, whose members the parser reads as the merging mapping's own,
// so it and a list of them are placed at that mapping. at is its position,
// once one is recorded.
type frame struct {
	node, key *yaml.Node
	holder    *frame
	name      string
	index     int
	element   bool
	merged    bool
	at        *provenance.Position
}

// token returns the pointer token naming f in its holder: its key, or its
// index spelled only now, so a walk spells none for a node nothing places.
func (f *frame) token() string {
	if f.element {
		return strconv.Itoa(f.index)
	}
	return f.name
}

// position returns f's position in positions, recording it and every position
// above it that is not recorded yet, in as many steps as f is deep.
func (f *frame) position(positions *provenance.Positions) *provenance.Position {
	var pending []*frame
	for at := f; at.at == nil; at = at.holder {
		pending = append(pending, at)
	}
	for _, p := range slices.Backward(pending) {
		if p.merged {
			p.at = p.holder.at
			continue
		}
		p.at = positions.Child(p.holder.at, p.token())
	}
	return f.at
}

// children returns the frames beneath f, placed the way the compiler places
// them: a mapping's values under their keys as nodeview.KeyOf names them, a
// sequence's elements under their positions, and a `<<` merge's value at the
// mapping merging it.
//
// Mapping keys are not walked in their own right; each rides on its value's
// frame, since a member is added with its key. An alias node is a leaf,
// because the content it stands for lives at the anchor's own position, which
// the walk reaches there, and the overlay writes no alias.
func children(f *frame) []*frame {
	switch f.node.Kind {
	case yaml.MappingNode:
		out := make([]*frame, 0, len(f.node.Content)/2)
		for i := 0; i+1 < len(f.node.Content); i += 2 {
			key, own := nodeview.KeyOf(f.node.Content[i])
			out = append(out, &frame{node: f.node.Content[i+1], key: f.node.Content[i], holder: f, name: key, merged: !own})
		}
		return out
	case yaml.SequenceNode:
		out := make([]*frame, 0, len(f.node.Content))
		for i, child := range f.node.Content {
			out = append(out, &frame{node: child, holder: f, index: i, element: !f.merged, merged: f.merged})
		}
		return out
	default:
		return nil
	}
}
