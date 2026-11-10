// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"reflect"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// boundsRelation judges two nodes alike in all but the bounds a and b. The node
// whose bounds say all the other's do is narrower, and two that say one thing
// are the same, as two written alike do without a keyword read. Otherwise
// neither holds the other, and the two meet, which is the verdict too on a
// keyword no one value states.
func boundsRelation(a, b *ir.Constraints) relation {
	if reflect.DeepEqual(a, b) {
		return relSame
	}
	if _, _, conflict := constraintsConflict(a, b); conflict {
		return relMeet
	}
	var met ir.Constraints
	Constraints(Constraints(&met, a), b)
	aHolds, bHolds := sameBounds(&met, a), sameBounds(&met, b)
	switch {
	case aHolds && bHolds:
		return relSame
	case aHolds:
		return relANarrower
	case bHolds:
		return relBNarrower
	default:
		return relMeet
	}
}

// sameBounds reports whether a and b say one thing of every keyword. No
// constraints and empty ones say nothing alike.
func sameBounds(a, b *ir.Constraints) bool {
	var none ir.Constraints
	if a == nil {
		a = &none
	}
	if b == nil {
		b = &none
	}
	return a.UniqueItems == b.UniqueItems &&
		!slices.ContainsFunc(constraintKeywords(), func(kw constraintKeyword) bool { return !kw.same(a, b) })
}

// boundsBeyond returns what c says that own does not: c without each keyword own
// states at the value c holds. It writes to neither.
func boundsBeyond(c, own *ir.Constraints) *ir.Constraints {
	beyond := *c
	for _, kw := range constraintKeywords() {
		if kw.same(c, own) {
			kw.clear(&beyond)
		}
	}
	beyond.UniqueItems = c.UniqueItems && !own.UniqueItems
	return &beyond
}

// boundsOf returns the constraints td's own node holds. A node of any other kind
// than the four the judgment can meet holds none it reads.
func boundsOf(td ir.TypeDef) *ir.Constraints {
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Constraints
	case *ir.List:
		return n.Constraints
	case *ir.MapT:
		return n.Constraints
	case *ir.Tuple:
		return n.Constraints
	default:
		return nil
	}
}

// withBounds returns a copy of td holding c, and td itself where boundsOf reads
// none of its kind. The copy shares the rest of td, which is read and never
// written: an editor clones a node before changing it.
func withBounds(td ir.TypeDef, c *ir.Constraints) ir.TypeDef {
	switch n := td.(type) {
	case *ir.Scalar:
		own := *n
		own.Constraints = c
		return &own
	case *ir.List:
		own := *n
		own.Constraints = c
		return &own
	case *ir.MapT:
		own := *n
		own.Constraints = c
		return &own
	case *ir.Tuple:
		own := *n
		own.Constraints = c
		return &own
	default:
		return td
	}
}

// meetConstraints returns what sets say together, each keyword taking the value
// saying what they all do (Constraints), whatever their order; a nil set says
// nothing. A keyword no one value states keeps the first set's value, and disagree
// hears of each later set pinning it otherwise: that set's index, the index the
// value was kept from, and the difference. No set is written to.
func meetConstraints(sets []*ir.Constraints, disagree func(i, kept int, detail string)) *ir.Constraints {
	keywords := constraintKeywords()
	setBy := make([]int, len(keywords))
	var kept *ir.Constraints
	for i, c := range sets {
		if c == nil {
			continue
		}
		if k, detail, conflict := constraintsConflict(kept, c); conflict {
			disagree(i, setBy[k], detail)
		}
		for k, kw := range keywords {
			if kw.set(c) && (kept == nil || !kw.set(kept)) {
				setBy[k] = i
			}
		}
		if kept == nil {
			own := *c
			kept = &own
			continue
		}
		kept = Constraints(kept, c)
	}
	return kept
}

// nodeBounds returns the constraints on the node ref names.
func nodeBounds(j typeJudge, ref ir.TypeRef) *ir.Constraints {
	td, ok := j.Resolve(ref.Target)
	if !ok {
		return nil
	}
	return boundsOf(td)
}

// allBounds returns the bounds p holds and those on its type's node together.
func allBounds(j typeJudge, p *ir.Property) *ir.Constraints {
	var all ir.Constraints
	return Constraints(Constraints(&all, p.Constraints), nodeBounds(j, p.Type))
}

// alike reports whether a and b are one type but for the bounds their nodes hold:
// the judgment on the two with those bounds set aside.
func alike(j typeJudge, a, b ir.TypeRef) bool {
	at, aok := j.Resolve(a.Target)
	bt, bok := j.Resolve(b.Target)
	return aok && bok && j.boundedNodes(withBounds(at, nil), withBounds(bt, nil), 0) == relSame
}
