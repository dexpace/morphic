// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"github.com/dexpace/morphic/ir"
)

// member is one declaration's type at a position of a field: the field's own
// type, or what a container that declaration's type names holds there.
type member struct {
	ref  ir.TypeRef
	decl int
}

// position is the members at one position of a field, every pair judged once:
// the one holding the most (winner) and those the field composes, judged alike
// for the field's own types and for what their containers hold.
type position struct {
	members []member
	// rel[i][j] is the judgment of members[i]'s type against members[j]'s.
	rel [][]relation
	// winner is the member whose type holds the most members' types.
	winner int
	// conjuncts are the members the position composes (composition), or none.
	conjuncts []conjunct
}

// judgePosition judges ms, members of decls at depth, against each other, each
// pair at the sites its two declarations were written, and picks the winner
// (widestCone) and the conjuncts (composition).
func (g *Merger) judgePosition(decls []declaration, ms []member, depth int) position {
	n := len(ms)
	p := position{members: ms, rel: make([][]relation, n)}
	for i := range n {
		p.rel[i] = make([]relation, n)
		p.rel[i][i] = relSame
	}
	for i := range n {
		for j := i + 1; j < n; j++ {
			judge := g.judgeAt(decls[ms[i].decl].prop.Provenance, decls[ms[j].decl].prop.Provenance)
			rel := judge.relateFrom(ms[i].ref, ms[j].ref, depth)
			p.rel[i][j], p.rel[j][i] = rel, rel.flipped()
		}
	}
	p.winner = p.widestCone()
	p.conjuncts = p.composition(registry{g.resolve})
	return p
}

// holds reports whether member i's type is the same as member j's or narrower,
// or meets it: a type of member i's, with what a meeting adds, loses nothing
// member j says of its type.
func (p position) holds(i, j int) bool {
	return p.rel[i][j] == relSame || p.rel[i][j] == relANarrower || p.rel[i][j] == relMeet
}

// widestCone returns the member whose type holds the most members' types, the
// first of them on a tie. Where one type is within every other, it is the
// intersection and wins whatever the order; where types meet, each holds the
// other, and the winner's node takes what each it meets adds (builder). Otherwise
// the position keeps the type that loses the fewest members, and each it loses
// is reported.
func (p position) widestCone() int {
	best, widest := 0, 0
	for i := range p.members {
		width := 0
		for j := range p.members {
			if p.holds(i, j) {
				width++
			}
		}
		if width > widest {
			best, widest = i, width
		}
	}
	return best
}

// meeting returns the winner and each other member of the cone whose type
// meets its (relMeet), the members a node holding them all is built of.
func (p position) meeting(cone func(k int) bool) (member, []member) {
	var partners []member
	for k, m := range p.members {
		if k != p.winner && cone(k) && p.rel[p.winner][k] == relMeet {
			partners = append(partners, m)
		}
	}
	return p.members[p.winner], partners
}

// null is what null the type of the members cone keeps admits: where every one
// of them does (intersectNull), as foldNullVerdicts states for a single schema.
// The top type admits null whatever its bit says, so a member of it below a
// winner of another type says nothing of null.
func (p position) null(reg registry, cone func(k int) bool, depth int) ir.TypeRef {
	var merged ir.TypeRef
	seeded := false
	for k, m := range p.members {
		switch {
		case !cone(k):
		case p.rel[p.winner][k] != relSame && targetsTop(reg, m.ref, depth):
		case !seeded:
			merged, seeded = m.ref, true
		default:
			merged = intersectNull(merged, m.ref)
		}
	}
	return merged
}

// targetsTop reports whether ref, a member's type at depth, targets the
// schemaless top type as the judgment reads one (registry.topAt).
func targetsTop(reg registry, ref ir.TypeRef, depth int) bool {
	td, ok := reg.resolve(ref.Target)
	return ok && reg.topAt(td, depth)
}
