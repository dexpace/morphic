// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import "slices"

// Narrowing is how a model's composition narrows a union, as [NarrowedShapes]
// derives it (docs/ir/models.md#the-shapes-of-a-model-narrowing-a-union): the
// union, and either the shapes an instance takes or why none is listed. Like a
// [Flattening], it points into the registry it was computed from.
type Narrowing struct {
	// Union is the union the composition narrows, read through alias scalars;
	// "" where it narrows none, or two or more.
	Union TypeID
	// Shapes are the shapes an instance takes, one for each variant of Union
	// and, where the composition over one narrows a union in turn, one for each
	// variant of that union instead, in source order and depth first. It is
	// empty where Undistributed says why, and for a union with no variant.
	Shapes []NarrowedShape
	// Undistributed says why the composition narrows a union and Shapes lists
	// no shape of it; "" where it lists them or the composition narrows none.
	Undistributed Undistributed
}

// NarrowedShape is one shape an instance of a model narrowing a union takes:
// the model's flattening with a variant read in the place of each union it
// narrows.
type NarrowedShape struct {
	// Over are the variants read in the unions' places, outermost first, each
	// as its union's Variants write it: the first one of Narrowing.Union's, and
	// each later one of the union the composition over those before it
	// narrows in turn.
	Over []TypeID
	// Flattening is the model's flattening with Over's variants read in their
	// unions' places.
	Flattening
}

// Undistributed says why a model narrowing a union has no shape
// [NarrowedShapes] can list.
type Undistributed string

const (
	// UndistributedSeveral is a composition narrowing two unions or more: the
	// model's own, or one over the variants a shape reads.
	UndistributedSeveral Undistributed = "several"
	// UndistributedUnending is a composition no distribution ends: a variant
	// read in a union's place composes the model in turn, or a union reaches
	// itself through the unions among its variants.
	UndistributedUnending Undistributed = "unending"
	// UndistributedNotValues is a composition narrowing a union whose variants
	// are not the values it admits: one tagged by a key, an envelope or a tuple
	// element.
	UndistributedNotValues Undistributed = "not_values"
)

// NarrowedShapes returns how the composition of the model root names narrows a
// union (docs/ir/models.md#the-shapes-of-a-model-narrowing-a-union). It counts
// as steps each step its flattening walks take, each variant they read in a
// union's place or to tell whether a union lists the model, and each shape,
// and spent is the steps it took. Once they pass limit it stops without an
// answer and ok is false, spent then passing limit by at most what the walk it
// was taking read. It writes nothing to types.
func NarrowedShapes(types TypeRegistry, root TypeID, limit int) (n Narrowing, spent int, ok bool) {
	first := narrowedWalk(types, root, nil)
	spent = first.steps
	if spent > limit {
		return Narrowing{}, spent, false
	}
	switch len(first.narrowed) {
	case 0:
		return Narrowing{}, spent, true
	case 1:
		n.Union = first.narrowed[0].id
	default:
		return Narrowing{Undistributed: UndistributedSeveral}, spent, true
	}

	pending, why := deeper(nil, nil, first.narrowed[0])
	for why == "" && len(pending) > 0 {
		p := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		// Clipped, so the append never writes the over its siblings share,
		// whatever order they are walked in.
		over := append(slices.Clip(p.over), p.next)
		w := narrowedWalk(types, root, over)
		if spent += w.steps; spent > limit {
			return Narrowing{}, spent, false
		}
		switch {
		case w.unending:
			why = UndistributedUnending
		case len(w.narrowed) > 1:
			why = UndistributedSeveral
		case len(w.narrowed) == 1:
			pending, why = deeper(pending, over, w.narrowed[0])
		default:
			if spent++; spent > limit {
				return Narrowing{}, spent, false
			}
			n.Shapes = append(n.Shapes, NarrowedShape{Over: variantsOf(over), Flattening: w.flat})
		}
	}
	if why != "" {
		return Narrowing{Union: n.Union, Undistributed: why}, spent, true
	}
	return n, spent, true
}

// substitution names the variant a walk reads where it meets a union.
type substitution struct {
	// union is the union, read through alias scalars.
	union TypeID
	// variant is the variant's target, as the union's Variants write it.
	variant TypeID
}

// pendingShape is a composition NarrowedShapes is yet to walk: over, which it
// shares with every composition pushed beside it and never writes, extended
// by next. Sharing it keeps what a push holds to the variant it adds, as a
// walk charges for over only once it reads it.
type pendingShape struct {
	over []substitution
	next substitution
}

// deeper pushes onto pending, in reverse so that they pop in source order, the
// compositions over extended by each set variant of the union met, and says
// why it takes none: it tags its variants by a key, an envelope or a tuple
// element.
func deeper(pending []pendingShape, over []substitution, met metUnion) ([]pendingShape, Undistributed) {
	u := met.union
	if d := u.Discriminator; u.WireTagged && (d == nil || d.Envelope != "" || d.Index != nil) {
		return pending, UndistributedNotValues
	}
	for _, v := range slices.Backward(u.Variants) {
		if v.Type.Target == "" {
			continue // an unset variant names nothing, so no shape is over it
		}
		pending = append(pending, pendingShape{over: over, next: substitution{union: met.id, variant: v.Type.Target}})
	}
	return pending, ""
}

// variantsOf returns the variants over reads, in its order.
func variantsOf(over []substitution) []TypeID {
	out := make([]TypeID, len(over))
	for i, s := range over {
		out[i] = s.variant
	}
	return out
}

// walked is what one [NarrowedShapes] walk read: the flattening, the unions it
// narrows, whether no distribution ends it, and the steps it took.
type walked struct {
	flat     Flattening
	narrowed []metUnion
	unending bool
	steps    int
}

// narrowedWalk flattens the model root names with each of over's variants
// read in its union's place, and reads off it the unions the composition
// narrows and whether no distribution ends it.
func narrowedWalk(types TypeRegistry, root TypeID, over []substitution) walked {
	f := flattener{types: types, narrow: &narrowState{over: over}}
	flat := f.flatten(root)
	narrowed := f.narrowed()
	return walked{flat: flat, narrowed: narrowed, unending: f.narrow.unending, steps: f.steps}
}

// narrowState is what a [NarrowedShapes] walk reads beside the flattening.
type narrowState struct {
	// root is the model the walk flattens, read through alias scalars; ""
	// until the walk meets it.
	root TypeID
	// over are the variants the walk reads in unions' places, and at places
	// each union among them in over, so a union met is looked up once.
	over []substitution
	at   map[TypeID]int
	// ends holds, at each union's place in over, what the chain of variants
	// read from that union ends at, so the walk reads a chain once however
	// many entries stand for its unions.
	ends []variantEnd
	// unions are the unions the walk met that over reads no variant of, read
	// through alias scalars, once each in the order it met them.
	unions []metUnion
	seen   map[TypeID]bool
	// placed are the models a variant read in a union's place stands for.
	placed map[TypeID]bool
	// unending reports that no distribution ends the composition: a chain of
	// variants read in unions' places reached a union twice, or a placed model
	// composes the root in turn.
	unending bool
}

// variantEnd is what a chain of variants read in unions' places ends at: the
// last variant as its union writes it and the type that one stands for
// through alias scalars, both "" until the walk reads the chain.
type variantEnd struct {
	target, id TypeID
	read       bool
}

// metUnion is a union a walk met, and the ID it is registered under.
type metUnion struct {
	id    TypeID
	union *Union
}

// substitute returns what stands where an entry writes target, which stands
// for id: the two themselves unless id is a union the walk reads a variant of,
// else that variant as its union writes it and the type it stands for through
// alias scalars, and so on along a chain of them. Each meeting reads the
// variant, a step, and the walk reads a chain once, as it does an alias chain,
// each variant along it a step. One longer than over has reached a union
// twice, so the walk substitutes nothing more. A model it ends at is placed.
func (f *flattener) substitute(target, id TypeID) (TypeID, TypeID) {
	n := f.narrow
	if len(n.over) == 0 || n.unending {
		return target, id
	}
	if n.at == nil {
		n.at = make(map[TypeID]int, len(n.over))
		for i, s := range n.over {
			n.at[s.union] = i
		}
		n.ends = make([]variantEnd, len(n.over))
	}
	var chain []int
	for {
		i, substituted := n.at[id]
		if !substituted {
			break
		}
		if len(chain) == len(n.over) {
			n.unending = true
			return target, id
		}
		f.steps++
		if e := n.ends[i]; e.read {
			target, id = e.target, e.id
			break
		}
		chain = append(chain, i)
		target = n.over[i].variant
		id = f.resolve(target)
	}
	for _, i := range chain {
		n.ends[i] = variantEnd{target: target, id: id, read: true}
	}
	if m, ok := f.types[id].(*Model); len(chain) > 0 && ok && m != nil {
		if n.placed == nil {
			n.placed = map[TypeID]bool{}
		}
		n.placed[id] = true
	}
	return target, id
}

// meet records id, a type an entry stands for that names no model, as a union
// the walk met that it reads no variant of, unless it is no union or met
// already.
func (n *narrowState) meet(types TypeRegistry, id TypeID) {
	u, ok := types[id].(*Union)
	if !ok || u == nil || n.seen[id] {
		return
	}
	if _, substituted := n.at[id]; substituted {
		return
	}
	if n.seen == nil {
		n.seen = map[TypeID]bool{}
	}
	n.seen[id] = true
	n.unions = append(n.unions, metUnion{id: id, union: u})
}

// readRoot records the models the walk read in its root's strongly
// connected component, the last it reads: a placed one among them composes
// the root in turn.
func (n *narrowState) readRoot(component []frame) {
	for _, c := range component {
		n.unending = n.unending || n.placed[c.id]
	}
}

// narrowed returns the unions the walk met that the composition of its root
// narrows: each it read no variant of that does not list the root, read
// through alias scalars, since a model a union lists is that variant. Each
// variant it reads is a step.
func (f *flattener) narrowed() []metUnion {
	var out []metUnion
	for _, u := range f.narrow.unions {
		if !f.lists(u.union) {
			out = append(out, u)
		}
	}
	return out
}

// lists reports whether the union u has a variant standing for the walk's
// root, read through alias scalars, each variant it reads a step.
func (f *flattener) lists(u *Union) bool {
	for _, v := range u.Variants {
		f.steps++
		if f.resolve(v.Type.Target) == f.narrow.root {
			return true
		}
	}
	return false
}
