// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// Flattening is the shape of a model across its composition, as
// [FlattenedProperties] computes it (ir-design §4.3): its properties and which
// are required, while each model's catch-all stays its own. It points into the
// registry it was computed from and copies no property, so it describes that
// registry as it stood; a caller that changes the registry flattens again.
type Flattening struct {
	// Properties are the members, one per wire name, each at the place the walk
	// first reads its wire name.
	Properties []FlatProperty
	// Undeclared are the names a Requires along the walk lists that no member
	// carries, once each, in the order the walk reads them: members only a
	// catch-all governs.
	Undeclared []string
	// Unflattened are the targets of the composition entries along the walk
	// that name no model once read through alias scalars, once each, in the
	// order the walk reads them. What such a conjunct contributes is no
	// property, so its consumer lowers it.
	Unflattened []TypeID
}

// FlatProperty is one member of a [Flattening]: the declaration it takes, the
// other declarations of its wire name, and whether an instance must carry it.
type FlatProperty struct {
	// PropertyDecl is the declaration the member takes: the last of its wire
	// name the walk reads, so a model's declaration overrides that of every
	// model it composes that does not compose it in turn.
	PropertyDecl
	// Overridden are the member's other declarations, once each, in the order
	// the walk first reads them.
	Overridden []PropertyDecl
	// Required reports that an instance must carry the member: one of its
	// declarations is Required, or a model along the walk lists its wire name
	// in Requires.
	Required bool
}

// PropertyDecl is one property declaration and the model declaring it.
type PropertyDecl struct {
	// Owner is the model whose Properties hold Property.
	Owner TypeID
	// Property points at the declaration in Owner's Properties.
	Property *Property
}

// FlattenedProperties returns the shape of the model root names across its
// composition (ir-design §4.3). Root and each composition entry are read
// through alias scalars. The walk goes depth first, following a model's Base,
// then Implements, then Mixins, and reads each model's own properties after
// those of every model it composes that does not compose it too: layout
// order. It reads the declarations [ExposedProps] reaches, one member per wire
// name, a later declaration overriding an earlier one in its place. A root
// naming no model has an empty shape; a cyclic composition is read to its
// fixpoint. It writes nothing to types.
func FlattenedProperties(types TypeRegistry, root TypeID) Flattening {
	f := flattener{types: types}
	return f.flatten(root)
}

// What a flattener's met holds for a type that is not a model on the walk's
// way: a model it has read, and an entry's target that names no model.
const (
	metRead        = -1
	metUnflattened = -2
)

// aliasOnChain is what a flattener's aliasOf holds for an alias scalar on the
// chain being read.
const aliasOnChain = -1

// flattener is the state of one [FlattenedProperties] walk.
type flattener struct {
	types TypeRegistry
	// met numbers each model the walk has met in the order it met them, until
	// it reads the model, and marks each entry target naming no model.
	met map[TypeID]int
	// finished are the models the walk is done composing but has yet to read,
	// in the order it finished them.
	finished []frame
	// members are the wire names read so far, in layout order, and index
	// places each name among them.
	members []member
	index   map[string]int
	// requires are the names a Requires lists, once each, in layout order.
	requires []string
	required map[string]bool
	// unflattened are the entry targets naming no model, in walk order.
	unflattened []TypeID
	// aliasOf places each alias scalar read through in aliasEnds, which holds
	// the type its chain stands for, so a chain is read once however many
	// entries name it.
	aliasOf   map[TypeID]int
	aliasEnds []TypeID
	// steps counts what the walk reads: each entry and model end, alias
	// scalar, property and Requires name.
	steps int
}

// member is one wire name's declarations, in the order the walk reads them.
type member struct {
	reads []PropertyDecl
}

// frame is a model the walk has met: the next of its composition entries to
// follow, the number met gives it, its low-link as Tarjan's algorithm keeps
// it, equal to num only for the first model of a component the walk met, and
// how many models were finished when it was met.
type frame struct {
	id    TypeID
	m     *Model
	next  int
	num   int
	low   int
	since int
}

// flatten is [FlattenedProperties] over f's registry.
func (f *flattener) flatten(root TypeID) Flattening {
	id := f.resolve(root)
	m, ok := f.types[id].(*Model)
	if !ok || m == nil {
		return Flattening{}
	}
	f.walk(id, m)
	return f.result()
}

// walk reads the model id names and every model it composes in layout order.
// It finds the composition's strongly connected components as Tarjan's
// algorithm does, and reads each when its first model finishes, so a model is
// read after every model it composes, unless that one composes it too; the
// models of one component are read in the order they finished. Each model is
// met once, so the walk follows at most every entry of the models it meets.
func (f *flattener) walk(id TypeID, m *Model) {
	f.met = map[TypeID]int{}
	f.index = map[string]int{}
	stack := []frame{f.meet(id, m)}
	for len(stack) > 0 {
		f.steps++
		top := &stack[len(stack)-1]
		if target, more := entry(top.m, top.next); more {
			top.next++
			if pid, pm, fresh := f.parent(target, top); fresh {
				stack = append(stack, f.meet(pid, pm))
			}
			continue
		}
		done := *top
		stack = stack[:len(stack)-1]
		if len(stack) > 0 {
			below := &stack[len(stack)-1]
			below.low = min(below.low, done.low)
		}
		f.finish(done)
	}
}

// meet numbers the model id names as the next the walk meets.
func (f *flattener) meet(id TypeID, m *Model) frame {
	num := len(f.met)
	f.met[id] = num
	return frame{id: id, m: m, num: num, low: num, since: len(f.finished)}
}

// finish records that the walk is done composing fr. When fr is the first
// model of its component the walk met, the component is every model finished
// since then and not yet read, and they are read now, in the order they
// finished.
func (f *flattener) finish(fr frame) {
	f.finished = append(f.finished, fr)
	if fr.low < fr.num {
		return
	}
	for _, c := range f.finished[fr.since:] {
		f.met[c.id] = metRead
		f.emit(c.id, c.m)
	}
	f.finished = f.finished[:fr.since]
}

// entry returns the target of m's i-th composition entry, counting its Base
// where it has one, then its Implements, then its Mixins, and false past the
// last.
func entry(m *Model, i int) (TypeID, bool) {
	if m.Base != nil {
		if i == 0 {
			return m.Base.Target, true
		}
		i--
	}
	if i < len(m.Implements) {
		return m.Implements[i].Target, true
	}
	i -= len(m.Implements)
	if i < len(m.Mixins) {
		return m.Mixins[i].Target, true
	}
	return "", false
}

// parent returns the model an entry of from naming target composes in, read
// through alias scalars, and whether the walk is yet to meet it. One met and
// not yet read shares a component with from, which from's low then says. An
// entry naming no model is recorded as unflattened, the first time the walk
// reads it, and an unset one names nothing at all.
func (f *flattener) parent(target TypeID, from *frame) (TypeID, *Model, bool) {
	if target == "" {
		return "", nil, false
	}
	id := f.resolve(target)
	m, ok := f.types[id].(*Model)
	if !ok || m == nil {
		if _, marked := f.met[target]; !marked {
			f.met[target] = metUnflattened
			f.unflattened = append(f.unflattened, target)
		}
		return "", nil, false
	}
	num, met := f.met[id]
	if !met {
		return id, m, true
	}
	if num != metRead {
		from.low = min(from.low, num)
	}
	return "", nil, false
}

// resolve returns the type id stands for: id itself unless it names an alias
// scalar, else the type at the end of its chain, or on a cyclic chain one of
// the cycle's scalars, which is no model either way. Each scalar is read once
// per walk.
func (f *flattener) resolve(id TypeID) TypeID {
	var path []TypeID
	cur := id
	for {
		f.steps++
		if at, seen := f.aliasOf[cur]; seen {
			if at != aliasOnChain {
				cur = f.aliasEnds[at]
			}
			break
		}
		base, alias := AliasBase(f.types, cur)
		if !alias {
			break
		}
		if f.aliasOf == nil {
			f.aliasOf = map[TypeID]int{}
		}
		f.aliasOf[cur] = aliasOnChain
		path = append(path, cur)
		cur = base
	}
	if len(path) > 0 {
		for _, s := range path {
			f.aliasOf[s] = len(f.aliasEnds)
		}
		f.aliasEnds = append(f.aliasEnds, cur)
	}
	return cur
}

// emit reads the model id names: each of its own properties, in source order,
// as a declaration of its wire name, and each name its Requires lists.
func (f *flattener) emit(id TypeID, m *Model) {
	for i := range m.Properties {
		f.steps++
		d := PropertyDecl{Owner: id, Property: &m.Properties[i]}
		wire := d.Property.EffectiveWireName()
		at, known := f.index[wire]
		if !known {
			at = len(f.members)
			f.index[wire] = at
			f.members = append(f.members, member{})
		}
		f.members[at].reads = append(f.members[at].reads, d)
	}
	for _, name := range m.Requires {
		f.steps++
		if f.required[name] {
			continue
		}
		if f.required == nil {
			f.required = map[string]bool{}
		}
		f.required[name] = true
		f.requires = append(f.requires, name)
	}
}

// result assembles the walk's members, the Requires names no member declares,
// and the entries naming no model.
func (f *flattener) result() Flattening {
	out := Flattening{Unflattened: f.unflattened}
	if len(f.members) > 0 {
		out.Properties = make([]FlatProperty, len(f.members))
	}
	for i, mb := range f.members {
		out.Properties[i] = mb.flat(f.required)
	}
	for _, name := range f.requires {
		if _, declared := f.index[name]; !declared {
			out.Undeclared = append(out.Undeclared, name)
		}
	}
	return out
}

// flat returns the member as a FlatProperty: the last declaration read takes
// it, and every other is overridden, listed at its first reading. required
// holds the names a Requires along the walk lists.
func (mb member) flat(required map[string]bool) FlatProperty {
	last := mb.reads[len(mb.reads)-1]
	fp := FlatProperty{PropertyDecl: last, Required: required[last.Property.EffectiveWireName()]}
	var listed map[PropID]bool
	for _, d := range mb.reads {
		fp.Required = fp.Required || d.Property.Required
		if d.Property.ID == last.Property.ID || listed[d.Property.ID] {
			continue
		}
		if listed == nil {
			listed = map[PropID]bool{}
		}
		listed[d.Property.ID] = true
		fp.Overridden = append(fp.Overridden, d)
	}
	return fp
}
