// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package provenance

import (
	"encoding/json/jsontext"
	"slices"

	yaml "go.yaml.in/yaml/v3"
)

// Positions records the positions of one tree an overlay wrote, and the nodes
// sitting at them. Each position is held as the position holding it and the
// token leading there, so recording one costs what it adds, never the bytes of
// every key above it, which a long key over a wide graft makes quadratic in
// the graft (GitHub #875). A pointer is spelled only when asked for.
type Positions struct {
	// root is the tree's root, the empty pointer.
	root Position
	// steps holds every position recorded beneath root, by the step leading
	// to it.
	steps map[step]*Position
	// nodes holds the position of each node placed at one.
	nodes map[*yaml.Node]*Position
}

// Position is one position of the tree Positions records.
type Position struct {
	// holder is the position holding this one, nil at the root.
	holder *Position
	// token names this position within its holder.
	token string
	// written reports that the overlay introduced or rewrote what sits here.
	written bool
}

// step is how a position is reached from the one holding it.
type step struct {
	holder *Position
	token  string
}

// NewPositions returns a record of no position.
func NewPositions() *Positions {
	return &Positions{steps: map[step]*Position{}, nodes: map[*yaml.Node]*Position{}}
}

// Root returns the position of the tree's root, the empty pointer.
func (p *Positions) Root() *Position { return &p.root }

// Child returns the position token leads to from holder, recording it the
// first time it is asked for. holder must be one of p's positions.
func (p *Positions) Child(holder *Position, token string) *Position {
	s := step{holder: holder, token: token}
	if at, ok := p.steps[s]; ok {
		return at
	}
	at := &Position{holder: holder, token: token}
	p.steps[s] = at
	return at
}

// Write records that the overlay introduced or rewrote what sits at at.
func (at *Position) Write() { at.written = true }

// Place records n as a node sitting at at, which a finding raised on n is
// placed at.
func (p *Positions) Place(n *yaml.Node, at *Position) { p.nodes[n] = at }

// pointer spells at's JSON pointer, walking up to the root in as many steps
// as at is deep.
func (at *Position) pointer() jsontext.Pointer {
	var tokens []string
	for ; at.holder != nil; at = at.holder {
		tokens = append(tokens, at.token)
	}
	var out jsontext.Pointer
	for _, token := range slices.Backward(tokens) {
		out = out.AppendToken(token)
	}
	return out
}
